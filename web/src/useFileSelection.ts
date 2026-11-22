import { useCallback, useMemo, useState } from 'react'
import { applyClick, applyHits, emptySelection, selectAllIn, type Selection } from './selection'

// useFileSelection keeps the Finder-style selection for one list. `order` is
// the currently shown ids, so a range or an "all" always follows what the user
// sees after filtering and sorting.
export function useFileSelection(order: string[]) {
  const [state, setState] = useState<Selection>(emptySelection)
  const selected = useMemo(() => new Set(state.ids), [state.ids])

  const select = useCallback(
    (index: number, e: { metaKey?: boolean; ctrlKey?: boolean; shiftKey?: boolean }) => {
      setState((prev) =>
        applyClick(prev, order, index, { toggle: Boolean(e.metaKey || e.ctrlKey), range: Boolean(e.shiftKey) }),
      )
    },
    [order],
  )

  const hitTest = useCallback(
    (ids: string[], additive: boolean) => setState((prev) => applyHits(prev, order, ids, additive)),
    [order],
  )

  const selectAll = useCallback(() => setState(selectAllIn(order)), [order])
  const clear = useCallback(() => setState(emptySelection), [])

  return { ids: state.ids, selected, select, hitTest, selectAll, clear }
}
