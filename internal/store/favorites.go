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
SELECT id, uid, share_id, title, display_title, pass_code_enc, created_at
FROM share_favorites WHERE uid = ? AND share_id = ?`, uid, shareID)
	return scanShareFavorite(row)
}

// ListShareFavorites returns the user's favourites, newest first.
func (s *Store) ListShareFavorites(ctx context.Context, uid int) ([]ShareFavorite, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, uid, share_id, title, display_title, pass_code_enc, created_at
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
SELECT id, uid, share_id, title, display_title, pass_code_enc, created_at
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
SELECT id, uid, share_id, title, display_title, pass_code_enc, created_at
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
	if err := row.Scan(&fav.ID, &fav.UID, &fav.ShareID, &fav.Title, &fav.DisplayTitle, &fav.encPass, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ShareFavorite{}, ErrNotFound
		}
		return ShareFavorite{}, err
	}
	fav.Protected = len(fav.encPass) > 0
	fav.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return fav, nil
}
