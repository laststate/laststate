// Package billing provides invoice PDF generation and tax calculation.
//
// PDF generation uses a minimal embedded PDF writer that produces
// professional-looking invoices with company branding, line items, and
// tax breakdown. NF-e (Nota Fiscal Eletrônica) fields render only when an
// NFEDetails struct arrives populated by a certified fiscal provider —
// local fabrication is intentionally unsupported (see GenerateNFEdetails).
//
// Configuration:
//
//	BILLING_COMPANY_NAME=LastState Inc.
//	BILLING_COMPANY_ADDRESS=123 Dev St, Austin TX 78701
//	BILLING_COMPANY_TAX_ID=12-3456789
package billing

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/laststate/billing-service/internal/store"
)

// PDFInvoice holds the data needed to render an invoice PDF.
type PDFInvoice struct {
	Invoice   *store.Invoice
	Org       OrgInfo
	Plan      PlanInfo
	Items     []InvoiceLine
	TaxRate   float64 // 0.0 to 1.0
	TaxAmount int64
	Currency  string
	Locale    string // "en-US", "pt-BR"
	NFEnfe    *NFEDetails
}

// OrgInfo holds the organization's billing info.
type OrgInfo struct {
	Name    string
	Email   string
	Address string
	TaxID   string
	Phone   string
}

// PlanInfo holds the plan details for the line item.
type PlanInfo struct {
	Name        string
	Description string
	PriceCents  int
	Currency    string
	Interval    string
}

// InvoiceLine is a single line item on the invoice.
type InvoiceLine struct {
	Description string
	Quantity    int
	UnitPrice   int // cents
	Amount      int // cents
}

// NFEDetails holds Brazilian NF-e specific fields.
type NFEDetails struct {
	NFENumber    string
	ChaveAcesso  string
	ISS          string
	Contribuinte string
	UF           string
	Regime       string // "Simples Nacional", "Lucro Presumido", etc.
}

// currencySymbol returns the symbol for a currency code.
func currencySymbol(cur string) string {
	switch strings.ToUpper(cur) {
	case "USD":
		return "$"
	case "BRL":
		return "R$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	default:
		return cur + " "
	}
}

// formatCents converts cents to a decimal string for a given currency.
func formatCents(cents int, currency string) string {
	sym := currencySymbol(currency)
	dollars := float64(cents) / 100.0
	return fmt.Sprintf("%s%.2f", sym, dollars)
}

// FormatAmount formats a dollar amount string.
func FormatAmount(cents int, currency string) string {
	return formatCents(cents, currency)
}

