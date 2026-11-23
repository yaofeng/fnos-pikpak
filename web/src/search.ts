// Web-search result types and the small pieces of copy-free logic the 全网搜索
// page uses. The server owns the query: it adds the site: filter, so nothing
// here ever builds one.
export type WebResult = {
  title: string
  url: string
  snippet: string
  // Share marks a page the app can open in its own share browser. With the
  // wide scope the list also holds ordinary pages on the site, and this is
  // what tells them apart.
  share?: boolean
  // Wrapped marks one of the search provider's own click redirects. Its
  // destination is asked for when the result is opened, not while the list is
  // built: a page can hold twenty of them.
  wrapped?: boolean
  // Display is the address the provider showed under the title, used as the
  // breadcrumb of a wrapped result until it is opened.
  display?: string
}
export type WebSearchPage = {
  items: WebResult[]
  query: string
  page: number
  raw?: number
  scope?: 'share' | 'all'
}

// shareCount is how many of the listed hits are share pages. The wide scope
// lists every hit the provider returned, so the count line says how many of
// them are shares.
export function shareCount(items: WebResult[]): number {
  return items.filter((r) => r.share).length
}

// crumbFromURL renders a result address the way a search engine does: the
// host first, then each path segment, without the scheme.
export function crumbFromURL(raw: string): string {
  try {
    const u = new URL(raw)
    const segments = u.pathname.split('/').filter(Boolean)
    return segments.length ? `${u.hostname} › ${segments.join(' › ')}` : u.hostname
  } catch {
    return raw
  }
}

// appendResults adds a page to the list already shown, dropping a hit the
// earlier pages had (the provider sometimes repeats one). Order is arrival
// order, so the list reads like a search engine's single long page.
export function appendResults(shown: WebResult[], next: WebResult[]): WebResult[] {
  const seen = new Set(shown.map(resultKey))
  const out = shown.slice()
  for (const hit of next) {
    const key = resultKey(hit)
    if (seen.has(key)) continue
    seen.add(key)
    out.push(hit)
  }
  return out
}

// resultKey is what tells two results apart. A wrapped share carries a
// different provider token on every page, so the address the provider showed
// under the title is what identifies it.
function resultKey(hit: WebResult): string {
  if (hit.wrapped && hit.display) return `display:${hit.display}`
  return shareKey(hit.url)
}

// shareKey is the share a result points at. Every PikPak host serves a share
// at /s/<id>, so the same share reached through two hosts is one entry.
function shareKey(raw: string): string {
  try {
    const parts = new URL(raw).pathname.split('/').filter(Boolean)
    if (parts.length >= 2 && parts[0] === 's') return parts[1]
  } catch {
    // Not a URL: compare it as it is.
  }
  return raw
}

// shareHosts are the domains PikPak serves share pages from, as the server
// knows them.
const shareHosts = ['mypikpak.com', 'mypikpak.net', 'pikpak.me', 'mypikpak.me']

// shareURL returns the share a resolved address points at, or '' when the
// share browser cannot open it. Opening a wrapped result asks the server what
// it leads to; this is what decides whether that answer continues in the app.
export function shareURL(raw: string): string {
  try {
    const u = new URL(raw)
    if (u.protocol !== 'https:' && u.protocol !== 'http:') return ''
    const host = u.hostname.toLowerCase().replace(/^www\./, '')
    if (!shareHosts.includes(host)) return ''
    const parts = u.pathname.split('/').filter(Boolean)
    if (parts.length < 2 || parts[0] !== 's' || !/^[A-Za-z0-9_-]{6,64}$/.test(parts[1])) return ''
    return `https://${host}/s/${parts[1]}`
  } catch {
    return ''
  }
}

// hasMorePages says whether another page is worth asking for: an empty page
// means the keyword is exhausted, and the provider stops at its last page.
export function hasMorePages(page: number, received: number, maxPage: number): boolean {
  return received > 0 && page < maxPage
}
