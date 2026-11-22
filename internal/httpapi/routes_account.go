package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/probe"
	"fnos-pikpak/internal/store"
)

const defaultAccountTTL = 60 * time.Second

// accountSnapshot is the quota/VIP summary shown in the app shell. It is
// cached per user because the upstream calls are slow and the numbers only
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
	entries map[int]accountEntry
}

func newAccountCache(ttl time.Duration) *accountCache {
	if ttl <= 0 {
		ttl = defaultAccountTTL
	}
	return &accountCache{ttl: ttl, entries: map[int]accountEntry{}}
}

func (c *accountCache) get(uid int) (accountSnapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[uid]
	if !ok || time.Since(e.fetchedAt) >= c.ttl {
		return accountSnapshot{}, false
	}
	return e.snap, true
}

func (c *accountCache) put(uid int, s accountSnapshot) {
	c.mu.Lock()
	c.entries[uid] = accountEntry{snap: s, fetchedAt: time.Now()}
	c.mu.Unlock()
}

func (c *accountCache) drop(uid int) {
	c.mu.Lock()
	delete(c.entries, uid)
	c.mu.Unlock()
}

func registerAccountRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/account", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		if _, err := deps.Sessions.Get(r.Context(), id.UID); errors.Is(err, pikpak.ErrNotBound) {
			writeError(w, http.StatusUnauthorized, "not_bound", "请先绑定 PikPak 账号")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "无法加载 PikPak 会话")
			return
		}

		if snap, ok := deps.Accounts.get(id.UID); ok {
			writeJSON(w, http.StatusOK, snap)
			return
		}

		client, err := deps.Sessions.ClientFor(r.Context(), id.UID)
		if err != nil {
			writeError(w, http.StatusBadGateway, "pikpak_error", probe.Redact(err.Error()))
			return
		}
		about, err := client.About(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, "pikpak_error", probe.Redact(err.Error()))
			return
		}
		vip, err := client.VIP(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, "pikpak_error", probe.Redact(err.Error()))
			return
		}

		snap := accountSnapshot{
			QuotaLimit: about.QuotaLimit,
			QuotaUsage: about.QuotaUsage,
			VIPType:    vip.Type,
			VIPExpire:  vip.Expire,
			CachedAt:   time.Now().UTC(),
		}
		deps.Accounts.put(id.UID, snap)
		writeJSON(w, http.StatusOK, snap)
	})

	mux.HandleFunc("POST /api/auth/rebind", func(w http.ResponseWriter, r *http.Request) {
		if !requireJSONBody(w, r) {
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		if _, err := deps.Sessions.Get(r.Context(), id.UID); errors.Is(err, pikpak.ErrNotBound) {
			writeError(w, http.StatusNotFound, "not_bound", "当前用户尚未绑定 PikPak 账号")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "无法加载 PikPak 会话")
			return
		}

		err := deps.Store.SetUserStatus(r.Context(), id.UID, "active")
		if errors.Is(err, store.ErrNotFound) {
			err = deps.Store.PutUser(r.Context(), store.User{UID: id.UID, Status: "active"})
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "无法更新账号状态")
			return
		}
		deps.Accounts.drop(id.UID)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "active"})
	})

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

// meResponse is the app shell's first call: who am I, is the account linked,
// and what can this session do.
func meResponse(ctx context.Context, deps Deps, uid int, bound bool) map[string]any {
	status := "unbound"
	var user store.User
	if u, err := deps.Store.GetUser(ctx, uid); err == nil {
		user = u
	} else if bound {
		status = "active"
	}
	if bound {
		status = "active"
		if user.Status != "" {
			status = user.Status
		}
	}

	return map[string]any{
		"uid":          uid,
		"bound":        bound,
		"status":       status,
		"debug":        deps.Debug,
		"display_name": user.DisplayName,
		"vip_type":     user.VIPType,
		"capabilities": map[string]bool{
			"can_purge":   true,
			"can_play":    true,
			"can_upload":  true,
			"can_offline": true,
		},
	}
}
