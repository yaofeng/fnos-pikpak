import { getLang, makeT, type Lang } from './i18n'

export type OfflineTask = {
  id: string
  type?: string
  name?: string
  file_id?: string
  file_name?: string
  file_size?: number
  phase: string
  progress: number
  message?: string
  // account_id stamps merged cross-account listings with the account that
  // owns the task; empty on rows saved before accounts existed.
  account_id?: string
}

const PHASE_PENDING = 'PHASE_TYPE_PENDING'
const PHASE_RUNNING = 'PHASE_TYPE_RUNNING'
const PHASE_COMPLETE = 'PHASE_TYPE_COMPLETE'
const PHASE_ERROR = 'PHASE_TYPE_ERROR'

export function isTaskActive(task: OfflineTask): boolean {
  return task.phase === PHASE_PENDING || task.phase === PHASE_RUNNING
}

export function taskPercent(task: OfflineTask): number {
  if (task.phase === PHASE_COMPLETE) return 100
  return Math.max(0, Math.min(100, task.progress || 0))
}

// taskStatusText names an offline task's phase; copy follows the i18n table
// and the process-wide language unless the caller passes one.
export function taskStatusText(task: OfflineTask, lang?: Lang): string {
  const t = makeT(lang ?? getLang())
  switch (task.phase) {
    case PHASE_PENDING:
      return t('status.queued')
    case PHASE_RUNNING:
      return t('task.running')
    case PHASE_COMPLETE:
      return t('status.done')
    case PHASE_ERROR:
      return task.message ? t('status.failed').replace('{reason}', task.message) : t('status.failed_plain')
    default:
      return task.phase
  }
}

export function taskTitle(task: OfflineTask): string {
  return task.file_name || task.name || task.id
}

export function canRetry(task: OfflineTask): boolean {
  // Only a failed download can be retried; retrying a completed one would just
  // queue a duplicate download.
  return task.phase === PHASE_ERROR
}

// groupByAccount folds merged task rows into one id list per owning account,
// keeping the accounts in first-seen order and the ids within a group in list
// order without duplicates, so a batch delete can loop one request per
// account, each naming its own ?account=. The row type stays loose (an id and
// an optional account id) because merged listings are its only real caller.
export function groupByAccount<T extends { id: string; account_id?: string }>(
  tasks: T[],
): { account_id: string; ids: string[] }[] {
  const groups: { account_id: string; ids: string[] }[] = []
  const byAccount = new Map<string, { account_id: string; ids: string[] }>()
  for (const task of tasks) {
    const account = task.account_id ?? ''
    let group = byAccount.get(account)
    if (!group) {
      group = { account_id: account, ids: [] }
      byAccount.set(account, group)
      groups.push(group)
    }
    if (!group.ids.includes(task.id)) group.ids.push(task.id)
  }
  return groups
}
