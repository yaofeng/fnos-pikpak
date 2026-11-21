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

func TestGatewayInfo(t *testing.T) {
	h := NewMux(Deps{Version: "test"})

	req := httptest.NewRequest(http.MethodGet, "/api/gateway/info?x=1", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("Origin", "https://nas.local")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	var body struct {
		Path             string         `json:"path"`
		Origin           string         `json:"origin"`
		ForwardedHeaders map[string]int `json:"forwarded_headers"`
		Identity         struct {
			UID int `json:"uid"`
		} `json:"identity"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Path != "/api/gateway/info" {
		t.Fatalf("path = %q", body.Path)
	}
	if body.Origin != "https://nas.local" {
		t.Fatalf("origin = %q", body.Origin)
	}
	if body.ForwardedHeaders["X-Trim-Userid"] != 1 {
		t.Fatalf("headers = %+v", body.ForwardedHeaders)
	}
	if body.Identity.UID != 1000 {
		t.Fatalf("identity = %+v", body.Identity)
	}
}

func TestGatewayForgedDetectsDuplicates(t *testing.T) {
	h := NewMux(Deps{Version: "test"})

	req := httptest.NewRequest(http.MethodPost, "/api/gateway/forged", nil)
	req.Header.Add("X-Trim-Userid", "1000")
	req.Header.Add("X-Trim-Userid", "99999")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	var body struct {
		HeaderCount       int  `json:"header_count"`
		DuplicateDetected bool `json:"duplicate_detected"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.HeaderCount != 2 || !body.DuplicateDetected {
		t.Fatalf("body = %+v", body)
	}
}
