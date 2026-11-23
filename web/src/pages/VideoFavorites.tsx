import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import { apiDelete, apiGet, apiPatch, apiPost, basePath, ApiError } from '../api'
import Player from '../components/Player'
import FileGrid from '../components/FileGrid'
import { CheckIcon, FileIcon, GridIcon, ListIcon, RefreshIcon, StarIcon, TrashIcon } from '../components/icons'
import { formatSize, thumbPath, type FileItem } from '../files'
import { iinaURL } from '../iina'
import { useFileSelection } from '../useFileSelection'
import { groupKeyOf, groupVideoFavorites, selectedFileIDs, type VideoFavorite, type VideoFavoriteGroup } from '../videoFavorites'
import { waitForVideoJob } from '../videoPrepare'

type PlayTarget = { file_id: string; name: string; stream_url: string }

// 视频收藏夹 keeps the videos saved from shares, grouped by the person who
// shared them. 预览 plays the share's own stream; 完整播放 restores a copy into
// 我的视频/临时 first, so it survives the share going away.
// toFileItem lets the saved video reuse the file manager's row and tile.
function toFileItem(f: VideoFavorite): FileItem {
  return {
    id: f.file_id,
    parent_id: '',
    name: f.display_name || f.name,
    kind: 'drive#file',
    size: f.size,
    restored: f.restored,
  }
}

