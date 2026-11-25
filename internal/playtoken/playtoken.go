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
	// ErrEmptyAccount is what the minting functions answer when the caller
	// has no account id. A token without one can never be spent — Verify
	// rejects the legacy account-less formats — so minting fails fast
	// instead of signing something unusable.
	ErrEmptyAccount = errors.New("playtoken: account id is empty")
)

// Claims is what a stream URL carries: who may read, which file, until when.
// ShareID is set only for a file inside a share; an empty ShareID means the
// file belongs to the user's own drive. AccountID names the pikpak account
// the file lives on, so a token minted for one account can never be spent
// through another account's session.
type Claims struct {
	UID       int
	AccountID string
	FileID    string
	ShareID   string
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
// where payload is "uid|acct|file|unix_expiry". accountID must be a real
// account id: an empty one is a caller bug (ErrEmptyAccount), not something
// to sign.
func Sign(secret []byte, uid int, accountID, fileID string, ttl time.Duration, now time.Time) (string, error) {
	if accountID == "" {
		return "", ErrEmptyAccount
	}
	payload := strconv.Itoa(uid) + "|" + accountID + "|" + fileID + "|" +
		strconv.FormatInt(now.Add(ttl).Unix(), 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sign(secret, payload), nil
}

// SignShare signs a file inside a share. The payload starts with "s" so it can
// never be confused with a drive-file token. Like Sign, it refuses an empty
// account id.
func SignShare(secret []byte, uid int, accountID, shareID, fileID string, ttl time.Duration, now time.Time) (string, error) {
	if accountID == "" {
		return "", ErrEmptyAccount
	}
	payload := "s|" + strconv.Itoa(uid) + "|" + accountID + "|" + shareID + "|" + fileID + "|" +
		strconv.FormatInt(now.Add(ttl).Unix(), 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sign(secret, payload), nil
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

	// 段数即格式：4 段是 drive 令牌，"s" 开头的 6 段是 share 令牌；旧格式
	// （3 段 drive / 5 段 share，无账号段）在这里直接拒绝，签发方升级后
	// 旧令牌一律失效。
	parts := strings.Split(string(raw), "|")
	var uidIdx int
	var accountID, shareID, fileID, expText string
	switch {
	case len(parts) == 4:
		uidIdx = 0
		accountID, fileID, expText = parts[1], parts[2], parts[3]
	case len(parts) == 6 && parts[0] == "s":
		uidIdx = 1
		accountID, shareID, fileID, expText = parts[2], parts[3], parts[4], parts[5]
	default:
		return Claims{}, ErrInvalid
	}
	uid, err := strconv.Atoi(parts[uidIdx])
	if err != nil || uid <= 0 {
		return Claims{}, ErrInvalid
	}
	exp, err := strconv.ParseInt(expText, 10, 64)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	if now.Unix() >= exp {
		return Claims{}, ErrExpired
	}
	return Claims{
		UID:       uid,
		AccountID: accountID,
		FileID:    fileID,
		ShareID:   shareID,
		ExpiresAt: time.Unix(exp, 0),
	}, nil
}
