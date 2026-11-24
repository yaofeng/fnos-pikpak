package playtoken

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

var secret = []byte("0123456789abcdef0123456789abcdef")

// signedToken 手拼一个 token：与 Sign 相同的编码方式，便于构造旧格式或
// 换了 secret 的载荷。
func signedToken(secret []byte, payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sign(secret, payload)
}

func TestSignVerifyRoundTrip(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	token := Sign(secret, 1000, "acct-1", "file-abc", 6*time.Hour, now)

	claims, err := Verify(secret, token, now)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.UID != 1000 || claims.AccountID != "acct-1" || claims.FileID != "file-abc" {
		t.Fatalf("claims = %+v", claims)
	}
	if got, want := claims.ExpiresAt, now.Add(6*time.Hour); !got.Equal(want) {
		t.Fatalf("expires = %v, want %v", got, want)
	}
}

func TestVerifyRejectsWrongSignature(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	token := signedToken([]byte("another-secret-another-secret-32"), "1000|acct-1|f1|9999999999")
	if _, err := Verify(secret, token, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	token := Sign(secret, 1000, "acct-1", "f1", time.Hour, now)
	if _, err := Verify([]byte("another-secret-another-secret-32"), token, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	token := Sign(secret, 1000, "acct-1", "f1", time.Hour, now)
	_, sig, _ := strings.Cut(token, ".")

	for _, payload := range []string{
		"2000|acct-1|f1|9999999999",
		"1000|acct-1|f2|9999999999",
	} {
		forged := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
		if _, err := Verify(secret, forged, now); !errors.Is(err, ErrInvalid) {
			t.Fatalf("payload %q: err = %v", payload, err)
		}
	}
}

func TestVerifyRejectsLegacyFormats(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, payload := range []string{
		"1001|file|9999999999",
		"s|1001|share|file|9999999999",
	} {
		token := signedToken(secret, payload)
		if _, err := Verify(secret, token, now); !errors.Is(err, ErrInvalid) {
			t.Fatalf("legacy payload %q: err = %v", payload, err)
		}
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	token := Sign(secret, 1000, "acct-1", "f1", time.Hour, now)
	if _, err := Verify(secret, token, now.Add(2*time.Hour)); !errors.Is(err, ErrExpired) {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, token := range []string{"", ".", "abc", "abc.def", "!!!!.!!!!"} {
		if _, err := Verify(secret, token, now); !errors.Is(err, ErrInvalid) {
			t.Fatalf("token %q: err = %v", token, err)
		}
	}
}

func TestDeriveSecretIsStableAndSeparate(t *testing.T) {
	master := []byte("master-key-master-key-master-key")
	a := DeriveSecret(master)
	b := DeriveSecret(master)
	if string(a) != string(b) {
		t.Fatal("DeriveSecret is not deterministic")
	}
	if string(a) == string(master) || len(a) != 32 {
		t.Fatalf("derived key = %x", a)
	}
}

func TestSignShareVerifyRoundTrip(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	token := SignShare(secret, 1000, "acct-1", "VOabc123", "sf1", 6*time.Hour, now)

	claims, err := Verify(secret, token, now)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.UID != 1000 || claims.AccountID != "acct-1" || claims.ShareID != "VOabc123" || claims.FileID != "sf1" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestDriveAndShareTokensDoNotSubstituteForEachOther(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	drive := Sign(secret, 1000, "acct-1", "f1", time.Hour, now)
	share := SignShare(secret, 1000, "acct-1", "VOabc123", "f1", time.Hour, now)

	dc, err := Verify(secret, drive, now)
	if err != nil || dc.ShareID != "" {
		t.Fatalf("drive claims = %+v err=%v", dc, err)
	}
	sc, err := Verify(secret, share, now)
	if err != nil || sc.ShareID != "VOabc123" {
		t.Fatalf("share claims = %+v err=%v", sc, err)
	}
}
