// listCache keeps the last listing each page loaded, so switching tabs does not
// re-query PikPak. Entries are replaced by an explicit 刷新 or by the mutation
// that changed the data; there is no timer, because a NAS file list does not go
// stale on its own.
const entries = new Map<string, unknown>()

export function readCache<T>(key: string): T | undefined {
  return entries.get(key) as T | undefined
}

export function writeCache<T>(key: string, value: T) {
  entries.set(key, value)
}

export function dropCache(key: string) {
  entries.delete(key)
}

export function dropCachePrefix(prefix: string) {
  for (const key of [...entries.keys()]) {
    if (key.startsWith(prefix)) entries.delete(key)
  }
}

export function resetCache() {
  entries.clear()
}

// cacheKey builds the keys the pages use, in one place so invalidation can be
// written against the same shape.
export const cacheKeys = {
  files: (folderId: string, kind: string) => `files:${folderId}|${kind}`,
  recent: () => 'recent',
  trash: () => 'trash',
}
