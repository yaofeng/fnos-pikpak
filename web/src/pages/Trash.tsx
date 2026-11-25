import { useCallback, useEffect, useRef, useState } from 'react'
import StatusBar from '../components/StatusBar'
import { apiGet, apiPost } from '../api'
import { accountLabel } from '../accounts'
import { useAccounts } from '../AccountContext'
import AccountSwitcher from '../components/AccountSwitcher'
import FileTable from '../components/FileTable'
import { RefreshIcon, RestoreIcon, TrashIcon } from '../components/icons'
import { groupByAccount, trashQuery, type FileItem } from '../files'
import { cacheKeys, dropCachePrefix, readCache, resetCache, writeCache } from '../listCache'
import { getLang, makeT } from '../i18n'
import { useFileSelection } from '../useFileSelection'
import { useMarquee } from '../useMarquee'

type AccountError = { account_id: string; account_name?: string; error: string }
type Page = { items: FileItem[]; next_page_token: string; account_errors?: AccountError[] }
type CachedList = { items: FileItem[]; token: string }

// withAccount scopes a request path to one account: the server resolves an
// empty id to the caller's default, so unbound-in-flight pages just omit it.
function withAccount(path: string, accountId: string): string {
  return accountId ? `${path}?account=${encodeURIComponent(accountId)}` : path
}

