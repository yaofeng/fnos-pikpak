package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// legacyFixture is a database in the pre-multi-account shape: every table the
// accounts migration or the Go data migration touches, with all SQL migrations
// that existed before it marked applied. store.Open then runs only the new
// migration itself and hands the data over to migrateData.
const legacyFixture = `
CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
CREATE TABLE users (uid INTEGER PRIMARY KEY, username TEXT NOT NULL DEFAULT '',
  pikpak_sub TEXT NOT NULL DEFAULT '', display_name TEXT NOT NULL DEFAULT '',
  avatar TEXT NOT NULL DEFAULT '', vip_type TEXT NOT NULL DEFAULT '',
  vip_expire TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active',
  bound_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '');
CREATE TABLE credentials (uid INTEGER PRIMARY KEY, auth_type TEXT NOT NULL DEFAULT 'oauth',
  client_id TEXT NOT NULL, access_token_enc BLOB NOT NULL, refresh_token_enc BLOB NOT NULL,
  access_expires_at TEXT NOT NULL, refresh_expires_at TEXT NOT NULL DEFAULT '',
  scopes TEXT NOT NULL DEFAULT '', device_id_enc BLOB NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE granted_folders (uid INTEGER NOT NULL, path TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '', granted_at TEXT NOT NULL, PRIMARY KEY (uid, path));
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE user_settings (uid INTEGER NOT NULL, key TEXT NOT NULL,
  value TEXT NOT NULL, PRIMARY KEY (uid, key));
CREATE TABLE download_jobs (id TEXT PRIMARY KEY, uid INTEGER NOT NULL, file_id TEXT NOT NULL,
  file_name TEXT NOT NULL, dest_dir TEXT NOT NULL, dest_path TEXT NOT NULL,
  size INTEGER NOT NULL DEFAULT 0, downloaded INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL,
  error_code TEXT NOT NULL DEFAULT '', error_message TEXT NOT NULL DEFAULT '',
  link_expire_at TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE upload_jobs (id TEXT PRIMARY KEY, uid INTEGER NOT NULL, local_path TEXT NOT NULL,
  parent_id TEXT NOT NULL, remote_name TEXT NOT NULL, size INTEGER NOT NULL DEFAULT 0,
  uploaded INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL, error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE auth_flows (id TEXT PRIMARY KEY, uid INTEGER NOT NULL, device_code_enc BLOB NOT NULL,
  user_code TEXT NOT NULL, verify_url TEXT NOT NULL, interval_sec INTEGER NOT NULL,
  expires_at TEXT NOT NULL, status TEXT NOT NULL, error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL);
CREATE TABLE share_favorites (id INTEGER PRIMARY KEY AUTOINCREMENT, uid INTEGER NOT NULL,
  share_id TEXT NOT NULL, title TEXT NOT NULL DEFAULT '', pass_code_enc BLOB NOT NULL DEFAULT x'',
  created_at TEXT NOT NULL, display_title TEXT NOT NULL DEFAULT '',
  owner_name TEXT NOT NULL DEFAULT '', owner_avatar TEXT NOT NULL DEFAULT '',
  share_created_at TEXT NOT NULL DEFAULT '', expire_at INTEGER NOT NULL DEFAULT 0,
  file_count INTEGER NOT NULL DEFAULT -1, total_size INTEGER NOT NULL DEFAULT 0,
  stats_at TEXT NOT NULL DEFAULT '', UNIQUE (uid, share_id));
CREATE TABLE video_favorites (id INTEGER PRIMARY KEY AUTOINCREMENT, uid INTEGER NOT NULL,
  share_id TEXT NOT NULL, file_id TEXT NOT NULL, name TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL DEFAULT 0, owner TEXT NOT NULL DEFAULT '',
  share_title TEXT NOT NULL DEFAULT '', display_name TEXT NOT NULL DEFAULT '',
  group_label TEXT NOT NULL DEFAULT '', restored_file_id TEXT NOT NULL DEFAULT '',
  pass_code_enc BLOB NOT NULL DEFAULT x'', created_at TEXT NOT NULL,
  UNIQUE (uid, share_id, file_id));
-- 已应用标记必须数齐当前 migrate.go 的全部既有条目（写本计划时为 16 条；执行前先
-- ` + "`grep -c`" + ` 核对实际条数并同步），本次追加的新迁移不在其中，由迁移器自己跑。
INSERT INTO schema_migrations(version, applied_at) VALUES
 (1,'2026-01-01'),(2,'2026-01-01'),(3,'2026-01-01'),(4,'2026-01-01'),(5,'2026-01-01'),
 (6,'2026-01-01'),(7,'2026-01-01'),(8,'2026-01-01'),(9,'2026-01-01'),(10,'2026-01-01'),
 (11,'2026-01-01'),(12,'2026-01-01'),(13,'2026-01-01'),(14,'2026-01-01'),(15,'2026-01-01'),
 (16,'2026-01-01');
`