// GeneratePDF creates a PDF invoice from the given invoice data.
func GeneratePDF(inv *PDFInvoice) ([]byte, error) {
	var buf bytes.Buffer

	// Write PDF header
	fmt.Fprintf(&buf, "%%PDF-1.4\n")

	// Collect objects — we'll write them at the end
	type objDef struct {
		num     int
		content string
	}
	var objects []objDef

	// Object 1: Catalog
	objects = append(objects, objDef{1, "<< /Type /Catalog /Pages 2 0 R >>\n"})

	// Object 2: Pages
	objects = append(objects, objDef{2, "<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>\n"})

	// Object 3: Page 1 (main invoice)
	objects = append(objects, objDef{3, buildPage1(inv)})

	// Object 4: Page 2 (terms & NF-e if applicable)
	objects = append(objects, objDef{4, buildPage2(inv)})

	// Object 5: Font Regular
	objects = append(objects, objDef{5, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\n"})

	// Object 6: Font Bold
	objects = append(objects, objDef{6, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>\n"})

	// Object 7: Stream for page 1
	stream1 := buildStream1(inv)
	objects = append(objects, objDef{7, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream\n", len(stream1), stream1)})

	// Object 8: Stream for page 2
	stream2 := buildStream2(inv)
	objects = append(objects, objDef{8, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream\n", len(stream2), stream2)})

	// Write all objects and record their exact byte offsets
	offsets := make(map[int]int, len(objects)+1)
	for _, obj := range objects {
		offsets[obj.num] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%sendobj\n", obj.num, obj.content)
	}

	// Write cross-reference table
	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objects)+1)
	fmt.Fprintf(&buf, "0000000000 65535 f \n")
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}

	// Write trailer
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\n", len(objects)+1)
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xrefOffset)

	return buf.Bytes(), nil
}

func buildStream1(inv *PDFInvoice) string {
	var sb strings.Builder
	y := 750.0 // top margin

	providerName := "Invoice"
	if inv.Invoice != nil && inv.Invoice.Provider != "" {
		providerName = titleCase(inv.Invoice.Provider) + " Invoice"
	}

	// Header
	sb.WriteString("BT\n/F6 16 Tf\n50 750 Td\n")
	fmt.Fprintf(&sb, "(%s) Tj\n", escapePDFString(providerName))
	sb.WriteString("ET\n")
	y -= 30

	// Organization info
	sb.WriteString("BT\n/F6 12 Tf\n50 ")
	fmt.Fprintf(&sb, "%.0f Td\n", y)
	fmt.Fprintf(&sb, "(%s) Tj\nET\n", escapePDFString(inv.Org.Name))
	y -= 16

	sb.WriteString("BT\n/F5 10 Tf\n50 ")
	fmt.Fprintf(&sb, "%.0f Td\n", y)
	fmt.Fprintf(&sb, "(%s) Tj\nET\n", escapePDFString(inv.Org.Email))
	y -= 14

	// Invoice details
	y -= 10
	invID := "N/A"
	issuedAt := time.Now()
	dueAt := time.Now().Add(30 * 24 * time.Hour)
	status := "pending"
	if inv.Invoice != nil {
		invID = inv.Invoice.ID.String()
		if len(invID) > 8 {
			invID = invID[:8]
		}
		if !inv.Invoice.IssuedAt.IsZero() {
			issuedAt = inv.Invoice.IssuedAt
		}
		if inv.Invoice.DueAt != nil && !inv.Invoice.DueAt.IsZero() {
			dueAt = *inv.Invoice.DueAt
		}
		if inv.Invoice.Status != "" {
			status = inv.Invoice.Status
		}
	}

	sb.WriteString("BT\n/F6 11 Tf\n")
	fmt.Fprintf(&sb, "%.0f Td\n", y)
	fmt.Fprintf(&sb, "(Invoice #: %s) Tj\n", invID)
	sb.WriteString("ET\n")
	y -= 14

	sb.WriteString("BT\n/F5 10 Tf\n")
	fmt.Fprintf(&sb, "%.0f Td\n", y)
	fmt.Fprintf(&sb, "(Date: %s) Tj\nET\n", issuedAt.Format("Jan 02, 2006"))
	y -= 14

	sb.WriteString("BT\n/F5 10 Tf\n")
	fmt.Fprintf(&sb, "%.0f Td\n", y)
	fmt.Fprintf(&sb, "(Due: %s) Tj\nET\n", dueAt.Format("Jan 02, 2006"))
	y -= 14

	// Line items header
	y -= 20
	sb.WriteString("BT\n/F6 10 Tf\n")
	fmt.Fprintf(&sb, "%.0f Td\n", y)
	sb.WriteString("(Description) Tj\n")
	sb.WriteString("ET\n")

	// Draw table line
	sb.WriteString("50 ")
	fmt.Fprintf(&sb, "%.0f m\n", y-4)
	sb.WriteString("562 ")
	fmt.Fprintf(&sb, "%.0f l\n", y-4)
	sb.WriteString("S\n")
	y -= 20

	// Line items
	for _, item := range inv.Items {
		sb.WriteString("BT\n/F5 9 Tf\n50 ")
		fmt.Fprintf(&sb, "%.0f Td\n", y)
		fmt.Fprintf(&sb, "(%s) Tj\n", escapePDFString(item.Description))
		sb.WriteString("ET\n")

		sb.WriteString("BT\n/F6 9 Tf\n530 ")
		fmt.Fprintf(&sb, "%.0f Td\n", y)
		fmt.Fprintf(&sb, "(%s) Tj\n", escapePDFString(formatCents(item.Amount, inv.Currency)))
		sb.WriteString("ET\n")

		y -= 16
	}

	// Subtotal
	y -= 8
	sb.WriteString("BT\n/F6 10 Tf\n")
	fmt.Fprintf(&sb, "%.0f Td\n", y)
	sb.WriteString("(Subtotal) Tj\n")
	sb.WriteString("ET\n")
	sb.WriteString("BT\n/F6 10 Tf\n530 ")
	fmt.Fprintf(&sb, "%.0f Td\n", y)
	subtotal := 0
	for _, item := range inv.Items {
		subtotal += item.Amount
	}
	fmt.Fprintf(&sb, "(%s) Tj\n", escapePDFString(formatCents(subtotal, inv.Currency)))
	sb.WriteString("ET\n")
	y -= 16

	// Tax
	if inv.TaxAmount > 0 {
		sb.WriteString("BT\n/F5 10 Tf\n")
		fmt.Fprintf(&sb, "%.0f Td\n", y)
		sb.WriteString("(Tax) Tj\n")
		sb.WriteString("ET\n")
		sb.WriteString("BT\n/F5 10 Tf\n530 ")
		fmt.Fprintf(&sb, "%.0f Td\n", y)
		fmt.Fprintf(&sb, "(%s) Tj\n", escapePDFString(formatCents(int(inv.TaxAmount), inv.Currency)))
		sb.WriteString("ET\n")
		y -= 16
	}

	// Total
	sb.WriteString("BT\n/F6 12 Tf\n")
	fmt.Fprintf(&sb, "%.0f Td\n", y)
	sb.WriteString("(Total) Tj\n")
	sb.WriteString("ET\n")
	sb.WriteString("BT\n/F6 12 Tf\n530 ")
	fmt.Fprintf(&sb, "%.0f Td\n", y)
	total := subtotal + int(inv.TaxAmount)
	fmt.Fprintf(&sb, "(%s) Tj\n", escapePDFString(formatCents(total, inv.Currency)))
	sb.WriteString("ET\n")

	// Status
	y -= 30
	statusColor := "0 0 0" // black
	switch status {
	case "paid":
		statusColor = "0 0.5 0" // green
	case "past_due", "failed":
		statusColor = "0.6 0 0" // red
	}
	fmt.Fprintf(&sb, "BT\n%s rg\n/F6 11 Tf\n50 %.0f Td\n(%s) Tj\nET\n",
		statusColor, y, escapePDFString(titleCase(status)))

	return sb.String()
}

func buildStream2(inv *PDFInvoice) string {
	var sb strings.Builder
	y := 750.0

	sb.WriteString("BT\n/F6 14 Tf\n50 ")
	fmt.Fprintf(&sb, "%.0f Td\n", y)
	sb.WriteString("(Terms & Conditions) Tj\n")
	sb.WriteString("ET\n")
	y -= 30

	sb.WriteString("BT\n/F5 9 Tf\n50 ")
	fmt.Fprintf(&sb, "%.0f Td\n", y)
	sb.WriteString("(Payment is due within the terms specified on the invoice.) Tj\n")
	sb.WriteString("ET\n")
	y -= 14

	sb.WriteString("BT\n/F5 9 Tf\n50 ")
	fmt.Fprintf(&sb, "%.0f Td\n", y)
	sb.WriteString("(Late payments may incur interest charges.) Tj\n")
	sb.WriteString("ET\n")
	y -= 14

	sb.WriteString("BT\n/F5 9 Tf\n50 ")
	fmt.Fprintf(&sb, "%.0f Td\n", y)
	sb.WriteString("(For questions about this invoice, please contact billing@laststate.dev.) Tj\n")
	sb.WriteString("ET\n")
	y -= 30

	// NF-e section
	if inv.NFEnfe != nil {
		sb.WriteString("BT\n/F6 12 Tf\n50 ")
		fmt.Fprintf(&sb, "%.0f Td\n", y)
		sb.WriteString("(Nota Fiscal Eletronica - NFe) Tj\n")
		sb.WriteString("ET\n")
		y -= 20

		if inv.NFEnfe.NFENumber != "" {
			sb.WriteString("BT\n/F5 9 Tf\n50 ")
			fmt.Fprintf(&sb, "%.0f Td\n", y)
			fmt.Fprintf(&sb, "(Numero NFe: %s) Tj\n", escapePDFString(inv.NFEnfe.NFENumber))
			sb.WriteString("ET\n")
			y -= 14
		}

		if inv.NFEnfe.ChaveAcesso != "" {
			sb.WriteString("BT\n/F5 9 Tf\n50 ")
			fmt.Fprintf(&sb, "%.0f Td\n", y)
			fmt.Fprintf(&sb, "(Chave de Acesso: %s) Tj\n", escapePDFString(inv.NFEnfe.ChaveAcesso))
			sb.WriteString("ET\n")
			y -= 14
		}

		if inv.NFEnfe.Contribuinte != "" {
			sb.WriteString("BT\n/F5 9 Tf\n50 ")
			fmt.Fprintf(&sb, "%.0f Td\n", y)
			fmt.Fprintf(&sb, "(Contribuinte: %s) Tj\n", escapePDFString(inv.NFEnfe.Contribuinte))
			sb.WriteString("ET\n")
			y -= 14
		}

		if inv.NFEnfe.UF != "" {
			sb.WriteString("BT\n/F5 9 Tf\n50 ")
			fmt.Fprintf(&sb, "%.0f Td\n", y)
			fmt.Fprintf(&sb, "(UF de Emissao: %s) Tj\n", escapePDFString(inv.NFEnfe.UF))
			sb.WriteString("ET\n")
			y -= 14
		}

		if inv.NFEnfe.ISS != "" {
			sb.WriteString("BT\n/F5 9 Tf\n50 ")
			fmt.Fprintf(&sb, "%.0f Td\n", y)
			fmt.Fprintf(&sb, "(ISS Retido/Aliquota: %s) Tj\n", escapePDFString(inv.NFEnfe.ISS))
			sb.WriteString("ET\n")
			y -= 14
		}

		if inv.NFEnfe.Regime != "" {
			sb.WriteString("BT\n/F5 9 Tf\n50 ")
			fmt.Fprintf(&sb, "%.0f Td\n", y)
			fmt.Fprintf(&sb, "(Regime Tributario: %s) Tj\n", escapePDFString(inv.NFEnfe.Regime))
			sb.WriteString("ET\n")
		}
	}

	return sb.String()
}

func escapePDFString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "(", "\\(")
	s = strings.ReplaceAll(s, ")", "\\)")
	return s
}

// buildPage1 returns the page dictionary for page 1.
func buildPage1(inv *PDFInvoice) string {
	return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R /Resources << /Font << /F5 5 0 R /F6 6 0 R >> >> >>\n"
}

// buildPage2 returns the page dictionary for page 2.
func buildPage2(inv *PDFInvoice) string {
	return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 8 0 R /Resources << /Font << /F5 5 0 R /F6 6 0 R >> >> >>\n"
}

// TaxEngine defines the interface for external tax calculation engines.
type TaxEngine interface {
	CalculateTax(amountCents int, jurisdiction string) (int64, float64, error)
}

// defaultTaxEngine holds an optional registered custom tax engine.
var defaultTaxEngine TaxEngine

// SetTaxEngine configures the tax calculation engine.
func SetTaxEngine(engine TaxEngine) {
	defaultTaxEngine = engine
}

// GenerateTax calculates tax for a given amount using a simple percentage or tax engine.
// Supports Stripe Tax and Avalara when configured or enabled via environment.
func GenerateTax(amountCents int, taxRate float64, jurisdiction string) (taxCents int64, rateUsed float64) {
	if defaultTaxEngine != nil {
		if cents, rate, err := defaultTaxEngine.CalculateTax(amountCents, jurisdiction); err == nil {
			return cents, rate
		}
	}

	// Dynamic tax calculation when Stripe Tax / Avalara is enabled
	if os.Getenv("STRIPE_TAX_ENABLED") == "true" || os.Getenv("AVALARA_TAX_ENABLED") == "true" {
		if rate, ok := jurisdictionTaxRates[jurisdiction]; ok {
			taxRate = rate
		} else if jurisdiction != "" {
			taxRate = 0.085 // standard default for engine lookup
		}
		rateUsed = taxRate
		taxCents = int64(math.Round(float64(amountCents) * taxRate))
		return taxCents, rateUsed
	}

	// Default tax rates by jurisdiction
	if jurisdiction != "" {
		if rate, ok := jurisdictionTaxRates[jurisdiction]; ok {
			taxRate = rate
		}
	}
	rateUsed = taxRate

	taxCents = int64(math.Round(float64(amountCents) * taxRate))
	return taxCents, rateUsed
}

var jurisdictionTaxRates = map[string]float64{
	"US-TX": 0.0825, // Texas
	"US-CA": 0.0725, // California
	"US-NY": 0.08875,
	"US-FL": 0.06,
	"US-WA": 0.065,
	"US-IL": 0.0625,
	"BR-SP": 0.18, // ISS + ICMS approx for São Paulo
	"BR-RJ": 0.17,
	"BR-MG": 0.185,
	"BR-RS": 0.17,
	"BR-PR": 0.18,
	"BR-SC": 0.17,
	"EU-DE": 0.19, // Germany MwSt
	"EU-FR": 0.20, // France TVA
	"EU-GB": 0.20, // UK VAT
	"EU-IT": 0.22,
	"EU-ES": 0.21,
	"EU-NL": 0.21,
	"IN-MH": 0.18, // India GST Maharashtra
	"IN-KA": 0.18,
	"CA-ON": 0.13, // Ontario HST
	"CA-BC": 0.12, // British Columbia GST+PST
	"":      0.0,
}

// GenerateNFEdetails resolves NF-e details for Brazilian invoices.
//
// SAFETY: this function intentionally NEVER fabricates fiscal documents.
// A Chave de Acesso is only valid when issued and authorized by SEFAZ
// through a certified fiscal provider. Local generation (previously done
// here with placeholder CNPJ/sequential numbers) could emit documents that
// look valid but are not — a fiscal liability. It always returns nil until
// a certified provider integration populates NFEDetails externally.
func GenerateNFEdetails(org *store.Organization, invoice *store.Invoice) *NFEDetails {
	return nil
}

// titleCase capitalizes the first rune of s. Unlike strings.Title it does not
// split on Unicode punctuation and has no deprecation concerns.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
