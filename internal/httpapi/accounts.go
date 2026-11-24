package httpapi

import (
	"context"
	crand "crypto/rand"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/probe"
	"fnos-pikpak/internal/store"
)

// namedClient pairs one account row with a client bound to its session, so a
// handler can act as the account and show which one it acted as.
type namedClient struct {
	Account store.PikPakAccount
	C       *pikpak.Client
}

// defaultAccount picks the account a bare uid resolves to: the first active
// row in bind order, else the oldest row. It mirrors the session layer's
// default-account fallback.
func defaultAccount(accounts []store.PikPakAccount) *store.PikPakAccount {
	for i := range accounts {
		if accounts[i].Status == "active" {
			return &accounts[i]
		}
	}
	if len(accounts) == 0 {
		return nil
	}
	return &accounts[0]
}

// resolveAccount maps the ?account= parameter to the caller's account row.
// An empty parameter selects the default account; a named one must be the
// caller's own (store.ErrNotFound otherwise, which the callers turn into a
// 404). A user bound before accounts existed owns a credential but no rows;
// like the session layer, the default lookup falls back to that credential
// under the legacy empty account id instead of demanding a re-bind.
func (deps Deps) resolveAccount(ctx context.Context, uid int, raw string) (store.PikPakAccount, error) {
	if raw != "" {
		return deps.Store.GetAccount(ctx, uid, raw)
	}
	accounts, err := deps.Store.ListAccounts(ctx, uid)
	if err != nil {
		return store.PikPakAccount{}, err
	}
	def := defaultAccount(accounts)
	if def != nil {
		return *def, nil
	}
	cred, err := deps.Store.GetCredential(ctx, uid)
	if errors.Is(err, store.ErrNotFound) {
		return store.PikPakAccount{}, pikpak.ErrNotBound
	}
	if err != nil {
		return store.PikPakAccount{}, err
	}
	// The session layer serves the placeholder credential ("uid:<n>" key,
	// see pikpak.legacyAccountID) under the empty account id; ask for exactly
	// that so both layers resolve the same session.
	id := ""
	if !strings.HasPrefix(cred.AccountID, "uid:") {
		id = cred.AccountID
	}
	return store.PikPakAccount{ID: id, UID: uid, Status: "active"}, nil
}

// accountClient returns a client bound to the ?account= account, or to the
// default account when the parameter is absent. On failure the response is
// already written and ok is false.
func accountClient(w http.ResponseWriter, r *http.Request, deps Deps) (*pikpak.Client, store.PikPakAccount, bool) {
	id, _ := fnos.IdentityFrom(r.Context())
	acct, err := deps.resolveAccount(r.Context(), id.UID, r.URL.Query().Get("account"))
	if err != nil {
		switch {
		case errors.Is(err, pikpak.ErrNotBound):
			writeError(w, http.StatusUnauthorized, "not_bound", "请先绑定 PikPak 账号")
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "账号不存在")
		default:
			writeError(w, http.StatusInternalServerError, "internal", "无法读取账号")
		}
		return nil, store.PikPakAccount{}, false
	}
	client, err := deps.Sessions.ClientForAccount(r.Context(), id.UID, acct.ID)
	if err != nil {
		if errors.Is(err, pikpak.ErrNotBound) {
			writeError(w, http.StatusUnauthorized, "not_bound", "请先绑定 PikPak 账号")
		} else {
			writeError(w, http.StatusInternalServerError, "internal", "无法加载 PikPak 会话")
		}
		return nil, store.PikPakAccount{}, false
	}
	return client, acct, true
}

// allAccountClients returns a client per active account. Accounts whose
// session cannot be loaded (credential already gone) are skipped; if that
// leaves nothing, the caller sees a 401 rather than an empty list that would
// look like success.
func allAccountClients(w http.ResponseWriter, r *http.Request, deps Deps) ([]namedClient, bool) {
	id, _ := fnos.IdentityFrom(r.Context())
	accounts, err := deps.Store.ListAccounts(r.Context(), id.UID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "无法读取账号")
		return nil, false
	}
	var out []namedClient
	for _, acct := range accounts {
		if acct.Status != "active" {
			continue
		}
		client, err := deps.Sessions.ClientForAccount(r.Context(), id.UID, acct.ID)
		if err != nil {
			continue
		}
		out = append(out, namedClient{Account: acct, C: client})
	}
	if len(out) == 0 {
		writeError(w, http.StatusUnauthorized, "not_bound", "请先绑定 PikPak 账号")
		return nil, false
	}
	return out, true
}

