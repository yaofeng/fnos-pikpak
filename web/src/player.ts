// Browsers decide playability from the MIME type. PikPak sometimes answers
// application/octet-stream, which no browser can classify, so fall back to the
// extension before giving up.
const mimeByExtension: Record<string, string> = {
  '.mp4': 'video/mp4',
  '.m4v': 'video/mp4',
  '.webm': 'video/webm',
  '.mkv': 'video/x-matroska',
  '.mov': 'video/quicktime',
  '.avi': 'video/x-msvideo',
  '.ts': 'video/mp2t',
  '.flv': 'video/x-flv',
}

export function guessMime(name: string, upstream: string): string {
  if (upstream && upstream !== 'application/octet-stream') return upstream
  const dot = name.lastIndexOf('.')
  if (dot >= 0) {
    const mime = mimeByExtension[name.slice(dot).toLowerCase()]
    if (mime) return mime
  }
  return upstream || 'application/octet-stream'
}

export function videoExtensions(): string[] {
  return Object.keys(mimeByExtension)
}

// playPath addresses the play-info route for a drive file. An account id
// scopes the route to that account's drive — without it the server resolves
// the default account, which is right only for the default account's files.
export function playPath(fileID: string, account?: string): string {
  return account ? `play/${fileID}?account=${encodeURIComponent(account)}` : `play/${fileID}`
}

// formatClock renders seconds as m:ss / h:mm:ss for the player footer.
export function formatClock(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '0:00'
  const total = Math.floor(seconds)
  const s = total % 60
  const m = Math.floor(total / 60) % 60
  const h = Math.floor(total / 3600)
  const mm = h > 0 ? String(m).padStart(2, '0') : String(m)
  return h > 0 ? `${h}:${mm}:${String(s).padStart(2, '0')}` : `${mm}:${String(s).padStart(2, '0')}`
}
