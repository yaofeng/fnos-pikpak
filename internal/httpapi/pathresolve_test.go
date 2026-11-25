package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// walkRecorder counts the fake upstream's /drive/v1/files calls and keeps
// each call's filters query, so resolver tests can observe how often the
// walk touched the wire and with which filters.
type walkRecorder struct {
	mu      sync.Mutex
	calls   int
	filters []string
}

func (wr *walkRecorder) record(filters string) {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	wr.calls++
	wr.filters = append(wr.filters, filters)
}

func (wr *walkRecorder) count() int {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	return wr.calls
}

func (wr *walkRecorder) lastFilters() string {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	if n := len(wr.filters); n > 0 {
		return wr.filters[n-1]
	}
	return ""
}

// folderTreeServer serves a fixed listing per parent_id ("" is the root) and
// records every files call.
func folderTreeServer(t *testing.T, rec *walkRecorder, tree map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/files" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		rec.record(r.URL.Query().Get("filters"))
		parent := r.URL.Query().Get("parent_id")
		w.Header().Set("Content-Type", "application/json")
		if body, ok := tree[parent]; ok {
			_, _ = io.WriteString(w, body)
			return
		}
		_, _ = io.WriteString(w, `{"files":[]}`)
	}))
}

// resolveTestClient binds one namedClient against srv through the same
// session machinery the route tests use.
func resolveTestClient(t *testing.T, srvURL string) namedClient {
	t.Helper()
	deps, st := newAuthTestDeps(t, srvURL)
	acct := bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	client, err := deps.Sessions.ClientForAccount(t.Context(), 1000, "acct-a")
	if err != nil {
		t.Fatalf("ClientForAccount: %v", err)
	}
	return namedClient{Account: acct, C: client}
}

// movieTree is the brief's two-level tree: 电影 at the root, 动作 inside it,
// plus a prefix decoy and a file sharing the folder's name.
func movieTree() map[string]string {
	return map[string]string{
		"": `{"files":[` +
			`{"id":"root1","kind":"drive#folder","name":"电影"},` +
			`{"id":"decoy","kind":"drive#folder","name":"电影续"},` +
			`{"id":"filetrap","kind":"drive#file","name":"电影"}]}`,
		"root1": `{"files":[{"id":"leaf","kind":"drive#folder","name":"动作"}]}`,
	}
}

func TestResolveFolderPathExactMatch(t *testing.T) {
	rec := &walkRecorder{}
	srv := folderTreeServer(t, rec, movieTree())
	defer srv.Close()
	nc := resolveTestClient(t, srv.URL)

	id, err := resolveFolderPath(t.Context(), "1000", nc, []string{"电影", "动作"}, newPathCache())
	if err != nil {
		t.Fatalf("resolveFolderPath: %v", err)
	}
	if id != "leaf" {
		t.Fatalf("resolved %q, want leaf", id)
	}
	if got := rec.count(); got != 2 {
		t.Fatalf("walk made %d upstream calls, want 2 (root + one level)", got)
	}

	// Every walk step must ask for live folders only.
	var filters struct {
		Kind    struct{ Eq string } `json:"kind"`
		Trashed struct{ Eq bool }   `json:"trashed"`
	}
	if err := json.Unmarshal([]byte(rec.lastFilters()), &filters); err != nil {
		t.Fatalf("filters %q: %v", rec.lastFilters(), err)
	}
	if filters.Kind.Eq != "drive#folder" || filters.Trashed.Eq {
		t.Fatalf("filters = %s, want kind drive#folder and trashed false", rec.lastFilters())
	}
}

