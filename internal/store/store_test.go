package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "pikpak.db")
	keyPath := filepath.Join(dir, "secret.key")
	s, err := Open(dbPath, keyPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dir
}

func sampleCredential(uid int, token string) Credential {
	return Credential{
		UID:              uid,
		AuthType:         "oauth",
		ClientID:         "client",
		AccessToken:      token,
		RefreshToken:     "refresh-" + token,
		DeviceID:         "device-abcdef0123456789abcdef0123456789",
		AccessExpiresAt:  time.Now().Add(time.Hour).Truncate(time.Second),
		RefreshExpiresAt: time.Now().Add(24 * time.Hour).Truncate(time.Second),
		Scopes:           "files:read",
	}
}

func TestCredentialRoundTrip(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if err := s.PutCredential(ctx, sampleCredential(1000, "first")); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := s.GetCredential(ctx, 1000)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.AccessToken != "first" || got.RefreshToken != "refresh-first" || got.Scopes != "files:read" {
		t.Fatalf("got %+v", got)
	}

	// UPSERT keeps one row per uid.
	if err := s.PutCredential(ctx, sampleCredential(1000, "second")); err != nil {
		t.Fatalf("put 2: %v", err)
	}
	got, _ = s.GetCredential(ctx, 1000)
	if got.AccessToken != "second" {
		t.Fatalf("token = %q", got.AccessToken)
	}
}

func TestCredentialIsolation(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	_ = s.PutCredential(ctx, sampleCredential(1000, "a"))
	_ = s.PutCredential(ctx, sampleCredential(1001, "b"))

	a, _ := s.GetCredential(ctx, 1000)
	b, _ := s.GetCredential(ctx, 1001)
	if a.AccessToken != "a" || b.AccessToken != "b" {
		t.Fatalf("a=%q b=%q", a.AccessToken, b.AccessToken)
	}

	if _, err := s.GetCredential(ctx, 2000); err == nil {
		t.Fatal("missing uid returned a credential")
	}
}

func TestWrongAADFailsToDecrypt(t *testing.T) {
	key := make([]byte, 32)
	ct, err := Encrypt(key, []byte("1000:access_token"), []byte("secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := Decrypt(key, []byte("1001:access_token"), ct); err == nil {
		t.Fatal("decrypted with the wrong AAD")
	}
	pt, err := Decrypt(key, []byte("1000:access_token"), ct)
	if err != nil || string(pt) != "secret" {
		t.Fatalf("decrypt: %q %v", pt, err)
	}
}

func TestFilePermissions(t *testing.T) {
	_, dir := newStore(t)

	dbInfo, err := os.Stat(filepath.Join(dir, "pikpak.db"))
	if err != nil {
		t.Fatal(err)
	}
	if dbInfo.Mode().Perm() != 0o600 {
		t.Fatalf("db mode = %o", dbInfo.Mode().Perm())
	}

	keyInfo, err := os.Stat(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	if keyInfo.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %o", keyInfo.Mode().Perm())
	}
	if keyInfo.Size() != 32 {
		t.Fatalf("key size = %d", keyInfo.Size())
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	_, dir := newStore(t)
	dbPath := filepath.Join(dir, "pikpak.db")
	keyPath := filepath.Join(dir, "secret.key")

	s2, err := Open(dbPath, keyPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	var count int
	if err := s2.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("migrations = %d, want 1", count)
	}
}

func TestDataSurvivesReopen(t *testing.T) {
	s, dir := newStore(t)
	ctx := context.Background()
	_ = s.PutCredential(ctx, sampleCredential(1000, "persisted"))
	_ = s.Close()

	s2, err := Open(filepath.Join(dir, "pikpak.db"), filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	got, err := s2.GetCredential(ctx, 1000)
	if err != nil || got.AccessToken != "persisted" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestAuthFlowRoundTrip(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	flow := Flow{
		ID:          "flow1",
		UID:         1000,
		DeviceCode:  "device-code",
		UserCode:    "ABCD",
		VerifyURL:   "https://mypikpak.com/device",
		IntervalSec: 2,
		ExpiresAt:   time.Now().Add(5 * time.Minute).Truncate(time.Second),
		Status:      "pending",
	}
	if err := s.PutAuthFlow(ctx, flow); err != nil {
		t.Fatalf("put flow: %v", err)
	}
	got, err := s.GetAuthFlow(ctx, "flow1")
	if err != nil || got.UserCode != "ABCD" || got.Status != "pending" {
		t.Fatalf("got %+v err %v", got, err)
	}

	flow.Status = "bound"
	if err := s.PutAuthFlow(ctx, flow); err != nil {
		t.Fatalf("update flow: %v", err)
	}
	got, _ = s.GetAuthFlow(ctx, "flow1")
	if got.Status != "bound" {
		t.Fatalf("status = %q", got.Status)
	}
}

func TestDeleteCredential(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if err := s.PutCredential(ctx, Credential{UID: 1, AuthType: "oauth", ClientID: "c", AccessToken: "a", RefreshToken: "r", DeviceID: "d", AccessExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := s.DeleteCredential(ctx, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetCredential(ctx, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteCredential(ctx, 1); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestDeriveDeviceID(t *testing.T) {
	s, _ := newStore(t)
	a := s.DeriveDeviceID(1000)
	if len(a) != 32 {
		t.Fatalf("device id %q has length %d, want 32", a, len(a))
	}
	if !isLowerHex(a) {
		t.Fatalf("device id %q is not lower-case hex", a)
	}
	if a != s.DeriveDeviceID(1000) {
		t.Fatal("device id is not stable for the same uid")
	}
	if a == s.DeriveDeviceID(1001) {
		t.Fatal("device id is shared across uids")
	}
	other, _ := newStore(t)
	if a == other.DeriveDeviceID(1000) {
		t.Fatal("device id must depend on the store key")
	}
}

func isLowerHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func TestCancelPendingFlows(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	mk := func(id string, uid int, status string) {
		t.Helper()
		if err := s.PutAuthFlow(ctx, Flow{
			ID: id, UID: uid, DeviceCode: "d", UserCode: "U",
			VerifyURL: "https://mypikpak.com/x", IntervalSec: 1,
			ExpiresAt: time.Now().Add(time.Minute), Status: status,
		}); err != nil {
			t.Fatalf("put flow %s: %v", id, err)
		}
	}
	mk("pending-1", 10, "pending")
	mk("bound-1", 10, "bound")
	mk("pending-2", 11, "pending")

	if err := s.CancelPendingFlows(ctx, 10); err != nil {
		t.Fatalf("CancelPendingFlows: %v", err)
	}

	if f, _ := s.GetAuthFlow(ctx, "pending-1"); f.Status != "cancelled" {
		t.Fatalf("pending-1 status = %q", f.Status)
	}
	if f, _ := s.GetAuthFlow(ctx, "bound-1"); f.Status != "bound" {
		t.Fatalf("bound-1 status = %q", f.Status)
	}
	if f, _ := s.GetAuthFlow(ctx, "pending-2"); f.Status != "pending" {
		t.Fatalf("pending-2 status = %q", f.Status)
	}
}
