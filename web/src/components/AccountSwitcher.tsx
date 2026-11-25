import type { AccountInfo } from '../accounts'
import { formatSize } from '../files'

type Props = {
  accounts: AccountInfo[]
  currentId: string
  onChange(id: string): void
}

// accountLabel composes one option's text: who the account is, its VIP mark,
// its own usage against its own limit, and a 需重新授权 note for a dead
// authorization. The note only informs — the account stays selectable, because
// opening it is what triggers the re-authorization prompt.
function accountLabel(a: AccountInfo): string {
  const parts = [a.display_name || a.id.slice(0, 6)]
  if (a.vip_type) parts.push(a.vip_type)
  parts.push(`${formatSize(a.quota_usage)} / ${formatSize(a.quota_limit)}`)
  if (a.status === 'reauth_required') parts.push('需重新授权')
  return parts.join(' · ')
}

// AccountSwitcher is the account picker the 我的文件 and 回收站 toolbars open
// with: every request those pages make is scoped to the chosen account. With
// fewer than two accounts there is nothing to choose, so it renders nothing
// and a single-account user never learns the switcher exists.
export default function AccountSwitcher({ accounts, currentId, onChange }: Props) {
  if (accounts.length < 2) return null
  return (
    <label className="fm-pick">
      <select value={currentId} onChange={(e) => onChange(e.target.value)} aria-label="切换账号">
        {accounts.map((a) => (
          <option key={a.id} value={a.id}>
            {accountLabel(a)}
          </option>
        ))}
      </select>
    </label>
  )
}
