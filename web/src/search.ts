// Web-search result types and the small pieces of copy-free logic the 全网搜索
// page uses. The server owns the query: it adds the site: filter, so nothing
// here ever builds one.
export type WebResult = { title: string; url: string; snippet: string }
export type WebSearchPage = { items: WebResult[]; query: string; page: number }

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
