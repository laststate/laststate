package billing

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/store"
)

func TestDunningWorkerLifecycle(t *testing.T) {
	db := newMockDB()
	cfg := DefaultDunningConfig()
	cfg.Interval = 10 * time.Millisecond
	worker := NewDunningWorker(cfg, db, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if worker.IsRunning() {
		t.Error("worker should not be running before Start")
	}

	worker.Start(ctx)
	if !worker.IsRunning() {
		t.Error("worker should be running after Start")
	}

	worker.Stop()
	if worker.IsRunning() {
		t.Error("worker should not be running after Stop")
	}
}

func TestDunningMultiChannelNotifications(t *testing.T) {
	db := newMockDB()
	ctx := context.Background()

	orgID := uuid.New()
	invID := uuid.New()
	attempt := store.PaymentAttempt{
		ID:             uuid.New(),
		OrganizationID: orgID,
		InvoiceID:      &invID,
		AttemptNumber:  3,
		Status:         "pending",
	}

	var mu sync.Mutex
	var emailsSent []DunningNotification
	var smssSent []DunningNotification
	var webhooksSent []DunningNotification

	cfg := DefaultDunningConfig()
	cfg.EmailEnabled = true
	cfg.SMSEnabled = true
	cfg.WebhookEnabled = true

	worker := NewDunningWorker(cfg, db, zap.NewNop())
	worker.SetEmailDispatcher(func(ctx context.Context, notif DunningNotification) error {
		mu.Lock()
		defer mu.Unlock()
		emailsSent = append(emailsSent, notif)
		return nil
	})
	worker.SetSMSDispatcher(func(ctx context.Context, notif DunningNotification) error {
		mu.Lock()
		defer mu.Unlock()
		smssSent = append(smssSent, notif)
		return nil
	})
	worker.SetWebhookDispatcher(func(ctx context.Context, notif DunningNotification) error {
		mu.Lock()
		defer mu.Unlock()
		webhooksSent = append(webhooksSent, notif)
		return nil
	})

	// 1. sendNotifications for schedule entry Day 3 (Email, SMS, Webhook)
	entry := &RetrySchedule{
		Day:           3,
		NotifyEmail:   true,
		NotifySMS:     true,
		NotifyWebhook: true,
		Action:        "retry",
	}
	worker.sendNotifications(ctx, attempt, entry)

	mu.Lock()
	if len(emailsSent) != 1 {
		t.Errorf("emailsSent = %d, want 1", len(emailsSent))
	}
	if len(smssSent) != 1 {
		t.Errorf("smssSent = %d, want 1", len(smssSent))
	}
	if len(webhooksSent) != 1 {
		t.Errorf("webhooksSent = %d, want 1", len(webhooksSent))
	}
	mu.Unlock()

	// 2. sendWarning (Email, SMS, Webhook)
	worker.sendWarning(ctx, attempt)
	mu.Lock()
	if len(emailsSent) != 2 {
		t.Errorf("emailsSent after warning = %d, want 2", len(emailsSent))
	}
	if len(smssSent) != 2 {
		t.Errorf("smssSent after warning = %d, want 2", len(smssSent))
	}
	if len(webhooksSent) != 2 {
		t.Errorf("webhooksSent after warning = %d, want 2", len(webhooksSent))
	}
	mu.Unlock()

	// 3. suspendOrg (Email, SMS, Webhook and status update)
	_ = db.InsertPaymentAttempt(ctx, &attempt)
	worker.suspendOrg(ctx, attempt)

	mu.Lock()
	if len(emailsSent) != 3 {
		t.Errorf("emailsSent after suspend = %d, want 3", len(emailsSent))
	}
	if len(smssSent) != 3 {
		t.Errorf("smssSent after suspend = %d, want 3", len(smssSent))
	}
	if len(webhooksSent) != 3 {
		t.Errorf("webhooksSent after suspend = %d, want 3", len(webhooksSent))
	}
	mu.Unlock()
}

func TestDunningCycleProcessing(t *testing.T) {
	db := newMockDB()
	ctx := context.Background()

	orgID := uuid.New()
	attemptID := uuid.New()
	past := time.Now().Add(-1 * time.Hour)
	attempt := &store.PaymentAttempt{
		ID:             attemptID,
		OrganizationID: orgID,
		AttemptNumber:  1,
		Status:         "pending",
		NextRetryAt:    &past,
	}
	_ = db.InsertPaymentAttempt(ctx, attempt)

	var notificationsReceived int
	var mu sync.Mutex

	cfg := DefaultDunningConfig()
	worker := NewDunningWorker(cfg, db, zap.NewNop())
	worker.SetEmailDispatcher(func(ctx context.Context, notif DunningNotification) error {
		mu.Lock()
		defer mu.Unlock()
		notificationsReceived++
		return nil
	})

	worker.runDunningCycle(ctx)

	mu.Lock()
	if notificationsReceived != 1 {
		t.Errorf("expected 1 notification on retry attempt 1, got %d", notificationsReceived)
	}
	mu.Unlock()

	// Verify attempt next retry was updated
	attempts, _ := db.ListPaymentAttemptsByOrg(ctx, orgID, 10)
	if len(attempts) == 0 {
		t.Fatal("expected payment attempt record")
	}
	if attempts[0].NextRetryAt == nil || !attempts[0].NextRetryAt.After(time.Now()) {
		t.Errorf("expected future nextRetryAt, got %v", attempts[0].NextRetryAt)
	}
}

func TestGetDunningStatus(t *testing.T) {
	db := newMockDB()
	ctx := context.Background()
	cfg := DefaultDunningConfig()
	worker := NewDunningWorker(cfg, db, zap.NewNop())

	orgID := uuid.New()

	// 1. No dunning
	status, err := worker.GetDunningStatus(ctx, orgID)
	if err != nil {
		t.Fatalf("GetDunningStatus: %v", err)
	}
	if status.Status != "no_dunning" {
		t.Errorf("status = %s, want no_dunning", status.Status)
	}

	// 2. Active dunning
	invID := uuid.New()
	attempt := &store.PaymentAttempt{
		ID:             uuid.New(),
		OrganizationID: orgID,
		InvoiceID:      &invID,
		AttemptNumber:  2,
		Status:         "pending",
		CreatedAt:      time.Now(),
	}
	_ = db.InsertPaymentAttempt(ctx, attempt)

	status, err = worker.GetDunningStatus(ctx, orgID)
	if err != nil {
		t.Fatalf("GetDunningStatus active: %v", err)
	}
	if status.Status != "active" {
		t.Errorf("status = %s, want active", status.Status)
	}
	if status.CurrentAttempt != 2 {
		t.Errorf("currentAttempt = %d, want 2", status.CurrentAttempt)
	}

	// 3. Suspended (terminal max_retries_exceeded state, strictly newest)
	suspendedAttempt := &store.PaymentAttempt{
		ID:             uuid.New(),
		OrganizationID: orgID,
		InvoiceID:      &invID,
		AttemptNumber:  5,
		Status:         "max_retries_exceeded",
		CreatedAt:      time.Now().Add(time.Second),
	}
	_ = db.InsertPaymentAttempt(ctx, suspendedAttempt)

	status, err = worker.GetDunningStatus(ctx, orgID)
	if err != nil {
		t.Fatalf("GetDunningStatus suspended: %v", err)
	}
	if status.Status != "suspended" {
		t.Errorf("status = %s, want suspended", status.Status)
	}
}
