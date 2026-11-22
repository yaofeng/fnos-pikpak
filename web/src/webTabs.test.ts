import { describe, expect, it } from 'vitest'
import { afterClose, openTab, type WebTab } from './webTabs'

const tabs: WebTab[] = [
  { id: 1, title: 'A', url: 'https://mypikpak.com/s/1' },
  { id: 2, title: 'B', url: 'https://mypikpak.com/s/2' },
  { id: 3, title: 'C', url: 'https://mypikpak.com/s/3' },
]

describe('openTab', () => {
  it('appends a tab for a new address and reports its id', () => {
    const next = { id: 4, title: 'D', url: 'https://mypikpak.com/s/4' }
    const got = openTab(tabs, next)
    expect(got.tabs.map((t) => t.id)).toEqual([1, 2, 3, 4])
    expect(got.id).toBe(4)
  })

  it('reuses the tab already open for an address instead of stacking a duplicate', () => {
    const got = openTab(tabs, { id: 9, title: 'B again', url: 'https://mypikpak.com/s/2' })
    expect(got.tabs).toBe(tabs)
    expect(got.id).toBe(2)
  })
})

describe('afterClose', () => {
  it('leaves the active tab alone when another tab closes', () => {
    expect(afterClose(tabs, 1, 2)).toBe(2)
  })

  it('shows the next tab when the active one closes', () => {
    expect(afterClose(tabs, 2, 2)).toBe(3)
  })

  it('falls back to the previous tab when the last active one closes', () => {
    expect(afterClose(tabs, 3, 3)).toBe(2)
  })

  it('returns to the search page when the final tab closes', () => {
    expect(afterClose([{ id: 1, title: 'A', url: 'https://mypikpak.com/s/1' }], 1, 1)).toBeNull()
  })
})
