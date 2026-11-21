package pikpak

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	ErrorDescription        string `json:"error_description"`
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	Sub              string `json:"sub"`
	Error            string `json:"error"`
	ErrorCode        int    `json:"error_code"`
	ErrorDescription string `json:"error_description"`
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

// Session is one fnOS user's PikPak session. It refreshes the access token at
// most once across concurrent callers.
type Session struct {
	client *Client
	st     *store.Store
	uid    int

	mu           sync.Mutex
	access       string
	refreshToken string
	expiresAt    time.Time
	refreshing   chan struct{}
	reauth       bool
	disabled     bool
}

func NewSession(c *Client, st *store.Store, uid int, cred store.Credential) *Session {
	return &Session{
		client:       c,
		st:           st,
		uid:          uid,
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

func (s *Session) ReauthRequired() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reauth
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
		s.markReauth()
		return ErrReauthRequired
	}

	tok, err := s.client.Refresh(ctx, rt)
	if err != nil {
		if errors.Is(err, ErrReauthRequired) || errors.Is(err, ErrAccessDenied) {
			s.markReauth()
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
// writing credentials back after a logout.
func (s *Session) disable() {
	s.mu.Lock()
	s.disabled = true
	s.mu.Unlock()
}

func (s *Session) markReauth() {
	s.mu.Lock()
	s.reauth = true
	s.mu.Unlock()
}

// SessionStore owns one Session per fnOS uid for the life of the process.
type SessionStore struct {
	st     *store.Store
	client *Client

	mu       sync.Mutex
	sessions map[int]*Session
	limiters map[int]*limiter
}

func NewSessionStore(st *store.Store, client *Client) *SessionStore {
	return &SessionStore{
		st:       st,
		client:   client,
		sessions: map[int]*Session{},
		limiters: map[int]*limiter{},
	}
}

// Client returns the shared base configuration. It carries no session and
// must not be used for authenticated calls.
func (s *SessionStore) Client() *Client {
	return s.client
}

func (s *SessionStore) Get(ctx context.Context, uid int) (*Session, error) {
	// Held across the DB read so a concurrent Drop (same lock) cannot be
	// undone by a stale session being cached afterwards.
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[uid]; ok {
		return sess, nil
	}

	cred, err := s.st.GetCredential(ctx, uid)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrNotBound
	}
	if err != nil {
		return nil, err
	}

	sess := NewSession(sessionClient(s.client, cred), s.st, uid, cred)
	s.sessions[uid] = sess
	return sess, nil
}

// ClientFor returns a Client bound to the uid's session, ready for
// authenticated PikPak calls.
func (s *SessionStore) ClientFor(ctx context.Context, uid int) (*Client, error) {
	sess, err := s.Get(ctx, uid)
	if err != nil {
		return nil, err
	}
	c := *s.client
	c.DeviceID = sess.client.DeviceID
	c.Session = sess
	// One bucket per user, reused across every client built for that user.
	c.limiter = s.limiterFor(uid)
	return &c, nil
}

func (s *SessionStore) limiterFor(uid int) *limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.limiters[uid]
	if !ok {
		l = newLimiter()
		s.limiters[uid] = l
	}
	return l
}

// Install caches a session created right after a successful device flow.
func (s *SessionStore) Install(uid int, cred store.Credential) *Session {
	sess := NewSession(sessionClient(s.client, cred), s.st, uid, cred)
	s.mu.Lock()
	s.sessions[uid] = sess
	s.mu.Unlock()
	return sess
}

// Drop forgets the uid's session and deletes the stored credential. The
// session is disabled first, so an in-flight refresh cannot write it back, and
// pending authorization flows are cancelled so a late poller cannot rebind.
func (s *SessionStore) Drop(ctx context.Context, uid int) error {
	s.mu.Lock()
	sess := s.sessions[uid]
	delete(s.sessions, uid)
	err := s.st.DeleteCredential(ctx, uid)
	if err == nil {
		err = s.st.CancelPendingFlows(ctx, uid)
	}
	s.mu.Unlock()

	if sess != nil {
		sess.disable()
	}
	return err
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
