package httpapi

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/store"
)

func testAccount(id string) namedClient {
	return namedClient{Account: store.PikPakAccount{ID: id, DisplayName: "账号" + id}}
}

func TestPageTokenRoundTrip(t *testing.T) {
	in := map[string]string{"acct-a": "t1", "acct-b": "t2", "acct-c": pageDone}
	enc := encodePageToken(in)
	if enc == "" {
		t.Fatal("encode produced an empty token for a non-empty map")
	}
	out, err := decodePageToken(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 3 || out["acct-a"] != "t1" || out["acct-b"] != "t2" || out["acct-c"] != pageDone {
		t.Fatalf("round trip = %v, want %v", out, in)
	}
	if _, err := decodePageToken("!!!!"); err == nil {
		t.Fatal("decode accepted an invalid token")
	}
	empty, err := decodePageToken("")
	if err != nil || len(empty) != 0 {
		t.Fatalf("decode(\"\") = %v, %v; want an empty map without error", empty, err)
	}
}

// TestRecentPaginationDoesNotRepeatExhaustedAccounts pins the merged
// pagination protocol: an account whose listing ended (or failed) is recorded
// in the page token and never asked again, so its rows cannot re-appear on
// later pages while a longer account is still paging.
func TestRecentPaginationDoesNotRepeatExhaustedAccounts(t *testing.T) {
	var mu sync.Mutex
	// Per upstream identity: the page tokens each one was asked with.
	asked := map[string][]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/events" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		page := r.URL.Query().Get("next_page_token")
		mu.Lock()
		asked[token] = append(asked[token], page)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch token {
		case "at-acct-a":
			// The long account: three pages, one row each.
			switch page {
			case "":
				_, _ = io.WriteString(w, `{"events":[{"file":{"id":"a1","name":"a1","modified_time":"2026-01-01T00:00:00Z"}}],"next_page_token":"n1"}`)
			case "n1":
				_, _ = io.WriteString(w, `{"events":[{"file":{"id":"a2","name":"a2","modified_time":"2026-01-02T00:00:00Z"}}],"next_page_token":"n2"}`)
			default:
				_, _ = io.WriteString(w, `{"events":[{"file":{"id":"a3","name":"a3","modified_time":"2026-01-03T00:00:00Z"}}]}`)
			}
		default:
			// The short account: one page, then exhausted.
			_, _ = io.WriteString(w, `{"events":[{"file":{"id":"b1","name":"b1","modified_time":"2026-02-01T00:00:00Z"}}]}`)
		}
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	bindAccount(t, st, 1000, "acct-b", "sub-b", "active")
	h := NewMux(deps)

	pageIDs := func(body map[string]any) []string {
		t.Helper()
		items, _ := body["items"].([]any)
		var ids []string
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			ids = append(ids, item["id"].(string))
		}
		return ids
	}
	askedByToken := func(token string) []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), asked[token]...)
	}

	// Page 1 serves both accounts.
	rec, body := getJSON(t, h, "/api/recent", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if ids := pageIDs(body); len(ids) != 2 {
		t.Fatalf("page 1 = %v, want a1 and b1", ids)
	}
	if got := askedByToken("at-acct-b"); len(got) != 1 || got[0] != "" {
		t.Fatalf("short account asked %v on page 1, want one first-page call", got)
	}
	token, _ := body["next_page_token"].(string)
	if token == "" {
		t.Fatal("page 1 omitted next_page_token while acct-a still has pages")
	}

	// Page 2 advances acct-a only; acct-b must not be asked again.
	rec, body = getJSON(t, h, "/api/recent?page_token="+token, "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("page 2 status = %d body=%v", rec.Code, body)
	}
	if ids := pageIDs(body); len(ids) != 1 || ids[0] != "a2" {
		t.Fatalf("page 2 = %v, want only a2 (b1 must not repeat)", ids)
	}
	if got := askedByToken("at-acct-b"); len(got) != 1 {
		t.Fatalf("exhausted account was asked again: %v", got)
	}
	token, _ = body["next_page_token"].(string)
	if token == "" {
		t.Fatal("page 2 omitted next_page_token while acct-a still has pages")
	}

	// Page 3 is acct-a's last: rows served once, and the envelope is omitted
	// because no account has pages left.
	rec, body = getJSON(t, h, "/api/recent?page_token="+token, "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("page 3 status = %d body=%v", rec.Code, body)
	}
	if ids := pageIDs(body); len(ids) != 1 || ids[0] != "a3" {
		t.Fatalf("page 3 = %v, want only a3", ids)
	}
	if _, ok := body["next_page_token"]; ok {
		t.Fatalf("final page carried next_page_token = %v, want it omitted", body["next_page_token"])
	}
	if errs, ok := body["account_errors"]; ok {
		t.Fatalf("final page reported account_errors = %v", errs)
	}
	if got := askedByToken("at-acct-a"); len(got) != 3 {
		t.Fatalf("long account asked %v, want three pages", got)
	}
	if got := askedByToken("at-acct-b"); len(got) != 1 {
		t.Fatalf("short account asked %v in total, want exactly one call", got)
	}
}

func TestMergeFilePagesSortsModifiedDesc(t *testing.T) {
	pages := [][]pikpak.File{
		{
			{ID: "a-old", Name: "a-old", ModifiedTime: "2026-01-01T00:00:00Z"},
			{ID: "a-new", Name: "a-new", ModifiedTime: "2026-03-01T00:00:00Z"},
		},
		{
			{ID: "b-mid", Name: "b-mid", ModifiedTime: "2026-02-01T00:00:00Z"},
			{ID: "b-bad", Name: "b-bad", ModifiedTime: "not-a-time"},
		},
	}
	got := mergeFilePages(pages, []namedClient{testAccount("acct-a"), testAccount("acct-b")})

	var ids []string
	for _, f := range got {
		ids = append(ids, f.ID)
	}
	// Newest first; the unparsable timestamp leads, where modifiedSortKey
	// keeps it in the oldest-first walks too.
	want := []string{"b-bad", "a-new", "b-mid", "a-old"}
	if len(ids) != len(want) {
		t.Fatalf("merged = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("merged = %v, want %v", ids, want)
		}
	}
	for _, f := range got {
		wantAcct := "acct-a"
		if f.ID[0] == 'b' {
			wantAcct = "acct-b"
		}
		if f.AccountID != wantAcct {
			t.Fatalf("%s stamped %q, want %q", f.ID, f.AccountID, wantAcct)
		}
	}
}

func TestIsAccountLevelErr(t *testing.T) {
	for _, err := range []error{
		&pikpak.APIError{Code: 11},
		&pikpak.APIError{Status: http.StatusTooManyRequests},
		&pikpak.APIError{Status: http.StatusInternalServerError},
		errSpaceInsufficient,
	} {
		if !isAccountLevelErr(err) {
			t.Fatalf("%v should be account-level", err)
		}
	}
	for _, err := range []error{
		pikpak.ErrPassCodeRequired,
		pikpak.ErrShareUnavailable,
		errors.New("x"),
	} {
		if isAccountLevelErr(err) {
			t.Fatalf("%v should not be account-level", err)
		}
	}
}
