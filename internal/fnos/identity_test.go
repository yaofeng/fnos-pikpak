package fnos

import (
	"errors"
	"net/http"
	"testing"
)

func TestExtractIdentity(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		_, err := ExtractIdentity(http.Header{})
		if !errors.Is(err, ErrNoIdentity) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("duplicate", func(t *testing.T) {
		h := http.Header{}
		h.Add("X-Trim-Userid", "1000")
		h.Add("X-Trim-Userid", "1001")
		_, err := ExtractIdentity(h)
		if !errors.Is(err, ErrDuplicateIdentity) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("bad", func(t *testing.T) {
		h := http.Header{"X-Trim-Userid": {"abc"}}
		_, err := ExtractIdentity(h)
		if !errors.Is(err, ErrBadIdentity) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("non-positive", func(t *testing.T) {
		h := http.Header{"X-Trim-Userid": {"0"}}
		_, err := ExtractIdentity(h)
		if !errors.Is(err, ErrBadIdentity) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("ok", func(t *testing.T) {
		h := http.Header{"X-Trim-Userid": {"1000"}, "X-Trim-Username": {"me"}, "X-Trim-Isadmin": {"1"}}
		id, err := ExtractIdentity(h)
		if err != nil || id.UID != 1000 || id.Username != "me" || !id.IsAdmin {
			t.Fatalf("%+v %v", id, err)
		}
	})
}
