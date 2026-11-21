package fnos

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

var (
	ErrNoIdentity        = errors.New("fnos: no identity header")
	ErrDuplicateIdentity = errors.New("fnos: duplicate identity header")
	ErrBadIdentity       = errors.New("fnos: bad identity header")
)

// Identity is the authenticated fnOS user, as forwarded by the unified gateway.
// It must never be taken from a request body or query parameter.
type Identity struct {
	UID      int
	Username string
	IsAdmin  bool
}

type identityKey struct{}

func withIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFrom returns the identity stored by the middleware.
func IdentityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(Identity)
	return id, ok
}

// ExtractIdentity reads X-Trim-Userid. More than one value is rejected: the
// gateway is not documented to strip client-supplied copies, so a duplicate
// means someone is trying to spoof another user.
func ExtractIdentity(h http.Header) (Identity, error) {
	values := h.Values("X-Trim-Userid")
	switch {
	case len(values) == 0:
		return Identity{}, ErrNoIdentity
	case len(values) > 1:
		return Identity{}, ErrDuplicateIdentity
	}

	uid, err := strconv.Atoi(values[0])
	if err != nil || uid <= 0 {
		return Identity{}, ErrBadIdentity
	}

	isAdmin := false
	switch h.Get("X-Trim-Isadmin") {
	case "1", "true":
		isAdmin = true
	}

	return Identity{UID: uid, Username: h.Get("X-Trim-Username"), IsAdmin: isAdmin}, nil
}

// WithIdentity authenticates the request. When the header is absent and
// devUID > 0 (development flag only), it falls back to that identity so a
// browser on localhost works without the gateway.
func WithIdentity(devUID int, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := ExtractIdentity(r.Header)
		if errors.Is(err, ErrNoIdentity) && devUID > 0 {
			id, err = Identity{UID: devUID, Username: "dev"}, nil
		}
		if err != nil {
			code := http.StatusUnauthorized
			msg := "缺少飞牛用户身份"
			switch {
			case errors.Is(err, ErrDuplicateIdentity):
				code = http.StatusBadRequest
				msg = "身份头重复，请求已拒绝"
			case errors.Is(err, ErrBadIdentity):
				msg = "身份头格式错误"
			}
			writeJSON(w, code, map[string]any{"error": msg})
			return
		}
		next.ServeHTTP(w, r.WithContext(withIdentity(r.Context(), id)))
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
