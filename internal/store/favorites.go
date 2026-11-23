package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ShareFavorite is a saved share link. The pass code is sealed at rest and
// only ever decrypted by GetShareFavorite, so a list response cannot leak it.
type ShareFavorite struct {
	ID      int64  `json:"id"`
	UID     int    `json:"-"`
	ShareID string `json:"share_id"`
	Title   string `json:"title"`
	// DisplayTitle renames the favourite locally. PikPak knows nothing about
	// it, so the real share title stays in Title.
	DisplayTitle string    `json:"display_title"`
	Protected    bool      `json:"protected"`
	CreatedAt    time.Time `json:"created_at"`

	// Metadata PikPak reports when the share is opened: who shared it, when,
	// whether it expires, and the totals a recursive walk counted.
	OwnerName      string `json:"owner_name"`
	OwnerAvatar    string `json:"owner_avatar"`
	ShareCreatedAt string `json:"share_created_at"`
	// ExpireAt is unix seconds; -1 permanent, 0 unknown.
	ExpireAt int64 `json:"expire_at"`
	// FileCount is -1 until the totals have been counted; TotalSize follows.
	FileCount  int64  `json:"file_count"`
	TotalSize  int64  `json:"total_size"`
	StatsAt    string `json:"-"`
	StatsReady bool   `json:"stats_checked"`

	PassCode string `json:"-"`
	encPass  []byte
}

func sharePassAAD(uid int, shareID string) []byte {
	return aadFor(uid, "share_pass_code:"+shareID)
}

// AddShareFavorite saves or updates the favourite for one share. An empty pass
// code leaves the stored one in place: the code is only needed once, and a
// second save from an already-open share must not erase it.
func (s *Store) AddShareFavorite(ctx context.Context, uid int, shareID, title, passCode string) (ShareFavorite, error) {
	enc := []byte{}
	var err error
	if passCode != "" {
		enc, err = Encrypt(s.key, sharePassAAD(uid, shareID), []byte(passCode))
		if err != nil {
			return ShareFavorite{}, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `
INSERT INTO share_favorites(uid, share_id, title, pass_code_enc, created_at)
VALUES(?,?,?,?,?)
ON CONFLICT(uid, share_id) DO UPDATE SET
  title=excluded.title,
  pass_code_enc=CASE WHEN length(excluded.pass_code_enc) > 0
    THEN excluded.pass_code_enc ELSE share_favorites.pass_code_enc END`,
		uid, shareID, title, enc, now)
	if err != nil {
		return ShareFavorite{}, err
	}
	return s.shareFavoriteByShareID(ctx, uid, shareID)
}

func (s *Store) shareFavoriteByShareID(ctx context.Context, uid int, shareID string) (ShareFavorite, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, uid, share_id, title, display_title, owner_name, owner_avatar, share_created_at, expire_at, file_count, total_size, stats_at, pass_code_enc, created_at
FROM share_favorites WHERE uid = ? AND share_id = ?`, uid, shareID)
	return scanShareFavorite(row)
}

// ShareFavoriteByShareID finds the row a share was saved under, so opening the
// share can refresh its cached statistics. ErrNotFound when it is not saved.
func (s *Store) ShareFavoriteByShareID(ctx context.Context, uid int, shareID string) (ShareFavorite, error) {
	return s.shareFavoriteByShareID(ctx, uid, shareID)
}

// ListShareFavorites returns the user's favourites, newest first.
func (s *Store) ListShareFavorites(ctx context.Context, uid int) ([]ShareFavorite, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, uid, share_id, title, display_title, owner_name, owner_avatar, share_created_at, expire_at, file_count, total_size, stats_at, pass_code_enc, created_at
FROM share_favorites WHERE uid = ? ORDER BY created_at DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShareFavorite{}
	for rows.Next() {
		fav, err := scanShareFavorite(rows)
		if err != nil {
			return nil, err
		}
		// The list is a read path: the code stays sealed.
		fav.PassCode = ""
		out = append(out, fav)
	}
	return out, rows.Err()
}

// GetShareFavorite returns one favourite with its pass code decrypted.
func (s *Store) GetShareFavorite(ctx context.Context, uid int, id int64) (ShareFavorite, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, uid, share_id, title, display_title, owner_name, owner_avatar, share_created_at, expire_at, file_count, total_size, stats_at, pass_code_enc, created_at
FROM share_favorites WHERE uid = ? AND id = ?`, uid, id)
	fav, err := scanShareFavorite(row)
	if err != nil {
		return ShareFavorite{}, err
	}
	if len(fav.encPass) > 0 {
		plain, err := Decrypt(s.key, sharePassAAD(uid, fav.ShareID), fav.encPass)
		if err != nil {
			return ShareFavorite{}, err
		}
		fav.PassCode = string(plain)
	}
	return fav, nil
}

