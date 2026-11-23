import { useCallback, useRef, useState, type MouseEvent as ReactMouseEvent } from 'react'

type MarqueeRect = { left: number; top: number; width: number; height: number }

type Options = {
  // Called with every item the band touches, and whether the add key was held
  // when the drag started.
  onHits: (ids: string[], additive: boolean) => void
  // Called when the drag turned out to be a click on the empty background.
  onEmptyClick?: () => void
}

const DRAG_THRESHOLD = 4

// useMarquee turns a drag that starts on the empty background into a
// Finder-style rubber band. Items are found through their data-file-id
// attribute, so the same hook serves the list and the icon view. A drag that
// starts on an item is left alone: that is a file drag or a click.
export function useMarquee({ onHits, onEmptyClick }: Options) {
  const box = useRef<HTMLDivElement | null>(null)
  const [rect, setRect] = useState<MarqueeRect | null>(null)

  const onMouseDown = useCallback(
    (e: ReactMouseEvent<HTMLDivElement>) => {
      if (e.button !== 0) return
      const el = box.current
      if (!el) return
      if ((e.target as HTMLElement).closest('[data-file-id]')) return

      const startX = e.clientX
      const startY = e.clientY
      const additive = e.metaKey || e.ctrlKey
      let moved = false
      let lastKey = ''
      const previousSelect = document.body.style.userSelect
      document.body.style.userSelect = 'none'

      if (!additive) onHits([], false)

      const onMove = (ev: MouseEvent) => {
        const dx = ev.clientX - startX
        const dy = ev.clientY - startY
        if (!moved && Math.abs(dx) < DRAG_THRESHOLD && Math.abs(dy) < DRAG_THRESHOLD) return
        moved = true
        const base = el.getBoundingClientRect()
        const left = Math.min(startX, ev.clientX)
        const top = Math.min(startY, ev.clientY)
        const right = Math.max(startX, ev.clientX)
        const bottom = Math.max(startY, ev.clientY)
        setRect({ left: left - base.left, top: top - base.top, width: right - left, height: bottom - top })

        const hits: string[] = []
        el.querySelectorAll<HTMLElement>('[data-file-id]').forEach((node) => {
          const r = node.getBoundingClientRect()
          if (r.left < right && r.right > left && r.top < bottom && r.bottom > top && node.dataset.fileId) {
            hits.push(node.dataset.fileId)
          }
        })
        const key = hits.join('\n')
        if (key !== lastKey) {
          lastKey = key
          onHits(hits, additive)
        }
      }

      const onUp = () => {
        window.removeEventListener('mousemove', onMove)
        window.removeEventListener('mouseup', onUp)
        document.body.style.userSelect = previousSelect
        setRect(null)
        if (!moved && !additive) onEmptyClick?.()
      }

      window.addEventListener('mousemove', onMove)
      window.addEventListener('mouseup', onUp)
    },
    [onHits, onEmptyClick],
  )

  return { box, rect, onMouseDown }
}
