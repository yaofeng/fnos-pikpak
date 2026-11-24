package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// dataVersionCurrent guards the one-shot Go data migration. SQL migrations are
// idempotent by version number, but re-encrypting credentials and backfilling
// account ids must run exactly once; settings.data_version records that it
// has.
const dataVersionCurrent = "2"

// legacyAAD is the pre-multi-account AAD, bound to the fnOS uid. Only the data
// migration still reads this format: it decrypts rows written by older
// versions before re-encrypting them under acctAAD.
func legacyAAD(uid int, field string) []byte {
	return []byte(strconv.Itoa(uid) + ":" + field)
}

// legacyCredential is one row of the old single-account credentials table.
type legacyCredential struct {
	uid        int
	authType   string
	clientID   string
	accessEnc  []byte
	refreshEnc []byte
	accessExp  string
	refreshExp string
	scopes     string
	deviceEnc  []byte
	updated    string
}

// migrateData carries a pre-multi-account database into the account model:
// one pikpak_accounts row per old credential, tokens re-encrypted under the
// account-bound AAD, transfer jobs backfilled with the account id. It runs
// inside Open, before any HTTP traffic.
//
// A crash mid-way leaves data_version unset, so the next start redoes the
// migration; each uid whose credentials2 row already exists is skipped, which
// is what keeps a half-finished run from duplicating accounts or losing
// credentials. Every uid is carried in one transaction, so the account row and
// its credential appear together or not at all.
func (s *Store) migrateData() error {
	ctx := context.Background()
	if v, err := s.GetSetting(ctx, "data_version"); err == nil && v == dataVersionCurrent {
		return nil
	}

	// A database without a credentials table has nothing to carry over; it
	// only needs the version marker.
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'credentials'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return s.SetSetting(ctx, "data_version", dataVersionCurrent)
	}

	// Read every legacy row up front: the connection pool holds a single
	// connection, so the rows must be closed before the per-row transactions
	// below can run.
	rows, err := s.db.QueryContext(ctx, `
SELECT uid, auth_type, client_id, access_token_enc, refresh_token_enc,
       access_expires_at, refresh_expires_at, scopes, device_id_enc, updated_at
FROM credentials`)
	if err != nil {
		return err
	}
	var legacy []legacyCredential
	for rows.Next() {
		var c legacyCredential
		if err := rows.Scan(&c.uid, &c.authType, &c.clientID, &c.accessEnc, &c.refreshEnc,
			&c.accessExp, &c.refreshExp, &c.scopes, &c.deviceEnc, &c.updated); err != nil {
			rows.Close()
			return err
		}
		legacy = append(legacy, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, c := range legacy {
		if err := s.migrateLegacyCredential(ctx, c); err != nil {
			return fmt.Errorf("store: migrate credential for uid %d: %w", c.uid, err)
		}
	}

	// The old table is kept until every row has landed, so a failed run can
	// pick up where it left off.
	if _, err := s.db.ExecContext(ctx, `DROP TABLE credentials`); err != nil {
		return err
	}
	return s.SetSetting(ctx, "data_version", dataVersionCurrent)
}

// migrateLegacyCredential turns one old credentials row into a pikpak_accounts
// row plus a credentials2 row, atomically. A uid that already has a
// credentials2 row was carried by an earlier, interrupted run.
func (s *Store) migrateLegacyCredential(ctx context.Context, c legacyCredential) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op once committed

	var exists int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM credentials2 WHERE uid = ?`, c.uid).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}

	id, err := NewAccountID()
	if err != nil {
		return err
	}

	// The profile display data moves to the account row; a user that only had
	// a credential migrates with empty fields.
	var sub, displayName, avatar, vipType, vipExpire string
	err = tx.QueryRowContext(ctx, `
SELECT pikpak_sub, display_name, avatar, vip_type, vip_expire FROM users WHERE uid = ?`, c.uid).
		Scan(&sub, &displayName, &avatar, &vipType, &vipExpire)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `
INSERT INTO pikpak_accounts(id, uid, pikpak_sub, display_name, avatar, vip_type, vip_expire,
  status, bound_at, updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?)`,
		id, c.uid, sub, displayName, avatar, vipType, vipExpire,
		"active", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}

	// Tokens keep their plaintext (and therefore the device fingerprint) but
	// are re-sealed under the account-bound AAD.
	access, err := reseal(s.key, legacyAAD(c.uid, "access_token"), acctAAD(id, "access_token"), c.accessEnc)
	if err != nil {
		return err
	}
	refresh, err := reseal(s.key, legacyAAD(c.uid, "refresh_token"), acctAAD(id, "refresh_token"), c.refreshEnc)
	if err != nil {
		return err
	}
	device, err := reseal(s.key, legacyAAD(c.uid, "device_id"), acctAAD(id, "device_id"), c.deviceEnc)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO credentials2(account_id, uid, auth_type, client_id, access_token_enc, refresh_token_enc,
  access_expires_at, refresh_expires_at, scopes, device_id_enc, updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		id, c.uid, c.authType, c.clientID, access, refresh,
		c.accessExp, c.refreshExp, c.scopes, device, c.updated); err != nil {
		return err
	}

	// Jobs queued before the migration belong to this account.
	for _, table := range []string{"download_jobs", "upload_jobs"} {
		if _, err := tx.ExecContext(ctx,
			`UPDATE `+table+` SET account_id = ? WHERE uid = ? AND account_id = ''`,
			id, c.uid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// reseal re-encrypts a ciphertext from one AAD to another without ever
// exposing the plaintext beyond this call.
func reseal(key, oldAAD, newAAD, ciphertext []byte) ([]byte, error) {
	pt, err := Decrypt(key, oldAAD, ciphertext)
	if err != nil {
		return nil, err
	}
	return Encrypt(key, newAAD, pt)
}
