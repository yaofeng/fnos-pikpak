type TransferStatus =
  | 'queued'
  | 'running'
  | 'paused'
  | 'done'
  | 'failed'
  | 'canceled'
  | 'skipped'

// TransferJob is the JSON shape of both download and upload jobs; the fields a
// given kind does not use are simply absent.
export type TransferJob = {
  id: string
  status: TransferStatus | string
  size: number
  downloaded?: number
  uploaded?: number
  file_name?: string
  dest_dir?: string
  dest_path?: string
  local_path?: string
  remote_name?: string
  parent_id?: string
  error_code?: string
  error_message?: string
  created_at?: string
  updated_at?: string
  // account_id names the PikPak account the job downloads or uploads under;
  // empty on rows queued before accounts existed.
  account_id?: string
}

export type Sample = { bytes: number; at: number }

// Progress is all the byte counters need; helpers accept partial jobs so the
// page can pass whatever the list delivered.
export type Progress = { size: number; downloaded?: number; uploaded?: number }

export function transferred(job: Progress): number {
  // Downloads fill `downloaded`, uploads fill `uploaded`; the other is absent or 0.
  return job.downloaded || job.uploaded || 0
}

export function percent(job: Progress): number {
  if (!job.size || job.size <= 0) return 0
  const pct = Math.floor((transferred(job) / job.size) * 100)
  return Math.max(0, Math.min(100, pct))
}

// speedOf compares two progress samples. A counter that went backwards (a job
// restarted) or a clock that did not move yields 0 instead of a bogus rate.
export function speedOf(prev: Sample | undefined, cur: Sample): number {
  if (!prev) return 0
  const dt = (cur.at - prev.at) / 1000
  const delta = cur.bytes - prev.bytes
  if (dt <= 0 || delta <= 0) return 0
  return delta / dt
}

export function etaSeconds(job: Progress, speed: number): number | null {
  if (speed <= 0) return null
  const remaining = Math.max(0, (job.size ?? 0) - transferred(job))
  if (remaining === 0) return 0
  return Math.ceil(remaining / speed)
}

export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.round(seconds))
  if (s < 60) return `${s} 秒`
  if (s < 3600) {
    const m = Math.floor(s / 60)
    return `${m} 分 ${String(s % 60).padStart(2, '0')} 秒`
  }
  const h = Math.floor(s / 3600)
  return `${h} 时 ${String(Math.floor((s % 3600) / 60)).padStart(2, '0')} 分`
}

export function statusText(job: TransferJob): string {
  switch (job.status) {
    case 'queued':
      return '排队中'
    case 'running':
      return '传输中'
    case 'paused':
      return '已暂停'
    case 'done':
      return '已完成'
    case 'canceled':
      return '已取消'
    case 'skipped':
      return '已跳过（同名）'
    case 'failed': {
      const reason = job.error_message || job.error_code
      return reason ? `失败：${reason}` : '失败'
    }
    default:
      return job.status
  }
}

export function isActive(job: TransferJob): boolean {
  return job.status === 'queued' || job.status === 'running'
}

export function canPause(job: TransferJob): boolean {
  return isActive(job)
}

export function canResume(job: TransferJob): boolean {
  return job.status === 'paused' || job.status === 'failed' || job.status === 'canceled'
}

export function canCancel(job: TransferJob): boolean {
  return job.status !== 'done' && job.status !== 'canceled'
}

export function jobName(job: TransferJob): string {
  return job.file_name || job.remote_name || job.id
}
