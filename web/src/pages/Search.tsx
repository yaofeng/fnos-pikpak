import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { apiGet } from '../api'
import { SearchIcon } from '../components/icons'
import type { FileItem } from '../files'

type PathSeg = { id: string; name: string }
type Hit = FileItem & { path: PathSeg[] }
type SearchPage = { items: Hit[]; scanned: number; truncated: boolean }

function formatSize(n: number): string {
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

// Search looks inside the whole PikPak account, laid out like a search engine:
// one box, then results as a title, a breadcrumb and a line about the entry.
// PikPak exposes no search endpoint, so the server walks every folder under a
// budget and says when the answer is partial.
export default function Search() {
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const [items, setItems] = useState<Hit[] | null>(null)
  const [scanned, setScanned] = useState(0)
  const [truncated, setTruncated] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  async function run(e?: FormEvent) {
    e?.preventDefault()
    const query = q.trim()
    if (!query) return
    setLoading(true)
    setError('')
    try {
      const page = await apiGet<SearchPage>(`files/search?q=${encodeURIComponent(query)}`)
      setItems(page.items ?? [])
      setScanned(page.scanned ?? 0)
      setTruncated(Boolean(page.truncated))
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  // A hit opens its folder: a folder opens itself, a file opens the folder it
  // lives in, where every action is available.
  function open(hit: Hit) {
    const crumbs = hit.kind === 'drive#folder' ? [...hit.path, { id: hit.id, name: hit.name }] : hit.path
    navigate(`/files?crumbs=${encodeURIComponent(JSON.stringify(crumbs))}`)
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
            placeholder="在整个 PikPak 中搜索文件、文件夹名"
            onChange={(e) => setQ(e.target.value)}
            aria-label="搜索关键词"
            autoFocus
          />
          <button type="submit" disabled={loading || !q.trim()}>
            {loading ? '搜索中…' : '搜索'}
          </button>
        </div>
        <p className="se-hint">
          PikPak 没有搜索接口，这里逐层遍历账号里的文件夹（上限 2000 个文件夹、200 条结果、20 秒），大网盘可能只返回一部分。
        </p>
      </form>

      {error && <p className="error se-error">{error}</p>}

      {searched && (
        <p className="se-count">
          找到 {items.length} 项（扫描 {scanned} 个条目）{truncated ? '，已达上限，结果可能不完整' : ''}
        </p>
      )}
      {searched && items.length === 0 && <p className="se-count">没有匹配的文件或文件夹。</p>}

      {searched && items.length > 0 && (
        <ol className="se-results">
          {items.map((hit) => (
            <li key={hit.id}>
              <p className="se-breadcrumb">{[...hit.path.map((p) => p.name), ''].join(' / ')}</p>
              <button type="button" className="se-title" onClick={() => open(hit)} title={hit.name}>
                {hit.name}
              </button>
              <p className="se-snippet">
                {hit.kind === 'drive#folder' ? '文件夹' : formatSize(hit.size)}
                {hit.modified_time ? ` · ${new Date(hit.modified_time).toLocaleString()}` : ''}
              </p>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}
