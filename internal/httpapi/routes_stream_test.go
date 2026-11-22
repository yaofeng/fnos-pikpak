package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamProbeRange(t *testing.T) {
	h := NewMux(Deps{Version: "test", Debug: true})

	req := httptest.NewRequest(http.MethodGet, "/api/stream-probe?size=4096", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("Range", "bytes=100-199")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 100-199/4096" {
		t.Fatalf("content-range = %q", got)
	}
	if rec.Body.Len() != 100 {
		t.Fatalf("body = %d bytes", rec.Body.Len())
	}
}

func TestStreamProbeRejectsTooLarge(t *testing.T) {
	h := NewMux(Deps{Version: "test", Debug: true})

	req := httptest.NewRequest(http.MethodGet, "/api/stream-probe?size=999999999999", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestProbeRoutesNeedTheDebugFlag(t *testing.T) {
	h := NewMux(Deps{Version: "test"})

	req := httptest.NewRequest(http.MethodGet, "/api/stream-probe?size=4096", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stream probe without --debug: status=%d, want 404", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/upload/probe", strings.NewReader(`{}`))
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("upload probe without --debug: status=%d, want 404", rec.Code)
	}
}
