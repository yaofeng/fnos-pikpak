import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { apiGet, ApiError } from '../api'
import { copyText } from '../clipboard'
import { GlobeIcon, SearchIcon } from '../components/icons'
import { crumbFromURL, type WebResult, type WebSearchPage } from '../search'
import { afterClose, openTab, type WebTab } from '../webTabs'

// The provider serves ten pages, each one a paid query.
const maxPage = 10

// Search is 全网搜索: a search-engine query limited to the PikPak site. The
// person types only the keyword — the server adds the site: filter. A result
// opens in an in-app tab (its page is rendered on mypikpak.com), and a
// right-click on a tab can copy its address.
export default function Search() {
  const [q, setQ] = useState('')
  const [items, setItems] = useState<WebResult[] | null>(null)
  const [query, setQuery] = useState('')
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [needKey, setNeedKey] = useState(false)

  const [tabs, setTabs] = useState<WebTab[]>([])
  const [activeId, setActiveId] = useState<number | null>(null)
  const [tabMenu, setTabMenu] = useState<{ x: number; y: number; tab: WebTab } | null>(null)
  const [copied, setCopied] = useState(false)
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

  async function run(e?: FormEvent, wanted = 1) {
    e?.preventDefault()
    const keyword = q.trim()
    if (!keyword) return
    setLoading(true)
    setError('')
    setNeedKey(false)
    try {
      const body = await apiGet<WebSearchPage>(
        `web-search?q=${encodeURIComponent(keyword)}&page=${wanted}`,
      )
      setItems(body.items ?? [])
      setQuery(body.query ?? keyword)
      setPage(body.page ?? wanted)
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

  function openResult(hit: WebResult) {
    const got = openTab(tabs, { id: nextTabId.current++, title: hit.title, url: hit.url })
    setTabs(got.tabs)
    setActiveId(got.id)
  }

  function closeTab(id: number) {
    setActiveId(afterClose(tabs, id, activeId))
    setTabs(tabs.filter((t) => t.id !== id))
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
          {copied && <span className="wtab-note">已复制地址</span>}
        </div>
      )}

      {active ? (
        <div className="wview">
          <iframe className="wframe" src={active.url} title={active.title} />
        </div>
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
            <p className="se-hint">
              通过搜索引擎查找 mypikpak.com 上的公开分享页，站内限定由服务端自动拼接，不用自己写 site: 语法。
              结果只在应用内打开，右键标签页可以复制地址。
            </p>
          </form>

          {needKey && (
            <p className="se-count">
              全网搜索需要先在 <Link to="/settings">参数设置</Link> 里填写搜索 API Key。
            </p>
          )}
          {error && <p className="error se-error">{error}</p>}

          {searched && items.length > 0 && (
            <p className="se-count">
              找到 {items.length} 条结果 · 第 {page} 页
            </p>
          )}
          {searched && items.length === 0 && page > 1 && (
            <p className="se-count">
              第 {page} 页没有更多结果。
              <button type="button" className="link" onClick={() => void run(undefined, 1)}>
                回到第 1 页
              </button>
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
                  <p className="se-breadcrumb">{crumbFromURL(hit.url)}</p>
                  <a
                    className="se-title"
                    href={hit.url}
                    title={hit.title}
                    onClick={(e) => {
                      // Modified clicks keep the browser's own behavior; a
                      // plain click stays in the app.
                      if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return
                      e.preventDefault()
                      openResult(hit)
                    }}
                  >
                    {hit.title}
                  </a>
                  {hit.snippet && <p className="se-snippet">{hit.snippet}</p>}
                </li>
              ))}
            </ol>
          )}

          {searched && (
            <div className="se-pager">
              <button type="button" disabled={page <= 1 || loading} onClick={() => void run(undefined, page - 1)}>
                上一页
              </button>
              <span className="muted small">第 {page} 页</span>
              <button type="button" disabled={page >= maxPage || loading} onClick={() => void run(undefined, page + 1)}>
                下一页
              </button>
            </div>
          )}
        </div>
      )}

      {tabMenu && (
        <div className="context-menu" style={{ left: tabMenu.x, top: tabMenu.y }}>
          <button type="button" onClick={() => void copyTabAddress(tabMenu.tab)}>
            复制地址
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
