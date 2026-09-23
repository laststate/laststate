package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/api"
	"github.com/laststate/billing-service/internal/billing"
	"github.com/laststate/billing-service/internal/entitlement"
	"github.com/laststate/billing-service/internal/store"
	"github.com/laststate/billing-service/internal/webhook"
)

// version is set at build time via -ldflags "-X main.version=$TAG".
var version = "dev"

func main() {
	var (
		port      = flag.Int("port", 8080, "HTTP server port")
		dbURL     = flag.String("db", "", "PostgreSQL connection URL")
		traceURL  = flag.String("trace-url", "", "Trace Admin API URL")
		stripeKey = flag.String("stripe-key", "", "Stripe API key (fallback; prefer STRIPE_KEY env var)")
		mpKey     = flag.String("mp-key", "", "Mercado Pago API key (fallback; prefer MP_KEY env var)")
		cryptoKey = flag.String("crypto-key", "", "Coinbase Commerce API key (fallback; prefer CRYPTO_KEY env var)")
		nowKey    = flag.String("nowpayments-key", "", "NOWPayments API key (fallback; prefer NOWPAYMENTS_KEY env var)")
	)
	flag.Parse()

	// P2: read secrets from env vars; CLI flags are optional fallbacks only.
	stripeKeyStr := envOr("STRIPE_KEY", *stripeKey)
	mpKeyStr := envOr("MP_KEY", *mpKey)
	cryptoKeyStr := envOr("CRYPTO_KEY", *cryptoKey)
	nowKeyStr := envOr("NOWPAYMENTS_KEY", *nowKey)
	dbURLStr := envOr("DB_URL", *dbURL)
	traceURLStr := envOr("TRACE_URL", *traceURL)

	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("starting billing service", zap.String("version", version))

	// Mispriced plans or overage rules cost real money: refuse to start on
	// malformed configuration rather than falling back to defaults silently.
	if cfgErr := billing.LoadPricingConfigFromEnv(); cfgErr != nil {
		logger.Fatal("invalid BILLING_PRICING_JSON", zap.Error(cfgErr))
	}
	if cfgErr := billing.LoadOverageConfigFromEnv(); cfgErr != nil {
		logger.Fatal("invalid BILLING_OVERAGE_JSON", zap.Error(cfgErr))
	}

	db, err := store.NewPostgres(dbURLStr)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		logger.Fatal("failed to run migrations", zap.Error(err))
	}

	stripeProvider := billing.NewStripeProvider(stripeKeyStr)
	mpProvider := billing.NewMercadoPagoProvider(mpKeyStr)
	cryptoProvider := billing.NewCryptoProvider(cryptoKeyStr)

	billingService := billing.NewService(db, stripeProvider, mpProvider, cryptoProvider)
	if nowKeyStr != "" {
		billingService.SetNowPayments(billing.NewNowPaymentsProvider(nowKeyStr))
		logger.Info("nowpayments gateway enabled")
	}
	// Billing API v2: outbound signed event webhooks (push, don't poll).
	billingService.SetEventDispatcher(billing.NewEventDispatcher())
	entitlementService := entitlement.NewService(traceURLStr)
	entitlementService.SetLogger(logger)
	entitlementService.SetStore(db)

	if os.Getenv("TRACE_ADMIN_TOKEN") == "" {
		logger.Warn("TRACE_ADMIN_TOKEN is not set; Trace entitlement provisioning/deprovisioning will fail")
	}

	// Read webhook secrets from env vars
	mpWebhookSecret := os.Getenv("MP_WEBHOOK_SECRET")
	cryptoWebhookSecret := os.Getenv("CRYPTO_WEBHOOK_SECRET")
	stripeWebhookSecret := os.Getenv("STRIPE_WEBHOOK_SECRET")

	webhookHandler := webhook.NewHandler(billingService, entitlementService, logger, mpWebhookSecret, cryptoWebhookSecret, stripeWebhookSecret)

	authMiddleware := api.NewAuthMiddleware(api.AuthConfig{
		EnableAPIKeyAuth: true,
	}, logger)
	rateLimiter := api.NewRateLimiter(api.RateLimiterConfig{
		RequestsPerMinute: 1000,
	}, logger)
	router := api.NewRouter(billingService, entitlementService, webhookHandler, authMiddleware, rateLimiter, logger, api.DefaultRouterConfig())

	// Prometheus metrics
	metricsRouter := http.NewServeMux()
	metricsRouter.Handle("/metrics", promhttp.Handler())

	mux := http.NewServeMux()
	mux.Handle("/", router)
	mux.Handle("/metrics", metricsRouter)

	// Dunning worker: retry schedule + suspension chain. The worker loop
	// itself is unconditional once started, so gate startup on the flag:
	// set BILLING_DUNNING_ENABLED=false to run a dry API-only instance.
	if dunningCfg := billing.DunningConfigFromEnv(); dunningCfg.Enabled {
		dunningWorker := billing.NewDunningWorker(dunningCfg, db, logger)
		dunningWorker.SetSuspendHook(func(ctx context.Context, orgID uuid.UUID) {
			billingService.Events().Emit(ctx, orgID, billing.EventDunningEscalated, map[string]any{})
		})
		dunningWorker.Start(context.Background())
		defer dunningWorker.Stop()
		logger.Info("dunning worker started", zap.Duration("interval", dunningCfg.Interval))
	} else {
		logger.Info("dunning worker disabled")
	}

	// Trial sweeper: email trial-ended notices, then converge expired trials.
	trialSweepInterval := billing.TrialSweepIntervalFromEnv()
	go func() {
		ticker := time.NewTicker(trialSweepInterval)
		defer ticker.Stop()
		for range ticker.C {
			n, err := billingService.SweepTrials(context.Background(), entitlementService)
			if err != nil {
				logger.Warn("trial sweep failed", zap.Error(err))
				continue
			}
			if n > 0 {
				logger.Info("trial sweep converged orgs", zap.Int("count", n))
			}
		}
	}()

	// Usage billing cycle: monthly base + overage invoices per active
	// subscription. OFF by default — double-billing is worse than no
	// billing. Enable with BILLING_USAGE_BILLING_ENABLED=true and run at
	// most one instance; runs are idempotent per (org, subscription, period)
	// via usage_billing_runs, so restarts/retries reuse the first invoice.
	if os.Getenv("BILLING_USAGE_BILLING_ENABLED") == "true" {
		go func() {
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			for now := range ticker.C {
				start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
				n, err := billingService.BillUsageCycle(context.Background(), start, start.AddDate(0, 1, 0))
				if err != nil {
					logger.Warn("usage billing cycle failed", zap.Error(err))
					continue
				}
				logger.Info("usage billing cycle done", zap.Int("invoices", n))
			}
		}()
	}

	server := &http.Server{
		Addr:         ":" + strconv.Itoa(*port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("starting billing service", zap.Int("port", *port))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Fatal("server forced to shutdown", zap.Error(err))
	}
}

// envOr returns os.Getenv(name) if set, otherwise returns fallback.
// Used to read secrets from env vars with CLI flag fallback.
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
