const basePath = (() => {
  const path = window.location.pathname
    .replace(/\/index\.html$/, '')
    .replace(/\/+$/, '')
  return path
})()

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${basePath}/api/${path.replace(/^\/+/, '')}`, {
    ...init,
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
  })
  const text = await res.text()
  const body = text ? JSON.parse(text) : undefined
  if (!res.ok) {
    const message = body?.error?.message ?? body?.error ?? res.statusText
    throw new Error(`${res.status}: ${message}`)
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
