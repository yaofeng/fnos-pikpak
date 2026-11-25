import { useCallback, useEffect, useRef, useState, type MouseEvent } from 'react'
import StatusBar from '../components/StatusBar'
import { apiGet, apiPatch, apiPost, basePath } from '../api'
import { accountLabel } from '../accounts'
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
  filesQuery,
  isFolder,
  isVideo,
  sortFiles,
  thumbPath,
  type FileItem,
  type KindFilter,
  type SortDir,
  type SortKey,
} from '../files'
import { getLang, makeT } from '../i18n'

type AccountError = { account_id: string; account_name?: string; error: string }
type Page = { items: FileItem[]; next_page_token: string; account_errors?: AccountError[] }
type Crumb = { id: string; name: string }
type CachedList = { items: FileItem[]; token: string }

// withAccount scopes a request path to one account: the server resolves an
// empty id to the caller's default, so unbound-in-flight pages just omit it.
function withAccount(path: string, accountId: string): string {
  return accountId ? `${path}?account=${encodeURIComponent(accountId)}` : path
}

export default function Files() {
  const lang = getLang()
  const t = makeT(lang)
  const { accounts, byId, currentId: accountId, scope, setCurrent } = useAccounts()
  // 'all' is the merged view: one listing across every account, walked by
  // path names because folder ids only mean something inside one account.
  const merged = scope === 'all'
  const [stack, setStack] = useState<Crumb[]>([])
  const folderId = stack.length ? stack[stack.length - 1].id : ''
  // The merged listing address: crumb names JSON-packed so a name containing
  // '/' cannot read as two levels in the cache key.
  const folderKey = merged ? `path:${JSON.stringify(stack.map((c) => c.name))}` : folderId

  const [items, setItems] = useState<FileItem[]>([])
  const [token, setToken] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  // Accounts whose page of a merged listing failed; single-account listings
  // leave it empty.
  const [accountErrors, setAccountErrors] = useState<AccountError[]>([])

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
  // The scope the current listing came from; the switch effect below uses
  // the delta to own the render in which the scope changed.
  const prevScope = useRef(scope)

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
      // filesQuery emits the scope's account/seg/parent_id/kind pairs; limit
      // and the pagination token append after it, keeping the single-account
      // parameter set identical to 2.0.0's.
      const params = new URLSearchParams({ limit: '100' })
      if (pageToken) params.set('page_token', pageToken)
      const query = `${filesQuery(scope, stack.map((c) => c.name), folderId, kind)}&${params.toString()}`
      try {
        const page = await apiGet<Page>(`files?${query}`)
        if (id !== reqId.current) return
        const nextItems = reset ? page.items : [...itemsRef.current, ...page.items]
        itemsRef.current = nextItems
        const nextToken = page.next_page_token || ''
        setItems(nextItems)
        setToken(nextToken)
        setAccountErrors(page.account_errors ?? [])
        writeCache<CachedList>(cacheKeys.files(scope, folderKey, kind), { items: nextItems, token: nextToken })
      } catch (e) {
        if (id === reqId.current) setError((e as Error).message)
        // A stale request must not blank what a newer one already rendered.
        if (id === reqId.current && reset) setItems([])
      } finally {
        if (id === reqId.current) setLoading(false)
      }
    },
    [scope, stack, folderId, folderKey, kind],
  )

  useEffect(() => {
    // The switch effect below owns the render in which the scope changed:
    // it refetches from the root, so the pre-switch folder request is skipped
    // rather than sent (a foreign parent id would only 404).
    if (prevScope.current !== scope) return
    clearSelection()
    // Coming back to a folder shows the listing it had; 刷新 and the mutation
    // paths replace it. Keys carry the scope, so a cached listing can never
    // be another scope's rows.
    const cached = readCache<CachedList>(cacheKeys.files(scope, folderKey, kind))
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
  }, [fetchPage, scope, folderKey, kind, clearSelection])

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

  // Switching scopes changes whose files everything is: drop every page's
  // cache, go back to the drive root with nothing selected, and refetch. The
  // folder effect skips this same render (prevScope delta), so only this
  // root request goes out; the reqId guard still drops anything stale.
  useEffect(() => {
    if (prevScope.current === scope) return
    prevScope.current = scope
    resetCache()
    setStack([])
    itemsRef.current = []
    setItems([])
    setToken('')
    setAccountErrors([])
    clearSelection()
    void fetchPage('', true)
  }, [scope, fetchPage, clearSelection])

  function openFile(f: FileItem) {
    if (!isFolder(f)) return
    // Merged rows carry no folder id that means anything across accounts:
    // the pushed crumb holds the name only, and the names are the path.
    setStack((prev) => [...prev, merged ? { id: '', name: f.name } : { id: f.id, name: f.name }])
    setQ('')
  }

  function navigate(index: number) {
    setStack((prev) => (index < 0 ? [] : prev.slice(0, index + 1)))
    setQ('')
  }

  async function createFolder() {
    const name = window.prompt(t('files.folder_name_prompt'))
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
    const name = window.prompt(t('files.rename_prompt'), f.name)
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
      const plan = deletePlan(mode, lang)
      const res = await apiPost<{ pending: boolean }>(withAccount(plan.path, accountId), { ids })
      setNotice(res.pending ? t('common.submitted') : plan.notice)
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
      setNotice(res.pending ? t('common.submitted') : move.mode === 'move' ? t('files.moved') : t('files.copied'))
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
      setError(t('files.folder_download_unsupported'))
      return
    }
    setBusy(true)
    try {
      // No directory in the request: downloads go to the folder configured in
      // settings, which the server validates against the user's grants. Every
      // item names this page's account, so the queue acts on the same drive
      // the listing came from.
      setNotice(await queueDownloads(fileIDs.map((file_id) => ({ file_id, account_id: accountId })), lang))
      clearSelection()
    } catch (e) {
      setError(downloadErrorMessage(e, lang))
    } finally {
      setBusy(false)
    }
  }

  async function uploadFiles() {
    let paths: string[] = []
    if (hostAvailable()) {
      paths = await pickFiles(t('files.pick_upload_files'))
    } else {
      const asked = askPath(t('files.ask_upload_file'))
      if (asked) paths = [asked]
    }
    if (!paths.length) return
    setBusy(true)
    try {
      const res = await apiPost<{ created: unknown[] }>(withAccount('uploads', accountId), {
        local_paths: paths,
        parent_id: folderId,
      })
      setNotice(t('files.queued_uploads').replace('{n}', String(res.created.length)))
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
      setError(t('files.move_into_self'))
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
        setNotice(res.pending ? t('common.submitted') : t('files.moved'))
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
        <AccountSwitcher accounts={accounts} scope={scope} onChange={setCurrent} />
        <button className="icon-btn" type="button" onClick={goUp} disabled={stack.length === 0} title={t('share.up_title')} aria-label={t('share.up_title')}>
          <ChevronLeft />
        </button>
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
        <div className="crumb-box">
          <Breadcrumb stack={stack} onNavigate={navigate} onDropOn={handleDrop} />
        </div>
        <label className="fm-search">
          <SearchIcon />
          <input
            type="search"
            placeholder={t('share.filter_placeholder')}
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </label>
      </div>

      <div className="fm-actions">
        <button className="btn-icon" type="button" onClick={() => void uploadFiles()} disabled={busy}>
          <UploadIcon />
          {t('files.upload')}
        </button>
        <button className="btn-icon" type="button" onClick={createFolder} disabled={busy}>
          <FolderPlusIcon />
          {t('files.new_folder')}
        </button>
        <button
          className="btn-icon"
          type="button"
          onClick={() => void download(selectedIds)}
          disabled={busy || selectedIds.length === 0}
        >
          <DownloadIcon />
          {t('common.download')}
        </button>
        <button
          className="btn-icon"
          type="button"
          onClick={() => setRemoveAsk(selectedIds)}
          disabled={busy || selectedIds.length === 0}
        >
          <TrashIcon />
          {t('common.delete')}
        </button>
        <button className="btn-icon" type="button" onClick={openMore}>
          <MoreIcon />
          {t('files.more')}
        </button>

        <span className="fm-spacer" />

        <label className="fm-pick">
          <select value={kind} onChange={(e) => setKind(e.target.value as KindFilter)} aria-label={t('files.filter_label')}>
            <option value="all">{t('files.kind_all')}</option>
            <option value="folder">{t('files.kind_folder')}</option>
            <option value="file">{t('files.kind_file')}</option>
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
            aria-label={t('favorites.sort_label')}
          >
            <option value="name:asc">{t('share.sort_name')} ↑</option>
            <option value="name:desc">{t('share.sort_name')} ↓</option>
            <option value="modified:desc">{t('share.sort_time')} ↓</option>
            <option value="modified:asc">{t('share.sort_time')} ↑</option>
            <option value="size:desc">{t('files.th_size')} ↓</option>
            <option value="size:asc">{t('files.th_size')} ↑</option>
          </select>
        </label>
        <div className="segmented" role="group" aria-label={t('vfav.view_label')}>
          <button
            type="button"
            className={view === 'list' ? 'active' : ''}
            onClick={() => setView('list')}
            title={t('vfav.view_list')}
            aria-label={t('vfav.view_list')}
          >
            <ListIcon />
          </button>
          <button
            type="button"
            className={view === 'grid' ? 'active' : ''}
            onClick={() => setView('grid')}
            title={t('vfav.view_grid')}
            aria-label={t('vfav.view_grid')}
          >
            <GridIcon />
          </button>
        </div>
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

      {!loading && shown.length === 0 && !error && (
        <p className="muted">{items.length === 0 ? t('files.empty_folder') : t('files.no_match')}</p>
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
            thumbSrc={(f) => `${basePath}/api/${thumbPath(f.id, undefined, f.account_id || accountId)}`}
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
        {loading && <p className="muted">{t('common.loading')}</p>}
      </div>

      <StatusBar selected={selectedIds.length} total={shown.length} notice={notice} />

      {menu && (
        <div className="context-menu" style={{ left: menu.x, top: menu.y }}>
          {menu.file ? (
            <>
              {isFolder(menu.file) && (
                <button type="button" onClick={() => openFile(menu.file!)}>
                  {t('files.open')}
                </button>
              )}
              <button type="button" onClick={() => void rename(menu.file!)}>
                {t('account.rename')}
              </button>
              <button type="button" onClick={() => setMove({ mode: 'move', ids: [menu.file!.id] })}>
                {t('files.move_to')}
              </button>
              <button type="button" onClick={() => setMove({ mode: 'copy', ids: [menu.file!.id] })}>
                {t('files.copy_to')}
              </button>
              {isVideo(menu.file) && (
                <button
                  type="button"
                  onClick={() => {
                    setPlaying(menu.file!)
                    setMenu(null)
                  }}
                >
                  {t('files.play')}
                </button>
              )}
              {!isFolder(menu.file) && (
                <button type="button" onClick={() => void download([menu.file!.id])}>
                  {t('common.download')}
                </button>
              )}
              <button type="button" className="danger" onClick={() => void remove([menu.file!.id], 'trash')}>
                {t('files.trash_it')}
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
                {t('files.move_to')}
              </button>
              <button
                type="button"
                disabled={selectedIds.length === 0}
                onClick={() => {
                  setMenu(null)
                  setMove({ mode: 'copy', ids: selectedIds })
                }}
              >
                {t('files.copy_to')}
              </button>
              <button type="button" onClick={() => selectAll()}>
                {t('files.select_all')}
              </button>
              <button type="button" onClick={() => clearSelection()}>
                {t('files.clear_selection')}
              </button>
            </>
          )}
        </div>
      )}

      {playing && (
        <Player
          fileId={playing.id}
          name={playing.name}
          account={playing.account_id || accountId}
          onClose={() => setPlaying(null)}
        />
      )}

      {move && (
        <div className="modal-backdrop" onClick={() => setMove(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h3>{move.mode === 'move' ? t('files.move_to') : t('files.copy_to')}</h3>
            <div className="dest-list">
              <button type="button" onClick={() => void applyMove('')} disabled={busy}>
                {t('files.root_dest')}
              </button>
              {stack
                .filter((c) => !move.ids.includes(c.id))
                .map((c) => (
                  <button key={c.id} type="button" onClick={() => void applyMove(c.id)} disabled={busy}>
                    {c.name}
                  </button>
                ))}
            </div>
            <p className="muted small">{t('files.move_hint').replace('{n}', String(move.ids.length))}</p>
            <button type="button" onClick={() => setMove(null)}>
              {t('common.cancel')}
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
            aria-label={t('common.delete')}
            onClick={(e) => e.stopPropagation()}
          >
            <h3>{t('files.remove_ask').replace('{n}', String(removeAsk.length))}</h3>
            <p className="muted small">{t('files.remove_hint')}</p>
            <div className="actions">
              <button type="button" className="primary" disabled={busy} onClick={() => void remove(removeAsk, 'trash')}>
                {t('files.trash_it')}
              </button>
              <button type="button" className="danger" disabled={busy} onClick={() => void remove(removeAsk, 'delete')}>
                {t('trash.delete_forever')}
              </button>
              <button type="button" onClick={() => setRemoveAsk(null)}>
                {t('common.cancel')}
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  )
}