func TestResolveFolderPathMissingSegment(t *testing.T) {
	rec := &walkRecorder{}
	tree := movieTree()
	// 第二层没有"动作"。
	tree["root1"] = `{"files":[{"id":"other","kind":"drive#folder","name":"喜剧"}]}`
	srv := folderTreeServer(t, rec, tree)
	defer srv.Close()
	nc := resolveTestClient(t, srv.URL)

	id, err := resolveFolderPath(t.Context(), "1000", nc, []string{"电影", "动作"}, newPathCache())
	if !errors.Is(err, errPathMissing) {
		t.Fatalf("err = %v, want errPathMissing", err)
	}
	if id != "" {
		t.Fatalf("resolved %q on a missing path, want empty", id)
	}
}

func TestPathCacheKeyIncludesUIDAndSegments(t *testing.T) {
	rec := &walkRecorder{}
	srv := folderTreeServer(t, rec, map[string]string{
		"": `{"files":[` +
			`{"id":"slash","kind":"drive#folder","name":"a/b"},` +
			`{"id":"dira","kind":"drive#folder","name":"a"}]}`,
		"slash": `{"files":[]}`,
		"dira":  `{"files":[{"id":"dirab","kind":"drive#folder","name":"b"}]}`,
	})
	defer srv.Close()
	nc := resolveTestClient(t, srv.URL)
	cache := newPathCache()
	ctx := t.Context()

	// uid 1000 walks ["a/b"]: one root call, resolved.
	id, err := resolveFolderPath(ctx, "1000", nc, []string{"a/b"}, cache)
	if err != nil || id != "slash" {
		t.Fatalf(`["a/b"] = %q, %v; want slash`, id, err)
	}
	if got := rec.count(); got != 1 {
		t.Fatalf("first walk made %d upstream calls, want 1", got)
	}

	// ["a","b"] 是不同的键：同 uid 也要再走一遍。
	id, err = resolveFolderPath(ctx, "1000", nc, []string{"a", "b"}, cache)
	if err != nil || id != "dirab" {
		t.Fatalf(`["a","b"] = %q, %v; want dirab`, id, err)
	}
	if got := rec.count(); got != 3 {
		t.Fatalf(`["a","b"] walk made %d upstream calls, want 3 (different cache key)`, got)
	}

	// uid 1001 shares no entry with uid 1000: the walk runs again.
	id, err = resolveFolderPath(ctx, "1001", nc, []string{"a", "b"}, cache)
	if err != nil || id != "dirab" {
		t.Fatalf("uid 1001 = %q, %v; want dirab", id, err)
	}
	if got := rec.count(); got != 5 {
		t.Fatalf("uid 1001 walk made %d upstream calls, want 5 (uid is part of the key)", got)
	}
}

func TestPathCacheHitsWithinTTL(t *testing.T) {
	rec := &walkRecorder{}
	srv := folderTreeServer(t, rec, movieTree())
	defer srv.Close()
	nc := resolveTestClient(t, srv.URL)
	cache := newPathCache()
	base := time.Now()
	cache.now = func() time.Time { return base }
	ctx := t.Context()

	id, err := resolveFolderPath(ctx, "1000", nc, []string{"电影", "动作"}, cache)
	if err != nil || id != "leaf" {
		t.Fatalf("first resolve = %q, %v; want leaf", id, err)
	}

	// 同 uid 同 segs 第二次解析：缓存命中，上游零调用。
	id, err = resolveFolderPath(ctx, "1000", nc, []string{"电影", "动作"}, cache)
	if err != nil || id != "leaf" {
		t.Fatalf("cached resolve = %q, %v; want leaf", id, err)
	}
	if got := rec.count(); got != 2 {
		t.Fatalf("cache hit still walked: %d upstream calls, want 2", got)
	}

	// 过了 TTL 的条目在下次解析时懒刷新：重新走一遍。
	cache.now = func() time.Time { return base.Add(6 * time.Minute) }
	id, err = resolveFolderPath(ctx, "1000", nc, []string{"电影", "动作"}, cache)
	if err != nil || id != "leaf" {
		t.Fatalf("expired resolve = %q, %v; want leaf", id, err)
	}
	if got := rec.count(); got != 4 {
		t.Fatalf("expired entry not refreshed: %d upstream calls, want 4", got)
	}
}
