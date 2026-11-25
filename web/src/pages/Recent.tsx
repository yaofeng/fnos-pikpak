import { useCallback, useEffect, useRef, useState } from 'react'
import { apiGet } from '../api'
import { accountLabel } from '../accounts'
import { useAccounts } from '../AccountContext'
import AccountSwitcher from '../components/AccountSwitcher'
import FileTable from '../components/FileTable'
import { RefreshIcon } from '../components/icons'
import { cacheKeys, readCache, resetCache, writeCache } from '../listCache'
import { getLang, makeT } from '../i18n'
import type { FileItem } from '../files'

type AccountError = { account_id: string; account_name?: string; error: string }
type Page = { items: FileItem[]; next_page_token?: string; account_errors?: AccountError[] }
type CachedList = { items: FileItem[]; token: string }

export default function Recent() {
  const lang = getLang()
  const t = makeT(lang)
  const { accounts, byId, scope, setCurrent } = useAccounts()
  const [items, setItems] = useState<FileItem[]>([])
  const [token, setToken] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [accountErrors, setAccountErrors] = useState<AccountError[]>([])
  const reqId = useRef(0)
  const itemsRef = useRef<FileItem[]>([])
  const sentinel = useRef<HTMLDivElement | null>(null)
  // The scope the current listing came from; the switch effect below uses
  // the delta to own the render in which the scope changed.
  const prevScope = useRef(scope)

  const fetchPage = useCallback(
    async (pageToken: string, reset: boolean) => {
      const id = ++reqId.current
      setLoading(true)
      setError('')
      try {
        // A concrete scope pins the listing to that account; 'all' keeps the
        // bare path, so the merged request stays parameter-free like 2.0's.
        const base = scope === 'all' ? 'recent' : `recent?account=${encodeURIComponent(scope)}`
        const params = pageToken ? `${base}&page_token=${encodeURIComponent(pageToken)}` : base
        const page = await apiGet<Page>(params)
        if (id !== reqId.current) return
        const nextItems = reset ? page.items : [...itemsRef.current, ...page.items]
        itemsRef.current = nextItems
        const nextToken = page.next_page_token || ''
        setItems(nextItems)
        setToken(nextToken)
        setAccountErrors(page.account_errors ?? [])
        writeCache<CachedList>(cacheKeys.recent(scope), { items: nextItems, token: nextToken })
      } catch (e) {
        if (id === reqId.current) setError((e as Error).message)
      } finally {
        if (id === reqId.current) setLoading(false)
      }
    },
    [scope],
  )

  useEffect(() => {
    // The switch effect below owns the render in which the scope changed: it
    // refetches, so the pre-switch scope's request is skipped rather than sent.
    if (prevScope.current !== scope) return
    // Coming back to the page shows the listing it had; 刷新 replaces it.
    // Keys carry the scope, so a cached listing can never be another scope's
    // rows.
    const cached = readCache<CachedList>(cacheKeys.recent(scope))
    if (cached) {
      itemsRef.current = cached.items
      setItems(cached.items)
      setToken(cached.token)
      return
    }
    void fetchPage('', true)
  }, [fetchPage, scope])

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

  // Switching scopes changes whose recent list everything is: drop the page
  // caches, clear what is on screen, and refetch. The reqId guard drops
  // whatever response the pre-switch request lands.
  useEffect(() => {
    if (prevScope.current === scope) return
    prevScope.current = scope
    resetCache()
    itemsRef.current = []
    setItems([])
    setToken('')
    setAccountErrors([])
    void fetchPage('', true)
  }, [scope, fetchPage])

  const accountOf = (f: FileItem) => accountLabel(byId, f.account_id, undefined, lang)

  return (
    <section>
      <div className="fm-nav">
        <AccountSwitcher accounts={accounts} scope={scope} onChange={setCurrent} />
        <div className="crumb-box">
          <span className="fm-title">{t('recent.title')}</span>
        </div>
        <button
          className="icon-btn"
          type="button"
          onClick={() => void fetchPage('', true)}
          disabled={loading}
          title={t('common.refresh')}
          aria-label={t('common.refresh')}
        >
          <RefreshIcon />
        </button>
      </div>
      <p className="muted small">{t('recent.hint')}</p>

      {error && <p className="error">{error}</p>}
      {/* One line per account whose page could not load: its rows are simply
          missing, so the failure has to say itself. */}
      {accountErrors.length > 0 && (
        <p className="muted small">
          {t('recent.account_errors')}
          {accountErrors
            .map((e) =>
              t('recent.account_error_line')
                .replace('{name}', accountLabel(byId, e.account_id, e.account_name, lang))
                .replace('{error}', e.error),
            )
            .join(lang === 'en' ? '; ' : '；')}
        </p>
      )}
      {!loading && items.length === 0 && !error && <p className="muted">{t('common.empty')}</p>}
      {items.length > 0 && <FileTable items={items} accountOf={accountOf} />}
      <div ref={sentinel} className="sentinel" />
      {loading && <p className="muted">{t('common.loading')}</p>}
    </section>
  )
}
