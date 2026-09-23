package main

import (
	"context"
	"crypto/rand"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/laststate/trace/internal/api"
	"github.com/laststate/trace/internal/config"
	"github.com/laststate/trace/internal/db"
	"github.com/laststate/trace/internal/gc"
	"github.com/laststate/trace/internal/objects"
	"github.com/laststate/trace/internal/queue"
	"github.com/laststate/trace/internal/secretbox"
	"github.com/laststate/trace/internal/store"
	"github.com/laststate/trace/internal/worker"
)

func secretboxConfigure(key string) error { return secretbox.Configure(key) }

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg := config.Load()

	if cfg.IsLocal() {
		log.Info("starting in LOCAL deployment mode — everything unlocked (no auth gate, no quotas, no paywall)",
			"deployment", cfg.Deployment)
	}

	// Generate random admin password if empty and Bootstrap is enabled.
	// This ensures the admin user always has a strong password.
	if cfg.Bootstrap && cfg.AdminPassword == "" {
		cfg.AdminPassword = generateRandomPassword(24)
		log.Info("generated random admin password", "length", len(cfg.AdminPassword))
	}

	// Mock mode: preview the UI with fake data, no DB/S3/queue required.
	// Set TRACE_MOCK=true to enable.
	if os.Getenv("TRACE_MOCK") == "true" {
		log.Info("starting in MOCK mode — UI preview with fake data (no DB/S3/queue)")
		cfg.OpenUI = true
		cfg.AllowPublicRegister = true
		cfg.Mode = "api"

		webDir := envOr("TRACE_WEB_DIR", "web/dist")
		msrv := api.NewMockServer(webDir, log)
		httpSrv := &http.Server{
			Addr:              cfg.Listen,
			Handler:           msrv.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    cfg.MaxHeaderBytes,
		}

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		go func() {
			log.Info("mock server listening",
				"addr", cfg.Listen,
				"open_ui", true,
				"mode", "mock",
				"deployment", "local",
			)
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error("http", "err", err)
				cancel()
			}
		}()

		<-ctx.Done()
		shutdownTimeout := time.Duration(cfg.ShutdownTimeoutSec) * time.Second
		shutdownCtx, c := context.WithTimeout(context.Background(), shutdownTimeout)
		defer c()
		_ = httpSrv.Shutdown(shutdownCtx)
		return
	}

	// Production path
	warnings, err := cfg.ValidateProduction()
	for _, w := range warnings {
		log.Warn("config validation warning", "field", w.Field, "msg", w.Msg)
	}
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Error("migrate", "err", err)
		os.Exit(1)
	}

	obj := &objects.Store{
		Root:     cfg.ObjectDir,
		Endpoint: cfg.S3Endpoint, Region: cfg.S3Region, Bucket: cfg.S3Bucket,
		AccessKey: cfg.S3AccessKey, SecretKey: cfg.S3SecretKey,
	}
	if err := obj.Ensure(); err != nil {
		log.Error("object store", "err", err)
		os.Exit(1)
	}

	st := &store.Store{Pool: pool}
	if cfg.Bootstrap {
		if org, project, secret, err := st.Bootstrap(ctx); err == nil {
			prefix := secret
			if len(secret) > 12 {
				prefix = secret[:12] + "…"
			}
			tokenPath := filepath.Join(filepath.Dir(cfg.ObjectDir), "bootstrap-token.txt")
			_ = os.MkdirAll(filepath.Dir(tokenPath), 0o755)
			_ = os.WriteFile(tokenPath, []byte(secret+"\n"), 0o600)
			log.Info("bootstrap complete",
				"organization", org.Slug,
				"project", project.Slug,
				"ingest_token_prefix", prefix,
				"ingest_token_file", tokenPath,
			)
		} else if err.Error() != "already bootstrapped" {
			log.Error("bootstrap", "err", err)
			os.Exit(1)
		}
		if u, pw, err := st.EnsureAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword, "Admin"); err == nil {
			pwPath := filepath.Join(filepath.Dir(cfg.ObjectDir), "bootstrap-admin.txt")
			_ = os.WriteFile(pwPath, []byte(u.Email+"\n"+pw+"\n"), 0o600)
			log.Info("admin user created", "email", u.Email, "password_file", pwPath, "password_len", len(pw))
		} else if err.Error() != "admin already exists" {
			log.Error("admin bootstrap", "err", err)
			os.Exit(1)
		}
	}

	runWorkers := cfg.Mode == "all" || cfg.Mode == "worker"
	runAPI := cfg.Mode == "all" || cfg.Mode == "api"

	// Always construct the queue for API+worker so ingest and consumers share backend.
	q, qName, qerr := queue.NewFromEnv(pool, cfg.QueueDriver, cfg.QueueURL)
	if qerr != nil {
		log.Error("queue", "err", qerr)
		os.Exit(1)
	}
	// Warn if using in-process queue with separate API/worker processes.
	// In-memory queues are only suitable for single-process deployments.
	if (qName == "memory" || qName == "nats-memory-sim") && cfg.Mode != "all" {
		log.Warn("in-memory queue in use with separate API/worker mode — jobs will not be shared between processes", "queue", qName)
	}
	_ = strings.TrimSpace(qName)

	if err := secretboxConfigure(cfg.SecretsKey); err != nil {
		log.Error("secrets key", "err", err)
		os.Exit(1)
	}
	api.ConfigureTrustedProxies(cfg.TrustedProxies)

	if runWorkers {
		n := cfg.WorkerN
		if n <= 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			w := &worker.Worker{Store: st, Queue: q, Object: obj, Lease: cfg.Lease, Log: log}
			go w.Run(ctx)
		}
		if cfg.EnableRetention {
			gcr := &gc.Runner{Store: st, Object: obj, RetentionDays: cfg.RetentionDays, Log: log}
			go func() {
				interval := cfg.GCInterval
				if interval <= 0 {
					interval = time.Hour
				}
				t := time.NewTicker(interval)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						if err := gcr.RunOnce(ctx); err != nil {
							log.Error("gc", "err", err)
						}
					}
				}
			}()
		}
		log.Info("workers started", "n", n, "queue", qName)
	}

	if !runAPI {
		log.Info("worker-only mode")
		<-ctx.Done()
		return
	}

	webDir := envOr("TRACE_WEB_DIR", "web/dist")
	var ui http.FileSystem
	if stInfo, err := os.Stat(webDir); err == nil && stInfo.IsDir() {
		ui = http.Dir(webDir)
	} else {
		ui = http.FS(emptyFS{})
	}

	srv := &api.Server{Cfg: cfg, Store: st, Object: obj, Queue: q, Log: log, UI: ui}
	srv.LogInsecureDefaults()
	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}

	go func() {
		log.Info("listening",
			"addr", cfg.Listen,
			"public_url", cfg.PublicURL,
			"open_ui", cfg.OpenUI,
			"mode", cfg.Mode,
			"deployment", cfg.Deployment,
			"billing_enabled", !cfg.IsLocal(),
			"s3", cfg.S3Endpoint != "",
			"oidc", cfg.OIDCIssuer != "",
			"queue", cfg.QueueDriver,
		)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	shutdownTimeout := time.Duration(cfg.ShutdownTimeoutSec) * time.Second
	shutdownCtx, c := context.WithTimeout(context.Background(), shutdownTimeout)
	defer c()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type emptyFS struct{}

func (emptyFS) Open(name string) (fs.File, error) { return nil, fs.ErrNotExist }

// generateRandomPassword creates a cryptographically secure random password
// using URL-safe base64 characters (alphanumeric + _-).
func generateRandomPassword(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		// Fallback: should never happen in practice
		return "generated-password-fallback"
	}
	for i := range buf {
		buf[i] = chars[int(buf[i])%len(chars)]
	}
	return string(buf)
}
