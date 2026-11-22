package store

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("store: not found")

// Credential is the per-user PikPak session. Token fields are plaintext in
// memory and encrypted at rest.
type Credential struct {
	UID              int
	AuthType         string
	ClientID         string
	AccessToken      string
	RefreshToken     string
	DeviceID         string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
	Scopes           string
}

type Flow struct {
	ID          string
	UID         int
	DeviceCode  string
	UserCode    string
	VerifyURL   string
	IntervalSec int
	ExpiresAt   time.Time
	Status      string
	Error       string
	CreatedAt   time.Time
}

// User is the fnOS user's account record. It holds display data only; the
// PikPak tokens live in credentials.
type User struct {
	UID         int
	Username    string
	PikPakSub   string
	DisplayName string
	Avatar      string
	VIPType     string
	VIPExpire   string
	Status      string
	BoundAt     time.Time
	UpdatedAt   time.Time
}

type GrantedFolder struct {
	UID       int
	Path      string
	Label     string
	GrantedAt time.Time
}

type Store struct {
	db   *sql.DB
	key  []byte
	path string
}

func Open(dbPath, keyPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	key, err := LoadOrCreateKey(keyPath)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(0)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	s := &Store{db: db, key: key, path: dbPath}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(dbPath, 0o600); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// LoadOrCreateKey returns the 32-byte encryption key, creating it with 0600 on
// first run. Losing this file means every user must re-bind.
// SecretKey returns a copy of the master key. Callers derive purpose-specific
// keys from it (the play-token secret, for example) instead of sharing the
// credential-encryption key itself.
func (s *Store) SecretKey() []byte {
	out := make([]byte, len(s.key))
	copy(out, s.key)
	return out
}

func LoadOrCreateKey(path string) ([]byte, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}

	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != 32 {
			return nil, fmt.Errorf("store: %s has %d bytes, want 32", path, len(key))
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return LoadOrCreateKey(path)
		}
		return nil, err
	}
	defer f.Close()

	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if _, err := f.Write(key); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return err
	}

	for i, m := range migrations {
		version := i + 1
		var exists int
		err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&exists)
		if err != nil {
			return err
		}
		if exists > 0 {
			continue
		}

		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: migration %d: %w", version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`,
			version, time.Now().Format(time.RFC3339)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) PutCredential(ctx context.Context, c Credential) error {
	access, err := Encrypt(s.key, aadFor(c.UID, "access_token"), []byte(c.AccessToken))
	if err != nil {
		return err
	}
	refresh, err := Encrypt(s.key, aadFor(c.UID, "refresh_token"), []byte(c.RefreshToken))
	if err != nil {
		return err
	}
	device, err := Encrypt(s.key, aadFor(c.UID, "device_id"), []byte(c.DeviceID))
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO credentials(uid, auth_type, client_id, access_token_enc, refresh_token_enc,
  access_expires_at, refresh_expires_at, scopes, device_id_enc, updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(uid) DO UPDATE SET
  auth_type=excluded.auth_type,
  client_id=excluded.client_id,
  access_token_enc=excluded.access_token_enc,
  refresh_token_enc=excluded.refresh_token_enc,
  access_expires_at=excluded.access_expires_at,
  refresh_expires_at=excluded.refresh_expires_at,
  scopes=excluded.scopes,
  device_id_enc=excluded.device_id_enc,
  updated_at=excluded.updated_at`,
		c.UID, c.AuthType, c.ClientID, access, refresh,
		formatTime(c.AccessExpiresAt), formatTime(c.RefreshExpiresAt), c.Scopes, device,
		time.Now().Format(time.RFC3339))
	return err
}

func (s *Store) GetCredential(ctx context.Context, uid int) (Credential, error) {
	var (
		c                          Credential
		access, refresh, device    []byte
		accessExp, refreshExp, upd string
	)
	err := s.db.QueryRowContext(ctx, `
SELECT uid, auth_type, client_id, access_token_enc, refresh_token_enc,
       access_expires_at, refresh_expires_at, scopes, device_id_enc, updated_at
FROM credentials WHERE uid = ?`, uid).
		Scan(&c.UID, &c.AuthType, &c.ClientID, &access, &refresh,
			&accessExp, &refreshExp, &c.Scopes, &device, &upd)
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, ErrNotFound
	}
	if err != nil {
		return Credential{}, err
	}

	pt, err := Decrypt(s.key, aadFor(uid, "access_token"), access)
	if err != nil {
		return Credential{}, err
	}
	c.AccessToken = string(pt)

	pt, err = Decrypt(s.key, aadFor(uid, "refresh_token"), refresh)
	if err != nil {
		return Credential{}, err
	}
	c.RefreshToken = string(pt)

	pt, err = Decrypt(s.key, aadFor(uid, "device_id"), device)
	if err != nil {
		return Credential{}, err
	}
	c.DeviceID = string(pt)

	c.AccessExpiresAt = parseTime(accessExp)
	c.RefreshExpiresAt = parseTime(refreshExp)
	return c, nil
}

func (s *Store) PutAuthFlow(ctx context.Context, f Flow) error {
	device, err := Encrypt(s.key, []byte("flow:"+f.ID), []byte(f.DeviceCode))
	if err != nil {
		return err
	}
	created := f.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO auth_flows(id, uid, device_code_enc, user_code, verify_url, interval_sec, expires_at, status, error, created_at)
VALUES(?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  device_code_enc=excluded.device_code_enc,
  user_code=excluded.user_code,
  verify_url=excluded.verify_url,
  interval_sec=excluded.interval_sec,
  expires_at=excluded.expires_at,
  status=excluded.status,
  error=excluded.error`,
		f.ID, f.UID, device, f.UserCode, f.VerifyURL, f.IntervalSec,
		formatTime(f.ExpiresAt), f.Status, f.Error, created.Format(time.RFC3339))
	return err
}

