package httpapi

import (
	"encoding/json"
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

	t.Run("missing asset is 404 not html", func(t *testing.T) {
		// A launcher icon that is not embedded must not be answered with the
		// SPA document: the browser would render a broken image either way,
		// and the HTML response hides the real cause.
		req := httptest.NewRequest(http.MethodGet, "/images/icon_64.png", nil)
		req.Header.Set("X-Trim-Userid", "1000")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
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

// The gateway loads the app at the configured URL, /app/pikpak, with no
// trailing slash. Without a redirect the page's relative asset URLs resolve
// against / and the app never boots.
func TestBareGatewayPrefixRedirectsToSlash(t *testing.T) {
	h := NewMux(Deps{Version: "test"})

	req := httptest.NewRequest(http.MethodGet, "/app/pikpak?x=1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound && rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status=%d, want a redirect", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/app/pikpak/?x=1" {
		t.Fatalf("Location = %q", loc)
	}

	req = httptest.NewRequest(http.MethodGet, "/app/pikpak/", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<div id=\"root\"") {
		t.Fatalf("slash status=%d body=%s", rec.Code, rec.Body)
	}
}

// Report item 1 asks whether the gateway forwards /app/pikpak or strips it;
// the answer has to survive prefix stripping.
func TestGatewayInfoReportsOriginalPath(t *testing.T) {
	h := NewMux(Deps{Version: "test"})

	req := httptest.NewRequest(http.MethodGet, "/app/pikpak/api/gateway/info", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["original_path"] != "/app/pikpak/api/gateway/info" {
		t.Fatalf("original_path = %v", body["original_path"])
	}
	if body["path"] != "/api/gateway/info" {
		t.Fatalf("path = %v", body["path"])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/gateway/info", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["original_path"] != "/api/gateway/info" {
		t.Fatalf("unprefixed original_path = %v", body["original_path"])
	}
}
