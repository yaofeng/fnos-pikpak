// Package websearch asks a search engine for pages on one site. PikPak has no
// search endpoint of its own, so 全网搜索 is a site-scoped query: the person
// types a keyword and the site filter is added here, never in the UI.
package websearch

import (
	"bytes"
	"context"
	"encoding/base64"
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

// Scope decides what a search keeps.
type Scope string

const (
	// ScopeShare keeps only share pages, the ones the app can open.
	ScopeShare Scope = "share"
	// ScopeSite keeps every hit the provider returned, including pages off
	// the PikPak hosts; share pages carry Share=true so the list can mark them.
	ScopeSite Scope = "all"
)

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
	// Share reports whether this is a share page the app can open in its own
	// browser. It is only ever false when the scope allows other pages.
	Share bool `json:"share"`
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

// Answer is one page of results and what the provider returned for it.
type Answer struct {
	Results []Result
	// Raw counts the distinct hits the provider returned for this page across
	// the hosts asked, before the share filter dropped any. A short list with
	// a large Raw means the provider answered with hits that are not shares.
	Raw int
}

// Search looks for keyword under Site. Scope decides what is kept: only share
// pages (the default), or every hit the provider returned with the shares
// marked. page is 1-based and callers validate its range.
//
// Sites are asked one at a time. Google's index for a single share domain is
// often sparse — a page of ten holds a handful of share pages — so the hosts
// after it are asked too until the page is full. A host that cannot be
// reached (mypikpak.me no longer resolves) is skipped; a provider refusal
// stops the search, since no other host would answer either. The same share
// is served by every host under one share id, so it is returned once.
func (c *Client) Search(ctx context.Context, apiKey, keyword string, page int, scope Scope) (Answer, error) {
	q := normalizeKeyword(keyword)
	if q == "" {
		return Answer{}, ErrEmptyQuery
	}
	if page < 1 {
		page = 1
	}
	if scope != ScopeSite {
		scope = ScopeShare
	}
	out := make([]Result, 0, pageSize)
	seen := make(map[string]bool, pageSize)
	// rawSeen counts every link the provider returned once, even when a
	// second host answers with the same list.
	rawSeen := make(map[string]bool, pageSize)
	raw := 0
	var unreachable error
	for _, site := range shareSites {
		if len(out) >= pageSize {
			break
		}
		hits, err := c.searchSite(ctx, apiKey, site, q, page)
		if err != nil {
			var upstream *UpstreamError
			if errors.As(err, &upstream) {
				return Answer{}, err
			}
			// The host could not be reached; the next one may answer.
			unreachable = err
			continue
		}
		for _, r := range hits {
			link := unwrapLink(r.Link)
			if _, ok := webAddress(link); !ok {
				continue
			}
			if rawSeen[link] {
				continue
			}
			rawSeen[link] = true
			raw++
			id, isShare := shareIDOf(link)
			if !isShare && scope != ScopeSite {
				continue
			}
			key := link
			if isShare {
				key = id
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Result{Title: r.Title, URL: link, Snippet: r.Snippet, Share: isShare})
			if len(out) >= pageSize {
				break
			}
		}
	}
	if len(out) == 0 && unreachable != nil {
		return Answer{}, unreachable
	}
	return Answer{Results: out, Raw: raw}, nil
}

// shareIDOf returns the share a result points at when it is a share page on
// one of PikPak's hosts. A site: query keeps results on the domain, not on
// share pages: the homepage, help and download pages pass the operator too,
// and none of them can be opened in the share browser.
func shareIDOf(raw string) (string, bool) {
	if !onSite(raw) {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "s" && parts[1] != "" {
		return parts[1], true
	}
	return "", false
}

// unwrapLink returns the address a search provider hid behind its own click
// redirect. Google answers with google.com/url?q=… or, in some regions,
// google.com/goto?url=<base64 protobuf>; the PikPak address underneath is
// what the app opens, dedupes and marks. A redirect that cannot be read, or
// that leads somewhere that is not an http(s) address, stays as it is.
func unwrapLink(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !isGoogleHost(strings.ToLower(u.Hostname())) {
		return raw
	}
	switch strings.TrimSuffix(u.Path, "/") {
	case "/url":
		for _, key := range []string{"q", "url"} {
			if dest := u.Query().Get(key); dest != "" {
				if _, ok := webAddress(dest); ok {
					return dest
				}
			}
		}
	case "/goto":
		if dest := decodeGoto(u.Query().Get("url")); dest != "" {
			if _, ok := webAddress(dest); ok {
				return dest
			}
		}
	}
	return raw
}

func isGoogleHost(host string) bool {
	return host == "google.com" ||
		strings.HasSuffix(host, ".google.com") ||
		strings.HasPrefix(host, "google.") ||
		strings.HasPrefix(host, "www.google.")
}

// decodeGoto reads the destination out of a /goto payload: base64 over a
// protobuf that stores the address as a plain string.
func decodeGoto(enc string) string {
	if enc == "" {
		return ""
	}
	for _, encoding := range []*base64.Encoding{
		base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding,
	} {
		decoded, err := encoding.DecodeString(enc)
		if err != nil {
			continue
		}
		if dest := firstAddress(string(decoded)); dest != "" {
			return dest
		}
	}
	return ""
}

// firstAddress returns the first http(s) address in s, up to the first byte
// that cannot be part of one.
func firstAddress(s string) string {
	start := -1
	for _, scheme := range []string{"https://", "http://"} {
		if i := strings.Index(s, scheme); i >= 0 && (start < 0 || i < start) {
			start = i
		}
	}
	if start < 0 {
		return ""
	}
	rest := s[start:]
	for i := 0; i < len(rest); i++ {
		if c := rest[i]; c <= ' ' || c >= 0x7f {
			return rest[:i]
		}
	}
	return rest
}

// webAddress reports whether raw is an http(s) address the page can open.
// A provider result is untrusted text; a link with any other scheme could
// run script in the app when the person clicks it.
func webAddress(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	return raw, true
}

// searchSite runs one site-scoped query and hands back every organic hit the
// provider answered with. The caller decides what to keep: the provider does
// not always honor the site: operator, so the decision belongs in one place.
func (c *Client) searchSite(ctx context.Context, apiKey, site, q string, page int) (hits []serperResult, err error) {
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
	return parsed.Organic, nil
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
