import { describe, expect, it, test } from 'vitest'
import { etaSeconds, formatDuration, percent, speedOf, statusText, transferred, visibleJobs } from './transfers'

function job(extra: Record<string, unknown> = {}) {
  return { id: 'j1', status: 'running', size: 100, downloaded: 40, ...extra } as never
}

describe('progress', () => {
  test('reports the percentage of the job size', () => {
    expect(percent(job())).toBe(40)
  })

  test('is zero when the size is unknown', () => {
    expect(percent(job({ size: 0 }))).toBe(0)
  })

  test('never exceeds 100 even if the counter overshoots', () => {
    expect(percent(job({ downloaded: 120 }))).toBe(100)
  })

  test('reads the upload counter for uploads', () => {
    expect(percent(job({ downloaded: 0, uploaded: 75 }))).toBe(75)
    expect(transferred(job({ downloaded: 0, uploaded: 75 }))).toBe(75)
  })
})

describe('speedOf', () => {
  test('returns zero without a previous sample', () => {
    expect(speedOf(undefined, { bytes: 10, at: 1000 })).toBe(0)
  })

  test('computes bytes per second between samples', () => {
    expect(speedOf({ bytes: 0, at: 1000 }, { bytes: 2 << 20, at: 3000 })).toBe(1 << 20)
  })

  test('returns zero rather than a negative speed when the counter resets', () => {
    expect(speedOf({ bytes: 500, at: 1000 }, { bytes: 10, at: 2000 })).toBe(0)
  })

  test('returns zero for a non-advancing clock', () => {
    expect(speedOf({ bytes: 10, at: 1000 }, { bytes: 20, at: 1000 })).toBe(0)
  })
})

describe('etaSeconds', () => {
  test('divides the remaining bytes by the speed', () => {
    expect(etaSeconds({ size: 100, downloaded: 40 }, 20)).toBe(3)
  })

  test('is null while the speed is unknown', () => {
    expect(etaSeconds({ size: 100, downloaded: 40 }, 0)).toBeNull()
  })

  test('is zero once the transfer is complete', () => {
    expect(etaSeconds({ size: 100, downloaded: 100 }, 20)).toBe(0)
  })
})

describe('formatDuration', () => {
  test('keeps short waits in seconds', () => {
    expect(formatDuration(45)).toBe('45 秒')
  })

  test('pads the seconds in minutes', () => {
    expect(formatDuration(125)).toBe('2 分 05 秒')
  })

  test('switches to hours past one hour', () => {
    expect(formatDuration(3725)).toBe('1 时 02 分')
  })
})

describe('statusText', () => {
  test('names the ordinary states', () => {
    expect(statusText({ id: '1', status: 'queued', size: 0 })).toBe('排队中')
    expect(statusText({ id: '1', status: 'running', size: 0 })).toBe('传输中')
    expect(statusText({ id: '1', status: 'paused', size: 0 })).toBe('已暂停')
    expect(statusText({ id: '1', status: 'done', size: 0 })).toBe('已完成')
    expect(statusText({ id: '1', status: 'canceled', size: 0 })).toBe('已取消')
  })

  test('shows the failure reason rather than a bare state', () => {
    expect(
      statusText({ id: '1', status: 'failed', size: 0, error_code: 'download_failed', error_message: '链接失效' }),
    ).toBe('失败：链接失效')
  })

  test('falls back to the error code when no message came along', () => {
    expect(statusText({ id: '1', status: 'failed', size: 0, error_code: 'size_mismatch' })).toBe(
      '失败：size_mismatch',
    )
  })

  test('marks skipped uploads as a conflict', () => {
    expect(statusText({ id: '1', status: 'skipped', size: 0 })).toBe('已跳过（同名）')
  })
})

describe('formatDuration（en）', () => {
  it('英文时长单位', () => {
    expect(formatDuration(45, 'en')).toBe('45s')
    expect(formatDuration(125, 'en')).toBe('2m 05s')
    expect(formatDuration(3725, 'en')).toBe('1h 02m')
  })
})

describe('statusText（en）', () => {
  it('英文状态与失败原因', () => {
    expect(statusText({ id: '1', status: 'queued', size: 0 }, 'en')).toBe('Queued')
    expect(statusText({ id: '1', status: 'running', size: 0 }, 'en')).toBe('Transferring')
    expect(statusText({ id: '1', status: 'skipped', size: 0 }, 'en')).toBe('Skipped (same name)')
    expect(statusText({ id: '1', status: 'failed', size: 0, error_message: 'disk full' }, 'en')).toBe(
      'Failed: disk full',
    )
    expect(statusText({ id: '1', status: 'failed', size: 0 }, 'en')).toBe('Failed')
  })
})

describe('visibleJobs', () => {
  const jobs = [
    job({ id: 'a', account_id: 'acct1' }),
    job({ id: 'b', account_id: 'acct2' }),
    // Rows queued before accounts existed carry no stamp.
    job({ id: 'old' }),
  ]

  test('keeps only the scoped account under a concrete scope', () => {
    expect(visibleJobs(jobs, 'acct1').map((j) => j.id)).toEqual(['a'])
    expect(visibleJobs(jobs, 'acct2').map((j) => j.id)).toEqual(['b'])
  })

  test('hides an unstamped row only when the scope is concrete', () => {
    // No stamp means the row cannot be attributed to one account, so a
    // single-account view has to leave it out.
    expect(visibleJobs(jobs, 'acct1').map((j) => j.id)).not.toContain('old')
  })

  test("keeps everything, unstamped rows included, under 'all'", () => {
    expect(visibleJobs(jobs, 'all').map((j) => j.id)).toEqual(['a', 'b', 'old'])
  })

  test('returns the list untouched under all', () => {
    expect(visibleJobs([], 'all')).toEqual([])
  })
})
