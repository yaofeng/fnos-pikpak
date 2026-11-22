import { useCallback, useEffect, useRef, useState } from 'react'
import { apiGet, apiGetQuiet, apiPost, basePath, ApiError } from '../api'
import { iinaURL, isMacOS } from '../iina'
import { waitForVideoJob } from '../videoPrepare'
import { filterFiles, isFolder, isVideo, sortFiles, thumbPath, type FileItem, type SortDir, type SortKey } from '../files'
import { useFileSelection } from '../useFileSelection'
import FileGrid from './FileGrid'
import FileTable from './FileTable'
import Player from './Player'
import {
  BookmarkIcon,
  DownloadIcon,
  GridIcon,
  HomeIcon,
  ListIcon,
  PlayIcon,
  RefreshIcon,
  RestoreIcon,
  SearchIcon,
  StarIcon,
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
  owner?: string
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

export type ShareState = { title: string; passCode: string; protected: boolean; owner: string }

export default function ShareBrowser({
  url,
  favoriteId,
  initialTitle,
  initialPassCode,
  onState,
  onTitle,
  onHome,
}: {
  url?: string
  favoriteId?: number
  initialTitle?: string
  // initialPassCode reopens a share whose tab was switched away: the code was
  // already typed and accepted, so asking for it again would be churn.
  initialPassCode?: string
  onState?: (state: ShareState) => void
  onTitle?: (title: string) => void
  // onHome adds a home button that leaves the share for its host page, the
  // favourites home. Search tabs have no such home, so they omit it.
  onHome?: () => void
}) {
  const [shareId, setShareId] = useState('')
  const [title, setTitle] = useState(initialTitle ?? '分享')
  const [owner, setOwner] = useState('')
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
  // directPlay carries a prepared copy's signed URL straight into the player.
  const [directPlay, setDirectPlay] = useState<{ file_id: string; name: string; stream_url: string } | null>(null)
  // iinaPending is a signed URL waiting for the click that opens IINA: an
  // external protocol needs a fresh user gesture after the async prepare.
  // onFallback plays the same stream in the built-in player, for a Mac (or
  // iPad) without IINA installed.
  const [iinaPending, setIinaPending] = useState<{
    url: string
    name: string
    onFallback: () => void
  } | null>(null)
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
  const rememberedCode = useRef(initialPassCode ?? '')
  rememberedCode.current = initialPassCode ?? rememberedCode.current

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
        setOwner(info.owner ?? '')
        setItems(info.items ?? [])
        setStack([])
        setQ('')
        setNeedPass(false)
        setPassCode(code)
        callbacks.current.onState?.({ title: name, passCode: code, protected: info.protected, owner: info.owner ?? '' })
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
    setPassCode(rememberedCode.current)
    void open(rememberedCode.current)
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
    else void preview(f)
  }

  // absoluteURL turns a server path such as /api/stream/f1?t=… into the URL an
  // external player has to be handed.
  function absoluteURL(path: string): string {
    return window.location.origin + basePath + path
  }

  // preview plays the share's own stream: nothing is restored.
  async function preview(f: FileItem) {
    if (isFolder(f)) {
      enter(f)
      return
    }
    if (!isMacOS(navigator.userAgent)) {
      setPlaying(f)
      return
    }
    setError('')
    setIinaPending(null)
    try {
      const got = await apiGet<ShareFileUrls>(
        `share/file?share_id=${encodeURIComponent(shareId)}&file_id=${encodeURIComponent(f.id)}`,
      )
      setIinaPending({
        url: absoluteURL(got.stream_url),
        name: f.name,
        onFallback: () => {
          setIinaPending(null)
          setNotice('')
          setPlaying(f)
        },
      })
      setNotice('点「用 IINA 播放」开始预览')
    } catch (err) {
      if (err instanceof ApiError && err.code === 'share_session_missing') void open(passCode)
      else setError((err as Error).message)
    }
  }

  const shown = sortFiles(filterFiles(items ?? [], q, 'all'), sortKey, sortDir)
  const { ids, selected, select, clear: clearSelection } = useFileSelection(shown.map((f) => f.id))

  const picked = shown.filter((f) => selected.has(f.id))
  const pickedFiles = picked.filter((f) => !isFolder(f))

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

  // 完整播放 restores the video into 我的视频/临时 (making room by purging the
  // oldest files) and plays that copy, so playback does not depend on the
  // share staying available.
  async function fullPlay(f: FileItem) {
    if (isFolder(f)) return
    setBusy('prepare')
    setError('')
    setNotice('正在把视频转存到「我的视频/临时」…')
    setIinaPending(null)
    try {
      const started = await apiPost<{ job_id: string }>('videos/prepare', {
        share_id: shareId,
        file_id: f.id,
      })
      const job = await waitForVideoJob(started.job_id, {
        get: (id) => apiGet(`videos/jobs/${encodeURIComponent(id)}`),
      })
      if (!job.stream_url) throw new Error('视频准备失败，请稍后重试')
      const direct = {
        file_id: job.file_id || f.id,
        name: job.name || f.name,
        stream_url: job.stream_url,
      }
      if (isMacOS(navigator.userAgent)) {
        setIinaPending({
          url: absoluteURL(job.stream_url),
          name: direct.name,
          onFallback: () => {
            setIinaPending(null)
            setNotice('')
            setDirectPlay(direct)
          },
        })
        setNotice('已准备好，点「用 IINA 播放」开始播放')
      } else {
        setDirectPlay(direct)
        setNotice('')
      }
    } catch (err) {
      setError((err as Error).message)
      setNotice('')
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
      setNotice('已收藏这个分享')
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  // 收藏视频 saves the selected videos with the share's code, so a later
  // 完整播放 from 视频收藏夹 never asks for it again.
  async function addVideoFavorites(list: FileItem[]) {
    if (list.length === 0) return
    setBusy('video-favorite')
    setError('')
    setNotice('')
    try {
      for (const f of list) {
        await apiPost('video-favorites', {
          share_id: shareId,
          file_id: f.id,
          name: f.name,
          size: f.size,
          owner,
          share_title: title,
          pass_code: passCode,
        })
      }
      setNotice(list.length === 1 ? `「${list[0].name}」已加入视频收藏夹` : `${list.length} 个视频已加入视频收藏夹`)
      clearSelection()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  const single = picked.length === 1 ? picked[0] : null
  // 预览 and 完整播放 act on one video at a time; a folder or a multi-select
  // leaves them disabled rather than guessing.
  const singleVideo = single && !isFolder(single) && isVideo(single) ? single : null
  const inFavorites = favoriteId !== undefined

  return (
    <section className="share-browser">
      {iinaPending && (
        <p className="share-iina">
          <button
            type="button"
            className="primary"
            onClick={() => {
              window.location.href = iinaURL(iinaPending.url)
            }}
          >
            用 IINA 播放
          </button>
          <button type="button" onClick={iinaPending.onFallback}>
            用内置播放器
          </button>
          <span className="muted small">
            {iinaPending.name} · 若没有打开，可能尚未安装 IINA
          </span>
        </p>
      )}

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
            {onHome && (
              <button
                className="icon-btn"
                type="button"
                onClick={onHome}
                title="收藏夹主页"
                aria-label="收藏夹主页"
              >
                <HomeIcon />
              </button>
            )}
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
                <button type="button" onClick={() => goTo(-1)} disabled={stack.length === 0} title="分享根目录">
                  {title || '分享'}
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
              disabled={!singleVideo}
              onClick={() => singleVideo && void preview(singleVideo)}
            >
              <PlayIcon />
              预览
            </button>
            <button
              className="btn-icon"
              type="button"
              disabled={!singleVideo || busy === 'prepare'}
              onClick={() => singleVideo && void fullPlay(singleVideo)}
              title="转存到「我的视频/临时」后播放，空间不足时自动清理最早的视频"
            >
              <DownloadIcon />
              {busy === 'prepare' ? '准备中…' : '完整播放'}
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
                {favorited ? '已收藏' : '收藏分享'}
              </button>
            )}
            <button
              className="btn-icon"
              type="button"
              disabled={pickedFiles.length === 0 || busy === 'video-favorite'}
              onClick={() => void addVideoFavorites(pickedFiles)}
            >
              <StarIcon />
              收藏视频
            </button>

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

          {error && <p className="error">{error}</p>}

          {items !== null && shown.length === 0 && !loading && (
            <p className="muted">{(items?.length ?? 0) === 0 ? '这个分享是空的。' : '没有匹配的条目。'}</p>
          )}

          <div className="file-body">
            {view === 'list' ? (
              <FileTable items={shown} selected={selected} onSelect={select} onOpen={openFile} />
            ) : (
              <FileGrid
                thumbSrc={(f) => `${basePath}/api/${thumbPath(f.id, shareId)}`}
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
            <span>{ids.length > 0 ? `选中 ${ids.length} 项（共 ${shown.length} 项）` : `共 ${shown.length} 项`}</span>
            {notice && <span className="ok status-note">{notice}</span>}
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

      {directPlay && (
        <Player
          fileId={directPlay.file_id}
          name={directPlay.name}
          streamURL={directPlay.stream_url}
          onClose={() => setDirectPlay(null)}
        />
      )}
    </section>
  )
}
