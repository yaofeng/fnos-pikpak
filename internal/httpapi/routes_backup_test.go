package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fnos-pikpak/internal/store"
)

// TestBackupContainsOnlyTheCurrentUser checks the download, its metadata and
// the one hard rule of the document: it carries favourites and settings, never
// the account's OAuth credentials.
func TestBackupContainsOnlyTheCurrentUser(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	ctx := context.Background()
	bindTestUser(t, deps.Store, 1000)
	bindTestUser(t, deps.Store, 1001)

	if _, err := deps.Store.AddShareFavorite(ctx, 1000, "share-a", "A", "1111"); err != nil {
		t.Fatalf("share favorite 1000: %v", err)
	}
	if _, err := deps.Store.AddShareFavorite(ctx, 1001, "share-b", "B", "2222"); err != nil {
		t.Fatalf("share favorite 1001: %v", err)
	}
	mine, err := deps.Store.AddVideoFavorite(ctx, 1000, store.VideoFavorite{
		ShareID: "share-a", FileID: "f1", Name: "a.mp4", Size: 9,
	})
	if err != nil {
		t.Fatalf("video favorite 1000: %v", err)
	}
	if _, err := deps.Store.MoveVideoFavoriteGroup(ctx, 1000, mine.ID, "我的分组"); err != nil {
		t.Fatalf("group: %v", err)
	}
	if _, err := deps.Store.AddVideoFavorite(ctx, 1001, store.VideoFavorite{
		ShareID: "share-b", FileID: "f2", Name: "b.mp4", Size: 9,
	}); err != nil {
		t.Fatalf("video favorite 1001: %v", err)
	}
	if err := deps.Store.SetUserSetting(ctx, 1000, "language", "en"); err != nil {
		t.Fatalf("user setting: %v", err)
	}
	if err := deps.Store.SetSetting(ctx, "search_api_key", "sk-secret"); err != nil {
		t.Fatalf("global setting: %v", err)
	}

	h := NewMux(deps)
	rec, body := getJSON(t, h, "/api/backup", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}

	if body["format"] != float64(1) {
		t.Fatalf("format = %v", body["format"])
	}
	if body["app_version"] != "test" {
		t.Fatalf("app_version = %v", body["app_version"])
	}
	if body["exported_at"] == "" {
		t.Fatal("exported_at is empty")
	}

	settings, _ := body["settings"].(map[string]any)
	if settings["language"] != "en" {
		t.Fatalf("settings.language = %v", settings["language"])
	}
	if settings["search_api_key"] != "sk-secret" {
		t.Fatalf("settings.search_api_key = %v", settings["search_api_key"])
	}

	shares, _ := body["share_favorites"].([]any)
	if len(shares) != 1 {
		t.Fatalf("share_favorites = %v", shares)
	}
	if shares[0].(map[string]any)["share_id"] != "share-a" {
		t.Fatalf("a foreign share leaked into the backup: %v", shares[0])
	}

	videos, _ := body["video_favorites"].([]any)
	if len(videos) != 1 || videos[0].(map[string]any)["file_id"] != "f1" {
		t.Fatalf("video_favorites = %v", videos)
	}

	groups, _ := body["video_groups"].([]any)
	if len(groups) != 1 || groups[0].(map[string]any)["name"] != "我的分组" {
		t.Fatalf("video_groups = %v", groups)
	}

	disposition := rec.Header().Get("Content-Disposition")
	if !strings.HasPrefix(disposition, `attachment; filename="pikpak-backup-`) || !strings.HasSuffix(disposition, `.json"`) {
		t.Fatalf("Content-Disposition = %q", disposition)
	}

	raw := rec.Body.String()
	for _, forbidden := range []string{"access_token", "refresh_token", "device_id", "credentials", `"uid"`} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("the backup contains %q: %s", forbidden, raw)
		}
	}
}

