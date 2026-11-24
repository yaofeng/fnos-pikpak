package store

// migrations are applied in order inside a transaction. Never edit an applied
// migration; add a new one.
var migrations = []string{
	`
CREATE TABLE users (
  uid           INTEGER PRIMARY KEY,
  username      TEXT NOT NULL DEFAULT '',
  pikpak_sub    TEXT NOT NULL DEFAULT '',
  display_name  TEXT NOT NULL DEFAULT '',
  avatar        TEXT NOT NULL DEFAULT '',
  vip_type      TEXT NOT NULL DEFAULT '',
  vip_expire    TEXT NOT NULL DEFAULT '',
  status        TEXT NOT NULL DEFAULT 'active',
  bound_at      TEXT NOT NULL DEFAULT '',
  updated_at    TEXT NOT NULL DEFAULT ''
);

CREATE TABLE credentials (
  uid                 INTEGER PRIMARY KEY,
  auth_type           TEXT NOT NULL DEFAULT 'oauth',
  client_id           TEXT NOT NULL,
  access_token_enc    BLOB NOT NULL,
  refresh_token_enc   BLOB NOT NULL,
  access_expires_at   TEXT NOT NULL,
  refresh_expires_at  TEXT NOT NULL DEFAULT '',
  scopes              TEXT NOT NULL DEFAULT '',
  device_id_enc       BLOB NOT NULL,
  updated_at          TEXT NOT NULL
);

CREATE TABLE auth_flows (
  id               TEXT PRIMARY KEY,
  uid              INTEGER NOT NULL,
  device_code_enc  BLOB NOT NULL,
  user_code        TEXT NOT NULL,
  verify_url       TEXT NOT NULL,
  interval_sec     INTEGER NOT NULL,
  expires_at       TEXT NOT NULL,
  status           TEXT NOT NULL,
  error            TEXT NOT NULL DEFAULT '',
  created_at       TEXT NOT NULL
);
`,
	`
CREATE TABLE granted_folders (
  uid        INTEGER NOT NULL,
  path       TEXT NOT NULL,
  label      TEXT NOT NULL DEFAULT '',
  granted_at TEXT NOT NULL,
  PRIMARY KEY (uid, path)
);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE user_settings (
  uid   INTEGER NOT NULL,
  key   TEXT NOT NULL,
  value TEXT NOT NULL,
  PRIMARY KEY (uid, key)
);
`,
	`
CREATE TABLE download_jobs (
  id             TEXT PRIMARY KEY,
  uid            INTEGER NOT NULL,
  file_id        TEXT NOT NULL,
  file_name      TEXT NOT NULL,
  dest_dir       TEXT NOT NULL,
  dest_path      TEXT NOT NULL,
  size           INTEGER NOT NULL DEFAULT 0,
  downloaded     INTEGER NOT NULL DEFAULT 0,
  status         TEXT NOT NULL,
  error_code     TEXT NOT NULL DEFAULT '',
  error_message  TEXT NOT NULL DEFAULT '',
  link_expire_at TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL
);
CREATE INDEX idx_download_jobs_uid_status ON download_jobs(uid, status);
`,
	`
CREATE TABLE upload_jobs (
  id            TEXT PRIMARY KEY,
  uid           INTEGER NOT NULL,
  local_path    TEXT NOT NULL,
  parent_id     TEXT NOT NULL,
  remote_name   TEXT NOT NULL,
  size          INTEGER NOT NULL DEFAULT 0,
  uploaded      INTEGER NOT NULL DEFAULT 0,
  status        TEXT NOT NULL,
  error_code    TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
CREATE INDEX idx_upload_jobs_uid_status ON upload_jobs(uid, status);
`,
	`
CREATE TABLE share_favorites (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  uid           INTEGER NOT NULL,
  share_id      TEXT NOT NULL,
  title         TEXT NOT NULL DEFAULT '',
  pass_code_enc BLOB NOT NULL DEFAULT x'',
  created_at    TEXT NOT NULL,
  UNIQUE (uid, share_id)
);
CREATE INDEX idx_share_favorites_uid ON share_favorites(uid, created_at);
`,
	`
CREATE TABLE video_favorites (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  uid           INTEGER NOT NULL,
  share_id      TEXT NOT NULL,
  file_id       TEXT NOT NULL,
  name          TEXT NOT NULL DEFAULT '',
  size          INTEGER NOT NULL DEFAULT 0,
  owner         TEXT NOT NULL DEFAULT '',
  share_title   TEXT NOT NULL DEFAULT '',
  display_name  TEXT NOT NULL DEFAULT '',
  pass_code_enc BLOB NOT NULL DEFAULT x'',
  created_at    TEXT NOT NULL,
  UNIQUE (uid, share_id, file_id)
);
CREATE INDEX idx_video_favorites_uid ON video_favorites(uid, created_at);
`,
	`
ALTER TABLE share_favorites ADD COLUMN display_title TEXT NOT NULL DEFAULT '';
`,
	`
ALTER TABLE video_favorites ADD COLUMN group_label TEXT NOT NULL DEFAULT '';
`,
	`
ALTER TABLE video_favorites ADD COLUMN restored_file_id TEXT NOT NULL DEFAULT '';
`,
	`
ALTER TABLE share_favorites ADD COLUMN owner_name TEXT NOT NULL DEFAULT '';
`,
	`
ALTER TABLE share_favorites ADD COLUMN owner_avatar TEXT NOT NULL DEFAULT '';
`,
	`
ALTER TABLE share_favorites ADD COLUMN share_created_at TEXT NOT NULL DEFAULT '';
`,
	`
ALTER TABLE share_favorites ADD COLUMN expire_at INTEGER NOT NULL DEFAULT 0;
`,
	`
ALTER TABLE share_favorites ADD COLUMN file_count INTEGER NOT NULL DEFAULT -1;
`,
	`
ALTER TABLE share_favorites ADD COLUMN total_size INTEGER NOT NULL DEFAULT 0;
`,
	`
ALTER TABLE share_favorites ADD COLUMN stats_at TEXT NOT NULL DEFAULT '';
`,
	`
-- Multi-account: one fnOS user may bind several PikPak accounts. The old
-- single-row tables keep their data until migrateData carries it over.
CREATE TABLE pikpak_accounts (
  id           TEXT PRIMARY KEY,          -- 16 hex 随机，绑定流程发起时生成，永久不变
  uid          INTEGER NOT NULL,
  pikpak_sub   TEXT NOT NULL DEFAULT '',
  display_name TEXT NOT NULL DEFAULT '',
  avatar       TEXT NOT NULL DEFAULT '',
  vip_type     TEXT NOT NULL DEFAULT '',
  vip_expire   TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL DEFAULT 'active',   -- active | reauth_required | disabled
  bound_at     TEXT NOT NULL DEFAULT '',
  updated_at   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_pikpak_accounts_uid ON pikpak_accounts(uid, bound_at);

-- 凭据表按账号重建（PK 从 uid 换成 account_id，uid 保留用于归属校验）
CREATE TABLE credentials2 (
  account_id         TEXT PRIMARY KEY,
  uid                INTEGER NOT NULL,
  auth_type          TEXT NOT NULL DEFAULT 'oauth',
  client_id          TEXT NOT NULL,
  access_token_enc   BLOB NOT NULL,
  refresh_token_enc  BLOB NOT NULL,
  access_expires_at  TEXT NOT NULL,
  refresh_expires_at TEXT NOT NULL DEFAULT '',
  scopes             TEXT NOT NULL DEFAULT '',
  device_id_enc      BLOB NOT NULL,
  updated_at         TEXT NOT NULL
);

ALTER TABLE download_jobs ADD COLUMN account_id TEXT NOT NULL DEFAULT '';
ALTER TABLE upload_jobs   ADD COLUMN account_id TEXT NOT NULL DEFAULT '';
ALTER TABLE auth_flows    ADD COLUMN account_id TEXT NOT NULL DEFAULT '';
ALTER TABLE video_favorites ADD COLUMN restored_account_id TEXT NOT NULL DEFAULT '';
`,
}
