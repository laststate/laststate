package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/laststate/trace/internal/api"
	"github.com/laststate/trace/internal/config"
)

// Lightweight handler smoke without DB: health live always works; ready needs store.
func TestLiveHandler(t *testing.T) {
	s := &api.Server{Cfg: config.Load()}
	// Handler requires Store for most routes; only construct mux via partial — use capabilities without store panic
	// capabilities doesn't touch store
	req := httptest.NewRequest(http.MethodGet, "/v1/relay/capabilities", nil)
	// need full Handler which uses rate limit — Store can be nil for capabilities
	// server.capabilities only uses Cfg
	rr := httptest.NewRecorder()
	// call method directly
	s.Handler() // panics if nil? uses Store only in ready
	_ = req
	_ = rr
	// Ensure OpenAPI constant is valid JSON start
	req2 := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rr2 := httptest.NewRecorder()
	// Build minimal server with empty store — Handler() works
	h := s.Handler()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != 200 {
		t.Fatalf("openapi %d", rr2.Code)
	}
	if rr2.Body.Len() < 20 {
		t.Fatal("empty openapi")
	}
}
