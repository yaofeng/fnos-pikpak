package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// PikPakAccount is one PikPak identity bound to an fnOS user. ID is generated
// when the bind flow starts and never changes; the display data is refreshed
// from the upstream profile on every bind or re-authorization.
type PikPakAccount struct {
	ID          string
	UID         int
	PikPakSub   string
	DisplayName string
	Avatar      string
	VIPType     string
	VIPExpire   string
	Status      string
	BoundAt     time.Time
	UpdatedAt   time.Time
}

// NewAccountID returns a fresh 16-hex-char account id.
func NewAccountID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// PutAccount upserts the display record for a bound PikPak account. bound_at
// is set on the first write — from a.BoundAt when the caller gives one,
// otherwise now — and kept afterwards, so the UI can show when the account
// was first linked.
func (s *Store) PutAccount(ctx context.Context, a PikPakAccount) error {
	if a.Status == "" {
		a.Status = "active"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	bound := now
	if !a.BoundAt.IsZero() {
		bound = a.BoundAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO pikpak_accounts(id, uid, pikpak_sub, display_name, avatar, vip_type, vip_expire,
  status, bound_at, updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  pikpak_sub=excluded.pikpak_sub,
  display_name=excluded.display_name,
  avatar=excluded.avatar,
  vip_type=excluded.vip_type,
  vip_expire=excluded.vip_expire,
  status=excluded.status,
  bound_at=CASE WHEN pikpak_accounts.bound_at = '' THEN excluded.bound_at ELSE pikpak_accounts.bound_at END,
  updated_at=excluded.updated_at`,
		a.ID, a.UID, a.PikPakSub, a.DisplayName, a.Avatar, a.VIPType, a.VIPExpire,
		a.Status, bound, now)
	return err
}

func (s *Store) GetAccount(ctx context.Context, uid int, id string) (PikPakAccount, error) {
	var (
		a              PikPakAccount
		bound, updated string
	)
	err := s.db.QueryRowContext(ctx, `
SELECT uid, pikpak_sub, display_name, avatar, vip_type, vip_expire, status, bound_at, updated_at
FROM pikpak_accounts WHERE id = ? AND uid = ?`, id, uid).
		Scan(&a.UID, &a.PikPakSub, &a.DisplayName, &a.Avatar, &a.VIPType, &a.VIPExpire,
			&a.Status, &bound, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return PikPakAccount{}, ErrNotFound
	}
	if err != nil {
		return PikPakAccount{}, err
	}
	a.ID = id
	a.BoundAt = parseTime(bound)
	a.UpdatedAt = parseTime(updated)
	return a, nil
}

// ListAccounts returns the user's accounts oldest-bound first, which is the
// order the default-account fallback and the switcher present them in.
func (s *Store) ListAccounts(ctx context.Context, uid int) ([]PikPakAccount, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, uid, pikpak_sub, display_name, avatar, vip_type, vip_expire, status, bound_at, updated_at
FROM pikpak_accounts WHERE uid = ? ORDER BY bound_at`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []PikPakAccount{}
	for rows.Next() {
		var (
			a              PikPakAccount
			bound, updated string
		)
		if err := rows.Scan(&a.ID, &a.UID, &a.PikPakSub, &a.DisplayName, &a.Avatar,
			&a.VIPType, &a.VIPExpire, &a.Status, &bound, &updated); err != nil {
			return nil, err
		}
		a.BoundAt = parseTime(bound)
		a.UpdatedAt = parseTime(updated)
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetAccountStatus flips active / reauth_required / disabled without touching
// the stored profile, so a token failure does not erase who the account is.
// An account known only by its status gets a row, because the shell needs the
// status to show the reauthorization banner.
func (s *Store) SetAccountStatus(ctx context.Context, uid int, id, status string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx,
		`UPDATE pikpak_accounts SET status = ?, updated_at = ? WHERE uid = ? AND id = ?`,
		status, now, uid, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO pikpak_accounts(uid, status, bound_at, updated_at) VALUES(?,?,?,?)`,
		uid, status, now, now)
	return err
}

func (s *Store) DeleteAccount(ctx context.Context, uid int, id string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM pikpak_accounts WHERE uid = ? AND id = ?`, uid, id)
	return err
}

// AccountIDBySub resolves a PikPak subject (the stable upstream user id) to
// the account row it was bound under.
func (s *Store) AccountIDBySub(ctx context.Context, uid int, sub string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM pikpak_accounts WHERE uid = ? AND pikpak_sub = ? LIMIT 1`, uid, sub).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// CancelAccountFlows marks the account's still-pending authorization flows as
// cancelled, so a poller that finishes after unbind cannot bind the account.
// An empty accountID cancels every pending flow of the user.
func (s *Store) CancelAccountFlows(ctx context.Context, uid int, accountID string) error {
	if accountID == "" {
		_, err := s.db.ExecContext(ctx,
			`UPDATE auth_flows SET status = 'cancelled' WHERE uid = ? AND status = 'pending'`, uid)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE auth_flows SET status = 'cancelled' WHERE uid = ? AND account_id = ? AND status = 'pending'`,
		uid, accountID)
	return err
}
