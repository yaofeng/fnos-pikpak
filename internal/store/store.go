package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

type Store struct {
	db  *sql.DB
	key []byte
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

	s := &Store{db: db, key: key}
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
