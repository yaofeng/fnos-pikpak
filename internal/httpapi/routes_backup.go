package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/store"
	"fnos-pikpak/internal/version"
)

const (
	backupFormat   = 1
	maxBackupBytes = 8 << 20
)

// backupGlobalSettingKeys are stored app-wide, so they are restored globally.
// backupUserSettingKeys belong to one fnOS user and are restored for the
// caller only. Keys outside settingSpecs are skipped, which keeps a backup
// from an older build from writing settings this build never reads.
//
// The search API key is not exported: it is a secret, and the file is meant to
// be copied between machines. A restore still accepts one written into the
// file by hand or by an older build, because isGlobalSettingKey covers it.
var backupGlobalSettingKeys = []string{"download_concurrency", "upload_concurrency"}

var backupUserSettingKeys = []string{"conflict", "play_mode", "player", "tile_size", "language", "search_scope", "download_dir"}

type backupDocument struct {
	Format     int    `json:"format"`
	AppVersion string `json:"app_version"`
	ExportedAt string `json:"exported_at"`

	Settings       map[string]string     `json:"settings"`
	ShareFavorites []backupShareFavorite `json:"share_favorites"`
	VideoFavorites []backupVideoFavorite `json:"video_favorites"`
	VideoGroups    []backupVideoGroup    `json:"video_groups"`
}

type backupShareFavorite struct {
	ShareID        string `json:"share_id"`
	Title          string `json:"title"`
	DisplayTitle   string `json:"display_title"`
	Protected      bool   `json:"protected"`
	OwnerName      string `json:"owner_name"`
	OwnerAvatar    string `json:"owner_avatar"`
	ShareCreatedAt string `json:"share_created_at"`
	ExpireAt       int64  `json:"expire_at"`
	FileCount      int64  `json:"file_count"`
	TotalSize      int64  `json:"total_size"`
	StatsAt        string `json:"stats_at"`
	PassCode       string `json:"pass_code,omitempty"`
	CreatedAt      string `json:"created_at"`
}

type backupVideoFavorite struct {
	ShareID     string `json:"share_id"`
	FileID      string `json:"file_id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Owner       string `json:"owner"`
	ShareTitle  string `json:"share_title"`
	DisplayName string `json:"display_name"`
	GroupLabel  string `json:"group_label"`
	PassCode    string `json:"pass_code,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type backupVideoGroup struct {
	Name string `json:"name"`
}

type restoreResult struct {
	SettingsOverwritten int                `json:"settings_overwritten"`
	ShareFavorites      restoreCounts      `json:"share_favorites"`
	VideoFavorites      restoreCounts      `json:"video_favorites"`
	VideoGroups         restoreGroupCounts `json:"video_groups"`
}

type restoreCounts struct {
	Added   int `json:"added"`
	Skipped int `json:"skipped"`
}

type restoreGroupCounts struct {
	Added  int `json:"added"`
	Merged int `json:"merged"`
}

