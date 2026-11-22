import { describe, expect, test } from 'vitest'
import { dismiss, getToasts, notifyError, resetToasts, subscribeToasts } from './toast'

describe('toast store', () => {
  test('adds a toast with the message and detail', () => {
    resetToasts()
    const id = notifyError('授权已失效', '401 pikpak_reauth_required: expired')
    const items = getToasts()
    expect(items).toHaveLength(1)
    expect(items[0].message).toBe('授权已失效')
    expect(items[0].detail).toContain('expired')
    dismiss(id)
    expect(getToasts()).toHaveLength(0)
  })

  test('a repeated error replaces the newest toast instead of stacking', () => {
    // The transfer and task pages poll every couple of seconds; without this
    // one broken account would bury the screen in identical toasts.
    resetToasts()
    notifyError('保存设置失败')
    notifyError('保存设置失败')
    const items = getToasts()
    expect(items).toHaveLength(1)
    expect(items[0].message).toBe('保存设置失败')
  })

  test('keeps the newest five toasts', () => {
    resetToasts()
    for (let i = 0; i < 7; i++) notifyError(`错误 ${i}`)
    const messages = getToasts().map((t) => t.message)
    expect(messages).toEqual(['错误 2', '错误 3', '错误 4', '错误 5', '错误 6'])
  })

  test('notifies subscribers on change', () => {
    resetToasts()
    let seen = 0
    const off = subscribeToasts(() => {
      seen++
    })
    notifyError('x')
    dismiss(getToasts()[0].id)
    off()
    notifyError('y')
    expect(seen).toBe(2)
  })
})
