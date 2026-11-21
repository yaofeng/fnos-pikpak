package pikpak

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"fnos-pikpak/internal/probe"
)

// OrderResult is one entry of the M0 sort investigation: whether PikPak
// honours the order parameter, and what came back first.
type OrderResult struct {
	Order     string `json:"order"`
	Status    int    `json:"status"`
	FirstName string `json:"first_name,omitempty"`
	Err       string `json:"error,omitempty"`
}

// orderCandidates covers the encodings the drive endpoint might expect. The
// device answered 400 for the four bare values, so the probe also tries the
// values JSON-encoded the way the share views send them, plus the share-link
// vocabulary.
var orderCandidates = []string{
	"name",
	`"name"`,
	`"-size"`,
	`"-modified_time"`,
	`"-created_time"`,
	"name_asc",
	"size_desc",
	"mod_desc",
}

// OrderCandidates asks for one file under each candidate order value. M0 uses
// the answers to decide whether server-side sorting can be trusted.
func (c *Client) OrderCandidates(ctx context.Context) []OrderResult {
	out := make([]OrderResult, 0, len(orderCandidates))
	for _, order := range orderCandidates {
		res := OrderResult{Order: order}
		page, err := c.ListFiles(ctx, "", ListOptions{Limit: 1, Order: order})
		if err != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) {
				res.Status = apiErr.Status
			}
			res.Err = probe.Redact(err.Error())
			out = append(out, res)
			continue
		}
		res.Status = 200
		if len(page.Items) > 0 {
			res.FirstName = page.Items[0].Name
		}
		out = append(out, res)
	}
	return out
}

// SearchProbeQuery is a name no real drive contains. An endpoint that filters
// answers with an empty list; one that ignores the query answers with the
// folder listing, which is how the probe tells them apart.
const SearchProbeQuery = "fnos-pikpak-probe-no-such-name"

// SearchResult records what one candidate search endpoint did. The body
// prefix is redacted before it leaves this package. Matched means the payload
// is consistent with server-side filtering: every name contains the query, or
// there are no names at all.
type SearchResult struct {
	Path              string   `json:"path"`
	Status            int      `json:"status"`
	BodyPrefix        string   `json:"body_prefix,omitempty"`
	LooksLikeFileList bool     `json:"looks_like_file_list"`
	Matched           bool     `json:"matched"`
	Names             []string `json:"names,omitempty"`
	Err               string   `json:"error,omitempty"`
}

func searchCandidateURLs(base, q string) []string {
	build := func(path string, params url.Values) string {
		u, err := url.Parse(base + path)
		if err != nil {
			return base + path
		}
		u.RawQuery = params.Encode()
		return u.String()
	}

	contains := `"name":{"contains":` + strconv.Quote(q) + `}`
	// The device rejected filters without phase/trashed ("file_invalid_file
	// filters"), so the filter candidate carries the full set the list
	// endpoint already requires.
	fullFilters := `{"phase":{"eq":"PHASE_TYPE_COMPLETE"},"trashed":{"eq":false},` + contains + `}`
	return []string{
		build("/drive/v1/files", url.Values{"filters": {fullFilters}, "limit": {"10"}}),
		build("/drive/v1/files", url.Values{"q": {q}, "limit": {"10"}}),
		build("/drive/v1/search", url.Values{"q": {q}, "limit": {"10"}}),
		build("/drive/v1/files:search", url.Values{"q": {q}, "limit": {"10"}}),
	}
}

// SearchCandidates tries the known possible search endpoints in order and
// reports their raw behavior. M0 decides the real implementation from the data.
func (c *Client) SearchCandidates(ctx context.Context, q string) []SearchResult {
	urls := searchCandidateURLs(c.BaseDrive, q)
	out := make([]SearchResult, 0, len(urls))
	for _, rawURL := range urls {
		u, _ := url.Parse(rawURL)
		res := SearchResult{Path: u.Path}
		status, body, err := c.getRaw(ctx, rawURL)
		if err != nil {
			res.Err = probe.Redact(err.Error())
			out = append(out, res)
			continue
		}
		res.Status = status
		res.BodyPrefix = probe.Redact(truncate(string(body), 200))
		var list struct {
			Files []struct {
				Name string `json:"name"`
			} `json:"files"`
		}
		if json.Unmarshal(body, &list) == nil && status == 200 {
			res.LooksLikeFileList = true
			// A 200 alone proves nothing: the list endpoint ignores q and
			// returns the folder contents. Only a payload whose every name
			// contains the query (or that is empty, when the query cannot
			// exist) proves the server filtered.
			needle := strings.ToLower(q)
			matched := true
			for i, f := range list.Files {
				if i < 5 {
					res.Names = append(res.Names, f.Name)
				}
				if !strings.Contains(strings.ToLower(f.Name), needle) {
					matched = false
				}
			}
			res.Matched = matched
		}
		out = append(out, res)
	}
	return out
}
