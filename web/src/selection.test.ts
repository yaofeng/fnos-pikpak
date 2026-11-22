import { describe, expect, test } from 'vitest'
import { applyClick, applyHits, emptySelection, selectAllIn } from './selection'

const order = ['a', 'b', 'c', 'd']

describe('applyClick', () => {
  test('a plain click keeps only that item and makes it the anchor', () => {
    const state = { ids: ['a', 'b'], anchor: 0 }
    expect(applyClick(state, order, 2, {})).toEqual({ ids: ['c'], anchor: 2 })
  })

  test('meta/ctrl toggles the item and moves the anchor', () => {
    const added = applyClick({ ids: ['a'], anchor: 0 }, order, 2, { toggle: true })
    expect(added).toEqual({ ids: ['a', 'c'], anchor: 2 })

    const removed = applyClick(added, order, 0, { toggle: true })
    expect(removed).toEqual({ ids: ['c'], anchor: 0 })
  })

  test('shift selects the range from the anchor and keeps the anchor', () => {
    expect(applyClick({ ids: ['b'], anchor: 1 }, order, 3, { range: true })).toEqual({ ids: ['b', 'c', 'd'], anchor: 1 })
    expect(applyClick({ ids: ['b'], anchor: 1 }, order, 0, { range: true })).toEqual({ ids: ['a', 'b'], anchor: 1 })
  })

  test('shift without an anchor behaves like a plain click', () => {
    expect(applyClick(emptySelection, order, 2, { range: true })).toEqual({ ids: ['c'], anchor: 2 })
  })

  test('an index outside the list leaves the selection alone', () => {
    const state = { ids: ['b'], anchor: 1 }
    expect(applyClick(state, order, 9, {})).toBe(state)
    expect(applyClick(state, order, -1, {})).toBe(state)
  })

  test('selection keeps the order of the list, not the click order', () => {
    const state = applyClick(emptySelection, order, 3, {})
    expect(applyClick(state, order, 1, { toggle: true }).ids).toEqual(['b', 'd'])
  })
})

describe('applyHits', () => {
  test('a plain rubber band replaces the selection', () => {
    expect(applyHits({ ids: ['a'], anchor: 0 }, order, ['c', 'b'], false)).toEqual({ ids: ['b', 'c'], anchor: 0 })
  })

  test('a rubber band with the add key unions with what was selected', () => {
    expect(applyHits({ ids: ['a'], anchor: 0 }, order, ['c', 'b'], true)).toEqual({ ids: ['a', 'b', 'c'], anchor: 0 })
  })
})

describe('selectAllIn', () => {
  test('selects every listed item', () => {
    expect(selectAllIn(order)).toEqual({ ids: ['a', 'b', 'c', 'd'], anchor: 0 })
    expect(selectAllIn([])).toEqual({ ids: [], anchor: null })
  })
})
