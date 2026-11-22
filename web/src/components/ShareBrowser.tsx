import { useCallback, useEffect, useRef, useState } from 'react'
import { apiGet, apiGetQuiet, apiPost, basePath, ApiError } from '../api'
import { filterFiles, isFolder, sortFiles, type FileItem, type SortDir, type SortKey } from '../files'
import { useFileSelection } from '../useFileSelection'
import FileGrid from './FileGrid'
import FileTable from './FileTable'
import Player from './Player'
import {
  BookmarkIcon,
  DownloadIcon,
  GridIcon,
  ListIcon,
  PlayIcon,
  RefreshIcon,
  RestoreIcon,
  SearchIcon,
  SortIcon,
} from './icons'

// The share browser mirrors 我的文件 inside a share: the same navigation and
// action toolbars, the same selection, list/icon views and sort. Folders open
// in place, files play or download through a signed URL, and anything can be
// restored into the user's own drive. The pass code is typed here, sent to the
// server once, and never comes back.
type ShareInfo = {
  share_id: string
  title: string
  protected: boolean
  items: FileItem[]
  next_page_token: string
}

type SharePage = { items: FileItem[]; next_page_token: string }

type ShareFileUrls = {
  file_id: string
  name: string
  mime: string
  size: number
  stream_url: string
  download_url: string
}

type Crumb = { id: string; name: string }

export type ShareState = { title: string; passCode: string; protected: boolean }

