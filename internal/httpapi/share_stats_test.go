package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

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

// The list counts a share once and remembers the totals; ?refresh=1 recounts.
func TestShareFavoritesListRemembersTotalsUntilRefreshed(t *testing.T) {
	up := newStatsUpstream(t)
	deps, _ := newAuthTestDeps(t, up.srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	fav, err := deps.Store.AddShareFavorite(context.Background(), 1000, "VOabc123", "资料合集", "7f3a")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	body := listShareFavorites(t, h, "/api/share-favorites", "1000")
	first := body[0]
	if first["owner_name"] != statsOwner {
		t.Fatalf("owner_name = %v", first["owner_name"])
	}
	if first["owner_avatar"] != up.srv.URL+"/avatar.png" {
		t.Fatalf("owner_avatar = %v", first["owner_avatar"])
	}
	if first["share_created_at"] != "2025-11-22T16:05:25.004+08:00" || first["expire_at"] != float64(1790000000) {
		t.Fatalf("times = %v / %v", first["share_created_at"], first["expire_at"])
	}
	if first["file_count"] != float64(4) || first["total_size"] != float64(36) || first["stats_checked"] != true {
		t.Fatalf("stats = %v", first)
	}
	counted := up.details()

	body = listShareFavorites(t, h, "/api/share-favorites", "1000")
	if body[0]["file_count"] != float64(4) || up.details() != counted {
		t.Fatalf("a cached total was recounted: %v", body[0])
	}

	listShareFavorites(t, h, "/api/share-favorites?refresh=1", "1000")
	if up.details() <= counted {
		t.Fatalf("refresh did not recount: %d calls", up.details())
	}

	// The favorite is still the same row, with its code intact.
	got, err := deps.Store.GetShareFavorite(context.Background(), 1000, fav.ID)
	if err != nil || got.PassCode != "7f3a" {
		t.Fatalf("row after stats = %+v err=%v", got, err)
	}
}

// A share that cannot be opened is still marked as checked, so the list does
// not hit PikPak again on every load.
func TestShareFavoritesMarkAFailedCountAsChecked(t *testing.T) {
	up := newStatsUpstream(t)
	deps, _ := newAuthTestDeps(t, up.srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	// No pass code saved and none cached: opening the share fails.
	if _, err := deps.Store.AddShareFavorite(context.Background(), 1000, "VOabc123", "资料合集", ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	body := listShareFavorites(t, h, "/api/share-favorites", "1000")
	if body[0]["stats_checked"] != true || body[0]["file_count"] != float64(-1) {
		t.Fatalf("failed count = %v", body[0])
	}
	if _, ok := body[0]["owner_name"]; !ok || body[0]["owner_name"] != "" {
		t.Fatalf("owner_name = %v", body[0]["owner_name"])
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
