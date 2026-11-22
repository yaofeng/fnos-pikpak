package playtoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrInvalid covers malformed tokens and signature mismatches alike; the
	// caller does not need to distinguish them.
	ErrInvalid = errors.New("playtoken: invalid token")
	ErrExpired = errors.New("playtoken: token expired")
)

// Claims is what a stream URL carries: who may read, which file, until when.
type Claims struct {
	UID       int
	FileID    string
	ExpiresAt time.Time
}

// DeriveSecret turns the store's master key into a key used only for stream
// tokens, so the credential-encryption key is never used for two purposes.
func DeriveSecret(master []byte) []byte {
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte("fnos-pikpak/play-token/v1"))
	return mac.Sum(nil)
}

// Sign returns base64url(payload) "." base64url(HMAC-SHA256(secret, payload))
// where payload is "uid|file|unix_expiry".
func Sign(secret []byte, uid int, fileID string, ttl time.Duration, now time.Time) string {
	payload := strconv.Itoa(uid) + "|" + fileID + "|" + strconv.FormatInt(now.Add(ttl).Unix(), 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sign(secret, payload)
}

func sign(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Verify checks the signature first, then the expiry, and only then parses the
// payload, so a forged payload never reaches the parser.
func Verify(secret []byte, token string, now time.Time) (Claims, error) {
	encoded, sig, ok := strings.Cut(token, ".")
	if !ok || encoded == "" || sig == "" {
		return Claims{}, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	want, err := base64.RawURLEncoding.DecodeString(sign(secret, string(raw)))
	if err != nil {
		return Claims{}, ErrInvalid
	}
	if !hmac.Equal(got, want) {
		return Claims{}, ErrInvalid
	}

	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return Claims{}, ErrInvalid
	}
	uid, err := strconv.Atoi(parts[0])
	if err != nil || uid <= 0 {
		return Claims{}, ErrInvalid
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	if now.Unix() >= exp {
		return Claims{}, ErrExpired
	}
	return Claims{UID: uid, FileID: parts[1], ExpiresAt: time.Unix(exp, 0)}, nil
}
