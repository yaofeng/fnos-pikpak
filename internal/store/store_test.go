package store

import (
	"bytes"
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

func TestEffectiveSettingPrefersTheUserRow(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if got := s.EffectiveSetting(ctx, 1000, "conflict"); got != "" {
		t.Fatalf("unset key = %q", got)
	}
	if err := s.SetSetting(ctx, "conflict", "skip"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if got := s.EffectiveSetting(ctx, 1000, "conflict"); got != "skip" {
		t.Fatalf("global value = %q", got)
	}
	if err := s.SetUserSetting(ctx, 1000, "conflict", "overwrite"); err != nil {
		t.Fatalf("SetUserSetting: %v", err)
	}
	if got := s.EffectiveSetting(ctx, 1000, "conflict"); got != "overwrite" {
		t.Fatalf("user value = %q", got)
	}
	// A user row that stores nothing has chosen nothing: the global value is
	// still the answer.
	if err := s.SetUserSetting(ctx, 1000, "conflict", ""); err != nil {
		t.Fatalf("SetUserSetting: %v", err)
	}
	if got := s.EffectiveSetting(ctx, 1000, "conflict"); got != "skip" {
		t.Fatalf("empty user value = %q", got)
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
	if count != len(migrations) {
		t.Fatalf("migrations = %d, want %d", count, len(migrations))
	}
}

func TestUserRoundTrip(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if err := s.PutUser(ctx, User{UID: 1000, Username: "alice", PikPakSub: "sub-1", DisplayName: "Alice"}); err != nil {
		t.Fatalf("put user: %v", err)
	}
	got, err := s.GetUser(ctx, 1000)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if got.Username != "alice" || got.PikPakSub != "sub-1" || got.Status != "active" {
		t.Fatalf("got %+v", got)
	}
	if got.BoundAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("timestamps were not filled in: %+v", got)
	}

	// Upsert keeps one row per uid and preserves bound_at.
	firstBound := got.BoundAt
	if err := s.PutUser(ctx, User{UID: 1000, Username: "alice2", PikPakSub: "sub-1", DisplayName: "Alice 2"}); err != nil {
		t.Fatalf("put user 2: %v", err)
	}
	got, _ = s.GetUser(ctx, 1000)
	if got.DisplayName != "Alice 2" || !got.BoundAt.Equal(firstBound) {
		t.Fatalf("after upsert: %+v (bound_at %v, was %v)", got, got.BoundAt, firstBound)
	}

	if _, err := s.GetUser(ctx, 2000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing user err = %v, want ErrNotFound", err)
	}
}

func TestGrantedFoldersArePerUser(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if err := s.GrantFolder(ctx, 1000, "/vol1/1001/private/pikpak", "存储空间 1/alice/private/pikpak"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if err := s.GrantFolder(ctx, 1000, "/vol1/1001/private/pikpak", "更新后的名字"); err != nil {
		t.Fatalf("re-grant: %v", err)
	}
	if err := s.GrantFolder(ctx, 1001, "/vol1/1002/other", ""); err != nil {
		t.Fatalf("grant other: %v", err)
	}

	folders, err := s.ListGrantedFolders(ctx, 1000)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(folders) != 1 || folders[0].Label != "更新后的名字" {
		t.Fatalf("folders = %+v", folders)
	}
	other, _ := s.ListGrantedFolders(ctx, 1001)
	if len(other) != 1 || other[0].Path != "/vol1/1002/other" {
		t.Fatalf("other = %+v", other)
	}

	if err := s.RevokeFolder(ctx, 1000, "/vol1/1001/private/pikpak"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if folders, _ := s.ListGrantedFolders(ctx, 1000); len(folders) != 0 {
		t.Fatalf("folders after revoke = %+v", folders)
	}
	if other, _ := s.ListGrantedFolders(ctx, 1001); len(other) != 1 {
		t.Fatalf("revoke leaked across users: %+v", other)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if _, err := s.GetSetting(ctx, "download_concurrency"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing setting err = %v, want ErrNotFound", err)
	}
	if err := s.SetSetting(ctx, "download_concurrency", "3"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if v, err := s.GetSetting(ctx, "download_concurrency"); err != nil || v != "3" {
		t.Fatalf("get = %q, %v", v, err)
	}
	if err := s.SetSetting(ctx, "download_concurrency", "2"); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if v, _ := s.GetSetting(ctx, "download_concurrency"); v != "2" {
		t.Fatalf("get after overwrite = %q", v)
	}
	if err := s.SetSetting(ctx, "too_long", string(make([]byte, 1025))); err == nil {
		t.Fatal("a setting over 1 KiB was accepted")
	}
}

func TestUserSettingsArePerUser(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if err := s.SetUserSetting(ctx, 1000, "language", "zh-CN"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.SetUserSetting(ctx, 1001, "language", "en"); err != nil {
		t.Fatalf("set other: %v", err)
	}
	if v, _ := s.GetUserSetting(ctx, 1000, "language"); v != "zh-CN" {
		t.Fatalf("uid 1000 = %q", v)
	}
	if v, _ := s.GetUserSetting(ctx, 1001, "language"); v != "en" {
		t.Fatalf("uid 1001 = %q", v)
	}
	if _, err := s.GetUserSetting(ctx, 1000, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err = %v, want ErrNotFound", err)
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
		AccountID:   "acct-1",
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
	if got.AccountID != "acct-1" {
		t.Fatalf("account id = %q, want acct-1", got.AccountID)
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
	a := s.DeriveDeviceID("acct-1000")
	if len(a) != 32 {
		t.Fatalf("device id %q has length %d, want 32", a, len(a))
	}
	if !isLowerHex(a) {
		t.Fatalf("device id %q is not lower-case hex", a)
	}
	if a != s.DeriveDeviceID("acct-1000") {
		t.Fatal("device id is not stable for the same account")
	}
	if a == s.DeriveDeviceID("acct-1001") {
		t.Fatal("device id is shared across accounts")
	}
	other, _ := newStore(t)
	if a == other.DeriveDeviceID("acct-1000") {
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

func TestSecretKeyReturnsCopy(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "secret.key")
	st, err := Open(filepath.Join(dir, "pikpak.db"), keyPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	onDisk, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	got := st.SecretKey()
	if !bytes.Equal(got, onDisk) {
		t.Fatalf("SecretKey = %x, want %x", got, onDisk)
	}
	got[0] ^= 0xff
	if bytes.Equal(st.SecretKey(), got) {
		t.Fatal("SecretKey must hand out a copy, not the live key")
	}
}

func TestJobCountsAndRecentFailures(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()

	for _, j := range []DownloadJob{
		{ID: "d1", UID: 1000, FileID: "f1", FileName: "a", DestDir: "/tmp", DestPath: "/tmp/a", Status: "queued"},
		{ID: "d2", UID: 1000, FileID: "f2", FileName: "b", DestDir: "/tmp", DestPath: "/tmp/b", Status: "done"},
		{ID: "d3", UID: 1000, FileID: "f3", FileName: "c", DestDir: "/tmp", DestPath: "/tmp/c", Status: "failed",
			ErrorCode: "download_failed", ErrorMessage: "boom"},
		{ID: "d4", UID: 2000, FileID: "f4", FileName: "d", DestDir: "/tmp", DestPath: "/tmp/d", Status: "failed",
			ErrorCode: "download_failed", ErrorMessage: "other user"},
	} {
		if err := st.PutDownloadJob(ctx, j); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	if err := st.PutUploadJob(ctx, UploadJob{
		ID: "u1", UID: 1000, LocalPath: "/tmp/a", RemoteName: "a", Status: "skipped",
		ErrorCode: "conflict", ErrorMessage: "exists",
	}); err != nil {
		t.Fatalf("put upload: %v", err)
	}

	counts, err := st.JobCounts(ctx, 1000)
	if err != nil {
		t.Fatalf("JobCounts: %v", err)
	}
	if counts.Downloads["queued"] != 1 || counts.Downloads["done"] != 1 || counts.Downloads["failed"] != 1 {
		t.Fatalf("downloads = %v", counts.Downloads)
	}
	if counts.Uploads["skipped"] != 1 {
		t.Fatalf("uploads = %v", counts.Uploads)
	}

	failures, err := st.RecentJobFailures(ctx, 1000, 10)
	if err != nil {
		t.Fatalf("RecentJobFailures: %v", err)
	}
	if len(failures) != 2 {
		t.Fatalf("failures = %+v", failures)
	}
	// Scoped to the caller: the other user's failure must not appear.
	for _, f := range failures {
		if f.ID == "d4" {
			t.Fatalf("leaked another user's job: %+v", failures)
		}
	}
	if failures[0].Kind == failures[1].Kind {
		t.Fatalf("expected one download and one upload failure: %+v", failures)
	}
}

func TestDBSize(t *testing.T) {
	st, dbPath := newStore(t)
	size, err := st.DBSize()
	if err != nil {
		t.Fatalf("DBSize: %v", err)
	}
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if size <= 0 || size > info.Size()+1<<20 {
		t.Fatalf("size = %d, file = %d", size, info.Size())
	}
}

func TestDeleteUserJobs(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()

	if err := st.PutDownloadJob(ctx, DownloadJob{
		ID: "d1", UID: 1000, FileID: "f1", FileName: "a", DestDir: "/tmp", DestPath: "/tmp/a", Status: "queued",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutUploadJob(ctx, UploadJob{
		ID: "u1", UID: 1000, LocalPath: "/tmp/a", RemoteName: "a", Status: "queued",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutDownloadJob(ctx, DownloadJob{
		ID: "d2", UID: 2000, FileID: "f2", FileName: "b", DestDir: "/tmp", DestPath: "/tmp/b", Status: "queued",
	}); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteUserJobs(ctx, 1000); err != nil {
		t.Fatalf("DeleteUserJobs: %v", err)
	}
	if jobs, _ := st.ListDownloadJobs(ctx, 1000); len(jobs) != 0 {
		t.Fatalf("downloads left: %+v", jobs)
	}
	if jobs, _ := st.ListUploadJobs(ctx, 1000); len(jobs) != 0 {
		t.Fatalf("uploads left: %+v", jobs)
	}
	if jobs, _ := st.ListDownloadJobs(ctx, 2000); len(jobs) != 1 {
		t.Fatalf("another user's job was deleted: %+v", jobs)
	}
}

func TestDeleteAccountJobs(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()

	for _, j := range []DownloadJob{
		{ID: "d-a", UID: 1000, AccountID: "acct-a", FileID: "f1", FileName: "a", DestDir: "/tmp", DestPath: "/tmp/a", Status: "queued"},
		{ID: "d-b", UID: 1000, AccountID: "acct-b", FileID: "f2", FileName: "b", DestDir: "/tmp", DestPath: "/tmp/b", Status: "queued"},
		{ID: "d-other", UID: 2000, AccountID: "acct-a", FileID: "f3", FileName: "c", DestDir: "/tmp", DestPath: "/tmp/c", Status: "queued"},
	} {
		if err := st.PutDownloadJob(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.PutUploadJob(ctx, UploadJob{
		ID: "u-a", UID: 1000, AccountID: "acct-a", LocalPath: "/tmp/a", RemoteName: "a", Status: "queued",
	}); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteAccountJobs(ctx, 1000, "acct-a"); err != nil {
		t.Fatalf("DeleteAccountJobs: %v", err)
	}
	if jobs, _ := st.ListDownloadJobs(ctx, 1000); len(jobs) != 1 || jobs[0].ID != "d-b" {
		t.Fatalf("downloads after delete = %+v", jobs)
	}
	if ups, _ := st.ListUploadJobs(ctx, 1000); len(ups) != 0 {
		t.Fatalf("uploads after delete = %+v", ups)
	}
	if jobs, _ := st.ListDownloadJobs(ctx, 2000); len(jobs) != 1 {
		t.Fatalf("another user's job was deleted: %+v", jobs)
	}
}

func TestSetUserStatusCreatesRowForCredentialOnlyUser(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	// A session can exist from a credential alone (an older database, or a
	// bind that crash-landed between the two writes). Marking such a user
	// for reauthorization must still be recorded, or the shell never learns.
	if err := st.SetUserStatus(ctx, 1000, "reauth_required"); err != nil {
		t.Fatalf("SetUserStatus: %v", err)
	}
	u, err := st.GetUser(ctx, 1000)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.Status != "reauth_required" {
		t.Fatalf("status = %q", u.Status)
	}

	// An existing profile keeps its fields; only the status changes.
	if err := st.PutUser(ctx, User{UID: 1000, DisplayName: "小明", Status: "active"}); err != nil {
		t.Fatalf("PutUser: %v", err)
	}
	if err := st.SetUserStatus(ctx, 1000, "reauth_required"); err != nil {
		t.Fatalf("SetUserStatus again: %v", err)
	}
	u, err = st.GetUser(ctx, 1000)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.Status != "reauth_required" || u.DisplayName != "小明" {
		t.Fatalf("user = %+v", u)
	}
}