export default function Trash() {
  const lang = getLang()
  const t = makeT(lang)
  const { accounts, byId, currentId: accountId, scope, setCurrent } = useAccounts()
  // 'all' is the merged view: one trash listing across every account, each
  // row stamped with the account that served it.
  const merged = scope === 'all'
  const [items, setItems] = useState<FileItem[]>([])
  const [token, setToken] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  // Accounts whose page of a merged listing failed; single-account listings
  // leave it empty.
  const [accountErrors, setAccountErrors] = useState<AccountError[]>([])
  const [busy, setBusy] = useState(false)
  const reqId = useRef(0)
  const itemsRef = useRef<FileItem[]>([])
  const sentinel = useRef<HTMLDivElement | null>(null)
  // The scope the current listing came from; the switch effect below uses
  // the delta to own the render in which the scope changed.
  const prevScope = useRef(scope)

  // Same Finder selection as 我的文件: click selects one, Command/Control
  // toggles, Shift takes the range, a drag on the background rubber-bands.
  // Declared before the effects below so they can clear it in their
  // dependency arrays.
  const { ids, selected, select, hitTest, clear: clearSelection } = useFileSelection(items.map((f) => f.id))
  const { box: bodyRef, rect: marqueeRect, onMouseDown: startMarquee } = useMarquee({ onHits: hitTest })

  const fetchPage = useCallback(
    async (pageToken: string, reset: boolean) => {
      const id = ++reqId.current
      setLoading(true)
      setError('')
      try {
        // trashQuery emits the scope's account pair; the pagination token
        // rides in it, keeping the single-account parameter set identical to
        // 2.0.0's.
        const page = await apiGet<Page>(`trash?${trashQuery(scope, pageToken)}`)
        if (id !== reqId.current) return
        const nextItems = reset ? page.items : [...itemsRef.current, ...page.items]
        itemsRef.current = nextItems
        const nextToken = page.next_page_token || ''
        setItems(nextItems)
        setToken(nextToken)
        setAccountErrors(page.account_errors ?? [])
        writeCache<CachedList>(cacheKeys.trash(scope), { items: nextItems, token: nextToken })
        if (reset) clearSelection()
      } catch (e) {
        if (id === reqId.current) setError((e as Error).message)
      } finally {
        if (id === reqId.current) setLoading(false)
      }
    },
    [scope, clearSelection],
  )

  useEffect(() => {
    // The switch effect below owns the render in which the scope changed: it
    // refetches, so the pre-switch scope's request is skipped rather than
    // sent — whose trash everything is, is the whole point of the switch.
    if (prevScope.current !== scope) return
    // Coming back to the page shows the listing it had; 刷新 and the
    // mutation paths replace it. Keys carry the scope, so a cached listing
    // can never be another scope's rows.
    const cached = readCache<CachedList>(cacheKeys.trash(scope))
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

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT')) return
      if (e.key === 'Escape') clearSelection()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [clearSelection])

  // Switching scopes changes whose trash everything is: drop every page's
  // cache, release the selection, and refetch. The reqId guard drops whatever
  // response the pre-switch request lands.
  useEffect(() => {
    if (prevScope.current === scope) return
    prevScope.current = scope
    resetCache()
    itemsRef.current = []
    setItems([])
    setToken('')
    setAccountErrors([])
    clearSelection()
    void fetchPage('', true)
  }, [scope, fetchPage, clearSelection])

  async function run(action: 'untrash' | 'delete', target: string[]) {
    if (!target.length) return
    if (action === 'delete' && !window.confirm(t('trash.confirm_delete').replace('{n}', String(target.length)))) return
    setBusy(true)
    try {
      // A merged selection spans accounts: split the rows by the account that
      // served each one and send that account its own request; rows without a
      // stamp land under the scope itself (single-account view), which is one
      // request exactly like before.
      const wanted = new Set(target)
      const groups = groupByAccount(
        items.filter((f) => wanted.has(f.id)),
        scope,
      )
      let pending = false
      for (const [acct, acctIds] of groups) {
        const res = await apiPost<{ pending: boolean }>(withAccount(`files/${action}`, acct), { ids: acctIds })
        if (res.pending) pending = true
      }
      setNotice(
        pending
          ? t('common.submitted')
          : action === 'untrash'
            ? t('trash.restored')
            : t('files.notice_deleted'),
      )
      // The other scopes' cached listings show the rows we just restored or
      // purged; drop them so returning to those scopes refetches.
      dropCachePrefix('trash:')
      await fetchPage('', true)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function emptyTrash() {
    const answer = window.prompt(t('trash.confirm_empty_prompt'))
    if (answer !== t('trash.confirm_word')) return
    setBusy(true)
    try {
      const res = await apiPost<{ pending: boolean }>(withAccount('trash/empty', accountId), {})
      setNotice(res.pending ? t('trash.empty_submitted') : t('trash.emptied'))
      dropCachePrefix('trash:')
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
        <AccountSwitcher accounts={accounts} scope={scope} onChange={setCurrent} />
        <div className="crumb-box">
          <span className="fm-title">{t('trash.title')}</span>
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

      <div className="fm-actions">
        <button
          className="btn-icon"
          type="button"
          onClick={() => void run('untrash', ids)}
          disabled={busy || ids.length === 0}
        >
          <RestoreIcon />
          {t('trash.restore')}
        </button>
        <button
          className="btn-icon"
          type="button"
          onClick={() => void run('delete', ids)}
          disabled={busy || ids.length === 0}
        >
          <TrashIcon />
          {t('trash.delete_forever')}
        </button>
        <span className="fm-spacer" />
        <button
          type="button"
          className="danger"
          onClick={emptyTrash}
          disabled={merged || busy || items.length === 0}
          title={merged ? t('files.merged_readonly') : undefined}
        >
          {t('trash.empty')}
        </button>
      </div>

      {error && <p className="error">{error}</p>}
      {/* One line per account whose page of a merged listing failed: its rows
          are simply missing, so the failure has to say itself. */}
      {accountErrors.length > 0 && (
        <p className="muted small">
          {t('files.account_errors')}
          {accountErrors
            .map((e) =>
              t('recent.account_error_line')
                .replace('{name}', accountLabel(byId, e.account_id, e.account_name, lang))
                .replace('{error}', e.error),
            )
            .join(lang === 'en' ? '; ' : '；')}
        </p>
      )}

      {!loading && items.length === 0 && !error && <p className="muted">{t('trash.empty_state')}</p>}

      {items.length > 0 && (
        <div className="file-body" ref={bodyRef} onMouseDown={startMarquee}>
          <FileTable items={items} selected={selected} onSelect={select} />
          {marqueeRect && (
            <div
              className="marquee"
              style={{ left: marqueeRect.left, top: marqueeRect.top, width: marqueeRect.width, height: marqueeRect.height }}
            />
          )}
        </div>
      )}

      <StatusBar selected={ids.length} total={items.length} notice={notice} />

      <div ref={sentinel} className="sentinel" />
      {loading && <p className="muted">{t('common.loading')}</p>}
    </section>
  )
}
