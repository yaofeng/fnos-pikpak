import { useCallback, useEffect, useRef, useState, type MouseEvent } from 'react'
import { Link } from 'react-router-dom'
import { apiDelete, apiGet, apiPatch, apiPost, basePath } from '../api'
import ShareBrowser from '../components/ShareBrowser'
import {
  BookmarkIcon,
  GlobeIcon,
  RefreshIcon,
  SearchIcon,
  TrashIcon,
} from '../components/icons'
import { formatSize } from '../files'
import { useFileSelection } from '../useFileSelection'

// A saved share, as the list route returns it. The pass code never comes back
// from the server; opening a favourite makes the server read the sealed one.
// The owner and the timestamps come from PikPak when the share was added or
// counted; file_count/total_size are the recursive totals, computed server-side
// and remembered, with -1 meaning "not countable".
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

function dateText(value: string): string {
  if (!value) return '—'
  const at = new Date(value)
  return Number.isNaN(at.getTime()) ? value : at.toLocaleString()
}

function countText(fav: ShareFavorite): string {
  if (fav.file_count >= 0) return fav.file_count.toLocaleString()
  return fav.stats_checked ? '—' : '统计中…'
}

function sizeText(fav: ShareFavorite): string {
  if (fav.total_size > 0) return formatSize(fav.total_size)
  if (!fav.stats_checked) return '统计中…'
  // A counted share with nothing in it is 0 B; an uncountable one is unknown.
  return fav.file_count === 0 ? '0 B' : '—'
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
  const [busy, setBusy] = useState(false)

  // The server counts a few uncounted shares per list call; while any remain,
  // the page asks again instead of leaving half the column as 统计中….
  const loadRef = useRef<(refresh?: boolean) => Promise<void>>(async () => undefined)
  const pollTimer = useRef<number | undefined>(undefined)
  const pendingPolls = useRef(0)

  const load = useCallback(async (refresh = false) => {
    window.clearTimeout(pollTimer.current)
    setLoading(true)
    setError('')
    try {
      const body = await apiGet<{ items: ShareFavorite[]; stats_pending?: number }>(
        refresh ? 'share-favorites?refresh=1' : 'share-favorites',
      )
      setItems(body.items ?? [])
      const pending = body.stats_pending ?? 0
      if (pending > 0 && pendingPolls.current < 5) {
        pendingPolls.current++
        pollTimer.current = window.setTimeout(() => void loadRef.current(false), 1500)
      } else if (pending === 0) {
        pendingPolls.current = 0
      }
    } catch (err) {
      setError((err as Error).message)
      setItems([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadRef.current = load
  }, [load])

  useEffect(() => {
    void load()
    return () => window.clearTimeout(pollTimer.current)
  }, [load])

  const shown = (items ?? []).filter((fav) => {
    if (!q.trim()) return true
    const needle = q.trim().toLowerCase()
    return (
      nameOf(fav).toLowerCase().includes(needle) ||
      (fav.owner_name || '').toLowerCase().includes(needle) ||
      fav.share_id.toLowerCase().includes(needle)
    )
  })
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
      pendingPolls.current = 0
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
    pendingPolls.current = 0
    setNotice('')
    void load(true)
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
          pendingPolls.current = 0
          void load()
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
          title="刷新并重新统计"
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
                  <td className="col-size muted small">{countText(fav)}</td>
                  <td className="col-size muted small">{sizeText(fav)}</td>
                  <td className="col-time muted small">{dateText(fav.share_created_at)}</td>
                  <td className="col-time muted small">{expireText(fav.expire_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div className="status-bar">
        <span>
          {picked.length > 0
            ? `选中 ${picked.length} 项（共 ${(items ?? []).length} 项）`
            : `共 ${(items ?? []).length} 项`}
        </span>
        {notice && <span className="ok status-note">{notice}</span>}
        {error && <span className="error status-note">{error}</span>}
      </div>
    </section>
  )
}
