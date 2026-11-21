package pikpak

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"

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

var orderCandidates = []string{"name", "-size", "-modified_time", "-created_time"}

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

// SearchResult records what one candidate search endpoint did. The body
// prefix is redacted before it leaves this package.
type SearchResult struct {
	Path              string `json:"path"`
	Status            int    `json:"status"`
	BodyPrefix        string `json:"body_prefix,omitempty"`
	LooksLikeFileList bool   `json:"looks_like_file_list"`
	Err               string `json:"error,omitempty"`
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

	filters := `{"name":{"contains":` + strconv.Quote(q) + `}}`
	return []string{
		build("/drive/v1/files", url.Values{"filters": {filters}, "limit": {"10"}}),
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
			Files []json.RawMessage `json:"files"`
		}
		if json.Unmarshal(body, &list) == nil && status == 200 {
			res.LooksLikeFileList = true
		}
		out = append(out, res)
	}
	return out
}
