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

export default function App() {
  const [health, setHealth] = useState<Health | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    apiGet<Health>('health')
      .then(setHealth)
      .catch((e: Error) => setError(e.message))
  }, [])

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

      <GatewayCard />
      <RangeCard />
      <ACLCard />
      <ProbesCard />
    </main>
  )
}
