package pikpak

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"fnos-pikpak/internal/version"
)

// TokenSource yields a valid access token, refreshing when needed.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// forceRefresher lets the client push back when PikPak rejects a token the
// clock still considers valid. Session implements it.
type forceRefresher interface {
	ForceRefresh(ctx context.Context) error
}

var UserAgent = "pikpak-fnos/" + version.Version

// retryBase is the first backoff step; tests shorten it.
var retryBase = 500 * time.Millisecond

const maxRetries = 3

// APIError is a PikPak error response. PikPak frequently reports failures with
// HTTP 200, so callers must check the decoded envelope, not only the status.
type APIError struct {
	Status      int
	Code        int
	Kind        string
	Description string

	retryAfter time.Duration // -1 when absent
}

func (e *APIError) Error() string {
	if e.Kind != "" {
		return fmt.Sprintf("pikpak: %s (%d, http %d)", e.Kind, e.Code, e.Status)
	}
	if e.Description != "" {
		return fmt.Sprintf("pikpak: %s (http %d)", e.Description, e.Status)
	}
	return fmt.Sprintf("pikpak: api error (http %d)", e.Status)
}

func (e *APIError) needsRefresh() bool {
	switch e.Code {
	case 16, 4121, 4122:
		return true
	}
	return e.Status == http.StatusUnauthorized || e.Kind == "unauthenticated"
}

func (e *APIError) isReauth() bool {
	return e.Code == 4126 || e.Kind == "invalid_grant"
}

func (e *APIError) isRateLimited() bool {
	// Code 11 is the daily offline-task quota: retrying cannot succeed, so it
	// must reach the caller as-is.
	if e.Code == 11 {
		return false
	}
	return e.Status == http.StatusTooManyRequests || e.Code == 10
}

// limiter is a hand-written token bucket so the client does not pull in
// golang.org/x/time for one type.
type limiter struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

const (
	limitRate  = 10.0
	limitBurst = 20.0
)

func newLimiter() *limiter {
	return &limiter{tokens: limitBurst, last: time.Now()}
}

