package api

import (
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestSCIMUserResourceShape(t *testing.T) {
	org, id := uuid.New(), uuid.New()
	res := scimUserResource(org, id, "ada@acme.dev", "Ada", "admin")
	if res["userName"] != "ada@acme.dev" {
		t.Fatalf("userName = %v", res["userName"])
	}
	schemas, _ := res["schemas"].([]string)
	if len(schemas) != 1 || schemas[0] != scimUserSchema {
		t.Fatalf("schemas = %v", schemas)
	}
	ext, ok := res["urn:laststate:params:scim:schemas:extension:2.0:User"].(map[string]any)
	if !ok || ext["role"] != "admin" || ext["organizationId"] != org.String() {
		t.Fatalf("extension = %v", res["urn:laststate:params:scim:schemas:extension:2.0:User"])
	}
	meta, _ := res["meta"].(map[string]any)
	if meta["resourceType"] != "User" {
		t.Fatalf("meta = %v", meta)
	}
}

func TestMatchUserNameFilter(t *testing.T) {
	if !matchUserNameFilter(`userName eq "Ada@Acme.dev"`, "ada@acme.dev") {
		t.Fatal("case-insensitive eq should match")
	}
	if matchUserNameFilter(`userName eq "bob@acme.dev"`, "ada@acme.dev") {
		t.Fatal("different address should not match")
	}
	if !matchUserNameFilter(`emails.value co "acme"`, "ada@acme.dev") {
		t.Fatal("unknown filter shape must not drop rows")
	}
}

func TestSCIMPageBounds(t *testing.T) {
	r := httptest.NewRequest("GET", "/scim/v2/Users?startIndex=999&count=10", nil)
	start, count := scimPage(r, 3)
	if start != 3 || count != 10 {
		t.Fatalf("start/count = %d/%d, want 3/10 clamped", start, count)
	}
}
