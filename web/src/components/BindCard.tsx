import { useCallback, useEffect, useState } from 'react'
import { apiGet, apiPost } from '../api'
import { copyText } from '../clipboard'

type Me = { uid: number; bound: boolean }

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

export function BindCard({ onChange }: { onChange: () => void }) {
  const [me, setMe] = useState<Me | null>(null)
  const [flow, setFlow] = useState<DeviceStart | null>(null)
  const [state, setState] = useState<FlowState | null>(null)
  const [left, setLeft] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const loadMe = useCallback(async () => {
    try {
      setMe(await apiGet<Me>('me'))
    } catch (e) {
      setError((e as Error).message)
    }
  }, [])

  useEffect(() => {
    void loadMe()
  }, [loadMe])

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
    if (state.status !== 'pending') return
    const timer = setInterval(async () => {
      try {
        const next = await apiGet<FlowState>(`auth/flow/${flow.id}`)
        setState(next)
        if (next.status === 'bound') {
          setFlow(null)
          setState(null)
          await loadMe()
          onChange()
        }
      } catch (e) {
        setError((e as Error).message)
      }
    }, 1500)
    return () => clearInterval(timer)
  }, [flow, state?.status, loadMe, onChange])

  async function bind() {
    setError(null)
    setBusy(true)
    try {
      const start = await apiPost<DeviceStart>('auth/device', {})
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

  async function logout() {
    setError(null)
    try {
      await apiPost('auth/logout', {})
      setFlow(null)
      setState(null)
      await loadMe()
      onChange()
    } catch (e) {
      setError((e as Error).message)
    }
  }

  const pending = state?.status === 'pending'

  return (
    <section className="card">
      <div className="card-header">
        <h2>PikPak 账号</h2>
        {me?.bound && (
          <button type="button" onClick={logout}>
            解绑
          </button>
        )}
      </div>

      {error && <p className="error">{error}</p>}

      {!me && !error && <p className="muted">检查中…</p>}

      {me && me.bound && (
        <p className="ok">
          已绑定 PikPak 账号（飞牛 UID {me.uid}）。
          {!flow && (
            <button type="button" className="link" onClick={bind} disabled={busy}>
              重新授权
            </button>
          )}
        </p>
      )}

      {me && !me.bound && !flow && (
        <>
          <p className="muted">尚未绑定。绑定后本应用才能访问你的 PikPak 文件。</p>
          <button type="button" onClick={bind} disabled={busy}>
            {busy ? '申请中…' : '绑定 PikPak'}
          </button>
        </>
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
