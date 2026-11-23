package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"fnos-pikpak/internal/store"
)

// statsUpstream serves a share tree whose totals only a recursive walk can
// know: two root files, one root folder paged across two pages, and a nested
// folder. It counts detail calls so a test can tell a remembered total from a
// recounted one.
type statsUpstream struct {
	srv         *httptest.Server
	mu          sync.Mutex
	detailCalls int
}

const (
	statsAvatarPNG = "\x89PNG\r\n\x1a\nfake"
	statsOwner     = "张三"
)

func newStatsUpstream(t *testing.T) *statsUpstream {
	t.Helper()
	up := &statsUpstream{}
	up.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/drive/v1/share":
			status := "OK"
			if r.URL.Query().Get("pass_code") == "" {
				status = "PASS_CODE_EMPTY"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"share_status":    status,
				"pass_code_token": "tok-stats",
				"title":           "资料合集",
				"file_num":        "2",
				"created_time":    "2025-11-22T16:05:25.004+08:00",
				"expiration_at":   "1790000000",
				"user_info": map[string]any{
					"nickname":     statsOwner,
					"portrait_url": up.srv.URL + "/avatar.png",
				},
			})
		case "/drive/v1/share/detail":
			up.mu.Lock()
			up.detailCalls++
			up.mu.Unlock()
			parent, page := r.URL.Query().Get("parent_id"), r.URL.Query().Get("page_token")
			var body map[string]any
			switch {
			case parent == "" && page == "":
				body = map[string]any{
					"files": []map[string]any{
						{"id": "f1", "parent_id": "", "name": "a.bin", "kind": "drive#file", "size": "5"},
						{"id": "A", "parent_id": "", "name": "子目录", "kind": "drive#folder"},
					},
					"next_page_token": "p2",
				}
			case parent == "" && page == "p2":
				body = map[string]any{
					"files": []map[string]any{
						{"id": "f4", "parent_id": "", "name": "d.bin", "kind": "drive#file", "size": "13"},
					},
				}
			case parent == "A":
				body = map[string]any{
					"files": []map[string]any{
						{"id": "f2", "parent_id": "A", "name": "b.bin", "kind": "drive#file", "size": "7"},
						{"id": "C", "parent_id": "A", "name": "深层", "kind": "drive#folder"},
					},
				}
			case parent == "C":
				body = map[string]any{
					"files": []map[string]any{
						{"id": "f3", "parent_id": "C", "name": "c.bin", "kind": "drive#file", "size": "11"},
					},
				}
			default:
				body = map[string]any{"files": []map[string]any{}}
			}
			_ = json.NewEncoder(w).Encode(body)
		case "/avatar.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte(statsAvatarPNG))
		case "/avatar.txt":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("not an image"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(up.srv.Close)
	return up
}

func (u *statsUpstream) details() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.detailCalls
}

func TestShareTotalsCountsEveryFolderAndPage(t *testing.T) {
	up := newStatsUpstream(t)
	deps, _ := newAuthTestDeps(t, up.srv.URL)
	bindTestUser(t, deps.Store, 1000)
	client, err := deps.Sessions.ClientFor(context.Background(), 1000)
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	files, size, err := shareTotals(context.Background(), client, "VOabc123", "tok-stats")
	if err != nil {
		t.Fatalf("shareTotals: %v", err)
	}
	if files != 4 || size != 36 {
		t.Fatalf("files=%d size=%d, want 4 and 36", files, size)
	}
}

