// IINA opens iina:// links on macOS only. The PikPak app runs inside fnOS,
// which is Linux, so this branch exists for people who open the web UI on a
// Mac; everywhere else the built-in player takes over.
export function isMacOS(ua: string): boolean {
  return /Macintosh|Mac OS X/.test(ua)
}

// iinaURL turns a signed stream URL into the weblink form IINA registers:
// iina://weblink?url=<percent-encoded URL>.
export function iinaURL(streamURL: string): string {
  return 'iina://weblink?url=' + encodeURIComponent(streamURL)
}
