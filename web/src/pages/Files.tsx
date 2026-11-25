import { useCallback, useEffect, useRef, useState, type MouseEvent } from 'react'
import StatusBar from '../components/StatusBar'
import { apiGet, apiPatch, apiPost, basePath } from '../api'
import { downloadErrorMessage, queueDownloads } from '../downloads'
import { useAccounts } from '../AccountContext'
import AccountSwitcher from '../components/AccountSwitcher'
import Breadcrumb from '../components/Breadcrumb'
import FileGrid from '../components/FileGrid'
import FileTable from '../components/FileTable'
import Player from '../components/Player'
import {
  ChevronLeft,
  DownloadIcon,
  FolderPlusIcon,
  GridIcon,
  ListIcon,
  MoreIcon,
  RefreshIcon,
  SearchIcon,
  SortIcon,
  TrashIcon,
  UploadIcon,
} from '../components/icons'
import { askPath, hostAvailable, pickFiles } from '../fnosSdk'
import { deletePlan, type DeleteMode } from '../deleteFlow'
import { cacheKeys, dropCachePrefix, readCache, resetCache, writeCache } from '../listCache'
import { useFileSelection } from '../useFileSelection'
import { useMarquee } from '../useMarquee'
import {
  filterFiles,
  isFolder,
  isVideo,
  sortFiles,
  thumbPath,
  type FileItem,
  type KindFilter,
  type SortDir,
  type SortKey,
} from '../files'

type Page = { items: FileItem[]; next_page_token: string }
type Crumb = { id: string; name: string }
type CachedList = { items: FileItem[]; token: string }

// withAccount scopes a request path to one account: the server resolves an
// empty id to the caller's default, so unbound-in-flight pages just omit it.
function withAccount(path: string, accountId: string): string {
  return accountId ? `${path}?account=${encodeURIComponent(accountId)}` : path
}