// TestRestoreMergesFavoritesAndOverwritesSettings covers the merge rules: an
// existing row is skipped, a new one is inserted, settings are overwritten and
// another user is untouched.
func TestRestoreMergesFavoritesAndOverwritesSettings(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	deps.Version = "1.0.0"
	ctx := context.Background()
	bindTestUser(t, deps.Store, 1000)
	bindTestUser(t, deps.Store, 1001)

	if _, err := deps.Store.AddShareFavorite(ctx, 1000, "share-a", "本机标题", ""); err != nil {
		t.Fatalf("seed share: %v", err)
	}
	existing, err := deps.Store.AddVideoFavorite(ctx, 1000, store.VideoFavorite{
		ShareID: "share-a", FileID: "f1", Name: "a.mp4", Size: 9,
	})
	if err != nil {
		t.Fatalf("seed video: %v", err)
	}
	if _, err := deps.Store.MoveVideoFavoriteGroup(ctx, 1000, existing.ID, "已有分组"); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if err := deps.Store.SetUserSetting(ctx, 1001, "language", "zh-CN"); err != nil {
		t.Fatalf("other user setting: %v", err)
	}

	doc := map[string]any{
		"format":      1,
		"app_version": "1.0.0",
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"settings": map[string]string{
			"language":             "en",
			"search_api_key":       "sk-restored",
			"download_concurrency": "4",
			"future_key":           "ignored",
		},
		"share_favorites": []map[string]any{
			{"share_id": "share-a", "title": "备份标题", "display_title": "备份显示名", "created_at": "2026-01-01T00:00:00Z"},
			{"share_id": "share-b", "title": "B", "created_at": "2026-01-02T00:00:00Z"},
		},
		"video_favorites": []map[string]any{
			// Same share id and file id as the local row.
			{"share_id": "share-a", "file_id": "f1", "name": "a.mp4", "size": 9},
			// Same name and size as the local row: the fallback duplicate.
			{"share_id": "share-a", "file_id": "f9", "name": "a.mp4", "size": 9},
			{"share_id": "share-b", "file_id": "f2", "name": "b.mp4", "size": 10, "group_label": "新分组"},
		},
		"video_groups": []map[string]any{{"name": "新分组"}, {"name": "已有分组"}},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	h := NewMux(deps)
	rec, body := postJSON(t, h, "/api/restore", "1000", string(raw))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}

	if body["settings_overwritten"] != float64(3) {
		t.Fatalf("settings_overwritten = %v (unknown keys must be skipped)", body["settings_overwritten"])
	}
	shares, _ := body["share_favorites"].(map[string]any)
	if shares["added"] != float64(1) || shares["skipped"] != float64(1) {
		t.Fatalf("share_favorites = %v", shares)
	}
	videos, _ := body["video_favorites"].(map[string]any)
	if videos["added"] != float64(1) || videos["skipped"] != float64(2) {
		t.Fatalf("video_favorites = %v", videos)
	}
	groups, _ := body["video_groups"].(map[string]any)
	if groups["added"] != float64(1) || groups["merged"] != float64(1) {
		t.Fatalf("video_groups = %v", groups)
	}

	if v, err := deps.Store.GetUserSetting(ctx, 1000, "language"); err != nil || v != "en" {
		t.Fatalf("language = %q, %v", v, err)
	}
	if v, err := deps.Store.GetSetting(ctx, "search_api_key"); err != nil || v != "sk-restored" {
		t.Fatalf("search_api_key = %q, %v", v, err)
	}
	if v, err := deps.Store.GetSetting(ctx, "download_concurrency"); err != nil || v != "4" {
		t.Fatalf("download_concurrency = %q, %v", v, err)
	}
	if _, err := deps.Store.GetUserSetting(ctx, 1000, "future_key"); err == nil {
		t.Fatal("an unknown key was stored")
	}
	if v, _ := deps.Store.GetUserSetting(ctx, 1001, "language"); v != "zh-CN" {
		t.Fatalf("another user's setting changed: %q", v)
	}

	// The local row wins: restore never overwrites what is already here.
	saved, err := deps.Store.ShareFavoriteByShareID(ctx, 1000, "share-a")
	if err != nil {
		t.Fatalf("share-a: %v", err)
	}
	if saved.Title != "本机标题" || saved.DisplayTitle != "" {
		t.Fatalf("local share was overwritten: %+v", saved)
	}
	if _, err := deps.Store.ShareFavoriteByShareID(ctx, 1000, "share-b"); err != nil {
		t.Fatalf("share-b was not added: %v", err)
	}

	list, err := deps.Store.ListVideoFavorites(ctx, 1000)
	if err != nil {
		t.Fatalf("list videos: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("videos = %d, want 2", len(list))
	}
	var imported *store.VideoFavorite
	for i := range list {
		if list[i].FileID == "f2" {
			imported = &list[i]
		}
	}
	if imported == nil || imported.GroupLabel != "新分组" {
		t.Fatalf("imported video = %+v", imported)
	}
}

func TestRestoreRejectsAnUnknownFormat(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)

	rec, body := postJSON(t, h, "/api/restore", "1000", `{"format":2}`)
	if rec.Code != http.StatusBadRequest || errCode(body) != "bad_backup_format" {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}

func TestRestoreRejectsANewerAppVersion(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	deps.Version = "1.0.0"
	h := NewMux(deps)

	rec, body := postJSON(t, h, "/api/restore", "1000", `{"format":1,"app_version":"2.0.0"}`)
	if rec.Code != http.StatusBadRequest || errCode(body) != "backup_version_too_new" {
		t.Fatalf("newer: status = %d body=%v", rec.Code, body)
	}

	// The same build is fine.
	rec, body = postJSON(t, h, "/api/restore", "1000", `{"format":1,"app_version":"1.0.0"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("same version: status = %d body=%v", rec.Code, body)
	}
}

func TestRestoreRejectsAnOversizedBody(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)

	// The settings value alone pushes the body past the 8 MiB limit.
	body := `{"format":1,"settings":{"language":"` + strings.Repeat("a", 8<<20) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/restore", strings.NewReader(body))
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}
