import { describe, expect, it } from 'vitest'
import { resolveCurrentId, sumQuota, worstStatus, type AccountInfo } from './accounts'

const base: AccountInfo = {
  id: 'a',
  display_name: '',
  avatar: '',
  vip_type: '',
  vip_expire: '',
  status: 'active',
  quota_usage: 0,
  quota_limit: 0,
}

describe('sumQuota', () => {
  it('跳过带 error 的账号', () => {
    expect(sumQuota([
      { ...base, quota_usage: 10, quota_limit: 100 },
      { ...base, id: 'b', error: 'x', quota_usage: 5, quota_limit: 50 },
    ])).toEqual({ usage: 10, limit: 100 })
  })

  it('合计正常账号的用量与上限', () => {
    expect(sumQuota([
      { ...base, quota_usage: 10, quota_limit: 100 },
      { ...base, id: 'b', quota_usage: 5, quota_limit: 50 },
    ])).toEqual({ usage: 15, limit: 150 })
  })

  it('空列表为 0', () => {
    expect(sumQuota([])).toEqual({ usage: 0, limit: 0 })
  })
})

describe('worstStatus', () => {
  it('任一 reauth_required 即返回它', () => {
    expect(worstStatus([{ ...base, status: 'active' }, { ...base, id: 'b', status: 'reauth_required' }])).toBe('reauth_required')
    expect(worstStatus([])).toBe('unbound')
  })

  it('全部 active 返回 active', () => {
    expect(worstStatus([base, { ...base, id: 'b' }])).toBe('active')
  })
})

describe('resolveCurrentId', () => {
  it('currentId 仍在账号列表里时保持不变', () => {
    expect(resolveCurrentId([base, { ...base, id: 'b' }], 'b')).toBe('b')
  })

  it('currentId 失效（账号被解绑）回落第一个', () => {
    expect(resolveCurrentId([{ ...base, id: 'c' }], 'b')).toBe('c')
  })

  it('没有账号时为空', () => {
    expect(resolveCurrentId([], 'b')).toBe('')
  })

  it('没有 currentId 时默认第一个', () => {
    expect(resolveCurrentId([{ ...base, id: 'c' }], '')).toBe('c')
  })
})
