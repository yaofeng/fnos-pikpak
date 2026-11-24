package pikpak

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"fnos-pikpak/internal/store"
)

var (
	ErrAuthorizationPending = errors.New("pikpak: authorization pending")
	ErrSlowDown             = errors.New("pikpak: slow down")
	ErrAccessDenied         = errors.New("pikpak: access denied")
	ErrExpiredToken         = errors.New("pikpak: device code expired")
	ErrReauthRequired       = errors.New("pikpak: re-authorization required")
	ErrNotBound             = errors.New("pikpak: user not bound")
)

// refreshSkew renews the access token before it would expire mid-request.
const refreshSkew = 10 * time.Minute

const deviceCodeGrant = "urn:ietf:params:oauth:grant-type:device_code"

type DeviceFlow struct {
	DeviceCode string
	UserCode   string
	VerifyURL  string
	Interval   int
	ExpiresIn  int
}

type Token struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	Sub          string
}

type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
	VerificationURL         string `json:"verification_url"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	Error                   string `json:"error"`
	ErrorCode               int    `json:"error_code"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Sub          string `json:"sub"`
	Error        string `json:"error"`
	ErrorCode    int    `json:"error_code"`
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US")
	req.Header.Set("X-Client-Id", c.ClientID)
	req.Header.Set("X-Client-Version", c.ClientVersion)
	if c.DeviceID != "" {
		req.Header.Set("X-Device-Id", c.DeviceID)
	}
	if c.BaseUser != "" {
		req.Header.Set("Referer", c.BaseUser+"/v1")
	}
}

func (c *Client) postJSON(ctx context.Context, url string, body any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	return c.HTTP.Do(req)
}

func (c *Client) StartDeviceFlow(ctx context.Context) (DeviceFlow, error) {
	resp, err := c.postJSON(ctx, c.BaseUser+"/v1/auth/device/code", map[string]any{
		"client_id": c.ClientID,
		"scope":     Scope,
	})
	if err != nil {
		return DeviceFlow{}, err
	}
	defer resp.Body.Close()

	var out deviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return DeviceFlow{}, fmt.Errorf("pikpak: decode device code: %w", err)
	}
	if out.Error != "" {
		return DeviceFlow{}, fmt.Errorf("pikpak: device code: %s (%d)", out.Error, out.ErrorCode)
	}
	if out.DeviceCode == "" {
		return DeviceFlow{}, fmt.Errorf("pikpak: device code response missing device_code (http %d)", resp.StatusCode)
	}
	verify := out.VerificationURIComplete
	if verify == "" {
		verify = out.VerificationURL
	}
	return DeviceFlow{
		DeviceCode: out.DeviceCode,
		UserCode:   out.UserCode,
		VerifyURL:  verify,
		Interval:   out.Interval,
		ExpiresIn:  out.ExpiresIn,
	}, nil
}

func (c *Client) PollDeviceToken(ctx context.Context, deviceCode string) (Token, error) {
	return c.tokenRequest(ctx, map[string]any{
		"client_id":   c.ClientID,
		"grant_type":  deviceCodeGrant,
		"device_code": deviceCode,
	})
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	return c.tokenRequest(ctx, map[string]any{
		"client_id":     c.ClientID,
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
	})
}

func (c *Client) tokenRequest(ctx context.Context, payload map[string]any) (Token, error) {
	resp, err := c.postJSON(ctx, c.BaseUser+"/v1/auth/token", payload)
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()

	var out tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Token{}, fmt.Errorf("pikpak: decode token: %w", err)
	}
	if out.Error != "" || out.AccessToken == "" {
		if out.Error == "" {
			return Token{}, fmt.Errorf("pikpak: token response missing access_token (http %d)", resp.StatusCode)
		}
		return Token{}, mapTokenError(out.Error)
	}
	return Token{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		ExpiresIn:    out.ExpiresIn,
		Sub:          out.Sub,
	}, nil
}

func mapTokenError(code string) error {
	switch code {
	case "authorization_pending":
		return ErrAuthorizationPending
	case "slow_down":
		return ErrSlowDown
	case "access_denied":
		return ErrAccessDenied
	case "expired_token":
		return ErrExpiredToken
	case "invalid_grant":
		return ErrReauthRequired
	default:
		return fmt.Errorf("pikpak: token error: %s", code)
	}
}

