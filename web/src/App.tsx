import { TrimApp } from '@trimjs/web-app'
import { useCallback, useEffect, useState } from 'react'
import { apiGet, apiPost } from './api'

type Health = { ok: boolean; version: string; uid: number }

type ProbeResult = {
  name: string
  status: 'pass' | 'fail' | 'warn' | 'skip'
  detail?: string
  data?: Record<string, unknown>
}

type ACLResult = {
  path: string
  readable: boolean
  writable: boolean
  deletable: boolean
}

type ACLProbeResponse = {
  folders: string[]
  acl: ACLResult[]
  semantic?: { path: string; semanticPath: string }[]
  under: Record<string, boolean>
  writable_ok: boolean
  writable_error?: string
  semantic_error?: string
}

const sdk = new TrimApp()

function StatusTag({ status }: { status: ProbeResult['status'] }) {
  return <span className={`tag tag-${status}`}>{status}</span>
}

type GatewayInfo = {
  path: string
  raw_query: string
  origin: string
  referer: string
  host: string
  forwarded_headers: Record<string, number>
  identity: { uid: number; username: string; is_admin: boolean }
}

function GatewayCard() {
  const [info, setInfo] = useState<GatewayInfo | null>(null)
  const [forged, setForged] = useState<{ header_count: number; duplicate_detected: boolean } | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      setInfo(await apiGet<GatewayInfo>('gateway/info?probe=1'))
    } catch (e) {
      setError((e as Error).message)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function testForged() {
    setError(null)
    try {
      const res = await fetch(`api/gateway/forged?probe=${Date.now()}`, {
        method: 'POST',
        headers: { 'X-Trim-Userid': '99999' },
      })
      setForged(await res.json())
    } catch (e) {
      setError((e as Error).message)
    }
  }

  return (
    <section className="card">
      <div className="card-header">
        <h2>网关信息</h2>
        <div className="actions">
          <button type="button" onClick={load}>
            刷新
          </button>
          <button type="button" onClick={testForged}>
            伪造身份头测试
          </button>
        </div>
      </div>

      {error && <p className="error">{error}</p>}
      {info && (
        <dl className="kv">
          <dt>路径</dt>
          <dd className="mono">{info.path}</dd>
          <dt>查询串</dt>
          <dd className="mono">{info.raw_query}</dd>
          <dt>Origin</dt>
          <dd className="mono">{info.origin || '（空）'}</dd>
          <dt>Host</dt>
          <dd className="mono">{info.host}</dd>
          <dt>身份</dt>
          <dd className="mono">
            uid={info.identity.uid} user={info.identity.username || '（空）'} admin=
            {String(info.identity.is_admin)}
          </dd>
        </dl>
      )}

      {info && (
        <table className="table">
          <thead>
            <tr>
              <th>请求头</th>
              <th>出现次数</th>
            </tr>
          </thead>
          <tbody>
            {Object.entries(info.forwarded_headers).map(([name, count]) => (
              <tr key={name}>
                <td className="mono">{name}</td>
                <td>{count}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {forged && (
        <p className={forged.duplicate_detected ? 'error' : 'ok'}>
          X-Trim-Userid 出现 {forged.header_count} 次，
          {forged.duplicate_detected
            ? '网关保留了客户端伪造的副本（需要依赖重复头拒绝）。'
            : '网关覆盖或剥离了客户端伪造的头。'}
        </p>
      )}
    </section>
  )
}

type RangeRow = {
  label: string
  status: number | string
  contentRange: string
  ttfb: string
  total: string
  bytes: number | string
}

function RangeCard() {
  const [rows, setRows] = useState<RangeRow[]>([])
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function run() {
    setRunning(true)
    setError(null)
    const size = 64 << 20
    const cases = [
      { label: '开头 1 KiB', range: 'bytes=0-1023' },
      { label: '中间 1 MiB', range: `bytes=${size / 2}-${size / 2 + (1 << 20) - 1}` },
      { label: '尾部 1 KiB', range: `bytes=${size - 1024}-${size - 1}` },
      { label: '超出 EOF', range: `bytes=${size + 1024}-${size + 2048}` },
    ]
    const out: RangeRow[] = []

    try {
      for (const c of cases) {
        const t0 = performance.now()
        const res = await fetch(`api/stream-probe?size=${size}&t=${Date.now()}`, {
          headers: { Range: c.range },
        })
        const ttfb = performance.now() - t0
        const body = await res.arrayBuffer()
        const total = performance.now() - t0
        out.push({
          label: c.label,
          status: res.status,
          contentRange: res.headers.get('Content-Range') ?? '—',
          ttfb: ttfb.toFixed(0),
          total: total.toFixed(0),
          bytes: body.byteLength,
        })
      }

      const t0 = performance.now()
      const head = await fetch(`api/stream-probe?size=${size}&t=${Date.now()}`, { method: 'HEAD' })
      const headTTFB = performance.now() - t0
      out.push({
        label: 'HEAD',
        status: head.status,
        contentRange: head.headers.get('Content-Range') ?? '—',
        ttfb: headTTFB.toFixed(0),
        total: (performance.now() - t0).toFixed(0),
        bytes: head.headers.get('Content-Length') ?? '—',
      })
      setRows(out)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setRunning(false)
    }
  }

  return (
    <section className="card">
      <div className="card-header">
        <h2>Range 探针</h2>
        <button type="button" onClick={run} disabled={running}>
          {running ? '测试中…' : '测试 64 MiB 文件'}
        </button>
      </div>
      <p className="muted">
        判断网关是否原样转发 206/Range、是否缓冲响应。首字节耗时明显接近总耗时说明整段被缓冲。
      </p>
      {error && <p className="error">{error}</p>}
      {rows.length > 0 && (
        <table className="table">
          <thead>
            <tr>
              <th>用例</th>
              <th>状态</th>
              <th>Content-Range</th>
              <th>首字节 ms</th>
              <th>总耗时 ms</th>
              <th>字节</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.label}>
                <td>{r.label}</td>
                <td>{r.status}</td>
                <td className="mono">{r.contentRange}</td>
                <td>{r.ttfb}</td>
                <td>{r.total}</td>
                <td>{r.bytes}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}

function ProbesCard() {
  const [results, setResults] = useState<ProbeResult[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [running, setRunning] = useState(false)

  async function run() {
    setRunning(true)
    setError(null)
    try {
      const res = await apiPost<{ results: ProbeResult[] }>('probes/run', {})
      setResults(res.results)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setRunning(false)
    }
  }

  return (
    <section className="card">
      <div className="card-header">
        <h2>探针</h2>
        <div className="actions">
          <button type="button" onClick={run} disabled={running}>
            {running ? '运行中…' : '运行全部探针'}
          </button>
          <a className="button" href="api/report" download>
            导出报告
          </a>
        </div>
      </div>

      {error && <p className="error">{error}</p>}
      {!results && !error && <p className="muted">尚未运行。</p>}
      {results && (
        <table className="table">
          <thead>
            <tr>
              <th>名称</th>
              <th>结果</th>
              <th>详情</th>
            </tr>
          </thead>
          <tbody>
            {results.map((r) => (
              <tr key={r.name}>
                <td className="mono">{r.name}</td>
                <td>
                  <StatusTag status={r.status} />
                </td>
                <td>
                  {r.detail ?? ''}
                  {r.data && (
                    <details>
                      <summary>数据</summary>
                      <pre>{JSON.stringify(r.data, null, 2)}</pre>
                    </details>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}

function ACLCard() {
  const [standalone, setStandalone] = useState(false)
  const [picked, setPicked] = useState<string[]>([])
  const [result, setResult] = useState<ACLProbeResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    sdk
      .ready()
      .then(() => setStandalone(sdk.isStandaloneWeb))
      .catch(() => setStandalone(true))
  }, [])

  const pick = useCallback(async () => {
    setError(null)
    if (sdk.isStandaloneWeb) {
      setError('请在飞牛桌面内打开本应用后再选择目录。')
      return
    }
    try {
      const res = await sdk.pickUserFile({ directory: true })
      const paths = res?.data ?? []
      if (paths.length === 0) return
      setPicked(paths)
      setResult(await apiPost<ACLProbeResponse>('fs/probe-acl', { paths, language: 'zh-CN' }))
    } catch (e) {
      setError((e as Error).message)
    }
  }, [])

  return (
    <section className="card">
      <div className="card-header">
        <h2>文件授权探针</h2>
        <button type="button" onClick={pick} disabled={standalone}>
          选择飞牛目录
        </button>
      </div>
      {standalone && <p className="muted">当前不在飞牛桌面内，目录选择不可用。</p>}
      {error && <p className="error">{error}</p>}
      {picked.length > 0 && (
        <p className="muted">
          已选择：<span className="mono">{picked.join(', ')}</span>
        </p>
      )}
      {result && (
        <>
          <h3>该用户的授权目录</h3>
          <ul className="mono">
            {result.folders.map((f) => (
              <li key={f}>{f}</li>
            ))}
          </ul>

          <h3>ACL</h3>
          <table className="table">
            <thead>
              <tr>
                <th>路径</th>
                <th>可读</th>
                <th>可写</th>
                <th>可删</th>
                <th>授权集合内</th>
              </tr>
            </thead>
            <tbody>
              {result.acl.map((a) => (
                <tr key={a.path}>
                  <td className="mono">{a.path}</td>
                  <td>{String(a.readable)}</td>
                  <td>{String(a.writable)}</td>
                  <td>{String(a.deletable)}</td>
                  <td>{String(result.under[a.path] ?? false)}</td>
                </tr>
              ))}
            </tbody>
          </table>

          <h3>显示路径</h3>
          {result.semantic_error && <p className="error">{result.semantic_error}</p>}
          <ul>
            {(result.semantic ?? []).map((s) => (
              <li key={s.path}>
                <span className="mono">{s.path}</span> → {s.semanticPath}
              </li>
            ))}
          </ul>

          <p className={result.writable_ok ? 'ok' : 'error'}>
            {result.writable_ok ? '可写：是（可以下载到这个目录）' : `可写：否（${result.writable_error}）`}
          </p>
        </>
      )}
    </section>
  )
}

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

function BindCard({ onChange }: { onChange: () => void }) {
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

      {me && me.bound && <p className="ok">已绑定 PikPak 账号（飞牛 UID {me.uid}）。</p>}

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

type UploadConfig = { generated_path: string; generated_size: number; bound: boolean }

type UploadReport = {
  local_path: string
  size: number
  gcid: string
  gcid_ms: number
  instant: boolean
  provider?: string
  param_keys?: string[]
  upload_ms: number
  wait_ms: number
  total_ms: number
  file?: { id: string; name: string; size: number; phase: string }
}

function UploadCard({ refresh }: { refresh: number }) {
  const [config, setConfig] = useState<UploadConfig | null>(null)
  const [bound, setBound] = useState(false)
  const [parentId, setParentId] = useState('')
  const [report, setReport] = useState<UploadReport | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [standalone, setStandalone] = useState(false)

  useEffect(() => {
    sdk
      .ready()
      .then(() => setStandalone(sdk.isStandaloneWeb))
      .catch(() => setStandalone(true))
    apiGet<UploadConfig>('upload/probe/config')
      .then((c) => {
        setConfig(c)
        setBound(c.bound)
      })
      .catch((e: Error) => setError(e.message))
  }, [refresh])

  async function upload(localPath: string) {
    setBusy(true)
    setError(null)
    setReport(null)
    try {
      setReport(
        await apiPost<UploadReport>('upload/probe', {
          local_path: localPath,
          parent_id: parentId.trim(),
        }),
      )
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function pickAndUpload() {
    setError(null)
    if (sdk.isStandaloneWeb) {
      setError('请在飞牛桌面内打开本应用后再选择文件。')
      return
    }
    try {
      const res = await sdk.pickUserFile({ directory: false })
      const paths = res?.data ?? []
      if (paths.length === 0) return
      await upload(paths[0])
    } catch (e) {
      setError((e as Error).message)
    }
  }

  return (
    <section className="card">
      <div className="card-header">
        <h2>上传探针</h2>
      </div>
      {!bound && <p className="muted">先绑定 PikPak 账号再上传。</p>}
      {error && <p className="error">{error}</p>}
      {config && bound && (
        <>
          <label className="field">
            <span>目标文件夹 id（留空为根目录）</span>
            <input
              type="text"
              value={parentId}
              onChange={(e) => setParentId(e.target.value)}
              placeholder="folder id"
            />
          </label>
          <div className="actions">
            <button type="button" onClick={() => upload(config.generated_path)} disabled={busy}>
              {busy ? '上传中…' : `上传生成文件（${Math.round(config.generated_size / 1048576)} MiB）`}
            </button>
            <button type="button" onClick={pickAndUpload} disabled={busy || standalone}>
              上传飞牛文件
            </button>
          </div>
        </>
      )}
      {report && (
        <dl className="kv">
          <dt>文件</dt>
          <dd className="mono">{report.file?.name ?? report.local_path}</dd>
          <dt>大小</dt>
          <dd>{report.size} 字节</dd>
          <dt>GCID</dt>
          <dd className="mono">{report.gcid}</dd>
          <dt>GCID 耗时</dt>
          <dd>{report.gcid_ms} ms</dd>
          <dt>秒传</dt>
          <dd>{report.instant ? '是（无需上传）' : '否'}</dd>
          {report.provider && (
            <>
              <dt>Provider</dt>
              <dd className="mono">{report.provider}</dd>
              <dt>params 键</dt>
              <dd className="mono">{(report.param_keys ?? []).join(', ')}</dd>
            </>
          )}
          <dt>上传耗时</dt>
          <dd>{report.upload_ms} ms</dd>
          <dt>等待完成</dt>
          <dd>{report.wait_ms} ms</dd>
          <dt>总耗时</dt>
          <dd>{report.total_ms} ms</dd>
          <dt>最终状态</dt>
          <dd className="mono">
            {report.file?.phase ?? '—'} {report.file?.id ? `（${report.file.id}）` : ''}
          </dd>
        </dl>
      )}
    </section>
  )
}

export default function App() {
  const [health, setHealth] = useState<Health | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [refresh, setRefresh] = useState(0)

  const loadHealth = useCallback(() => {
    apiGet<Health>('health')
      .then(setHealth)
      .catch((e: Error) => setError(e.message))
  }, [])

  useEffect(() => {
    loadHealth()
  }, [loadHealth])

  return (
    <main className="page">
      <header className="page-header">
        <h1>PikPak</h1>
        <span className="muted">M0 验证版</span>
      </header>

      <section className="card">
        <h2>服务状态</h2>
        {error && <p className="error">{error}</p>}
        {!error && !health && <p className="muted">检查中…</p>}
        {health && (
          <dl className="kv">
            <dt>版本</dt>
            <dd>{health.version}</dd>
            <dt>飞牛用户 UID</dt>
            <dd>{health.uid}</dd>
          </dl>
        )}
      </section>

      <BindCard onChange={() => { loadHealth(); setRefresh((n) => n + 1) }} />
      <GatewayCard />
      <RangeCard />
      <UploadCard refresh={refresh} />
      <ACLCard />
      <ProbesCard />
    </main>
  )
}
