import { useCallback, useEffect, useState, type MouseEvent } from 'react'
import StatusBar from '../components/StatusBar'
import { Link } from 'react-router-dom'
import { dateText } from '../dates'
import { apiDelete, apiGet, apiPatch, apiPost, basePath } from '../api'
import ShareBrowser from '../components/ShareBrowser'
import {
  BookmarkIcon,
  GlobeIcon,
  RefreshIcon,
  SearchIcon,
  SortIcon,
  TrashIcon,
} from '../components/icons'
import {
  favoriteCountText,
  favoriteSizeText,
  favoriteStatsHint,
  sortShareFavorites,
  type FavoriteSortDir,
  type FavoriteSortKey,
} from '../shares'
import { useFileSelection } from '../useFileSelection'

// A saved share, as the list route returns it. The pass code never comes back
// from the server; opening a favourite makes the server read the sealed one.
// The owner and the timestamps come from PikPak when the share was added or
// first visited; file_count/total_size are the recursive totals, counted in
// the background when the share is entered and cached for a day, with -1
// meaning "not counted or not countable".
type ShareFavorite = {
  id: number
  share_id: string
  title: string
  display_title: string
  protected: boolean
  created_at: string
  owner_name: string
  owner_avatar: string
  share_created_at: string
  expire_at: number
  file_count: number
  total_size: number
  stats_checked: boolean
}

function nameOf(fav: ShareFavorite): string {
  return fav.display_title || fav.title || fav.share_id
}

function expireText(expireAt: number): string {
  // -1 is PikPak's "never expires"; 0 means the response did not say.
  if (expireAt === -1) return '永久'
  if (expireAt > 0) return new Date(expireAt * 1000).toLocaleString()
  return '—'
}