// Session is one fnOS user's PikPak session for one account. It refreshes the
// access token at most once across concurrent callers. accountID is empty for
// sessions created before accounts existed (legacy path).
type Session struct {
	client    *Client
	st        *store.Store
	uid       int
	accountID string

	mu           sync.Mutex
	access       string
	refreshToken string
	expiresAt    time.Time
	refreshing   chan struct{}
	reauth       bool
	disabled     bool
}

func NewSession(c *Client, st *store.Store, uid int, accountID string, cred store.Credential) *Session {
	return &Session{
		client:       c,
		st:           st,
		uid:          uid,
		accountID:    accountID,
		access:       cred.AccessToken,
		refreshToken: cred.RefreshToken,
		expiresAt:    cred.AccessExpiresAt,
	}
}

// Token returns a usable access token, refreshing it if it is close to
// expiry. Concurrent callers share one refresh.
func (s *Session) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	for {
		if s.disabled {
			s.mu.Unlock()
			return "", ErrNotBound
		}
		if s.reauth {
			s.mu.Unlock()
			return "", ErrReauthRequired
		}
		if s.access != "" && !s.expiresAt.IsZero() && time.Now().Before(s.expiresAt.Add(-refreshSkew)) {
			tok := s.access
			s.mu.Unlock()
			return tok, nil
		}
		if s.refreshing != nil {
			wait := s.refreshing
			s.mu.Unlock()
			select {
			case <-wait:
				s.mu.Lock()
				continue
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		done := make(chan struct{})
		s.refreshing = done
		s.mu.Unlock()

		err := s.refreshOnce(ctx)

		s.mu.Lock()
		s.refreshing = nil
		close(done)
		if err != nil {
			s.mu.Unlock()
			return "", err
		}
	}
}

// ForceRefresh discards the cached access token and renews it. The client
// calls it when PikPak rejects a token the clock still considers valid.
func (s *Session) ForceRefresh(ctx context.Context) error {
	s.mu.Lock()
	if s.reauth {
		s.mu.Unlock()
		return ErrReauthRequired
	}
	s.access = ""
	s.mu.Unlock()

	_, err := s.Token(ctx)
	return err
}

func (s *Session) refreshOnce(ctx context.Context) error {
	s.mu.Lock()
	rt := s.refreshToken
	s.mu.Unlock()
	if rt == "" {
		s.markReauth(ctx)
		return ErrReauthRequired
	}

	tok, err := s.client.Refresh(ctx, rt)
	if err != nil {
		if errors.Is(err, ErrReauthRequired) || errors.Is(err, ErrAccessDenied) {
			s.markReauth(ctx)
		}
		return err
	}

	expiresAt := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)

	// Hold the session lock through persistence so Drop (which disables the
	// session under the same lock) cannot interleave and be undone.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disabled {
		return ErrNotBound
	}
	if s.st != nil {
		// Persist the rotated refresh token before the new access token is
		// used; losing the rotation would strand the session.
		cred := store.Credential{
			UID:             s.uid,
			AccountID:       s.accountID,
			AuthType:        "oauth",
			ClientID:        s.client.ClientID,
			AccessToken:     tok.AccessToken,
			RefreshToken:    tok.RefreshToken,
			DeviceID:        s.client.DeviceID,
			AccessExpiresAt: expiresAt,
			Scopes:          Scope,
		}
		if err := s.st.PutCredential(ctx, cred); err != nil {
			return err
		}
	}

	s.access = tok.AccessToken
	s.refreshToken = tok.RefreshToken
	s.expiresAt = expiresAt
	return nil
}

// disable makes the session unusable and prevents any in-flight refresh from
// writing credentials back after a logout. The flag is checked under sess.mu
// (Token, refreshOnce), so it must be written under it too; callers hold
// SessionStore.mu first — nothing ever takes SessionStore.mu while holding
// sess.mu, which fixes the lock order SessionStore.mu → sess.mu.
func (s *Session) disable() {
	s.mu.Lock()
	s.disabled = true
	s.mu.Unlock()
}

// markReauth latches the session as needing reauthorization and records that
// on the owning account row, which is where the shell reads the banner state
// from. Sessions that predate account rows keep writing the users row.
func (s *Session) markReauth(ctx context.Context) {
	s.mu.Lock()
	s.reauth = true
	st, uid, accountID := s.st, s.uid, s.accountID
	s.mu.Unlock()
	if st == nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if accountID != "" {
		_ = st.SetAccountStatus(writeCtx, uid, accountID, "reauth_required")
		return
	}
	_ = st.SetUserStatus(writeCtx, uid, "reauth_required")
}

