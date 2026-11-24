package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fnos-pikpak/internal/probe"
)

// State-changing endpoints only accept JSON bodies. A cross-site form can send
// urlencoded/text bodies without a CORS preflight; requiring JSON means such a
// request is rejected before it can use the victim's gateway identity.
func TestMutationsRequireJSONContentType(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	deps.Probes = &probe.Registry{}
	h := NewMux(deps)

	for _, path := range []string{"/api/auth/device", "/api/auth/logout", "/api/probes/run", "/api/upload/probe"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.Header.Set("X-Trim-Userid", "1000")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("%s status=%d, want 415 (body=%s)", path, rec.Code, rec.Body)
		}
	}

	// The forged-header probe takes no meaningful body; it must stay reachable
	// so the M0 report can measure gateway behavior.
	req := httptest.NewRequest(http.MethodPost, "/api/gateway/forged", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("forged probe status=%d", rec.Code)
	}
}

func TestJSONBodyStillAccepted(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	deps.Probes = &probe.Registry{}
	h := NewMux(deps)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", strings.NewReader(`{"account_id":"x"}`))
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
}
