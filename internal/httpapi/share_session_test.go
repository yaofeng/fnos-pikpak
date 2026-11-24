package httpapi

import (
	"testing"
	"time"
)

func TestShareSessionStoresPassCode(t *testing.T) {
	s := newShareSessions()
	now := time.Now()
	s.put(1000, "acct-a", "VOabc123", "tok-1", "123", now)

	token, code, ok := s.get(1000, "acct-a", "VOabc123", now.Add(time.Minute))
	if !ok || token != "tok-1" || code != "123" {
		t.Fatalf("get = %q %q %v, want tok-1 123 true", token, code, ok)
	}

	// The same share opened under another account is a different session.
	if _, _, ok := s.get(1000, "acct-b", "VOabc123", now.Add(time.Minute)); ok {
		t.Fatal("another account saw this account's session")
	}
	// Another user neither.
	if _, _, ok := s.get(1001, "acct-a", "VOabc123", now.Add(time.Minute)); ok {
		t.Fatal("another user saw the session")
	}
	// Past the TTL the entry is gone, whatever it held.
	if _, _, ok := s.get(1000, "acct-a", "VOabc123", now.Add(shareSessionTTL+time.Minute)); ok {
		t.Fatal("an expired session was served")
	}
}
