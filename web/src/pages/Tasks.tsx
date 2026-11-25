import { useCallback, useEffect, useRef, useState } from 'react'
import { apiDelete, apiGet, apiPost } from '../api'
import { accountLabel } from '../accounts'
import { useAccounts } from '../AccountContext'
import AccountSwitcher from '../components/AccountSwitcher'
import { formatSize } from '../files'
import {
  canRetry,
  groupByAccount,
  isTaskActive,
  taskPercent,
  taskStatusText,
  taskTitle,
  type OfflineTask,
} from '../tasks'
import { getLang, makeT } from '../i18n'

type AccountError = { account_id: string; account_name?: string; error: string }

const POLL_MS = 3000

// withScope pins a request to the picked account; 'all' keeps the bare path,
// so a merged create goes through the server's account rotation and a merged
// list/clear acts on every account.
function withScope(path: string, scope: string): string {
  return scope === 'all' ? path : `${path}?account=${encodeURIComponent(scope)}`
}

export default function Tasks() {
  const lang = getLang()
  const t = makeT(lang)
  const { accounts, byId, scope, setCurrent } = useAccounts()
  const [items, setItems] = useState<OfflineTask[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [accountErrors, setAccountErrors] = useState<AccountError[]>([])
  const [link, setLink] = useState('')
  const [busy, setBusy] = useState('')
  const [deleteFiles, setDeleteFiles] = useState(false)
  // The scope the current listing came from; the switch effect below uses
  // the delta to own the render in which the scope changed.
  const prevScope = useRef(scope)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const page = await apiGet<{ items?: OfflineTask[]; account_errors?: AccountError[] }>(
        withScope('tasks', scope),
      )
      setItems(page.items ?? [])
      setAccountErrors(page.account_errors ?? [])
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [scope])

  // Poll every three seconds while the page is visible; a hidden tab stops.
  useEffect(() => {
    let timer: number | undefined
    let stopped = false
    const tick = () => {
      if (stopped) return
      void load().then(() => {
        if (!stopped) timer = window.setTimeout(tick, POLL_MS)
      })
    }
    timer = window.setTimeout(tick, 0)
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
  }, [load])

  // Switching scopes changes whose tasks everything is: clear what is on
  // screen so the old account's rows never linger. This render's poll effect
  // restart (load depends on the scope) issues the new scope's first fetch,
  // so no second request goes out here.
  useEffect(() => {
    if (prevScope.current === scope) return
    prevScope.current = scope
    setItems([])
    setAccountErrors([])
  }, [scope])

  async function create() {
    const text = link.trim()
    if (!text) return
    setBusy('create')
    setError('')
    try {
      // A concrete scope pins the new task to that account; 'all' leaves the
      // path bare, so the server picks the account for us (random rotation
      // among the healthy ones) and the success notice names whoever accepted
      // it.
      const res = await apiPost<{ account_id?: string; account_name?: string }>(withScope('tasks', scope), {
        link: text,
        parent_id: '',
      })
      setLink('')
      setNotice(
        t('tasks.created_via')
          .replace('{name}', accountLabel(byId, res.account_id, res.account_name, lang)),
      )
      await load()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function retry(task: OfflineTask) {
    setBusy(`${task.id}:retry`)
    try {
      // The merged listing knows which account owns the task; retrying it
      // anywhere else would be a 404 on that account.
      await apiPost(`tasks/${task.id}/retry?account=${encodeURIComponent(task.account_id ?? '')}`, {})
      setNotice(t('tasks.retry_submitted'))
      await load()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function remove(task: OfflineTask) {
    const extra = deleteFiles ? t('tasks.confirm_delete_extra') : ''
    if (!window.confirm(t('tasks.confirm_delete').replace('{name}', taskTitle(task)).replace('{extra}', extra))) return
    setBusy(`${task.id}:delete`)
    try {
      // groupByAccount keeps one request per owning account, each naming its
      // own ?account=; a single task is the one-group case.
      for (const group of groupByAccount([task])) {
        await apiDelete(
          `tasks?ids=${encodeURIComponent(group.ids.join(','))}` +
            `&delete_files=${deleteFiles ? 'true' : 'false'}` +
            `&account=${encodeURIComponent(group.account_id)}`,
        )
      }
      await load()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function clearFinished() {
    if (!window.confirm(t('tasks.confirm_clear'))) return
    setBusy('clear')
    try {
      // Same shape as the list: a concrete scope clears only that account's
      // finished tasks, 'all' keeps the bare path for every account.
      const res = await apiPost<{ deleted?: number }>(withScope('tasks/clear', scope), {})
      setNotice(res.deleted ? t('tasks.cleared').replace('{n}', String(res.deleted)) : t('tasks.nothing_to_clear'))
      await load()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  const active = items.filter(isTaskActive).length

  return (
    <section className="card task-page">
      <div className="transfer-head">
        <h2>{t('tasks.title')}</h2>
        <div className="actions">
          <AccountSwitcher accounts={accounts} scope={scope} onChange={setCurrent} />
          {active > 0 && <span className="muted small">{t('tasks.active_count').replace('{n}', String(active))}</span>}
          <button type="button" onClick={() => void load()} disabled={loading}>
            {t('common.refresh')}
          </button>
          <button type="button" onClick={() => void clearFinished()} disabled={busy === 'clear'}>
            {t('tasks.clear_finished')}
          </button>
        </div>
      </div>

      <form
        className="task-form"
        onSubmit={(e) => {
          e.preventDefault()
          void create()
        }}
      >
        <input
          type="text"
          placeholder={t('tasks.link_placeholder')}
          value={link}
          onChange={(e) => setLink(e.target.value)}
        />
        <button type="submit" disabled={busy === 'create' || !link.trim()}>
          {t('tasks.create')}
        </button>
      </form>

      <label className="muted small task-delete-flag">
        <input type="checkbox" checked={deleteFiles} onChange={(e) => setDeleteFiles(e.target.checked)} />
        {t('tasks.delete_files_flag')}
      </label>

      {error && <p className="error">{error}</p>}
      {/* One line per account whose task page could not load: its rows are
          simply missing, so the failure has to say itself. */}
      {accountErrors.length > 0 && (
        <p className="muted small">
          {t('tasks.account_errors')}
          {accountErrors
            .map((e) =>
              t('recent.account_error_line')
                .replace('{name}', accountLabel(byId, e.account_id, e.account_name, lang))
                .replace('{error}', e.error),
            )
            .join(lang === 'en' ? '; ' : '；')}
        </p>
      )}
      {notice && <p className="ok small">{notice}</p>}

      {items.length === 0 && !loading && <p className="muted">{t('tasks.empty')}</p>}

      <ul className="transfer-list">
        {items.map((task) => (
          <li key={task.id} className={`transfer-row status-${task.phase === 'PHASE_TYPE_ERROR' ? 'failed' : 'ok'}`}>
            <div className="transfer-line">
              <span className="transfer-name" title={taskTitle(task)}>
                {taskTitle(task)}
              </span>
              <span className={task.phase === 'PHASE_TYPE_ERROR' ? 'error small' : 'muted small'}>
                {taskStatusText(task, lang)}
              </span>
            </div>

            <div className="progress" aria-label={`${taskPercent(task)}%`}>
              <div style={{ width: `${taskPercent(task)}%` }} />
            </div>

            <div className="transfer-line small muted">
              <span>
                {taskPercent(task)}%{task.file_size ? ` · ${formatSize(task.file_size)}` : ''}
              </span>
              <span>
                {accountLabel(byId, task.account_id, undefined, lang)}
                {task.type ? ` · ${task.type === 'offline' ? t('tasks.type_offline') : task.type}` : ''}
              </span>
            </div>

            <div className="transfer-actions">
              {canRetry(task) && (
                <button type="button" disabled={busy === `${task.id}:retry`} onClick={() => void retry(task)}>
                  {t('tasks.retry')}
                </button>
              )}
              <button
                type="button"
                className="danger"
                disabled={busy === `${task.id}:delete`}
                onClick={() => void remove(task)}
              >
                {t('common.delete')}
              </button>
            </div>
          </li>
        ))}
      </ul>

      <p className="muted small">{t('tasks.hint')}</p>
    </section>
  )
}