func (l *limiter) wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := time.Now()
		l.tokens = math.Min(limitBurst, l.tokens+now.Sub(l.last).Seconds()*limitRate)
		l.last = now
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}
		need := time.Duration((1 - l.tokens) / limitRate * float64(time.Second))
		l.mu.Unlock()
		select {
		case <-time.After(need):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

type File struct {
	ID           string `json:"id"`
	ParentID     string `json:"parent_id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Size         int64  `json:"size"`
	Phase        string `json:"phase"`
	Category     string `json:"category"`
	MimeType     string `json:"mime_type"`
	ModifiedTime string `json:"modified_time"`
	// ThumbnailLink is a short-lived CDN URL. It stays server-side: the
	// browser asks /api/thumb instead.
	ThumbnailLink string `json:"-"`
}

// UnmarshalJSON accepts size as either a JSON string (what PikPak sends) or a
// number.
func (f *File) UnmarshalJSON(b []byte) error {
	var raw struct {
		ID            string          `json:"id"`
		ParentID      string          `json:"parent_id"`
		Name          string          `json:"name"`
		Kind          string          `json:"kind"`
		Size          json.RawMessage `json:"size"`
		Phase         string          `json:"phase"`
		Category      string          `json:"file_category"`
		MimeType      string          `json:"mime_type"`
		ModifiedTime  string          `json:"modified_time"`
		ThumbnailLink string          `json:"thumbnail_link"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*f = File{
		ID:            raw.ID,
		ParentID:      raw.ParentID,
		Name:          raw.Name,
		Kind:          raw.Kind,
		Size:          parseSize(raw.Size),
		Phase:         raw.Phase,
		Category:      raw.Category,
		MimeType:      raw.MimeType,
		ModifiedTime:  raw.ModifiedTime,
		ThumbnailLink: raw.ThumbnailLink,
	}
	return nil
}

func parseSize(raw json.RawMessage) int64 {
	s := string(bytes.TrimSpace(raw))
	if s == "" || s == "null" {
		return 0
	}
	if s[0] == '"' {
		unquoted, err := strconv.Unquote(s)
		if err != nil {
			return 0
		}
		s = unquoted
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

type ListOptions struct {
	PageToken string
	Limit     int
	Order     string
	Filters   string
}

type FilePage struct {
	Items         []File
	NextPageToken string
}

type About struct {
	QuotaLimit int64
	QuotaUsage int64
	ExpiresAt  string
}

type VIP struct {
	Type   string
	Expire string
}

func (c *Client) ListFiles(ctx context.Context, parentID string, o ListOptions) (FilePage, error) {
	u, err := url.Parse(c.BaseDrive + "/drive/v1/files")
	if err != nil {
		return FilePage{}, err
	}
	q := u.Query()
	if parentID != "" {
		q.Set("parent_id", parentID)
	}
	if o.Limit > 0 {
		q.Set("limit", strconv.Itoa(o.Limit))
	}
	if o.PageToken != "" {
		q.Set("page_token", o.PageToken)
	}
	if o.Order != "" {
		q.Set("order", o.Order)
	}
	if o.Filters != "" {
		q.Set("filters", o.Filters)
	}
	q.Set("thumbnail_size", "SIZE_LARGE")
	u.RawQuery = q.Encode()

	var raw struct {
		Files         []File `json:"files"`
		NextPageToken string `json:"next_page_token"`
	}
	if err := c.do(ctx, http.MethodGet, u.String(), nil, &raw); err != nil {
		return FilePage{}, err
	}
	return FilePage{Items: raw.Files, NextPageToken: raw.NextPageToken}, nil
}

// ListAllFiles follows page tokens until parentID is exhausted. Preparation
// walks folders to find the oldest videos and to notice a freshly restored
// file, and both need the whole folder, not its first page. The page cap
// keeps a misbehaving upstream from looping forever.
func (c *Client) ListAllFiles(ctx context.Context, parentID string) ([]File, error) {
	const maxPages = 50
	out := []File{}
	pageToken := ""
	for i := 0; i < maxPages; i++ {
		page, err := c.ListFiles(ctx, parentID, ListOptions{PageToken: pageToken, Limit: 100})
		if err != nil {
			return nil, err
		}
		out = append(out, page.Items...)
		if page.NextPageToken == "" {
			return out, nil
		}
		pageToken = page.NextPageToken
	}
	return out, nil
}

func (c *Client) About(ctx context.Context) (About, error) {
	var raw struct {
		Quota struct {
			Limit string `json:"limit"`
			Usage string `json:"usage"`
		} `json:"quota"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := c.do(ctx, http.MethodGet, c.BaseDrive+"/drive/v1/about", nil, &raw); err != nil {
		return About{}, err
	}
	return About{
		QuotaLimit: parseSize([]byte(strconv.Quote(raw.Quota.Limit))),
		QuotaUsage: parseSize([]byte(strconv.Quote(raw.Quota.Usage))),
		ExpiresAt:  raw.ExpiresAt,
	}, nil
}

func (c *Client) VIP(ctx context.Context) (VIP, error) {
	var raw struct {
		Data struct {
			Type   string `json:"type"`
			Expire string `json:"expire"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, c.BaseDrive+"/drive/v1/privilege/vip", nil, &raw); err != nil {
		return VIP{}, err
	}
	return VIP{Type: raw.Data.Type, Expire: raw.Data.Expire}, nil
}

func (c *Client) Events(ctx context.Context, pageToken string) (FilePage, error) {
	u, err := url.Parse(c.BaseDrive + "/drive/v1/events")
	if err != nil {
		return FilePage{}, err
	}
	q := u.Query()
	q.Set("limit", "100")
	q.Set("thumbnail_size", "SIZE_MEDIUM")
	if pageToken != "" {
		q.Set("next_page_token", pageToken)
	}
	u.RawQuery = q.Encode()

	var raw struct {
		Files []File `json:"files"`
		// Events is the documented shape; each entry embeds the file.
		Events []struct {
			File File `json:"file"`
		} `json:"events"`
		NextPageToken string `json:"next_page_token"`
	}
	if err := c.do(ctx, http.MethodGet, u.String(), nil, &raw); err != nil {
		return FilePage{}, err
	}
	items := raw.Files
	if len(items) == 0 {
		for _, e := range raw.Events {
			if e.File.ID != "" {
				items = append(items, e.File)
			}
		}
	}
	return FilePage{Items: items, NextPageToken: raw.NextPageToken}, nil
}

// do performs one logical request with token refresh and retries.
func (c *Client) do(ctx context.Context, method, rawURL string, body, out any) error {
	if c.Session == nil {
		return ErrNotBound
	}

	refreshed := false
	retries := 0
	backoff := retryBase

	for {
		if c.limiter != nil {
			if err := c.limiter.wait(ctx); err != nil {
				return err
			}
		}
		token, err := c.Session.Token(ctx)
		if err != nil {
			return err
		}

		resp, raw, err := c.exchange(ctx, method, rawURL, body, token)
		if err != nil {
			return err
		}

		apiErr := parseAPIError(resp.StatusCode, raw, resp.Header)
		if apiErr == nil {
			if out != nil && len(raw) > 0 {
				if err := json.Unmarshal(raw, out); err != nil {
					return fmt.Errorf("pikpak: decode %s: %w", rawURL, err)
				}
			}
			return nil
		}

		switch {
		case apiErr.needsRefresh():
			if refreshed {
				return ErrReauthRequired
			}
			if err := c.forceRefresh(ctx); err != nil {
				return err
			}
			refreshed = true
			continue

		case apiErr.isReauth():
			return ErrReauthRequired

		case apiErr.isRateLimited() || apiErr.Status >= 500:
			if retries >= maxRetries {
				return apiErr
			}
			retries++
			delay := backoff
			if apiErr.retryAfter >= 0 {
				delay = apiErr.retryAfter
			}
			backoff *= 2
			if backoff > 2*time.Second {
				backoff = 2 * time.Second
			}
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue

		default:
			return apiErr
		}
	}
}

func (c *Client) forceRefresh(ctx context.Context) error {
	if f, ok := c.Session.(forceRefresher); ok {
		return f.ForceRefresh(ctx)
	}
	return ErrReauthRequired
}

func (c *Client) exchange(ctx context.Context, method, rawURL string, body any, token string) (*http.Response, []byte, error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
	if err != nil {
		return nil, nil, err
	}
	c.setHeaders(req)
	req.Header.Set("User-Agent", UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, nil, err
	}
	return resp, raw, nil
}

// getRaw returns the status and body without classification, for probes that
// must report upstream behavior rather than translate it.
func (c *Client) getRaw(ctx context.Context, rawURL string) (int, []byte, error) {
	if c.Session == nil {
		return 0, nil, ErrNotBound
	}
	if c.limiter != nil {
		if err := c.limiter.wait(ctx); err != nil {
			return 0, nil, err
		}
	}
	token, err := c.Session.Token(ctx)
	if err != nil {
		return 0, nil, err
	}
	resp, raw, err := c.exchange(ctx, http.MethodGet, rawURL, nil, token)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, raw, nil
}

// parseAPIError turns a response into an APIError, or nil when it is a success.
func parseAPIError(status int, raw []byte, header http.Header) *APIError {
	var env struct {
		Error            string `json:"error"`
		ErrorCode        int    `json:"error_code"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(raw, &env)
	if env.Error == "" && env.ErrorCode == 0 && status < http.StatusBadRequest {
		return nil
	}
	e := &APIError{
		Status:      status,
		Code:        env.ErrorCode,
		Kind:        env.Error,
		Description: env.ErrorDescription,
		retryAfter:  -1,
	}
	if status >= http.StatusBadRequest && env.Error == "" {
		e.Description = truncate(string(raw), 200)
	}
	if header != nil {
		if ra := header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil {
				e.retryAfter = time.Duration(secs) * time.Second
			}
		}
	}
	return e
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
