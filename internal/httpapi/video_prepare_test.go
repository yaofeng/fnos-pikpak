package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"fnos-pikpak/internal/playtoken"
	"fnos-pikpak/internal/store"
)

// The multi-account prepare tests run two bound accounts against one fake
// upstream that dispatches by the X-Device-Id each session pins onto its
// client: acct-a is bound first, so it is the default account the share
// browser and the prepare's source resolution act as.
const (
	acctA = "acct-a"
	acctB = "acct-b"
)

// testDeviceID mirrors the device id bindAccount gives an account, so the
// fake can tell the two drives apart.
func testDeviceID(accountID string) string {
	return "dev-" + accountID + "-0123456789abcdef"
}

// newMultiVideoEnv binds uid 1000 with two accounts whose drives are separate
// videoUpstream fakes behind one server. The share session is seeded under
// acct-a, the default account.
func newMultiVideoEnv(t *testing.T, tweakA, tweakB func(*videoUpstream)) (Deps, http.Handler, *videoUpstream, *videoUpstream) {
	t.Helper()
	upA, upB := newVideoUpstream(), newVideoUpstream()
	if tweakA != nil {
		tweakA(upA)
	}
	if tweakB != nil {
		tweakB(upB)
	}
	hA, hB := upA.handler(), upB.handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Device-Id") == testDeviceID(acctB) {
			hB.ServeHTTP(w, r)
			return
		}
		hA.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	deps, st := newAuthTestDeps(t, srv.URL)
	base := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	bindAccountAt(t, st, 1000, acctA, "sub-a", "active", base)
	bindAccountAt(t, st, 1000, acctB, "sub-b", "active", base.Add(time.Minute))
	deps.PlaySecret = []byte("test-play-secret-test-play-secret")
	deps.ShareSessions = newShareSessions()
	deps.Videos = newVideoJobs()
	deps.ShareSessions.put(1000, acctA, "VOabc123", "tok-1", "", time.Now())
	return deps, NewMux(deps), upA, upB
}

// tokenFromStreamURL pulls the signed token out of a result's stream_url.
func tokenFromStreamURL(t *testing.T, raw any) string {
	t.Helper()
	s, _ := raw.(string)
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("stream_url %q: %v", s, err)
	}
	return u.Query().Get("t")
}

// 完整播放 never restores a second copy: a match in ANY account's Pack From
// Shared plays from there under that account's identity, and the favourite
// remembers which account holds it.
func TestPrepareReusesAnyAccountCopy(t *testing.T) {
	deps, h, upA, upB := newMultiVideoEnv(t, nil, func(up *videoUpstream) {
		up.set("", folderEntry("pack-b", packFromSharedName))
		up.set("pack-b", fileEntry("df-b", "new.mp4", 50, "2026-11-20T10:00:00Z"))
	})
	fav, err := deps.Store.AddVideoFavorite(t.Context(), 1000, store.VideoFavorite{
		ShareID: "VOabc123", FileID: "sf2", Name: "new.mp4", Size: 50, PassCode: "7f3a",
	})
	if err != nil {
		t.Fatalf("favorite: %v", err)
	}

	job := readyVideoJob(t, h, `{"video_favorite_id":`+jsonInt(fav.ID)+`}`)
	if job["state"] != "ready" {
		t.Fatalf("job = %v", job)
	}
	if job["file_id"] != "df-b" {
		t.Fatalf("file_id = %v, want acct-b's existing copy", job["file_id"])
	}
	if job["account_id"] != acctB {
		t.Fatalf("account_id = %v, want %q", job["account_id"], acctB)
	}
	claims, err := playtoken.Verify(deps.PlaySecret, tokenFromStreamURL(t, job["stream_url"]), time.Now())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.AccountID != acctB {
		t.Fatalf("token account = %q, want %q", claims.AccountID, acctB)
	}
	if _, _, restoresA, _ := upA.snapshot(); restoresA != 0 {
		t.Fatalf("acct-a restored %d times", restoresA)
	}
	if _, _, restoresB, _ := upB.snapshot(); restoresB != 0 {
		t.Fatalf("acct-b restored %d times, want the existing copy reused", restoresB)
	}
	saved, err := deps.Store.GetVideoFavorite(t.Context(), 1000, fav.ID)
	if err != nil || saved.RestoredFileID != "df-b" || saved.RestoredAccountID != acctB {
		t.Fatalf("saved = %+v err=%v", saved, err)
	}
}

// 账号 a 满且清不动（配额固定，无可清理文件）时，完整播放换下一个账号：
// b 腾得出，副本就落在 b 上，令牌和结果都指向 b。
func TestPrepareRotatesOnInsufficientSpace(t *testing.T) {
	deps, h, upA, upB := newMultiVideoEnv(t, func(up *videoUpstream) {
		up.usage = 990 // 10 free of 1000: the 50-byte video never fits
	}, nil)

	job := readyVideoJob(t, h, `{"share_id":"VOabc123","file_id":"sf2"}`)
	if job["state"] != "ready" {
		t.Fatalf("job = %v", job)
	}
	if job["account_id"] != acctB {
		t.Fatalf("account_id = %v, want the account that had room", job["account_id"])
	}
	claims, err := playtoken.Verify(deps.PlaySecret, tokenFromStreamURL(t, job["stream_url"]), time.Now())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.AccountID != acctB {
		t.Fatalf("token account = %q, want %q", claims.AccountID, acctB)
	}
	if _, _, restoresA, _ := upA.snapshot(); restoresA != 0 {
		t.Fatalf("acct-a restored %d times, want the rotation to skip it", restoresA)
	}
	if _, _, restoresB, _ := upB.snapshot(); restoresB != 1 {
		t.Fatalf("acct-b restores = %d, want 1", restoresB)
	}
}

// 两个账号都腾不出空间时，耗尽是结构化的：errAllAccountsFailed 带每个账号
// 的真实原因，writeVideoError 把它映射为 429 all_accounts_failed。
func TestPrepareFailsWhenAllExhausted(t *testing.T) {
	deps, _, upA, upB := newMultiVideoEnv(t,
		func(up *videoUpstream) { up.usage = 990 },
		func(up *videoUpstream) { up.usage = 990 },
	)

	_, err := prepareVideo(context.Background(), deps, 1000, videoPrepareRequest{ShareID: "VOabc123", FileID: "sf2"})
	if !errors.Is(err, errAllAccountsFailed) {
		t.Fatalf("err = %v, want errAllAccountsFailed", err)
	}
	for _, label := range []string{"账号" + acctA, "账号" + acctB} {
		if !strings.Contains(err.Error(), label) {
			t.Fatalf("error %q does not name %s", err.Error(), label)
		}
	}
	rec := httptest.NewRecorder()
	writeVideoError(rec, err)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if errCode(body) != "all_accounts_failed" {
		t.Fatalf("error code = %q, want all_accounts_failed", errCode(body))
	}
	if _, _, restoresA, _ := upA.snapshot(); restoresA != 0 {
		t.Fatalf("acct-a restored %d times without space", restoresA)
	}
	if _, _, restoresB, _ := upB.snapshot(); restoresB != 0 {
		t.Fatalf("acct-b restored %d times without space", restoresB)
	}
}