// 分享收藏夹 is a read path: it shows what the cache holds and never walks a
// share, so opening the page costs no PikPak traffic.
func TestShareFavoritesListOnlyReadsCachedTotals(t *testing.T) {
	up := newStatsUpstream(t)
	deps, _ := newAuthTestDeps(t, up.srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	fav, err := deps.Store.AddShareFavorite(context.Background(), 1000, "share-list", "资料合集", "7f3a")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	items := listShareFavorites(t, h, "/api/share-favorites", "1000")
	if items[0]["file_count"] != float64(-1) || items[0]["stats_checked"] != false {
		t.Fatalf("uncached row = %v", items[0])
	}
	if up.details() != 0 {
		t.Fatalf("the list walked the share %d times", up.details())
	}

	if err := deps.Store.UpdateShareFavoriteMeta(context.Background(), 1000, fav.ID, store.ShareFavoriteMeta{
		FileCount: 4, TotalSize: 36, StatsAt: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("seed stats: %v", err)
	}
	items = listShareFavorites(t, h, "/api/share-favorites", "1000")
	if items[0]["file_count"] != float64(4) || items[0]["total_size"] != float64(36) || items[0]["stats_checked"] != true {
		t.Fatalf("cached row = %v", items[0])
	}
	if up.details() != 0 {
		t.Fatalf("the list recounted a cached share: %d walks", up.details())
	}
}

// A share is counted when the user enters it, in the background, and the
// total is remembered for the next visit to 分享收藏夹.
func TestOpeningAFavoriteCountsItsTotalsInTheBackground(t *testing.T) {
	up := newStatsUpstream(t)
	deps, _ := newAuthTestDeps(t, up.srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	fav, err := deps.Store.AddShareFavorite(context.Background(), 1000, "share-open", "资料合集", "7f3a")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec, body := getJSON(t, h, "/api/share?favorite_id="+strconv.FormatInt(fav.ID, 10), "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("open: %d %v", rec.Code, body)
	}

	got := waitForShareStats(t, deps.Store, 1000, fav.ID, 4)
	if got.FileCount != 4 || got.TotalSize != 36 {
		t.Fatalf("counted = %d files / %d bytes, want 4 and 36", got.FileCount, got.TotalSize)
	}

	walks := up.details()
	items := listShareFavorites(t, h, "/api/share-favorites", "1000")
	if items[0]["file_count"] != float64(4) || items[0]["total_size"] != float64(36) {
		t.Fatalf("list after the visit = %v", items[0])
	}
	if up.details() != walks {
		t.Fatalf("the list walked the share again: %d -> %d", walks, up.details())
	}
}

// A remembered total is trusted for a day; after that, the next visit counts
// it again in the background.
func TestOpeningAFavoriteCountsAgainOnlyAfterADay(t *testing.T) {
	up := newStatsUpstream(t)
	deps, _ := newAuthTestDeps(t, up.srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)
	ctx := context.Background()

	fresh, err := deps.Store.AddShareFavorite(ctx, 1000, "share-fresh", "资料合集", "7f3a")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := deps.Store.UpdateShareFavoriteMeta(ctx, 1000, fresh.ID, store.ShareFavoriteMeta{
		FileCount: 9, TotalSize: 90, StatsAt: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("seed stats: %v", err)
	}
	if rec, body := getJSON(t, h, "/api/share?favorite_id="+strconv.FormatInt(fresh.ID, 10), "1000"); rec.Code != http.StatusOK {
		t.Fatalf("open fresh: %d %v", rec.Code, body)
	}
	time.Sleep(150 * time.Millisecond)
	if up.details() != 0 {
		t.Fatalf("a fresh total was recounted: %d walks", up.details())
	}
	if got, _ := deps.Store.GetShareFavorite(ctx, 1000, fresh.ID); got.FileCount != 9 {
		t.Fatalf("fresh count changed to %d", got.FileCount)
	}

	stale, err := deps.Store.AddShareFavorite(ctx, 1000, "share-stale", "资料合集", "7f3a")
	if err != nil {
		t.Fatalf("seed stale: %v", err)
	}
	if err := deps.Store.UpdateShareFavoriteMeta(ctx, 1000, stale.ID, store.ShareFavoriteMeta{
		FileCount: 1, TotalSize: 1, StatsAt: time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("seed stale stats: %v", err)
	}
	if rec, body := getJSON(t, h, "/api/share?favorite_id="+strconv.FormatInt(stale.ID, 10), "1000"); rec.Code != http.StatusOK {
		t.Fatalf("open stale: %d %v", rec.Code, body)
	}
	got := waitForShareStats(t, deps.Store, 1000, stale.ID, 4)
	if got.FileCount != 4 || got.TotalSize != 36 {
		t.Fatalf("stale recount = %d files / %d bytes", got.FileCount, got.TotalSize)
	}
}

func waitForShareStats(t *testing.T, st *store.Store, uid int, id int64, want int64) store.ShareFavorite {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		fav, err := st.GetShareFavorite(context.Background(), uid, id)
		if err == nil && fav.FileCount == want {
			return fav
		}
		if time.Now().After(deadline) {
			t.Fatalf("stats never reached %d: %+v err=%v", want, fav, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestShareFavoriteAvatarIsProxiedFromTheOwner(t *testing.T) {
	up := newStatsUpstream(t)
	deps, _ := newAuthTestDeps(t, up.srv.URL)
	bindTestUser(t, deps.Store, 1000)
	bindTestUser(t, deps.Store, 2000)
	h := NewMux(deps)

	fav, err := deps.Store.AddShareFavorite(context.Background(), 1000, "VOabc123", "资料合集", "7f3a")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := deps.Store.UpdateShareFavoriteMeta(context.Background(), 1000, fav.ID,
		storeMeta(up.srv.URL+"/avatar.png")); err != nil {
		t.Fatalf("meta: %v", err)
	}

	rec := rawGet(t, h, "/api/share-favorites/"+strconv.FormatInt(fav.ID, 10)+"/avatar", "1000")
	if rec.Code != http.StatusOK || rec.Body.String() != statsAvatarPNG {
		t.Fatalf("avatar = %d %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if rec.Header().Get("Cache-Control") == "" {
		t.Fatal("the avatar carries no cache header")
	}

	// Another user's favorite is invisible.
	if rec := rawGet(t, h, "/api/share-favorites/"+strconv.FormatInt(fav.ID, 10)+"/avatar", "2000"); rec.Code != http.StatusNotFound {
		t.Fatalf("other user = %d", rec.Code)
	}

	// A URL answering with something that is not an image is refused.
	if err := deps.Store.UpdateShareFavoriteMeta(context.Background(), 1000, fav.ID,
		storeMeta(up.srv.URL+"/avatar.txt")); err != nil {
		t.Fatalf("meta: %v", err)
	}
	if rec := rawGet(t, h, "/api/share-favorites/"+strconv.FormatInt(fav.ID, 10)+"/avatar", "1000"); rec.Code != http.StatusBadGateway {
		t.Fatalf("non-image = %d", rec.Code)
	}
}

func listShareFavorites(t *testing.T, h http.Handler, path, uid string) []map[string]any {
	t.Helper()
	rec, body := getJSON(t, h, path, uid)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: %d %v", path, rec.Code, body)
	}
	raw, _ := body["items"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		out = append(out, it.(map[string]any))
	}
	return out
}

func rawGet(t *testing.T, h http.Handler, path, uid string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if uid != "" {
		req.Header.Set("X-Trim-Userid", uid)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func storeMeta(avatar string) store.ShareFavoriteMeta {
	return store.ShareFavoriteMeta{OwnerAvatar: avatar}
}
