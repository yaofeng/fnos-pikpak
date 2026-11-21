package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/store"
)

func newAuthTestDeps(t *testing.T, pikpakURL string) (Deps, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "pikpak.db"), filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sessions := pikpak.NewSessionStore(st, pikpak.NewClient(pikpak.ClientOptions{BaseUser: pikpakURL}))
	return Deps{Version: "test", Store: st, Sessions: sessions}, st
}

func pikpakAuthServer(t *testing.T, tokenBody string, tokenStatus int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/auth/device/code", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"device_code": "dc-test",
			"user_code": "TEST-CODE",
			"expires_in": 300,
			"interval": 1,
			"verification_url": "https://mypikpak.com/drive/activate?client_id=x",
			"verification_uri_complete": "https://mypikpak.com/drive/activate?client_id=x&user_code=TEST-CODE"
		}`)
	})
	mux.HandleFunc("POST /v1/auth/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(tokenStatus)
		_, _ = io.WriteString(w, tokenBody)
	})
	return httptest.NewServer(mux)
}

func getJSON(t *testing.T, h http.Handler, path, uid string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if uid != "" {
		req.Header.Set("X-Trim-Userid", uid)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec, body
}

func waitFlowStatus(t *testing.T, h http.Handler, id, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec, body := getJSON(t, h, "/api/auth/flow/"+id, "1000")
		if rec.Code != http.StatusOK {
			t.Fatalf("flow status=%d body=%s", rec.Code, rec.Body)
		}
		if body["status"] == want {
			return body
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("flow never reached %q", want)
	return nil
}

func TestAuthDeviceFlowBindsAndLogsOut(t *testing.T) {
	srv := pikpakAuthServer(t, `{"token_type":"Bearer","access_token":"at-1","refresh_token":"rt-1","expires_in":43200,"sub":"sub-1"}`, http.StatusOK)
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	h := NewMux(deps)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/device", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("device status=%d body=%s", rec.Code, rec.Body)
	}
	var start struct {
		ID         string `json:"id"`
		UserCode   string `json:"user_code"`
		VerifyURL  string `json:"verify_url"`
		ExpiresIn  int    `json:"expires_in"`
		Interval   int    `json:"interval"`
		DeviceCode string `json:"device_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatalf("decode device: %v", err)
	}
	if start.ID == "" || start.UserCode != "TEST-CODE" || start.Interval != 1 || start.ExpiresIn != 300 {
		t.Fatalf("device response = %+v", start)
	}
	if start.DeviceCode != "" {
		t.Fatal("device_code must never reach the browser")
	}

	waitFlowStatus(t, h, start.ID, "bound")

	// The credential is persisted, encrypted, and usable.
	cred, err := st.GetCredential(t.Context(), 1000)
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if cred.AccessToken != "at-1" || cred.RefreshToken != "rt-1" {
		t.Fatalf("cred = %+v", cred)
	}

	_, me := getJSON(t, h, "/api/me", "1000")
	if me["bound"] != true {
		t.Fatalf("me = %v", me)
	}

	logout := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logout.Header.Set("X-Trim-Userid", "1000")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, logout)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status=%d body=%s", rec.Code, rec.Body)
	}

	_, me = getJSON(t, h, "/api/me", "1000")
	if me["bound"] != false {
		t.Fatalf("after logout me = %v", me)
	}
}

func TestAuthFlowIsScopedToItsUser(t *testing.T) {
	srv := pikpakAuthServer(t, `{"access_token":"at-1","refresh_token":"rt-1","expires_in":43200}`, http.StatusOK)
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	h := NewMux(deps)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/device", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var start struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &start)

	rec, _ = getJSON(t, h, "/api/auth/flow/"+start.ID, "1001")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("other user status=%d, want 404", rec.Code)
	}
}

func TestAuthFlowDenied(t *testing.T) {
	srv := pikpakAuthServer(t, `{"error":"access_denied","error_code":4126}`, http.StatusBadRequest)
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	h := NewMux(deps)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/device", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var start struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &start)

	body := waitFlowStatus(t, h, start.ID, "denied")
	if body["error"] == "" {
		t.Fatalf("denied flow has no error text: %v", body)
	}
}

func TestAuthDeviceUpstreamFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	h := NewMux(deps)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/device", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
}

func TestMeRequiresIdentity(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)
	rec, _ := getJSON(t, h, "/api/me", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}
