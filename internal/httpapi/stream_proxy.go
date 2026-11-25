package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// linkTTL is how long a signed download URL is reused. PikPak signs are valid
// for hours, but a short cache keeps a stale link from being reused after the
// server rotates it.
const linkTTL = 10 * time.Minute

type cachedLink struct {
	url     string
	expires time.Time
}

// linkCache remembers signed download URLs per user, account and file so a
// seek-heavy video does not ask PikPak for a fresh link on every range
// request. The account is part of the key: the same file id on two accounts
// is two different files with two different signed URLs.
type linkCache struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]cachedLink
}

func newLinkCache(ttl time.Duration) *linkCache {
	if ttl <= 0 {
		ttl = linkTTL
	}
	return &linkCache{ttl: ttl, items: map[string]cachedLink{}}
}

func (c *linkCache) key(uid int, accountID, fileID string) string {
	return strconv.Itoa(uid) + "|" + accountID + "|" + fileID
}

func (c *linkCache) get(key string, now time.Time) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.items[key]
	if !ok || now.After(item.expires) {
		return "", false
	}
	return item.url, true
}

func (c *linkCache) put(key, url string, now time.Time) {
	c.mu.Lock()
	c.items[key] = cachedLink{url: url, expires: now.Add(c.ttl)}
	c.mu.Unlock()
}

func (c *linkCache) drop(key string) {
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()
}

// linkFor returns a signed URL, using the cache unless refresh is set. The
// URL is fetched through the named account's session.
func (deps Deps) linkFor(ctx context.Context, uid int, accountID, fileID string, refresh bool) (string, error) {
	key := deps.Links.key(uid, accountID, fileID)
	if !refresh {
		if url, ok := deps.Links.get(key, time.Now()); ok {
			return url, nil
		}
	}
	client, err := deps.Sessions.ClientForAccount(ctx, uid, accountID)
	if err != nil {
		return "", err
	}
	link, err := client.GetDownloadLink(ctx, fileID)
	if err != nil {
		return "", err
	}
	if link.URL == "" {
		return "", errNoLink
	}
	deps.Links.put(key, link.URL, time.Now())
	return link.URL, nil
}

// proxyStream relays the media with Range semantics intact: PikPak's CDN does
// the seeking, and the signed URL never reaches the browser.
func (deps Deps) proxyStream(w http.ResponseWriter, r *http.Request, uid int, accountID, fileID string) {
	url, err := deps.linkFor(r.Context(), uid, accountID, fileID, false)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	resp, err := fetchMedia(r, url)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusGone {
		// The signature expired: drop it, ask for a new one, retry once.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		resp.Body.Close()
		deps.Links.drop(deps.Links.key(uid, accountID, fileID))
		url, err = deps.linkFor(r.Context(), uid, accountID, fileID, true)
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		resp, err = fetchMedia(r, url)
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
	}
	defer resp.Body.Close()

	copyMediaHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, resp.Body)
}

var errNoLink = errors.New("PikPak 没有返回下载地址")

func fetchMedia(r *http.Request, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, url, nil)
	if err != nil {
		return nil, err
	}
	if v := r.Header.Get("Range"); v != "" {
		req.Header.Set("Range", v)
	}
	if v := r.Header.Get("If-Range"); v != "" {
		req.Header.Set("If-Range", v)
	}
	return http.DefaultClient.Do(req)
}

// copyMediaHeaders forwards only the headers a media client needs; upstream
// cookies and signatures stay behind.
func copyMediaHeaders(dst, src http.Header) {
	for _, key := range []string{
		"Content-Type", "Content-Length", "Content-Range", "Content-Encoding",
		"Accept-Ranges", "Last-Modified", "ETag", "Content-Disposition",
	} {
		if v := src.Get(key); v != "" {
			dst.Set(key, v)
		}
	}
}

// playMode returns "redirect" or "proxy" (the default). A per-user setting
// wins over the global one.
func playMode(ctx context.Context, deps Deps, uid int) string {
	if deps.Store != nil && deps.Store.EffectiveSetting(ctx, uid, "play_mode") == "redirect" {
		return "redirect"
	}
	return "proxy"
}
