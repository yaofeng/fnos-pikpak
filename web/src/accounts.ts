import { apiPost } from './api'
import { getLang, makeT, type Lang } from './i18n'

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

// vipTypeLabel maps one account's upstream vip_type to user-visible copy.
// Tiers the backend has never seen keep their upstream word under a VIP
// prefix, so an unknown tier still reads as a tier rather than as gibberish.
export function vipTypeLabel(vipType: string, lang?: Lang): string {
  if (!vipType) return ''
  const t = makeT(lang ?? getLang())
  if (vipType === 'platinum') return t('account.vip_platinum')
  if (vipType === 'gold') return t('account.vip_gold')
  if (vipType === 'NON-member') return ''
  return `VIP·${vipType}`
}

// resolveCurrentId keeps the in-memory selection honest: once the account it
// names is unbound (or nothing was selected yet), the first remaining account
// becomes current — the same default the backend resolves a bare uid to.
export function resolveCurrentId(items: AccountInfo[], currentId: string): string {
  if (currentId && items.some((item) => item.id === currentId)) return currentId
  return items[0]?.id ?? ''
}

// validateDisplayName mirrors the rename endpoint's rules so the inline
// rename edit rejects before the request: trim, non-empty, at most 50 code
// points. The error copy comes from the i18n table; the process-wide language
// applies unless the caller passes one.
export function validateDisplayName(
  raw: string,
  lang?: Lang,
): { ok: true; name: string } | { ok: false; error: string } {
  const name = raw.trim()
  const t = makeT(lang ?? getLang())
  if (!name) return { ok: false, error: t('account.name_empty') }
  if ([...name].length > 50) return { ok: false, error: t('account.name_too_long') }
  return { ok: true, name }
}

// accountLabel names the account one merged row or mutation response refers
// to, the same way everywhere: a name that arrived with the response wins,
// then the bound account's display name, then the id's first six characters
// for an id no bound row knows, and 「默认账号」 for the legacy rows that
// carry no id at all.
export function accountLabel(
  byId: Map<string, AccountInfo> | undefined,
  id: string | undefined,
  name?: string,
  lang?: Lang,
): string {
  if (name) return name
  if (!id) return makeT(lang ?? getLang())('account.default_label')
  return byId?.get(id)?.display_name || id.slice(0, 6)
}

// renameAccount POSTs the rename endpoint and returns the refreshed row, so
// the caller can patch one entry of its list without a full reload.
export async function renameAccount(id: string, displayName: string): Promise<AccountInfo> {
  const body = await apiPost<{ item: AccountInfo }>(
    `accounts/${encodeURIComponent(id)}/rename`,
    { display_name: displayName },
  )
  return body.item
}