// registerBackupRoutes exports one user's favourites and settings as a single
// JSON file and imports it back. The identity always comes from the gateway,
// so a backup can never name another user.
func registerBackupRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/backup", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		doc, err := buildBackup(r.Context(), deps, id.UID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "导出备份失败")
			return
		}
		filename := "pikpak-backup-" + time.Now().Format("20060102") + ".json"
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(doc)
	})

	mux.HandleFunc("POST /api/restore", func(w http.ResponseWriter, r *http.Request) {
		var doc backupDocument
		if !decodeBackup(w, r, &doc) {
			return
		}
		if doc.Format != backupFormat {
			writeError(w, http.StatusBadRequest, "bad_backup_format", "备份文件格式不受支持或已损坏")
			return
		}
		current := deps.Version
		if current == "" {
			current = version.Version
		}
		if versionNewer(doc.AppVersion, current) {
			writeError(w, http.StatusBadRequest, "backup_version_too_new", "备份文件来自更新版本的应用，请先升级应用后再恢复")
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		result, err := restoreBackup(r.Context(), deps, id.UID, doc)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "恢复备份失败")
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

// buildBackup reads only this user's rows plus the app-wide settings. The
// credentials table is never touched: an exported file must not carry an
// account's OAuth session.
func buildBackup(ctx context.Context, deps Deps, uid int) (backupDocument, error) {
	st := deps.Store
	doc := backupDocument{
		Format:         backupFormat,
		AppVersion:     deps.Version,
		ExportedAt:     time.Now().UTC().Format(time.RFC3339),
		Settings:       map[string]string{},
		ShareFavorites: []backupShareFavorite{},
		VideoFavorites: []backupVideoFavorite{},
		VideoGroups:    []backupVideoGroup{},
	}
	if doc.AppVersion == "" {
		doc.AppVersion = version.Version
	}

	for _, key := range backupGlobalSettingKeys {
		if v, err := st.GetSetting(ctx, key); err == nil {
			doc.Settings[key] = v
		}
	}
	for _, key := range backupUserSettingKeys {
		if v, err := st.GetUserSetting(ctx, uid, key); err == nil {
			doc.Settings[key] = v
		}
	}

	shares, err := st.ListShareFavorites(ctx, uid)
	if err != nil {
		return backupDocument{}, err
	}
	for _, fav := range shares {
		item := backupShareFavorite{
			ShareID:        fav.ShareID,
			Title:          fav.Title,
			DisplayTitle:   fav.DisplayTitle,
			Protected:      fav.Protected,
			OwnerName:      fav.OwnerName,
			OwnerAvatar:    fav.OwnerAvatar,
			ShareCreatedAt: fav.ShareCreatedAt,
			ExpireAt:       fav.ExpireAt,
			FileCount:      fav.FileCount,
			TotalSize:      fav.TotalSize,
			StatsAt:        fav.StatsAt,
			CreatedAt:      backupTime(fav.CreatedAt),
		}
		// The list path deliberately keeps the code sealed; a backup carries it
		// so a restored protected share opens without asking again.
		if full, err := st.GetShareFavorite(ctx, uid, fav.ID); err == nil {
			item.PassCode = full.PassCode
		}
		doc.ShareFavorites = append(doc.ShareFavorites, item)
	}

	videos, err := st.ListVideoFavorites(ctx, uid)
	if err != nil {
		return backupDocument{}, err
	}
	groupNames := map[string]bool{}
	for _, fav := range videos {
		item := backupVideoFavorite{
			ShareID:     fav.ShareID,
			FileID:      fav.FileID,
			Name:        fav.Name,
			Size:        fav.Size,
			Owner:       fav.Owner,
			ShareTitle:  fav.ShareTitle,
			DisplayName: fav.DisplayName,
			GroupLabel:  fav.GroupLabel,
			CreatedAt:   backupTime(fav.CreatedAt),
		}
		if full, err := st.GetVideoFavorite(ctx, uid, fav.ID); err == nil {
			item.PassCode = full.PassCode
		}
		doc.VideoFavorites = append(doc.VideoFavorites, item)
		if label := strings.TrimSpace(fav.GroupLabel); label != "" {
			groupNames[label] = true
		}
	}
	names := make([]string, 0, len(groupNames))
	for name := range groupNames {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		doc.VideoGroups = append(doc.VideoGroups, backupVideoGroup{Name: name})
	}
	return doc, nil
}

// restoreBackup merges favourites and overwrites settings. It validates the
// document before calling it, so a rejected format never leaves a half import.
func restoreBackup(ctx context.Context, deps Deps, uid int, doc backupDocument) (restoreResult, error) {
	st := deps.Store
	result := restoreResult{}

	for key, value := range doc.Settings {
		if _, ok := settingSpecs[key]; !ok {
			continue
		}
		// A download directory grant belongs to the machine the backup came
		// from, so the restore writes the saved path as-is instead of asking
		// this machine's ACL guard about it.
		var err error
		if isGlobalSettingKey(key) {
			err = st.SetSetting(ctx, key, value)
		} else {
			err = st.SetUserSetting(ctx, uid, key, value)
		}
		if err != nil {
			return result, err
		}
		result.SettingsOverwritten++
	}

	for _, item := range doc.ShareFavorites {
		shareID := strings.TrimSpace(item.ShareID)
		if shareID == "" {
			continue
		}
		inserted, err := st.InsertShareFavoriteIfAbsent(ctx, uid, store.ShareFavorite{
			ShareID:        shareID,
			Title:          item.Title,
			DisplayTitle:   item.DisplayTitle,
			OwnerName:      item.OwnerName,
			OwnerAvatar:    item.OwnerAvatar,
			ShareCreatedAt: item.ShareCreatedAt,
			ExpireAt:       item.ExpireAt,
			FileCount:      item.FileCount,
			TotalSize:      item.TotalSize,
			StatsAt:        item.StatsAt,
			PassCode:       item.PassCode,
			CreatedAt:      parseBackupTime(item.CreatedAt),
		})
		if err != nil {
			return result, err
		}
		if inserted {
			result.ShareFavorites.Added++
		} else {
			result.ShareFavorites.Skipped++
		}
	}

	beforeGroups := map[string]bool{}
	local, err := st.ListVideoFavorites(ctx, uid)
	if err != nil {
		return result, err
	}
	for _, fav := range local {
		if label := strings.TrimSpace(fav.GroupLabel); label != "" {
			beforeGroups[label] = true
		}
	}

	addedGroups := map[string]bool{}
	for _, item := range doc.VideoFavorites {
		shareID := strings.TrimSpace(item.ShareID)
		fileID := strings.TrimSpace(item.FileID)
		if shareID == "" || fileID == "" {
			continue
		}
		inserted, err := st.InsertVideoFavoriteIfAbsent(ctx, uid, store.VideoFavorite{
			ShareID:     shareID,
			FileID:      fileID,
			Name:        item.Name,
			Size:        item.Size,
			Owner:       item.Owner,
			ShareTitle:  item.ShareTitle,
			DisplayName: item.DisplayName,
			GroupLabel:  item.GroupLabel,
			PassCode:    item.PassCode,
			CreatedAt:   parseBackupTime(item.CreatedAt),
		})
		if err != nil {
			return result, err
		}
		if inserted {
			result.VideoFavorites.Added++
			if label := strings.TrimSpace(item.GroupLabel); label != "" {
				addedGroups[label] = true
			}
		} else {
			result.VideoFavorites.Skipped++
		}
	}

	// Groups live only as labels on saved videos. A name in the backup is
	// merged when the local list already has it, and added only when an
	// imported video actually brought it in.
	names := map[string]bool{}
	for _, group := range doc.VideoGroups {
		if name := strings.TrimSpace(group.Name); name != "" {
			names[name] = true
		}
	}
	for _, item := range doc.VideoFavorites {
		if name := strings.TrimSpace(item.GroupLabel); name != "" {
			names[name] = true
		}
	}
	for name := range names {
		switch {
		case beforeGroups[name]:
			result.VideoGroups.Merged++
		case addedGroups[name]:
			result.VideoGroups.Added++
		}
	}
	return result, nil
}

func decodeBackup(w http.ResponseWriter, r *http.Request, v any) bool {
	if !requireJSONBody(w, r) {
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "backup_too_large", "备份文件过大，最大支持 8 MiB")
			return false
		}
		writeError(w, http.StatusBadRequest, "bad_json", "备份文件不是合法的 JSON")
		return false
	}
	return true
}

func isGlobalSettingKey(key string) bool {
	if globalOnlySettings[key] {
		return true
	}
	for _, k := range backupGlobalSettingKeys {
		if k == key {
			return true
		}
	}
	return false
}

func backupTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseBackupTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// versionNewer reports whether candidate is a later x.y.z release than current.
// An unparseable version (the dev builds call themselves "test") means the
// comparison is skipped rather than rejecting a usable file.
func versionNewer(candidate, current string) bool {
	a, okA := parseSemver(candidate)
	b, okB := parseSemver(current)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func parseSemver(s string) ([3]int, bool) {
	var out [3]int
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return out, false
	}
	for i := 0; i < len(parts) && i < len(out); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
