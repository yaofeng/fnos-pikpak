// Selection follows the Finder: a click selects one item, Command/Control
// toggles an item, Shift selects the range from the anchor, and a rubber band
// selects whatever it touches. The list order is the selection order, so the
// same helpers serve the icon view and the list view.

export type Selection = { ids: string[]; anchor: number | null }

export const emptySelection: Selection = { ids: [], anchor: null }

export type ClickMods = { toggle?: boolean; range?: boolean }

function ordered(order: string[], chosen: Set<string>): string[] {
  return order.filter((id) => chosen.has(id))
}

/** Apply one click on the item at index. */
export function applyClick(state: Selection, order: string[], index: number, mods: ClickMods): Selection {
  if (index < 0 || index >= order.length) return state
  const id = order[index]
  const chosen = new Set(state.ids)

  if (mods.toggle) {
    if (chosen.has(id)) chosen.delete(id)
    else chosen.add(id)
    return { ids: ordered(order, chosen), anchor: index }
  }
  if (mods.range && state.anchor !== null) {
    const from = Math.min(state.anchor, index)
    const to = Math.max(state.anchor, index)
    return { ids: order.slice(from, to + 1), anchor: state.anchor }
  }
  return { ids: [id], anchor: index }
}

/** Apply a rubber-band hit list; additive keeps what was already selected. */
export function applyHits(state: Selection, order: string[], hits: string[], additive: boolean): Selection {
  const chosen = new Set(additive ? state.ids : [])
  for (const id of hits) chosen.add(id)
  return { ids: ordered(order, chosen), anchor: state.anchor }
}

export function selectAllIn(order: string[]): Selection {
  return { ids: [...order], anchor: order.length ? 0 : null }
}
