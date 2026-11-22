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
}
