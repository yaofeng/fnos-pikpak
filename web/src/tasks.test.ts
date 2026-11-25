import { describe, expect, it, test } from 'vitest'
import { canRetry, groupByAccount, isTaskActive, taskPercent, taskStatusText, taskTitle } from './tasks'

function task(extra: Record<string, unknown> = {}) {
  return { id: 't1', phase: 'PHASE_TYPE_RUNNING', progress: 40, ...extra } as never
}

describe('isTaskActive', () => {
  test('is true while pending or running', () => {
    expect(isTaskActive(task({ phase: 'PHASE_TYPE_PENDING' }))).toBe(true)
    expect(isTaskActive(task({ phase: 'PHASE_TYPE_RUNNING' }))).toBe(true)
  })

  test('is false once the task finished or failed', () => {
    expect(isTaskActive(task({ phase: 'PHASE_TYPE_COMPLETE' }))).toBe(false)
    expect(isTaskActive(task({ phase: 'PHASE_TYPE_ERROR' }))).toBe(false)
  })
})

describe('taskPercent', () => {
  test('passes the upstream progress through', () => {
    expect(taskPercent(task({ progress: 42 }))).toBe(42)
  })

  test('clamps out-of-range progress', () => {
    expect(taskPercent(task({ progress: -5 }))).toBe(0)
    expect(taskPercent(task({ progress: 140 }))).toBe(100)
  })

  test('treats a finished task as complete even without progress', () => {
    expect(taskPercent(task({ phase: 'PHASE_TYPE_COMPLETE', progress: 0 }))).toBe(100)
  })
})

describe('taskStatusText', () => {
  test('names the phases', () => {
    expect(taskStatusText(task({ phase: 'PHASE_TYPE_PENDING' }))).toBe('排队中')
    expect(taskStatusText(task({ phase: 'PHASE_TYPE_RUNNING' }))).toBe('下载中')
    expect(taskStatusText(task({ phase: 'PHASE_TYPE_COMPLETE' }))).toBe('已完成')
  })

  test('shows the upstream message for failures', () => {
    expect(taskStatusText(task({ phase: 'PHASE_TYPE_ERROR', message: 'link expired' }))).toBe('失败：link expired')
    expect(taskStatusText(task({ phase: 'PHASE_TYPE_ERROR' }))).toBe('失败')
  })
})

describe('taskTitle', () => {
  test('prefers the downloaded file name', () => {
    expect(taskTitle(task({ file_name: 'a.mkv', name: 'task' }))).toBe('a.mkv')
    expect(taskTitle(task({ name: 'task' }))).toBe('task')
    expect(taskTitle(task({}))).toBe('t1')
  })
})

describe('canRetry', () => {
  test('only a failed task can be retried', () => {
    expect(canRetry(task({ phase: 'PHASE_TYPE_ERROR' }))).toBe(true)
    // A completed task has nothing left to retry: PikPak would only create a
    // duplicate download.
    expect(canRetry(task({ phase: 'PHASE_TYPE_COMPLETE' }))).toBe(false)
    expect(canRetry(task({ phase: 'PHASE_TYPE_RUNNING' }))).toBe(false)
    expect(canRetry(task({ phase: 'PHASE_TYPE_PENDING' }))).toBe(false)
  })
})

describe('groupByAccount', () => {
  it('groupByAccount 保序分组', () => {
    expect(groupByAccount([
      { id: '1', account_id: 'a' }, { id: '2', account_id: 'b' }, { id: '3', account_id: 'a' },
    ])).toEqual([{ account_id: 'a', ids: ['1', '3'] }, { account_id: 'b', ids: ['2'] }])
  })

  it('缺 account_id 的旧行归入空组', () => {
    expect(groupByAccount([{ id: '9' }])).toEqual([{ account_id: '', ids: ['9'] }])
  })
})
