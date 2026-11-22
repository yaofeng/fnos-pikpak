// In-app viewer tabs for 全网搜索. A result opens in a tab inside the app
// instead of a browser window; the tabs live on the search page and are not
// persisted anywhere.
export type WebTab = { id: number; title: string; url: string }

// openTab adds a tab for url, or reuses the one already showing it, so clicking
// the same result twice focuses instead of stacking duplicates.
export function openTab(tabs: WebTab[], tab: WebTab): { tabs: WebTab[]; id: number } {
  const existing = tabs.find((t) => t.url === tab.url)
  if (existing) return { tabs, id: existing.id }
  return { tabs: [...tabs, tab], id: tab.id }
}

// afterClose picks the tab to show once closedId is gone: the active tab when
// it survived, otherwise the tab that took its place (the next one, or the
// previous one when the last tab closed). null means "back to the results".
export function afterClose(tabs: WebTab[], closedId: number, activeId: number | null): number | null {
  if (activeId !== closedId) return activeId
  const index = tabs.findIndex((t) => t.id === closedId)
  const rest = tabs.filter((t) => t.id !== closedId)
  if (rest.length === 0) return null
  return rest[Math.min(index, rest.length - 1)].id
}
