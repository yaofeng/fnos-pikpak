import { useState, type FormEvent } from 'react'
import { apiGet, apiPost } from '../api'

type Hit = { title: string; url: string; snippet: string }
type SearchPage = { items: Hit[] }

function hostOf(raw: string): string {
  try {
    return new URL(raw).host
  } catch {
    return ''
  }
}

// Search asks the engine for links, scoped to one site when the user names
// one (the engine's site: operator). A hit can become an offline task
// directly, which is what most searches here are for.
export default function Search() {
  const [q, setQ] = useState('')
  const [site, setSite] = useState('')
  const [items, setItems] = useState<Hit[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  async function run(e?: FormEvent) {
    e?.preventDefault()
    const query = q.trim()
    if (!query) return
    setLoading(true)
    setError('')
    setNotice('')
    try {
      const params = new URLSearchParams({ q: query })
      if (site.trim()) params.set('site', site.trim())
      const page = await apiGet<SearchPage>(`web-search?${params.toString()}`)
      setItems(page.items ?? [])
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  async function addOffline(hit: Hit) {
    if (!window.confirm(`把这条链接加入离线下载？\n${hit.url}`)) return
    try {
      await apiPost('tasks', { link: hit.url, parent_id: '' })
      setNotice('已创建离线任务，可在「离线任务」查看进度')
    } catch (err) {
      setError((err as Error).message)
    }
  }

  async function copy(url: string) {
    try {
      await navigator.clipboard.writeText(url)
      setNotice('链接已复制')
    } catch {
      setError('浏览器不允许写入剪贴板，请手动复制')
    }
  }

  return (
    <section className="card search-page">
      <form className="search-form" onSubmit={(e) => void run(e)}>
        <input
          type="search"
          value={q}
          placeholder="搜索全网资源"
          onChange={(e) => setQ(e.target.value)}
          aria-label="搜索关键词"
        />
        <input
          type="text"
          value={site}
          placeholder="限定站点（可选，如 example.com）"
          onChange={(e) => setSite(e.target.value)}
          aria-label="限定站点"
        />
        <button type="submit" disabled={loading || !q.trim()}>
          {loading ? '搜索中…' : '搜索'}
        </button>
      </form>
      <p className="muted small">
        关键词交给搜索引擎，站点一栏走它的 site: 站内限定；留空则是全网搜索。结果只是网页链接，是否加入离线下载由你决定。
      </p>

      {error && <p className="error">{error}</p>}
      {notice && <p className="ok small">{notice}</p>}
      {loading && items === null && <p className="muted">搜索中…</p>}
      {items !== null && items.length === 0 && !loading && <p className="muted">没有搜索到结果。</p>}

      {items && items.length > 0 && (
        <ul className="search-results">
          {items.map((hit) => (
            <li key={hit.url}>
              <div className="search-result-head">
                <a href={hit.url} target="_blank" rel="noreferrer noopener" title={hit.title}>
                  {hit.title}
                </a>
                <span className="search-host">{hostOf(hit.url)}</span>
              </div>
              {hit.snippet && <p className="search-snippet">{hit.snippet}</p>}
              <div className="search-actions">
                <button type="button" onClick={() => void addOffline(hit)}>
                  加入离线下载
                </button>
                <button type="button" onClick={() => void copy(hit.url)}>
                  复制链接
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