export default function VideoFavorites() {
  const [items, setItems] = useState<VideoFavorite[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState('')
  const [playing, setPlaying] = useState<PlayTarget | null>(null)
  // 我的文件 opens in the large-icon view; 视频收藏夹 does the same.
  const [view, setView] = useState<'list' | 'grid'>('grid')
  // A tile being dragged toward another group; drop files it under that
  // group's local label.
  const [dragging, setDragging] = useState<VideoFavorite | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const body = await apiGet<{ items: VideoFavorite[] }>('video-favorites')
      setItems(body.items ?? [])
    } catch (err) {
      setError((err as Error).message)
      setItems([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const groups = useMemo(() => groupVideoFavorites(items ?? []), [items])
  const order = useMemo(() => (items ?? []).map((f) => String(f.id)), [items])
  const { selected, select, clear } = useFileSelection(order)
  const picked = (items ?? []).filter((f) => selected.has(String(f.id)))
  const one = picked.length === 1 ? picked[0] : null
  // The grid keys its tiles by file id; the page selects favourites by row id.
  const gridSelected = selectedFileIDs(picked)

  function patchLocal(id: number, displayName: string) {
    setItems((prev) => (prev ?? []).map((f) => (f.id === id ? { ...f, display_name: displayName } : f)))
  }

  function playInline(target: PlayTarget) {
    setNotice('')
    setPlaying(target)
  }

  // handToIINA opens the external player with the original link the server
  // supplied for 播放器 = IINA; nothing goes through this app.
  function handToIINA(directURL: string) {
    window.location.href = iinaURL(directURL)
    setNotice('已交给 IINA 打开；若没有打开，可能尚未安装 IINA（可在设置里改回内置播放器）')
  }

  async function preview(f: VideoFavorite) {
    setBusy('preview')
    setError('')
    setNotice('')
    try {
      const got = await apiPost<{ stream_url: string; direct_url?: string }>('videos/preview', {
        video_favorite_id: f.id,
      })
      if (got.direct_url) {
        handToIINA(got.direct_url)
        return
      }
      playInline({ file_id: f.file_id, name: f.display_name || f.name, stream_url: got.stream_url })
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function fullPlay(f: VideoFavorite) {
    setBusy('prepare')
    setError('')
    setNotice(f.restored ? '正在打开 Pack From Shared 中已转存的视频…' : '正在把视频转存到「Pack From Shared」…')
    try {
      const started = await apiPost<{ job_id: string }>('videos/prepare', { video_favorite_id: f.id })
      const job = await waitForVideoJob(started.job_id, {
        get: (id) => apiGet(`videos/jobs/${encodeURIComponent(id)}`),
      })
      if (!job.stream_url) throw new Error('视频准备失败，请稍后重试')
      if (job.direct_url) {
        handToIINA(job.direct_url)
        void load()
        return
      }
      playInline({
        file_id: job.file_id || f.file_id,
        name: job.name || f.display_name || f.name,
        stream_url: job.stream_url,
      })
      // The prepare recorded the restored copy, so the tile now earns its check badge.
      void load()
    } catch (err) {
      setError((err as Error).message)
      setNotice('')
    } finally {
      setBusy('')
    }
  }

  // 下载 restores the video into Pack From Shared first (reusing an existing
  // copy), then queues that copy for download.
  async function download(list: VideoFavorite[]) {
    if (list.length === 0) return
    setBusy('download')
    setError('')
    setNotice(`正在转存 ${list.length} 个视频以便下载…`)
    try {
      const fileIDs: string[] = []
      for (const f of list) {
        const started = await apiPost<{ job_id: string }>('videos/prepare', { video_favorite_id: f.id })
        const job = await waitForVideoJob(started.job_id, {
          get: (id) => apiGet(`videos/jobs/${encodeURIComponent(id)}`),
        })
        if (job.file_id) fileIDs.push(job.file_id)
      }
      const res = await apiPost<{ created: unknown[]; skipped: string[] }>('downloads', { file_ids: fileIDs })
      const skipped = res.skipped?.length ? `，跳过 ${res.skipped.length} 个同名文件` : ''
      setNotice(`已加入下载队列：${res.created.length} 个${skipped}。可在「传输」中查看进度。`)
      clear()
      void load()
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

  // Groups are local: renaming one labels every video in it, and dragging a
  // video between groups writes its label. PikPak knows nothing about either.
  async function renameGroup(group: VideoFavoriteGroup) {
    const next = window.prompt('分组名称（仅本机显示）', group.label)
    if (next === null) return
    const label = next.trim()
    if (!label || label === group.label) return
    setBusy('group')
    setError('')
    try {
      await apiPatch('video-favorites/groups', { from_key: group.key, label })
      await load()
      setNotice(`分组已改为「${label}」`)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function dropOnGroup(target: VideoFavoriteGroup) {
    const fav = dragging
    setDragging(null)
    if (!fav || groupKeyOf(fav) === target.key) return
    setBusy('group')
    setError('')
    try {
      await apiPatch(`video-favorites/${fav.id}`, { group_label: target.label })
      setItems((prev) =>
        (prev ?? []).map((f) => (f.id === fav.id ? { ...f, group_label: target.label } : f)),
      )
      setNotice(`「${fav.display_name || fav.name}」已移到「${target.label}」`)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  // Renaming is local only: it changes how this list shows the video, never
  // the file in the share. An empty prompt restores the original name.
  async function rename(f: VideoFavorite) {
    const next = window.prompt('新名称（留空恢复原名）', f.display_name || f.name)
    if (next === null) return
    setBusy('rename')
    setError('')
    try {
      await apiPatch(`video-favorites/${f.id}`, { name: next.trim() })
      patchLocal(f.id, next.trim())
      setNotice(next.trim() ? '已重命名（仅本机显示）' : '已恢复原名')
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function remove(list: VideoFavorite[]) {
    if (list.length === 0) return
    const label = list.length === 1 ? `「${list[0].display_name || list[0].name}」` : `${list.length} 个视频`
    if (!window.confirm(`从视频收藏夹移除${label}？分享本身不会受影响。`)) return
    setBusy('remove')
    setError('')
    try {
      for (const f of list) await apiDelete(`video-favorites/${f.id}`)
      setItems((prev) => (prev ?? []).filter((f) => !list.some((gone) => gone.id === f.id)))
      clear()
      setNotice(`已移除${label}`)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  return (
    <section className="file-page">
      <div className="actions fm-actions">
        <button type="button" disabled={!one || busy !== ''} onClick={() => one && void preview(one)}>
          <StarIcon /> 预览
        </button>
        <button
          type="button"
          disabled={!one || busy !== ''}
          onClick={() => one && void fullPlay(one)}
          title="转存到「我的视频/临时」后播放，空间不足时自动清理最早的视频"
        >
          {busy === 'prepare' ? '准备中…' : '完整播放'}
        </button>
        <button
          type="button"
          disabled={picked.length === 0 || busy !== ''}
          onClick={() => void download(picked)}
          title="先转存到「Pack From Shared」，再加入下载队列"
        >
          {busy === 'download' ? '准备下载…' : '下载'}
        </button>
        <button type="button" disabled={!one || busy !== ''} onClick={() => one && void rename(one)}>
          重命名
        </button>
        <button type="button" className="danger" disabled={picked.length === 0 || busy !== ''} onClick={() => void remove(picked)}>
          <TrashIcon size={14} /> 移除收藏
        </button>
        <button type="button" disabled={loading} onClick={() => void load()}>
          <RefreshIcon /> 刷新
        </button>

        <span className="fm-spacer" />

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

      <div className="file-body">
        {error && <p className="error">{error}</p>}

        {items === null || (loading && items.length === 0) ? (
        <p className="muted">加载中…</p>
      ) : items.length === 0 ? (
        <div className="empty-state">
          <StarIcon size={28} />
          <p>视频收藏夹还是空的。</p>
          <p className="muted small">在分享里选中视频后点「收藏视频」，之后可以在这里直接预览或完整播放。</p>
        </div>
      ) : view === 'grid' ? (
        groups.map((group) => (
          <section
            className="vgroup"
            key={group.key}
            onDragOver={(e) => e.preventDefault()}
            onDrop={(e) => {
              e.preventDefault()
              void dropOnGroup(group)
            }}
          >
            <h3 className="vgroup-head">
              {group.label}
              <span className="muted small">{group.items.length} 个视频</span>
              <button
                type="button"
                className="link vgroup-rename"
                disabled={busy !== ''}
                title="重命名这个分组（仅本机显示）"
                onClick={() => void renameGroup(group)}
              >
                重命名
              </button>
            </h3>
            <FileGrid
              items={group.items.map(toFileItem)}
              selected={gridSelected}
              thumbSrc={(f) => {
                const fav = group.items.find((g) => g.file_id === f.id)
                return fav ? `${basePath}/api/${thumbPath(fav.file_id, fav.share_id)}` : ''
              }}
              onSelect={(index, e) => select(order.indexOf(String(group.items[index].id)), e)}
              onOpen={(f) => {
                // A double click is 完整播放: the copy in Pack From Shared,
                // restored only if it is not there yet.
                const fav = group.items.find((g) => g.file_id === f.id)
                if (fav) void fullPlay(fav)
              }}
              onMenu={() => undefined}
              onDragStart={(f) => {
                const fav = group.items.find((g) => g.file_id === f.id)
                if (fav) setDragging(fav)
              }}
              onDropOn={() => undefined}
              badge={(f) =>
                f.restored
                  ? {
                      icon: <CheckIcon size={13} />,
                      title: '已转存到 Pack From Shared',
                      kind: 'restored' as const,
                    }
                  : null
              }
            />
          </section>
        ))
      ) : (
        // The list view is the 我的文件 table: a group header row, then the
        // videos of that group with the same glyph, size and time columns.
        <table className="file-table vfav-table">
          <thead>
            <tr>
              <th className="col-open" />
              <th>文件名</th>
              <th className="col-size">大小</th>
              <th className="col-time">收藏时间</th>
            </tr>
          </thead>
          <tbody>
            {groups.map((group) => (
              <Fragment key={group.key}>
                <tr
                  className="vgroup-row"
                  onDragOver={(e) => e.preventDefault()}
                  onDrop={(e) => {
                    e.preventDefault()
                    void dropOnGroup(group)
                  }}
                >
                  <td colSpan={4}>
                    {group.label}
                    <span className="muted small"> · {group.items.length} 个视频</span>
                    <button
                      type="button"
                      className="link vgroup-rename"
                      disabled={busy !== ''}
                      title="重命名这个分组（仅本机显示）"
                      onClick={() => void renameGroup(group)}
                    >
                      重命名
                    </button>
                  </td>
                </tr>
                {group.items.map((f) => (
                  <tr
                    key={f.id}
                    className={selected.has(String(f.id)) ? 'selected' : ''}
                    onClick={(e) => select(order.indexOf(String(f.id)), e)}
                    onDoubleClick={() => void fullPlay(f)}
                  >
                    <td className="col-open" />
                    <td>
                      <span className="shine-name">
                        {f.restored && (
                          <span className="list-badge restored" title="已转存到 Pack From Shared">
                            <CheckIcon size={14} />
                          </span>
                        )}
                        <FileIcon file={toFileItem(f)} size={20} />
                        <span title={f.name}>{f.display_name || f.name}</span>
                      </span>
                      {f.display_name && <span className="muted small">（{f.name}）</span>}
                    </td>
                    <td className="col-size muted small">{formatSize(f.size)}</td>
                    <td className="col-time muted small">
                      {f.created_at ? new Date(f.created_at).toLocaleString() : '—'}
                    </td>
                  </tr>
                ))}
              </Fragment>
            ))}
          </tbody>
        </table>
      )}
      </div>

      <div className="status-bar">
        <span>
          {picked.length > 0
            ? `选中 ${picked.length} 项（共 ${(items ?? []).length} 项）`
            : `共 ${(items ?? []).length} 项`}
        </span>
        {notice && <span className="ok status-note">{notice}</span>}
      </div>

      {playing && (
        <Player
          fileId={playing.file_id}
          name={playing.name}
          streamURL={playing.stream_url}
          onClose={() => setPlaying(null)}
        />
      )}
    </section>
  )
}
