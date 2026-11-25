import { useCallback, useEffect, useRef, useState } from 'react'
import { apiGet } from '../api'
import { accountLabel } from '../accounts'
import { useAccounts } from '../AccountContext'
import FileTable from '../components/FileTable'
import { RefreshIcon } from '../components/icons'
import { cacheKeys, readCache, writeCache } from '../listCache'
import type { FileItem } from '../files'

type AccountError = { account_id: string; account_name?: string; error: string }
type Page = { items: FileItem[]; next_page_token?: string; account_errors?: AccountError[] }
type CachedList = { items: FileItem[]; token: string }

export default function Recent() {
  const { byId } = useAccounts()
  const [items, setItems] = useState<FileItem[]>([])
  const [token, setToken] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [accountErrors, setAccountErrors] = useState<AccountError[]>([])
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
      setAccountErrors(page.account_errors ?? [])
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

  const accountOf = useCallback((f: FileItem) => accountLabel(byId, f.account_id), [byId])

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
      {/* One line per account whose page could not load: its rows are simply
          missing, so the failure has to say itself. */}
      {accountErrors.length > 0 && (
        <p className="muted small">
          部分账号的最近记录没有加载：
          {accountErrors
            .map((e) => `账号 ${accountLabel(byId, e.account_id, e.account_name)}：${e.error}`)
            .join('；')}
        </p>
      )}
      {!loading && items.length === 0 && !error && <p className="muted">暂无记录。</p>}
      {items.length > 0 && <FileTable items={items} accountOf={accountOf} />}
      <div ref={sentinel} className="sentinel" />
      {loading && <p className="muted">加载中…</p>}
    </section>
  )
}
