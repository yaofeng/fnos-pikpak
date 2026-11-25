// Preparing a video for 完整播放 can restore, purge and move files, so the
// server answers 202 with a job id and the browser polls it.
import { getLang, makeT, type Lang } from './i18n'

type VideoJobState = 'running' | 'ready' | 'failed'

export type VideoJob = {
  id: string
  state: VideoJobState
  stage?: string
  error?: string
  file_id?: string
  name?: string
  size?: number
  stream_url?: string
  // Set only when 播放器 = IINA: the original CDN link for the external player.
  direct_url?: string
  // account_id names the account the restored copy lives on, so a download
  // queued from it lands in the same account's drive.
  account_id?: string
}

type WaitOptions = {
  get: (id: string) => Promise<VideoJob>
  sleep?: (ms: number) => Promise<void>
  now?: () => number
  timeoutMs?: number
  intervalMs?: number
}

const defaultSleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms))

export async function waitForVideoJob(id: string, opts: WaitOptions, lang?: Lang): Promise<VideoJob> {
  const sleep = opts.sleep ?? defaultSleep
  const now = opts.now ?? Date.now
  const interval = opts.intervalMs ?? 1000
  const deadline = now() + (opts.timeoutMs ?? 15 * 60 * 1000)
  const t = makeT(lang ?? getLang())
  for (;;) {
    const job = await opts.get(id)
    if (job.state === 'ready') return job
    if (job.state === 'failed') throw new Error(job.error || t('video.prepare_failed'))
    if (now() >= deadline) throw new Error(t('video.prepare_timeout'))
    await sleep(interval)
  }
}
