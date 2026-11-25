package pikpak

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fnos-pikpak/internal/store"
)

func writeJSONBody(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func testStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "pikpak.db"), filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestStartDeviceFlow(t *testing.T) {
	var (
		gotPath   string
		gotClient string
		gotVer    string
		gotDevice string
		gotBody   map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotClient = r.Header.Get("X-Client-Id")
		gotVer = r.Header.Get("X-Client-Version")
		gotDevice = r.Header.Get("X-Device-Id")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSONBody(w, http.StatusOK, `{
			"device_code": "dc-1",
			"user_code": "ABCD-1234",
			"expires_in": 300,
			"interval": 2,
			"verification_url": "https://mypikpak.com/drive/activate?client_id=x",
			"verification_uri_complete": "https://mypikpak.com/drive/activate?client_id=x&user_code=ABCD-1234"
		}`)
	}))
	defer srv.Close()

	c := NewClient(ClientOptions{BaseUser: srv.URL, DeviceID: "0123456789abcdef0123456789abcdef"})
	flow, err := c.StartDeviceFlow(context.Background())
	if err != nil {
		t.Fatalf("StartDeviceFlow: %v", err)
	}
	if flow.DeviceCode != "dc-1" || flow.UserCode != "ABCD-1234" {
		t.Fatalf("flow codes = %q/%q", flow.DeviceCode, flow.UserCode)
	}
	if flow.Interval != 2 || flow.ExpiresIn != 300 {
		t.Fatalf("flow interval/expiry = %d/%d", flow.Interval, flow.ExpiresIn)
	}
	if flow.VerifyURL != "https://mypikpak.com/drive/activate?client_id=x&user_code=ABCD-1234" {
		t.Fatalf("VerifyURL = %q", flow.VerifyURL)
	}

	if gotPath != "/v1/auth/device/code" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotClient != DefaultClientID {
		t.Fatalf("X-Client-Id = %q", gotClient)
	}
	if gotVer != DefaultClientVersion {
		t.Fatalf("X-Client-Version = %q", gotVer)
	}
	if gotDevice != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("X-Device-Id = %q", gotDevice)
	}
	if gotBody["scope"] != Scope {
		t.Fatalf("scope = %v", gotBody["scope"])
	}
	if gotBody["client_id"] != DefaultClientID {
		t.Fatalf("client_id = %v", gotBody["client_id"])
	}
}

func TestPollDeviceTokenTransitions(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["grant_type"] != "urn:ietf:params:oauth:grant-type:device_code" {
			t.Errorf("grant_type = %v", body["grant_type"])
		}
		if body["device_code"] != "dc-1" {
			t.Errorf("device_code = %v", body["device_code"])
		}
		switch atomic.AddInt32(&calls, 1) {
		case 1:
			writeJSONBody(w, http.StatusBadRequest, `{"error":"authorization_pending","error_code":4124,"error_description":"pending"}`)
		case 2:
			writeJSONBody(w, http.StatusBadRequest, `{"error":"slow_down","error_code":4125,"error_description":"slow"}`)
		default:
			writeJSONBody(w, http.StatusOK, `{"token_type":"Bearer","access_token":"at-1","refresh_token":"rt-1","expires_in":43200,"sub":"sub-1"}`)
		}
	}))
	defer srv.Close()

	c := NewClient(ClientOptions{BaseUser: srv.URL, DeviceID: "0123456789abcdef0123456789abcdef"})
	ctx := context.Background()

	if _, err := c.PollDeviceToken(ctx, "dc-1"); !errors.Is(err, ErrAuthorizationPending) {
		t.Fatalf("first poll err = %v, want ErrAuthorizationPending", err)
	}
	if _, err := c.PollDeviceToken(ctx, "dc-1"); !errors.Is(err, ErrSlowDown) {
		t.Fatalf("second poll err = %v, want ErrSlowDown", err)
	}
	tok, err := c.PollDeviceToken(ctx, "dc-1")
	if err != nil {
		t.Fatalf("third poll: %v", err)
	}
	if tok.AccessToken != "at-1" || tok.RefreshToken != "rt-1" || tok.ExpiresIn != 43200 || tok.Sub != "sub-1" {
		t.Fatalf("token = %+v", tok)
	}
}

