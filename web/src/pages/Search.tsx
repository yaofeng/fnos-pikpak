import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { apiGet, apiPost, ApiError } from '../api'
import { copyText } from '../clipboard'
import ShareBrowser, { type ShareState } from '../components/ShareBrowser'
import { GlobeIcon, SearchIcon, ShareIcon } from '../components/icons'
import {
  appendResults,
  crumbFromURL,
  hasMorePages,
  shareCount,
  shareURL,
  type WebResult,
  type WebSearchPage,
} from '../search'
import { afterClose, openTab, type WebTab } from '../webTabs'

// The provider serves ten pages, each one a paid query.
const maxPage = 10

// Search is 全网搜索: a search-engine query limited to the PikPak site. The
// person types only the keyword — the server adds the site: filter. A result
// opens in an in-app tab (its page is rendered on mypikpak.com), and a
// right-click on a tab can copy its address. Results append as the list is
// scrolled: there is no pager, and a short page keeps loading until the
// window is filled or the keyword runs out.
export default function Search() {
  const [q, setQ] = useState('')
  const [items, setItems] = useState<WebResult[] | null>(null)
  const [query, setQuery] = useState('')
  const [page, setPage] = useState(1)
  const [hasMore, setHasMore] = useState(false)
  // What the provider returned for the first page and how many of those were
  // share pages: shown when most of a page was dropped for not being a share.
  const [firstRaw, setFirstRaw] = useState(0)
  const [firstKept, setFirstKept] = useState(0)
  // 'share' lists only pages the app can open; 'all' also lists the site's
  // other pages, and the share pages are marked with an icon.
  const [scope, setScope] = useState<'share' | 'all'>('share')
  const [firstShares, setFirstShares] = useState(0)
  const [loading, setLoading] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  // The wrapped result whose destination is being asked for right now.
  const [resolving, setResolving] = useState('')
  const [error, setError] = useState('')
  const [needKey, setNeedKey] = useState(false)

  const [tabs, setTabs] = useState<WebTab[]>([])
  const [activeId, setActiveId] = useState<number | null>(null)
  const [tabMenu, setTabMenu] = useState<{ x: number; y: number; tab: WebTab } | null>(null)
  const [copied, setCopied] = useState(false)
  const [tabNote, setTabNote] = useState('')
  // What each share tab learned while it was open: the title it settled on and
  // the pass code that worked, so 加入收藏 can save the same thing.
  const [tabStates, setTabStates] = useState<Record<number, ShareState>>({})
  const nextTabId = useRef(1)
  const active = tabs.find((t) => t.id === activeId) ?? null

  // A context menu closes on the next click anywhere, like the file menu.
  useEffect(() => {
    if (!tabMenu) return
    const close = () => setTabMenu(null)
    window.addEventListener('click', close)
    return () => window.removeEventListener('click', close)
  }, [tabMenu])

  // "已复制地址" is a receipt, not a state: it clears itself.
  useEffect(() => {
    if (!copied) return
    const timer = window.setTimeout(() => setCopied(false), 2000)
    return () => window.clearTimeout(timer)
  }, [copied])

  useEffect(() => {
    if (!tabNote) return
    const timer = window.setTimeout(() => setTabNote(''), 2500)
    return () => window.clearTimeout(timer)
  }, [tabNote])

  async function run(e?: FormEvent) {
    e?.preventDefault()
    const keyword = q.trim()
    if (!keyword) return
    setLoading(true)
    setError('')
    setNeedKey(false)
    setHasMore(false)
    try {
      const body = await apiGet<WebSearchPage>(
        `web-search?q=${encodeURIComponent(keyword)}&page=1`,
      )
      const got = body.items ?? []
      setItems(got)
      setQuery(body.query ?? keyword)
      setPage(1)
      setHasMore(hasMorePages(1, got.length, maxPage))
      setFirstRaw(body.raw ?? 0)
      setFirstKept(got.length)
      setScope(body.scope === 'all' ? 'all' : 'share')
      setFirstShares(shareCount(got))
    } catch (err) {
      // A missing or rejected provider key is a setup step, not a failed
      // search: point at 参数设置 instead of shouting in red.
      const code = err instanceof ApiError ? err.code : ''
      if (code === 'search_key_missing' || code === 'search_key_rejected') {
        setNeedKey(true)
      } else {
        setError((err as Error).message)
      }
      setItems(null)
    } finally {
      setLoading(false)
    }
  }

  // The next page appends to the list already on screen. It is one more paid
  // query, so nothing asks for it until the list is scrolled near its end (or
  // the window still has room after a short page).
  const loadMore = useCallback(async () => {
    if (loading || loadingMore || !hasMore || items === null) return
    setLoadingMore(true)
    const next = page + 1
    try {
      const body = await apiGet<WebSearchPage>(
        `web-search?q=${encodeURIComponent(query)}&page=${next}`,
      )
      const got = body.items ?? []
      setItems((prev) => appendResults(prev ?? [], got))
      setPage(next)
      setHasMore(hasMorePages(next, got.length, maxPage))
    } catch (err) {
      // Keep what is on screen; the person can search again.
      setError((err as Error).message)
      setHasMore(false)
    } finally {
      setLoadingMore(false)
    }
  }, [hasMore, items, loading, loadingMore, page, query])

  // The sentinel sits after the last result. Re-observing whenever the list
  // grows means a page too short to fill the window loads the next one at
  // once, instead of waiting for a scroll that cannot happen.
  const sentinel = useRef<HTMLDivElement | null>(null)
  const loadMoreRef = useRef(loadMore)
  useEffect(() => {
    loadMoreRef.current = loadMore
  }, [loadMore])
  useEffect(() => {
    const node = sentinel.current
    if (!node || !hasMore || loading || loadingMore) return
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) void loadMoreRef.current()
      },
      { rootMargin: '300px 0px' },
    )
    observer.observe(node)
    return () => observer.disconnect()
  }, [hasMore, loading, loadingMore, items?.length])

  function openResult(hit: WebResult) {
    const got = openTab(tabs, { id: nextTabId.current++, title: hit.title, url: hit.url })
    setTabs(got.tabs)
    setActiveId(got.id)
  }

  // A wrapped result is one of the search provider's click redirects: its
  // address is asked for now, when it is opened, and not while the list was
  // built. A share continues in an in-app tab like any other share; anything
  // else goes to the browser. When it cannot be read the wrapper itself still
  // opens: it is a redirect, so the browser lands on the same page.
  async function followResult(hit: WebResult) {
    if (!hit.wrapped) {
      if (hit.share) openResult(hit)
      else openExternal(hit.url)
      return
    }
    setError('')
    setResolving(hit.url)
    try {
      const body = await apiGet<{ url: string }>(
        `web-search/resolve?url=${encodeURIComponent(hit.url)}`,
      )
      const dest = body.url
      const share = shareURL(dest) !== ''
      // The answer is kept, so opening it again does not ask twice.
      setItems(
        (prev) =>
          prev &&
          prev.map((r) => (r.url === hit.url ? { ...r, url: dest, wrapped: false, share } : r)),
      )
      if (share) openResult({ ...hit, url: dest })
      else openExternal(dest)
    } catch {
      openExternal(hit.url)
    } finally {
      setResolving('')
    }
  }

  // A new tab, opened by the person's own click so the browser allows it.
  function openExternal(url: string) {
    const a = document.createElement('a')
    a.href = url
    a.target = '_blank'
    a.rel = 'noopener noreferrer'
    a.click()
  }

  function closeTab(id: number) {
    setActiveId(afterClose(tabs, id, activeId))
    setTabs(tabs.filter((t) => t.id !== id))
    setTabStates((prev) => {
      const next = { ...prev }
      delete next[id]
      return next
    })
  }

  function noteTabTitle(id: number, title: string) {
    setTabs((prev) => prev.map((t) => (t.id === id ? { ...t, title } : t)))
  }

  async function addFavorite(tab: WebTab) {
    setTabMenu(null)
    setError('')
    const state = tabStates[tab.id]
    try {
      await apiPost('share-favorites', {
        url: tab.url,
        title: state?.title ?? tab.title,
        pass_code: state?.passCode ?? '',
      })
      setTabNote('已加入收藏夹')
    } catch (err) {
      setError((err as Error).message)
    }
  }

  async function copyTabAddress(tab: WebTab) {
    setTabMenu(null)
    if (await copyText(tab.url)) {
      setCopied(true)
    } else {
      setError('复制地址失败，请打开标签页后手动复制')
    }
  }

  const searched = items !== null && !loading

  return (
    <section className={active ? 'search-page viewer' : 'search-page'}>
      {tabs.length > 0 && (
        <div className="wtabs" role="tablist" aria-label="搜索结果标签页">
          <div
            role="tab"
            aria-selected={!active}
            className={active ? 'wtab' : 'wtab active'}
            onClick={() => setActiveId(null)}
          >
            <SearchIcon size={14} />
            <span className="wtab-title">搜索结果</span>
          </div>
          {tabs.map((tab) => (
            <div
              key={tab.id}
              role="tab"
              aria-selected={tab.id === activeId}
              className={tab.id === activeId ? 'wtab active' : 'wtab'}
              title={tab.title}
              onClick={() => setActiveId(tab.id)}
              onContextMenu={(e) => {
                e.preventDefault()
                setTabMenu({ x: e.clientX, y: e.clientY, tab })
              }}
            >
              <GlobeIcon size={14} />
              <span className="wtab-title">{tab.title}</span>
              <button
                type="button"
                className="wtab-close"
                aria-label="关闭标签页"
                onClick={(e) => {
                  e.stopPropagation()
                  closeTab(tab.id)
                }}
              >
                ×
              </button>
            </div>
          ))}
          {(copied || tabNote) && <span className="wtab-note">{tabNote || '已复制地址'}</span>}
        </div>
      )}

      {active ? (
        <ShareBrowser
          key={active.id}
          url={active.url}
          initialTitle={active.title}
          initialPassCode={tabStates[active.id]?.passCode}
          onTitle={(title) => noteTabTitle(active.id, title)}
          onState={(state) => setTabStates((prev) => ({ ...prev, [active.id]: state }))}
        />
      ) : (
        <div className={searched ? 'search-engine has-results' : 'search-engine'}>
          <form className="se-logo-form" onSubmit={(e) => void run(e)}>
            <h1 className="se-logo">
              PikPak <span>搜索</span>
            </h1>
            <div className="se-field">
              <SearchIcon size={18} />
              <input
                type="search"
                value={q}
                placeholder="搜索 mypikpak.com 上的分享内容"
                onChange={(e) => setQ(e.target.value)}
                aria-label="搜索关键词"
              />
              <button type="submit" disabled={loading || !q.trim()}>
                {loading ? '搜索中…' : '搜索'}
              </button>
            </div>
          </form>

          {needKey && (
            <p className="se-count">
              全网搜索需要先在 <Link to="/settings">参数设置</Link> 里填写搜索 API Key。
            </p>
          )}
          {error && <p className="error se-error">{error}</p>}

          {searched && items.length > 0 && (
            <p className="se-count">已加载 {items.length} 条结果</p>
          )}
          {searched && scope === 'share' && firstKept > 0 && firstRaw > firstKept && (
            <p className="se-count">
              搜索引擎返回 {firstRaw} 条原始结果，其中 {firstKept} 条是 PikPak 分享页或待解析的分享链接，
              其余不是分享页，已忽略。
            </p>
          )}
          {searched && scope === 'all' && (
            <p className="se-count">
              搜索引擎返回 {firstRaw} 条结果，其中 {firstShares} 条是 PikPak 分享页（带分享图标）；其余不是分享页，
              在浏览器中打开。
            </p>
          )}
          {searched && items.length === 0 && page === 1 && (
            <p className="se-count">
              没有找到「{query}」相关的分享页。搜索引擎只收录公开分享，未分享的文件和文件夹搜不到。
            </p>
          )}

          {searched && items.length > 0 && (
            <ol className="se-results">
              {items.map((hit) => (
                <li key={hit.url}>
                  <p className="se-breadcrumb">
                    {resolving === hit.url
                      ? '正在解析链接…'
                      : hit.display || crumbFromURL(hit.url)}
                  </p>
                  <div className="se-title-row">
                    <a
                      className="se-title"
                      href={hit.url}
                      title={hit.title}
                      aria-busy={resolving === hit.url}
                      onClick={(e) => {
                        // Modified clicks keep the browser's own behavior; a
                        // plain click on a share stays in the app, and a
                        // wrapped result is resolved before it is opened.
                        if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return
                        if (!hit.share && !hit.wrapped) return
                        e.preventDefault()
                        void followResult(hit)
                      }}
                    >
                      {hit.title}
                    </a>
                    {scope === 'all' && hit.share && (
                      <span className="se-share" title="PikPak 分享页">
                        <ShareIcon size={13} />
                      </span>
                    )}
                  </div>
                  {hit.snippet && <p className="se-snippet">{hit.snippet}</p>}
                </li>
              ))}
            </ol>
          )}

          {searched && items.length > 0 && (
            <div className="se-more" ref={sentinel} aria-live="polite">
              {loadingMore ? '正在加载更多…' : hasMore ? '' : '没有更多结果了'}
            </div>
          )}
        </div>
      )}

      {tabMenu && (
        <div className="context-menu" style={{ left: tabMenu.x, top: tabMenu.y }}>
          <button type="button" onClick={() => void copyTabAddress(tabMenu.tab)}>
            复制地址
          </button>
          <button type="button" onClick={() => void addFavorite(tabMenu.tab)}>
            加入收藏
          </button>
          <button
            type="button"
            className="danger"
            onClick={() => {
              closeTab(tabMenu.tab.id)
              setTabMenu(null)
            }}
          >
            关闭标签页
          </button>
        </div>
      )}
    </section>
  )
}
