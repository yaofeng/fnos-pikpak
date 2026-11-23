package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"fnos-pikpak/internal/store"
)

func patchJSON(t *testing.T, h http.Handler, path, uid, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
	req.Header.Set("X-Trim-Userid", uid)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestVideoFavoritesRoundTrip(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := postJSON(t, h, "/api/video-favorites", "1000",
		`{"share_id":"VOabc123","file_id":"sf2","name":"a.mp4","size":9,"owner":"bob","share_title":"资料","pass_code":"7f3a"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add status = %d body=%v", rec.Code, body)
	}
	if strings.Contains(rec.Body.String(), "7f3a") {
		t.Fatal("the pass code reached the response")
	}

	_, list := getJSON(t, h, "/api/video-favorites", "1000")
	items, _ := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", list["items"])
	}
	first, _ := items[0].(map[string]any)
	if first["owner"] != "bob" || first["share_title"] != "资料" || first["name"] != "a.mp4" {
		t.Fatalf("favorite = %v", first)
	}
	id := strconv.FormatInt(int64(first["id"].(float64)), 10)

	rec, renamed := patchJSON(t, h, "/api/video-favorites/"+id, "1000", `{"name":"我的片单"}`)
	if rec.Code != http.StatusOK || renamed["display_name"] != "我的片单" || renamed["name"] != "a.mp4" {
		t.Fatalf("rename: %d %v", rec.Code, renamed)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/video-favorites/"+id, nil)
	req.Header.Set("X-Trim-Userid", "1000")
	recDel := httptest.NewRecorder()
	h.ServeHTTP(recDel, req)
	if recDel.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", recDel.Code, recDel.Body.String())
	}
	_, list = getJSON(t, h, "/api/video-favorites", "1000")
	if items, _ := list["items"].([]any); len(items) != 0 {
		t.Fatalf("items after delete = %v", items)
	}
}

func TestVideoFavoriteValidation(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	for _, body := range []string{
		`{"share_id":"","file_id":"sf2"}`,
		`{"share_id":"VOabc123","file_id":""}`,
		`{"share_id":"VOabc123","file_id":"sf2","size":-1}`,
	} {
		if rec, out := postJSON(t, h, "/api/video-favorites", "1000", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s → %d %v", body, rec.Code, out)
		}
	}
	if rec, out := patchJSON(t, h, "/api/video-favorites/99", "1000", `{"name":"x"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("missing rename → %d %v", rec.Code, out)
	}
}

// A video starred from a share opened through 分享收藏夹 arrives without the
// code (the browser never saw it), so the saved share's code must stand in.
func TestVideoFavoriteInheritsTheShareFavoritePassCode(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	if rec, body := postJSON(t, h, "/api/share-favorites", "1000",
		`{"url":"https://mypikpak.com/s/VOabc123","pass_code":"7f3a"}`); rec.Code != http.StatusCreated {
		t.Fatalf("share favorite: %d %v", rec.Code, body)
	}
	rec, body := postJSON(t, h, "/api/video-favorites", "1000",
		`{"share_id":"VOabc123","file_id":"sf2","name":"a.mp4","size":9,"owner":"bob"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("video favorite: %d %v", rec.Code, body)
	}
	id := int64(body["id"].(float64))
	saved, err := deps.Store.GetVideoFavorite(context.Background(), 1000, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if saved.PassCode != "7f3a" {
		t.Fatalf("pass code = %q, want the share favorite's", saved.PassCode)
	}
}

// 视频收藏夹 lets a group be renamed and a video dragged into another one;
// both are local display changes.
func TestVideoFavoriteGroupsRenameAndMove(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := postJSON(t, h, "/api/video-favorites", "1000",
		`{"share_id":"VOabc123","file_id":"sf2","name":"a.mp4","size":9,"owner":"bob"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add: %d %v", rec.Code, body)
	}
	id := strconv.FormatInt(int64(body["id"].(float64)), 10)

	rec, moved := patchJSON(t, h, "/api/video-favorites/"+id, "1000", `{"group_label":"我的功夫片"}`)
	if rec.Code != http.StatusOK || moved["group_label"] != "我的功夫片" {
		t.Fatalf("move: %d %v", rec.Code, moved)
	}

	rec, out := patchJSON(t, h, "/api/video-favorites/groups", "1000",
		`{"from_key":"label:我的功夫片","label":"朋友分享"}`)
	if rec.Code != http.StatusOK || out["moved"] != float64(1) {
		t.Fatalf("rename group: %d %v", rec.Code, out)
	}
	_, list := getJSON(t, h, "/api/video-favorites", "1000")
	items, _ := list["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["group_label"] != "朋友分享" {
		t.Fatalf("items = %v", list["items"])
	}
}

// The check badge means "a copy still exists". A copy the user deleted sits in
// the trash, which PikPak's plain listing still returns, so the listing must
// leave it out or the badge outlives the copy.
func TestARestoredCopyInTheTrashLosesItsBadge(t *testing.T) {
	deps, h, _ := newVideoEnv(t, func(up *videoUpstream) {
		up.set("", folderEntry("pack-1", packFromSharedName))
		up.set("pack-1", map[string]any{
			"id": "copy-1", "name": "a.mp4", "kind": "drive#file", "size": "9", "trashed": true,
		})
	})
	rec, body := postJSON(t, h, "/api/video-favorites", "1000",
		`{"share_id":"VOabc123","file_id":"sf2","name":"a.mp4","size":9}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add status = %d body=%v", rec.Code, body)
	}
	id := int64(body["id"].(float64))
	if err := deps.Store.SetVideoFavoriteRestoredID(context.Background(), 1000, id, "copy-1"); err != nil {
		t.Fatalf("remember the copy: %v", err)
	}

	_, list := getJSON(t, h, "/api/video-favorites", "1000")
	items, _ := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", list["items"])
	}
	first, _ := items[0].(map[string]any)
	if first["restored"] != false {
		t.Fatalf("restored = %v, want false for a trashed copy", first["restored"])
	}
}

// The stored id only says where a copy used to be. When the drive cannot be
// read the badge stays off, so a copy the user deleted never keeps a badge.
func TestTheRestoredBadgeIsNotAssumedWhenTheDriveCannotBeRead(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)
	ctx := context.Background()
	fav, err := deps.Store.AddVideoFavorite(ctx, 1000, store.VideoFavorite{
		ShareID: "VOabc123", FileID: "sf2", Name: "a.mp4", Size: 9,
	})
	if err != nil {
		t.Fatalf("AddVideoFavorite: %v", err)
	}
	if err := deps.Store.SetVideoFavoriteRestoredID(ctx, 1000, fav.ID, "copy-1"); err != nil {
		t.Fatalf("remember the copy: %v", err)
	}

	_, list := getJSON(t, h, "/api/video-favorites", "1000")
	items, _ := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", list["items"])
	}
	first, _ := items[0].(map[string]any)
	if first["restored"] != false {
		t.Fatalf("restored = %v, want false when the drive cannot be read", first["restored"])
	}
}
