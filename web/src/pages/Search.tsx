import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { apiGet, apiPost, type ApiError } from '../api'
import { FileIcon } from '../components/icons'
import { formatSize, isFolder, type FileItem } from '../files'

type PathSeg = { id: string; name: string }
type Hit = FileItem & { path: PathSeg[] }
type SearchPage = { items: Hit[]; scanned: number; truncated: boolean }

// Search looks inside the whole PikPak account, not the current folder. PikPak
// exposes no search endpoint, so the server walks every folder under a budget;
// the page says so instead of pretending the list is exhaustive.
export default function Search() {
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const [items, setItems] = useState<Hit[] | null>(null)
  const [scanned, setScanned] = useState(0)
  const [truncated, setTruncated] = useState(false)
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

  function crumbs(hit: Hit): PathSeg[] {
    return [...hit.path, { id: hit.id, name: hit.name }]
  }

  function openFolder(hit: Hit) {
    navigate(`/files?crumbs=${encodeURIComponent(JSON.stringify(crumbs(hit)))}`)
  }

  // Opens the folder that contains the hit, so the file can be worked with in
  // its own place.
  function openParent(hit: Hit) {
    navigate(`/files?crumbs=${encodeURIComponent(JSON.stringify(hit.path))}`)
  }

  async function download(hit: Hit) {
    setError('')
    setNotice('')
    try {
      await apiPost('downloads', { file_ids: [hit.id] })
      setNotice(`已加入下载队列：${hit.name}`)
    } catch (err) {
      setError(
        (err as ApiError).code === 'download_dir_unset'
          ? '还没有设置默认下载目录，请到「设置 → 参数设置」指定一个目录。'
          : (err as Error).message,
      )
    }
  }

  return (
    <section className="card search-page">
      <form className="search-form" onSubmit={(e) => void run(e)}>
        <input
          type="search"
          value={q}
          placeholder="在整个 PikPak 中搜索文件、文件夹名"
          onChange={(e) => setQ(e.target.value)}
          aria-label="搜索关键词"
        />
        <button type="submit" disabled={loading || !q.trim()}>
          {loading ? '搜索中…' : '搜索'}
        </button>
      </form>
      <p className="muted small">
        PikPak 没有搜索接口，这里会逐层遍历你账号里的文件夹（上限 2000 个文件夹、200 条结果、20 秒），大网盘可能只返回一部分。
      </p>

      {error && <p className="error">{error}</p>}
      {notice && <p className="ok small">{notice}</p>}
      {loading && <p className="muted">搜索中…</p>}

      {items !== null && !loading && (
        <p className="muted small">
          找到 {items.length} 项（扫描 {scanned} 个条目）
          {truncated ? '，已达上限，结果可能不完整' : ''}
        </p>
      )}
      {items !== null && !loading && items.length === 0 && <p className="muted">没有匹配的文件或文件夹。</p>}

      {items && items.length > 0 && (
        <ul className="search-results">
          {items.map((hit) => (
            <li key={hit.id}>
              <div className="search-result-head">
                <span className="search-hit">
                  <span className="file-icon">
                    <FileIcon file={hit} />
                  </span>
                  <span className="name-text" title={hit.name}>
                    {hit.name}
                  </span>
                </span>
                <span className="search-host">{isFolder(hit) ? '文件夹' : formatSize(hit.size)}</span>
              </div>
              <p className="search-snippet">
                {[hit.path.map((p) => p.name).join(' / '), hit.modified_time ? new Date(hit.modified_time).toLocaleString() : '']
                  .filter(Boolean)
                  .join(' · ') || '根目录'}
              </p>
              <div className="search-actions">
                {isFolder(hit) ? (
                  <button type="button" onClick={() => openFolder(hit)}>
                    打开
                  </button>
                ) : (
                  <button type="button" onClick={() => void download(hit)}>
                    下载
                  </button>
                )}
                <button type="button" onClick={() => openParent(hit)}>
                  打开所在文件夹
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
