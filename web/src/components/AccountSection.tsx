import { useCallback, useEffect, useState } from 'react'
import { apiGet, apiPost } from '../api'
import { useAccounts } from '../AccountContext'
import { renameAccount, validateDisplayName, type AccountInfo } from '../accounts'
import { copyText } from '../clipboard'
import { unbindWarning } from '../settings'

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
// /api/me on the same event. A rename keeps the set intact, so it reloads the
// context only — the cached per-page rows are keyed by account id, not name.
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

// FlowPanel is the device-code progress block: the verify link with its copy
// button, the user code, the countdown while pending — and the failure line
// with its retry once the flow ends in denied/expired/error.
function FlowPanel(props: {
  flow: DeviceStart
  state: FlowState
  left: number
  onRetry(): void
  onCopyFail(): void
}) {
  const { flow, state, left, onRetry, onCopyFail } = props
  const pending = state.status === 'pending'
  return (
    <>
      {pending && (
        <div className="bind-flow">
          <p>在浏览器中打开下面的链接，输入或确认用户码：</p>
          <p className="user-code mono">{flow.user_code}</p>
          <p>
            <a href={flow.verify_url} target="_blank" rel="noreferrer">
              {flow.verify_url}
            </a>
            <button type="button" className="link" onClick={onCopyFail}>
              复制链接
            </button>
          </p>
          <p className="muted">
            剩余 {Math.floor(left / 60)}:{String(left % 60).padStart(2, '0')}，等待授权…
          </p>
        </div>
      )}
      {(state.status === 'denied' || state.status === 'expired' || state.status === 'error') && (
        <p className="error">
          {state.error || state.status}
          <button type="button" className="link" onClick={onRetry}>
            重试
          </button>
        </p>
      )}
    </>
  )
}

// AccountSection is the account card the 设置 page renders directly: the
// title row carries the bound-count badge and the 添加账号 button at its far
// right, and each row offers 重命名 / 重新授权 / 解绑. There is no separate
// bind page any more — adding or re-authorizing expands the device-code flow
// right under the row (or under the empty state) it was started from.
export function AccountSection() {
  const { accounts, reload } = useAccounts()
  const [flow, setFlow] = useState<DeviceStart | null>(null)
  // Which row the flow belongs to: '' adds a new account, an account id
  // re-authorizes that row, null means no flow is running.
  const [flowTarget, setFlowTarget] = useState<string | null>(null)
  const [state, setState] = useState<FlowState | null>(null)
  const [left, setLeft] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [draft, setDraft] = useState('')
  const [renameError, setRenameError] = useState('')
  const [confirmId, setConfirmId] = useState<string | null>(null)

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
          setFlowTarget(null)
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
      setFlowTarget(accountId ?? '')
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
      setConfirmId(null)
      setFlow(null)
      setFlowTarget(null)
      setState(null)
      await refresh()
    } catch (e) {
      setError((e as Error).message)
    }
  }

  // beginRename pre-fills the inline edit with the row's current label — the
  // same accountLabel the switcher shows.
  function beginRename(acct: AccountInfo) {
    setRenameError('')
    setEditingId(acct.id)
    setDraft(acct.display_name || acct.id.slice(0, 6))
  }

  function cancelRename() {
    setEditingId(null)
    setDraft('')
    setRenameError('')
  }

  // submitRename validates with the endpoint's own rules before the request;
  // blur after a successful Enter submit is a no-op because editingId is
  // already null.
  async function submitRename(accountId: string) {
    if (editingId !== accountId) return
    const check = validateDisplayName(draft)
    if (!check.ok) {
      setRenameError(check.error)
      return
    }
    try {
      await renameAccount(accountId, check.name)
      cancelRename()
      await reload()
    } catch (e) {
      setRenameError((e as Error).message)
    }
  }

  const pending = state?.status === 'pending'
  const flowPanel =
    flow && state ? (
      <FlowPanel
        flow={flow}
        state={state}
        left={left}
        onRetry={() => {
          setFlow(null)
          setFlowTarget(null)
          setState(null)
        }}
        onCopyFail={() => {
          void copyText(flow.verify_url).then((ok) => {
            if (!ok) setError('复制失败，请手动选中链接复制')
          })
        }}
      />
    ) : null

  return (
    <section className="card">
      <div className="card-header">
        <h2>PikPak 账号</h2>
        <span style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 10 }}>
          <span className={accounts.length > 0 ? 'tag tag-pass' : 'tag tag-warn'}>
            {accounts.length > 0 ? `已绑定 ${accounts.length} 个` : '未绑定'}
          </span>
          <button type="button" onClick={() => startBind()} disabled={busy || !!flow}>
            {busy ? '申请中…' : '添加账号'}
          </button>
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
          <div key={acct.id}>
            <div
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
                  {editingId === acct.id ? (
                    // Inline rename: pre-filled, Enter or blur commits, Esc
                    // cancels.
                    <input
                      autoFocus
                      value={draft}
                      aria-label="账号名称"
                      onChange={(e) => setDraft(e.target.value)}
                      onBlur={() => void submitRename(acct.id)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault()
                          void submitRename(acct.id)
                        } else if (e.key === 'Escape') {
                          cancelRename()
                        }
                      }}
                    />
                  ) : (
                    <strong>{acct.display_name || acct.id.slice(0, 6)}</strong>
                  )}{' '}
                  {acct.vip_type && <span className="muted small">{acct.vip_type}</span>}{' '}
                  <span className={tag.cls}>{tag.text}</span>
                </p>
                {renameError && editingId === acct.id && <p className="error small">{renameError}</p>}
                {acct.error && <p className="muted small">{acct.error}</p>}
              </div>
              <div style={{ display: 'flex', gap: 8, flex: 'none' }}>
                <button
                  type="button"
                  className="link"
                  disabled={pending || editingId === acct.id}
                  onClick={() => beginRename(acct)}
                >
                  重命名
                </button>
                <button type="button" className="link" disabled={pending} onClick={() => startBind(acct.id)}>
                  重新授权
                </button>
                <button
                  type="button"
                  className="link"
                  disabled={pending}
                  onClick={() => setConfirmId(acct.id)}
                >
                  解绑
                </button>
              </div>
            </div>
            {flow && flowTarget === acct.id && flowPanel}
          </div>
        )
      })}

      {flow && flowTarget === '' && flowPanel}

      {confirmId && (
        <div className="modal-backdrop" onClick={() => setConfirmId(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h2>确认解绑</h2>
            <p>{unbindWarning}</p>
            <div className="actions">
              <button type="button" className="danger" onClick={() => void unbind(confirmId)}>
                确认解绑
              </button>
              <button type="button" onClick={() => setConfirmId(null)}>
                取消
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  )
}
