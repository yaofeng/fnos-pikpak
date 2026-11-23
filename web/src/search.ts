// Web-search result types and the small pieces of copy-free logic the 全网搜索
// page uses. The server owns the query: it adds the site: filter, so nothing
// here ever builds one.
export type WebResult = { title: string; url: string; snippet: string }
export type WebSearchPage = { items: WebResult[]; query: string; page: number; raw?: number }

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
  const seen = new Set(shown.map((r) => shareKey(r.url)))
  const out = shown.slice()
  for (const hit of next) {
    const key = shareKey(hit.url)
    if (seen.has(key)) continue
    seen.add(key)
    out.push(hit)
  }
  return out
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

// hasMorePages says whether another page is worth asking for: an empty page
// means the keyword is exhausted, and the provider stops at its last page.
export function hasMorePages(page: number, received: number, maxPage: number): boolean {
  return received > 0 && page < maxPage
}
