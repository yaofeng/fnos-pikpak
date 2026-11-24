package httpapi

import (
	"errors"
	"net/http"
	"testing"

	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/store"
)

func testAccount(id string) namedClient {
	return namedClient{Account: store.PikPakAccount{ID: id, DisplayName: "账号" + id}}
}

func TestPageTokenRoundTrip(t *testing.T) {
	in := map[string]string{"acct-a": "t1", "acct-b": "t2"}
	enc := encodePageToken(in)
	if enc == "" {
		t.Fatal("encode produced an empty token for a non-empty map")
	}
	out, err := decodePageToken(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 2 || out["acct-a"] != "t1" || out["acct-b"] != "t2" {
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
