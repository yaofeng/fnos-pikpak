package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

// testAccountID is the account row bindTestUser creates for a uid, so tests
// that seed account-keyed state can name the same id.
func testAccountID(uid int) string {
	return "acct-" + strconv.Itoa(uid)
}

// bindTestUser binds one PikPak account to the uid: an account row plus its
// stored credential. Every minting site now signs stream tokens with a real
// account id, so a credential-only user would fail at mint time — tests bind
// the real row instead of minting empty-account tokens.
func bindTestUser(t *testing.T, st *store.Store, uid int) {
	t.Helper()
	if err := st.PutAccount(context.Background(), store.PikPakAccount{
		ID:          testAccountID(uid),
		UID:         uid,
		DisplayName: "测试账号" + strconv.Itoa(uid),
		Status:      "active",
		BoundAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutAccount: %v", err)
	}
	if err := st.PutCredential(context.Background(), store.Credential{
		UID: uid, AccountID: testAccountID(uid), AuthType: "oauth", ClientID: pikpak.DefaultClientID,
		AccessToken: "at", RefreshToken: "rt",
		DeviceID:        "0123456789abcdef0123456789abcdef",
		AccessExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}
}

func TestAccountsRequiresBinding(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)
	rec, body := getJSON(t, h, "/api/accounts", "1000")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%v, want 401", rec.Code, body)
	}
	if !strings.Contains(rec.Body.String(), "not_bound") {
		t.Fatalf("body = %s, want not_bound", rec.Body)
	}
}

func TestAccountsListsQuotaAndVIPFromOneFetch(t *testing.T) {
	var aboutCalls, vipCalls int32
	srv := accountServer(t, &aboutCalls, &vipCalls)
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	deps.Accounts = newAccountCache(time.Minute)
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/accounts", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	var out struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items = %v", out.Items)
	}
	item := out.Items[0]
	if item["quota_limit"] != float64(1000) || item["quota_usage"] != float64(250) {
		t.Fatalf("quota = %v", item)
	}
	if item["vip_type"] != "platinum" || item["vip_expire"] != "2027-01-01T00:00:00Z" {
		t.Fatalf("vip = %v", item)
	}
	if item["id"] != "acct-a" || item["status"] != "active" || item["error"] != "" {
		t.Fatalf("item = %v", item)
	}
	if got := atomic.LoadInt32(&aboutCalls); got != 1 {
		t.Fatalf("about calls = %d, want 1", got)
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

func TestBindingWritesAccountRecord(t *testing.T) {
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

	accounts, err := st.ListAccounts(t.Context(), 1000)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("ListAccounts = %+v, %v", accounts, err)
	}
	acct := accounts[0]
	if acct.PikPakSub != "sub-9" || acct.Status != "active" {
		t.Fatalf("account = %+v", acct)
	}
	cred, err := st.GetAccountCredential(t.Context(), 1000, acct.ID)
	if err != nil {
		t.Fatalf("GetAccountCredential: %v", err)
	}
	if cred.AccessToken != "at-1" || cred.DeviceID != st.DeriveDeviceID(acct.ID) {
		t.Fatalf("cred = %+v (want the derived device id)", cred)
	}
}

func TestAccountRename(t *testing.T) {
	var aboutCalls, vipCalls int32
	srv := accountServer(t, &aboutCalls, &vipCalls)
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	deps.Accounts = newAccountCache(time.Minute)
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	h := NewMux(deps)

	// Rename trims whitespace and takes effect on the row and the listing.
	rec, body := postJSON(t, h, "/api/accounts/acct-a/rename", "1000", `{"display_name":"  主力号  "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d body=%v", rec.Code, body)
	}
	if item, _ := body["item"].(map[string]any); item["display_name"] != "主力号" {
		t.Fatalf("item display_name = %v", item["display_name"])
	}
	if acct, err := st.GetAccount(context.Background(), 1000, "acct-a"); err != nil || acct.DisplayName != "主力号" {
		t.Fatalf("store display_name = %q, %v", acct.DisplayName, err)
	}
	rec2, body2 := getJSON(t, h, "/api/accounts", "1000")
	if rec2.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec2.Code)
	}
	items := body2["items"].([]any)
	if items[0].(map[string]any)["display_name"] != "主力号" {
		t.Fatalf("listing display_name = %v", items[0])
	}

	// Whitespace-only → 400 with the exact copy.
	rec3, body3 := postJSON(t, h, "/api/accounts/acct-a/rename", "1000", `{"display_name":"   "}`)
	errObj3, _ := body3["error"].(map[string]any)
	if rec3.Code != http.StatusBadRequest || errObj3["message"] != "账号名称不能为空" {
		t.Fatalf("empty rename = %d %v", rec3.Code, body3)
	}

	// Over the 50-rune cap → 400.
	long := strings.Repeat("长", 51)
	rec4, body4 := postJSON(t, h, "/api/accounts/acct-a/rename", "1000",
		fmt.Sprintf(`{"display_name":%q}`, long))
	errObj4, _ := body4["error"].(map[string]any)
	if rec4.Code != http.StatusBadRequest || errObj4["message"] != "账号名称过长，最多 50 个字符" {
		t.Fatalf("long rename = %d %v", rec4.Code, body4)
	}

	// Foreign uid and unknown id → 404, name untouched.
	rec5, _ := postJSON(t, h, "/api/accounts/acct-a/rename", "1001", `{"display_name":"偷改"}`)
	if rec5.Code != http.StatusNotFound {
		t.Fatalf("foreign uid rename status = %d", rec5.Code)
	}
	rec6, _ := postJSON(t, h, "/api/accounts/no-such/rename", "1000", `{"display_name":"x"}`)
	if rec6.Code != http.StatusNotFound {
		t.Fatalf("unknown id rename status = %d", rec6.Code)
	}
	if acct, _ := st.GetAccount(context.Background(), 1000, "acct-a"); acct.DisplayName != "主力号" {
		t.Fatalf("display_name changed by rejected attempts: %q", acct.DisplayName)
	}
}
