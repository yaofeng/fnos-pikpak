package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"fnos-pikpak/internal/pikpak"
)

// errPathMissing reports the requested path does not resolve on this account.
// Every walk failure — a segment not found as much as any upstream error —
// comes back wrapped in this, so callers treat the whole account as "lacks
// the path" without distinguishing causes.
var errPathMissing = errors.New("path missing on this account")

// folderWalkFilters asks PikPak for live folders only: the plain listing
// still returns trashed rows, and only drive#folder rows can be walked into.
const folderWalkFilters = `{"trashed":{"eq":false},"kind":{"eq":"drive#folder"}}`

// walkSegmentPages caps how many listing pages one segment may consume, the
// same guard ListAllFiles uses to keep a misbehaving upstream from looping.
const walkSegmentPages = 50

// pathEntry is one cached resolution: the folder id and when it was resolved.
type pathEntry struct {
	folderID string
	at       time.Time
}

// pathCache memoizes resolveFolderPath results per (fnOS uid, PikPak
// account, path segments). Entries expire lazily — an expired one is simply
// re-walked on the next request, there is no sweeper — and one mutex covers
// the map.
type pathCache struct {
	mu      sync.Mutex
	entries map[string]pathEntry
	ttl     time.Duration
	// now is injectable so tests can expire entries without sleeping.
	now func() time.Time
}

func newPathCache() *pathCache {
	return &pathCache{
		entries: map[string]pathEntry{},
		ttl:     5 * time.Minute,
		now:     time.Now,
	}
}

// cacheKey names one resolution: uid and account id keep fnOS users and
// PikPak accounts apart, the marshaled segments keep ["a/b"] and ["a","b"]
// different paths.
func (c *pathCache) cacheKey(uid, accountID string, segs []string) string {
	raw, _ := json.Marshal(segs)
	return uid + "\x00" + accountID + "\x00" + string(raw)
}

func (c *pathCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || c.now().Sub(e.at) >= c.ttl {
		return "", false
	}
	return e.folderID, true
}

func (c *pathCache) put(key, folderID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = pathEntry{folderID: folderID, at: c.now()}
}

// resolveFolderPath returns the id of the folder at segs, walked segment by
// segment from the drive root with exact name matching on drive#folder rows.
// A cache hit within the TTL answers without any upstream call; a miss (or an
// expired entry) walks the tree and writes the result back. An empty segs
// resolves to the root (the empty id). The ctx is for upstream calls only —
// no HTTP request context is required. Every failure — missing segment, dead
// upstream, cancelled context — wraps errPathMissing.
func resolveFolderPath(ctx context.Context, uid string, nc namedClient, segs []string, cache *pathCache) (string, error) {
	key := cache.cacheKey(uid, nc.Account.ID, segs)
	if id, ok := cache.get(key); ok {
		return id, nil
	}
	parent := ""
	for _, seg := range segs {
		id, err := walkFolderSegment(ctx, nc, parent, seg)
		if err != nil {
			return "", err
		}
		parent = id
	}
	cache.put(key, parent)
	return parent, nil
}

// walkFolderSegment lists parentID ("" is the drive root) and returns the id
// of its child named seg, matched exactly. Pages are followed until the
// folder is exhausted or the segment is found, so a target beyond the first
// page still resolves.
func walkFolderSegment(ctx context.Context, nc namedClient, parentID, seg string) (string, error) {
	pageToken := ""
	for i := 0; i < walkSegmentPages; i++ {
		page, err := nc.C.ListFiles(ctx, parentID, pikpak.ListOptions{
			PageToken: pageToken,
			Limit:     100,
			Filters:   folderWalkFilters,
		})
		if err != nil {
			return "", fmt.Errorf("%w: listing for %q: %v", errPathMissing, seg, err)
		}
		for _, f := range page.Items {
			if f.Kind == "drive#folder" && f.Name == seg {
				return f.ID, nil
			}
		}
		if page.NextPageToken == "" {
			return "", fmt.Errorf("%w: no folder %q", errPathMissing, seg)
		}
		pageToken = page.NextPageToken
	}
	return "", fmt.Errorf("%w: listing for %q never ended", errPathMissing, seg)
}