// SharePassCode returns the decrypted code saved with a share favourite. A
// video starred from an already-favourited share inherited no code (the
// browser never saw it), so it takes the code from here.
func (s *Store) SharePassCode(ctx context.Context, uid int, shareID string) (string, error) {
	fav, err := s.shareFavoriteByShareID(ctx, uid, shareID)
	if err != nil {
		return "", err
	}
	if len(fav.encPass) == 0 {
		return "", ErrNotFound
	}
	plain, err := Decrypt(s.key, sharePassAAD(uid, shareID), fav.encPass)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// RenameShareFavorite changes only the local display title. An empty title
// restores the share's own name.
func (s *Store) RenameShareFavorite(ctx context.Context, uid int, id int64, title string) (ShareFavorite, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE share_favorites SET display_title = ? WHERE uid = ? AND id = ?`, title, uid, id)
	if err != nil {
		return ShareFavorite{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return ShareFavorite{}, err
	}
	if n == 0 {
		return ShareFavorite{}, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx, `
SELECT id, uid, share_id, title, display_title, owner_name, owner_avatar, share_created_at, expire_at, file_count, total_size, stats_at, pass_code_enc, created_at
FROM share_favorites WHERE uid = ? AND id = ?`, uid, id)
	fav, err := scanShareFavorite(row)
	if err != nil {
		return ShareFavorite{}, err
	}
	fav.PassCode = ""
	return fav, nil
}

func (s *Store) DeleteShareFavorite(ctx context.Context, uid int, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM share_favorites WHERE uid = ? AND id = ?`, uid, id)
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

type rowScanner interface {
	Scan(dest ...any) error
}

func scanShareFavorite(row rowScanner) (ShareFavorite, error) {
	var fav ShareFavorite
	var created string
	if err := row.Scan(&fav.ID, &fav.UID, &fav.ShareID, &fav.Title, &fav.DisplayTitle,
		&fav.OwnerName, &fav.OwnerAvatar, &fav.ShareCreatedAt, &fav.ExpireAt,
		&fav.FileCount, &fav.TotalSize, &fav.StatsAt, &fav.encPass, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ShareFavorite{}, ErrNotFound
		}
		return ShareFavorite{}, err
	}
	fav.Protected = len(fav.encPass) > 0
	fav.StatsReady = fav.StatsAt != ""
	fav.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return fav, nil
}

// MoveShareFavoriteShare rewrites a favorite saved under an old key — a
// pasted link, from before links were parsed on add — to its share id, and
// re-seals the code under the new key. When that share id is already
// favorited the stale row is dropped and ErrNotFound returned.
func (s *Store) MoveShareFavoriteShare(ctx context.Context, uid int, id int64, shareID, title, passCode string) (ShareFavorite, error) {
	enc := []byte{}
	var err error
	if passCode != "" {
		enc, err = Encrypt(s.key, sharePassAAD(uid, shareID), []byte(passCode))
		if err != nil {
			return ShareFavorite{}, err
		}
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE share_favorites SET share_id = ?, title = ?,
  pass_code_enc = CASE WHEN length(?) > 0 THEN ? ELSE pass_code_enc END
WHERE uid = ? AND id = ?`, shareID, title, enc, enc, uid, id)
	if err != nil {
		// The share id is already favorited: the old row is redundant.
		if _, delErr := s.db.ExecContext(ctx,
			`DELETE FROM share_favorites WHERE uid = ? AND id = ?`, uid, id); delErr != nil {
			return ShareFavorite{}, err
		}
		return ShareFavorite{}, ErrNotFound
	}
	n, err := res.RowsAffected()
	if err != nil {
		return ShareFavorite{}, err
	}
	if n == 0 {
		return ShareFavorite{}, ErrNotFound
	}
	return s.shareFavoriteByShareID(ctx, uid, shareID)
}

// ShareFavoriteMeta is what opening a share taught us: who shared it, when,
// whether it expires, and the totals a recursive count produced.
type ShareFavoriteMeta struct {
	OwnerName      string
	OwnerAvatar    string
	ShareCreatedAt string
	ExpireAt       int64
	// FileCount and TotalSize are -1 when unknown; StatsAt is set once a
	// count was attempted, successful or not.
	FileCount int64
	TotalSize int64
	StatsAt   string
}

// UpdateShareFavoriteMeta fills in what PikPak reported, keeping whatever was
// stored for fields this response omits.
func (s *Store) UpdateShareFavoriteMeta(ctx context.Context, uid int, id int64, m ShareFavoriteMeta) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE share_favorites SET
  owner_name = CASE WHEN ? <> '' THEN ? ELSE owner_name END,
  owner_avatar = CASE WHEN ? <> '' THEN ? ELSE owner_avatar END,
  share_created_at = CASE WHEN ? <> '' THEN ? ELSE share_created_at END,
  expire_at = CASE WHEN ? <> 0 THEN ? ELSE expire_at END,
  file_count = CASE WHEN ? >= 0 THEN ? ELSE file_count END,
  total_size = CASE WHEN ? >= 0 THEN ? ELSE total_size END,
  stats_at = CASE WHEN ? <> '' THEN ? ELSE stats_at END
WHERE uid = ? AND id = ?`,
		m.OwnerName, m.OwnerName,
		m.OwnerAvatar, m.OwnerAvatar,
		m.ShareCreatedAt, m.ShareCreatedAt,
		m.ExpireAt, m.ExpireAt,
		m.FileCount, m.FileCount,
		m.TotalSize, m.TotalSize,
		m.StatsAt, m.StatsAt,
		uid, id)
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
