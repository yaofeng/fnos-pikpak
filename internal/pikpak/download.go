package pikpak

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
)

// ErrNoDownloadLink means the file detail carried no usable URL, which in
// practice happens for files still transcoding or blocked by the account.
var ErrNoDownloadLink = errors.New("pikpak: no download link")

type DownloadLink struct {
	URL  string
	Size int64
}

// GetDownloadLink asks PikPak for a fresh signed URL. The link expires, so
// callers must fetch it per transfer attempt and refresh on 403/410.
func (c *Client) GetDownloadLink(ctx context.Context, id string) (DownloadLink, error) {
	u := c.BaseDrive + "/drive/v1/files/" + url.PathEscape(id) +
		"?usage=FETCH&_magic=2021&thumbnail_size=SIZE_LARGE"

	var raw struct {
		Size           json.RawMessage `json:"size"`
		WebContentLink string          `json:"web_content_link"`
		Links          map[string]struct {
			URL string `json:"url"`
		} `json:"links"`
	}
	if err := c.do(ctx, http.MethodGet, u, nil, &raw); err != nil {
		return DownloadLink{}, err
	}

	link := DownloadLink{Size: parseSize(raw.Size)}
	if raw.WebContentLink != "" {
		link.URL = raw.WebContentLink
		return link, nil
	}
	// Map order is random; pick the first key in sorted order so a response
	// with several encodings behaves deterministically.
	keys := make([]string, 0, len(raw.Links))
	for k := range raw.Links {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if raw.Links[k].URL != "" {
			link.URL = raw.Links[k].URL
			return link, nil
		}
	}
	return DownloadLink{}, ErrNoDownloadLink
}
