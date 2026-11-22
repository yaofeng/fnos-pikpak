import { useCallback, useEffect, useRef, useState } from 'react'
import { apiGet } from '../api'
import FileTable from '../components/FileTable'
import { RefreshIcon } from '../components/icons'
import { cacheKeys, readCache, writeCache } from '../listCache'
import type { FileItem } from '../files'

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
      <div className="fm-nav">
        <div className="crumb-box">
          <span className="fm-title">最近</span>
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
      <p className="muted small">最近添加来自 PikPak 的事件流，包含新增、恢复等操作。</p>

      {error && <p className="error">{error}</p>}
      {!loading && items.length === 0 && !error && <p className="muted">暂无记录。</p>}
      {items.length > 0 && <FileTable items={items} />}
      <div ref={sentinel} className="sentinel" />
      {loading && <p className="muted">加载中…</p>}
    </section>
  )
}
