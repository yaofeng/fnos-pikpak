package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func mustNewAccountID(t *testing.T) string {
	t.Helper()
	id, err := NewAccountID()
	if err != nil {
		t.Fatalf("NewAccountID: %v", err)
	}
	return id
}

func TestAccountCRUDAndIsolation(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := PikPakAccount{ID: mustNewAccountID(t), UID: 1001, PikPakSub: "sub-a", DisplayName: "账号A", BoundAt: base}
	b := PikPakAccount{ID: mustNewAccountID(t), UID: 1001, PikPakSub: "sub-b", DisplayName: "账号B", BoundAt: base.Add(time.Hour)}
	c := PikPakAccount{ID: mustNewAccountID(t), UID: 1002, PikPakSub: "sub-c", DisplayName: "账号C", BoundAt: base}
	for _, acct := range []PikPakAccount{a, b, c} {
		if err := s.PutAccount(ctx, acct); err != nil {
			t.Fatalf("PutAccount %s: %v", acct.ID, err)
		}
	}

	// Oldest bound first, and only the caller's own accounts.
	accounts, err := s.ListAccounts(ctx, 1001)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accounts) != 2 || accounts[0].ID != a.ID || accounts[1].ID != b.ID {
		t.Fatalf("ListAccounts = %+v, want [%s %s]", accounts, a.ID, b.ID)
	}

	got, err := s.GetAccount(ctx, 1001, a.ID)
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if got.DisplayName != a.DisplayName || got.PikPakSub != a.PikPakSub || !got.BoundAt.Equal(base) {
		t.Fatalf("GetAccount = %+v", got)
	}
	if _, err := s.GetAccount(ctx, 1001, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign account err = %v, want ErrNotFound", err)
	}
	if _, err := s.GetAccount(ctx, 1001, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing account err = %v, want ErrNotFound", err)
	}

	// Re-put keeps the original bound_at.
	if err := s.PutAccount(ctx, PikPakAccount{ID: a.ID, UID: 1001, PikPakSub: "sub-a2", DisplayName: "账号A2", BoundAt: base.Add(24 * time.Hour)}); err != nil {
		t.Fatalf("PutAccount again: %v", err)
	}
	got, _ = s.GetAccount(ctx, 1001, a.ID)
	if got.DisplayName != "账号A2" || got.PikPakSub != "sub-a2" || !got.BoundAt.Equal(base) {
		t.Fatalf("after re-put = %+v (bound_at %v, was %v)", got, got.BoundAt, base)
	}

	if err := s.SetAccountStatus(ctx, 1001, a.ID, "reauth_required"); err != nil {
		t.Fatalf("SetAccountStatus: %v", err)
	}
	if got, _ = s.GetAccount(ctx, 1001, a.ID); got.Status != "reauth_required" {
		t.Fatalf("status = %q, want reauth_required", got.Status)
	}

	if err := s.DeleteAccount(ctx, 1001, a.ID); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if accounts, _ = s.ListAccounts(ctx, 1001); len(accounts) != 1 || accounts[0].ID != b.ID {
		t.Fatalf("after delete = %+v, want [%s]", accounts, b.ID)
	}

	id, err := s.AccountIDBySub(ctx, 1001, "sub-b")
	if err != nil || id != b.ID {
		t.Fatalf("AccountIDBySub = %q, %v, want %q", id, err, b.ID)
	}
	if _, err := s.AccountIDBySub(ctx, 1001, "sub-c"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign sub err = %v, want ErrNotFound", err)
	}
	if _, err := s.AccountIDBySub(ctx, 1001, "sub-unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown sub err = %v, want ErrNotFound", err)
	}

	// Pending flows: one carries an account id, one predates accounts, one is
	// already bound, one belongs to another user.
	putFlow := func(id string, uid int, status string) {
		t.Helper()
		if err := s.PutAuthFlow(ctx, Flow{
			ID: id, UID: uid, DeviceCode: "d", UserCode: "U",
			VerifyURL: "https://mypikpak.com/x", IntervalSec: 1,
			ExpiresAt: time.Now().Add(time.Minute), Status: status,
		}); err != nil {
			t.Fatalf("put flow %s: %v", id, err)
		}
	}
	putFlow("flow-a", 1001, "pending")
	putFlow("flow-b", 1001, "pending")
	putFlow("flow-bound", 1001, "bound")
	putFlow("flow-other", 1002, "pending")
	if _, err := s.db.Exec(`UPDATE auth_flows SET account_id = ? WHERE id = 'flow-a'`, b.ID); err != nil {
		t.Fatalf("tag flow: %v", err)
	}

	if err := s.CancelAccountFlows(ctx, 1001, b.ID); err != nil {
		t.Fatalf("CancelAccountFlows: %v", err)
	}
	if f, _ := s.GetAuthFlow(ctx, "flow-a"); f.Status != "cancelled" {
		t.Fatalf("flow-a status = %q, want cancelled", f.Status)
	}
	if f, _ := s.GetAuthFlow(ctx, "flow-b"); f.Status != "pending" {
		t.Fatalf("flow-b status = %q, want pending", f.Status)
	}

	// An empty account id means "every pending flow of the uid".
	if err := s.CancelAccountFlows(ctx, 1001, ""); err != nil {
		t.Fatalf("CancelAccountFlows all: %v", err)
	}
	if f, _ := s.GetAuthFlow(ctx, "flow-b"); f.Status != "cancelled" {
		t.Fatalf("flow-b status = %q, want cancelled", f.Status)
	}
	if f, _ := s.GetAuthFlow(ctx, "flow-bound"); f.Status != "bound" {
		t.Fatalf("flow-bound status = %q, want bound", f.Status)
	}
	if f, _ := s.GetAuthFlow(ctx, "flow-other"); f.Status != "pending" {
		t.Fatalf("flow-other leaked across users: %q", f.Status)
	}

	first := mustNewAccountID(t)
	second := mustNewAccountID(t)
	if first == second {
		t.Fatal("NewAccountID returned the same id twice")
	}
	if len(first) != 16 || !isLowerHex(first) {
		t.Fatalf("account id %q is not 16 lower-case hex chars", first)
	}
}

