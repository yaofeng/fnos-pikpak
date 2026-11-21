package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSPAFallbackAndAPI404(t *testing.T) {
	h := NewMux(Deps{Version: "test"})

	t.Run("index", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Trim-Userid", "1000")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("status=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
		}
	})

	t.Run("spa fallback", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/some/deep/link", nil)
		req.Header.Set("X-Trim-Userid", "1000")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d", rec.Code)
		}
	})

	t.Run("unknown api is json 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/nope", nil)
		req.Header.Set("X-Trim-Userid", "1000")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("status=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
		}
	})
}

func TestGatewayPrefixIsOptional(t *testing.T) {
	h := NewMux(Deps{Version: "test"})

	req := httptest.NewRequest(http.MethodGet, "/app/pikpak/api/health", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
}
