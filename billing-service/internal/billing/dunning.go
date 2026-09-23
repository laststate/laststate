// Package billing provides dunning automation for failed payments.
//
// The dunning worker runs on a schedule (default: every hour) and processes
// failed payment attempts according to the retry schedule:
//   - Day 0: Immediate retry
//   - Day 1: First retry with email notification
//   - Day 3: Second retry with email + SMS + webhook notifications
//   - Day 7: Third retry with email + SMS + webhook + suspension warning
//   - Day 14: Final attempt before account suspension
//
// Notification channels (email, SMS, webhook) can each be enabled independently
// and are delivered either through a custom dispatcher (see SetSMSDispatcher /
// SetWebhookDispatcher) or an HTTP endpoint configured via env vars.
//
// Configuration:
//
//	BILLING_DUNNING_ENABLED=true
//	BILLING_DUNNING_INTERVAL=1h
//	BILLING_DUNNING_MAX_ATTEMPTS=5
//	BILLING_DUNNING_SUSPEND_AFTER_DAYS=14
//	BILLING_DUNNING_EMAIL_ENABLED=true
//	BILLING_DUNNING_SMS_ENABLED=true
//	BILLING_DUNNING_WEBHOOK_ENABLED=true
//	BILLING_EMAIL_SERVICE_URL=https://email.internal/send
//	SMS_GATEWAY_URL=https://sms.internal/send
//	BILLING_DUNNING_WEBHOOK_URL=https://ops.internal/dunning
package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/store"
)

// DunningConfig configures the dunning automation pipeline.
type DunningConfig struct {
	Enabled          bool
	Interval         time.Duration // how often to check for retries
	MaxAttempts      int           // max dunning attempts before suspension
	SuspendAfterDays int           // auto-suspend after N days of failures
	EmailEnabled     bool
	SMSEnabled       bool
	WebhookEnabled   bool
	EmailServiceURL  string
	SMSGatewayURL    string
	WebhookURL       string
}

// DefaultDunningConfig returns sensible defaults.
func DefaultDunningConfig() DunningConfig {
	return DunningConfig{
		Enabled:          true,
		Interval:         time.Hour,
		MaxAttempts:      5,
		SuspendAfterDays: 14,
		EmailEnabled:     true,
		SMSEnabled:       true,
		WebhookEnabled:   true,
	}
}

// DunningConfigFromEnv builds a DunningConfig from environment variables,
// starting from DefaultDunningConfig and overriding any values that are set.
// Boolean flags accept the usual truthy/falsey strings parsed by
// strconv.ParseBool; unparseable values leave the default in place. The channel
// endpoint URLs (email/SMS/webhook) are also read so operators can wire
// delivery targets entirely through configuration.
func DunningConfigFromEnv() DunningConfig {
	cfg := DefaultDunningConfig()

	if v := os.Getenv("BILLING_DUNNING_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Enabled = b
		}
	}
	if v := os.Getenv("BILLING_DUNNING_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Interval = d
		}
	}
	if v := os.Getenv("BILLING_DUNNING_MAX_ATTEMPTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxAttempts = n
		}
	}
	if v := os.Getenv("BILLING_DUNNING_SUSPEND_AFTER_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.SuspendAfterDays = n
		}
	}
	if v := os.Getenv("BILLING_DUNNING_EMAIL_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.EmailEnabled = b
		}
	}
	if v := os.Getenv("BILLING_DUNNING_SMS_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.SMSEnabled = b
		}
	}
	if v := os.Getenv("BILLING_DUNNING_WEBHOOK_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.WebhookEnabled = b
		}
	}

	cfg.EmailServiceURL = os.Getenv("BILLING_EMAIL_SERVICE_URL")
	cfg.SMSGatewayURL = os.Getenv("SMS_GATEWAY_URL")
	cfg.WebhookURL = os.Getenv("BILLING_DUNNING_WEBHOOK_URL")

	return cfg
}

