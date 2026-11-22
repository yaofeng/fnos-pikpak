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

export function taskStatusText(task: OfflineTask): string {
  switch (task.phase) {
    case PHASE_PENDING:
      return '排队中'
    case PHASE_RUNNING:
      return '下载中'
    case PHASE_COMPLETE:
      return '已完成'
    case PHASE_ERROR:
      return task.message ? `失败：${task.message}` : '失败'
    default:
      return task.phase
  }
}

export function taskTitle(task: OfflineTask): string {
  return task.file_name || task.name || task.id
}

export function canRetry(task: OfflineTask): boolean {
  return !isTaskActive(task)
}