func (s *Store) GetAuthFlow(ctx context.Context, id string) (Flow, error) {
	var (
		f        Flow
		device   []byte
		exp, cre string
	)
	err := s.db.QueryRowContext(ctx, `
SELECT id, uid, device_code_enc, user_code, verify_url, interval_sec, expires_at, status, error, created_at
FROM auth_flows WHERE id = ?`, id).
		Scan(&f.ID, &f.UID, &device, &f.UserCode, &f.VerifyURL, &f.IntervalSec, &exp, &f.Status, &f.Error, &cre)
	if errors.Is(err, sql.ErrNoRows) {
		return Flow{}, ErrNotFound
	}
	if err != nil {
		return Flow{}, err
	}

	pt, err := Decrypt(s.key, []byte("flow:"+id), device)
	if err != nil {
		return Flow{}, err
	}
	f.DeviceCode = string(pt)
	f.ExpiresAt = parseTime(exp)
	f.CreatedAt = parseTime(cre)
	return f, nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// DeleteCredential unbinds a user's PikPak session. Missing rows are not an
// error: the caller's intent (no credential) already holds.
func (s *Store) DeleteCredential(ctx context.Context, uid int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM credentials WHERE uid = ?`, uid)
	return err
}

// DeriveDeviceID returns a stable, per-user PikPak device id. It is derived
// from the store key so it never needs to be persisted before the first bind
// and cannot be guessed from a uid alone.
func (s *Store) DeriveDeviceID(uid int) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte("device:" + strconv.Itoa(uid)))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

// CancelPendingFlows marks every still-pending authorization flow of a user as
// cancelled, so a poller that finishes after logout cannot bind the account.
func (s *Store) CancelPendingFlows(ctx context.Context, uid int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE auth_flows SET status = 'cancelled' WHERE uid = ? AND status = 'pending'`, uid)
	return err
}

