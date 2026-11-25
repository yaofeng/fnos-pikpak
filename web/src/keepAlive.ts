// The keep-alive layer's pure logic: which paths belong to a kept-alive page,
// the visited set that defers first mounts, and the per-account page key that
// remounts a page when the PikPak account changes. Kept in one module so the
// rules are testable without a DOM (vitest here is node-env).

// The nine pages that survive navigation inside the SPA. Bind (a flow page)
// and Debug (a lazy debug page) are deliberately not kept alive.
export const KEEP_ALIVE_PATHS = [
  '/files',
  '/recent',
  '/tasks',
  '/transfers',
  '/trash',
  '/favorites',
  '/video-favorites',
  '/search',
  '/settings',
] as const

// activePath maps a location pathname to the keep-alive page that owns it:
// /files and anything under /files/* belongs to /files (the folder tree
// navigates inside the path), the other eight pages match exactly, and
// everything else — including "/" before its redirect — owns nothing.
export function activePath(pathname: string): string | null {
  for (const page of KEEP_ALIVE_PATHS) {
    if (page === '/files') {
      if (pathname === '/files' || pathname.startsWith('/files/')) return page
      continue
    }
    if (pathname === page) return page
  }
  return null
}

// nextVisited adds the newly activated page to the visited set immutably: the
// previous set is never mutated, and an already-visited page returns the same
// set so React skips the state update entirely.
export function nextVisited(visited: ReadonlySet<string>, pathname: string): Set<string> {
  if (visited.has(pathname)) return visited as Set<string>
  const next = new Set(visited)
  next.add(pathname)
  return next
}

// pageKey scopes a kept-alive page's instance to one PikPak account: the key
// feeds the Activity child's React key, so switching accounts remounts the
// page and its UI state (folder stack, search terms) resets for the new
// account's data.
export function pageKey(page: string, accountID: string): string {
  return `${page}|${accountID}`
}
