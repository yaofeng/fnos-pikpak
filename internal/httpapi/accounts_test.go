package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/store"
)

// bindAccount creates one bound PikPak account for tests: the account row plus
// its stored credential, with a distinctive device id so "the original device
// id was reused" is observable.
func bindAccount(t *testing.T, st *store.Store, uid int, id, sub, status string) store.PikPakAccount {
	t.Helper()
	acct := store.PikPakAccount{
		ID:          id,
		UID:         uid,
		PikPakSub:   sub,
		DisplayName: "账号" + id,
		Status:      status,
		BoundAt:     time.Now().UTC(),
	}
	if err := st.PutAccount(context.Background(), acct); err != nil {
		t.Fatalf("PutAccount %s: %v", id, err)
	}
	if err := st.PutCredential(context.Background(), store.Credential{
		UID: uid, AccountID: id, AuthType: "oauth", ClientID: pikpak.DefaultClientID,
		AccessToken: "at-" + id, RefreshToken: "rt-" + id,
		DeviceID:        "dev-" + id + "-0123456789abcdef",
		AccessExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("PutCredential %s: %v", id, err)
	}
	return acct
}

// serveAs runs a probe handler through the gateway identity middleware, so
// helpers reading fnos.IdentityFrom see the uid named by the header.
func serveAs(t *testing.T, deps Deps, uid, path string, probe func(w http.ResponseWriter, r *http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Trim-Userid", uid)
	inner := http.HandlerFunc(probe)
	fnos.WithIdentity(0, inner).ServeHTTP(rec, req)
	return rec
}

func TestResolveAccountOwnership(t *testing.T) {
	deps, st := newAuthTestDeps(t, "http://127.0.0.1:1")
	a := bindAccount(t, st, 1001, "acct-a", "sub-a", "active")
	bindAccount(t, st, 1001, "acct-b", "sub-b", "active")

	// ?account=a resolves the caller's own account.
	rec := serveAs(t, deps, "1001", "/api/anything?account=acct-a", func(w http.ResponseWriter, r *http.Request) {
		client, acct, ok := accountClient(w, r, deps)
		if !ok || client == nil || acct.ID != a.ID {
			t.Fatalf("accountClient ok=%v acct=%+v", ok, acct)
		}
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}

	// An unknown id is a 404 with the structured error body.
	rec = serveAs(t, deps, "1001", "/api/anything?account=missing", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := accountClient(w, r, deps); ok {
			t.Fatal("accountClient resolved a missing account")
		}
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "not_found" {
		t.Fatalf("error code = %q, want not_found", body.Error.Code)
	}

	// Another user's account id resolves to nothing (same 404).
	rec = serveAs(t, deps, "2000", "/api/anything?account=acct-a", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := accountClient(w, r, deps); ok {
			t.Fatal("accountClient resolved another user's account")
		}
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign account status = %d, want 404", rec.Code)
	}

	// No parameter selects the default account.
	rec = serveAs(t, deps, "1001", "/api/anything", func(w http.ResponseWriter, r *http.Request) {
		_, acct, ok := accountClient(w, r, deps)
		if !ok || acct.ID != a.ID {
			t.Fatalf("default account = %+v ok=%v (want %s)", acct, ok, a.ID)
		}
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("default status = %d body=%s", rec.Code, rec.Body)
	}
}

func TestRandomAccountWithoutActiveFails(t *testing.T) {
	// The only account needs reauthorization: no usable account.
	deps, st := newAuthTestDeps(t, "http://127.0.0.1:1")
	bindAccount(t, st, 1000, "acct-a", "sub-a", "reauth_required")

	rec := serveAs(t, deps, "1000", "/api/anything", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := randomAccount(w, r, deps); ok {
			t.Fatal("randomAccount served a reauth_required account")
		}
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody.Error.Code != "no_active_account" {
		t.Fatalf("error code = %q, want no_active_account", errBody.Error.Code)
	}

	// Nothing bound at all: the same structured refusal.
	deps2, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	rec2 := serveAs(t, deps2, "1000", "/api/anything", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := randomAccount(w, r, deps2); ok {
			t.Fatal("randomAccount served an unbound user")
		}
	})
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("unbound status = %d, want 401", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), "no_active_account") {
		t.Fatalf("unbound body = %s", rec2.Body)
	}
}

func TestShuffledActivesCoversAll(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var accounts []store.PikPakAccount
	want := []string{"a", "b", "c", "d", "e"}
	for i, id := range want {
		accounts = append(accounts, store.PikPakAccount{ID: id, Status: "active", BoundAt: base.Add(time.Duration(i) * time.Hour)})
	}
	// A reauth_required row never enters the shuffle.
	accounts = append(accounts, store.PikPakAccount{ID: "skip", Status: "reauth_required"})

	for i := 0; i < 50; i++ {
		got := shuffledActives(accounts)
		if len(got) != len(want) {
			t.Fatalf("shuffle %d: len = %d, want %d", i, len(got), len(want))
		}
		ids := make([]string, 0, len(got))
		for _, a := range got {
			ids = append(ids, a.ID)
		}
		sort.Strings(ids)
		for j := range want {
			if ids[j] != want[j] {
				t.Fatalf("shuffle %d: ids = %v, want %v", i, ids, want)
			}
		}
	}
}

func TestAccountsEndpointListsAll(t *testing.T) {
	// The upstream answers for account a's token and refuses account b's, so
	// one row carries quotas and the other carries the failure.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "Bearer at-acct-b" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":"forbidden","error_code":4000}`)
			return
		}
		switch r.URL.Path {
		case "/drive/v1/about":
			_, _ = io.WriteString(w, `{"quota":{"limit":"1000","usage":"250"},"expires_at":""}`)
		case "/drive/v1/privilege/vip":
			_, _ = io.WriteString(w, `{"data":{"type":"platinum","expire":"2027-01-01T00:00:00Z"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	bindAccount(t, st, 1000, "acct-b", "sub-b", "active")
	h := NewMux(deps)

	rec, _ := getJSON(t, h, "/api/accounts", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	var out struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("items = %d, want 2 (%s)", len(out.Items), rec.Body)
	}
	byID := map[string]map[string]any{}
	for _, item := range out.Items {
		byID[item["id"].(string)] = item
	}
	good := byID["acct-a"]
	if good == nil || good["quota_limit"] != float64(1000) || good["quota_usage"] != float64(250) {
		t.Fatalf("acct-a item = %v", good)
	}
	if good["vip_type"] != "platinum" || good["status"] != "active" || good["display_name"] != "账号acct-a" {
		t.Fatalf("acct-a item = %v", good)
	}
	if good["error"] != "" {
		t.Fatalf("acct-a error = %v", good["error"])
	}
	bad := byID["acct-b"]
	if bad == nil || bad["error"] == "" {
		t.Fatalf("acct-b item = %v, want a non-empty error", bad)
	}
	if bad["quota_limit"] != float64(0) || bad["quota_usage"] != float64(0) {
		t.Fatalf("acct-b quota = %v", bad)
	}

	// A second request inside the TTL re-serves the fetched snapshot.
	if rec, _ := getJSON(t, h, "/api/accounts", "1000"); rec.Code != http.StatusOK {
		t.Fatalf("second status = %d", rec.Code)
	}
}

func TestLogoutSingleAccount(t *testing.T) {
	deps, st := newAuthTestDeps(t, "http://127.0.0.1:1")
	a := bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	b := bindAccount(t, st, 1000, "acct-b", "sub-b", "active")
	h := NewMux(deps)

	// Each account has its own job rows and a pending flow.
	for _, j := range []store.DownloadJob{
		{ID: "d-a", UID: 1000, AccountID: a.ID, FileID: "f1", FileName: "a", DestDir: "/tmp", DestPath: "/tmp/a", Status: "queued"},
		{ID: "d-b", UID: 1000, AccountID: b.ID, FileID: "f2", FileName: "b", DestDir: "/tmp", DestPath: "/tmp/b", Status: "queued"},
		{ID: "d-other", UID: 2000, FileID: "f3", FileName: "c", DestDir: "/tmp", DestPath: "/tmp/c", Status: "queued"},
	} {
		if err := st.PutDownloadJob(t.Context(), j); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.PutUploadJob(t.Context(), store.UploadJob{
		ID: "u-a", UID: 1000, AccountID: a.ID, LocalPath: "/tmp/a", RemoteName: "a", Status: "queued",
	}); err != nil {
		t.Fatal(err)
	}
	putFlow := func(id string, accountID string) {
		t.Helper()
		if err := st.PutAuthFlow(t.Context(), store.Flow{
			ID: id, UID: 1000, AccountID: accountID, DeviceCode: "d", UserCode: "U",
			VerifyURL: "https://mypikpak.com/x", IntervalSec: 1,
			ExpiresAt: time.Now().Add(time.Minute), Status: "pending",
		}); err != nil {
			t.Fatalf("PutAuthFlow %s: %v", id, err)
		}
	}
	putFlow("flow-a", a.ID)
	putFlow("flow-b", b.ID)

	rec, _ := postJSON(t, h, "/api/auth/logout", "1000", `{"account_id":"acct-a"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d body=%s", rec.Code, rec.Body)
	}

	if _, err := st.GetAccountCredential(t.Context(), 1000, a.ID); err != store.ErrNotFound {
		t.Fatalf("a credential after logout = %v, want ErrNotFound", err)
	}
	if _, err := st.GetAccountCredential(t.Context(), 1000, b.ID); err != nil {
		t.Fatalf("b credential after logout: %v", err)
	}
	if _, err := st.GetAccount(t.Context(), 1000, a.ID); err != store.ErrNotFound {
		t.Fatalf("a row after logout = %v, want ErrNotFound", err)
	}
	if _, err := st.GetAccount(t.Context(), 1000, b.ID); err != nil {
		t.Fatalf("b row after logout: %v", err)
	}
	jobs, _ := st.ListDownloadJobs(t.Context(), 1000)
	if len(jobs) != 1 || jobs[0].ID != "d-b" {
		t.Fatalf("downloads after logout = %+v", jobs)
	}
	if ups, _ := st.ListUploadJobs(t.Context(), 1000); len(ups) != 0 {
		t.Fatalf("uploads after logout = %+v", ups)
	}
	if other, _ := st.ListDownloadJobs(t.Context(), 2000); len(other) != 1 {
		t.Fatalf("another user's job was deleted: %+v", other)
	}
	if f, _ := st.GetAuthFlow(t.Context(), "flow-a"); f.Status != "cancelled" {
		t.Fatalf("flow-a status = %q, want cancelled", f.Status)
	}
	if f, _ := st.GetAuthFlow(t.Context(), "flow-b"); f.Status != "pending" {
		t.Fatalf("flow-b status = %q, want pending", f.Status)
	}
}

func TestRebindFlowTargetsExistingRow(t *testing.T) {
	t.Run("same sub writes back to the row", func(t *testing.T) {
		srv := pikpakAuthServer(t, `{"access_token":"at-2","refresh_token":"rt-2","expires_in":43200,"sub":"sub-a"}`, http.StatusOK)
		defer srv.Close()
		deps, st := newAuthTestDeps(t, srv.URL)
		a := bindAccount(t, st, 1000, "acct-a", "sub-a", "reauth_required")
		h := NewMux(deps)

		rec, _ := postJSON(t, h, "/api/auth/device", "1000", `{"account_id":"acct-a"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("device status = %d body=%s", rec.Code, rec.Body)
		}
		var start struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &start)
		waitFlowStatus(t, h, start.ID, "bound")

		cred, err := st.GetAccountCredential(t.Context(), 1000, a.ID)
		if err != nil {
			t.Fatalf("GetAccountCredential: %v", err)
		}
		if cred.AccountID != a.ID || cred.AccessToken != "at-2" || cred.RefreshToken != "rt-2" {
			t.Fatalf("cred = %+v", cred)
		}
		if cred.DeviceID != "dev-acct-a-0123456789abcdef" {
			t.Fatalf("device id was re-derived: %q", cred.DeviceID)
		}

		acct, err := st.GetAccount(t.Context(), 1000, a.ID)
		if err != nil {
			t.Fatalf("GetAccount: %v", err)
		}
		if acct.Status != "active" || acct.PikPakSub != "sub-a" {
			t.Fatalf("account after rebind = %+v", acct)
		}
		if !acct.BoundAt.Equal(a.BoundAt) {
			t.Fatalf("bound_at changed: %v, was %v", acct.BoundAt, a.BoundAt)
		}
	})

	t.Run("different sub fails the flow", func(t *testing.T) {
		srv := pikpakAuthServer(t, `{"access_token":"at-x","refresh_token":"rt-x","expires_in":43200,"sub":"sub-zzz"}`, http.StatusOK)
		defer srv.Close()
		deps, st := newAuthTestDeps(t, srv.URL)
		a := bindAccount(t, st, 1000, "acct-b", "sub-b", "active")
		h := NewMux(deps)

		rec, _ := postJSON(t, h, "/api/auth/device", "1000", `{"account_id":"acct-b"}`)
		var start struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &start)
		body := waitFlowStatus(t, h, start.ID, "error")
		if msg, _ := body["error"].(string); !strings.Contains(msg, "不一致") {
			t.Fatalf("flow error = %v, want 不一致", body["error"])
		}
		cred, err := st.GetAccountCredential(t.Context(), 1000, a.ID)
		if err != nil || cred.AccessToken != "at-acct-b" {
			t.Fatalf("credential after failed rebind = %+v, %v", cred, err)
		}
	})
}

func TestNewBindRejectsDuplicateSub(t *testing.T) {
	srv := pikpakAuthServer(t, `{"access_token":"at-9","refresh_token":"rt-9","expires_in":43200,"sub":"sub-S"}`, http.StatusOK)
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	bindAccount(t, st, 1000, "acct-a", "sub-S", "active")
	h := NewMux(deps)

	rec, _ := postJSON(t, h, "/api/auth/device", "1000", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("device status = %d body=%s", rec.Code, rec.Body)
	}
	var start struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &start)
	body := waitFlowStatus(t, h, start.ID, "error")
	if msg, _ := body["error"].(string); !strings.Contains(msg, "已绑定") {
		t.Fatalf("flow error = %v, want 已绑定", body["error"])
	}
	accounts, _ := st.ListAccounts(t.Context(), 1000)
	if len(accounts) != 1 || accounts[0].ID != "acct-a" {
		t.Fatalf("accounts after duplicate bind = %+v", accounts)
	}
	// The duplicate bind must not have replaced the bound account's tokens.
	if cred, err := st.GetAccountCredential(t.Context(), 1000, "acct-a"); err != nil || cred.AccessToken != "at-acct-a" {
		t.Fatalf("bound credential overwritten: %+v, %v", cred, err)
	}
}

// TestAccountsCacheIsPerAccount replaces the per-uid cache check: two accounts
// of one user must each be probed once per TTL window.
func TestAccountsCacheIsPerAccount(t *testing.T) {
	var aboutCalls, vipCalls int32
	srv := accountServer(t, &aboutCalls, &vipCalls)
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	deps.Accounts = newAccountCache(time.Minute)
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	bindAccount(t, st, 1000, "acct-b", "sub-b", "active")
	h := NewMux(deps)

	if rec, _ := getJSON(t, h, "/api/accounts", "1000"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := atomic.LoadInt32(&aboutCalls); got != 2 {
		t.Fatalf("about calls = %d, want 2 (one per account)", got)
	}
	if rec, _ := getJSON(t, h, "/api/accounts", "1000"); rec.Code != http.StatusOK {
		t.Fatalf("second status = %d", rec.Code)
	}
	if got := atomic.LoadInt32(&aboutCalls); got != 2 {
		t.Fatalf("about calls after cached reload = %d, want 2", got)
	}
}
