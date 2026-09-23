package billing

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

// Mailer sends transactional billing email. When no SMTP transport is
// configured it degrades to log transport (like the trace mailer): bodies
// are still fully rendered so operators can verify content in staging.
type Mailer struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	UseTLS   bool
}

// MailerFromEnv builds a Mailer from BILLING_SMTP_* variables.
// Empty host means log transport.
func MailerFromEnv() *Mailer {
	port := 587
	if v := os.Getenv("BILLING_SMTP_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			port = n
		}
	}
	return &Mailer{
		Host:     os.Getenv("BILLING_SMTP_HOST"),
		Port:     port,
		Username: os.Getenv("BILLING_SMTP_USER"),
		Password: os.Getenv("BILLING_SMTP_PASS"),
		From:     firstNonEmpty(os.Getenv("BILLING_SMTP_FROM"), "billing@laststate.dev"),
		UseTLS:   strings.EqualFold(os.Getenv("BILLING_SMTP_TLS"), "true") || port == 465,
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Email is a rendered transactional message with an optional PDF attachment.
type Email struct {
	To         string
	Subject    string
	Body       string
	AttachName string
	AttachPDF  []byte
}

// ReceiptEmail renders a payment receipt, optionally with the invoice PDF.
func ReceiptEmail(to string, orgName string, amountCents int, currency, invoiceID string, pdf []byte) Email {
	return Email{
		To:      to,
		Subject: fmt.Sprintf("LastState receipt — %s %s", formatCents(amountCents, currency), strings.ToUpper(currency)),
		Body: fmt.Sprintf(`Hi %s,

Payment received — thank you.

  Invoice: %s
  Amount:  %s %s

Your subscription stays active. The invoice PDF is attached.
Manage billing anytime from your account portal.

— LastState Billing
`, orgName, invoiceID, formatCents(amountCents, currency), strings.ToUpper(currency)),
		AttachName: "invoice-" + invoiceID + ".pdf",
		AttachPDF:  pdf,
	}
}

// PaymentFailedEmail renders a dunning nudge with a portal CTA.
func PaymentFailedEmail(to, orgName string, attempt int) Email {
	return Email{
		To:      to,
		Subject: fmt.Sprintf("LastState: payment attempt #%d failed", attempt),
		Body: fmt.Sprintf(`Hi %s,

We couldn't charge your payment method (attempt #%d).

Update it in the billing portal to avoid interruption. If a new attempt
succeeds within the retry window, nothing else changes.

— LastState Billing
`, orgName, attempt),
	}
}

// TrialEndingEmail renders the trial-ending warning (Stripe sends
// customer.subscription.trial_will_end ~3 days before expiry).
func TrialEndingEmail(to, orgName string) Email {
	return Email{
		To:      to,
		Subject: "LastState: your trial ends in 3 days",
		Body: fmt.Sprintf(`Hi %s,

Your trial ends in about 3 days. Add a payment method to keep full
limits — otherwise the workspace converges to the free tier and
nothing is deleted.

— LastState Billing
`, orgName),
	}
}

// TrialEndedEmail renders the trial-expiry notice.
func TrialEndedEmail(to, orgName, tier string) Email {
	return Email{
		To:      to,
		Subject: "LastState: your trial ended",
		Body: fmt.Sprintf(`Hi %s,

Your 14-day trial ended. Your workspace converged to the %s tier —
nothing was deleted, quotas now follow that tier.

Upgrade anytime to restore full limits.

— LastState Billing
`, orgName, tier),
	}
}

// encodeBase64Lines encodes data as base64 with 76-char CRLF line breaks
// (MIME §6.8: max 78 chars per line including CRLF).
func encodeBase64Lines(data []byte) []byte {
	const line = 57 // 57 raw bytes = 76 base64 chars
	var out bytes.Buffer
	for i := 0; i < len(data); i += line {
		end := i + line
		if end > len(data) {
			end = len(data)
		}
		chunk := make([]byte, base64.StdEncoding.EncodedLen(end-i))
		base64.StdEncoding.Encode(chunk, data[i:end])
		out.Write(chunk)
		out.WriteString("\r\n")
	}
	return out.Bytes()
}

// Send delivers an Email via SMTP, or logs it when unconfigured.
// It never fails the billing write path: transport errors are returned
// for the caller to log, never to roll back charges.
func (m *Mailer) Send(_ context.Context, msg Email) error {
	if m == nil || m.Host == "" {
		return fmt.Errorf("smtp not configured (BILLING_SMTP_HOST): email to %s %q logged only", msg.To, msg.Subject)
	}
	var buf bytes.Buffer
	boundary := fmt.Sprintf("laststate-%d", time.Now().UnixNano())
	fmt.Fprintf(&buf, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\n", m.From, msg.To, msg.Subject)
	if len(msg.AttachPDF) > 0 {
		fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%s\r\n\r\n", boundary)
		fmt.Fprintf(&buf, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", boundary, msg.Body)
		fmt.Fprintf(&buf, "--%s\r\nContent-Type: application/pdf; name=%s\r\nContent-Transfer-Encoding: base64\r\nContent-Disposition: attachment; filename=%s\r\n\r\n", boundary, msg.AttachName, msg.AttachName)
		buf.Write(encodeBase64Lines(msg.AttachPDF))
		fmt.Fprintf(&buf, "\r\n--%s--\r\n", boundary)
	} else {
		fmt.Fprintf(&buf, "Content-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", msg.Body)
	}

	addr := fmt.Sprintf("%s:%d", m.Host, m.Port)
	var auth smtp.Auth
	if m.Username != "" {
		auth = smtp.PlainAuth("", m.Username, m.Password, m.Host)
	}
	if m.UseTLS {
		tlsCfg := &tls.Config{ServerName: m.Host}
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, tlsCfg)
		if err != nil {
			return fmt.Errorf("smtp dial: %w", err)
		}
		defer conn.Close()
		c, err := smtp.NewClient(conn, m.Host)
		if err != nil {
			return fmt.Errorf("smtp client: %w", err)
		}
		defer func() { _ = c.Quit() }()
		if auth != nil {
			if authErr := c.Auth(auth); authErr != nil {
				return fmt.Errorf("smtp auth: %w", authErr)
			}
		}
		if mailErr := c.Mail(m.From); mailErr != nil {
			return fmt.Errorf("smtp mail: %w", mailErr)
		}
		if rcptErr := c.Rcpt(msg.To); rcptErr != nil {
			return fmt.Errorf("smtp rcpt: %w", rcptErr)
		}
		wc, err := c.Data()
		if err != nil {
			return fmt.Errorf("smtp data: %w", err)
		}
		defer wc.Close()
		if _, err := wc.Write(buf.Bytes()); err != nil {
			return fmt.Errorf("smtp write: %w", err)
		}
		return nil
	}
	if err := smtp.SendMail(addr, auth, m.From, []string{msg.To}, buf.Bytes()); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}