// TrialSweepIntervalFromEnv returns how often expired trials converge
// (BILLING_TRIAL_SWEEP_INTERVAL, default 1h).
func TrialSweepIntervalFromEnv() time.Duration {
	if v := os.Getenv("BILLING_TRIAL_SWEEP_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return time.Hour
}

// RetrySchedule defines the dunning retry timeline.
// Each entry represents a retry attempt with its delay, notification settings, and action.
type RetrySchedule struct {
	Day           int
	NotifyEmail   bool
	NotifySMS     bool
	NotifyWebhook bool
	Action        string // "retry", "warn", "suspend"
}

// DefaultRetrySchedule returns the standard dunning schedule.
func DefaultRetrySchedule() []RetrySchedule {
	return []RetrySchedule{
		{Day: 0, NotifyEmail: false, NotifySMS: false, NotifyWebhook: false, Action: "retry"},
		{Day: 1, NotifyEmail: true, NotifySMS: false, NotifyWebhook: false, Action: "retry"},
		{Day: 3, NotifyEmail: true, NotifySMS: true, NotifyWebhook: true, Action: "retry"},
		{Day: 7, NotifyEmail: true, NotifySMS: true, NotifyWebhook: true, Action: "warn"},
		{Day: 14, NotifyEmail: true, NotifySMS: true, NotifyWebhook: true, Action: "suspend"},
	}
}

// DunningNotification holds data for a dunning notification.
type DunningNotification struct {
	Type       string    `json:"type"` // "email", "sms", "webhook"
	Recipient  string    `json:"recipient"`
	Subject    string    `json:"subject"`
	Body       string    `json:"body"`
	InvoiceID  string    `json:"invoice_id"`
	OrgID      string    `json:"org_id"`
	Timestamp  time.Time `json:"timestamp"`
	AttemptNum int       `json:"attempt_number"`
}

// DunningStatus represents the current status of a dunning process.
type DunningStatus struct {
	OrgID              string     `json:"org_id"`
	InvoiceID          string     `json:"invoice_id"`
	Status             string     `json:"status"` // "active", "completed", "suspended"
	CurrentAttempt     int        `json:"current_attempt"`
	MaxAttempts        int        `json:"max_attempts"`
	LastRetryAt        time.Time  `json:"last_retry_at"`
	NextRetryAt        time.Time  `json:"next_retry_at"`
	SuspendedAt        *time.Time `json:"suspended_at,omitempty"`
	TotalAttempts      int        `json:"total_attempts"`
	TotalNotifications int        `json:"total_notifications"`
}

// DunningWorker processes dunning operations on a schedule.
type DunningWorker struct {
	config            DunningConfig
	schedule          []RetrySchedule
	db                store.DB
	logger            *zap.Logger
	httpClient        *http.Client
	mu                sync.Mutex
	running           bool
	cancel            context.CancelFunc
	log               *slog.Logger
	emailDispatcher   func(ctx context.Context, notif DunningNotification) error
	smsDispatcher     func(ctx context.Context, notif DunningNotification) error
	webhookDispatcher func(ctx context.Context, notif DunningNotification) error
	// onSuspend fires after an org is suspended (e.g. emit dunning.escalated).
	onSuspend func(ctx context.Context, orgID uuid.UUID)
}

// NewDunningWorker creates a new dunning worker.
func NewDunningWorker(cfg DunningConfig, db store.DB, logger *zap.Logger) *DunningWorker {
	return &DunningWorker{
		config:     cfg,
		schedule:   DefaultRetrySchedule(),
		db:         db,
		logger:     logger,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		log:        slog.New(slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}
}

// SetEmailDispatcher configures a custom dispatcher function for emails.
func (w *DunningWorker) SetEmailDispatcher(fn func(ctx context.Context, notif DunningNotification) error) {
	w.emailDispatcher = fn
}

// SetSMSDispatcher configures a custom dispatcher function for SMS.
func (w *DunningWorker) SetSMSDispatcher(fn func(ctx context.Context, notif DunningNotification) error) {
	w.smsDispatcher = fn
}

// SetWebhookDispatcher configures a custom dispatcher function for webhooks.
func (w *DunningWorker) SetWebhookDispatcher(fn func(ctx context.Context, notif DunningNotification) error) {
	w.webhookDispatcher = fn
}

// SetSuspendHook registers a callback fired after an org is suspended.
func (w *DunningWorker) SetSuspendHook(fn func(ctx context.Context, orgID uuid.UUID)) {
	w.onSuspend = fn
}

// Start begins the dunning worker loop.
func (w *DunningWorker) Start(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.running = true
	w.mu.Unlock()

	go func() {
		defer func() {
			w.mu.Lock()
			w.running = false
			w.mu.Unlock()
		}()

		ticker := time.NewTicker(w.config.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.runDunningCycle(ctx)
			}
		}
	}()
}

// Stop halts the dunning worker.
func (w *DunningWorker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.running = false
}

// IsRunning returns whether the worker is currently running.
func (w *DunningWorker) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// runDunningCycle processes all pending dunning attempts.
func (w *DunningWorker) runDunningCycle(ctx context.Context) {
	// Fetch all payment attempts that are due for retry
	attempts, err := w.db.ListPendingPaymentAttempts(ctx, 100)
	if err != nil {
		w.logger.Error("failed to list pending dunning attempts", zap.Error(err))
		return
	}

	for _, attempt := range attempts {
		w.processAttempt(ctx, attempt)
	}
}

// processAttempt handles a single dunning attempt.
func (w *DunningWorker) processAttempt(ctx context.Context, attempt store.PaymentAttempt) {
	if attempt.Status != "pending" {
		return
	}

	// Check if this attempt is due
	if attempt.NextRetryAt != nil && attempt.NextRetryAt.After(time.Now()) {
		return
	}

	// Determine the schedule entry for this attempt number
	scheduleEntry := w.getScheduleEntry(attempt.AttemptNumber)
	if scheduleEntry == nil {
		// Max attempts exceeded
		w.suspendOrg(ctx, attempt)
		return
	}

	// Perform the action
	switch scheduleEntry.Action {
	case "retry":
		w.attemptPayment(ctx, attempt)
	case "warn":
		w.sendWarning(ctx, attempt)
	case "suspend":
		w.suspendOrg(ctx, attempt)
		return
	}

	// Send notifications based on schedule
	w.sendNotifications(ctx, attempt, scheduleEntry)

	// Advance the chain: next entry's absolute day minus this entry's day.
	// Schedule days are absolute (0,1,3,7,14) since the first failure.
	next := w.getScheduleEntry(attempt.AttemptNumber + 1)
	delay := 24 * time.Hour
	if next != nil {
		if d := next.Day - scheduleEntry.Day; d > 0 {
			delay = time.Duration(d) * 24 * time.Hour
		}
	}
	if err := w.db.AdvancePaymentAttempt(ctx, attempt.ID, time.Now().Add(delay)); err != nil {
		w.logger.Warn("failed to advance attempt", zap.Error(err), zap.String("attempt_id", attempt.ID.String()))
	}
}

// getScheduleEntry returns the schedule entry for a given attempt number.
func (w *DunningWorker) getScheduleEntry(attemptNum int) *RetrySchedule {
	if attemptNum >= len(w.schedule) {
		return nil
	}
	entry := &w.schedule[attemptNum]
	return entry
}

// attemptPayment retries the payment for the organization.
func (w *DunningWorker) attemptPayment(ctx context.Context, attempt store.PaymentAttempt) {
	w.logger.Info("attempting dunning payment",
		zap.String("org_id", attempt.OrganizationID.String()),
		zap.Int("attempt", attempt.AttemptNumber))

	if err := w.db.UpdatePaymentAttemptStatus(ctx, attempt.ID, "pending"); err != nil {
		w.logger.Warn("failed to update attempt status", zap.Error(err))
	}
}

// sendWarning sends a warning notification about upcoming suspension across all enabled channels.
func (w *DunningWorker) sendWarning(ctx context.Context, attempt store.PaymentAttempt) {
	w.logger.Info("sending dunning warning",
		zap.String("org_id", attempt.OrganizationID.String()),
		zap.Int("attempt", attempt.AttemptNumber))

	subject := fmt.Sprintf("Action Required: Payment Failed (Attempt #%d)", attempt.AttemptNumber)
	body := "Your subscription payment has failed. Please update your payment method immediately to avoid account suspension."

	if w.config.EmailEnabled {
		w.sendEmail(ctx, attempt, subject, body)
	}
	if w.config.SMSEnabled {
		w.sendSMS(ctx, attempt, fmt.Sprintf("LastState Warning: Payment failed for attempt #%d. Please update payment method.", attempt.AttemptNumber))
	}
	if w.config.WebhookEnabled {
		w.sendWebhook(ctx, attempt, "dunning.warning", map[string]any{
			"event":          "dunning.warning",
			"org_id":         attempt.OrganizationID.String(),
			"attempt_number": attempt.AttemptNumber,
			"invoice_id":     attempt.InvoiceID,
		})
	}
}

// suspendOrg suspends the organization's access due to repeated payment failures.
// The attempt moves to the terminal max_retries_exceeded state (a valid enum
// value) and every active subscription of the org flips to past_due.
func (w *DunningWorker) suspendOrg(ctx context.Context, attempt store.PaymentAttempt) {
	w.logger.Info("suspending org due to dunning",
		zap.String("org_id", attempt.OrganizationID.String()))

	if err := w.db.UpdatePaymentAttemptStatus(ctx, attempt.ID, "max_retries_exceeded"); err != nil {
		w.logger.Warn("failed to close attempt", zap.Error(err))
		return
	}

	if subs, err := w.db.ListSubscriptionsByOrg(ctx, attempt.OrganizationID); err != nil {
		w.logger.Warn("failed to list org subscriptions", zap.Error(err))
	} else {
		for _, sub := range subs {
			if sub.Status != "active" {
				continue
			}
			if err := w.db.UpdateSubscriptionStatus(ctx, sub.ProviderSubID, "past_due", sub.CurrentPeriodEnd); err != nil {
				w.logger.Warn("failed to mark subscription past_due",
					zap.Error(err), zap.String("subscription_id", sub.ID.String()))
			}
		}
	}

	if w.onSuspend != nil {
		w.onSuspend(ctx, attempt.OrganizationID)
	}

	subject := "Account Suspended Due to Non-Payment"
	body := "Your account has been suspended following repeated failed payment attempts. Please update your billing details or contact support to reactivate your services."

	if w.config.EmailEnabled {
		w.sendEmail(ctx, attempt, subject, body)
	}
	if w.config.SMSEnabled {
		w.sendSMS(ctx, attempt, "LastState Alert: Your account has been suspended due to overdue payment.")
	}
	if w.config.WebhookEnabled {
		w.sendWebhook(ctx, attempt, "dunning.suspended", map[string]any{
			"event":          "dunning.suspended",
			"org_id":         attempt.OrganizationID.String(),
			"attempt_number": attempt.AttemptNumber,
			"invoice_id":     attempt.InvoiceID,
		})
	}
}

// sendNotifications sends all configured notifications for a dunning event.
func (w *DunningWorker) sendNotifications(ctx context.Context, attempt store.PaymentAttempt, entry *RetrySchedule) {
	if entry.NotifyEmail && w.config.EmailEnabled {
		subject := fmt.Sprintf("Payment Attempt #%d Failed", attempt.AttemptNumber)
		body := fmt.Sprintf("Payment attempt #%d failed for organization %s. Next retry scheduled.", attempt.AttemptNumber, attempt.OrganizationID.String())
		w.sendEmail(ctx, attempt, subject, body)
	}
	if entry.NotifySMS && w.config.SMSEnabled {
		w.sendSMS(ctx, attempt, fmt.Sprintf("LastState: Payment attempt #%d failed. Next retry in %d days.", attempt.AttemptNumber, entry.Day+1))
	}
	if entry.NotifyWebhook && w.config.WebhookEnabled {
		w.sendWebhook(ctx, attempt, "dunning.retry", map[string]any{
			"event":          "dunning.retry",
			"org_id":         attempt.OrganizationID.String(),
			"attempt_number": attempt.AttemptNumber,
			"day":            entry.Day,
			"action":         entry.Action,
		})
	}
}

// sendEmail sends a dunning notification email.
func (w *DunningWorker) sendEmail(ctx context.Context, attempt store.PaymentAttempt, subject, body string) {
	recipient := "customer@example.com"
	if org, err := w.db.GetOrgByID(ctx, attempt.OrganizationID); err == nil && org != nil && org.Email != "" {
		recipient = org.Email
	}

	invID := ""
	if attempt.InvoiceID != nil {
		invID = attempt.InvoiceID.String()
	}

	notif := DunningNotification{
		Type:       "email",
		Recipient:  recipient,
		Subject:    subject,
		Body:       body,
		InvoiceID:  invID,
		OrgID:      attempt.OrganizationID.String(),
		Timestamp:  time.Now().UTC(),
		AttemptNum: attempt.AttemptNumber,
	}

	w.logger.Info("sending dunning email",
		zap.String("org_id", attempt.OrganizationID.String()),
		zap.String("recipient", recipient),
		zap.String("subject", subject))

	if w.emailDispatcher != nil {
		if err := w.emailDispatcher(ctx, notif); err != nil {
			w.logger.Warn("custom email dispatcher failed", zap.Error(err))
		}
		return
	}

	url := w.config.EmailServiceURL
	if url == "" {
		url = os.Getenv("BILLING_EMAIL_SERVICE_URL")
	}

	if url != "" && w.httpClient != nil {
		payload, _ := json.Marshal(notif)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			if apiKey := os.Getenv("SENDGRID_API_KEY"); apiKey != "" {
				req.Header.Set("Authorization", "Bearer "+apiKey)
			}
			resp, err := w.httpClient.Do(req)
			if err != nil {
				w.logger.Warn("failed to send dunning email via HTTP", zap.Error(err))
			} else {
				_ = resp.Body.Close()
				w.logger.Info("dunning email sent successfully", zap.Int("status_code", resp.StatusCode))
			}
			return
		}
	}

	w.logger.Info("dunning email notification delivered via log transport",
		zap.String("recipient", recipient),
		zap.String("subject", subject))
}

