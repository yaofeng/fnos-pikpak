package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// InsertShareFavoriteIfAbsent adds a favourite unless this user already saved
// the same share. Restore uses it instead of AddShareFavorite because a
// restore must never overwrite what the local user already has. The returned
// bool reports whether a row was inserted.
func (s *Store) InsertShareFavoriteIfAbsent(ctx context.Context, uid int, fav ShareFavorite) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM share_favorites WHERE uid = ? AND share_id = ?`, uid, fav.ShareID).Scan(&exists)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	enc := []byte{}
	if fav.PassCode != "" {
		enc, err = Encrypt(s.key, sharePassAAD(uid, fav.ShareID), []byte(fav.PassCode))
		if err != nil {
			return false, err
		}
	}
	created := fav.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO share_favorites(uid, share_id, title, display_title, owner_name, owner_avatar,
  share_created_at, expire_at, file_count, total_size, stats_at, pass_code_enc, created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		uid, fav.ShareID, fav.Title, fav.DisplayTitle, fav.OwnerName, fav.OwnerAvatar,
		fav.ShareCreatedAt, fav.ExpireAt, fav.FileCount, fav.TotalSize, fav.StatsAt, enc,
		created.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, err
	}
	return true, nil
}

// InsertVideoFavoriteIfAbsent adds a saved video unless this user already has
// the same entry. PikPak can reissue a file id, so the same share's name and
// size — the pair the listing already uses — count as the same video too.
func (s *Store) InsertVideoFavoriteIfAbsent(ctx context.Context, uid int, fav VideoFavorite) (bool, error) {
	query := `SELECT 1 FROM video_favorites WHERE uid = ? AND share_id = ? AND (file_id = ?`
	args := []any{uid, fav.ShareID, fav.FileID}
	if fav.Name != "" {
		query += ` OR (name = ? AND size = ?)`
		args = append(args, fav.Name, fav.Size)
	}
	query += `) LIMIT 1`

	var exists int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&exists)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	enc := []byte{}
	if fav.PassCode != "" {
		enc, err = Encrypt(s.key, videoPassAAD(uid, fav.ShareID), []byte(fav.PassCode))
		if err != nil {
			return false, err
		}
	}
	created := fav.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	// restored_file_id is deliberately left empty: it names a copy that lived
	// on the machine the backup came from, not on this one.
	_, err = s.db.ExecContext(ctx, `
INSERT INTO video_favorites(uid, share_id, file_id, name, size, owner, share_title, display_name,
  group_label, pass_code_enc, created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		uid, fav.ShareID, fav.FileID, fav.Name, fav.Size, fav.Owner, fav.ShareTitle,
		fav.DisplayName, fav.GroupLabel, enc, created.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, err
	}
	return true, nil
}
