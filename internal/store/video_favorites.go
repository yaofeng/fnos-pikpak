package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// VideoFavorite is one video saved from a share. The share pass code is sealed
// at rest so a later "完整播放" can open the share without asking again, and the
// list path never decrypts it.
type VideoFavorite struct {
	ID          int64  `json:"id"`
	UID         int    `json:"-"`
	ShareID     string `json:"share_id"`
	FileID      string `json:"file_id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Owner       string `json:"owner"`
	ShareTitle  string `json:"share_title"`
	DisplayName string `json:"display_name"`
	// GroupLabel groups a saved video under a local name instead of its
	// sharing user; 视频收藏夹 lets the group be renamed and videos dragged.
	GroupLabel string    `json:"group_label"`
	CreatedAt  time.Time `json:"created_at"`

	// RestoredFileID is the copy left in Pack From Shared by an earlier
	// 完整播放; Restored says whether that copy still exists upstream.
	RestoredFileID string `json:"restored_file_id"`
	Restored       bool   `json:"restored"`

	PassCode string `json:"-"`
	encPass  []byte
}

func videoPassAAD(uid int, shareID string) []byte {
	return aadFor(uid, "video_pass_code:"+shareID)
}

// AddVideoFavorite saves or updates one video. An empty pass code leaves the
// stored one in place, and the display name is never touched: only an explicit
// rename changes it.
func (s *Store) AddVideoFavorite(ctx context.Context, uid int, fav VideoFavorite) (VideoFavorite, error) {
	enc := []byte{}
	var err error
	if fav.PassCode != "" {
		enc, err = Encrypt(s.key, videoPassAAD(uid, fav.ShareID), []byte(fav.PassCode))
		if err != nil {
			return VideoFavorite{}, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `
INSERT INTO video_favorites(uid, share_id, file_id, name, size, owner, share_title, pass_code_enc, created_at)
VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(uid, share_id, file_id) DO UPDATE SET
  name=excluded.name,
  size=CASE WHEN excluded.size > 0 THEN excluded.size ELSE video_favorites.size END,
  owner=CASE WHEN excluded.owner <> '' THEN excluded.owner ELSE video_favorites.owner END,
  share_title=CASE WHEN excluded.share_title <> '' THEN excluded.share_title ELSE video_favorites.share_title END,
  pass_code_enc=CASE WHEN length(excluded.pass_code_enc) > 0
    THEN excluded.pass_code_enc ELSE video_favorites.pass_code_enc END`,
		uid, fav.ShareID, fav.FileID, fav.Name, fav.Size, fav.Owner, fav.ShareTitle, enc, now)
	if err != nil {
		return VideoFavorite{}, err
	}
	return s.videoFavoriteByKey(ctx, uid, fav.ShareID, fav.FileID)
}

func (s *Store) videoFavoriteByKey(ctx context.Context, uid int, shareID, fileID string) (VideoFavorite, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, uid, share_id, file_id, name, size, owner, share_title, display_name, group_label, restored_file_id, pass_code_enc, created_at
FROM video_favorites WHERE uid = ? AND share_id = ? AND file_id = ?`, uid, shareID, fileID)
	return scanVideoFavorite(row)
}