// PutUser upserts the display record for a bound account. bound_at is set on
// the first write and kept on later ones, so the UI can show when the user
// first linked the account.
func (s *Store) PutUser(ctx context.Context, u User) error {
	if u.Status == "" {
		u.Status = "active"
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO users(uid, username, pikpak_sub, display_name, avatar, vip_type, vip_expire,
  status, bound_at, updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(uid) DO UPDATE SET
  username=excluded.username,
  pikpak_sub=excluded.pikpak_sub,
  display_name=excluded.display_name,
  avatar=excluded.avatar,
  vip_type=excluded.vip_type,
  vip_expire=excluded.vip_expire,
  status=excluded.status,
  bound_at=CASE WHEN users.bound_at = '' THEN excluded.bound_at ELSE users.bound_at END,
  updated_at=excluded.updated_at`,
		u.UID, u.Username, u.PikPakSub, u.DisplayName, u.Avatar,
		u.VIPType, u.VIPExpire, u.Status,
		now.Format(time.RFC3339), now.Format(time.RFC3339))
	return err
}

func (s *Store) GetUser(ctx context.Context, uid int) (User, error) {
	var (
		u              User
		bound, updated string
	)
	err := s.db.QueryRowContext(ctx, `
SELECT uid, username, pikpak_sub, display_name, avatar, vip_type, vip_expire,
       status, bound_at, updated_at
FROM users WHERE uid = ?`, uid).
		Scan(&u.UID, &u.Username, &u.PikPakSub, &u.DisplayName, &u.Avatar,
			&u.VIPType, &u.VIPExpire, &u.Status, &bound, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u.BoundAt = parseTime(bound)
	u.UpdatedAt = parseTime(updated)
	return u, nil
}

// SetUserStatus flips active / reauth_required / disabled without touching the
// stored profile, so a token failure does not erase who the user is. A user
// known only by its credential gets a row, because the shell needs the status
// to show the reauthorization banner.
func (s *Store) SetUserStatus(ctx context.Context, uid int, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO users(uid, status, bound_at, updated_at) VALUES(?,?,?,?)
ON CONFLICT(uid) DO UPDATE SET status=excluded.status, updated_at=excluded.updated_at`,
		uid, status, now, now)
	return err
}

// GrantFolder records a directory the user authorized. Re-granting updates the
// label but keeps the original granted_at.
func (s *Store) GrantFolder(ctx context.Context, uid int, path, label string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO granted_folders(uid, path, label, granted_at)
VALUES(?,?,?,?)
ON CONFLICT(uid, path) DO UPDATE SET label=excluded.label`,
		uid, path, label, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) ListGrantedFolders(ctx context.Context, uid int) ([]GrantedFolder, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT uid, path, label, granted_at FROM granted_folders
WHERE uid = ? ORDER BY path`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GrantedFolder
	for rows.Next() {
		var (
			f       GrantedFolder
			granted string
		)
		if err := rows.Scan(&f.UID, &f.Path, &f.Label, &granted); err != nil {
			return nil, err
		}
		f.GrantedAt = parseTime(granted)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) RevokeFolder(ctx context.Context, uid int, path string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM granted_folders WHERE uid = ? AND path = ?`, uid, path)
	return err
}

const maxSettingValue = 1024

func validateSetting(key, value string) error {
	if key == "" {
		return errors.New("store: setting key is empty")
	}
	if len(value) > maxSettingValue {
		return fmt.Errorf("store: setting %q has %d bytes, limit is %d", key, len(value), maxSettingValue)
	}
	return nil
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	if err := validateSetting(key, value); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO settings(key, value) VALUES(?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Store) SetUserSetting(ctx context.Context, uid int, key, value string) error {
	if err := validateSetting(key, value); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO user_settings(uid, key, value) VALUES(?,?,?)
ON CONFLICT(uid, key) DO UPDATE SET value=excluded.value`, uid, key, value)
	return err
}

func (s *Store) GetUserSetting(ctx context.Context, uid int, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM user_settings WHERE uid = ? AND key = ?`, uid, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}
