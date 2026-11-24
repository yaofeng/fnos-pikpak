package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/store"
)

const defaultAccountTTL = 60 * time.Second

// accountSnapshot is the quota/VIP summary shown in the app shell. It is
// cached per account because the upstream calls are slow and the numbers only
// matter at minute granularity.
type accountSnapshot struct {
	QuotaLimit int64     `json:"quota_limit"`
	QuotaUsage int64     `json:"quota_usage"`
	VIPType    string    `json:"vip_type"`
	VIPExpire  string    `json:"vip_expire"`
	CachedAt   time.Time `json:"cached_at"`
}

type accountEntry struct {
	snap      accountSnapshot
	fetchedAt time.Time
}

type accountCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]accountEntry
}

func newAccountCache(ttl time.Duration) *accountCache {
	if ttl <= 0 {
		ttl = defaultAccountTTL
	}
	return &accountCache{ttl: ttl, entries: map[string]accountEntry{}}
}

func (c *accountCache) get(accountID string) (accountSnapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[accountID]
	if !ok || time.Since(e.fetchedAt) >= c.ttl {
		return accountSnapshot{}, false
	}
	return e.snap, true
}

func (c *accountCache) put(accountID string, s accountSnapshot) {
	c.mu.Lock()
	c.entries[accountID] = accountEntry{snap: s, fetchedAt: time.Now()}
	c.mu.Unlock()
}

func (c *accountCache) drop(accountID string) {
	c.mu.Lock()
	delete(c.entries, accountID)
	c.mu.Unlock()
}

func registerAccountRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("POST /api/auth/flow/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if !requireJSONBody(w, r) {
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		flow, err := deps.Store.GetAuthFlow(r.Context(), r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) || (err == nil && flow.UID != id.UID) {
			writeError(w, http.StatusNotFound, "not_found", "授权流程不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "无法读取授权流程")
			return
		}
		if flow.Status == flowStatusPending {
			flow.Status = "cancelled"
			if err := deps.Store.PutAuthFlow(r.Context(), flow); err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "无法取消授权流程")
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": flow.ID, "status": flow.Status})
	})
}

// meResponse is the app shell's first call: who am I, is a PikPak account
// linked, and what can this session do. Every bound account counts; the
// worst status wins so a single reauth banner is never hidden behind a
// healthy default account.
func meResponse(ctx context.Context, deps Deps, uid int) map[string]any {
	accounts, err := deps.Store.ListAccounts(ctx, uid)
	if err != nil {
		accounts = nil
	}
	bound := len(accounts) > 0
	status := "unbound"
	displayName, vipType := "", ""
	if bound {
		status = "active"
		for _, a := range accounts {
			if a.Status == "reauth_required" {
				status = "reauth_required"
				break
			}
		}
		if def := defaultAccount(accounts); def != nil {
			displayName = def.DisplayName
			vipType = def.VIPType
		}
	}

	return map[string]any{
		"uid":          uid,
		"bound":        bound,
		"status":       status,
		"debug":        deps.Debug,
		"display_name": displayName,
		"vip_type":     vipType,
		"accounts":     len(accounts),
		"capabilities": map[string]bool{
			"can_purge":   true,
			"can_play":    true,
			"can_upload":  true,
			"can_offline": true,
		},
	}
}