func TestPollDeviceTokenMapsDeniedAndExpired(t *testing.T) {
	cases := []struct {
		body string
		want error
	}{
		{`{"error":"access_denied","error_code":4126}`, ErrAccessDenied},
		{`{"error":"expired_token","error_code":4127}`, ErrExpiredToken},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSONBody(w, http.StatusBadRequest, tc.body)
		}))
		c := NewClient(ClientOptions{BaseUser: srv.URL})
		_, err := c.PollDeviceToken(context.Background(), "dc")
		srv.Close()
		if !errors.Is(err, tc.want) {
			t.Fatalf("body %s: err = %v, want %v", tc.body, err, tc.want)
		}
	}
}

func TestRefreshRotatesToken(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		writeJSONBody(w, http.StatusOK, `{"token_type":"Bearer","access_token":"at-2","refresh_token":"rt-2","expires_in":43200,"sub":"sub-1"}`)
	}))
	defer srv.Close()

	c := NewClient(ClientOptions{BaseUser: srv.URL, DeviceID: "0123456789abcdef0123456789abcdef"})
	tok, err := c.Refresh(context.Background(), "rt-1")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if tok.AccessToken != "at-2" || tok.RefreshToken != "rt-2" {
		t.Fatalf("token = %+v", tok)
	}
	if got["grant_type"] != "refresh_token" || got["refresh_token"] != "rt-1" {
		t.Fatalf("refresh body = %v", got)
	}
}

func TestSessionSingleFlightRefresh(t *testing.T) {
	var refreshCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshCalls, 1)
		// Hold long enough that a broken implementation would overlap.
		time.Sleep(50 * time.Millisecond)
		writeJSONBody(w, http.StatusOK, `{"token_type":"Bearer","access_token":"at-new","refresh_token":"rt-new","expires_in":43200,"sub":"sub-1"}`)
	}))
	defer srv.Close()

	st := testStore(t)
	ctx := context.Background()
	err := st.PutCredential(ctx, store.Credential{
		UID: 1000, AuthType: "oauth", ClientID: DefaultClientID,
		AccessToken: "at-old", RefreshToken: "rt-old",
		DeviceID:        "0123456789abcdef0123456789abcdef",
		AccessExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("PutCredential: %v", err)
	}

	ss := NewSessionStore(st, NewClient(ClientOptions{BaseUser: srv.URL}))
	sess, err := ss.GetAccountSession(ctx, 1000, "")
	if err != nil {
		t.Fatalf("GetAccountSession: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 100)
	tokens := make(chan string, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := sess.Token(ctx)
			if err != nil {
				errs <- err
				return
			}
			tokens <- tok
		}()
	}
	wg.Wait()
	close(errs)
	close(tokens)
	for err := range errs {
		t.Fatalf("Token: %v", err)
	}
	for tok := range tokens {
		if tok != "at-new" {
			t.Fatalf("token = %q, want at-new", tok)
		}
	}
	if n := atomic.LoadInt32(&refreshCalls); n != 1 {
		t.Fatalf("refresh calls = %d, want 1", n)
	}

	cred, err := st.GetAccountCredential(ctx, 1000, "")
	if err != nil {
		t.Fatalf("GetAccountCredential: %v", err)
	}
	if cred.RefreshToken != "rt-new" || cred.AccessToken != "at-new" {
		t.Fatalf("persisted cred = %+v", cred)
	}
}

func TestSessionInvalidGrantRequiresReauth(t *testing.T) {
	var refreshCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshCalls, 1)
		writeJSONBody(w, http.StatusBadRequest, `{"error":"invalid_grant","error_code":4126,"error_description":"refresh token invalid"}`)
	}))
	defer srv.Close()

	st := testStore(t)
	ctx := context.Background()
	if err := st.PutCredential(ctx, store.Credential{
		UID: 1000, AuthType: "oauth", ClientID: DefaultClientID,
		AccessToken: "at-old", RefreshToken: "rt-old",
		DeviceID:        "0123456789abcdef0123456789abcdef",
		AccessExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}

	ss := NewSessionStore(st, NewClient(ClientOptions{BaseUser: srv.URL}))
	sess, err := ss.GetAccountSession(ctx, 1000, "")
	if err != nil {
		t.Fatalf("GetAccountSession: %v", err)
	}

	if _, err := sess.Token(ctx); !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("Token err = %v, want ErrReauthRequired", err)
	}
	if _, err := sess.Token(ctx); !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("second Token err = %v, want ErrReauthRequired", err)
	}
	if n := atomic.LoadInt32(&refreshCalls); n != 1 {
		t.Fatalf("refresh calls = %d, want 1 (no retry after reauth)", n)
	}

	// The shell decides whether to show the reauthorization banner from the
	// stored status, so a failed refresh has to write it.
	u, err := st.GetUser(ctx, 1000)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.Status != "reauth_required" {
		t.Fatalf("stored status = %q, want reauth_required", u.Status)
	}
}

