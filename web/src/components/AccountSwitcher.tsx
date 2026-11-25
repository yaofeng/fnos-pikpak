import type { AccountInfo } from '../accounts'
import { getLang, makeT } from '../i18n'

// accountLabel is the name-only option text: the stored label the user set,
// else the id's first six characters. VIP tier, usage and status used to ride
// along here; they live in the settings account section and the shell's
// reauth banners now, so the picker reads as a plain list of names.
function accountLabel(a: AccountInfo): string {
  return a.display_name || a.id.slice(0, 6)
}

// AccountSwitcher is the account picker the 我的文件 and 回收站 toolbars open
// with: its value is the context's scope — the fixed first option 'all'
// (-全部帐号-) is the merged view, the rest pick one account. With fewer than
// two accounts there is nothing to choose, so it renders nothing and a
// single-account user never learns the switcher exists.
export default function AccountSwitcher({ accounts, scope, onChange }: {
  accounts: AccountInfo[]
  scope: string
  onChange(id: string): void
}) {
  if (accounts.length < 2) return null
  const t = makeT(getLang())
  return (
    <label className="fm-pick">
      <select value={scope} onChange={(e) => onChange(e.target.value)} aria-label={t('account.switch_label')}>
        <option value="all">{t('account.all_accounts')}</option>
        {accounts.map((a) => (
          <option key={a.id} value={a.id}>
            {accountLabel(a)}
          </option>
        ))}
      </select>
    </label>
  )
}