func TestCredentialPerAccount(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	aID, bID := mustNewAccountID(t), mustNewAccountID(t)
	ca := sampleCredential(1001, "tok-a")
	ca.AccountID = aID
	cb := sampleCredential(1001, "tok-b")
	cb.AccountID = bID
	if err := s.PutCredential(ctx, ca); err != nil {
		t.Fatalf("put a: %v", err)
	}
	if err := s.PutCredential(ctx, cb); err != nil {
		t.Fatalf("put b: %v", err)
	}

	// Each row must carry its own ciphertext under the account-bound AAD.
	for _, tc := range []struct{ id, want string }{{aID, "tok-a"}, {bID, "tok-b"}} {
		var enc []byte
		if err := s.db.QueryRow(`SELECT access_token_enc FROM credentials2 WHERE account_id = ?`, tc.id).Scan(&enc); err != nil {
			t.Fatalf("load credential %s: %v", tc.id, err)
		}
		pt, err := Decrypt(s.key, acctAAD(tc.id, "access_token"), enc)
		if err != nil {
			t.Fatalf("decrypt %s: %v", tc.id, err)
		}
		if string(pt) != tc.want {
			t.Fatalf("credential %s token = %q, want %q", tc.id, pt, tc.want)
		}
	}

	// The per-account accessors resolve exactly their own row, even with two
	// credentials per uid — the legacy accessor above is the ambiguous one.
	gotA, err := s.GetAccountCredential(ctx, 1001, aID)
	if err != nil || gotA.AccessToken != "tok-a" || gotA.AccountID != aID {
		t.Fatalf("GetAccountCredential a = %+v, %v", gotA, err)
	}
	gotB, err := s.GetAccountCredential(ctx, 1001, bID)
	if err != nil || gotB.AccessToken != "tok-b" || gotB.DeviceID != cb.DeviceID {
		t.Fatalf("GetAccountCredential b = %+v, %v", gotB, err)
	}
	if _, err := s.GetAccountCredential(ctx, 1001, mustNewAccountID(t)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown account err = %v, want ErrNotFound", err)
	}
	if _, err := s.GetAccountCredential(ctx, 1002, aID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign uid err = %v, want ErrNotFound", err)
	}

	// The legacy single-credential accessor still resolves the uid's row.
	cred, err := s.GetCredential(ctx, 1001)
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if cred.AccessToken != "tok-a" && cred.AccessToken != "tok-b" {
		t.Fatalf("GetCredential token = %q, want one of the two accounts", cred.AccessToken)
	}

	// The per-account delete removes only its own row.
	if err := s.DeleteAccountCredential(ctx, 1001, aID); err != nil {
		t.Fatalf("DeleteAccountCredential: %v", err)
	}
	if _, err := s.GetAccountCredential(ctx, 1001, aID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a after delete err = %v, want ErrNotFound", err)
	}
	if _, err := s.GetAccountCredential(ctx, 1001, bID); err != nil {
		t.Fatalf("b after a's delete: %v", err)
	}
	// Deleting a missing row is not an error.
	if err := s.DeleteAccountCredential(ctx, 1001, aID); err != nil {
		t.Fatalf("delete again: %v", err)
	}

	// The empty account id selects the pre-account placeholder row.
	legacy := sampleCredential(1003, "tok-old")
	legacy.AccountID = ""
	if err := s.PutCredential(ctx, legacy); err != nil {
		t.Fatalf("put legacy: %v", err)
	}
	got, err := s.GetAccountCredential(ctx, 1003, "")
	if err != nil || got.AccessToken != "tok-old" {
		t.Fatalf("placeholder read = %+v, %v", got, err)
	}
	if err := s.DeleteAccountCredential(ctx, 1003, ""); err != nil {
		t.Fatalf("placeholder delete: %v", err)
	}
	if _, err := s.GetAccountCredential(ctx, 1003, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("placeholder after delete err = %v, want ErrNotFound", err)
	}

	if err := s.DeleteCredential(ctx, 1001); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM credentials2 WHERE uid = 1001`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("DeleteCredential left %d rows", n)
	}
	if _, err := s.GetCredential(ctx, 1001); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete err = %v, want ErrNotFound", err)
	}
}

func TestJobsCarryAccount(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if err := s.PutDownloadJob(ctx, DownloadJob{
		ID: "d1", UID: 1001, FileID: "f1", FileName: "movie.mkv",
		DestDir: "/vol1/downloads", DestPath: "/vol1/downloads/movie.mkv",
		Status: "queued", AccountID: "acct-d",
	}); err != nil {
		t.Fatalf("PutDownloadJob: %v", err)
	}
	if err := s.PutUploadJob(ctx, UploadJob{
		ID: "u1", UID: 1001, LocalPath: "/vol1/movie.mkv", RemoteName: "movie.mkv",
		Status: "queued", AccountID: "acct-u",
	}); err != nil {
		t.Fatalf("PutUploadJob: %v", err)
	}

	if j, err := s.GetDownloadJob(ctx, 1001, "d1"); err != nil || j.AccountID != "acct-d" {
		t.Fatalf("GetDownloadJob account = %q, %v", j.AccountID, err)
	}
	if jobs, err := s.ListDownloadJobs(ctx, 1001); err != nil || len(jobs) != 1 || jobs[0].AccountID != "acct-d" {
		t.Fatalf("ListDownloadJobs = %+v, %v", jobs, err)
	}
	if jobs, err := s.QueuedDownloadJobs(ctx, 10); err != nil || len(jobs) != 1 || jobs[0].AccountID != "acct-d" {
		t.Fatalf("QueuedDownloadJobs = %+v, %v", jobs, err)
	}

	if j, err := s.GetUploadJob(ctx, 1001, "u1"); err != nil || j.AccountID != "acct-u" {
		t.Fatalf("GetUploadJob account = %q, %v", j.AccountID, err)
	}
	if jobs, err := s.ListUploadJobs(ctx, 1001); err != nil || len(jobs) != 1 || jobs[0].AccountID != "acct-u" {
		t.Fatalf("ListUploadJobs = %+v, %v", jobs, err)
	}
	if jobs, err := s.QueuedUploadJobs(ctx, 10); err != nil || len(jobs) != 1 || jobs[0].AccountID != "acct-u" {
		t.Fatalf("QueuedUploadJobs = %+v, %v", jobs, err)
	}
}
