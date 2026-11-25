import { useCallback, useEffect, useRef, useState } from 'react'
import { apiDelete, apiGet, apiPost } from '../api'
import { accountLabel } from '../accounts'
import { useAccounts } from '../AccountContext'
import { formatSize } from '../files'
import { hostAvailable, openFolder } from '../fnosSdk'
import {
  canCancel,
  canPause,
  canResume,
  etaSeconds,
  formatDuration,
  isActive,
  jobName,
  percent,
  speedOf,
  statusText,
  transferred,
  type Sample,
  type TransferJob,
} from '../transfers'

type Kind = 'downloads' | 'uploads'

const POLL_MS = 2000

export default function Transfers() {
  const { byId } = useAccounts()
  const [tab, setTab] = useState<Kind>('downloads')
  const [downloads, setDownloads] = useState<TransferJob[]>([])
  const [uploads, setUploads] = useState<TransferJob[]>([])
  const [speeds, setSpeeds] = useState<Record<string, number>>({})
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')
  const [loading, setLoading] = useState(false)

  const samples = useRef<Map<string, Sample>>(new Map())
  const canOpenFolder = hostAvailable()

  const load = useCallback(async (kind: Kind) => {
    setLoading(true)
    try {
      const page = await apiGet<{ items: TransferJob[] }>(kind)
      const items = page.items ?? []
      const fresh: Record<string, number> = {}
      const now = Date.now()
      for (const job of items) {
        const bytes = transferred(job)
        fresh[job.id] = speedOf(samples.current.get(job.id), { bytes, at: now })
        samples.current.set(job.id, { bytes, at: now })
      }
      setSpeeds((prev) => ({ ...prev, ...fresh }))
      if (kind === 'downloads') setDownloads(items)
      else setUploads(items)
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  // Poll while the page is visible; a hidden tab stops issuing requests.
  useEffect(() => {
    let timer: number | undefined
    let stopped = false
    const tick = () => {
      if (stopped) return
      void load(tab).then(() => {
        if (!stopped) timer = window.setTimeout(tick, POLL_MS)
      })
    }
    void load(tab).then(() => {
      if (!stopped) timer = window.setTimeout(tick, POLL_MS)
    })
    const onVisibility = () => {
      if (stopped) return
      window.clearTimeout(timer)
      timer = undefined
      if (document.visibilityState === 'visible') timer = window.setTimeout(tick, 0)
    }
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      stopped = true
      window.clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [tab, load])

  async function control(kind: Kind, job: TransferJob, action: 'pause' | 'resume' | 'cancel') {
    setBusy(`${job.id}:${action}`)
    try {
      // No ?account= here on purpose: the server resolves the account from
      // the job row itself.
      await apiPost(`${kind}/${job.id}/${action}`, {})
      await load(kind)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function remove(kind: Kind, job: TransferJob) {
    const finishedDownload = kind === 'downloads' && job.status === 'done'
    const deleteFile = finishedDownload && window.confirm(`同时删除已下载的文件「${jobName(job)}」？\n取消则只移除任务记录。`)
    setBusy(`${job.id}:delete`)
    try {
      // Same as control: the job row carries its account, the request names none.
      await apiDelete(`${kind}/${job.id}?delete_file=${deleteFile ? 'true' : 'false'}`)
      samples.current.delete(job.id)
      await load(kind)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  const items = tab === 'downloads' ? downloads : uploads
  const active = items.filter(isActive).length

  return (
    <section className="card transfer-page">
      <div className="transfer-head">
        <div className="tabs">
          <button type="button" className={tab === 'downloads' ? 'tab active' : 'tab'} onClick={() => setTab('downloads')}>
            下载 {downloads.length ? `(${downloads.length})` : ''}
          </button>
          <button type="button" className={tab === 'uploads' ? 'tab active' : 'tab'} onClick={() => setTab('uploads')}>
            上传 {uploads.length ? `(${uploads.length})` : ''}
          </button>
        </div>
        <div className="actions">
          {active > 0 && <span className="muted small">{active} 个进行中</span>}
          <button type="button" onClick={() => void load(tab)} disabled={loading}>
            刷新
          </button>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <p className="muted small">
        {tab === 'downloads'
          ? '在「我的文件」里选中文件后点「下载」创建任务。'
          : '在「我的文件」里点「上传」把飞牛上的文件传到 PikPak。'}
      </p>

      {items.length === 0 && !loading && <p className="muted">还没有{tab === 'downloads' ? '下载' : '上传'}任务。</p>}

      <ul className="transfer-list">
        {items.map((job) => {
          const pct = percent(job)
          const speed = speeds[job.id] ?? 0
          const eta = etaSeconds(job, speed)
          const running = job.status === 'running'
          return (
            <li key={job.id} className={`transfer-row status-${job.status}`}>
              <div className="transfer-line">
                <span className="transfer-name" title={job.dest_path || job.local_path || jobName(job)}>
                  {jobName(job)}
                </span>
                <span className={job.status === 'failed' ? 'error small' : 'muted small'}>{statusText(job)}</span>
              </div>

              <div className="progress" aria-label={`${pct}%`}>
                <div style={{ width: `${pct}%` }} />
              </div>

              <div className="transfer-line small muted">
                <span>
                  {accountLabel(byId, job.account_id)} · {pct}% · {formatSize(transferred(job))}
                  {job.size ? ` / ${formatSize(job.size)}` : ''}
                </span>
                <span>
                  {running && speed > 0 ? `${formatSize(speed)}/s` : ''}
                  {running && eta !== null ? ` · 剩余 ${formatDuration(eta)}` : ''}
                </span>
              </div>

              <div className="transfer-actions">
                {canPause(job) && (
                  <button type="button" disabled={busy === `${job.id}:pause`} onClick={() => void control(tab, job, 'pause')}>
                    暂停
                  </button>
                )}
                {canResume(job) && (
                  <button type="button" disabled={busy === `${job.id}:resume`} onClick={() => void control(tab, job, 'resume')}>
                    {job.status === 'failed' ? '重试' : '继续'}
                  </button>
                )}
                {canCancel(job) && (
                  <button type="button" className="danger" disabled={busy === `${job.id}:cancel`} onClick={() => void control(tab, job, 'cancel')}>
                    取消
                  </button>
                )}
                {tab === 'downloads' && job.status === 'done' && canOpenFolder && (
                  <button type="button" onClick={() => void openFolder(job.dest_dir || '')}>
                    打开所在目录
                  </button>
                )}
                {!isActive(job) && (
                  <button type="button" className="subtle" disabled={busy === `${job.id}:delete`} onClick={() => void remove(tab, job)}>
                    移除记录
                  </button>
                )}
              </div>
            </li>
          )
        })}
      </ul>
    </section>
  )
}
