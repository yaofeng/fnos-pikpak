// Package websearch asks a search engine for pages on one site. PikPak has no
// search endpoint of its own, so 全网搜索 is a site-scoped query: the person
// types a keyword and the site filter is added here, never in the UI.
package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Site is the domain a search starts on.
const Site = "mypikpak.com"

// shareSites are the hosts PikPak actually serves share pages from, in the
// order they are tried. A search asks one of them at a time and keeps asking
// while its page is short: Google's index for a single host often holds only
// a few share pages for a keyword, and a page of three is not a page. Results
// are accepted from any of them, since the same share is served under every
// host. mypikpak.me no longer resolves, so it goes last.
var shareSites = []string{Site, "mypikpak.net", "pikpak.me", "mypikpak.me"}

// defaultEndpoint is Serper's Google proxy: it honors the site: operator and
// returns organic results as JSON.
const defaultEndpoint = "https://google.serper.dev/search"

// MaxPage is the last page a provider will serve; each page is one paid query.
const MaxPage = 10

// pageSize is larger than a Google page so the site filter still leaves a
// full screen of hits; Serper bills the request, not the result count.
const pageSize = 20

// ErrEmptyQuery means there was nothing to search for.
var ErrEmptyQuery = errors.New("websearch: empty query")

// UpstreamError is a provider refusal, carrying only its status so callers can
// tell a bad key (401/403) from an outage without reading the body.
type UpstreamError struct {
	Status int
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("websearch: upstream status %d", e.Status)
}

// Result is one organic hit.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// Client talks to one search provider. HTTP is injectable so tests can point
// it at a local server.
type Client struct {
	Endpoint string
	HTTP     *http.Client
}

// New returns the production client.
func New() *Client {
	return &Client{Endpoint: defaultEndpoint, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

type serperResult struct {
	Title   string `json:"title"`
	Link    string `json:"link"`
	Snippet string `json:"snippet"`
}

type serperResponse struct {
	Organic []serperResult `json:"organic"`
}

// Search looks for keyword under Site. Only results hosted on the site are
// returned; a provider that ignores the site filter therefore cannot put an
// unrelated page in the list. page is 1-based; callers validate its range.
//
// Sites are asked one at a time. Google's index for a single share domain is
// often sparse — a page of ten holds a handful of share pages — so the hosts
// after it are asked too until the page is full. A host that cannot be
// reached (mypikpak.me no longer resolves) is skipped; a provider refusal
// stops the search, since no other host would answer either. The same share
// is served by every host under one share id, so it is returned once.
func (c *Client) Search(ctx context.Context, apiKey, keyword string, page int) ([]Result, error) {
	q := normalizeKeyword(keyword)
	if q == "" {
		return nil, ErrEmptyQuery
	}
	if page < 1 {
		page = 1
	}
	out := make([]Result, 0, pageSize)
	seen := make(map[string]bool, pageSize)
	var unreachable error
	for _, site := range shareSites {
		if len(out) >= pageSize {
			break
		}
		results, err := c.searchSite(ctx, apiKey, site, q, page)
		if err != nil {
			var upstream *UpstreamError
			if errors.As(err, &upstream) {
				return nil, err
			}
			// The host could not be reached; the next one may answer.
			unreachable = err
			continue
		}
		for _, r := range results {
			key := shareKey(r.URL)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, r)
			if len(out) >= pageSize {
				break
			}
		}
	}
	if len(out) == 0 && unreachable != nil {
		return nil, unreachable
	}
	return out, nil
}

// shareKey is the share a result points at. Every PikPak host serves a share
// at /s/<id>, sometimes followed by the path inside it, so two hosts serving
// the same share collapse to one key.
func shareKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "s" && parts[1] != "" {
		return parts[1]
	}
	return raw
}

// searchSite runs one site-scoped query and keeps only results on a PikPak
// share host.
func (c *Client) searchSite(ctx context.Context, apiKey, site, q string, page int) ([]Result, error) {
	payload, err := json.Marshal(map[string]any{
		"q":    "site:" + site + " " + q,
		"gl":   "cn",
		"hl":   "zh-cn",
		"num":  pageSize,
		"page": page,
	})
	if err != nil {
		return nil, err
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", apiKey)

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("websearch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// The status is the whole story; the body and the key stay out of it.
		return nil, &UpstreamError{Status: resp.StatusCode}
	}
	var parsed serperResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("websearch: decode response: %w", err)
	}

	out := make([]Result, 0, len(parsed.Organic))
	for _, r := range parsed.Organic {
		if !onSite(r.Link) {
			continue
		}
		out = append(out, Result{Title: r.Title, URL: r.Link, Snippet: r.Snippet})
	}
	return out, nil
}

// normalizeKeyword trims the keyword and drops a leading site: operator. The
// filter is this package's job, so a pasted "site://mypikpak.com/" must not
// stack a second, conflicting one on the same query.
func normalizeKeyword(keyword string) string {
	q := strings.TrimSpace(keyword)
	for strings.HasPrefix(strings.ToLower(q), "site:") {
		rest := q[len("site:"):]
		if i := strings.IndexAny(rest, " \t\r\n"); i >= 0 {
			q = strings.TrimSpace(rest[i+1:])
		} else {
			// The whole keyword was a bare site: expression.
			return ""
		}
	}
	return q
}

// onSite reports whether raw points at one of PikPak's share hosts or a
// subdomain of one.
func onSite(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, site := range shareSites {
		if host == site || strings.HasSuffix(host, "."+site) {
			return true
		}
	}
	return false
}
