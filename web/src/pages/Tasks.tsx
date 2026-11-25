import { useCallback, useEffect, useState } from 'react'
import { apiDelete, apiGet, apiPost } from '../api'
import { accountLabel } from '../accounts'
import { useAccounts } from '../AccountContext'
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

type AccountError = { account_id: string; account_name?: string; error: string }

const POLL_MS = 3000

export default function Tasks() {
  const { byId } = useAccounts()
  const [items, setItems] = useState<OfflineTask[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [accountErrors, setAccountErrors] = useState<AccountError[]>([])
  const [link, setLink] = useState('')
  const [busy, setBusy] = useState('')
  const [deleteFiles, setDeleteFiles] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const page = await apiGet<{ items?: OfflineTask[]; account_errors?: AccountError[] }>('tasks')
      setItems(page.items ?? [])
      setAccountErrors(page.account_errors ?? [])
      setError('')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

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

  async function create() {
    const text = link.trim()
    if (!text) return
    setBusy('create')
    setError('')
    try {
      // The server picks the account for us (random rotation among the
      // healthy ones), so the success notice names whoever accepted it.
      const res = await apiPost<{ account_id?: string; account_name?: string }>('tasks', {
        link: text,
        parent_id: '',
      })
      setLink('')
      setNotice(`已通过账号 ${accountLabel(byId, res.account_id, res.account_name)} 创建离线任务`)
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
      setNotice('已提交重试')
      await load()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function remove(task: OfflineTask) {
    const extra = deleteFiles ? '，并删除已下载的文件' : ''
    if (!window.confirm(`删除任务「${taskTitle(task)}」${extra}？`)) return
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
    if (!window.confirm('清理所有已结束的任务记录？已下载的文件会保留。')) return
    setBusy('clear')
    try {
      const res = await apiPost<{ deleted?: number }>('tasks/clear', {})
      setNotice(res.deleted ? `已清理 ${res.deleted} 个已结束任务` : '没有需要清理的已结束任务')
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
        <h2>离线任务</h2>
        <div className="actions">
          {active > 0 && <span className="muted small">{active} 个进行中</span>}
          <button type="button" onClick={() => void load()} disabled={loading}>
            刷新
          </button>
          <button type="button" onClick={() => void clearFinished()} disabled={busy === 'clear'}>
            清理已结束
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
          placeholder="粘贴磁力链接或下载链接"
          value={link}
          onChange={(e) => setLink(e.target.value)}
        />
        <button type="submit" disabled={busy === 'create' || !link.trim()}>
          新建任务
        </button>
      </form>

      <label className="muted small task-delete-flag">
        <input type="checkbox" checked={deleteFiles} onChange={(e) => setDeleteFiles(e.target.checked)} />
        删除任务时同时删除已下载的文件
      </label>

      {error && <p className="error">{error}</p>}
      {/* One line per account whose task page could not load: its rows are
          simply missing, so the failure has to say itself. */}
      {accountErrors.length > 0 && (
        <p className="muted small">
          部分账号的任务没有加载：
          {accountErrors
            .map((e) => `账号 ${accountLabel(byId, e.account_id, e.account_name)}：${e.error}`)
            .join('；')}
        </p>
      )}
      {notice && <p className="ok small">{notice}</p>}

      {items.length === 0 && !loading && <p className="muted">还没有离线任务。粘贴链接即可创建。</p>}

      <ul className="transfer-list">
        {items.map((task) => (
          <li key={task.id} className={`transfer-row status-${task.phase === 'PHASE_TYPE_ERROR' ? 'failed' : 'ok'}`}>
            <div className="transfer-line">
              <span className="transfer-name" title={taskTitle(task)}>
                {taskTitle(task)}
              </span>
              <span className={task.phase === 'PHASE_TYPE_ERROR' ? 'error small' : 'muted small'}>
                {taskStatusText(task)}
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
                {accountLabel(byId, task.account_id)}
                {task.type ? ` · ${task.type === 'offline' ? '离线下载' : task.type}` : ''}
              </span>
            </div>

            <div className="transfer-actions">
              {canRetry(task) && (
                <button type="button" disabled={busy === `${task.id}:retry`} onClick={() => void retry(task)}>
                  重试
                </button>
              )}
              <button
                type="button"
                className="danger"
                disabled={busy === `${task.id}:delete`}
                onClick={() => void remove(task)}
              >
                删除
              </button>
            </div>
          </li>
        ))}
      </ul>

      <p className="muted small">任务列表来自 PikPak，页面可见时每 3 秒刷新一次。</p>
    </section>
  )
}
