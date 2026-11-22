package pikpak

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var (
	// ErrShareLink means the text is not a PikPak share link or id.
	ErrShareLink = errors.New("pikpak: not a share link")
	// ErrPassCodeRequired means the share is protected and no code arrived.
	ErrPassCodeRequired = errors.New("pikpak: share pass code required")
	// ErrPassCodeWrong means the code was rejected.
	ErrPassCodeWrong = errors.New("pikpak: share pass code wrong")
	// ErrShareUnavailable covers expired, canceled, or removed shares.
	ErrShareUnavailable = errors.New("pikpak: share unavailable")
)

// ShareInfo is one opened share: what it is called, the token that unlocks
// further reads, and the entries at its root.
type ShareInfo struct {
	ShareID       string
	Title         string
	PassCodeToken string
	Files         []File
	NextPageToken string
}

var shareIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{6,64}$`)

// ParseShareLink accepts the forms a person pastes: a full link on any PikPak
// domain, a link without a scheme, or a bare share id. A pwd/pass_code query
// parameter becomes the code.
func ParseShareLink(raw string) (shareID, passCode string, err error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", "", ErrShareLink
	}
	if shareIDPattern.MatchString(text) {
		return text, "", nil
	}
	if !strings.Contains(text, "://") {
		text = "https://" + text
	}
	u, err := url.Parse(text)
	if err != nil {
		return "", "", ErrShareLink
	}
	host := strings.ToLower(u.Hostname())
	onShareHost := false
	for _, site := range []string{"mypikpak.com", "mypikpak.net", "mypikpak.me", "pikpak.me"} {
		if host == site || strings.HasSuffix(host, "."+site) {
			onShareHost = true
			break
		}
	}
	if !onShareHost {
		return "", "", ErrShareLink
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "s" || !shareIDPattern.MatchString(parts[1]) {
		return "", "", ErrShareLink
	}
	passCode = u.Query().Get("pwd")
	if passCode == "" {
		passCode = u.Query().Get("pass_code")
	}
	return parts[1], passCode, nil
}

// GetShare opens a share. The returned token is what later browse and link
// calls carry; it never leaves the server.
func (c *Client) GetShare(ctx context.Context, shareID, passCode string) (ShareInfo, error) {
	q := url.Values{}
	q.Set("share_id", shareID)
	if passCode != "" {
		q.Set("pass_code", passCode)
	}
	q.Set("thumbnail_size", "SIZE_LARGE")
	q.Set("limit", "100")
	u := c.BaseDrive + "/drive/v1/share?" + q.Encode()

	var raw struct {
		ShareStatus   string `json:"share_status"`
		ShareID       string `json:"share_id"`
		Title         string `json:"title"`
		PassCodeToken string `json:"pass_code_token"`
		Files         []File `json:"files"`
		NextPageToken string `json:"next_page_token"`
	}
	if err := c.do(ctx, http.MethodGet, u, nil, &raw); err != nil {
		return ShareInfo{}, err
	}
	switch raw.ShareStatus {
	case "PASS_CODE_EMPTY":
		return ShareInfo{}, ErrPassCodeRequired
	case "PASS_CODE_ERROR":
		return ShareInfo{}, ErrPassCodeWrong
	case "", "OK", "SHARE_STATUS_OK":
	default:
		return ShareInfo{}, fmt.Errorf("%w: %s", ErrShareUnavailable, raw.ShareStatus)
	}
	return ShareInfo{
		ShareID:       shareID,
		Title:         raw.Title,
		PassCodeToken: raw.PassCodeToken,
		Files:         raw.Files,
		NextPageToken: raw.NextPageToken,
	}, nil
}

// ListShareFiles browses one folder of an opened share. parentID "" is the
// share root.
func (c *Client) ListShareFiles(ctx context.Context, shareID, parentID, passToken, pageToken string) (FilePage, error) {
	q := url.Values{}
	q.Set("share_id", shareID)
	q.Set("parent_id", parentID)
	q.Set("pass_code_token", passToken)
	q.Set("thumbnail_size", "SIZE_LARGE")
	q.Set("with_audit", "true")
	q.Set("limit", "100")
	if pageToken != "" {
		q.Set("page_token", pageToken)
	}
	u := c.BaseDrive + "/drive/v1/share/detail?" + q.Encode()

	var raw struct {
		Files         []File `json:"files"`
		NextPageToken string `json:"next_page_token"`
	}
	if err := c.do(ctx, http.MethodGet, u, nil, &raw); err != nil {
		return FilePage{}, err
	}
	return FilePage{Items: raw.Files, NextPageToken: raw.NextPageToken}, nil
}

// shareFileInfo is the part of a share file entry links are read from.
type shareFileInfo struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Size           json.RawMessage `json:"size"`
	MimeType       string          `json:"mime_type"`
	WebContentLink string          `json:"web_content_link"`
	Medias         []struct {
		Link struct {
			URL string `json:"url"`
		} `json:"link"`
		MediaName string `json:"media_name"`
	} `json:"medias"`
}

func (f shareFileInfo) downloadLink() DownloadLink {
	out := DownloadLink{Size: parseSize(f.Size)}
	if f.WebContentLink != "" {
		out.URL = f.WebContentLink
		return out
	}
	for _, m := range f.Medias {
		if m.Link.URL != "" {
			out.URL = m.Link.URL
			return out
		}
	}
	return out
}

// ShareFileLink is a share file's signed URL plus the facts a player needs.
type ShareFileLink struct {
	DownloadLink
	ID   string
	Name string
	Mime string
}

// GetShareFileLink asks for a fresh signed URL for one file of an opened
// share.
func (c *Client) GetShareFileLink(ctx context.Context, shareID, fileID, passToken string) (ShareFileLink, error) {
	q := url.Values{}
	q.Set("share_id", shareID)
	q.Set("file_id", fileID)
	q.Set("pass_code_token", passToken)
	u := c.BaseDrive + "/drive/v1/share/file_info?" + q.Encode()

	var raw struct {
		FileInfo *shareFileInfo `json:"file_info"`
		shareFileInfo
	}
	if err := c.do(ctx, http.MethodGet, u, nil, &raw); err != nil {
		return ShareFileLink{}, err
	}
	info := raw.shareFileInfo
	if raw.FileInfo != nil {
		info = *raw.FileInfo
	}
	link := info.downloadLink()
	if link.URL == "" {
		return ShareFileLink{}, ErrNoDownloadLink
	}
	return ShareFileLink{DownloadLink: link, ID: fileID, Name: info.Name, Mime: info.MimeType}, nil
}

// RestoreShare saves the selected share files into the user's own drive root.
func (c *Client) RestoreShare(ctx context.Context, shareID, passToken string, fileIDs []string) error {
	u := c.BaseDrive + "/drive/v1/share/restore"
	return c.do(ctx, http.MethodPost, u, map[string]any{
		"share_id":        shareID,
		"pass_code_token": passToken,
		"file_ids":        fileIDs,
	}, nil)
}