// seedLegacyDB writes the old-schema database and one bound user: credential
// sealed under the legacy uid AAD, profile row, and a queued transfer job.
func seedLegacyDB(t *testing.T, dir string) (dbPath, keyPath string) {
	t.Helper()
	dbPath = filepath.Join(dir, "pikpak.db")
	keyPath = filepath.Join(dir, "secret.key")

	key, err := LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(legacyFixture); err != nil {
		t.Fatalf("apply fixture: %v", err)
	}

	enc := func(field, plain string) []byte {
		ct, err := Encrypt(key, legacyAAD(1001, field), []byte(plain))
		if err != nil {
			t.Fatalf("encrypt %s: %v", field, err)
		}
		return ct
	}
	if _, err := db.Exec(`
INSERT INTO credentials(uid, auth_type, client_id, access_token_enc, refresh_token_enc,
  access_expires_at, refresh_expires_at, scopes, device_id_enc, updated_at)
VALUES(1001, 'oauth', 'client', ?, ?, '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z',
  'files:read', ?, '2026-01-01T00:00:00Z')`,
		enc("access_token", "at-old"), enc("refresh_token", "rt-old"), enc("device_id", "dev-old")); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO users(uid, username, pikpak_sub, display_name, avatar, vip_type, vip_expire,
  status, bound_at, updated_at)
VALUES(1001, 'alice', 'sub-legacy', '老账号', 'https://example.com/a.png', 'svip',
  '2026-02-01', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO download_jobs(id, uid, file_id, file_name, dest_dir, dest_path, status,
  created_at, updated_at)
VALUES('job-legacy', 1001, 'file-1', 'movie.mkv', '/vol1/downloads', '/vol1/downloads/movie.mkv',
  'queued', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed download job: %v", err)
	}
	return dbPath, keyPath
}

func openAt(t *testing.T, dbPath, keyPath string) *Store {
	t.Helper()
	s, err := Open(dbPath, keyPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestLegacyDataMigrates(t *testing.T) {
	dir := t.TempDir()
	dbPath, keyPath := seedLegacyDB(t, dir)
	s := openAt(t, dbPath, keyPath)
	ctx := context.Background()

	accounts, err := s.ListAccounts(ctx, 1001)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("accounts = %d rows, want 1: %+v", len(accounts), accounts)
	}
	a := accounts[0]
	if a.DisplayName != "老账号" {
		t.Fatalf("display name = %q, want 老账号", a.DisplayName)
	}
	if a.Status != "active" {
		t.Fatalf("status = %q, want active", a.Status)
	}
	if a.PikPakSub != "sub-legacy" {
		t.Fatalf("sub = %q, want sub-legacy", a.PikPakSub)
	}
	if a.BoundAt.IsZero() {
		t.Fatal("bound_at was not set during migration")
	}

	cred, err := s.GetAccountCredential(ctx, 1001, a.ID)
	if err != nil {
		t.Fatalf("GetAccountCredential: %v", err)
	}
	if cred.AccessToken != "at-old" || cred.RefreshToken != "rt-old" || cred.DeviceID != "dev-old" {
		t.Fatalf("credential did not survive re-encryption: %+v", cred)
	}
	if cred.AccountID != a.ID {
		t.Fatalf("credential account = %q, want %q", cred.AccountID, a.ID)
	}

	job, err := s.GetDownloadJob(ctx, 1001, "job-legacy")
	if err != nil {
		t.Fatalf("GetDownloadJob: %v", err)
	}
	if job.AccountID != a.ID {
		t.Fatalf("job account = %q, want %q", job.AccountID, a.ID)
	}

	if v, err := s.GetSetting(ctx, "data_version"); err != nil || v != "2" {
		t.Fatalf("data_version = %q, %v, want \"2\"", v, err)
	}
}

func TestDataMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath, keyPath := seedLegacyDB(t, dir)
	s := openAt(t, dbPath, keyPath)
	ctx := context.Background()

	first, err := s.ListAccounts(ctx, 1001)
	if err != nil || len(first) != 1 {
		t.Fatalf("first open accounts = %+v, %v", first, err)
	}
	firstID := first[0].ID
	_ = s.Close()

	// A second start must not build a second account or break the tokens.
	s2 := openAt(t, dbPath, keyPath)
	accounts, err := s2.ListAccounts(ctx, 1001)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != firstID {
		t.Fatalf("reopened accounts = %+v, want exactly %q", accounts, firstID)
	}
	cred, err := s2.GetAccountCredential(ctx, 1001, firstID)
	if err != nil || cred.AccessToken != "at-old" {
		t.Fatalf("reopened credential = %+v, %v", cred, err)
	}
}

func TestFreshInstallHasNoAccounts(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	accounts, err := s.ListAccounts(ctx, 1001)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accounts) != 0 {
		t.Fatalf("fresh install has accounts: %+v", accounts)
	}
	if v, err := s.GetSetting(ctx, "data_version"); err != nil || v != "2" {
		t.Fatalf("data_version = %q, %v, want \"2\"", v, err)
	}
}
