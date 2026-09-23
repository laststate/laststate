package billing

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/store"
)

func TestReceiptEmailRenders(t *testing.T) {
	msg := ReceiptEmail("a@acme.dev", "Acme", 4900, "usd", "inv-1", []byte("%PDF-fake"))
	if !strings.Contains(msg.Subject, "49.00") {
		t.Fatalf("subject = %q, want amount", msg.Subject)
	}
	if !strings.Contains(msg.Body, "inv-1") || !strings.Contains(msg.Body, "Acme") {
		t.Fatalf("body missing invoice/org: %q", msg.Body)
	}
	if msg.To != "a@acme.dev" || msg.AttachName != "invoice-inv-1.pdf" {
		t.Fatalf("routing wrong: %+v", msg)
	}
}

func TestMailerLogTransport(t *testing.T) {
	m := MailerFromEnv()
	m.Host = ""
	if err := m.Send(context.Background(), ReceiptEmail("a@b.co", "Acme", 900, "usd", "i", nil)); err == nil {
		t.Fatal("expected log-transport error without SMTP host")
	}
}

func TestMailerMIMEWithAttachment(t *testing.T) {
	// Exercise MIME assembly without network: point at an unroutable
	// host and assert the failure happens at dial, not at rendering.
	m := &Mailer{Host: "127.0.0.1", Port: 1, From: "billing@laststate.dev"}
	msg := ReceiptEmail("a@b.co", "Acme", 900, "usd", "i-1", []byte("%PDF-1.4 fake"))
	err := m.Send(context.Background(), msg)
	if err == nil || !strings.Contains(err.Error(), "smtp") {
		t.Fatalf("want smtp transport error, got %v", err)
	}
}

func TestSweepTrials(t *testing.T) {
	db := newMockDB()
	svc := NewService(db, &mockProvider{}, &mockProvider{}, &mockProvider{})
	svc.SetLogger(zap.NewNop())

	past := time.Now().Add(-time.Hour)
	orgID := uuid.New()
	db.trialEndsAt[orgID] = &past

	calls := 0
	conv := TrialConvergerFunc(func(ctx context.Context) (int, error) {
		calls++
		return 2, nil
	})
	n, err := svc.SweepTrials(context.Background(), conv)
	if err != nil {
		t.Fatalf("SweepTrials: %v", err)
	}
	if n != 2 || calls != 1 {
		t.Fatalf("n = %d calls = %d, want 2/1", n, calls)
	}
}

func TestBillUsageCycle(t *testing.T) {
	db := newMockDB()
	svc := NewService(db, &mockProvider{}, &mockProvider{}, &mockProvider{})
	svc.SetLogger(zap.NewNop())
	ctx := context.Background()

	orgID := uuid.New()
	sub := &store.Subscription{
		ID: uuid.New(), OrganizationID: orgID, Provider: "stripe",
		ProviderSubID: "sub_cycle", PlanID: "team", Status: "active",
		CurrentPeriodStart: time.Now(), CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour),
	}
	if err := db.InsertSubscription(ctx, sub); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	n, err := svc.BillUsageCycle(ctx, start, start.AddDate(0, 1, 0))
	if err != nil {
		t.Fatalf("BillUsageCycle: %v", err)
	}
	if n != 1 {
		t.Fatalf("billed = %d, want 1", n)
	}
	invoices, total, err := db.ListInvoices(ctx, orgID, 1, 10)
	if err != nil || total != 1 || len(invoices) != 1 {
		t.Fatalf("invoices = %d total = %d err = %v", len(invoices), total, err)
	}
	if invoices[0].AmountCents != 4900 {
		t.Fatalf("amount = %d, want 4900 (team base, no usage)", invoices[0].AmountCents)
	}
}

// TrialConvergerFunc adapts a func to the TrialConverger interface.
type TrialConvergerFunc func(ctx context.Context) (int, error)

func (f TrialConvergerFunc) ExpireDueTrials(ctx context.Context) (int, error) {
	return f(ctx)
}
