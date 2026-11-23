import { useCallback, useEffect, useRef, useState } from 'react'
import { apiDelete, apiGet, apiGetQuiet, apiPost, basePath, ApiError } from '../api'
import { iinaURL } from '../iina'
import { waitForVideoJob } from '../videoPrepare'
import { filterFiles, isFolder, isVideo, sortFiles, thumbPath, type FileItem, type SortDir, type SortKey } from '../files'
import { onlyFolderEntry } from '../shares'
import { useFileSelection } from '../useFileSelection'
import FileGrid from './FileGrid'
import FileTable from './FileTable'
import Player from './Player'
import {
  CheckIcon,
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
  // Set only when 播放器 = IINA: the share's own CDN link for the external
  // player.
  direct_url?: string
}

type Crumb = { id: string; name: string }

export type ShareState = { title: string; passCode: string; protected: boolean; owner: string }

// shareBadge marks the videos the app already knows: a copy in Pack From
// Shared (已转存) wins over a place in 视频收藏夹 (已收藏).
function shareBadge(f: FileItem) {
  if (f.restored) {
    return { icon: <CheckIcon size={13} />, title: '已转存到 Pack From Shared', kind: 'restored' as const }
  }
  if (f.favorited) {
    return { icon: <StarIcon size={13} filled />, title: '已在视频收藏夹', kind: 'star' as const }
  }
  return null
}

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
  // rootSkipped says the share's root held one folder and was stepped through;
  // going "up" from that folder would only show a level with no choice.
  const [rootSkipped, setRootSkipped] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState('')
  const [playing, setPlaying] = useState<FileItem | null>(null)
  // directPlay carries a prepared copy's signed URL straight into the player.
  const [directPlay, setDirectPlay] = useState<{ file_id: string; name: string; stream_url: string } | null>(null)
  // The star in the address bar toggles this share's 分享收藏, like the star
  // in a browser: filled when saved, click again to remove.
  const [favoriteID, setFavoriteID] = useState(favoriteId ?? 0)

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
        const rootItems = info.items ?? []
        // A root that is a single folder is stepped through: the browser
        // opens that folder and offers no way back to the level above it.
        const skip = onlyFolderEntry(rootItems, info.next_page_token ?? '')
        setShareId(info.share_id)
        setTitle(name)
        setOwner(info.owner ?? '')
        setItems(skip ? null : rootItems)
        setStack(skip ? [{ id: skip.id, name: skip.name }] : [])
        setRootSkipped(Boolean(skip))
        setQ('')
        if (skip) void loadFolder(skip.id, info.share_id)
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

  async function loadFolder(parentId: string, sid = shareId) {
    setLoading(true)
    setError('')
    try {
      const page = await apiGet<SharePage>(
        `share/files?share_id=${encodeURIComponent(sid)}&parent_id=${encodeURIComponent(parentId)}`,
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
    // The stepped-through root is not reachable from below.
    if (rootSkipped && index < 0) return
    const next = index < 0 ? [] : stack.slice(0, index + 1)
    setStack(next)
    void loadFolder(next.length ? next[next.length - 1].id : '')
  }

  // A double click on a video is 完整播放: restore a copy into Pack From
  // Shared (reusing one already there) and play that. 预览 stays the explicit
  // toolbar button.
  function openFile(f: FileItem) {
    if (isFolder(f)) enter(f)
    else void fullPlay(f)
  }

  // handToIINA opens the external player with the original link the server
  // supplied for 播放器 = IINA. Nothing is proxied through this app.
  function handToIINA(directURL: string) {
    window.location.href = iinaURL(directURL)
    setNotice('已交给 IINA 打开；若没有打开，可能尚未安装 IINA（可在设置里改回内置播放器）')
  }

  // preview plays the share's own stream: nothing is restored.
  async function preview(f: FileItem) {
    if (isFolder(f)) {
      enter(f)
      return
    }
    setError('')
    try {
      const got = await apiGet<ShareFileUrls>(
        `share/file?share_id=${encodeURIComponent(shareId)}&file_id=${encodeURIComponent(f.id)}`,
      )
      if (got.direct_url) {
        handToIINA(got.direct_url)
        return
      }
      setPlaying(f)
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
    setNotice('正在把视频转存到「Pack From Shared」…')
    try {
      const started = await apiPost<{ job_id: string }>('videos/prepare', {
        share_id: shareId,
        file_id: f.id,
      })
      const job = await waitForVideoJob(started.job_id, {
        get: (id) => apiGet(`videos/jobs/${encodeURIComponent(id)}`),
      })
      if (!job.stream_url) throw new Error('视频准备失败，请稍后重试')
      // The copy now sits in Pack From Shared, so the tile earns its check
      // without another listing.
      setItems((prev) => (prev ?? []).map((item) => (item.id === f.id ? { ...item, restored: true } : item)))
      if (job.direct_url) {
        handToIINA(job.direct_url)
        return
      }
      setDirectPlay({
        file_id: job.file_id || f.id,
        name: job.name || f.name,
        stream_url: job.stream_url,
      })
      setNotice('')
    } catch (err) {
      setError((err as Error).message)
      setNotice('')
    } finally {
      setBusy('')
    }
  }

  async function toggleFavorite() {
    if (busy === 'favorite') return
    setBusy('favorite')
    setError('')
    try {
      if (favoriteID) {
        await apiDelete(`share-favorites/${favoriteID}`)
        setFavoriteID(0)
        setNotice('已取消收藏')
      } else {
        const created = await apiPost<{ id: number }>('share-favorites', {
          url: favoriteId ? undefined : url,
          share_id: favoriteId ? shareId : undefined,
          title,
          pass_code: passCode,
        })
        setFavoriteID(created.id ?? 0)
        setNotice('已收藏这个分享')
      }
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  // 下载 first restores a copy into Pack From Shared (reusing one already
  // there), then hands the copy to the download queue.
  async function download(list: FileItem[]) {
    const videos = list.filter((f) => !isFolder(f) && isVideo(f))
    if (videos.length === 0) return
    setBusy('download')
    setError('')
    setNotice(`正在转存 ${videos.length} 个视频以便下载…`)
    try {
      const fileIDs: string[] = []
      for (const f of videos) {
        const started = await apiPost<{ job_id: string }>('videos/prepare', {
          share_id: shareId,
          file_id: f.id,
        })
        const job = await waitForVideoJob(started.job_id, {
          get: (id) => apiGet(`videos/jobs/${encodeURIComponent(id)}`),
        })
        if (job.file_id) fileIDs.push(job.file_id)
      }
      const res = await apiPost<{ created: unknown[]; skipped: string[] }>('downloads', {
        file_ids: fileIDs,
      })
      const skipped = res.skipped?.length ? `，跳过 ${res.skipped.length} 个同名文件` : ''
      setNotice(`已加入下载队列：${res.created.length} 个${skipped}。可在「传输」中查看进度。`)
      clearSelection()
    } catch (err) {
      setError(
        err instanceof ApiError && err.code === 'download_dir_unset'
          ? '还没有设置默认下载目录，请到「设置 → 传输」指定一个目录。'
          : (err as Error).message,
      )
      setNotice('')
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
      // The listing was rendered before the save; mark the copies now so the
      // star badge appears without a refresh.
      const added = new Set(list.map((f) => f.id))
      setItems((prev) => (prev ?? []).map((f) => (added.has(f.id) ? { ...f, favorited: true } : f)))
      setNotice(list.length === 1 ? `「${list[0].name}」已加入视频收藏夹` : `${list.length} 个视频已加入视频收藏夹`)
      clearSelection()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  const single = picked.length === 1 ? picked[0] : null
  const pickedVideos = pickedFiles.filter((f) => isVideo(f))
  // 预览 and 完整播放 act on one video at a time; a folder or a multi-select
  // leaves them disabled rather than guessing.
  const singleVideo = single && !isFolder(single) && isVideo(single) ? single : null

  return (
    <section className="file-page share-browser">
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
              disabled={stack.length === 0 || (rootSkipped && stack.length === 1)}
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
                <button
                  type="button"
                  onClick={() => goTo(-1)}
                  disabled={stack.length === 0 || rootSkipped}
                  title="分享根目录"
                >
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
            <button
              className={favoriteID ? 'icon-btn fav-star active' : 'icon-btn fav-star'}
              type="button"
              disabled={!shareId || busy === 'favorite'}
              onClick={() => void toggleFavorite()}
              title={favoriteID ? '取消收藏这个分享' : '收藏这个分享'}
              aria-label={favoriteID ? '取消收藏这个分享' : '收藏这个分享'}
            >
              <StarIcon filled={favoriteID > 0} />
            </button>
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
            <button
              className="btn-icon"
              type="button"
              disabled={pickedVideos.length === 0 || busy === 'download'}
              onClick={() => void download(picked)}
            >
              <DownloadIcon />
              {busy === 'download' ? '准备下载…' : '下载'}
            </button>
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
              <FileTable
                items={shown}
                selected={selected}
                onSelect={select}
                onOpen={openFile}
                badge={(f) => shareBadge(f)}
              />
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
                badge={(f) => shareBadge(f)}
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