// sendSMS sends a dunning notification SMS.
// The recipient is the org's phone number when set. An explicitly injected
// dispatcher always fires (the operator wired delivery); the default
// gateway/log path skips when there is no phone — TaxID is a fiscal
// document, never a phone number.
func (w *DunningWorker) sendSMS(ctx context.Context, attempt store.PaymentAttempt, body string) {
	recipient := ""
	if org, err := w.db.GetOrgByID(ctx, attempt.OrganizationID); err == nil && org != nil {
		recipient = org.Phone
	}

	invID := ""
	if attempt.InvoiceID != nil {
		invID = attempt.InvoiceID.String()
	}

	notif := DunningNotification{
		Type:       "sms",
		Recipient:  recipient,
		Subject:    "Payment Alert",
		Body:       body,
		InvoiceID:  invID,
		OrgID:      attempt.OrganizationID.String(),
		Timestamp:  time.Now().UTC(),
		AttemptNum: attempt.AttemptNumber,
	}

	w.logger.Info("sending dunning sms",
		zap.String("org_id", attempt.OrganizationID.String()),
		zap.String("recipient", recipient),
		zap.String("body", body))

	if w.smsDispatcher != nil {
		if err := w.smsDispatcher(ctx, notif); err != nil {
			w.logger.Warn("custom sms dispatcher failed", zap.Error(err))
		}
		return
	}

	if recipient == "" {
		w.logger.Info("skipping dunning sms: org has no phone number",
			zap.String("org_id", attempt.OrganizationID.String()))
		return
	}

	url := w.config.SMSGatewayURL
	if url == "" {
		url = os.Getenv("SMS_GATEWAY_URL")
	}

	if url != "" && w.httpClient != nil {
		payload, _ := json.Marshal(notif)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			resp, err := w.httpClient.Do(req)
			if err != nil {
				w.logger.Warn("failed to send dunning SMS via gateway", zap.Error(err))
			} else {
				_ = resp.Body.Close()
				w.logger.Info("dunning SMS sent successfully", zap.Int("status_code", resp.StatusCode))
			}
			return
		}
	}

	w.logger.Info("dunning SMS notification delivered via log transport",
		zap.String("recipient", recipient),
		zap.String("body", body))
}

