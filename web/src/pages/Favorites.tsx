import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { apiDelete, apiGet, apiPatch, apiPost } from '../api'
import ShareBrowser from '../components/ShareBrowser'
import { BookmarkIcon, RefreshIcon } from '../components/icons'

// A saved share, as the list route returns it. The pass code never comes back
// from the server; opening a favourite makes the server read the sealed one.
type ShareFavorite = {
  id: number
  share_id: string
  title: string
  display_title: string
  protected: boolean
  created_at: string
}

export default function Favorites() {
  const [items, setItems] = useState<ShareFavorite[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [openFav, setOpenFav] = useState<ShareFavorite | null>(null)
  const [busy, setBusy] = useState(0)
  const [adding, setAdding] = useState(false)
  const [newURL, setNewURL] = useState('')
  const [addError, setAddError] = useState('')
  const [saving, setSaving] = useState(false)

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

  async function remove(fav: ShareFavorite) {
    if (!window.confirm(`从收藏夹移除「${fav.title || fav.share_id}」？分享本身不会受影响。`)) return
    setBusy(fav.id)
    try {
      await apiDelete(`share-favorites/${fav.id}`)
      setItems((prev) => (prev ?? []).filter((f) => f.id !== fav.id))
      if (openFav?.id === fav.id) setOpenFav(null)
    } finally {
      setBusy(0)
    }
  }

  // 新增收藏 takes the link a person copied from PikPak; the server parses the
  // share id and picks up a ?pwd= code from it.
  async function addShare() {
    const url = newURL.trim()
    if (!url) return
    setSaving(true)
    setAddError('')
    try {
      await apiPost('share-favorites', { url })
      setNewURL('')
      setAdding(false)
      await load()
    } catch (err) {
      setAddError((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  async function rename(fav: ShareFavorite) {
    const next = window.prompt('新名称（留空恢复原名）', fav.display_title || fav.title || fav.share_id)
    if (next === null) return
    try {
      const updated = await apiPatch<ShareFavorite>(`share-favorites/${fav.id}`, { title: next.trim() })
      setItems((prev) => (prev ?? []).map((f) => (f.id === fav.id ? { ...f, display_title: updated.display_title } : f)))
    } catch (err) {
      setError((err as Error).message)
    }
  }

  if (openFav !== null) {
    return (
      <section className="page-stack">
        <ShareBrowser
          key={openFav.id}
          favoriteId={openFav.id}
          initialTitle={openFav.title}
          onHome={() => setOpenFav(null)}
        />
      </section>
    )
  }

  return (
    <section className="page-stack">
      <div className="actions">
        <button type="button" disabled={loading} onClick={() => void load()}>
          <RefreshIcon /> 刷新
        </button>
        <Link to="/search" className="button">
          去全网搜索
        </Link>
        <button type="button" onClick={() => { setAdding((v) => !v); setAddError('') }}>
          <BookmarkIcon /> 新增收藏
        </button>
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

      {error && <p className="error">{error}</p>}

      {items === null || (loading && items.length === 0) ? (
        <p className="muted">加载中…</p>
      ) : items.length === 0 ? (
        <div className="empty-state">
          <BookmarkIcon size={28} />
          <p>收藏夹还是空的。</p>
          <p className="muted small">
            点「新增收藏」粘贴分享链接，或者在全网搜索里右键标签页选「收藏分享」，之后可以在这里直接进入。
          </p>
        </div>
      ) : (
        <table className="table fav-table">
          <thead>
            <tr>
              <th>名称</th>
              <th>分享编号</th>
              <th className="col-time">收藏时间</th>
              <th className="fav-actions">操作</th>
            </tr>
          </thead>
          <tbody>
            {items.map((fav) => (
              <tr key={fav.id} onDoubleClick={() => setOpenFav(fav)}>
                <td>
                  <span className="shine-name">
                    <BookmarkIcon size={16} />
                    <button
                      type="button"
                      className="link"
                      onClick={() => setOpenFav(fav)}
                    >
                      {fav.display_title || fav.title || fav.share_id}
                    </button>
                  </span>
                  {fav.protected && <span className="badge-lock">有密码</span>}
                </td>
                <td className="muted small">{fav.share_id}</td>
                <td className="col-time muted small">
                  {fav.created_at ? new Date(fav.created_at).toLocaleString() : '—'}
                </td>
                <td className="fav-actions">
                  <button
                    type="button"
                    onClick={() => setOpenFav(fav)}
                  >
                    打开
                  </button>
                  <button type="button" onClick={() => void rename(fav)}>
                    重命名
                  </button>
                  <button type="button" className="danger" disabled={busy === fav.id} onClick={() => void remove(fav)}>
                    移除
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}
