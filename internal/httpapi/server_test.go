package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthRequiresIdentity(t *testing.T) {
	h := NewMux(Deps{Version: "test"})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestHealthOK(t *testing.T) {
	h := NewMux(Deps{Version: "test"})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var body struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
		UID     int    `json:"uid"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body)
	}
	if !body.OK || body.Version != "test" || body.UID != 1000 {
		t.Fatalf("body = %+v", body)
	}
}

func TestHealthDuplicateIdentityRejected(t *testing.T) {
	h := NewMux(Deps{Version: "test"})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Add("X-Trim-Userid", "1000")
	req.Header.Add("X-Trim-Userid", "1001")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
