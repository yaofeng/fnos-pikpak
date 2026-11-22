// A tiny toast store. API failures call notifyError from one place (api.ts),
// so every page reports them the same way instead of each keeping its own
// error string.
export type Toast = {
  id: number
  message: string
  detail?: string
}

const maxToasts = 5

let toasts: Toast[] = []
let nextId = 1
const listeners = new Set<() => void>()

function emit() {
  for (const fn of listeners) fn()
}

export function getToasts(): Toast[] {
  return toasts
}

export function subscribeToasts(fn: () => void): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

export function notifyError(message: string, detail?: string): number {
  const newest = toasts[toasts.length - 1]
  if (newest && newest.message === message) {
    // Polling pages re-send the same failure every couple of seconds; refresh
    // the existing toast rather than stacking copies.
    newest.detail = detail ?? newest.detail
    emit()
    return newest.id
  }
  const toast: Toast = { id: nextId++, message, detail }
  toasts = [...toasts, toast].slice(-maxToasts)
  emit()
  return toast.id
}

export function dismiss(id: number) {
  const before = toasts.length
  toasts = toasts.filter((t) => t.id !== id)
  if (toasts.length !== before) emit()
}

export function resetToasts() {
  toasts = []
  emit()
}