export default function ShareBrowser({
  url,
  favoriteId,
  initialTitle,
  onState,
  onTitle,
}: {
  url?: string
  favoriteId?: number
  initialTitle?: string
  onState?: (state: ShareState) => void
  onTitle?: (title: string) => void
}) {
  const [shareId, setShareId] = useState('')
  const [title, setTitle] = useState(initialTitle ?? '分享')
  const [protectedShare, setProtectedShare] = useState(false)
  const [passCode, setPassCode] = useState('')
  const [needPass, setNeedPass] = useState(false)
  const [openError, setOpenError] = useState('')
  const [opening, setOpening] = useState(false)

  const [items, setItems] = useState<FileItem[] | null>(null)
  const [stack, setStack] = useState<Crumb[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState('')
  const [playing, setPlaying] = useState<FileItem | null>(null)
  const [favorited, setFavorited] = useState(false)

  // The toolbars follow 我的文件: a local filter, a sort key, and the view.
  const [q, setQ] = useState('')
  const [sortKey, setSortKey] = useState<SortKey>('name')
  const [sortDir, setSortDir] = useState<SortDir>('asc')
  const [view, setView] = useState<'list' | 'grid'>('grid')

  // Callbacks belong to the parent; a ref keeps them out of the open effect's
  // dependency list, so a re-render never reopens the share.
  const callbacks = useRef({ onState, onTitle })
  callbacks.current = { onState, onTitle }
  // The parent may retitle the tab once the share reports its own name; that
  // must not reopen the share, so the fallback title lives in a ref too.
  const fallbackTitle = useRef(initialTitle)
  fallbackTitle.current = initialTitle

  const open = useCallback(
    async (code: string) => {
      setOpening(true)
      setOpenError('')
      setError('')
      setNotice('')
      try {
        const base = favoriteId
          ? `share?favorite_id=${favoriteId}`
          : `share?url=${encodeURIComponent(url ?? '')}`
        const suffix = !favoriteId && code ? `&pass_code=${encodeURIComponent(code)}` : ''
        // Opening a protected share answers pass_code_required first: that is
        // the form, not a failure to shout about.
        const info = await apiGetQuiet<ShareInfo>(base + suffix)
        const name = info.title || fallbackTitle.current || '分享'
        setShareId(info.share_id)
        setTitle(name)
        setProtectedShare(info.protected)
        setItems(info.items ?? [])
        setStack([])
        setQ('')
        setNeedPass(false)
        setPassCode(code)
        callbacks.current.onState?.({ title: name, passCode: code, protected: info.protected })
        callbacks.current.onTitle?.(name)
      } catch (err) {
        const code2 = err instanceof ApiError ? err.code : ''
        if (code2 === 'pass_code_required' || code2 === 'pass_code_wrong') {
          setNeedPass(true)
          setOpenError((err as Error).message)
          setItems(null)
        } else {
          setOpenError((err as Error).message)
          setItems(null)
        }
      } finally {
        setOpening(false)
      }
    },
    [url, favoriteId],
  )

  useEffect(() => {
    setPassCode('')
    void open('')
  }, [open])

  async function loadFolder(parentId: string) {
    setLoading(true)
    setError('')
    try {
      const page = await apiGet<SharePage>(
        `share/files?share_id=${encodeURIComponent(shareId)}&parent_id=${encodeURIComponent(parentId)}`,
      )
      setItems(page.items ?? [])
      setQ('')
    } catch (err) {
      if (err instanceof ApiError && err.code === 'share_session_missing') {
        // The server forgot the session (restart): reopen with the code that
        // already worked.
        void open(passCode)
        return
      }
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  function enter(f: FileItem) {
    setStack((prev) => [...prev, { id: f.id, name: f.name }])
    void loadFolder(f.id)
  }

  function goTo(index: number) {
    const next = index < 0 ? [] : stack.slice(0, index + 1)
    setStack(next)
    void loadFolder(next.length ? next[next.length - 1].id : '')
  }

  function openFile(f: FileItem) {
    if (isFolder(f)) enter(f)
    else setPlaying(f)
  }

  const shown = sortFiles(filterFiles(items ?? [], q, 'all'), sortKey, sortDir)
  const { ids, selected, select, clear: clearSelection } = useFileSelection(shown.map((f) => f.id))

  const picked = shown.filter((f) => selected.has(f.id))
  const pickedFiles = picked.filter((f) => !isFolder(f))

  async function download(list: FileItem[]) {
    if (list.length === 0) return
    setBusy('download')
    setError('')
    setNotice('')
    try {
      for (const f of list) {
        const got = await apiGet<ShareFileUrls>(
          `share/file?share_id=${encodeURIComponent(shareId)}&file_id=${encodeURIComponent(f.id)}`,
        )
        // An attachment navigation keeps the browser's own download manager and
        // the signed URL out of the page.
        const a = document.createElement('a')
        a.href = basePath + got.download_url
        a.download = got.name || f.name
        document.body.appendChild(a)
        a.click()
        a.remove()
      }
      setNotice(list.length === 1 ? `开始下载 ${list[0].name}` : `开始下载 ${list.length} 个文件`)
    } catch (err) {
      if (err instanceof ApiError && err.code === 'share_session_missing') void open(passCode)
      else setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function restore(list: FileItem[]) {
    if (list.length === 0) return
    setBusy('restore')
    setError('')
    setNotice('')
    try {
      await apiPost('share/restore', { share_id: shareId, file_ids: list.map((f) => f.id) })
      setNotice(
        list.length === 1
          ? `「${list[0].name}」已转存到我的文件根目录`
          : `${list.length} 项已转存到我的文件根目录`,
      )
      clearSelection()
    } catch (err) {
      if (err instanceof ApiError && err.code === 'share_session_missing') void open(passCode)
      else setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function addFavorite() {
    setBusy('favorite')
    setError('')
    try {
      await apiPost('share-favorites', {
        url: favoriteId ? undefined : url,
        share_id: favoriteId ? shareId : undefined,
        title,
        pass_code: passCode,
      })
      setFavorited(true)
      setNotice('已加入收藏夹')
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  const single = picked.length === 1 ? picked[0] : null
  const inFavorites = favoriteId !== undefined

  return (
    <section className="share-browser">
      <div className="share-heading">
        <h2>{title}</h2>
        <span className="muted small">
          {protectedShare ? '受密码保护' : '公开分享'}
          {shareId ? ` · ${shareId}` : ''}
        </span>
      </div>

      {notice && <p className="ok small">{notice}</p>}

      {needPass ? (
        <form
          className="share-pass"
          onSubmit={(e) => {
            e.preventDefault()
            void open(passCode)
          }}
        >
          <p className="muted">{openError || '该分享需要访问密码'}</p>
          <div className="dir-row">
            <input
              value={passCode}
              onChange={(e) => setPassCode(e.target.value)}
              placeholder="访问密码"
              aria-label="访问密码"
              autoFocus
            />
            <button type="submit" className="primary" disabled={opening || !passCode.trim()}>
              {opening ? '打开中…' : '打开'}
            </button>
          </div>
        </form>
      ) : (
        <>
          {openError && <p className="error">{openError}</p>}

          <div className="fm-nav">
            <button
              className="icon-btn"
              type="button"
              onClick={() => goTo(stack.length - 2)}
              disabled={stack.length === 0}
              title="上一级"
              aria-label="上一级"
            >
              ‹
            </button>
            <button
              className="icon-btn"
              type="button"
              onClick={() => void open(passCode)}
              disabled={opening}
              title="刷新"
              aria-label="刷新"
            >
              <RefreshIcon />
            </button>
            <div className="crumb-box">
              <nav className="share-crumbs" aria-label="分享目录">
                <button type="button" onClick={() => goTo(-1)} disabled={stack.length === 0}>
                  分享根目录
                </button>
                {stack.map((c, i) => (
                  <span key={c.id} className="share-crumb">
                    ›
                    <button type="button" onClick={() => goTo(i)} disabled={i === stack.length - 1}>
                      {c.name}
                    </button>
                  </span>
                ))}
                {loading && <span className="muted small">加载中…</span>}
              </nav>
            </div>
            <label className="fm-search">
              <SearchIcon />
              <input
                type="search"
                placeholder="在当前目录中筛选"
                value={q}
                onChange={(e) => setQ(e.target.value)}
                title="PikPak 分享接口不支持服务端搜索，这里只筛选已加载的条目"
              />
            </label>
          </div>

          <div className="fm-actions">
            <button
              className="btn-icon"
              type="button"
              disabled={!single || busy === 'download'}
              onClick={() => single && openFile(single)}
            >
              <PlayIcon />
              打开
            </button>
            <button
              className="btn-icon"
              type="button"
              disabled={pickedFiles.length === 0 || busy === 'download'}
              onClick={() => void download(pickedFiles)}
            >
              <DownloadIcon />
              下载
            </button>
            <button
              className="btn-icon"
              type="button"
              disabled={picked.length === 0 || busy === 'restore'}
              onClick={() => void restore(picked)}
            >
              <RestoreIcon />
              转存
            </button>
            {inFavorites ? (
              <span className="tag">已在收藏夹</span>
            ) : (
              <button
                className="btn-icon"
                type="button"
                disabled={!shareId || busy === 'favorite' || favorited}
                onClick={() => void addFavorite()}
              >
                <BookmarkIcon />
                {favorited ? '已收藏' : '加入收藏'}
              </button>
            )}

            <span className="fm-spacer" />

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

          <p className="muted small">
            排序与筛选只作用于已加载的条目（PikPak 分享列表接口不支持服务端排序/搜索）。双击文件夹可进入，双击文件可播放。
          </p>

          {error && <p className="error">{error}</p>}

          {items !== null && shown.length === 0 && !loading && (
            <p className="muted">{(items?.length ?? 0) === 0 ? '这个分享是空的。' : '没有匹配的条目。'}</p>
          )}

          <div className="file-body">
            {view === 'list' ? (
              <FileTable items={shown} selected={selected} onSelect={select} onOpen={openFile} />
            ) : (
              <FileGrid
                items={shown}
                selected={selected}
                onSelect={select}
                onOpen={openFile}
                onMenu={() => undefined}
                onDragStart={() => undefined}
                onDropOn={() => undefined}
              />
            )}
          </div>

          <div className="status-bar">
            {ids.length > 0 ? `选中 ${ids.length} 项（共 ${shown.length} 项）` : `共 ${shown.length} 项`}
          </div>
        </>
      )}

      {playing && (
        <Player
          fileId={playing.id}
          name={playing.name}
          infoPath={`share/file?share_id=${encodeURIComponent(shareId)}&file_id=${encodeURIComponent(playing.id)}`}
          onClose={() => setPlaying(null)}
        />
      )}
    </section>
  )
}
