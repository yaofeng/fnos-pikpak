import { useCallback, useEffect, useRef, useState } from 'react'
import { apiGet, apiPost } from '../api'
import { formatSize, isFolder, type FileItem } from '../files'
import { cacheKeys, readCache, writeCache } from '../listCache'

type Page = { items: FileItem[]; next_page_token: string }
type CachedList = { items: FileItem[]; token: string }

export default function Trash() {
  const [items, setItems] = useState<FileItem[]>([])
  const [token, setToken] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  const reqId = useRef(0)
  const itemsRef = useRef<FileItem[]>([])
  const sentinel = useRef<HTMLDivElement | null>(null)

  const fetchPage = useCallback(async (pageToken: string, reset: boolean) => {
    const id = ++reqId.current
    setLoading(true)
    setError('')
    try {
      const params = pageToken ? `trash?page_token=${encodeURIComponent(pageToken)}` : 'trash'
      const page = await apiGet<Page>(params)
      if (id !== reqId.current) return
      const nextItems = reset ? page.items : [...itemsRef.current, ...page.items]
      itemsRef.current = nextItems
      const nextToken = page.next_page_token || ''
      setItems(nextItems)
      setToken(nextToken)
      writeCache<CachedList>(cacheKeys.trash(), { items: nextItems, token: nextToken })
      if (reset) setSelected(new Set())
    } catch (e) {
      if (id === reqId.current) setError((e as Error).message)
    } finally {
      if (id === reqId.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    const cached = readCache<CachedList>(cacheKeys.trash())
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

  const ids = [...selected]

  function toggle(id: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  async function run(action: 'untrash' | 'delete', target: string[]) {
    if (!target.length) return
    if (action === 'delete' && !window.confirm(`永久删除 ${target.length} 项？此操作不可恢复。`)) return
    setBusy(true)
    try {
      const res = await apiPost<{ pending: boolean }>(`files/${action}`, { ids: target })
      setNotice(res.pending ? '任务已提交，稍后自动刷新' : action === 'untrash' ? '已恢复' : '已永久删除')
      await fetchPage('', true)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function emptyTrash() {
    const answer = window.prompt('清空回收站会永久删除全部文件，且不可恢复。输入"确认"继续：')
    if (answer !== '确认') return
    setBusy(true)
    try {
      const res = await apiPost<{ pending: boolean }>('trash/empty', {})
      setNotice(res.pending ? '清空任务已提交' : '回收站已清空')
      await fetchPage('', true)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <section>
      <div className="file-toolbar">
        <span />
        <div className="file-tools">
          <button type="button" onClick={() => void fetchPage('', true)} disabled={loading}>
            刷新
          </button>
          <button type="button" onClick={emptyTrash} disabled={busy || items.length === 0} className="danger">
            清空回收站
          </button>
        </div>
      </div>

      {error && <p className="error">{error}</p>}
      {notice && <p className="ok small">{notice}</p>}

      {ids.length > 0 && (
        <div className="selection-bar">
          <span>已选 {ids.length} 项</span>
          <button type="button" onClick={() => void run('untrash', ids)} disabled={busy}>
            恢复
          </button>
          <button type="button" onClick={() => void run('delete', ids)} disabled={busy}>
            永久删除
          </button>
          <button type="button" onClick={() => setSelected(new Set())}>
            取消选择
          </button>
        </div>
      )}

      {!loading && items.length === 0 && !error && <p className="muted">回收站是空的。</p>}

      {items.length > 0 && (
        <table className="file-table">
          <thead>
            <tr>
              <th className="col-check" />
              <th>名称</th>
              <th className="col-size">大小</th>
              <th className="col-time">删除前修改时间</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {items.map((f) => (
              <tr key={f.id} className={selected.has(f.id) ? 'selected' : ''} onClick={() => toggle(f.id)}>
                <td className="col-check">
                  <input
                    type="checkbox"
                    checked={selected.has(f.id)}
                    onChange={() => toggle(f.id)}
                    onClick={(e) => e.stopPropagation()}
                  />
                </td>
                <td>
                  <span className="file-icon">{isFolder(f) ? '📁' : '📄'}</span>
                  {f.name}
                </td>
                <td className="col-size muted">{isFolder(f) ? '—' : formatSize(f.size)}</td>
                <td className="col-time muted">
                  {f.modified_time ? new Date(f.modified_time).toLocaleString() : '—'}
                </td>
                <td>
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation()
                      void run('untrash', [f.id])
                    }}
                    disabled={busy}
                  >
                    恢复
                  </button>
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