// randomAccount returns one active account in random order, so operations
// that may act on any identity spread load instead of always leaning on the
// default account. Without a usable active account the refusal is structured:
// the caller should show the re-bind prompt, not an error toast.
func randomAccount(w http.ResponseWriter, r *http.Request, deps Deps) (namedClient, bool) {
	id, _ := fnos.IdentityFrom(r.Context())
	accounts, err := deps.Store.ListAccounts(r.Context(), id.UID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "无法读取账号")
		return namedClient{}, false
	}
	for _, acct := range shuffledActives(accounts) {
		client, err := deps.Sessions.ClientForAccount(r.Context(), id.UID, acct.ID)
		if err != nil {
			continue
		}
		return namedClient{Account: acct, C: client}, true
	}
	writeError(w, http.StatusUnauthorized, "no_active_account", "没有可用的 PikPak 账号，请重新授权或绑定")
	return namedClient{}, false
}

// shuffledActives returns the active accounts in crypto-random order. A
// randomness failure keeps the original order, which is still a permutation.
func shuffledActives(accounts []store.PikPakAccount) []store.PikPakAccount {
	actives := make([]store.PikPakAccount, 0, len(accounts))
	for _, a := range accounts {
		if a.Status == "active" {
			actives = append(actives, a)
		}
	}
	for i := len(actives) - 1; i > 0; i-- {
		j, err := crand.Int(crand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			break
		}
		actives[i], actives[j.Int64()] = actives[j.Int64()], actives[i]
	}
	return actives
}

// registerAccountsRoute serves GET /api/accounts: one row per bound account,
// with quotas and VIP probed per account in parallel (a handful of accounts,
// so no semaphore) and cached by account id.
func registerAccountsRoute(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		accounts, err := deps.Store.ListAccounts(r.Context(), id.UID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "无法读取账号")
			return
		}
		if len(accounts) == 0 {
			writeError(w, http.StatusUnauthorized, "not_bound", "请先绑定 PikPak 账号")
			return
		}
		items := make([]map[string]any, len(accounts))
		var wg sync.WaitGroup
		for i, acct := range accounts {
			wg.Add(1)
			go func(i int, acct store.PikPakAccount) {
				defer wg.Done()
				items[i] = deps.accountItem(r.Context(), id.UID, acct)
			}(i, acct)
		}
		wg.Wait()
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})
}

// accountItem builds one /api/accounts row: identity fields from the account
// record, quotas and VIP from the cached snapshot or a fresh probe. A failed
// probe keeps the stored VIP data, zeroes the quotas, and reports the
// redacted upstream message in the row itself — one dead account must not
// blank the others.
func (deps Deps) accountItem(ctx context.Context, uid int, acct store.PikPakAccount) map[string]any {
	item := map[string]any{
		"id":           acct.ID,
		"display_name": acct.DisplayName,
		"avatar":       acct.Avatar,
		"vip_type":     acct.VIPType,
		"vip_expire":   acct.VIPExpire,
		"status":       acct.Status,
		"quota_limit":  int64(0),
		"quota_usage":  int64(0),
		"error":        "",
	}
	if snap, ok := deps.Accounts.get(acct.ID); ok {
		applySnapshot(item, snap)
		return item
	}
	client, err := deps.Sessions.ClientForAccount(ctx, uid, acct.ID)
	if err != nil {
		item["error"] = probe.Redact(err.Error())
		return item
	}
	about, err := client.About(ctx)
	if err != nil {
		item["error"] = probe.Redact(err.Error())
		return item
	}
	vip, err := client.VIP(ctx)
	if err != nil {
		item["error"] = probe.Redact(err.Error())
		return item
	}
	snap := accountSnapshot{
		QuotaLimit: about.QuotaLimit,
		QuotaUsage: about.QuotaUsage,
		VIPType:    vip.Type,
		VIPExpire:  vip.Expire,
		CachedAt:   time.Now().UTC(),
	}
	deps.Accounts.put(acct.ID, snap)
	applySnapshot(item, snap)
	return item
}

// applySnapshot overlays a quota/VIP snapshot onto an item row, keeping the
// account record's VIP data when the snapshot carries none.
func applySnapshot(item map[string]any, snap accountSnapshot) {
	item["quota_limit"] = snap.QuotaLimit
	item["quota_usage"] = snap.QuotaUsage
	if snap.VIPType != "" {
		item["vip_type"] = snap.VIPType
	}
	if snap.VIPExpire != "" {
		item["vip_expire"] = snap.VIPExpire
	}
}
