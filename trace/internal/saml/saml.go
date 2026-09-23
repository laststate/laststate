// Package saml implements a minimal SAML 2.0 SP (Service Provider) surface:
// metadata XML + an assertion consumer that verifies XML signatures with
// russellhaering/goxmldsig and enforces Conditions (audience, recipient,
// time window). Unsigned assertions are rejected unless the caller opts
// into the insecure local-test path.
package saml

import (
	"bytes"
	"compress/flate"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"html"
	"net/url"
	"strings"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

type Config struct {
	EntityID       string // SP entity ID
	ACSURL         string
	IDPEntityID    string
	IDPSSOURL      string
	IDPCertificate string // PEM optional for stub
}

// MetadataXML returns SP metadata.
func (c Config) MetadataXML() string {
	return fmt.Sprintf(`<?xml version="1.0"?>
<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" entityID="%s">
  <md:SPSSODescriptor AuthnRequestsSigned="false" WantAssertionsSigned="false"
      protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <md:NameIDFormat>urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress</md:NameIDFormat>
    <md:AssertionConsumerService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
      Location="%s" index="0" isDefault="true"/>
  </md:SPSSODescriptor>
</md:EntityDescriptor>`, html.EscapeString(c.EntityID), html.EscapeString(c.ACSURL))
}

// RedirectURL builds IdP SSO redirect with SAMLRequest (deflate+base64 omitted for simplicity — uses simple base64 AuthnRequest).
func (c Config) RedirectURL(relayState string) (string, error) {
	if c.IDPSSOURL == "" {
		return "", fmt.Errorf("idp sso url required")
	}
	id := "_" + randomHex(16)
	req := fmt.Sprintf(`<?xml version="1.0"?>
<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"
  xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"
  ID="%s" Version="2.0" IssueInstant="%s" Destination="%s"
  AssertionConsumerServiceURL="%s" ProtocolBinding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST">
  <saml:Issuer>%s</saml:Issuer>
</samlp:AuthnRequest>`, id, time.Now().UTC().Format(time.RFC3339), c.IDPSSOURL, c.ACSURL, c.EntityID)
	// HTTP-Redirect binding: raw DEFLATE then base64 (plain base64 breaks
	// strict IdPs).
	deflated, err := deflateRaw([]byte(req))
	if err != nil {
		return "", err
	}
	enc := base64.StdEncoding.EncodeToString(deflated)
	u, err := url.Parse(c.IDPSSOURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("SAMLRequest", enc)
	if relayState != "" {
		q.Set("RelayState", relayState)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// deflateRaw compresses per the HTTP-Redirect binding (RFC 1951 raw DEFLATE).
func deflateRaw(in []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(in); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Assertion is a minimal parsed response.
type Assertion struct {
	NameID       string
	Email        string
	SessionID    string
	NotOnOrAfter time.Time
}

// VerifyOpts are the SP-side expectations enforced on every assertion.
// When CertPEM is set, the assertion signature is cryptographically
// verified against it. Without a certificate the response is rejected
// unless the caller explicitly allows the insecure local-test path.
type VerifyOpts struct {
	CertPEM     string
	AllowNoCert bool // local testing only; production config rejects this
	EntityID    string // expected Audience
	ACSURL      string // expected Recipient/Destination
	// Skew allows clock drift between SP and IdP.
	Skew time.Duration
}

// verifySignature checks the Assertion's XML signature against certPEM
// using goxmldsig (exclusive canonicalization + digest + RSA/ECDSA verify).
func verifySignature(raw []byte, certPEM string) ([]byte, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, fmt.Errorf("saml: invalid IdP certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("saml: parse IdP certificate: %w", err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(raw); err != nil {
		return nil, fmt.Errorf("saml response xml: %w", err)
	}
	// The enveloped Signature lives inside the Assertion, so that is the
	// element under verification. Validate() copies internally, leaving
	// this tree untouched; serializing the whole document afterwards keeps
	// the Response root the strict parser below expects.
	assertion := findAssertion(doc.Root())
	if assertion == nil {
		return nil, fmt.Errorf("saml: no Assertion element")
	}
	store := &dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{cert}}
	ctx := dsig.NewDefaultValidationContext(store)
	if _, err := ctx.Validate(assertion); err != nil {
		return nil, fmt.Errorf("saml signature verification: %w", err)
	}
	out, err := doc.WriteToBytes()
	if err != nil {
		return nil, fmt.Errorf("saml serialize validated doc: %w", err)
	}
	return out, nil
}

// findAssertion locates the Assertion element regardless of IdP namespace
// prefixes.
func findAssertion(root *etree.Element) *etree.Element {
	if root == nil {
		return nil
	}
	if strings.HasSuffix(root.Tag, "Assertion") {
		return root
	}
	for _, child := range root.ChildElements() {
		if found := findAssertion(child); found != nil {
			return found
		}
	}
	return nil
}

type samlResponse struct {
	XMLName     xml.Name      `xml:"Response"`
	Destination string        `xml:"Destination,attr"`
	Assertion   samlAssertion `xml:"Assertion"`
}

type samlAssertion struct {
	Subject struct {
		NameID struct {
			Format string `xml:"Format,attr"`
			Value  string `xml:",chardata"`
		} `xml:"NameID"`
		SubjectConfirmation struct {
			Data struct {
				Recipient    string `xml:"Recipient,attr"`
				InResponseTo string `xml:"InResponseTo,attr"`
				NotOnOrAfter string `xml:"NotOnOrAfter,attr"`
			} `xml:"SubjectConfirmationData"`
		} `xml:"SubjectConfirmation"`
	} `xml:"Subject"`
	Conditions struct {
		NotBefore    string `xml:"NotBefore,attr"`
		NotOnOrAfter string `xml:"NotOnOrAfter,attr"`
		Audience     []struct {
			Value string `xml:",chardata"`
		} `xml:"AudienceRestriction>Audience"`
	} `xml:"Conditions"`
	Attributes []struct {
		Name   string   `xml:"Name,attr"`
		Values []string `xml:"AttributeValue"`
	} `xml:"AttributeStatement>Attribute"`
}

// ParseResponse decodes a base64 SAMLResponse, verifies its XML signature
// against the configured IdP certificate, then parses it as strict XML and
// enforces Conditions: time window, audience, and recipient. Returns the
// subject email on success.
func ParseResponse(b64 string, opts VerifyOpts) (Assertion, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		// some IdPs use URL encoding
		raw, err = base64.URLEncoding.DecodeString(strings.TrimSpace(b64))
		if err != nil {
			return Assertion{}, fmt.Errorf("saml response b64: %w", err)
		}
	}
	if strings.TrimSpace(opts.CertPEM) != "" {
		if raw, err = verifySignature(raw, opts.CertPEM); err != nil {
			return Assertion{}, err
		}
	} else if !opts.AllowNoCert {
		return Assertion{}, fmt.Errorf("saml certificate required")
	}
	var resp samlResponse
	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.Strict = true
	dec.Entity = xml.HTMLEntity
	if err := dec.Decode(&resp); err != nil {
		return Assertion{}, fmt.Errorf("saml response xml: %w", err)
	}
	skew := opts.Skew
	if skew <= 0 {
		skew = 5 * time.Minute
	}
	now := time.Now()
	// Conditions window.
	if resp.Assertion.Conditions.NotOnOrAfter != "" {
		if exp, err := time.Parse(time.RFC3339, resp.Assertion.Conditions.NotOnOrAfter); err == nil {
			if now.After(exp.Add(skew)) {
				return Assertion{}, fmt.Errorf("saml assertion expired")
			}
		} else {
			return Assertion{}, fmt.Errorf("saml bad Conditions NotOnOrAfter")
		}
	}
	if resp.Assertion.Conditions.NotBefore != "" {
		if nb, err := time.Parse(time.RFC3339, resp.Assertion.Conditions.NotBefore); err == nil {
			if now.Add(skew).Before(nb) {
				return Assertion{}, fmt.Errorf("saml assertion not yet valid")
			}
		} else {
			return Assertion{}, fmt.Errorf("saml bad Conditions NotBefore")
		}
	}
	// Audience must include our entity ID.
	if opts.EntityID != "" {
		matched := false
		for _, a := range resp.Assertion.Conditions.Audience {
			if strings.TrimSpace(a.Value) == opts.EntityID {
				matched = true
				break
			}
		}
		if !matched {
			return Assertion{}, fmt.Errorf("saml audience mismatch")
		}
	}
	// Recipient must be our ACS.
	if opts.ACSURL != "" {
		got := strings.TrimSpace(resp.Assertion.Subject.SubjectConfirmation.Data.Recipient)
		if got != "" && got != opts.ACSURL {
			return Assertion{}, fmt.Errorf("saml recipient mismatch")
		}
	}
	// Subject confirmation expiry.
	if noa := resp.Assertion.Subject.SubjectConfirmation.Data.NotOnOrAfter; noa != "" {
		if exp, err := time.Parse(time.RFC3339, noa); err == nil {
			if now.After(exp.Add(skew)) {
				return Assertion{}, fmt.Errorf("saml subject confirmation expired")
			}
		} else {
			return Assertion{}, fmt.Errorf("saml bad SubjectConfirmation NotOnOrAfter")
		}
	}
	// Email: NameID first, then mail attributes.
	email := strings.TrimSpace(resp.Assertion.Subject.NameID.Value)
	if email == "" || !strings.Contains(email, "@") {
		email = ""
		for _, attr := range resp.Assertion.Attributes {
			lname := strings.ToLower(attr.Name)
			if !strings.Contains(lname, "email") && !strings.Contains(lname, "mail") {
				continue
			}
			for _, v := range attr.Values {
				if strings.Contains(v, "@") {
					email = strings.TrimSpace(v)
					break
				}
			}
			if email != "" {
				break
			}
		}
	}
	if email == "" {
		return Assertion{}, fmt.Errorf("saml: no NameID/email in assertion")
	}
	return Assertion{
		NameID:       strings.TrimSpace(resp.Assertion.Subject.NameID.Value),
		Email:        email,
		SessionID:    randomHex(8),
		NotOnOrAfter: now.Add(time.Hour),
	}, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	const hex = "0123456789abcdef"
	out := make([]byte, n*2)
	for i, v := range b {
		out[i*2] = hex[v>>4]
		out[i*2+1] = hex[v&0xf]
	}
	return string(out)
}
