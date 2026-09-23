package saml

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

func TestMetadataAndParse(t *testing.T) {
	c := Config{EntityID: "https://sp/trace", ACSURL: "https://sp/saml/acs", IDPSSOURL: "https://idp/sso"}
	md := c.MetadataXML()
	if !strings.Contains(md, "SPSSODescriptor") {
		t.Fatal(md)
	}
	u, err := c.RedirectURL("rs")
	if err != nil || !strings.Contains(u, "SAMLRequest=") {
		t.Fatal(u, err)
	}
	nb := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	noa := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	raw := `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
  <saml:Assertion><saml:Subject><saml:NameID>user@example.com</saml:NameID>
  <saml:SubjectConfirmation><saml:SubjectConfirmationData Recipient="https://sp/saml/acs" NotOnOrAfter="` + noa + `"/></saml:SubjectConfirmation></saml:Subject>
  <saml:Conditions NotBefore="` + nb + `" NotOnOrAfter="` + noa + `"><saml:AudienceRestriction><saml:Audience>https://sp/trace</saml:Audience></saml:AudienceRestriction></saml:Conditions>
  </saml:Assertion>
</samlp:Response>`
	a, err := ParseResponse(base64.StdEncoding.EncodeToString([]byte(raw)), VerifyOpts{
		AllowNoCert: true, EntityID: "https://sp/trace", ACSURL: "https://sp/saml/acs",
	})
	if err != nil || a.Email != "user@example.com" {
		t.Fatal(a, err)
	}
}

func TestParseRejectsWrongAudience(t *testing.T) {
	noa := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	raw := `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
  <saml:Assertion><saml:Subject><saml:NameID>evil@example.com</saml:NameID></saml:Subject>
  <saml:Conditions NotOnOrAfter="` + noa + `"><saml:AudienceRestriction><saml:Audience>https://evil/x</saml:Audience></saml:AudienceRestriction></saml:Conditions>
  </saml:Assertion>
</samlp:Response>`
	if _, err := ParseResponse(base64.StdEncoding.EncodeToString([]byte(raw)), VerifyOpts{AllowNoCert: true, EntityID: "https://sp/trace"}); err == nil {
		t.Fatal("expected audience mismatch")
	}
}

func TestParseRejectsExpired(t *testing.T) {
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	raw := `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
  <saml:Assertion><saml:Subject><saml:NameID>user@example.com</saml:NameID></saml:Subject>
  <saml:Conditions NotOnOrAfter="` + past + `"></saml:Conditions>
  </saml:Assertion>
</samlp:Response>`
	if _, err := ParseResponse(base64.StdEncoding.EncodeToString([]byte(raw)), VerifyOpts{AllowNoCert: true}); err == nil {
		t.Fatal("expected expiry rejection")
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := ParseResponse("!!!not-base64!!!", VerifyOpts{AllowNoCert: true}); err == nil {
		t.Fatal("expected b64 error")
	}
	if _, err := ParseResponse(base64.StdEncoding.EncodeToString([]byte("<xml><oops>")), VerifyOpts{AllowNoCert: true}); err == nil {
		t.Fatal("expected xml error")
	}
}

func TestParseRequiresCertByDefault(t *testing.T) {
	nb := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	noa := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	raw := `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
  <saml:Assertion><saml:Subject><saml:NameID>user@example.com</saml:NameID></saml:Subject>
  <saml:Conditions NotBefore="` + nb + `" NotOnOrAfter="` + noa + `"><saml:AudienceRestriction><saml:Audience>https://sp/trace</saml:Audience></saml:AudienceRestriction></saml:Conditions>
  </saml:Assertion>
</samlp:Response>`
	if _, err := ParseResponse(base64.StdEncoding.EncodeToString([]byte(raw)), VerifyOpts{EntityID: "https://sp/trace"}); err == nil {
		t.Fatal("expected certificate-required rejection")
	}
}

// TestSignedRoundTrip signs an assertion with a throwaway key and verifies
// it through the production path (no AllowNoCert).
func TestSignedRoundTrip(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-idp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	// cert is parsed inside verifySignature from certPEM; keep DER for the signer.
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))

	nb := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	noa := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	assertion := `<saml:Assertion xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="_abc123">
  <saml:Subject><saml:NameID>signed@example.com</saml:NameID>
  <saml:SubjectConfirmation><saml:SubjectConfirmationData Recipient="https://sp/saml/acs" NotOnOrAfter="` + noa + `"/></saml:SubjectConfirmation></saml:Subject>
  <saml:Conditions NotBefore="` + nb + `" NotOnOrAfter="` + noa + `"><saml:AudienceRestriction><saml:Audience>https://sp/trace</saml:Audience></saml:AudienceRestriction></saml:Conditions>
</saml:Assertion>`
	doc := etree.NewDocument()
	if err := doc.ReadFromString(assertion); err != nil {
		t.Fatal(err)
	}
	signer, err := dsig.NewSigningContext(key, [][]byte{der})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signer.SignEnveloped(doc.Root())
	if err != nil {
		t.Fatal(err)
	}
	respDoc := etree.NewDocument()
	respEl := respDoc.CreateElement("samlp:Response")
	respEl.CreateAttr("xmlns:samlp", "urn:oasis:names:tc:SAML:2.0:protocol")
	respEl.CreateAttr("Destination", "https://sp/saml/acs")
	respEl.AddChild(signed.Copy())
	resp, err := respDoc.WriteToString()
	if err != nil {
		t.Fatal(err)
	}

	a, err := ParseResponse(base64.StdEncoding.EncodeToString([]byte(resp)), VerifyOpts{
		CertPEM: certPEM, EntityID: "https://sp/trace", ACSURL: "https://sp/saml/acs",
	})
	if err != nil || a.Email != "signed@example.com" {
		t.Fatalf("signed round trip: %v %+v", err, a)
	}

	// Tamper after signing: verification must fail.
	tampered := strings.Replace(resp, "signed@example.com", "mallory@example.com", 1)
	if _, err := ParseResponse(base64.StdEncoding.EncodeToString([]byte(tampered)), VerifyOpts{
		CertPEM: certPEM, EntityID: "https://sp/trace", ACSURL: "https://sp/saml/acs",
	}); err == nil {
		t.Fatal("expected tampered assertion to fail verification")
	}
}

func signedToString(t *testing.T, el *etree.Element) string {
	t.Helper()
	doc := etree.NewDocument()
	doc.SetRoot(el.Copy())
	s, err := doc.WriteToString()
	if err != nil {
		t.Fatal(err)
	}
	return s
}
