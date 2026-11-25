import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import StatusBar from '../components/StatusBar'
import { apiDelete, apiGet, apiPatch, apiPost, basePath } from '../api'
import { dateText } from '../dates'
import { downloadErrorMessage, queueDownloads, type DownloadItem } from '../downloads'
import Player from '../components/Player'
import FileGrid from '../components/FileGrid'
import { CheckIcon, FileIcon, GridIcon, ListIcon, RefreshIcon, StarIcon, TrashIcon } from '../components/icons'
import { formatSize, thumbPath, type FileItem } from '../files'
import { iinaURL } from '../iina'
import { useFileSelection } from '../useFileSelection'
import {
  groupKeyOf,
  groupVideoFavorites,
  selectedFileIDs,
  unknownShareKey,
  type VideoFavorite,
  type VideoFavoriteGroup,
} from '../videoFavorites'
import { waitForVideoJob } from '../videoPrepare'
import { getLang, makeT } from '../i18n'

type PlayTarget = { file_id: string; name: string; stream_url: string }

// 视频收藏夹 keeps the videos saved from shares, grouped by the person who
// shared them. 预览 plays the share's own stream; 完整播放 restores a copy into
// Pack From Shared first, so it survives the share going away.
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

// openIINA navigates the whole window to the external player from module
// scope — an assignment the render-purity rule should not inspect.
function openIINA(directURL: string) {
  window.location.href = iinaURL(directURL)
}