// sessionKey namespaces the session and limiter maps by (uid, account): one
// fnOS user can hold several PikPak identities side by side. The "uid:" prefix
// of the store's placeholder credential key can never collide with the "|" in
// the middle.
func sessionKey(uid int, accountID string) string {
	return strconv.Itoa(uid) + "|" + accountID
}

// legacyAccountID maps the store's pre-account placeholder key ("uid:<n>",
// written by bind completion before routes create account rows) back to the
// legacy empty account id, so sessions — and everything derived from them,
// like persisted refreshes — carry real account ids or nothing at all.
func legacyAccountID(accountID string) string {
	if strings.HasPrefix(accountID, "uid:") {
		return ""
	}
	return accountID
}

// SessionStore owns one Session per (fnOS uid, PikPak account) for the life of
// the process.
type SessionStore struct {
	st     *store.Store
	client *Client

	mu       sync.Mutex
	sessions map[string]*Session
	limiters map[string]*limiter
}

func NewSessionStore(st *store.Store, client *Client) *SessionStore {
	return &SessionStore{
		st:       st,
		client:   client,
		sessions: map[string]*Session{},
		limiters: map[string]*limiter{},
	}
}

// Client returns the shared base configuration. It carries no session and
// must not be used for authenticated calls.
func (s *SessionStore) Client() *Client {
	return s.client
}

// Get returns the session of the user's default account. Kept as a thin
// wrapper until every caller knows about accounts; torn down with the rest of
// the legacy surface.
func (s *SessionStore) Get(ctx context.Context, uid int) (*Session, error) {
	return s.GetAccountSession(ctx, uid, "")
}

// GetAccountSession returns the cached session for (uid, accountID), loading
// it from the store on first use. An empty accountID resolves the user's
// default account: the first active row in bind order, else the oldest row.
// The store lock is held across the DB reads so a concurrent Drop cannot be
// undone by a stale session being cached afterwards.
func (s *SessionStore) GetAccountSession(ctx context.Context, uid int, accountID string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if accountID == "" {
		// A session installed without an account id (bind completion before
		// the routes grow account handling) is the credential the user last
		// authenticated with — keep serving it, like the pre-account Get did.
		if sess, ok := s.sessions[sessionKey(uid, "")]; ok {
			return sess, nil
		}
		id, err := s.defaultAccountID(ctx, uid)
		if err != nil {
			return nil, err
		}
		accountID = id
	}

	key := sessionKey(uid, accountID)
	if sess, ok := s.sessions[key]; ok {
		return sess, nil
	}

	cred, err := s.accountCredential(ctx, uid, accountID)
	if err != nil {
		return nil, err
	}
	sess := NewSession(sessionClient(s.client, cred), s.st, uid, accountID, cred)
	s.sessions[key] = sess
	return sess, nil
}

// defaultAccountID picks the account a bare uid resolves to: the first active
// row in bind order, else the oldest row. A user with no account rows at all
// is either mid-migration or bound before accounts existed; the stored
// credential's own account id keeps the legacy wrappers resolving for them.
func (s *SessionStore) defaultAccountID(ctx context.Context, uid int) (string, error) {
	accounts, err := s.st.ListAccounts(ctx, uid)
	if err != nil {
		return "", err
	}
	oldest := ""
	for _, a := range accounts {
		if oldest == "" {
			oldest = a.ID
		}
		if a.Status == "active" {
			return a.ID, nil
		}
	}
	if oldest != "" {
		return oldest, nil
	}

	cred, err := s.st.GetCredential(ctx, uid)
	if errors.Is(err, store.ErrNotFound) {
		return "", ErrNotBound
	}
	if err != nil {
		return "", err
	}
	return legacyAccountID(cred.AccountID), nil
}

// accountCredential loads the stored credential of one account. An empty
// accountID selects the pre-account placeholder row (see store.credentialKey).
// The store resolves the exact per-account row, so two accounts of one user
// can never read each other's credentials.
func (s *SessionStore) accountCredential(ctx context.Context, uid int, accountID string) (store.Credential, error) {
	cred, err := s.st.GetAccountCredential(ctx, uid, accountID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Credential{}, ErrNotBound
	}
	if err != nil {
		return store.Credential{}, err
	}
	return cred, nil
}

// ClientFor returns a Client bound to the user's default account. Kept as a
// thin wrapper until every caller knows about accounts.
func (s *SessionStore) ClientFor(ctx context.Context, uid int) (*Client, error) {
	return s.ClientForAccount(ctx, uid, "")
}

