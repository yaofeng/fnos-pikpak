package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// VideoFavorite is one video saved from a share. The share pass code is sealed
// at rest so a later "完整播放" can open the share without asking again, and the
// list path never decrypts it.
type VideoFavorite struct {
	ID          int64     `json:"id"`
	UID         int       `json:"-"`
	ShareID     string    `json:"share_id"`
	FileID      string    `json:"file_id"`
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	Owner       string    `json:"owner"`
	ShareTitle  string    `json:"share_title"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`

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
  size=excluded.size,
  owner=excluded.owner,
  share_title=excluded.share_title,
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
SELECT id, uid, share_id, file_id, name, size, owner, share_title, display_name, pass_code_enc, created_at
FROM video_favorites WHERE uid = ? AND share_id = ? AND file_id = ?`, uid, shareID, fileID)
	return scanVideoFavorite(row)
}

// ListVideoFavorites returns the user's saved videos, newest first.
func (s *Store) ListVideoFavorites(ctx context.Context, uid int) ([]VideoFavorite, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, uid, share_id, file_id, name, size, owner, share_title, display_name, pass_code_enc, created_at
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
SELECT id, uid, share_id, file_id, name, size, owner, share_title, display_name, pass_code_enc, created_at
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
SELECT id, uid, share_id, file_id, name, size, owner, share_title, display_name, pass_code_enc, created_at
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
		&fav.Owner, &fav.ShareTitle, &fav.DisplayName, &fav.encPass, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return VideoFavorite{}, ErrNotFound
		}
		return VideoFavorite{}, err
	}
	fav.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return fav, nil
}
