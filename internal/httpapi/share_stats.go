package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/store"
)

const (
	// shareStatsMaxPages bounds one share's walk: a share that needs more
	// pages than this is reported as uncountable rather than walking forever.
	shareStatsMaxPages = 400
	// shareStatsJobTimeout is how long a background count may take. It runs
	// off the request path, so it can outlast the 12s the list used to allow.
	shareStatsJobTimeout = 2 * time.Minute
	// shareStatsStaleAfter is how long a remembered total is trusted; a visit
	// after that counts the share again.
	shareStatsStaleAfter = 24 * time.Hour
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

// refreshShareFavoriteStats opens a share and records who shared it, when,
// whether it expires, and (when count is set) the totals a recursive walk
// finds. Only a successful walk writes stats_at: a failed count leaves the
// cache empty, so the next visit tries again instead of showing a dash for a
// day. Metadata is kept even when the walk fails.
func refreshShareFavoriteStats(ctx context.Context, deps Deps, uid int, fav store.ShareFavorite, client *pikpak.Client, count bool) store.ShareFavorite {
	now := time.Now()
	meta := store.ShareFavoriteMeta{FileCount: -1, TotalSize: -1}
	// The walk runs as the default account, so its pass-code session lives
	// under the default account's id — the key the share browser writes.
	acctID := defaultAccountID(ctx, deps, uid)
	token, _, cached := deps.ShareSessions.get(uid, acctID, fav.ShareID, now)
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
				// Nothing learned and no token to walk with.
				return fav
			}
			// The cached token still lets the walk below run.
		} else {
			meta.OwnerName = info.Owner
			meta.OwnerAvatar = info.Avatar
			meta.ShareCreatedAt = info.CreatedAt
			meta.ExpireAt = info.ExpireAt
			token = info.PassCodeToken
			deps.ShareSessions.put(uid, acctID, fav.ShareID, token, code, now)
		}
	}
	// A share without a pass code answers with no token, and its folders are
	// still counted: ListShareFiles accepts an empty pass_code_token.
	if count {
		if n, size, err := shareTotals(ctx, client, fav.ShareID, token); err == nil {
			meta.FileCount, meta.TotalSize = n, size
			meta.StatsAt = now.UTC().Format(time.RFC3339Nano)
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
	if meta.StatsAt != "" {
		fav.StatsAt = meta.StatsAt
		fav.StatsReady = true
	}
	return fav
}

// runningShareStats holds the "uid/shareID" keys of counts in flight, so a
// share opened twice in one gesture is walked once.
var runningShareStats sync.Map

// shareStatsStale reports whether a remembered count should be refreshed: a
// share never counted, an unreadable timestamp, or one older than a day.
func shareStatsStale(statsAt string, now time.Time) bool {
	if statsAt == "" {
		return true
	}
	at, err := time.Parse(time.RFC3339Nano, statsAt)
	if err != nil {
		return true
	}
	return now.Sub(at) > shareStatsStaleAfter
}

// startShareStats counts a saved share in the background when its cached
// total is missing, stale, or the last attempt failed. 分享收藏夹 itself never
// counts — the walk happens when the user enters the share, and the next
// visit to the list reads it.
func startShareStats(deps Deps, uid int, fav store.ShareFavorite) {
	if fav.FileCount >= 0 && !shareStatsStale(fav.StatsAt, time.Now()) {
		return
	}
	key := strconv.Itoa(uid) + "/" + fav.ShareID
	if _, busy := runningShareStats.LoadOrStore(key, struct{}{}); busy {
		return
	}
	go func() {
		defer runningShareStats.Delete(key)
		ctx, cancel := context.WithTimeout(context.Background(), shareStatsJobTimeout)
		defer cancel()
		client, err := deps.Sessions.ClientFor(ctx, uid)
		if err != nil {
			return
		}
		refreshShareFavoriteStats(ctx, deps, uid, fav, client, true)
	}()
}

// registerShareFavoriteAvatar proxies the sharing user's avatar. The CDN URL
// stays server-side, like thumbnails; the browser asks for the favourite.
func registerShareFavoriteAvatar(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/share-favorites/{id}/avatar", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		favID, ok := pathID(w, r, "id")
		if !ok {
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
