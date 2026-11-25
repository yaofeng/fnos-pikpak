import { describe, expect, it, test } from 'vitest'
import { waitForVideoJob, type VideoJob } from './videoPrepare'

function job(state: VideoJob['state'], extra: Partial<VideoJob> = {}): VideoJob {
  return { id: 'j1', state, ...extra }
}

describe('waitForVideoJob', () => {
  test('polls until the job is ready', async () => {
    const states = [job('running'), job('running'), job('ready', { stream_url: '/api/stream/f1?t=x' })]
    let calls = 0
    const got = await waitForVideoJob('j1', {
      get: async () => states[calls++],
      sleep: async () => {},
    })
    expect(calls).toBe(3)
    expect(got.state).toBe('ready')
    expect(got.stream_url).toBe('/api/stream/f1?t=x')
  })

  test('rejects with the server message when the job failed', async () => {
    await expect(
      waitForVideoJob('j1', {
        get: async () => job('failed', { error: '空间不足：清理后仍无法容纳该视频' }),
        sleep: async () => {},
      }),
    ).rejects.toThrow('空间不足')
  })

  test('gives up when the job never settles', async () => {
    let now = 0
    await expect(
      waitForVideoJob('j1', {
        get: async () => job('running'),
        sleep: async () => {},
        timeoutMs: 3000,
        now: () => (now += 1000),
      }),
    ).rejects.toThrow('超时')
  })
})

describe('waitForVideoJob（en）', () => {
  it('失败与超时抛英文错误', async () => {
    const failed = { id: 'j', state: 'failed', error: 'boom' }
    await expect(
      waitForVideoJob('j', { get: async () => failed as never, sleep: async () => {} }, 'en'),
    ).rejects.toThrow('boom')
    const running = { id: 'j', state: 'running' }
    let tick = 0
    await expect(
      waitForVideoJob(
        'j',
        {
          get: async () => running as never,
          sleep: async () => {},
          now: () => (tick += 500),
          timeoutMs: 1000,
        },
        'en',
      ),
    ).rejects.toThrow('Video preparation timed out. Try again later.')
    await expect(
      waitForVideoJob(
        'j',
        { get: async () => ({ id: 'j', state: 'failed' }) as never, sleep: async () => {} },
        'en',
      ),
    ).rejects.toThrow('Video preparation failed')
  })
})
