import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { apiDelete, apiGet } from '../api'
import ShareBrowser from '../components/ShareBrowser'
import { BookmarkIcon, RefreshIcon } from '../components/icons'

// A saved share, as the list route returns it. The pass code never comes back
// from the server; opening a favourite makes the server read the sealed one.
type ShareFavorite = {
  id: number
  share_id: string
  title: string
  protected: boolean
  created_at: string
}

export default function Favorites() {
  const [items, setItems] = useState<ShareFavorite[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [openFav, setOpenFav] = useState<ShareFavorite | null>(null)
  const [busy, setBusy] = useState(0)

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
      </div>

      {error && <p className="error">{error}</p>}

      {items === null || (loading && items.length === 0) ? (
        <p className="muted">加载中…</p>
      ) : items.length === 0 ? (
        <div className="empty-state">
          <BookmarkIcon size={28} />
          <p>收藏夹还是空的。</p>
          <p className="muted small">
            在全网搜索里右键标签页选「加入收藏」，或者打开分享后点「加入收藏」，之后可以在这里直接进入。
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
                      {fav.title || fav.share_id}
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