func TestSessionStoreNotBound(t *testing.T) {
	st := testStore(t)
	ss := NewSessionStore(st, NewClient(ClientOptions{BaseUser: "http://127.0.0.1:1"}))
	if _, err := ss.GetAccountSession(context.Background(), 42, ""); !errors.Is(err, ErrNotBound) {
		t.Fatalf("GetAccountSession err = %v, want ErrNotBound", err)
	}
	if _, err := ss.ClientForAccount(context.Background(), 42, ""); !errors.Is(err, ErrNotBound) {
		t.Fatalf("ClientForAccount err = %v, want ErrNotBound", err)
	}
}

func TestClientForAccountBindsSession(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	if err := st.PutCredential(ctx, store.Credential{
		UID: 7, AuthType: "oauth", ClientID: DefaultClientID,
		AccessToken: "at-7", RefreshToken: "rt-7",
		DeviceID:        "0123456789abcdef0123456789abcdef",
		AccessExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}

	ss := NewSessionStore(st, NewClient(ClientOptions{BaseUser: "http://user.test", BaseDrive: "http://drive.test", DeviceID: "base"}))
	c, err := ss.ClientForAccount(ctx, 7, "")
	if err != nil {
		t.Fatalf("ClientForAccount: %v", err)
	}
	if c.Session == nil {
		t.Fatal("Session is nil")
	}
	if c.BaseDrive != "http://drive.test" {
		t.Fatalf("BaseDrive = %q", c.BaseDrive)
	}
	tok, err := c.Session.Token(ctx)
	if err != nil || tok != "at-7" {
		t.Fatalf("Token = %q, %v", tok, err)
	}
}

func TestSessionForceRefresh(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		writeJSONBody(w, http.StatusOK, `{"access_token":"at-`+string(rune('0'+n))+`","refresh_token":"rt-`+string(rune('0'+n))+`","expires_in":43200}`)
	}))
	defer srv.Close()

	st := testStore(t)
	ctx := context.Background()
	if err := st.PutCredential(ctx, store.Credential{
		UID: 1000, AuthType: "oauth", ClientID: DefaultClientID,
		AccessToken: "at-old", RefreshToken: "rt-old",
		DeviceID:        "0123456789abcdef0123456789abcdef",
		AccessExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}

	ss := NewSessionStore(st, NewClient(ClientOptions{BaseUser: srv.URL}))
	sess, err := ss.GetAccountSession(ctx, 1000, "")
	if err != nil {
		t.Fatalf("GetAccountSession: %v", err)
	}
	if tok, _ := sess.Token(ctx); tok != "at-old" {
		t.Fatalf("initial token = %q", tok)
	}
	if err := sess.ForceRefresh(ctx); err != nil {
		t.Fatalf("ForceRefresh: %v", err)
	}
	if tok, _ := sess.Token(ctx); tok != "at-1" {
		t.Fatalf("after force token = %q", tok)
	}
	if err := sess.ForceRefresh(ctx); err != nil {
		t.Fatalf("second ForceRefresh: %v", err)
	}
	if tok, _ := sess.Token(ctx); tok != "at-2" {
		t.Fatalf("after second force token = %q", tok)
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Fatalf("refresh calls = %d, want 2", n)
	}
}

