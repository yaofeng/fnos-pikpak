import { describe, expect, it } from 'vitest'
import { accountLabel, resolveCurrentId, validateDisplayName, sumQuota, vipTypeLabel, worstStatus, type AccountInfo } from './accounts'

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

describe('vipTypeLabel', () => {
  it('已知档位译为中文', () => {
    expect(vipTypeLabel('platinum')).toBe('白金会员')
    expect(vipTypeLabel('gold')).toBe('黄金会员')
  })

  it('非会员不带 VIP 标记', () => {
    expect(vipTypeLabel('NON-member')).toBe('')
    expect(vipTypeLabel('')).toBe('')
  })

  it('未知档位保留上游词并加 VIP 前缀', () => {
    expect(vipTypeLabel('diamond')).toBe('VIP·diamond')
  })
})

describe('accountLabel', () => {
  const byId = new Map([['a', { ...base, id: 'a', display_name: '主号' }]])

  it('响应自带的账号名优先', () => {
    expect(accountLabel(byId, 'a', '响应名')).toBe('响应名')
  })

  it('已知账号显示 display_name', () => {
    expect(accountLabel(byId, 'a')).toBe('主号')
  })

  it('未知 id 显示前 6 位', () => {
    expect(accountLabel(byId, 'abcdef123456')).toBe('abcdef')
  })

  it('空 id 的旧行显示默认账号', () => {
    expect(accountLabel(byId, '')).toBe('默认账号')
    expect(accountLabel(byId, undefined)).toBe('默认账号')
  })
})

describe('validateDisplayName', () => {
  it('去首尾空格并通过合法名称', () => {
    expect(validateDisplayName('  主力号  ')).toEqual({ ok: true, name: '主力号' })
  })
  it('纯空格拒绝且提示不能为空', () => {
    expect(validateDisplayName('   ')).toEqual({ ok: false, error: '账号名称不能为空' })
  })
  it('超过 50 字拒绝', () => {
    expect(validateDisplayName('长'.repeat(51))).toEqual({ ok: false, error: '账号名称过长，最多 50 个字符' })
  })
  it('恰好 50 字通过', () => {
    expect(validateDisplayName('长'.repeat(50)).ok).toBe(true)
  })
})

describe('validateDisplayName（en）', () => {
  it('纯空格拒绝时返回英文提示', () => {
    expect(validateDisplayName('   ', 'en')).toEqual({ ok: false, error: 'Account name cannot be empty' })
  })
  it('超过 50 字返回英文提示', () => {
    expect(validateDisplayName('长'.repeat(51), 'en')).toEqual({
      ok: false,
      error: 'Account name is too long; 50 characters at most',
    })
  })
  it('默认语言为中文', () => {
    expect(validateDisplayName(' ', undefined)).toEqual({ ok: false, error: '账号名称不能为空' })
  })
})

describe('vipTypeLabel（en）', () => {
  it('英文会员名', () => {
    expect(vipTypeLabel('platinum', 'en')).toBe('Platinum member')
    expect(vipTypeLabel('gold', 'en')).toBe('Gold member')
    expect(vipTypeLabel('diamond', 'en')).toBe('VIP·diamond')
  })
})

describe('accountLabel（en）', () => {
  it('无 id 行回退英文默认标签', () => {
    const byId = new Map([['a', { id: 'a', display_name: '主号' } as AccountInfo]])
    expect(accountLabel(byId, 'a', undefined, 'en')).toBe('主号')
    expect(accountLabel(undefined, '', undefined, 'en')).toBe('Default account')
  })
})
