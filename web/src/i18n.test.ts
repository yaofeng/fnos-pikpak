import { describe, expect, test } from 'vitest'
import { errorText, makeT, setLang, getLang } from './i18n'

describe('errorText', () => {
  test('translates a known error code', () => {
    expect(errorText('pikpak_reauth_required', 'zh-CN')).toContain('重新绑定')
    expect(errorText('pikpak_reauth_required', 'en')).toContain('Link')
  })

  test('translates the quota refusal', () => {
    expect(errorText('daily_limit_reached', 'zh-CN')).toContain('额度')
    expect(errorText('daily_limit_reached', 'en')).toContain('quota')
  })

  test('returns undefined for an unknown code so the server message wins', () => {
    expect(errorText('something_new', 'zh-CN')).toBeUndefined()
  })
})

describe('language state', () => {
  test('setLang changes what makeT and errorText use by default', () => {
    setLang('en')
    expect(getLang()).toBe('en')
    expect(makeT(getLang())('nav.files')).toBe('My Files')
    expect(errorText('not_bound', getLang())).toContain('No PikPak account')
    setLang('zh-CN')
    expect(makeT(getLang())('nav.files')).toBe('我的文件')
  })
})