func TestClientForAccountReusesPerUserLimiter(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	for _, uid := range []int{7, 8} {
		if err := st.PutCredential(ctx, store.Credential{
			UID: uid, AuthType: "oauth", ClientID: DefaultClientID,
			AccessToken: "at", RefreshToken: "rt",
			DeviceID:        "0123456789abcdef0123456789abcdef",
			AccessExpiresAt: time.Now().Add(time.Hour),
		}); err != nil {
			t.Fatalf("PutCredential: %v", err)
		}
	}

	ss := NewSessionStore(st, NewClient(ClientOptions{BaseUser: "http://user.test", BaseDrive: "http://drive.test"}))
	c1, err := ss.ClientForAccount(ctx, 7, "")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := ss.ClientForAccount(ctx, 7, "")
	if err != nil {
		t.Fatal(err)
	}
	c3, err := ss.ClientForAccount(ctx, 8, "")
	if err != nil {
		t.Fatal(err)
	}

	if c1.limiter == nil || c1.limiter != c2.limiter {
		t.Fatal("the same user must share one rate limiter")
	}
	if c1.limiter == c3.limiter {
		t.Fatal("different users must not share a rate limiter")
	}
}

func TestDropAccountStopsInFlightRefresh(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		<-release
		writeJSONBody(w, http.StatusOK, `{"access_token":"at-new","refresh_token":"rt-new","expires_in":43200}`)
	}))
	defer srv.Close()

	st := testStore(t)
	ctx := context.Background()
	acct, err := store.NewAccountID()
	if err != nil {
		t.Fatalf("NewAccountID: %v", err)
	}
	if err := st.PutAccount(ctx, store.PikPakAccount{
		ID: acct, UID: 1000, DisplayName: "A", Status: "active", BoundAt: time.Now(),
	}); err != nil {
		t.Fatalf("PutAccount: %v", err)
	}
	if err := st.PutCredential(ctx, store.Credential{
		UID: 1000, AccountID: acct, AuthType: "oauth", ClientID: DefaultClientID,
		AccessToken: "at-old", RefreshToken: "rt-old",
		DeviceID:        "0123456789abcdef0123456789abcdef",
		AccessExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}

	ss := NewSessionStore(st, NewClient(ClientOptions{BaseUser: srv.URL}))
	sess, err := ss.GetAccountSession(ctx, 1000, acct)
	if err != nil {
		t.Fatalf("GetAccountSession: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := sess.Token(ctx)
		done <- err
	}()
	<-started

	if err := ss.DropAccount(ctx, 1000, acct); err != nil {
		t.Fatalf("DropAccount: %v", err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("in-flight refresh should fail after DropAccount")
	}

	// The refresh response must not resurrect the credential.
	if _, err := st.GetAccountCredential(ctx, 1000, acct); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("credential after DropAccount: err=%v, want ErrNotFound", err)
	}
}

func TestSessionStoreTwoAccountsIsolated(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	uid := 1000

	acctA, err := store.NewAccountID()
	if err != nil {
		t.Fatalf("NewAccountID: %v", err)
	}
	acctB, err := store.NewAccountID()
	if err != nil {
		t.Fatalf("NewAccountID: %v", err)
	}
	credA := store.Credential{
		UID: uid, AccountID: acctA, AuthType: "oauth", ClientID: DefaultClientID,
		AccessToken: "at-a", RefreshToken: "rt-a",
		DeviceID:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AccessExpiresAt: time.Now().Add(time.Hour),
	}
	credB := store.Credential{
		UID: uid, AccountID: acctB, AuthType: "oauth", ClientID: DefaultClientID,
		AccessToken: "at-b", RefreshToken: "rt-b",
		DeviceID:        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		AccessExpiresAt: time.Now().Add(time.Hour),
	}

	ss := NewSessionStore(st, NewClient(ClientOptions{BaseUser: "http://127.0.0.1:1"}))
	ss.InstallAccount(uid, acctA, credA)
	ss.InstallAccount(uid, acctB, credB)
	// b's credential is persisted too, the way bind completion leaves it.
	if err := st.PutCredential(ctx, credB); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}
	if !ss.HasAny(ctx, uid) {
		t.Fatal("HasAny = false, want true")
	}

	ca, err := ss.ClientForAccount(ctx, uid, acctA)
	if err != nil {
		t.Fatalf("ClientForAccount(a): %v", err)
	}
	cb, err := ss.ClientForAccount(ctx, uid, acctB)
	if err != nil {
		t.Fatalf("ClientForAccount(b): %v", err)
	}
	if ca.Session == cb.Session {
		t.Fatal("two accounts resolved to the same session")
	}
	if tok, err := ca.Session.Token(ctx); err != nil || tok != "at-a" {
		t.Fatalf("account a token = %q, %v", tok, err)
	}
	if tok, err := cb.Session.Token(ctx); err != nil || tok != "at-b" {
		t.Fatalf("account b token = %q, %v", tok, err)
	}
	if ca.DeviceID != credA.DeviceID {
		t.Fatalf("account a device = %q, want %q", ca.DeviceID, credA.DeviceID)
	}
	if cb.DeviceID != credB.DeviceID {
		t.Fatalf("account b device = %q, want %q", cb.DeviceID, credB.DeviceID)
	}

	// Dropping a must not touch b's session or b's stored credential.
	if err := ss.DropAccount(ctx, uid, acctA); err != nil {
		t.Fatalf("DropAccount(a): %v", err)
	}
	if _, err := ss.GetAccountSession(ctx, uid, acctA); !errors.Is(err, ErrNotBound) {
		t.Fatalf("GetAccountSession(a) after drop = %v, want ErrNotBound", err)
	}
	cb2, err := ss.ClientForAccount(ctx, uid, acctB)
	if err != nil {
		t.Fatalf("ClientForAccount(b) after drop: %v", err)
	}
	if tok, err := cb2.Session.Token(ctx); err != nil || tok != "at-b" {
		t.Fatalf("account b token after drop = %q, %v", tok, err)
	}
	got, err := st.GetAccountCredential(ctx, uid, acctB)
	if err != nil {
		t.Fatalf("GetAccountCredential after drop: %v", err)
	}
	if got.AccountID != acctB || got.RefreshToken != "rt-b" {
		t.Fatalf("surviving credential = %+v, want account b untouched", got)
	}

	// Dropping the last account leaves nothing behind.
	if err := ss.DropAccount(ctx, uid, acctB); err != nil {
		t.Fatalf("DropAccount(b): %v", err)
	}
	if _, err := ss.ClientForAccount(ctx, uid, ""); !errors.Is(err, ErrNotBound) {
		t.Fatalf(`ClientForAccount("") after all drops = %v, want ErrNotBound`, err)
	}
	if ss.HasAny(ctx, uid) {
		t.Fatal("HasAny = true after dropping every account, want false")
	}
}

