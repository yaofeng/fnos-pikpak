// AccountInfo is one row of GET /api/accounts: identity and VIP fields from
// the stored account, quota from the cached probe snapshot, and a non-empty
// error when that account's probe or session failed (its quotas are then zero
// and must not enter the combined pill).
export type AccountInfo = {
  id: string
  display_name: string
  avatar: string
  vip_type: string
  vip_expire: string
  status: string
  quota_limit: number
  quota_usage: number
  error?: string
}

// sumQuota folds the account rows into one quota pair for the sidebar pill.
// A row carrying an error has no trustworthy numbers, so it is skipped rather
// than counted as zero usage on zero limit.
export function sumQuota(items: AccountInfo[]): { usage: number; limit: number } {
  let usage = 0
  let limit = 0
  for (const item of items) {
    if (item.error) continue
    usage += item.quota_usage
    limit += item.quota_limit
  }
  return { usage, limit }
}

// worstStatus boils the account list down to the shell's one status word: a
// single account needing re-authorization wins over any number of healthy
// ones; nothing bound reads as unbound.
export function worstStatus(items: AccountInfo[]): string {
  if (items.length === 0) return 'unbound'
  return items.some((item) => item.status === 'reauth_required') ? 'reauth_required' : 'active'
}

// resolveCurrentId keeps the in-memory selection honest: once the account it
// names is unbound (or nothing was selected yet), the first remaining account
// becomes current — the same default the backend resolves a bare uid to.
export function resolveCurrentId(items: AccountInfo[], currentId: string): string {
  if (currentId && items.some((item) => item.id === currentId)) return currentId
  return items[0]?.id ?? ''
}