export default function Files() {
  const { accounts, currentId: accountId, setCurrent } = useAccounts()
  const [stack, setStack] = useState<Crumb[]>([])
  const folderId = stack.length ? stack[stack.length - 1].id : ''

  const [items, setItems] = useState<FileItem[]>([])
  const [token, setToken] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const [view, setView] = useState<'list' | 'grid'>('grid')
  const [q, setQ] = useState('')
  const [kind, setKind] = useState<KindFilter>('all')
  const [sortKey, setSortKey] = useState<SortKey>('name')
  const [sortDir, setSortDir] = useState<SortDir>('asc')

  const [menu, setMenu] = useState<{ x: number; y: number; file: FileItem | null } | null>(null)
  const [move, setMove] = useState<{ mode: 'move' | 'copy'; ids: string[] } | null>(null)
  // Ids waiting on the delete dialog, which asks 移入回收站 or 永久删除.
  const [removeAsk, setRemoveAsk] = useState<string[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [playing, setPlaying] = useState<FileItem | null>(null)

  const reqId = useRef(0)
  const itemsRef = useRef<FileItem[]>([])
  const sentinel = useRef<HTMLDivElement | null>(null)
  const dragged = useRef<FileItem | null>(null)

  const shown = sortFiles(filterFiles(items, q, kind), sortKey, sortDir)
  // Selection is by list position, like the Finder: click selects one,
  // Command/Control adds or removes, Shift takes the range, and a drag on the
  // background rubber-bands over whatever it touches. Declared before the
  // effects below so they can clear it in their dependency arrays.
  const {
    ids: selectedIds,
    selected,
    select,
    hitTest,
    selectAll,
    clear: clearSelection,
  } = useFileSelection(shown.map((f) => f.id))
  const { box: bodyRef, rect: marqueeRect, onMouseDown: startMarquee } = useMarquee({ onHits: hitTest })

  const fetchPage = useCallback(
    async (pageToken: string, reset: boolean) => {
      const id = ++reqId.current
      setLoading(true)
      setError('')
      const params = new URLSearchParams({ parent_id: folderId, limit: '100' })
      if (pageToken) params.set('page_token', pageToken)
      if (kind !== 'all') params.set('kind', kind === 'folder' ? 'drive#folder' : 'drive#file')
      if (accountId) params.set('account', accountId)
      try {
        const page = await apiGet<Page>(`files?${params.toString()}`)
        if (id !== reqId.current) return
        const nextItems = reset ? page.items : [...itemsRef.current, ...page.items]
        itemsRef.current = nextItems
        const nextToken = page.next_page_token || ''
        setItems(nextItems)
        setToken(nextToken)
        writeCache<CachedList>(cacheKeys.files(accountId, folderId, kind), { items: nextItems, token: nextToken })
      } catch (e) {
        if (id === reqId.current) setError((e as Error).message)
        if (reset) setItems([])
      } finally {
        if (id === reqId.current) setLoading(false)
      }
    },
    [accountId, folderId, kind],
  )

  useEffect(() => {
    clearSelection()
    // Coming back to a folder shows the listing it had; 刷新 and the mutation
    // paths replace it. Keys carry the account, so a cached listing can never
    // be another account's rows.
    const cached = readCache<CachedList>(cacheKeys.files(accountId, folderId, kind))
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
  }, [fetchPage, folderId, kind, accountId, clearSelection])

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

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT')) return
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'a') {
        e.preventDefault()
        selectAll()
        return
      }
      if (e.key === 'Escape') clearSelection()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [selectAll, clearSelection])

  // Switching accounts changes whose files everything is: drop every page's
  // cache, go back to the drive root with nothing selected, and refetch. The
  // reqId guard drops whatever response the pre-switch folder request lands,
  // and a folder id foreign to this account 404s into that same drop.
  const prevAccount = useRef(accountId)
  useEffect(() => {
    if (prevAccount.current === accountId) return
    prevAccount.current = accountId
    resetCache()
    setStack([])
    itemsRef.current = []
    setItems([])
    setToken('')
    clearSelection()
    void fetchPage('', true)
  }, [accountId, fetchPage, clearSelection])

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
      await apiPost(withAccount('folders', accountId), { parent_id: folderId, name: name.trim() })
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
      await apiPatch(withAccount(`files/${f.id}`, accountId), { name: name.trim() })
      dropCachePrefix('files:')
      await fetchPage('', true)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function remove(ids: string[], mode: DeleteMode) {
    if (!ids.length) return
    setRemoveAsk(null)
    setBusy(true)
    try {
      const plan = deletePlan(mode)
      const res = await apiPost<{ pending: boolean }>(withAccount(plan.path, accountId), { ids })
      setNotice(res.pending ? '任务已提交，稍后自动刷新' : plan.notice)
      clearSelection()
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
      const res = await apiPost<{ pending: boolean }>(withAccount(`files/${move.mode}`, accountId), {
        ids: move.ids,
        to_parent_id: destId,
      })
      setNotice(res.pending ? '任务已提交，稍后自动刷新' : move.mode === 'move' ? '已移动' : '已复制')
      setMove(null)
      clearSelection()
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
      // settings, which the server validates against the user's grants. Every
      // item names this page's account, so the queue acts on the same drive
      // the listing came from.
      setNotice(await queueDownloads(fileIDs.map((file_id) => ({ file_id, account_id: accountId }))))
      clearSelection()
    } catch (e) {
      setError(downloadErrorMessage(e))
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
      const res = await apiPost<{ created: unknown[] }>(withAccount('uploads', accountId), {
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
        const res = await apiPost<{ pending: boolean }>(withAccount('files/move', accountId), {
          ids,
          to_parent_id: destId ?? '',
        })
        setNotice(res.pending ? '任务已提交，稍后自动刷新' : '已移动')
        setMove(null)
        clearSelection()
        dropCachePrefix('files:')
      await fetchPage('', true)
      } catch (e) {
        setError((e as Error).message)
      } finally {
        setBusy(false)
      }
    })()
  }

  function goUp() {
    if (stack.length === 0) return
    navigate(stack.length - 2)
  }

  // 更多 opens the same context menu at the button, with the actions that act
  // on the current selection.
  function openMore(e: MouseEvent<HTMLButtonElement>) {
    const r = e.currentTarget.getBoundingClientRect()
    setMenu({ x: r.left, y: r.bottom + 4, file: null })
  }

  return (
    <section className="file-page">
      <div className="fm-nav">
        <AccountSwitcher accounts={accounts} currentId={accountId} onChange={setCurrent} />
        <button className="icon-btn" type="button" onClick={goUp} disabled={stack.length === 0} title="上一级" aria-label="上一级">
          <ChevronLeft />
        </button>
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
        <div className="crumb-box">
          <Breadcrumb stack={stack} onNavigate={navigate} onDropOn={handleDrop} />
        </div>
        <label className="fm-search">
          <SearchIcon />
          <input
            type="search"
            placeholder="在当前目录中筛选"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </label>
      </div>

      <div className="fm-actions">
        <button className="btn-icon" type="button" onClick={() => void uploadFiles()} disabled={busy}>
          <UploadIcon />
          上传
        </button>
        <button className="btn-icon" type="button" onClick={createFolder} disabled={busy}>
          <FolderPlusIcon />
          新建文件夹
        </button>
        <button
          className="btn-icon"
          type="button"
          onClick={() => void download(selectedIds)}
          disabled={busy || selectedIds.length === 0}
        >
          <DownloadIcon />
          下载
        </button>
        <button
          className="btn-icon"
          type="button"
          onClick={() => setRemoveAsk(selectedIds)}
          disabled={busy || selectedIds.length === 0}
        >
          <TrashIcon />
          删除
        </button>
        <button className="btn-icon" type="button" onClick={openMore}>
          <MoreIcon />
          更多
        </button>

        <span className="fm-spacer" />

        <label className="fm-pick">
          <select value={kind} onChange={(e) => setKind(e.target.value as KindFilter)} aria-label="筛选">
            <option value="all">全部</option>
            <option value="folder">文件夹</option>
            <option value="file">文件</option>
          </select>
        </label>
        <label className="fm-pick">
          <SortIcon />
          <select
            value={`${sortKey}:${sortDir}`}
            onChange={(e) => {
              const [k, d] = e.target.value.split(':')
              setSortKey(k as SortKey)
              setSortDir(d as SortDir)
            }}
            aria-label="排序"
          >
            <option value="name:asc">名称 ↑</option>
            <option value="name:desc">名称 ↓</option>
            <option value="modified:desc">时间 ↓</option>
            <option value="modified:asc">时间 ↑</option>
            <option value="size:desc">大小 ↓</option>
            <option value="size:asc">大小 ↑</option>
          </select>
        </label>
        <div className="segmented" role="group" aria-label="视图">
          <button
            type="button"
            className={view === 'list' ? 'active' : ''}
            onClick={() => setView('list')}
            title="列表视图"
            aria-label="列表视图"
          >
            <ListIcon />
          </button>
          <button
            type="button"
            className={view === 'grid' ? 'active' : ''}
            onClick={() => setView('grid')}
            title="大图标视图"
            aria-label="大图标视图"
          >
            <GridIcon />
          </button>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      {!loading && shown.length === 0 && !error && (
        <p className="muted">{items.length === 0 ? '这个文件夹是空的。' : '没有匹配的条目。'}</p>
      )}

      <div className="file-body" ref={bodyRef} onMouseDown={startMarquee}>
        {view === 'list' ? (
          <FileTable
            items={shown}
            selected={selected}
            onSelect={select}
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
            thumbSrc={(f) => `${basePath}/api/${thumbPath(f.id, undefined, accountId)}`}
            items={shown}
            selected={selected}
            onSelect={select}
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
        {marqueeRect && (
          <div
            className="marquee"
            style={{ left: marqueeRect.left, top: marqueeRect.top, width: marqueeRect.width, height: marqueeRect.height }}
          />
        )}
        {/* Inside the scroll area: the observer sees it only when the list
            really reached its end. */}
        <div ref={sentinel} className="sentinel" />
        {loading && <p className="muted">加载中…</p>}
      </div>

      <StatusBar selected={selectedIds.length} total={shown.length} notice={notice} />

      {menu && (
        <div className="context-menu" style={{ left: menu.x, top: menu.y }}>
          {menu.file ? (
            <>
              {isFolder(menu.file) && (
                <button type="button" onClick={() => openFile(menu.file!)}>
                  打开
                </button>
              )}
              <button type="button" onClick={() => void rename(menu.file!)}>
                重命名
              </button>
              <button type="button" onClick={() => setMove({ mode: 'move', ids: [menu.file!.id] })}>
                移动到…
              </button>
              <button type="button" onClick={() => setMove({ mode: 'copy', ids: [menu.file!.id] })}>
                复制到…
              </button>
              {isVideo(menu.file) && (
                <button
                  type="button"
                  onClick={() => {
                    setPlaying(menu.file!)
                    setMenu(null)
                  }}
                >
                  播放
                </button>
              )}
              {!isFolder(menu.file) && (
                <button type="button" onClick={() => void download([menu.file!.id])}>
                  下载
                </button>
              )}
              <button type="button" className="danger" onClick={() => void remove([menu.file!.id], 'trash')}>
                移入回收站
              </button>
            </>
          ) : (
            <>
              <button
                type="button"
                disabled={selectedIds.length === 0}
                onClick={() => {
                  setMenu(null)
                  setMove({ mode: 'move', ids: selectedIds })
                }}
              >
                移动到…
              </button>
              <button
                type="button"
                disabled={selectedIds.length === 0}
                onClick={() => {
                  setMenu(null)
                  setMove({ mode: 'copy', ids: selectedIds })
                }}
              >
                复制到…
              </button>
              <button type="button" onClick={() => selectAll()}>
                全选
              </button>
              <button type="button" onClick={() => clearSelection()}>
                取消选择
              </button>
            </>
          )}
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

      {removeAsk && (
        <div className="modal-backdrop" onClick={() => setRemoveAsk(null)}>
          <div
            className="modal"
            role="dialog"
            aria-modal="true"
            aria-label="删除"
            onClick={(e) => e.stopPropagation()}
          >
            <h3>删除 {removeAsk.length} 项？</h3>
            <p className="muted small">移入回收站后仍可找回；永久删除会跳过回收站，无法恢复。</p>
            <div className="actions">
              <button type="button" className="primary" disabled={busy} onClick={() => void remove(removeAsk, 'trash')}>
                移入回收站
              </button>
              <button type="button" className="danger" disabled={busy} onClick={() => void remove(removeAsk, 'delete')}>
                永久删除
              </button>
              <button type="button" onClick={() => setRemoveAsk(null)}>
                取消
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  )
}
