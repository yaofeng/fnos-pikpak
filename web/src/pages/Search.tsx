import { useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { apiGet, ApiError } from '../api'
import { SearchIcon } from '../components/icons'
import { crumbFromURL, type WebResult, type WebSearchPage } from '../search'

// Search is 全网搜索: a search-engine query limited to the PikPak site. The
// person types only the keyword — the server adds the site: filter — and each
// result opens in a new tab, because the page lives on mypikpak.com.
export default function Search() {
  const [q, setQ] = useState('')
  const [items, setItems] = useState<WebResult[] | null>(null)
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [needKey, setNeedKey] = useState(false)

  async function run(e?: FormEvent) {
    e?.preventDefault()
    const keyword = q.trim()
    if (!keyword) return
    setLoading(true)
    setError('')
    setNeedKey(false)
    try {
      const page = await apiGet<WebSearchPage>(`web-search?q=${encodeURIComponent(keyword)}`)
      setItems(page.items ?? [])
      setQuery(page.query ?? keyword)
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

  const searched = items !== null && !loading

  return (
    <section className={searched ? 'search-engine has-results' : 'search-engine'}>
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
            autoFocus
          />
          <button type="submit" disabled={loading || !q.trim()}>
            {loading ? '搜索中…' : '搜索'}
          </button>
        </div>
        <p className="se-hint">
          通过搜索引擎查找 mypikpak.com 上的公开分享页，站内限定由服务端自动拼接，不用自己写 site: 语法。
        </p>
      </form>

      {needKey && (
        <p className="se-count">
          全网搜索需要先在 <Link to="/settings">参数设置</Link> 里填写搜索 API Key。
        </p>
      )}
      {error && <p className="error se-error">{error}</p>}

      {searched && items.length > 0 && <p className="se-count">找到 {items.length} 条结果</p>}
      {searched && items.length === 0 && (
        <p className="se-count">
          没有找到「{query}」相关的分享页。搜索引擎只收录公开分享，未分享的文件和文件夹搜不到。
        </p>
      )}

      {searched && items.length > 0 && (
        <ol className="se-results">
          {items.map((hit) => (
            <li key={hit.url}>
              <p className="se-breadcrumb">{crumbFromURL(hit.url)}</p>
              <a className="se-title" href={hit.url} target="_blank" rel="noopener noreferrer" title={hit.title}>
                {hit.title}
              </a>
              {hit.snippet && <p className="se-snippet">{hit.snippet}</p>}
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}
