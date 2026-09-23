package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAPIJSONValid(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(openapiJSON), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["openapi"] == nil || doc["paths"] == nil {
		t.Fatal(doc)
	}
	paths, _ := doc["paths"].(map[string]any)
	required := []string{
		"/v1/ingest", "/api/issues", "/api/events", "/openapi.json",
		"/health/live", "/api/auth/login", "/api/search",
	}
	for _, p := range required {
		if paths[p] == nil {
			t.Fatalf("missing path %s", p)
		}
	}
	if !strings.Contains(openapiJSON, "1.0.0") {
		t.Fatal("version")
	}
}