func TestDropAccountRemovesItsCredentialRow(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	uid := 1000
	acct, err := store.NewAccountID()
	if err != nil {
		t.Fatalf("NewAccountID: %v", err)
	}
	if err := st.PutCredential(ctx, store.Credential{
		UID: uid, AccountID: acct, AuthType: "oauth", ClientID: DefaultClientID,
		AccessToken: "at-1", RefreshToken: "rt-1",
		DeviceID:        "0123456789abcdef0123456789abcdef",
		AccessExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}

	ss := NewSessionStore(st, NewClient(ClientOptions{BaseUser: "http://127.0.0.1:1"}))
	sess, err := ss.GetAccountSession(ctx, uid, acct)
	if err != nil {
		t.Fatalf("GetAccountSession: %v", err)
	}
	if sess.accountID != acct {
		t.Fatalf("session account = %q, want %q", sess.accountID, acct)
	}

	if err := ss.DropAccount(ctx, uid, acct); err != nil {
		t.Fatalf("DropAccount: %v", err)
	}
	// The dropped session is disabled, so an in-flight refresh cannot write
	// the credential back.
	if _, err := sess.Token(ctx); !errors.Is(err, ErrNotBound) {
		t.Fatalf("dropped session Token = %v, want ErrNotBound", err)
	}
	if _, err := st.GetAccountCredential(ctx, uid, acct); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("credential after DropAccount = %v, want ErrNotFound", err)
	}
}

