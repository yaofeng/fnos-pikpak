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
	sess, err := ss.Get(ctx, 1000)
	if err != nil {
		t.Fatalf("Get: %v", err)
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

	cred, err := st.GetCredential(ctx, 1000)
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
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
	sess, err := ss.Get(ctx, 1000)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if _, err := sess.Token(ctx); !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("Token err = %v, want ErrReauthRequired", err)
	}
	if !sess.ReauthRequired() {
		t.Fatal("ReauthRequired() = false")
	}
	if _, err := sess.Token(ctx); !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("second Token err = %v, want ErrReauthRequired", err)
	}
	if n := atomic.LoadInt32(&refreshCalls); n != 1 {
		t.Fatalf("refresh calls = %d, want 1 (no retry after reauth)", n)
	}
}

func TestSessionStoreNotBound(t *testing.T) {
	st := testStore(t)
	ss := NewSessionStore(st, NewClient(ClientOptions{BaseUser: "http://127.0.0.1:1"}))
	if _, err := ss.Get(context.Background(), 42); !errors.Is(err, ErrNotBound) {
		t.Fatalf("Get err = %v, want ErrNotBound", err)
	}
	if _, err := ss.ClientFor(context.Background(), 42); !errors.Is(err, ErrNotBound) {
		t.Fatalf("ClientFor err = %v, want ErrNotBound", err)
	}
}

func TestClientForBindsSession(t *testing.T) {
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
	c, err := ss.ClientFor(ctx, 7)
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
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
