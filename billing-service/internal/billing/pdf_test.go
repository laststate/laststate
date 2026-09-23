package billing

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/laststate/billing-service/internal/store"
)

func TestGeneratePDFValidSyntax(t *testing.T) {
	invID := uuid.New()
	invoice := &store.Invoice{
		ID:             invID,
		OrganizationID: uuid.New(),
		Provider:       "stripe",
		AmountCents:    4900,
		Currency:       "usd",
		Status:         "paid",
		IssuedAt:       time.Now(),
		DueAt:          timePtr(time.Now().Add(30 * 24 * time.Hour)),
	}

	pdfInv := &PDFInvoice{
		Invoice:   invoice,
		Currency:  "USD",
		TaxRate:   0.0825,
		TaxAmount: 404,
		Org: OrgInfo{
			Name:  "Acme Corp",
			Email: "billing@acme.com",
		},
		Plan: PlanInfo{
			Name:       "Team Plan",
			PriceCents: 4900,
			Currency:   "usd",
			Interval:   "monthly",
		},
		Items: []InvoiceLine{
			{
				Description: "Team Plan - monthly",
				Quantity:    1,
				UnitPrice:   4900,
				Amount:      4900,
			},
		},
	}

	pdfBytes, err := GeneratePDF(pdfInv)
	if err != nil {
		t.Fatalf("GeneratePDF failed: %v", err)
	}

	// 1. Must start with %PDF-
	if !bytes.HasPrefix(pdfBytes, []byte("%PDF-")) {
		t.Errorf("PDF does not start with %%PDF-")
	}

	// 2. Must end with %%EOF
	trimmed := bytes.TrimSpace(pdfBytes)
	if !bytes.HasSuffix(trimmed, []byte("%%EOF")) {
		t.Errorf("PDF does not end with %%%%EOF")
	}

	pdfStr := string(pdfBytes)

	// 3. Verify xref table and startxref
	startxrefIdx := strings.LastIndex(pdfStr, "startxref\n")
	if startxrefIdx == -1 {
		t.Fatalf("startxref not found")
	}
	afterStartxref := pdfStr[startxrefIdx+len("startxref\n"):]
	eofIdx := strings.Index(afterStartxref, "\n%%EOF")
	if eofIdx == -1 {
		t.Fatalf("startxref offset format error")
	}
	offsetStr := strings.TrimSpace(afterStartxref[:eofIdx])
	offset, err := strconv.Atoi(offsetStr)
	if err != nil {
		t.Fatalf("failed to parse startxref offset %q: %v", offsetStr, err)
	}

	// 4. Verify that the xref keyword actually begins at 'offset' in bytes
	if offset <= 0 || offset >= len(pdfBytes) {
		t.Fatalf("invalid xref offset %d (total len %d)", offset, len(pdfBytes))
	}
	if string(pdfBytes[offset:offset+4]) != "xref" {
		t.Errorf("expected 'xref' at offset %d, got %q", offset, string(pdfBytes[offset:offset+4]))
	}

	// 5. Verify objects exist
	for i := 1; i <= 8; i++ {
		objTag := fmt.Sprintf("%d 0 obj", i)
		if !strings.Contains(pdfStr, objTag) {
			t.Errorf("expected object %s in PDF", objTag)
		}
	}
}

func TestGenerateNFEdetailsNeverFabricates(t *testing.T) {
	// Fiscal safety: local generation was removed. Even for a qualifying
	// Brazilian invoice, GenerateNFEdetails must return nil — a Chave de
	// Acesso is only valid when issued by SEFAZ via a certified provider.
	org := &store.Organization{
		ID:      uuid.New(),
		Name:    "Empresa Brasileira LTDA",
		Email:   "contato@empresa.com.br",
		Address: "Av Paulista, 1000, Sao Paulo, SP, Brazil",
		TaxID:   "12.345.678/0001-95",
	}
	invoice := &store.Invoice{
		ID:          uuid.New(),
		AmountCents: 4900,
		Currency:    "BRL",
		Provider:    "mercado_pago",
		IssuedAt:    time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
	}

	if nfe := GenerateNFEdetails(org, invoice); nfe != nil {
		t.Fatalf("expected nil NFEDetails (no local fabrication), got %+v", nfe)
	}
	if nfe := GenerateNFEdetails(nil, nil); nfe != nil {
		t.Fatalf("expected nil NFEDetails for nil input, got %+v", nfe)
	}
}

func TestGenerateNFEdetailsDisabledNonBR(t *testing.T) {
	os.Unsetenv("BILLING_NFE_ENABLED")
	org := &store.Organization{
		ID:      uuid.New(),
		Name:    "US Tech Corp",
		Address: "123 Market St, San Francisco, CA, USA",
		TaxID:   "12-3456789",
	}
	invoice := &store.Invoice{
		ID:          uuid.New(),
		AmountCents: 9900,
		Currency:    "USD",
		Provider:    "stripe",
	}

	nfe := GenerateNFEdetails(org, invoice)
	if nfe != nil {
		t.Errorf("expected nil NFEDetails for non-BR invoice, got %+v", nfe)
	}
}

func TestGeneratePDFWithNFERendering(t *testing.T) {
	org := &store.Organization{
		ID:      uuid.New(),
		Name:    "Tech Brasil S.A.",
		Email:   "financeiro@techbrasil.com.br",
		Address: "Rua das Flores, 50, Curitiba, PR, Brazil",
		TaxID:   "98.765.432/0001-10",
	}
	invoice := &store.Invoice{
		ID:          uuid.New(),
		AmountCents: 15000,
		Currency:    "BRL",
		Provider:    "mercado_pago",
		Status:      "paid",
		IssuedAt:    time.Now(),
	}

	// NF-e data arrives populated by a certified fiscal provider —
	// never generated locally (see TestGenerateNFEdetailsNeverFabricates).
	nfe := &NFEDetails{
		NFENumber:    "NFE-001-000000001",
		ChaveAcesso:  "41260898765432000110550010000000011234567890",
		ISS:          "5.00%",
		Contribuinte: "Tech Brasil S.A. (98.765.432/0001-10)",
		UF:           "PR",
		Regime:       "Simples Nacional",
	}

	pdfInv := &PDFInvoice{
		Invoice:   invoice,
		Currency:  "BRL",
		TaxRate:   0.05,
		TaxAmount: 750,
		Org: OrgInfo{
			Name:  org.Name,
			Email: org.Email,
		},
		Plan: PlanInfo{
			Name:       "Team Plan",
			PriceCents: 15000,
			Currency:   "BRL",
		},
		NFEnfe: nfe,
	}

	pdfBytes, err := GeneratePDF(pdfInv)
	if err != nil {
		t.Fatalf("GeneratePDF with NF-e failed: %v", err)
	}

	pdfStr := string(pdfBytes)
	if !strings.Contains(pdfStr, "Nota Fiscal Eletronica") {
		t.Errorf("PDF does not contain NF-e section header")
	}
	if !strings.Contains(pdfStr, nfe.ChaveAcesso) {
		t.Errorf("PDF does not contain NF-e ChaveAcesso %s", nfe.ChaveAcesso)
	}
}