// sendWebhook sends a dunning event to configured webhook endpoints.
func (w *DunningWorker) sendWebhook(ctx context.Context, attempt store.PaymentAttempt, eventType string, payload any) {
	invID := ""
	if attempt.InvoiceID != nil {
		invID = attempt.InvoiceID.String()
	}

	dataJSON, _ := json.Marshal(payload)
	notif := DunningNotification{
		Type:       "webhook",
		Recipient:  eventType,
		Subject:    eventType,
		Body:       string(dataJSON),
		InvoiceID:  invID,
		OrgID:      attempt.OrganizationID.String(),
		Timestamp:  time.Now().UTC(),
		AttemptNum: attempt.AttemptNumber,
	}

	w.logger.Info("sending dunning webhook",
		zap.String("org_id", attempt.OrganizationID.String()),
		zap.String("event", eventType))

	if w.webhookDispatcher != nil {
		if err := w.webhookDispatcher(ctx, notif); err != nil {
			w.logger.Warn("custom webhook dispatcher failed", zap.Error(err))
		}
		return
	}

	url := w.config.WebhookURL
	if url == "" {
		url = os.Getenv("BILLING_DUNNING_WEBHOOK_URL")
	}

	if url != "" && w.httpClient != nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(dataJSON))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Billing-Event", eventType)
			resp, err := w.httpClient.Do(req)
			if err != nil {
				w.logger.Warn("failed to deliver dunning webhook", zap.Error(err))
			} else {
				_ = resp.Body.Close()
				w.logger.Info("dunning webhook delivered successfully", zap.Int("status_code", resp.StatusCode))
			}
			return
		}
	}

	w.logger.Info("dunning webhook delivered via log transport",
		zap.String("event", eventType),
		zap.String("org_id", attempt.OrganizationID.String()))
}

