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
}
