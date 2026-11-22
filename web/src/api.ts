import { errorText, getLang } from './i18n'
import { notifyError } from './toast'

const basePath = (() => {
  // No window outside the browser (unit tests, prerendering): requests then
  // resolve against the server root.
  if (typeof window === 'undefined') return ''
  const path = window.location.pathname
    .replace(/\/index\.html$/, '')
    .replace(/\/+$/, '')
  return path
})()

// ApiError keeps the protocol facts (status, code, the server's own wording)
// apart from the copy a person reads, so a translated message never hides what
// actually failed.
export class ApiError extends Error {
  status: number
  code: string
  detail: string

  constructor(status: number, code: string, message: string, detail: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.detail = detail
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${basePath}/api/${path.replace(/^\/+/, '')}`, {
    ...init,
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
  })
  const text = await res.text()
  const body = text ? JSON.parse(text) : undefined
  if (!res.ok) {
    const code: string = body?.error?.code ?? 'http_error'
    const raw: string =
      body?.error?.message ?? (typeof body?.error === 'string' ? body.error : res.statusText)
    const message = errorText(code, getLang()) ?? raw
    // One place raises the visible error, so no page can forget to report it.
    notifyError(message, `${res.status} ${code}: ${raw}`)
    throw new ApiError(res.status, code, message, `${res.status} ${code}: ${raw}`)
  }
  return body as T
}

export const apiGet = <T,>(path: string) => request<T>(path)
export const apiPost = <T,>(path: string, data: unknown) =>
  request<T>(path, { method: 'POST', body: JSON.stringify(data ?? {}) })
export const apiPatch = <T,>(path: string, data: unknown) =>
  request<T>(path, { method: 'PATCH', body: JSON.stringify(data ?? {}) })
export const apiPut = <T,>(path: string, data: unknown) =>
  request<T>(path, { method: 'PUT', body: JSON.stringify(data ?? {}) })
export const apiDelete = <T,>(path: string, data?: unknown) =>
  request<T>(path, { method: 'DELETE', body: data === undefined ? undefined : JSON.stringify(data) })

export { basePath }
