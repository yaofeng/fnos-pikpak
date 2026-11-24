package httpapi

import (
	"strconv"
	"sync"
	"time"
)

// shareSessionTTL bounds how long an opened share stays browsable without the
// pass code being entered again. The upstream token is never sent to the
// browser; it lives here and dies with the process. The pass code is kept in
// plaintext on purpose: a restore may have to reopen the share as a different
// account, and only the code can do that.
const shareSessionTTL = 2 * time.Hour

type shareSession struct {
	token    string
	passCode string
	expires  time.Time
}

type shareSessions struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]shareSession
}

func newShareSessions() *shareSessions {
	return &shareSessions{ttl: shareSessionTTL, items: map[string]shareSession{}}
}

// shareSessionKey namespaces sessions by (uid, account, share): one fnOS user
// can hold several PikPak identities, and each opens a share with its own
// pass-code token.
func shareSessionKey(uid int, accountID, shareID string) string {
	return strconv.Itoa(uid) + "|" + accountID + "|" + shareID
}

func (s *shareSessions) put(uid int, accountID, shareID, token, passCode string, now time.Time) {
	s.mu.Lock()
	s.items[shareSessionKey(uid, accountID, shareID)] = shareSession{
		token:    token,
		passCode: passCode,
		expires:  now.Add(s.ttl),
	}
	s.mu.Unlock()
}

func (s *shareSessions) get(uid int, accountID, shareID string, now time.Time) (token, passCode string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[shareSessionKey(uid, accountID, shareID)]
	if !ok || now.After(item.expires) {
		return "", "", false
	}
	return item.token, item.passCode, true
}
