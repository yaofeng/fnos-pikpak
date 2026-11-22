import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import { apiDelete, apiGet, apiPatch, apiPost, basePath } from '../api'
import Player from '../components/Player'
import { FileIcon, RefreshIcon, StarIcon, TrashIcon } from '../components/icons'
import { formatSize } from '../files'
import { iinaURL, isMacOS } from '../iina'
import { useFileSelection } from '../useFileSelection'
import { groupVideoFavorites, type VideoFavorite } from '../videoFavorites'
import { waitForVideoJob } from '../videoPrepare'

type PlayTarget = { file_id: string; name: string; stream_url: string }

// 视频收藏夹 keeps the videos saved from shares, grouped by the person who
// shared them. 预览 plays the share's own stream; 完整播放 restores a copy into
// 我的视频/临时 first, so it survives the share going away.
export default function VideoFavorites() {
  const [items, setItems] = useState<VideoFavorite[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState('')
  const [playing, setPlaying] = useState<PlayTarget | null>(null)
  // An external protocol needs a fresh user gesture after the async prepare,
  // so macOS gets an explicit 用 IINA 播放 button.
  const [iinaPending, setIinaPending] = useState<{ url: string; name: string } | null>(null)

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

  function patchLocal(id: number, displayName: string) {
    setItems((prev) => (prev ?? []).map((f) => (f.id === id ? { ...f, display_name: displayName } : f)))
  }

  function absolute(path: string): string {
    return window.location.origin + basePath + path
  }

  function playInline(target: PlayTarget) {
    setIinaPending(null)
    setPlaying(target)
  }

  async function preview(f: VideoFavorite) {
    setBusy('preview')
    setError('')
    setNotice('')
    setIinaPending(null)
    try {
      const got = await apiPost<{ stream_url: string }>('videos/preview', { video_favorite_id: f.id })
      if (isMacOS(navigator.userAgent)) {
        setIinaPending({ url: absolute(got.stream_url), name: f.display_name || f.name })
        setNotice('点「用 IINA 播放」开始预览')
      } else {
        playInline({ file_id: f.file_id, name: f.display_name || f.name, stream_url: got.stream_url })
      }
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function fullPlay(f: VideoFavorite) {
    setBusy('prepare')
    setError('')
    setNotice('正在把视频转存到「我的视频/临时」…')
    setIinaPending(null)
    try {
      const started = await apiPost<{ job_id: string }>('videos/prepare', { video_favorite_id: f.id })
      const job = await waitForVideoJob(started.job_id, {
        get: (id) => apiGet(`videos/jobs/${encodeURIComponent(id)}`),
      })
      if (!job.stream_url) throw new Error('视频准备失败，请稍后重试')
      if (isMacOS(navigator.userAgent)) {
        setIinaPending({ url: absolute(job.stream_url), name: job.name || f.display_name || f.name })
        setNotice('已准备好，点「用 IINA 播放」开始播放')
      } else {
        playInline({
          file_id: job.file_id || f.file_id,
          name: job.name || f.display_name || f.name,
          stream_url: job.stream_url,
        })
        setNotice('')
      }
    } catch (err) {
      setError((err as Error).message)
      setNotice('')
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
    <section className="page-stack">
      <div className="actions">
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
        <button type="button" disabled={!one || busy !== ''} onClick={() => one && void rename(one)}>
          重命名
        </button>
        <button type="button" className="danger" disabled={picked.length === 0 || busy !== ''} onClick={() => void remove(picked)}>
          <TrashIcon size={14} /> 移除收藏
        </button>
        <button type="button" disabled={loading} onClick={() => void load()}>
          <RefreshIcon /> 刷新
        </button>
      </div>

      {notice && <p className="ok small">{notice}</p>}

      {iinaPending && (
        <p className="share-iina">
          <button type="button" className="primary" onClick={() => { window.location.href = iinaURL(iinaPending.url) }}>
            用 IINA 播放
          </button>
          <span className="muted small">{iinaPending.name}</span>
        </p>
      )}

      {error && <p className="error">{error}</p>}

      {items === null || (loading && items.length === 0) ? (
        <p className="muted">加载中…</p>
      ) : items.length === 0 ? (
        <div className="empty-state">
          <StarIcon size={28} />
          <p>视频收藏夹还是空的。</p>
          <p className="muted small">在分享里选中视频后点「收藏视频」，之后可以在这里直接预览或完整播放。</p>
        </div>
      ) : (
        // One list in the 我的文件 shape: a group header row, then the videos
        // of that group with the same glyph, size and time columns.
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
                <tr className="vgroup-row">
                  <td colSpan={4}>
                    {group.label}
                    <span className="muted small"> · {group.items.length} 个视频</span>
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
                        <FileIcon file={{ id: f.file_id, parent_id: '', name: f.name, kind: 'drive#file', size: f.size }} size={20} />
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
