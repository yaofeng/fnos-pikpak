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

export function dropCachePrefix(prefix: string) {
  for (const key of [...entries.keys()]) {
    if (key.startsWith(prefix)) entries.delete(key)
  }
}

export function resetCache() {
  entries.clear()
}

// cacheKey builds the keys the pages use, in one place so invalidation can be
// written against the same shape. Every key leads with the account id: two
// accounts' listings must never share an entry, because a read through the
// other account's key is the one way stale rows could cross accounts.
export const cacheKeys = {
  files: (acct: string, folderId: string, kind: string) => `files:${acct}:${folderId}|${kind}`,
  recent: () => 'recent:merged',
  trash: (acct: string) => `trash:${acct}`,
}
