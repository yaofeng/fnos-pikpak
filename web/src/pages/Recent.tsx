import { useCallback, useEffect, useRef, useState } from 'react'
import { apiGet } from '../api'
import { formatSize, isFolder, type FileItem } from '../files'
import { cacheKeys, readCache, writeCache } from '../listCache'

type Page = { items: FileItem[]; next_page_token: string }
type CachedList = { items: FileItem[]; token: string }

export default function Recent() {
  const [items, setItems] = useState<FileItem[]>([])
  const [token, setToken] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const reqId = useRef(0)
  const itemsRef = useRef<FileItem[]>([])
  const sentinel = useRef<HTMLDivElement | null>(null)

  const fetchPage = useCallback(async (pageToken: string, reset: boolean) => {
    const id = ++reqId.current
    setLoading(true)
    setError('')
    try {
      const params = pageToken ? `recent?page_token=${encodeURIComponent(pageToken)}` : 'recent'
      const page = await apiGet<Page>(params)
      if (id !== reqId.current) return
      const nextItems = reset ? page.items : [...itemsRef.current, ...page.items]
      itemsRef.current = nextItems
      const nextToken = page.next_page_token || ''
      setItems(nextItems)
      setToken(nextToken)
      writeCache<CachedList>(cacheKeys.recent(), { items: nextItems, token: nextToken })
    } catch (e) {
      if (id === reqId.current) setError((e as Error).message)
    } finally {
      if (id === reqId.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    const cached = readCache<CachedList>(cacheKeys.recent())
    if (cached) {
      itemsRef.current = cached.items
      setItems(cached.items)
      setToken(cached.token)
      return
    }
    void fetchPage('', true)
  }, [fetchPage])

  useEffect(() => {
    const el = sentinel.current
    if (!el) return
    const obs = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting && token && !loading) void fetchPage(token, false)
      },
      { rootMargin: '200px' },
    )
    obs.observe(el)
    return () => obs.disconnect()
  }, [token, loading, fetchPage])

  return (
    <section>
      <div className="file-toolbar">
        <p className="muted small">最近添加来自 PikPak 的事件流，包含新增、恢复等操作。</p>
        <div className="file-tools">
          <button type="button" onClick={() => void fetchPage('', true)} disabled={loading}>
            刷新
          </button>
        </div>
      </div>
      {error && <p className="error">{error}</p>}
      {!loading && items.length === 0 && !error && <p className="muted">暂无记录。</p>}
      {items.length > 0 && (
        <table className="file-table">
          <thead>
            <tr>
              <th>名称</th>
              <th className="col-size">大小</th>
              <th className="col-time">修改时间</th>
            </tr>
          </thead>
          <tbody>
            {items.map((f) => (
              <tr key={f.id}>
                <td>
                  <span className="file-icon">{isFolder(f) ? '📁' : '📄'}</span>
                  {f.name}
                </td>
                <td className="col-size muted">{isFolder(f) ? '—' : formatSize(f.size)}</td>
                <td className="col-time muted">
                  {f.modified_time ? new Date(f.modified_time).toLocaleString() : '—'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <div ref={sentinel} className="sentinel" />
      {loading && <p className="muted">加载中…</p>}
    </section>
  )
}