// VideoSharePassCode returns the decrypted code of any saved video from this
// share. 视频收藏夹 never opens its shares, so the thumbnail and replay paths
// reopen them with this code. ErrNotFound when no saved video carries one.
func (s *Store) VideoSharePassCode(ctx context.Context, uid int, shareID string) (string, error) {
	var enc []byte
	err := s.db.QueryRowContext(ctx, `
SELECT pass_code_enc FROM video_favorites
WHERE uid = ? AND share_id = ? AND length(pass_code_enc) > 0 LIMIT 1`, uid, shareID).Scan(&enc)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	plain, err := Decrypt(s.key, videoPassAAD(uid, shareID), enc)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// ListVideoFavorites returns the user's saved videos, newest first.
func (s *Store) ListVideoFavorites(ctx context.Context, uid int) ([]VideoFavorite, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, uid, share_id, file_id, name, size, owner, share_title, display_name, group_label, restored_file_id, pass_code_enc, created_at
FROM video_favorites WHERE uid = ? ORDER BY created_at DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []VideoFavorite{}
	for rows.Next() {
		fav, err := scanVideoFavorite(rows)
		if err != nil {
			return nil, err
		}
		fav.PassCode = ""
		out = append(out, fav)
	}
	return out, rows.Err()
}

// GetVideoFavorite returns one saved video with its pass code decrypted.
func (s *Store) GetVideoFavorite(ctx context.Context, uid int, id int64) (VideoFavorite, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, uid, share_id, file_id, name, size, owner, share_title, display_name, group_label, restored_file_id, pass_code_enc, created_at
FROM video_favorites WHERE uid = ? AND id = ?`, uid, id)
	fav, err := scanVideoFavorite(row)
	if err != nil {
		return VideoFavorite{}, err
	}
	if len(fav.encPass) > 0 {
		plain, err := Decrypt(s.key, videoPassAAD(uid, fav.ShareID), fav.encPass)
		if err != nil {
			return VideoFavorite{}, err
		}
		fav.PassCode = string(plain)
	}
	return fav, nil
}

// RenameVideoFavorite changes only the local display name; the share and file
// are untouched. An empty name restores the original one in the UI.
func (s *Store) RenameVideoFavorite(ctx context.Context, uid int, id int64, name string) (VideoFavorite, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE video_favorites SET display_name = ? WHERE uid = ? AND id = ?`, name, uid, id)
	if err != nil {
		return VideoFavorite{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return VideoFavorite{}, err
	}
	if n == 0 {
		return VideoFavorite{}, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx, `
SELECT id, uid, share_id, file_id, name, size, owner, share_title, display_name, group_label, restored_file_id, pass_code_enc, created_at
FROM video_favorites WHERE uid = ? AND id = ?`, uid, id)
	fav, err := scanVideoFavorite(row)
	if err != nil {
		return VideoFavorite{}, err
	}
	fav.PassCode = ""
	return fav, nil
}

func (s *Store) DeleteVideoFavorite(ctx context.Context, uid int, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM video_favorites WHERE uid = ? AND id = ?`, uid, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanVideoFavorite(row rowScanner) (VideoFavorite, error) {
	var fav VideoFavorite
	var created string
	if err := row.Scan(&fav.ID, &fav.UID, &fav.ShareID, &fav.FileID, &fav.Name, &fav.Size,
		&fav.Owner, &fav.ShareTitle, &fav.DisplayName, &fav.GroupLabel, &fav.RestoredFileID, &fav.encPass, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return VideoFavorite{}, ErrNotFound
		}
		return VideoFavorite{}, err
	}
	fav.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return fav, nil
}

// VideoGroupLabel is the group name a saved video shows under: the local
// label if one was chosen, else the sharing user, else the share title, else
// 未知分享. The browser builds the same name.
func VideoGroupLabel(fav VideoFavorite) string {
	if label := strings.TrimSpace(fav.GroupLabel); label != "" {
		return label
	}
	if owner := strings.TrimSpace(fav.Owner); owner != "" {
		return owner
	}
	if title := strings.TrimSpace(fav.ShareTitle); title != "" {
		return title
	}
	return "未知分享"
}

// VideoGroupKey is the group a saved video shows in. It is built from the
// displayed name alone, so videos that happen to share a name — one by its
// sharing user, one filed under that label — are one group.
func VideoGroupKey(fav VideoFavorite) string {
	return "label:" + VideoGroupLabel(fav)
}

// MoveVideoFavoriteGroup files one saved video under a local group label.
func (s *Store) MoveVideoFavoriteGroup(ctx context.Context, uid int, id int64, label string) (VideoFavorite, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE video_favorites SET group_label = ? WHERE uid = ? AND id = ?`, label, uid, id)
	if err != nil {
		return VideoFavorite{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return VideoFavorite{}, err
	}
	if n == 0 {
		return VideoFavorite{}, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx, `
SELECT id, uid, share_id, file_id, name, size, owner, share_title, display_name, group_label, restored_file_id, pass_code_enc, created_at
FROM video_favorites WHERE uid = ? AND id = ?`, uid, id)
	fav, err := scanVideoFavorite(row)
	if err != nil {
		return VideoFavorite{}, err
	}
	fav.PassCode = ""
	return fav, nil
}

// RenameVideoGroup gives every saved video currently grouped under fromKey a
// local label, so the group shows the new name. It returns how many moved.
func (s *Store) RenameVideoGroup(ctx context.Context, uid int, fromKey, label string) (int, error) {
	items, err := s.ListVideoFavorites(ctx, uid)
	if err != nil {
		return 0, err
	}
	ids := make([]int64, 0, len(items))
	for _, f := range items {
		if VideoGroupKey(f) == fromKey {
			ids = append(ids, f.ID)
		}
	}
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE video_favorites SET group_label = ? WHERE uid = ? AND id = ?`, label, uid, id); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// SetVideoFavoriteRestoredID remembers the copy 完整播放 left in Pack From
// Shared, so the next play reuses it instead of restoring again. An empty id
// forgets the copy (it no longer exists upstream).
func (s *Store) SetVideoFavoriteRestoredID(ctx context.Context, uid int, id int64, fileID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE video_favorites SET restored_file_id = ? WHERE uid = ? AND id = ?`, fileID, uid, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// VideoFavoriteFileIDs is the set of share file ids this user saved from one
// share; the share browser flags those videos with 🌟.
func (s *Store) VideoFavoriteFileIDs(ctx context.Context, uid int, shareID string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT file_id FROM video_favorites WHERE uid = ? AND share_id = ?`, uid, shareID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