func TestClientForEmptyMeansDefault(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	ss := NewSessionStore(st, NewClient(ClientOptions{BaseUser: "http://127.0.0.1:1"}))
	if _, err := ss.ClientForAccount(ctx, 2000, ""); !errors.Is(err, ErrNotBound) {
		t.Fatalf(`ClientForAccount with no accounts = %v, want ErrNotBound`, err)
	}

	acctOld, err := store.NewAccountID()
	if err != nil {
		t.Fatalf("NewAccountID: %v", err)
	}
	acctActive, err := store.NewAccountID()
	if err != nil {
		t.Fatalf("NewAccountID: %v", err)
	}
	base := time.Now().Add(-time.Minute)
	for _, a := range []store.PikPakAccount{
		{ID: acctOld, UID: 2001, DisplayName: "old", Status: "reauth_required", BoundAt: base},
		{ID: acctActive, UID: 2001, DisplayName: "active", Status: "active", BoundAt: base.Add(time.Second)},
	} {
		if err := st.PutAccount(ctx, a); err != nil {
			t.Fatalf("PutAccount(%s): %v", a.ID, err)
		}
	}
	expiry := time.Now().Add(time.Hour)
	ss.InstallAccount(2001, acctOld, store.Credential{
		UID: 2001, AccountID: acctOld, AuthType: "oauth", ClientID: DefaultClientID,
		AccessToken: "at-reauth", RefreshToken: "rt-reauth",
		DeviceID:        "0123456789abcdef0123456789abcdef",
		AccessExpiresAt: expiry,
	})
	ss.InstallAccount(2001, acctActive, store.Credential{
		UID: 2001, AccountID: acctActive, AuthType: "oauth", ClientID: DefaultClientID,
		AccessToken: "at-active", RefreshToken: "rt-active",
		DeviceID:        "0123456789abcdef0123456789abcdef",
		AccessExpiresAt: expiry,
	})

	// The oldest row is reauth_required, so the default skips to the active one.
	c, err := ss.ClientForAccount(ctx, 2001, "")
	if err != nil {
		t.Fatalf(`ClientForAccount("") default: %v`, err)
	}
	if tok, err := c.Session.Token(ctx); err != nil || tok != "at-active" {
		t.Fatalf("default account token = %q, %v", tok, err)
	}

	// With no active account left, the oldest row wins.
	if err := st.SetAccountStatus(ctx, 2001, acctActive, "reauth_required"); err != nil {
		t.Fatalf("SetAccountStatus: %v", err)
	}
	c, err = ss.ClientForAccount(ctx, 2001, "")
	if err != nil {
		t.Fatalf(`ClientForAccount("") without active: %v`, err)
	}
	if tok, err := c.Session.Token(ctx); err != nil || tok != "at-reauth" {
		t.Fatalf("fallback default token = %q, %v", tok, err)
	}
}

func TestReauthMarksAccountRow(t *testing.T) {
	var refreshCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshCalls, 1)
		writeJSONBody(w, http.StatusBadRequest, `{"error":"invalid_grant","error_code":4126,"error_description":"refresh token invalid"}`)
	}))
	defer srv.Close()

	st := testStore(t)
	ctx := context.Background()
	uid := 1000
	acct, err := store.NewAccountID()
	if err != nil {
		t.Fatalf("NewAccountID: %v", err)
	}
	if err := st.PutAccount(ctx, store.PikPakAccount{
		ID: acct, UID: uid, PikPakSub: "sub-1", DisplayName: "A", Status: "active", BoundAt: time.Now(),
	}); err != nil {
		t.Fatalf("PutAccount: %v", err)
	}
	if err := st.PutCredential(ctx, store.Credential{
		UID: uid, AccountID: acct, AuthType: "oauth", ClientID: DefaultClientID,
		AccessToken: "at-old", RefreshToken: "rt-old",
		DeviceID:        "0123456789abcdef0123456789abcdef",
		AccessExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}

	ss := NewSessionStore(st, NewClient(ClientOptions{BaseUser: srv.URL}))
	sess, err := ss.GetAccountSession(ctx, uid, acct)
	if err != nil {
		t.Fatalf("GetAccountSession: %v", err)
	}
	if sess.accountID != acct {
		t.Fatalf("session account = %q, want %q", sess.accountID, acct)
	}

	if _, err := sess.Token(ctx); !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("Token err = %v, want ErrReauthRequired", err)
	}
	if n := atomic.LoadInt32(&refreshCalls); n != 1 {
		t.Fatalf("refresh calls = %d, want 1 (no retry after reauth)", n)
	}

	// The failure must land on the owning account row, which is where the
	// per-account shell reads the banner state from.
	a, err := st.GetAccount(ctx, uid, acct)
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if a.Status != "reauth_required" {
		t.Fatalf("account status = %q, want reauth_required", a.Status)
	}
}
