import { describe, expect, test } from 'vitest'
import { changedSettings, globalSettingKeys, pickSettings, unbindWarning, userSettingKeys } from './settings'

const all = {
  download_concurrency: '3',
  upload_concurrency: '2',
  conflict: 'skip',
  play_mode: 'redirect',
  language: 'en',
}

describe('pickSettings', () => {
  test('keeps only the keys a form owns', () => {
    expect(pickSettings(all, globalSettingKeys)).toEqual({
      download_concurrency: '3',
      upload_concurrency: '2',
    })
    expect(Object.keys(pickSettings(all, userSettingKeys)).sort()).toEqual([
      'conflict',
      'language',
      'play_mode',
    ])
  })

  test('skips keys the server did not return', () => {
    const picked = pickSettings({ conflict: 'rename' }, userSettingKeys)
    expect(Object.keys(picked)).toEqual(['conflict'])
    expect(picked).toEqual({ conflict: 'rename' })
  })
})

describe('changedSettings', () => {
  test('returns only values that differ', () => {
    const next = { ...all, conflict: 'overwrite' }
    expect(changedSettings(all, next)).toEqual({ conflict: 'overwrite' })
  })

  test('returns nothing when nothing changed', () => {
    expect(changedSettings(all, { ...all })).toEqual({})
  })
})

describe('unbindWarning', () => {
  test('names both the cleared credentials and the cleared task records', () => {
    expect(unbindWarning).toContain('凭据')
    expect(unbindWarning).toContain('任务记录')
  })
})
