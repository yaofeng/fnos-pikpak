package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/probe"
	"fnos-pikpak/internal/store"
)

const (
	flowStatusPending = "pending"
	flowStatusBound   = "bound"
	flowStatusDenied  = "denied"
	flowStatusExpired = "expired"
	flowStatusError   = "error"
)

func registerAuthRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		writeJSON(w, http.StatusOK, meResponse(r.Context(), deps, id.UID))
	})

	mux.HandleFunc("POST /api/auth/device", func(w http.ResponseWriter, r *http.Request) {
		if !requireJSONBody(w, r) {
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		var body struct {
			AccountID string `json:"account_id"`
		}
		// The body is optional (an empty one starts a new-account bind), so
		// only malformed JSON is rejected here.
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
			return
		}

		// The flow lands on one account: an existing row when account_id
		// names one (404 when it is not the caller's), a freshly minted id
		// otherwise. Fixing the id at creation is what lets a logout of that
		// account cancel this flow before its poller finishes.
		targetID := body.AccountID
		deviceID := ""
		if targetID != "" {
			acct, err := deps.Store.GetAccount(r.Context(), id.UID, targetID)
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "账号不存在")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "无法读取账号")
				return
			}
			// Re-authorization keeps the device fingerprint the account
			// originally bound with.
			if cred, cerr := deps.Store.GetAccountCredential(r.Context(), id.UID, acct.ID); cerr == nil && cred.DeviceID != "" {
				deviceID = cred.DeviceID
			}
		} else {
			fresh, err := store.NewAccountID()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "无法创建授权流程")
				return
			}
			targetID = fresh
		}
		if deviceID == "" {
			deviceID = deps.Store.DeriveDeviceID(targetID)
		}

		client := deps.Sessions.Client()
		bound := *client
		bound.DeviceID = deviceID

		flow, err := bound.StartDeviceFlow(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, "pikpak_error", probe.Redact(err.Error()))
			return
		}

		flowID, err := randomFlowID()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "无法创建授权流程")
			return
		}
		expiresIn := flow.ExpiresIn
		if expiresIn <= 0 {
			expiresIn = 300
		}
		interval := flow.Interval
		if interval <= 0 {
			interval = 2
		}

		rec := store.Flow{
			ID:          flowID,
			UID:         id.UID,
			AccountID:   targetID,
			DeviceCode:  flow.DeviceCode,
			UserCode:    flow.UserCode,
			VerifyURL:   flow.VerifyURL,
			IntervalSec: interval,
			ExpiresAt:   time.Now().Add(time.Duration(expiresIn) * time.Second),
			Status:      flowStatusPending,
		}
		if err := deps.Store.PutAuthFlow(r.Context(), rec); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "无法保存授权流程")
			return
		}

		go pollAuthFlow(deps, &bound, rec)

		writeJSON(w, http.StatusOK, map[string]any{
			"id":         flowID,
			"user_code":  flow.UserCode,
			"verify_url": flow.VerifyURL,
			"expires_in": expiresIn,
			"interval":   interval,
		})
	})

	mux.HandleFunc("GET /api/auth/flow/{id}", func(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, http.StatusOK, map[string]any{
			"id":         flow.ID,
			"status":     flow.Status,
			"user_code":  flow.UserCode,
			"verify_url": flow.VerifyURL,
			"expires_at": flow.ExpiresAt.UTC().Format(time.RFC3339),
			"error":      flow.Error,
		})
	})

	mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if !requireJSONBody(w, r) {
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		var body struct {
			AccountID string `json:"account_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
			return
		}
		if body.AccountID == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "请指定要解绑的账号")
			return
		}

		if err := deps.Sessions.DropAccount(r.Context(), id.UID, body.AccountID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "解绑失败")
			return
		}
		if deps.Store != nil {
			if err := deps.Store.DeleteAccount(r.Context(), id.UID, body.AccountID); err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "解绑失败")
				return
			}
			// Unbinding throws the account away; its transfer jobs can never
			// run again, so their workers stop and the records go with them.
			// Files on disk stay.
			if err := deps.Store.DeleteAccountJobs(r.Context(), id.UID, body.AccountID); err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "解绑失败")
				return
			}
		}
		if deps.Queue != nil {
			deps.Queue.CancelAccount(id.UID, body.AccountID)
		}
		deps.Accounts.drop(body.AccountID)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}

// pollAuthFlow runs in the background until the flow resolves or expires.
// It outlives the request that started it, so it uses its own context.
func pollAuthFlow(deps Deps, client *pikpak.Client, flow store.Flow) {
	ctx := context.Background()
	interval := time.Duration(flow.IntervalSec) * time.Second
	deadline := flow.ExpiresAt

	for {
		timer := time.NewTimer(interval)
		select {
		case <-timer.C:
		}

		if time.Now().After(deadline) {
			finishFlow(deps, flow, flowStatusExpired, "设备码已过期，请重新发起绑定")
			return
		}

		tok, err := client.PollDeviceToken(ctx, flow.DeviceCode)
		switch {
		case err == nil:
			// Logout cancels pending flows; a token that arrives afterwards
			// must not bind the account.
			current, ferr := deps.Store.GetAuthFlow(ctx, flow.ID)
			if ferr != nil || current.Status != flowStatusPending {
				return
			}
			if msg := completeBind(deps, current, client, tok); msg != "" {
				finishFlow(deps, flow, flowStatusError, msg)
				return
			}
			finishFlow(deps, flow, flowStatusBound, "")
			return
		case errors.Is(err, pikpak.ErrAuthorizationPending):
			continue
		case errors.Is(err, pikpak.ErrSlowDown):
			interval += 5 * time.Second
			continue
		case errors.Is(err, pikpak.ErrAccessDenied):
			finishFlow(deps, flow, flowStatusDenied, "授权已被拒绝")
			return
		case errors.Is(err, pikpak.ErrExpiredToken):
			finishFlow(deps, flow, flowStatusExpired, "设备码已过期，请重新发起绑定")
			return
		default:
			finishFlow(deps, flow, flowStatusError, probe.Redact(pikpak.TrimFlowError(err)))
			return
		}
	}
}

// completeBind stores the authorized token under the flow's account and
// refreshes the account row, returning the flow error message (empty on
// success). A flow that targets an existing row must authorize the identity
// already bound there; a new one must not duplicate an identity this user has
// bound to another account.
func completeBind(deps Deps, flow store.Flow, client *pikpak.Client, tok pikpak.Token) string {
	ctx := context.Background()
	uid := flow.UID
	acctID := flow.AccountID
	if acctID == "" {
		// Flows created before accounts existed carry no id; bind completion
		// mints one so the account model stays uniform.
		id, err := store.NewAccountID()
		if err != nil {
			return "保存账号信息失败"
		}
		acctID = id
	}

	existing, err := deps.Store.GetAccount(ctx, uid, acctID)
	switch {
	case err == nil:
		// Re-binding an existing row.
		if existing.PikPakSub != "" {
			if tok.Sub != existing.PikPakSub {
				return "授权的账号与原账号不一致，请解绑后重新添加"
			}
		} else if boundID, lerr := deps.Store.AccountIDBySub(ctx, uid, tok.Sub); lerr == nil && boundID != acctID {
			return "该 PikPak 账号已绑定"
		}
		existing.PikPakSub = tok.Sub
		existing.Status = "active"
		if err := deps.Store.PutAccount(ctx, existing); err != nil {
			return "保存账号信息失败"
		}
	case errors.Is(err, store.ErrNotFound):
		// Adding a new account.
		if _, lerr := deps.Store.AccountIDBySub(ctx, uid, tok.Sub); lerr == nil {
			return "该 PikPak 账号已绑定"
		} else if !errors.Is(lerr, store.ErrNotFound) {
			return "保存账号信息失败"
		}
		if err := deps.Store.PutAccount(ctx, store.PikPakAccount{
			ID:        acctID,
			UID:       uid,
			PikPakSub: tok.Sub,
			Status:    "active",
		}); err != nil {
			return "保存账号信息失败"
		}
	default:
		return "保存账号信息失败"
	}

	cred := store.Credential{
		UID:             uid,
		AccountID:       acctID,
		AuthType:        "oauth",
		ClientID:        client.ClientID,
		AccessToken:     tok.AccessToken,
		RefreshToken:    tok.RefreshToken,
		DeviceID:        client.DeviceID,
		AccessExpiresAt: time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
		Scopes:          pikpak.Scope,
	}
	if err := deps.Store.PutCredential(ctx, cred); err != nil {
		return "保存凭据失败"
	}
	deps.Accounts.drop(acctID)
	deps.Sessions.InstallAccount(uid, acctID, cred)
	return ""
}

func finishFlow(deps Deps, flow store.Flow, status, msg string) {
	ctx := context.Background()
	current, err := deps.Store.GetAuthFlow(ctx, flow.ID)
	if err != nil {
		current = flow
	}
	current.Status = status
	current.Error = msg
	_ = deps.Store.PutAuthFlow(ctx, current)
}

func randomFlowID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
