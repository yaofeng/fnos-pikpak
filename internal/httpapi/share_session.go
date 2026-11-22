package httpapi

import (
	"strconv"
	"sync"
	"time"
)

// shareSessionTTL bounds how long an opened share stays browsable without the
// pass code being entered again. The upstream token is never sent to the
// browser; it lives here and dies with the process.
const shareSessionTTL = 2 * time.Hour

type shareSession struct {
	token   string
	expires time.Time
}

type shareSessions struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]shareSession
}

func newShareSessions() *shareSessions {
	return &shareSessions{ttl: shareSessionTTL, items: map[string]shareSession{}}
}

func shareSessionKey(uid int, shareID string) string {
	return strconv.Itoa(uid) + "|" + shareID
}

func (s *shareSessions) put(uid int, shareID, token string, now time.Time) {
	s.mu.Lock()
	s.items[shareSessionKey(uid, shareID)] = shareSession{token: token, expires: now.Add(s.ttl)}
	s.mu.Unlock()
}

func (s *shareSessions) get(uid int, shareID string, now time.Time) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[shareSessionKey(uid, shareID)]
	if !ok || now.After(item.expires) {
		return "", false
	}
	return item.token, true
}
