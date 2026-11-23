// Preparing a video for 完整播放 can restore, purge and move files, so the
// server answers 202 with a job id and the browser polls it.

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
}

type WaitOptions = {
  get: (id: string) => Promise<VideoJob>
  sleep?: (ms: number) => Promise<void>
  now?: () => number
  timeoutMs?: number
  intervalMs?: number
}

const defaultSleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms))

export async function waitForVideoJob(id: string, opts: WaitOptions): Promise<VideoJob> {
  const sleep = opts.sleep ?? defaultSleep
  const now = opts.now ?? Date.now
  const interval = opts.intervalMs ?? 1000
  const deadline = now() + (opts.timeoutMs ?? 15 * 60 * 1000)
  for (;;) {
    const job = await opts.get(id)
    if (job.state === 'ready') return job
    if (job.state === 'failed') throw new Error(job.error || '视频准备失败')
    if (now() >= deadline) throw new Error('视频准备超时，请稍后重试')
    await sleep(interval)
  }
}
