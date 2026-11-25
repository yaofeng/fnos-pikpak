import { useEffect, useState } from 'react'
import { getLang, makeT } from '../i18n'
import { dismiss, getToasts, subscribeToasts, type Toast } from '../toast'

// Toaster renders every API error raised through api.ts. The short message is
// translated copy; 详情 opens the raw status, code and upstream text, which is
// what a report needs when the friendly line is not enough.
export default function Toaster() {
  const [items, setItems] = useState<Toast[]>(getToasts)
  const [open, setOpen] = useState<number | null>(null)
  const t = makeT(getLang())

  useEffect(() => subscribeToasts(() => setItems([...getToasts()])), [])

  useEffect(() => {
    const timers = items.map((toast) => setTimeout(() => dismiss(toast.id), 8000))
    return () => timers.forEach(clearTimeout)
  }, [items])

  if (items.length === 0) return null

  return (
    <div className="toasts" role="status" aria-live="polite">
      {items.map((toast) => (
        <div key={toast.id} className="toast">
          <div className="toast-head">
            <span className="toast-message">{toast.message}</span>
            <div className="actions">
              {toast.detail && (
                <button type="button" className="link" onClick={() => setOpen(open === toast.id ? null : toast.id)}>
                  {open === toast.id ? t('common.collapse') : t('error.details')}
                </button>
              )}
              <button type="button" className="link" onClick={() => dismiss(toast.id)}>
                {t('error.dismiss')}
              </button>
            </div>
          </div>
          {open === toast.id && toast.detail && <pre className="toast-detail">{toast.detail}</pre>}
        </div>
      ))}
    </div>
  )
}
