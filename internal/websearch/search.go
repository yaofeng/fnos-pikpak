// Package websearch asks a search engine's site-scoped search for links.
// It talks to Bing's RSS output, which needs no API key and supports the
// site: operator; the base URL is configurable so an instance can point at a
// proxy or another engine with the same shape.
package websearch

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrEmptyQuery means the caller asked for a search with no keywords.
var ErrEmptyQuery = errors.New("websearch: empty query")

const (
	maxResults = 20
	maxBody    = 2 << 20
)

// Result is one hit of the engine.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// Client performs searches against one engine endpoint.
type Client struct {
	// BaseURL is the engine origin; https://cn.bing.com by default.
	BaseURL string
	HTTP    *http.Client
}

func NewClient() *Client {
	return &Client{
		BaseURL: "https://cn.bing.com",
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

// Search runs the query, scoped to site when one is given.
func (c *Client) Search(ctx context.Context, query, site string) ([]Result, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, ErrEmptyQuery
	}
	if s := strings.TrimSpace(site); s != "" {
		q = "site:" + s + " " + q
	}

	u, err := url.Parse(strings.TrimRight(c.BaseURL, "/") + "/search")
	if err != nil {
		return nil, fmt.Errorf("websearch: bad base url: %w", err)
	}
	params := u.Query()
	params.Set("q", q)
	params.Set("format", "rss")
	params.Set("count", fmt.Sprint(maxResults))
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; fnos-pikpak)")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("websearch: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("websearch: engine answered %s", resp.Status)
	}

	var feed struct {
		Channel struct {
			Items []struct {
				Title       string `xml:"title"`
				Link        string `xml:"link"`
				Description string `xml:"description"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&feed); err != nil {
		return nil, fmt.Errorf("websearch: unreadable answer: %w", err)
	}

	items := make([]Result, 0, len(feed.Channel.Items))
	for _, item := range feed.Channel.Items {
		if len(items) >= maxResults {
			break
		}
		link := strings.TrimSpace(item.Link)
		title := strings.TrimSpace(item.Title)
		if link == "" || title == "" {
			continue
		}
		items = append(items, Result{
			Title:   title,
			URL:     link,
			Snippet: strings.TrimSpace(item.Description),
		})
	}
	return items, nil
}
