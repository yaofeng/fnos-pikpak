import { filenameFromDisposition } from './backup'
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

// apiFailure turns an error response body into the one error shape the pages
// catch, keeping the server's own wording as the fallback.
function apiFailure(res: Response, body: any): ApiError {
  const code: string = body?.error?.code ?? 'http_error'
  const raw: string =
    body?.error?.message ?? (typeof body?.error === 'string' ? body.error : res.statusText)
  const message = errorText(code, getLang()) ?? raw
  return new ApiError(res.status, code, message, `${res.status} ${code}: ${raw}`)
}

async function request<T>(path: string, init?: RequestInit, quiet = false): Promise<T> {
  const res = await fetch(`${basePath}/api/${path.replace(/^\/+/, '')}`, {
    ...init,
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
  })
  const text = await res.text()
  const body = text ? JSON.parse(text) : undefined
  if (!res.ok) {
    const err = apiFailure(res, body)
    // One place raises the visible error, so no page can forget to report it.
    // A quiet read opts out: its callers handle the failure themselves, and a
    // toast for an expected state (a share asking for its pass code) is noise.
    if (!quiet) notifyError(err.message, err.detail)
    throw err
  }
  return body as T
}

export const apiGet = <T,>(path: string) => request<T>(path)
// apiGetQuiet is apiGet without the global error toast.
export const apiGetQuiet = <T,>(path: string) => request<T>(path, undefined, true)
export const apiPost = <T,>(path: string, data: unknown) =>
  request<T>(path, { method: 'POST', body: JSON.stringify(data ?? {}) })
export const apiPatch = <T,>(path: string, data: unknown) =>
  request<T>(path, { method: 'PATCH', body: JSON.stringify(data ?? {}) })
export const apiPut = <T,>(path: string, data: unknown) =>
  request<T>(path, { method: 'PUT', body: JSON.stringify(data ?? {}) })
export const apiDelete = <T,>(path: string, data?: unknown) =>
  request<T>(path, { method: 'DELETE', body: data === undefined ? undefined : JSON.stringify(data) })

// apiDownload fetches a binary response (the backup file) and reports the name
// the server suggested, so the page can save it with a download link.
export async function apiDownload(path: string): Promise<{ blob: Blob; filename: string }> {
  const res = await fetch(`${basePath}/api/${path.replace(/^\/+/, '')}`, {
    credentials: 'same-origin',
  })
  if (!res.ok) {
    const text = await res.text()
    let body: any
    try {
      body = text ? JSON.parse(text) : undefined
    } catch {
      body = undefined
    }
    const err = apiFailure(res, body)
    notifyError(err.message, err.detail)
    throw err
  }
  return {
    blob: await res.blob(),
    filename: filenameFromDisposition(res.headers.get('Content-Disposition')),
  }
}

// apiUploadJSON posts a document that is already JSON text. A backup file can
// be large, so it is sent as read instead of being parsed and re-encoded.
export async function apiUploadJSON<T,>(path: string, text: string): Promise<T> {
  const res = await fetch(`${basePath}/api/${path.replace(/^\/+/, '')}`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: text,
  })
  const raw = await res.text()
  let body: any
  try {
    body = raw ? JSON.parse(raw) : undefined
  } catch {
    body = undefined
  }
  if (!res.ok) {
    const err = apiFailure(res, body)
    notifyError(err.message, err.detail)
    throw err
  }
  return body as T
}

export { basePath }
