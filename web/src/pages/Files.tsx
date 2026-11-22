import { useCallback, useEffect, useRef, useState } from 'react'
import { apiGet, apiPatch, apiPost, type ApiError } from '../api'
import Breadcrumb from '../components/Breadcrumb'
import FileGrid from '../components/FileGrid'
import FileTable from '../components/FileTable'
import Player from '../components/Player'
import { askPath, hostAvailable, pickFiles } from '../fnosSdk'
import { cacheKeys, dropCachePrefix, readCache, writeCache } from '../listCache'
import {
  filterFiles,
  isFolder,
  isVideo,
  sortFiles,
  type FileItem,
  type KindFilter,
  type SortDir,
  type SortKey,
} from '../files'

type Page = { items: FileItem[]; next_page_token: string }
type Crumb = { id: string; name: string }
type CachedList = { items: FileItem[]; token: string }

export default function Files() {
  const [stack, setStack] = useState<Crumb[]>([])
  const folderId = stack.length ? stack[stack.length - 1].id : ''

  const [items, setItems] = useState<FileItem[]>([])
  const [token, setToken] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [view, setView] = useState<'list' | 'grid'>('list')
  const [q, setQ] = useState('')
  const [kind, setKind] = useState<KindFilter>('all')
  const [sortKey, setSortKey] = useState<SortKey>('name')
  const [sortDir, setSortDir] = useState<SortDir>('asc')

  const [menu, setMenu] = useState<{ x: number; y: number; file: FileItem } | null>(null)
  const [move, setMove] = useState<{ mode: 'move' | 'copy'; ids: string[] } | null>(null)
  const [busy, setBusy] = useState(false)
  const [playing, setPlaying] = useState<FileItem | null>(null)

  const reqId = useRef(0)
  const itemsRef = useRef<FileItem[]>([])
  const sentinel = useRef<HTMLDivElement | null>(null)
  const dragged = useRef<FileItem | null>(null)

  const fetchPage = useCallback(
    async (pageToken: string, reset: boolean) => {
      const id = ++reqId.current
      setLoading(true)
      setError('')
      const params = new URLSearchParams({ parent_id: folderId, limit: '100' })
      if (pageToken) params.set('page_token', pageToken)
      if (kind !== 'all') params.set('kind', kind === 'folder' ? 'drive#folder' : 'drive#file')
      try {
        const page = await apiGet<Page>(`files?${params.toString()}`)
        if (id !== reqId.current) return
        const nextItems = reset ? page.items : [...itemsRef.current, ...page.items]
        itemsRef.current = nextItems
        const nextToken = page.next_page_token || ''
        setItems(nextItems)
        setToken(nextToken)
        writeCache<CachedList>(cacheKeys.files(folderId, kind), { items: nextItems, token: nextToken })
      } catch (e) {
        if (id === reqId.current) setError((e as Error).message)
        if (reset) setItems([])
      } finally {
        if (id === reqId.current) setLoading(false)
      }
    },
    [folderId, kind],
  )

  useEffect(() => {
    setSelected(new Set())
    // Coming back to a folder shows the listing it had; 刷新 and the mutation
    // paths replace it.
    const cached = readCache<CachedList>(cacheKeys.files(folderId, kind))
    if (cached) {
      itemsRef.current = cached.items
      setItems(cached.items)
      setToken(cached.token)
      return
    }
    itemsRef.current = []
    setItems([])
    setToken('')
    void fetchPage('', true)
  }, [fetchPage, folderId, kind])

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
    if (!menu) return
    const close = () => setMenu(null)
    window.addEventListener('click', close)
    return () => window.removeEventListener('click', close)
  }, [menu])

  const shown = sortFiles(filterFiles(items, q, kind), sortKey, sortDir)

  function toggle(id: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  function openFile(f: FileItem) {
    if (!isFolder(f)) return
    setStack((prev) => [...prev, { id: f.id, name: f.name }])
    setQ('')
  }

  function navigate(index: number) {
    setStack((prev) => (index < 0 ? [] : prev.slice(0, index + 1)))
    setQ('')
  }

  async function createFolder() {
    const name = window.prompt('文件夹名称')
    if (!name?.trim()) return
    setBusy(true)
    try {
      await apiPost('folders', { parent_id: folderId, name: name.trim() })
      dropCachePrefix('files:')
      await fetchPage('', true)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function rename(f: FileItem) {
    const name = window.prompt('新名称', f.name)
    if (!name?.trim() || name.trim() === f.name) return
    setBusy(true)
    try {
      await apiPatch(`files/${f.id}`, { name: name.trim() })
      dropCachePrefix('files:')
      await fetchPage('', true)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function trash(ids: string[]) {
    if (!ids.length) return
    if (!window.confirm(`将 ${ids.length} 项移入回收站？`)) return
    setBusy(true)
    try {
      const res = await apiPost<{ pending: boolean }>('files/trash', { ids })
      setNotice(res.pending ? '任务已提交，稍后自动刷新' : '已移入回收站')
      setSelected(new Set())
      dropCachePrefix('files:')
      await fetchPage('', true)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function applyMove(destId: string) {
    if (!move) return
    setBusy(true)
    try {
      const res = await apiPost<{ pending: boolean }>(`files/${move.mode}`, {
        ids: move.ids,
        to_parent_id: destId,
      })
      setNotice(res.pending ? '任务已提交，稍后自动刷新' : move.mode === 'move' ? '已移动' : '已复制')
      setMove(null)
      setSelected(new Set())
      dropCachePrefix('files:')
      await fetchPage('', true)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function download(ids: string[]) {
    const fileIDs = ids.filter((id) => {
      const f = items.find((item) => item.id === id)
      return f && !isFolder(f)
    })
    if (!fileIDs.length) {
      setError('文件夹暂不支持下载，请逐个选择其中的文件。')
      return
    }
    setBusy(true)
    try {
      // No directory in the request: downloads go to the folder configured in
      // settings, which the server validates against the user's grants.
      const res = await apiPost<{ created: unknown[]; skipped: string[] }>('downloads', {
        file_ids: fileIDs,
      })
      const skipped = res.skipped?.length ? `，跳过 ${res.skipped.length} 个同名文件` : ''
      setNotice(`已加入下载队列：${res.created.length} 个${skipped}。可在「传输」中查看进度。`)
      setSelected(new Set())
    } catch (e) {
      setError(
        (e as ApiError).code === 'download_dir_unset'
          ? '还没有设置默认下载目录，请到「设置 → 传输」指定一个目录。'
          : (e as Error).message,
      )
    } finally {
      setBusy(false)
    }
  }

  async function uploadFiles() {
    let paths: string[] = []
    if (hostAvailable()) {
      paths = await pickFiles('选择要上传的文件')
    } else {
      const asked = askPath('要上传的文件（绝对路径）')
      if (asked) paths = [asked]
    }
    if (!paths.length) return
    setBusy(true)
    try {
      const res = await apiPost<{ created: unknown[] }>('uploads', {
        local_paths: paths,
        parent_id: folderId,
      })
      setNotice(`已加入上传队列：${res.created.length} 个。可在「传输」中查看进度。`)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  function handleDrop(destId: string | null) {
    const f = dragged.current
    dragged.current = null
    if (!f) return
    if (destId === f.id) {
      setError('不能把文件夹移动到它自己里面')
      return
    }
    const ids = selected.has(f.id) ? [...selected] : [f.id]
    // Dropping applies immediately; the dialog is only for the menu path.
    void (async () => {
      setBusy(true)
      try {
        const res = await apiPost<{ pending: boolean }>('files/move', {
          ids,
          to_parent_id: destId ?? '',
        })
        setNotice(res.pending ? '任务已提交，稍后自动刷新' : '已移动')
        setMove(null)
        setSelected(new Set())
        dropCachePrefix('files:')
      await fetchPage('', true)
      } catch (e) {
        setError((e as Error).message)
      } finally {
        setBusy(false)
      }
    })()
  }

  const selectedIds = [...selected]

  return (
    <section className="file-page">
      <div className="file-toolbar">
        <Breadcrumb stack={stack} onNavigate={navigate} onDropOn={handleDrop} />
        <div className="file-tools">
          <input
            className="search"
            type="search"
            placeholder="在当前目录中筛选"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            title="PikPak 不支持服务端搜索，这里只筛选已加载的条目"
          />
          <select value={kind} onChange={(e) => setKind(e.target.value as KindFilter)}>
            <option value="all">全部</option>
            <option value="folder">文件夹</option>
            <option value="file">文件</option>
          </select>
          <select value={`${sortKey}:${sortDir}`} onChange={(e) => {
            const [k, d] = e.target.value.split(':')
            setSortKey(k as SortKey)
            setSortDir(d as SortDir)
          }}>
            <option value="name:asc">名称 ↑</option>
            <option value="name:desc">名称 ↓</option>
            <option value="modified:desc">时间 ↓</option>
            <option value="modified:asc">时间 ↑</option>
            <option value="size:desc">大小 ↓</option>
            <option value="size:asc">大小 ↑</option>
          </select>
          <button type="button" onClick={() => setView(view === 'list' ? 'grid' : 'list')}>
            {view === 'list' ? '网格' : '列表'}
          </button>
          <button type="button" onClick={createFolder} disabled={busy}>
            新建文件夹
          </button>
          <button type="button" onClick={() => void uploadFiles()} disabled={busy}>
            上传
          </button>
          <button type="button" onClick={() => void fetchPage('', true)} disabled={loading}>
            刷新
          </button>
        </div>
      </div>

      <p className="muted small">
        排序与筛选只作用于已加载的条目（PikPak 列表接口不支持服务端排序/搜索）。
      </p>

      {error && <p className="error">{error}</p>}
      {notice && <p className="ok small">{notice}</p>}

      {selectedIds.length > 0 && (
        <div className="selection-bar">
          <span>已选 {selectedIds.length} 项</span>
          <button type="button" onClick={() => setMove({ mode: 'move', ids: selectedIds })} disabled={busy}>
            移动到…
          </button>
          <button type="button" onClick={() => setMove({ mode: 'copy', ids: selectedIds })} disabled={busy}>
            复制到…
          </button>
          <button type="button" onClick={() => void download(selectedIds)} disabled={busy}>
            下载
          </button>
          <button type="button" onClick={() => void trash(selectedIds)} disabled={busy}>
            移入回收站
          </button>
          <button type="button" onClick={() => setSelected(new Set())}>
            取消选择
          </button>
        </div>
      )}

      {!loading && shown.length === 0 && !error && (
        <p className="muted">{items.length === 0 ? '这个文件夹是空的。' : '没有匹配的条目。'}</p>
      )}

      {view === 'list' ? (
        <FileTable
          items={shown}
          selected={selected}
          onToggle={toggle}
          onOpen={openFile}
          onMenu={(e, f) => {
            e.preventDefault()
            setMenu({ x: e.clientX, y: e.clientY, file: f })
          }}
          onDragStart={(f) => {
            dragged.current = f
          }}
          onDropOn={(id) => handleDrop(id)}
        />
      ) : (
        <FileGrid
          items={shown}
          selected={selected}
          onToggle={toggle}
          onOpen={openFile}
          onMenu={(e, f) => {
            e.preventDefault()
            setMenu({ x: e.clientX, y: e.clientY, file: f })
          }}
          onDragStart={(f) => {
            dragged.current = f
          }}
          onDropOn={(id) => handleDrop(id)}
        />
      )}

      <div ref={sentinel} className="sentinel" />
      {loading && <p className="muted">加载中…</p>}

      {menu && (
        <div className="context-menu" style={{ left: menu.x, top: menu.y }}>
          {isFolder(menu.file) && (
            <button type="button" onClick={() => openFile(menu.file)}>
              打开
            </button>
          )}
          <button type="button" onClick={() => void rename(menu.file)}>
            重命名
          </button>
          <button type="button" onClick={() => setMove({ mode: 'move', ids: [menu.file.id] })}>
            移动到…
          </button>
          <button type="button" onClick={() => setMove({ mode: 'copy', ids: [menu.file.id] })}>
            复制到…
          </button>
          {isVideo(menu.file) && (
            <button
              type="button"
              onClick={() => {
                setPlaying(menu.file)
                setMenu(null)
              }}
            >
              播放
            </button>
          )}
          {!isFolder(menu.file) && (
            <button type="button" onClick={() => void download([menu.file.id])}>
              下载
            </button>
          )}
          <button type="button" className="danger" onClick={() => void trash([menu.file.id])}>
            移入回收站
          </button>
        </div>
      )}

      {playing && <Player fileId={playing.id} name={playing.name} onClose={() => setPlaying(null)} />}

      {move && (
        <div className="modal-backdrop" onClick={() => setMove(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h3>{move.mode === 'move' ? '移动到' : '复制到'}</h3>
            <div className="dest-list">
              <button type="button" onClick={() => void applyMove('')} disabled={busy}>
                我的文件（根目录）
              </button>
              {stack
                .filter((c) => !move.ids.includes(c.id))
                .map((c) => (
                  <button key={c.id} type="button" onClick={() => void applyMove(c.id)} disabled={busy}>
                    {c.name}
                  </button>
                ))}
            </div>
            <p className="muted small">已选 {move.ids.length} 项。也可以把它们拖到面包屑上的文件夹。</p>
            <button type="button" onClick={() => setMove(null)}>
              取消
            </button>
          </div>
        </div>
      )}
    </section>
  )
}
