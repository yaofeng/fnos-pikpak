package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/store"
)

const (
	// shareStatsMaxPages bounds one share's walk: a share that needs more
	// pages than this is reported as uncountable rather than locking the
	// list for minutes.
	shareStatsMaxPages = 400
	// shareStatsTimeout is how long one share may take to open and count.
	shareStatsTimeout = 12 * time.Second
	// shareStatsPerList caps the uncounted favourites one list call fills in,
	// so the first load after an upgrade stays responsive; the client asks
	// again while stats_pending stays above zero.
	shareStatsPerList = 3
)

var errShareTooLarge = errors.New("share has too many entries to count")

// shareTotals walks every folder of a share and counts the files in them.
// PikPak reports only the root entry count (file_num), so the totals the list
// shows have to be counted here and remembered by the caller.
func shareTotals(ctx context.Context, client *pikpak.Client, shareID, passToken string) (files, size int64, err error) {
	queue := []string{""}
	pages := 0
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		pageToken := ""
		for {
			if err := ctx.Err(); err != nil {
				return 0, 0, err
			}
			pages++
			if pages > shareStatsMaxPages {
				return 0, 0, errShareTooLarge
			}
			page, err := client.ListShareFiles(ctx, shareID, parent, passToken, pageToken)
			if err != nil {
				return 0, 0, err
			}
			for _, f := range page.Items {
				if f.Kind == "drive#folder" {
					queue = append(queue, f.ID)
					continue
				}
				files++
				size += f.Size
			}
			if page.NextPageToken == "" {
				break
			}
			pageToken = page.NextPageToken
		}
	}
	return files, size, nil
}

// refreshShareFavoriteStats opens a share and records it needs no more: who
// shared it, when, whether it expires, and (when count is set) the totals a
// recursive walk finds. A failed attempt is still marked checked, so a share
// that cannot be opened does not cost a PikPak round trip on every load.
func refreshShareFavoriteStats(ctx context.Context, deps Deps, uid int, fav store.ShareFavorite, client *pikpak.Client, count bool) store.ShareFavorite {
	now := time.Now()
	meta := store.ShareFavoriteMeta{
		FileCount: -1,
		TotalSize: -1,
		StatsAt:   now.UTC().Format(time.RFC3339Nano),
	}
	token, cached := deps.ShareSessions.get(uid, fav.ShareID, now)
	needInfo := fav.OwnerName == "" || fav.ShareCreatedAt == ""
	if !cached || needInfo {
		code := fav.PassCode
		if code == "" {
			// List rows keep the code sealed; this is where it is needed.
			if saved, err := deps.Store.SharePassCode(ctx, uid, fav.ShareID); err == nil {
				code = saved
			}
		}
		info, err := client.GetShare(ctx, fav.ShareID, code)
		if err != nil {
			if !cached {
				if deps.Store.UpdateShareFavoriteMeta(ctx, uid, fav.ID, meta) == nil {
					fav.StatsAt = meta.StatsAt
					fav.StatsReady = true
				}
				return fav
			}
			// The cached token still lets the walk below run.
		} else {
			if !count {
				// Metadata only: the list still owes this share a count, so it
				// must not look checked yet.
				meta.StatsAt = ""
			}
			meta.OwnerName = info.Owner
			meta.OwnerAvatar = info.Avatar
			meta.ShareCreatedAt = info.CreatedAt
			meta.ExpireAt = info.ExpireAt
			token = info.PassCodeToken
			deps.ShareSessions.put(uid, fav.ShareID, token, now)
		}
	}
	if count && token != "" {
		if n, size, err := shareTotals(ctx, client, fav.ShareID, token); err == nil {
			meta.FileCount, meta.TotalSize = n, size
		}
	}
	if err := deps.Store.UpdateShareFavoriteMeta(ctx, uid, fav.ID, meta); err != nil {
		return fav
	}
	fav.OwnerName = orString(meta.OwnerName, fav.OwnerName)
	fav.OwnerAvatar = orString(meta.OwnerAvatar, fav.OwnerAvatar)
	fav.ShareCreatedAt = orString(meta.ShareCreatedAt, fav.ShareCreatedAt)
	if meta.ExpireAt != 0 {
		fav.ExpireAt = meta.ExpireAt
	}
	if meta.FileCount >= 0 {
		fav.FileCount = meta.FileCount
	}
	if meta.TotalSize >= 0 {
		fav.TotalSize = meta.TotalSize
	}
	fav.StatsAt = meta.StatsAt
	fav.StatsReady = true
	return fav
}

// listShareFavoritesWithStats returns the favourites with their metadata,
// counting the ones never counted (or all of them, when force is set).
// stats_pending tells the client how many rows still have no count, so it can
// ask again instead of walking every share in one request.
func listShareFavoritesWithStats(ctx context.Context, deps Deps, uid int, force bool) ([]store.ShareFavorite, int, error) {
	items, err := deps.Store.ListShareFavorites(ctx, uid)
	if err != nil {
		return nil, 0, err
	}
	items = migrateShareFavorites(ctx, deps, uid, items)
	client, err := deps.Sessions.ClientFor(ctx, uid)
	if err != nil {
		return items, 0, nil
	}
	budget := shareStatsPerList
	if force {
		budget = len(items)
	}
	for i := range items {
		if !force && items[i].StatsAt != "" {
			continue
		}
		if budget <= 0 {
			break
		}
		budget--
		shareCtx, cancel := context.WithTimeout(ctx, shareStatsTimeout)
		items[i] = refreshShareFavoriteStats(shareCtx, deps, uid, items[i], client, true)
		cancel()
	}
	pending := 0
	for _, fav := range items {
		if fav.StatsAt == "" {
			pending++
		}
	}
	return items, pending, nil
}

// registerShareFavoriteAvatar proxies the sharing user's avatar. The CDN URL
// stays server-side, like thumbnails; the browser asks for the favourite.
func registerShareFavoriteAvatar(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/share-favorites/{id}/avatar", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		favID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || favID <= 0 {
			writeError(w, http.StatusBadRequest, "bad_ids", "收藏编号不合法")
			return
		}
		fav, err := deps.Store.GetShareFavorite(r.Context(), id.UID, favID)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "favorite_not_found", "收藏不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "读取收藏失败")
			return
		}
		if fav.OwnerAvatar == "" {
			writeError(w, http.StatusNotFound, "no_avatar", "该分享没有头像")
			return
		}
		data, mime, err := fetchImage(r, fav.OwnerAvatar)
		if err != nil {
			writeError(w, http.StatusBadGateway, "avatar_failed", "头像加载失败")
			return
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
}

func orString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
