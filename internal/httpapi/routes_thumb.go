package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"fnos-pikpak/internal/fnos"
)

// thumbTTL bounds how long a proxied thumbnail stays in memory. The upstream
// link is short-lived, and a fresh request refetches it, so the cache only
// saves traffic on repeated grids.
const thumbTTL = 30 * time.Minute

// maxThumbBytes caps one image: PikPak thumbnails are small, and a CDN that
// answers with something else must not fill the process memory.
const maxThumbBytes = 5 << 20

type thumbItem struct {
	data    []byte
	mime    string
	expires time.Time
}

type thumbCache struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]thumbItem
}

func newThumbCache() *thumbCache {
	return &thumbCache{ttl: thumbTTL, items: map[string]thumbItem{}}
}

func thumbKey(uid int, shareID, fileID string) string {
	return strconv.Itoa(uid) + "|" + shareID + "|" + fileID
}

func (c *thumbCache) get(uid int, shareID, fileID string, now time.Time) ([]byte, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.items[thumbKey(uid, shareID, fileID)]
	if !ok || now.After(item.expires) {
		return nil, "", false
	}
	return item.data, item.mime, true
}

func (c *thumbCache) put(uid int, shareID, fileID string, data []byte, mime string, now time.Time) {
	c.mu.Lock()
	c.items[thumbKey(uid, shareID, fileID)] = thumbItem{data: data, mime: mime, expires: now.Add(c.ttl)}
	c.mu.Unlock()
}

// registerThumbRoutes proxies PikPak's thumbnail images so the browser never
// sees the CDN's signed URL.
func registerThumbRoutes(mux *http.ServeMux, deps Deps) {
	if deps.Thumbs == nil {
		deps.Thumbs = newThumbCache()
	}
	mux.HandleFunc("GET /api/thumb", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		shareID := strings.TrimSpace(r.URL.Query().Get("share_id"))
		fileID := strings.TrimSpace(r.URL.Query().Get("file_id"))
		if fileID == "" {
			writeError(w, http.StatusBadRequest, "bad_ids", "缺少文件编号")
			return
		}
		now := time.Now()
		if data, mime, ok := deps.Thumbs.get(id.UID, shareID, fileID, now); ok {
			writeThumb(w, data, mime)
			return
		}

		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}
		var link string
		if shareID != "" {
			token, ok := shareToken(w, deps, id.UID, shareID)
			if !ok {
				return
			}
			got, err := client.GetShareFileLink(r.Context(), shareID, fileID, token)
			if err != nil {
				writeUpstreamError(w, err)
				return
			}
			link = got.ThumbnailLink
		} else {
			got, err := client.FileDetail(r.Context(), fileID)
			if err != nil {
				writeUpstreamError(w, err)
				return
			}
			link = got.ThumbnailLink
		}
		if link == "" {
			writeError(w, http.StatusNotFound, "no_thumbnail", "该文件没有缩略图")
			return
		}

		data, mime, err := fetchThumb(r, link)
		if err != nil {
			writeError(w, http.StatusBadGateway, "thumb_failed", "缩略图加载失败")
			return
		}
		deps.Thumbs.put(id.UID, shareID, fileID, data, mime, now)
		writeThumb(w, data, mime)
	})
}

func fetchThumb(r *http.Request, link string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, link, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", errors.New("thumbnail upstream " + resp.Status)
	}
	mime := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(mime, "image/") {
		return nil, "", errors.New("not an image")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxThumbBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxThumbBytes {
		return nil, "", errors.New("thumbnail too large")
	}
	return data, mime, nil
}

func writeThumb(w http.ResponseWriter, data []byte, mime string) {
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
