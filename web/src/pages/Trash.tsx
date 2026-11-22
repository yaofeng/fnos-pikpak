import { useCallback, useEffect, useRef, useState } from 'react'
import { apiGet, apiPost } from '../api'
import FileTable from '../components/FileTable'
import { RefreshIcon, RestoreIcon, TrashIcon } from '../components/icons'
import type { FileItem } from '../files'
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
      <div className="fm-nav">
        <div className="crumb-box">
          <span className="fm-title">回收站</span>
        </div>
        <button
          className="icon-btn"
          type="button"
          onClick={() => void fetchPage('', true)}
          disabled={loading}
          title="刷新"
          aria-label="刷新"
        >
          <RefreshIcon />
        </button>
      </div>

      <div className="fm-actions">
        <button
          className="btn-icon"
          type="button"
          onClick={() => void run('untrash', ids)}
          disabled={busy || ids.length === 0}
        >
          <RestoreIcon />
          恢复
        </button>
        <button
          className="btn-icon"
          type="button"
          onClick={() => void run('delete', ids)}
          disabled={busy || ids.length === 0}
        >
          <TrashIcon />
          永久删除
        </button>
        <span className="fm-spacer" />
        <button type="button" className="danger" onClick={emptyTrash} disabled={busy || items.length === 0}>
          清空回收站
        </button>
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
        <FileTable items={items} selected={selected} selectable onToggle={toggle} />
      )}

      <div ref={sentinel} className="sentinel" />
      {loading && <p className="muted">加载中…</p>}
    </section>
  )
}