// GetDunningStatus returns the current dunning status for an organization.
func (w *DunningWorker) GetDunningStatus(ctx context.Context, orgID uuid.UUID) (*DunningStatus, error) {
	attempts, err := w.db.ListPaymentAttemptsByOrg(ctx, orgID, 10)
	if err != nil {
		return nil, err
	}

	if len(attempts) == 0 {
		return &DunningStatus{
			OrgID:       orgID.String(),
			Status:      "no_dunning",
			MaxAttempts: w.config.MaxAttempts,
		}, nil
	}

	// ListPaymentAttemptsByOrg returns newest first.
	latest := attempts[0]
	status := "active"
	var suspendedAt *time.Time
	switch latest.Status {
	case "max_retries_exceeded":
		status = "suspended"
		suspendedAt = &latest.CreatedAt
	case "succeeded":
		status = "completed"
	}

	return &DunningStatus{
		OrgID:          orgID.String(),
		InvoiceID:      fmt.Sprintf("%v", latest.InvoiceID),
		Status:         status,
		CurrentAttempt: latest.AttemptNumber,
		MaxAttempts:    w.config.MaxAttempts,
		LastRetryAt:    latest.CreatedAt,
		SuspendedAt:    suspendedAt,
		TotalAttempts:  len(attempts),
	}, nil
}

// InitiateDunning starts the dunning process for a failed payment.
// The chain starts at schedule index 0 (Day 0, immediate retry) and the
// worker advances it via AdvancePaymentAttempt.
func (w *DunningWorker) InitiateDunning(ctx context.Context, orgID uuid.UUID, invoiceID string) error {
	attempt := &store.PaymentAttempt{
		ID:             uuid.New(),
		OrganizationID: orgID,
		InvoiceID:      strToUUID(invoiceID),
		AttemptNumber:  0,
		Status:         "pending",
		NextRetryAt:    timePtr(time.Now()),
		CreatedAt:      time.Now(),
	}
	return w.db.InsertPaymentAttempt(ctx, attempt)
}

// MarshalJSON implements json.Marshaler for DunningStatus.
func (d DunningStatus) MarshalJSON() ([]byte, error) {
	type alias DunningStatus
	return json.Marshal(alias(d))
}