// ClientForAccount returns a Client bound to one account's session, ready for
// authenticated PikPak calls. An empty accountID selects the default account.
func (s *SessionStore) ClientForAccount(ctx context.Context, uid int, accountID string) (*Client, error) {
	sess, err := s.GetAccountSession(ctx, uid, accountID)
	if err != nil {
		return nil, err
	}
	c := *s.client
	c.DeviceID = sess.client.DeviceID
	c.Session = sess
	// One bucket per account, reused across every client built for it.
	c.limiter = s.limiterFor(sessionKey(uid, sess.accountID))
	return &c, nil
}

func (s *SessionStore) limiterFor(key string) *limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.limiters[key]
	if !ok {
		l = newLimiter()
		s.limiters[key] = l
	}
	return l
}

// Install caches a session created right after a successful device flow.
// Kept as a wrapper until bind completion learns about accounts.
func (s *SessionStore) Install(uid int, cred store.Credential) *Session {
	return s.InstallAccount(uid, cred.AccountID, cred)
}

// InstallAccount caches a session created right after a successful device
// flow for one account.
func (s *SessionStore) InstallAccount(uid int, accountID string, cred store.Credential) *Session {
	sess := NewSession(sessionClient(s.client, cred), s.st, uid, accountID, cred)
	s.mu.Lock()
	s.sessions[sessionKey(uid, accountID)] = sess
	s.mu.Unlock()
	return sess
}

// Drop forgets everything the user has bound: each account's session,
// credential and pending flows, plus anything stored without an account row.
// Kept as a wrapper until logout learns about accounts.
func (s *SessionStore) Drop(ctx context.Context, uid int) error {
	accounts, err := s.st.ListAccounts(ctx, uid)
	if err != nil {
		return err
	}
	for _, a := range accounts {
		if err := s.DropAccount(ctx, uid, a.ID); err != nil {
			return err
		}
	}

	// Credentials and flows without an account row (binds completed before
	// the routes grew account handling) are cleaned up wholesale. Sessions
	// are disabled before the store writes — under sess.mu via disable, the
	// same ordering as DropAccount — so an in-flight refresh cannot
	// interleave and write the credential back.
	s.mu.Lock()
	prefix := sessionKey(uid, "")
	var doomed []*Session
	for key, sess := range s.sessions {
		if strings.HasPrefix(key, prefix) {
			doomed = append(doomed, sess)
			delete(s.sessions, key)
		}
	}
	for _, sess := range doomed {
		sess.disable()
	}
	err = s.st.DeleteCredential(ctx, uid)
	if err == nil {
		err = s.st.CancelAccountFlows(ctx, uid, "")
	}
	s.mu.Unlock()
	return err
}

// DropAccount forgets one account's session, deletes its stored credential
// and cancels its still-pending authorization flows, so a late poller cannot
// rebind the account. The session is disabled before the store writes, so an
// in-flight refresh cannot resurrect the credential afterwards.
func (s *SessionStore) DropAccount(ctx context.Context, uid int, accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := sessionKey(uid, accountID)
	sess := s.sessions[key]
	delete(s.sessions, key)
	if sess != nil {
		sess.disable()
	}

	if err := s.dropAccountCredential(ctx, uid, accountID); err != nil {
		return err
	}
	return s.st.CancelAccountFlows(ctx, uid, accountID)
}

// dropAccountCredential deletes the account's stored credential, exactly one
// row: the sibling accounts of the same user keep theirs.
func (s *SessionStore) dropAccountCredential(ctx context.Context, uid int, accountID string) error {
	return s.st.DeleteAccountCredential(ctx, uid, accountID)
}

// HasAny reports whether the user holds any PikPak identity: an account row
// or a stored credential. Store errors count as "nothing" — callers use this
// to pick between UI states, not to gate writes.
func (s *SessionStore) HasAny(ctx context.Context, uid int) bool {
	accounts, err := s.st.ListAccounts(ctx, uid)
	if err == nil && len(accounts) > 0 {
		return true
	}
	_, err = s.st.GetCredential(ctx, uid)
	return err == nil
}

// sessionClient copies the base client and pins it to the user's device id,
// so refreshes carry the same device as the original bind.
func sessionClient(base *Client, cred store.Credential) *Client {
	c := *base
	if cred.DeviceID != "" {
		c.DeviceID = cred.DeviceID
	}
	return &c
}

// TrimFlowError keeps an upstream error string safe to show and store.
func TrimFlowError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if i := strings.IndexAny(msg, "\n\r"); i >= 0 {
		msg = msg[:i]
	}
	return msg
}