export default function VideoFavorites() {
  const lang = getLang()
  const t = makeT(lang)
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
  function handToIINA(directURL: string, notice: string) {
    openIINA(directURL)
    setNotice(notice)
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
        handToIINA(got.direct_url, t('vfav.iina_hint'))
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
    setNotice(f.restored ? t('vfav.opening_restored') : t('vfav.restoring'))
    try {
      const started = await apiPost<{ job_id: string }>('videos/prepare', { video_favorite_id: f.id })
      const job = await waitForVideoJob(started.job_id, {
        get: (id) => apiGet(`videos/jobs/${encodeURIComponent(id)}`),
      })
      if (!job.stream_url) throw new Error(t('vfav.prepare_no_stream'))
      if (job.direct_url) {
        handToIINA(job.direct_url, t('vfav.iina_hint'))
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
    setNotice(t('vfav.restoring_for_download').replace('{n}', String(list.length)))
    try {
      // Each prepare job reports the account its restored copy lives on;
      // queue the download for exactly that account, so the job never
      // reaches for a default account the copy is not on.
      const copies: DownloadItem[] = []
      for (const f of list) {
        const started = await apiPost<{ job_id: string }>('videos/prepare', { video_favorite_id: f.id })
        const job = await waitForVideoJob(started.job_id, {
          get: (id) => apiGet(`videos/jobs/${encodeURIComponent(id)}`),
        })
        if (job.file_id) copies.push({ file_id: job.file_id, account_id: job.account_id ?? '' })
      }
      setNotice(await queueDownloads(copies, lang))
      clear()
      void load()
    } catch (err) {
      setError(downloadErrorMessage(err, lang))
      setNotice('')
    } finally {
      setBusy('')
    }
  }

  // Groups are local: renaming one labels every video in it, and dragging a
  // video between groups writes its label. PikPak knows nothing about either.
  async function renameGroup(group: VideoFavoriteGroup) {
    const next = window.prompt(t('vfav.group_rename_prompt'), group.label)
    if (next === null) return
    const label = next.trim()
    if (!label || label === group.label) return
    setBusy('group')
    setError('')
    try {
      await apiPatch('video-favorites/groups', { from_key: group.key, label })
      await load()
      setNotice(t('vfav.group_renamed').replace('{label}', label))
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
      setNotice(
        t('vfav.moved_to')
          .replace('{name}', fav.display_name || fav.name)
          .replace('{target}', target.label),
      )
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  // Renaming is local only: it changes how this list shows the video, never
  // the file in the share. An empty prompt restores the original name.
  async function rename(f: VideoFavorite) {
    const next = window.prompt(t('favorites.rename_prompt'), f.display_name || f.name)
    if (next === null) return
    setBusy('rename')
    setError('')
    try {
      await apiPatch(`video-favorites/${f.id}`, { name: next.trim() })
      patchLocal(f.id, next.trim())
      setNotice(next.trim() ? t('favorites.renamed') : t('favorites.name_restored'))
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function remove(list: VideoFavorite[]) {
    if (list.length === 0) return
    const label =
      list.length === 1
        ? t('favorites.one_label').replace('{name}', list[0].display_name || list[0].name)
        : t('vfav.count_label').replace('{n}', String(list.length))
    if (!window.confirm(t('vfav.confirm_remove').replace('{label}', label))) return
    setBusy('remove')
    setError('')
    try {
      for (const f of list) await apiDelete(`video-favorites/${f.id}`)
      setItems((prev) => (prev ?? []).filter((f) => !list.some((gone) => gone.id === f.id)))
      clear()
      setNotice(t('favorites.removed').replace('{label}', label))
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
          <StarIcon /> {t('vfav.preview')}
        </button>
        <button
          type="button"
          disabled={!one || busy !== ''}
          onClick={() => one && void fullPlay(one)}
          title={t('vfav.fullplay_title')}
        >
          {busy === 'prepare' ? t('vfav.preparing') : t('vfav.fullplay')}
        </button>
        <button
          type="button"
          disabled={picked.length === 0 || busy !== ''}
          onClick={() => void download(picked)}
          title={t('vfav.download_title')}
        >
          {busy === 'download' ? t('vfav.preparing_download') : t('common.download')}
        </button>
        <button type="button" disabled={!one || busy !== ''} onClick={() => one && void rename(one)}>
          {t('account.rename')}
        </button>
        <button type="button" className="danger" disabled={picked.length === 0 || busy !== ''} onClick={() => void remove(picked)}>
          <TrashIcon size={14} /> {t('vfav.remove_favorite')}
        </button>
        <button type="button" disabled={loading} onClick={() => void load()}>
          <RefreshIcon /> {t('common.refresh')}
        </button>

        <span className="fm-spacer" />

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

      <div className="file-body">
        {error && <p className="error">{error}</p>}

        {items === null || (loading && items.length === 0) ? (
        <p className="muted">{t('common.loading')}</p>
      ) : items.length === 0 ? (
        <div className="empty-state">
          <StarIcon size={28} />
          <p>{t('vfav.empty')}</p>
          <p className="muted small">{t('vfav.empty_hint')}</p>
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
              {group.label === unknownShareKey ? t('videoFavorites.unknown_share') : group.label}
              <span className="muted small">{t('vfav.group_count').replace('{n}', String(group.items.length))}</span>
              <button
                type="button"
                className="link vgroup-rename"
                disabled={busy !== ''}
                title={t('vfav.group_rename_title')}
                onClick={() => void renameGroup(group)}
              >
                {t('account.rename')}
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
                      title: t('vfav.restored_badge'),
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
              <th>{t('files.th_name')}</th>
              <th className="col-size">{t('files.th_size')}</th>
              <th className="col-time">{t('vfav.col_favorited')}</th>
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
                    {group.label === unknownShareKey ? t('videoFavorites.unknown_share') : group.label}
                    <span className="muted small"> · {t('vfav.group_count').replace('{n}', String(group.items.length))}</span>
                    <button
                      type="button"
                      className="link vgroup-rename"
                      disabled={busy !== ''}
                      title={t('vfav.group_rename_title')}
                      onClick={() => void renameGroup(group)}
                    >
                      {t('account.rename')}
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
                          <span className="list-badge restored" title={t('vfav.restored_badge')}>
                            <CheckIcon size={14} />
                          </span>
                        )}
                        <FileIcon file={toFileItem(f)} size={20} />
                        <span title={f.name}>{f.display_name || f.name}</span>
                      </span>
                      {f.display_name && (
                        <span className="muted small">
                          ({f.name})
                        </span>
                      )}
                    </td>
                    <td className="col-size muted small">{formatSize(f.size)}</td>
                    <td className="col-time muted small">
                      {dateText(f.created_at)}
                    </td>
                  </tr>
                ))}
              </Fragment>
            ))}
          </tbody>
        </table>
      )}
      </div>

      <StatusBar
        selected={picked.length}
        total={(items ?? []).length}
        notice={notice}
      />

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
