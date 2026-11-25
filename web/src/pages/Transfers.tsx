import { useCallback, useEffect, useRef, useState } from 'react'
import { apiDelete, apiGet, apiPost } from '../api'
import { accountLabel } from '../accounts'
import { useAccounts } from '../AccountContext'
import AccountSwitcher from '../components/AccountSwitcher'
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
  visibleJobs,
  type Sample,
  type TransferJob,
} from '../transfers'
import { getLang, makeT } from '../i18n'

type Kind = 'downloads' | 'uploads'

const POLL_MS = 2000

export default function Transfers() {
  const lang = getLang()
  const t = makeT(lang)
  const { accounts, byId, scope, setCurrent } = useAccounts()
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
    const deleteFile =
      finishedDownload &&
      window.confirm(t('transfers.confirm_delete_file').replace('{name}', jobName(job)))
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

  // The listing is always the merged one — the fetch names no scope — so the
  // switcher filters on the way in: both lists, and the tab counts with them.
  const shownDownloads = visibleJobs(downloads, scope)
  const shownUploads = visibleJobs(uploads, scope)
  const items = tab === 'downloads' ? shownDownloads : shownUploads
  const active = items.filter(isActive).length

  return (
    <section className="card transfer-page">
      <div className="transfer-head">
        <div className="tabs">
          <button type="button" className={tab === 'downloads' ? 'tab active' : 'tab'} onClick={() => setTab('downloads')}>
            {t('common.download')} {shownDownloads.length ? `(${shownDownloads.length})` : ''}
          </button>
          <button type="button" className={tab === 'uploads' ? 'tab active' : 'tab'} onClick={() => setTab('uploads')}>
            {t('common.upload')} {shownUploads.length ? `(${shownUploads.length})` : ''}
          </button>
        </div>
        <div className="actions">
          <AccountSwitcher accounts={accounts} scope={scope} onChange={setCurrent} />
          {active > 0 && <span className="muted small">{t('tasks.active_count').replace('{n}', String(active))}</span>}
          <button type="button" onClick={() => void load(tab)} disabled={loading}>
            {t('common.refresh')}
          </button>
        </div>
      </div>

      {error && <p className="error">{error}</p>}

      <p className="muted small">
        {tab === 'downloads' ? t('transfers.hint_downloads') : t('transfers.hint_uploads')}
      </p>

      {items.length === 0 && !loading && <p className="muted">{tab === 'downloads' ? t('transfers.empty_downloads') : t('transfers.empty_uploads')}</p>}

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
                <span className={job.status === 'failed' ? 'error small' : 'muted small'}>{statusText(job, lang)}</span>
              </div>

              <div className="progress" aria-label={`${pct}%`}>
                <div style={{ width: `${pct}%` }} />
              </div>

              <div className="transfer-line small muted">
                <span>
                  {accountLabel(byId, job.account_id, undefined, lang)} · {pct}% · {formatSize(transferred(job))}
                  {job.size ? ` / ${formatSize(job.size)}` : ''}
                </span>
                <span>
                  {running && speed > 0 ? `${formatSize(speed)}/s` : ''}
                  {running && eta !== null ? ` ${t('transfers.eta_prefix')} ${formatDuration(eta, lang)}` : ''}
                </span>
              </div>

              <div className="transfer-actions">
                {canPause(job) && (
                  <button type="button" disabled={busy === `${job.id}:pause`} onClick={() => void control(tab, job, 'pause')}>
                    {t('transfers.pause')}
                  </button>
                )}
                {canResume(job) && (
                  <button type="button" disabled={busy === `${job.id}:resume`} onClick={() => void control(tab, job, 'resume')}>
                    {job.status === 'failed' ? t('tasks.retry') : t('transfers.resume')}
                  </button>
                )}
                {canCancel(job) && (
                  <button type="button" className="danger" disabled={busy === `${job.id}:cancel`} onClick={() => void control(tab, job, 'cancel')}>
                    {t('common.cancel')}
                  </button>
                )}
                {tab === 'downloads' && job.status === 'done' && canOpenFolder && (
                  <button type="button" onClick={() => void openFolder(job.dest_dir || '')}>
                    {t('transfers.open_folder')}
                  </button>
                )}
                {!isActive(job) && (
                  <button type="button" className="subtle" disabled={busy === `${job.id}:delete`} onClick={() => void remove(tab, job)}>
                    {t('transfers.remove_record')}
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
