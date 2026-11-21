package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/store"
)

// accountServer serves the two endpoints the account summary needs and counts
// how often each was hit, so cache behavior is observable.
func accountServer(t *testing.T, aboutCalls, vipCalls *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/drive/v1/about":
			atomic.AddInt32(aboutCalls, 1)
			_, _ = io.WriteString(w, `{"quota":{"limit":"1000","usage":"250"},"expires_at":""}`)
		case "/drive/v1/privilege/vip":
			atomic.AddInt32(vipCalls, 1)
			_, _ = io.WriteString(w, `{"data":{"type":"platinum","expire":"2027-01-01T00:00:00Z"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func bindTestUser(t *testing.T, st *store.Store, uid int) {
	t.Helper()
	if err := st.PutCredential(context.Background(), store.Credential{
		UID: uid, AuthType: "oauth", ClientID: pikpak.DefaultClientID,
		AccessToken: "at", RefreshToken: "rt",
		DeviceID:        "0123456789abcdef0123456789abcdef",
		AccessExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}
}

func TestAccountRequiresBinding(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)
	rec, body := getJSON(t, h, "/api/account", "1000")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%v, want 401", rec.Code, body)
	}
}

func TestAccountReturnsQuotaAndVIPTwiceFromOneFetch(t *testing.T) {
	var aboutCalls, vipCalls int32
	srv := accountServer(t, &aboutCalls, &vipCalls)
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	deps.Accounts = newAccountCache(time.Minute)
	bindTestUser(t, st, 1000)
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/account", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if body["quota_limit"] != float64(1000) || body["quota_usage"] != float64(250) {
		t.Fatalf("quota = %v", body)
	}
	if body["vip_type"] != "platinum" || body["vip_expire"] != "2027-01-01T00:00:00Z" {
		t.Fatalf("vip = %v", body)
	}

	// A second request inside the TTL is served from the per-user cache.
	if rec, _ := getJSON(t, h, "/api/account", "1000"); rec.Code != http.StatusOK {
		t.Fatalf("second status = %d", rec.Code)
	}
	if got := atomic.LoadInt32(&aboutCalls); got != 1 {
		t.Fatalf("about calls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&vipCalls); got != 1 {
		t.Fatalf("vip calls = %d, want 1", got)
	}
}

func TestAccountCacheIsPerUser(t *testing.T) {
	var aboutCalls, vipCalls int32
	srv := accountServer(t, &aboutCalls, &vipCalls)
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	deps.Accounts = newAccountCache(time.Minute)
	bindTestUser(t, st, 1000)
	bindTestUser(t, st, 1001)
	h := NewMux(deps)

	getJSON(t, h, "/api/account", "1000")
	getJSON(t, h, "/api/account", "1001")
	if got := atomic.LoadInt32(&aboutCalls); got != 2 {
		t.Fatalf("about calls = %d, want 2 (one per user)", got)
	}
}

func TestRebindClearsReauthStatus(t *testing.T) {
	deps, st := newAuthTestDeps(t, "http://127.0.0.1:1")
	bindTestUser(t, st, 1000)
	if err := st.PutUser(t.Context(), store.User{UID: 1000, Status: "reauth_required"}); err != nil {
		t.Fatalf("PutUser: %v", err)
	}
	h := NewMux(deps)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/rebind", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}

	u, err := st.GetUser(t.Context(), 1000)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.Status != "active" {
		t.Fatalf("status = %q, want active", u.Status)
	}
}

func TestRebindRequiresBinding(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/rebind", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestCancelFlowOnlyTouchesOwnPendingFlow(t *testing.T) {
	deps, st := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)

	put := func(id string, uid int, status string) {
		t.Helper()
		if err := st.PutAuthFlow(t.Context(), store.Flow{
			ID: id, UID: uid, DeviceCode: "d", UserCode: "U",
			VerifyURL: "https://mypikpak.com/x", IntervalSec: 1,
			ExpiresAt: time.Now().Add(time.Minute), Status: status,
		}); err != nil {
			t.Fatalf("PutAuthFlow: %v", err)
		}
	}
	put("f1", 1000, "pending")
	put("f2", 1001, "pending")

	post := func(path, uid string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("X-Trim-Userid", uid)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := post("/api/auth/flow/f2/cancel", "1000"); rec.Code != http.StatusNotFound {
		t.Fatalf("other user's flow status = %d, want 404", rec.Code)
	}
	if rec := post("/api/auth/flow/f1/cancel", "1000"); rec.Code != http.StatusOK {
		t.Fatalf("cancel status = %d body=%s", rec.Code, rec.Body)
	}

	f, _ := st.GetAuthFlow(t.Context(), "f1")
	if f.Status != "cancelled" {
		t.Fatalf("f1 status = %q", f.Status)
	}
	f2, _ := st.GetAuthFlow(t.Context(), "f2")
	if f2.Status != "pending" {
		t.Fatalf("f2 status = %q", f2.Status)
	}
}

func TestBindingWritesUserRecord(t *testing.T) {
	srv := pikpakAuthServer(t, `{"access_token":"at-1","refresh_token":"rt-1","expires_in":43200,"sub":"sub-9"}`, http.StatusOK)
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	h := NewMux(deps)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/device", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var start struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatalf("decode: %v", err)
	}
	waitFlowStatus(t, h, start.ID, "bound")

	u, err := st.GetUser(t.Context(), 1000)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.PikPakSub != "sub-9" || u.Status != "active" {
		t.Fatalf("user = %+v", u)
	}
}