export default function Favorites() {
  const [items, setItems] = useState<ShareFavorite[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [openFav, setOpenFav] = useState<ShareFavorite | null>(null)
  const [adding, setAdding] = useState(false)
  const [newURL, setNewURL] = useState('')
  const [addError, setAddError] = useState('')
  const [saving, setSaving] = useState(false)
  const [q, setQ] = useState('')
  const [sortKey, setSortKey] = useState<FavoriteSortKey>('default')
  const [sortDir, setSortDir] = useState<FavoriteSortDir>('asc')
  const [busy, setBusy] = useState(false)
  // Bumped when a share closes: the server started counting it as it was
  // opened, so the list takes a few cached reads before settling.
  const [visitReload, setVisitReload] = useState(0)

  // The list only reads the totals the server has cached. A share counted
  // for the first time — or recounted a day later — is visited from here, so
  // the next look shows its numbers.
  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const body = await apiGet<{ items: ShareFavorite[] }>('share-favorites')
      setItems(body.items ?? [])
    } catch (err) {
      setError((err as Error).message)
      setItems([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  // Coming back from a share, its count may still be running in the
  // background. Re-read the cache a few times instead of leaving the row
  // blank until the next manual refresh; every read is cache-only.
  useEffect(() => {
    if (!visitReload) return
    let cancelled = false
    const tick = async (attempt: number) => {
      await new Promise((resolve) => setTimeout(resolve, 2000))
      if (cancelled || attempt > 3) return
      try {
        const body = await apiGet<{ items: ShareFavorite[] }>('share-favorites')
        if (cancelled) return
        const list = body.items ?? []
        setItems(list)
        if (list.some((fav) => !fav.stats_checked)) void tick(attempt + 1)
      } catch {
        // Keep the list as it stands.
      }
    }
    void tick(1)
    return () => {
      cancelled = true
    }
  }, [visitReload])

  const shown = sortShareFavorites(
    (items ?? []).filter((fav) => {
      if (!q.trim()) return true
      const needle = q.trim().toLowerCase()
      return (
        nameOf(fav).toLowerCase().includes(needle) ||
        (fav.owner_name || '').toLowerCase().includes(needle) ||
        fav.share_id.toLowerCase().includes(needle)
      )
    }),
    sortKey,
    sortDir,
  )
  const order = shown.map((fav) => String(fav.id))
  const { selected, select, clear } = useFileSelection(order)
  const picked = shown.filter((fav) => selected.has(String(fav.id)))
  const one = picked.length === 1 ? picked[0] : null

  async function addShare() {
    const url = newURL.trim()
    if (!url) return
    setSaving(true)
    setAddError('')
    try {
      await apiPost('share-favorites', { url })
      setNewURL('')
      setAdding(false)
      setNotice('已加入分享收藏夹')
      await load()
    } catch (err) {
      setAddError((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  // Renaming is local only: it changes how this list shows the share, never
  // the share itself. An empty prompt restores the share's own title.
  async function rename(fav: ShareFavorite) {
    const next = window.prompt('新名称（留空恢复原名）', nameOf(fav))
    if (next === null) return
    setBusy(true)
    setError('')
    try {
      const updated = await apiPatch<ShareFavorite>(`share-favorites/${fav.id}`, { title: next.trim() })
      setItems((prev) => (prev ?? []).map((f) => (f.id === fav.id ? { ...f, display_title: updated.display_title } : f)))
      setNotice(next.trim() ? '已重命名（仅本机显示）' : '已恢复原名')
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function remove(list: ShareFavorite[]) {
    if (list.length === 0) return
    const label = list.length === 1 ? `「${nameOf(list[0])}」` : `${list.length} 个分享`
    if (!window.confirm(`从分享收藏夹移除${label}？分享本身不会受影响。`)) return
    setBusy(true)
    setError('')
    try {
      for (const fav of list) await apiDelete(`share-favorites/${fav.id}`)
      setItems((prev) => (prev ?? []).filter((f) => !list.some((gone) => gone.id === f.id)))
      clear()
      setNotice(`已移除${label}`)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  // Double click opens the share itself; the star beside the address bar in
  // that view adds or removes the favourite.
  function open(fav: ShareFavorite) {
    setOpenFav(fav)
  }

  function refresh() {
    setNotice('')
    void load()
  }

  if (openFav !== null) {
    // The share opens as the page itself, laid out exactly like 我的文件; the
    // list reloads when it closes, since the star can add or remove favourites.
    return (
      <ShareBrowser
        key={openFav.id}
        favoriteId={openFav.id}
        initialTitle={nameOf(openFav)}
        onHome={() => {
          setOpenFav(null)
          void load()
          setVisitReload((n) => n + 1)
        }}
      />
    )
  }

  return (
    <section className="file-page">
      <div className="fm-nav">
        <button
          className="icon-btn"
          type="button"
          onClick={refresh}
          disabled={loading}
          title="刷新"
          aria-label="刷新"
        >
          <RefreshIcon />
        </button>
        <div className="crumb-box">
          <span className="fm-title">分享收藏夹</span>
        </div>
        <label className="fm-search">
          <SearchIcon />
          <input
            type="search"
            placeholder="筛选分享名称或创建者"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </label>
      </div>

      <div className="fm-actions">
        <button className="btn-icon" type="button" onClick={() => { setAdding((v) => !v); setAddError('') }}>
          <BookmarkIcon />
          新增收藏
        </button>
        <Link to="/search" className="btn-icon">
          <GlobeIcon />
          去全网搜索
        </Link>
        <button
          className="btn-icon"
          type="button"
          disabled={!one || busy}
          onClick={() => one && void rename(one)}
        >
          重命名
        </button>
        <button
          className="btn-icon"
          type="button"
          disabled={picked.length === 0 || busy}
          onClick={() => void remove(picked)}
        >
          <TrashIcon />
          删除
        </button>

        <span className="fm-spacer" />

        <label className="fm-pick">
          <SortIcon />
          <select
            value={`${sortKey}:${sortDir}`}
            onChange={(e) => {
              const [k, d] = e.target.value.split(':')
              setSortKey(k as FavoriteSortKey)
              setSortDir(d as FavoriteSortDir)
            }}
            aria-label="排序"
          >
            <option value="default:asc">默认（收藏时间）</option>
            <option value="name:asc">分享名称 ↑</option>
            <option value="name:desc">分享名称 ↓</option>
            <option value="owner:asc">创建者 ↑</option>
            <option value="owner:desc">创建者 ↓</option>
            <option value="count:desc">总文件数 ↓</option>
            <option value="count:asc">总文件数 ↑</option>
            <option value="size:desc">总大小 ↓</option>
            <option value="size:asc">总大小 ↑</option>
            <option value="shared:desc">分享时间 ↓</option>
            <option value="shared:asc">分享时间 ↑</option>
            <option value="expire:asc">过期时间 ↑</option>
            <option value="expire:desc">过期时间 ↓</option>
          </select>
        </label>
      </div>

      {adding && (
        <form
          className="fav-add"
          onSubmit={(e) => {
            e.preventDefault()
            void addShare()
          }}
        >
          <input
            value={newURL}
            onChange={(e) => setNewURL(e.target.value)}
            placeholder="粘贴分享链接，例如 https://mypikpak.com/s/xxxx?pwd=1234"
            aria-label="分享链接"
          />
          <button type="submit" className="primary" disabled={!newURL.trim() || saving}>
            {saving ? '保存中…' : '保存'}
          </button>
          <button type="button" onClick={() => { setAdding(false); setNewURL(''); setAddError('') }}>
            取消
          </button>
          {addError && <span className="error small">{addError}</span>}
        </form>
      )}

      <div className="file-body">
        {items === null || (loading && (items ?? []).length === 0) ? (
          <p className="muted">加载中…</p>
        ) : (items ?? []).length === 0 ? (
          <div className="empty-state">
            <BookmarkIcon size={28} />
            <p>分享收藏夹还是空的。</p>
            <p className="muted small">
              点「新增收藏」粘贴分享链接，或者在全网搜索里右键标签页选「收藏分享」，之后可以在这里直接进入。
            </p>
          </div>
        ) : shown.length === 0 ? (
          <p className="muted">没有匹配的分享。</p>
        ) : (
          <table className="file-table fav-table">
            <thead>
              <tr>
                <th className="col-open" />
                <th>分享名称</th>
                <th>创建者</th>
                <th className="col-size">总文件数</th>
                <th className="col-size">总大小</th>
                <th className="col-time">分享时间</th>
                <th className="col-time">过期时间</th>
              </tr>
            </thead>
            <tbody>
              {shown.map((fav, index) => (
                <tr
                  key={fav.id}
                  className={selected.has(String(fav.id)) ? 'selected' : ''}
                  onClick={(e: MouseEvent) => select(index, e)}
                  onDoubleClick={() => open(fav)}
                >
                  <td className="col-open" />
                  <td>
                    <span className="shine-name">
                      <BookmarkIcon size={16} />
                      <span title={nameOf(fav)}>{nameOf(fav)}</span>
                    </span>
                    {fav.protected && <span className="badge-lock">有密码</span>}
                  </td>
                  <td>
                    <span className="fav-owner">
                      {fav.owner_avatar ? (
                        <img
                          className="fav-avatar"
                          src={`${basePath}/api/share-favorites/${fav.id}/avatar`}
                          alt=""
                          loading="lazy"
                          onError={(e) => {
                            e.currentTarget.style.visibility = 'hidden'
                          }}
                        />
                      ) : (
                        <span className="fav-avatar blank" aria-hidden="true" />
                      )}
                      <span>{fav.owner_name || '—'}</span>
                    </span>
                  </td>
                  <td className="col-size muted small" title={favoriteStatsHint(fav) || undefined}>
                    {favoriteCountText(fav)}
                  </td>
                  <td className="col-size muted small" title={favoriteStatsHint(fav) || undefined}>
                    {favoriteSizeText(fav)}
                  </td>
                  <td className="col-time muted small">{dateText(fav.share_created_at)}</td>
                  <td className="col-time muted small">{expireText(fav.expire_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <StatusBar
        selected={picked.length}
        total={(items ?? []).length}
        notice={notice}
        error={error}
      />
    </section>
  )
}
