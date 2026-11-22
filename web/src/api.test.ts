import { afterEach, describe, expect, test } from 'vitest'
import { ApiError, apiGet } from './api'
import { getToasts, resetToasts } from './toast'

const realFetch = globalThis.fetch

function stubFetch(status: number, body: string) {
  globalThis.fetch = (async () =>
    new Response(body, { status, headers: { 'Content-Type': 'application/json' } })) as typeof fetch
}

afterEach(() => {
  globalThis.fetch = realFetch
  resetToasts()
})

describe('ApiError', () => {
  test('carries the status and protocol code and translates the message', async () => {
    stubFetch(401, JSON.stringify({ error: { code: 'pikpak_reauth_required', message: "pikpak: re-authorization required" } }))
    const err = (await apiGet('me').catch((e) => e)) as ApiError
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(401)
    expect(err.code).toBe('pikpak_reauth_required')
    expect(err.message).toContain('重新绑定')
    expect(err.detail).toContain('re-authorization required')
  })

  test('keeps the server message for a code the table does not know', async () => {
    stubFetch(400, JSON.stringify({ error: { code: 'brand_new', message: '文件夹名不能为空' } }))
    const err = (await apiGet('files').catch((e) => e)) as ApiError
    expect(err.code).toBe('brand_new')
    expect(err.message).toBe('文件夹名不能为空')
  })

  test('raises one toast, expandable down to the raw detail', async () => {
    stubFetch(502, JSON.stringify({ error: { code: 'pikpak_error', message: 'upstream said no' } }))
    await apiGet('files').catch(() => undefined)
    await apiGet('files').catch(() => undefined)
    const toasts = getToasts()
    expect(toasts).toHaveLength(1)
    expect(toasts[0].detail).toContain('upstream said no')
  })
})
