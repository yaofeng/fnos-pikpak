import { useCallback, useEffect, useState } from 'react'
import { apiGet, apiPost } from '../api'
import { useAccounts } from '../AccountContext'
import type { AccountInfo } from '../accounts'
import { copyText } from '../clipboard'

type DeviceStart = {
  id: string
  user_code: string
  verify_url: string
  expires_in: number
  interval: number
}

type FlowState = {
  id: string
  status: 'pending' | 'bound' | 'denied' | 'expired' | 'error'
  user_code: string
  verify_url: string
  expires_at: string
  error: string
}

// announce tells the rest of the app that the account set changed: the
// provider refetches the list, and the shell resets its page caches and
// /api/me on the same event.
function announce() {
  window.dispatchEvent(new Event('pikpak:account-changed'))
}

// statusTag maps an account row to the badge wording and severity. A row with
// a probe error still works for most calls, so the badge stays green and the
// reason is shown as detail text instead.
function statusTag(acct: AccountInfo): { cls: string; text: string } {
  return acct.status === 'reauth_required'
    ? { cls: 'tag tag-warn', text: '需重新授权' }
    : { cls: 'tag tag-pass', text: '正常' }
}

export function BindCard() {
  const { accounts, reload } = useAccounts()
  const [flow, setFlow] = useState<DeviceStart | null>(null)
  const [state, setState] = useState<FlowState | null>(null)
  const [left, setLeft] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  // refresh follows every unbind and completed bind: the context re-reads
  // /api/accounts and every listener of the event catches up with it.
  const refresh = useCallback(async () => {
    await reload()
    announce()
  }, [reload])

  useEffect(() => {
    if (!flow) return
    const deadline = Date.now() + flow.expires_in * 1000
    const tick = setInterval(() => {
      setLeft(Math.max(0, Math.round((deadline - Date.now()) / 1000)))
    }, 1000)
    setLeft(flow.expires_in)
    return () => clearInterval(tick)
  }, [flow])

  useEffect(() => {
    if (!flow || !state || state.status !== 'pending') return
    const timer = setInterval(async () => {
      try {
        const next = await apiGet<FlowState>(`auth/flow/${flow.id}`)
        setState(next)
        if (next.status === 'bound') {
          setFlow(null)
          setState(null)
          await refresh()
        }
      } catch (e) {
        setError((e as Error).message)
      }
    }, 1500)
    return () => clearInterval(timer)
  }, [flow, state?.status, refresh])

  // startBind opens the device-code flow. With an account id it re-authorizes
  // that row (the server refuses a different PikPak identity); without one it
  // adds a new account.
  async function startBind(accountId?: string) {
    setError(null)
    setBusy(true)
    try {
      const start = await apiPost<DeviceStart>('auth/device', accountId ? { account_id: accountId } : {})
      setFlow(start)
      setState({
        id: start.id,
        status: 'pending',
        user_code: start.user_code,
        verify_url: start.verify_url,
        expires_at: '',
        error: '',
      })
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function unbind(accountId: string) {
    setError(null)
    try {
      await apiPost('auth/logout', { account_id: accountId })
      setFlow(null)
      setState(null)
      await refresh()
    } catch (e) {
      setError((e as Error).message)
    }
  }

  const pending = state?.status === 'pending'

  return (
    <section className="card">
      <div className="card-header">
        <h2>PikPak 账号</h2>
        <span className={accounts.length > 0 ? 'tag tag-pass' : 'tag tag-warn'}>
          {accounts.length > 0 ? `已绑定 ${accounts.length} 个` : '未绑定'}
        </span>
      </div>

      {error && <p className="error">{error}</p>}

      {accounts.length === 0 && !flow && (
        <p className="muted">尚未绑定。绑定后本应用才能访问你的 PikPak 文件。</p>
      )}

      {accounts.map((acct) => {
        const tag = statusTag(acct)
        return (
          // The row layout is inline: the stylesheet carries no account-card
          // rule, and adding one is another task's file.
          <div
            key={acct.id}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 10,
              padding: '10px 0',
              borderBottom: '1px solid var(--border)',
            }}
          >
            {acct.avatar ? (
              <img className="fav-avatar" src={acct.avatar} alt="" referrerPolicy="no-referrer" />
            ) : (
              <span className="fav-avatar blank" aria-hidden="true" />
            )}
            <div style={{ flex: 1, minWidth: 0 }}>
              <p>
                <strong>{acct.display_name || acct.id.slice(0, 6)}</strong>{' '}
                {acct.vip_type && <span className="muted small">{acct.vip_type}</span>}{' '}
                <span className={tag.cls}>{tag.text}</span>
              </p>
              {acct.error && <p className="muted small">{acct.error}</p>}
            </div>
            <div style={{ display: 'flex', gap: 8, flex: 'none' }}>
              <button type="button" className="link" disabled={pending} onClick={() => startBind(acct.id)}>
                重新授权
              </button>
              <button type="button" className="link" disabled={pending} onClick={() => unbind(acct.id)}>
                解绑
              </button>
            </div>
          </div>
        )
      })}

      {!flow && (
        <div className="actions">
          <button type="button" onClick={() => startBind()} disabled={busy}>
            {busy ? '申请中…' : '添加账号'}
          </button>
        </div>
      )}

      {flow && pending && (
        <div className="bind-flow">
          <p>在浏览器中打开下面的链接，输入或确认用户码：</p>
          <p className="user-code mono">{flow.user_code}</p>
          <p>
            <a href={flow.verify_url} target="_blank" rel="noreferrer">
              {flow.verify_url}
            </a>
            <button
              type="button"
              className="link"
              onClick={() => {
                void copyText(flow.verify_url).then((ok) => {
                  if (!ok) setError('复制失败，请手动选中链接复制')
                })
              }}
            >
              复制链接
            </button>
          </p>
          <p className="muted">
            剩余 {Math.floor(left / 60)}:{String(left % 60).padStart(2, '0')}，等待授权…
          </p>
        </div>
      )}

      {state && (state.status === 'denied' || state.status === 'expired' || state.status === 'error') && (
        <p className="error">
          {state.error || state.status}
          <button type="button" className="link" onClick={() => { setFlow(null); setState(null) }}>
            重试
          </button>
        </p>
      )}
    </section>
  )
}
