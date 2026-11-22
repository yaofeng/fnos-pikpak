type BridgeResponse = { code?: number; data?: string[] }

// pathsFrom keeps the absolute paths a picker granted, dropping duplicates,
// relative entries and empty strings. A cancelled picker comes back as
// undefined or a non-zero code and yields nothing.
export function pathsFrom(resp: BridgeResponse | undefined | null): string[] {
  if (!resp || (resp.code !== undefined && resp.code !== 0) || !Array.isArray(resp.data)) return []
  const seen = new Set<string>()
  const out: string[] = []
  for (const p of resp.data) {
    if (typeof p !== 'string' || !p.startsWith('/') || seen.has(p)) continue
    seen.add(p)
    out.push(p)
  }
  return out
}
