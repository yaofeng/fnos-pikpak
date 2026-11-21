package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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
		bound := false
		if _, err := deps.Sessions.Get(r.Context(), id.UID); err == nil {
			bound = true
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"uid":   id.UID,
			"bound": bound,
		})
	})

	mux.HandleFunc("POST /api/auth/device", func(w http.ResponseWriter, r *http.Request) {
		if !requireJSONBody(w, r) {
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		client := deps.Sessions.Client()
		bound := *client
		bound.DeviceID = deps.Store.DeriveDeviceID(id.UID)

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
		if err := deps.Sessions.Drop(r.Context(), id.UID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "解绑失败")
			return
		}
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
			// must not rebind the account.
			current, ferr := deps.Store.GetAuthFlow(ctx, flow.ID)
			if ferr != nil || current.Status != flowStatusPending {
				return
			}
			cred := store.Credential{
				UID:             flow.UID,
				AuthType:        "oauth",
				ClientID:        client.ClientID,
				AccessToken:     tok.AccessToken,
				RefreshToken:    tok.RefreshToken,
				DeviceID:        client.DeviceID,
				AccessExpiresAt: time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
				Scopes:          pikpak.Scope,
			}
			if err := deps.Store.PutCredential(ctx, cred); err != nil {
				finishFlow(deps, flow, flowStatusError, "保存凭据失败")
				return
			}
			deps.Sessions.Install(flow.UID, cred)
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
