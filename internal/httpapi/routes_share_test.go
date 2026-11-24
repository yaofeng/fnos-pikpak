package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"fnos-pikpak/internal/playtoken"
)

// shareUpstream fakes PikPak's share endpoints. The media server is separate so
// the signed link points at bytes.
func shareUpstream(t *testing.T) (*httptest.Server, *httptest.Server) {
	t.Helper()
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "PDF-BYTES")
	}))
	t.Cleanup(media.Close)

	var restoreBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/drive/v1/share":
			pass := r.URL.Query().Get("pass_code")
			status := "OK"
			if pass == "" {
				status = "PASS_CODE_EMPTY"
			} else if pass == "bad" {
				status = "PASS_CODE_ERROR"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"share_status":    status,
				"pass_code_token": "tok-1",
				"title":           "大模型资料",
				"share_creator":   map[string]any{"name": "张三"},
				"files": []map[string]any{
					{"id": "sf1", "parent_id": "", "name": "资料", "kind": "drive#folder"},
					{"id": "sf2", "parent_id": "", "name": "notes.pdf", "kind": "drive#file", "size": "9"},
				},
			})
		case "/drive/v1/share/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"id": "sf3", "parent_id": "sf1", "name": "inner.bin", "kind": "drive#file", "size": "3"},
				},
			})
		case "/drive/v1/share/file_info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"file_info": map[string]any{
					"id":               r.URL.Query().Get("file_id"),
					"name":             "notes.pdf",
					"mime_type":        "application/pdf",
					"size":             "9",
					"web_content_link": media.URL + "/notes.pdf",
				},
			})
		case "/drive/v1/share/restore":
			_ = json.NewDecoder(r.Body).Decode(&restoreBody)
			_ = json.NewEncoder(w).Encode(map[string]any{"task_id": "t1"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, media
}

func TestShareOpenAsksForThePassCodeThenKeepsTheTokenServerSide(t *testing.T) {
	srv, _ := shareUpstream(t)
	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/share?url=https%3A%2F%2Fmypikpak.com%2Fs%2FVOabc123", "1000")
	if rec.Code != http.StatusBadRequest || errCode(body) != "pass_code_required" {
		t.Fatalf("no code: status=%d body=%v", rec.Code, body)
	}
	rec, body = getJSON(t, h, "/api/share?url=https%3A%2F%2Fmypikpak.com%2Fs%2FVOabc123&pass_code=bad", "1000")
	if rec.Code != http.StatusBadRequest || errCode(body) != "pass_code_wrong" {
		t.Fatalf("wrong code: status=%d body=%v", rec.Code, body)
	}

	rec, body = getJSON(t, h, "/api/share?url=https%3A%2F%2Fmypikpak.com%2Fs%2FVOabc123&pass_code=7f3a", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if body["title"] != "大模型资料" || body["protected"] != true {
		t.Fatalf("body = %v", body)
	}
	if body["owner"] != "张三" {
		t.Fatalf("owner = %v", body["owner"])
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %v", body["items"])
	}
	if strings.Contains(rec.Body.String(), "tok-1") {
		t.Fatal("the pass-code token reached the client")
	}

	// Browsing uses the cached token; the client sends only the share id.
	rec, body = getJSON(t, h, "/api/share/files?share_id=VOabc123&parent_id=sf1", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("browse status = %d body=%v", rec.Code, body)
	}
	browsed, _ := body["items"].([]any)
	if len(browsed) != 1 {
		t.Fatalf("browsed = %v", body["items"])
	}
}

func TestShareFileIssuesSignedStreamAndDownloadURLs(t *testing.T) {
	srv, _ := shareUpstream(t)
	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	if rec, body := getJSON(t, h, "/api/share?url=VOabc123&pass_code=7f3a", "1000"); rec.Code != http.StatusOK {
		t.Fatalf("open: %d %v", rec.Code, body)
	}
	rec, body := getJSON(t, h, "/api/share/file?share_id=VOabc123&file_id=sf2", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("file status = %d body=%v", rec.Code, body)
	}
	stream, _ := body["stream_url"].(string)
	download, _ := body["download_url"].(string)
	if !strings.HasPrefix(stream, "/api/share/stream/VOabc123/sf2?t=") {
		t.Fatalf("stream_url = %q", stream)
	}
	if !strings.HasPrefix(download, "/api/share/download/VOabc123/sf2?t=") {
		t.Fatalf("download_url = %q", download)
	}

	// The signed URLs work without the gateway header, like /api/stream.
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	resp, err := http.Get(ts.URL + download)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(got) != "PDF-BYTES" {
		t.Fatalf("download status=%d body=%q", resp.StatusCode, got)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("Content-Disposition = %q", cd)
	}

	// A drive-file token must not open a share stream.
	driveToken := playtoken.Sign(deps.PlaySecret, 1000, "", "sf2", time.Hour, time.Now())
	resp, err = http.Get(ts.URL + "/api/share/stream/VOabc123/sf2?t=" + driveToken)
	if err != nil {
		t.Fatalf("cross token: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("drive token opened a share: %d", resp.StatusCode)
	}
}

func TestShareRestoreSavesFilesToTheDrive(t *testing.T) {
	srv, _ := shareUpstream(t)
	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	if rec, body := getJSON(t, h, "/api/share?url=VOabc123&pass_code=7f3a", "1000"); rec.Code != http.StatusOK {
		t.Fatalf("open: %d %v", rec.Code, body)
	}
	rec, body := postJSON(t, h, "/api/share/restore", "1000", `{"share_id":"VOabc123","file_ids":["sf2"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore status = %d body=%v", rec.Code, body)
	}
}

func TestShareFavoritesAreListedAndDeleted(t *testing.T) {
	srv, _ := shareUpstream(t)
	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := postJSON(t, h, "/api/share-favorites", "1000",
		`{"url":"https://mypikpak.com/s/VOabc123?pwd=7f3a","title":"大模型资料"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add status = %d body=%v", rec.Code, body)
	}
	if strings.Contains(rec.Body.String(), "7f3a") {
		t.Fatal("the pass code reached the response")
	}

	_, list := getJSON(t, h, "/api/share-favorites", "1000")
	items, _ := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", list["items"])
	}
	first, _ := items[0].(map[string]any)
	if first["share_id"] != "VOabc123" || first["protected"] != true {
		t.Fatalf("favorite = %v", first)
	}

	// Opening the favourite decrypts the code and opens the share.
	id := strconv.FormatInt(int64(first["id"].(float64)), 10)
	rec, opened := getJSON(t, h, "/api/share?favorite_id="+id, "1000")
	if rec.Code != http.StatusOK || opened["title"] != "大模型资料" {
		t.Fatalf("open favorite: %d %v", rec.Code, opened)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/share-favorites/"+id, nil)
	req.Header.Set("X-Trim-Userid", "1000")
	recDel := httptest.NewRecorder()
	h.ServeHTTP(recDel, req)
	if recDel.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", recDel.Code, recDel.Body.String())
	}
	_, list = getJSON(t, h, "/api/share-favorites", "1000")
	if items, _ := list["items"].([]any); len(items) != 0 {
		t.Fatalf("items after delete = %v", items)
	}
}

func TestShareFavoritesPatch(t *testing.T) {
	srv, _ := shareUpstream(t)
	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	bindTestUser(t, deps.Store, 2000)
	h := NewMux(deps)

	rec, fav := postJSON(t, h, "/api/share-favorites", "1000",
		`{"url":"https://mypikpak.com/s/VOabc123?pwd=7f3a","title":"大模型资料"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %v", rec.Code, fav)
	}
	id := strconv.FormatInt(int64(fav["id"].(float64)), 10)

	rec, out := patchJSON(t, h, "/api/share-favorites/"+id, "1000", `{"title":"网课"}`)
	if rec.Code != http.StatusOK || out["display_title"] != "网课" || out["title"] != "大模型资料" {
		t.Fatalf("patch = %d %v", rec.Code, out)
	}

	rec, list := getJSON(t, h, "/api/share-favorites", "1000")
	items, _ := list["items"].([]any)
	first, _ := items[0].(map[string]any)
	if first["display_title"] != "网课" {
		t.Fatalf("list = %v", list)
	}
	if strings.Contains(rec.Body.String(), "7f3a") {
		t.Fatal("the list leaked the pass code")
	}

	// Another user cannot rename it.
	if rec, _ := patchJSON(t, h, "/api/share-favorites/"+id, "2000", `{"title":"x"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("other user = %d", rec.Code)
	}

	// An empty title restores the share's own name.
	rec, cleared := patchJSON(t, h, "/api/share-favorites/"+id, "1000", `{"title":""}`)
	if rec.Code != http.StatusOK || cleared["display_title"] != "" {
		t.Fatalf("clear = %d %v", rec.Code, cleared)
	}
}

func TestShareFileHandsIINATheDirectLink(t *testing.T) {
	srv, media := shareUpstream(t)
	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	if rec, body := getJSON(t, h, "/api/share?url=VOabc123&pass_code=7f3a", "1000"); rec.Code != http.StatusOK {
		t.Fatalf("open: %d %v", rec.Code, body)
	}
	_, builtin := getJSON(t, h, "/api/share/file?share_id=VOabc123&file_id=sf2", "1000")
	if _, ok := builtin["direct_url"]; ok {
		t.Fatalf("builtin player got a direct link: %v", builtin["direct_url"])
	}

	if err := deps.Store.SetUserSetting(context.Background(), 1000, "player", "iina"); err != nil {
		t.Fatalf("set player: %v", err)
	}
	_, body := getJSON(t, h, "/api/share/file?share_id=VOabc123&file_id=sf2", "1000")
	if body["direct_url"] != media.URL+"/notes.pdf" {
		t.Fatalf("direct_url = %v", body["direct_url"])
	}
}

// Older favorites stored the pasted link; opening 分享收藏夹 rewrites the row
// to the share id (keeping the sealed code) so every consumer sees an id.
func TestShareFavoritesMigrateStoredLinksToShareIDs(t *testing.T) {
	srv, _ := shareUpstream(t)
	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	stale, err := deps.Store.AddShareFavorite(context.Background(), 1000,
		"https://mypikpak.com/s/VOabc123?pwd=7f3a", "", "7f3a")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, body := getJSON(t, h, "/api/share-favorites", "1000")
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", body["items"])
	}
	first := items[0].(map[string]any)
	if first["share_id"] != "VOabc123" {
		t.Fatalf("share_id = %v", first["share_id"])
	}
	if first["title"] != "VOabc123" {
		t.Fatalf("title = %v", first["title"])
	}
	got, err := deps.Store.GetShareFavorite(context.Background(), 1000, stale.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ShareID != "VOabc123" || got.PassCode != "7f3a" {
		t.Fatalf("migrated row = %+v", got)
	}
}
