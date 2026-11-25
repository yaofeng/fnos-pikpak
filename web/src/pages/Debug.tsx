import { TrimApp } from '@trimjs/web-app'
import { useCallback, useEffect, useState } from 'react'
import { apiGet, apiPost } from '../api'
import { getLang, makeT } from '../i18n'
import { AccountSection } from '../components/AccountSection'

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
  const t = makeT(getLang())
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
        <h2>{t('debug.gateway')}</h2>
        <div className="actions">
          <button type="button" onClick={load}>
            {t('common.refresh')}
          </button>
          <button type="button" onClick={testForged}>
            {t('debug.forged_test')}
          </button>
        </div>
      </div>

      {error && <p className="error">{error}</p>}
      {info && (
        <dl className="kv">
          <dt>{t('debug.path')}</dt>
          <dd className="mono">{info.path}</dd>
          <dt>{t('debug.query')}</dt>
          <dd className="mono">{info.raw_query}</dd>
          <dt>Origin</dt>
          <dd className="mono">{info.origin || t('debug.empty')}</dd>
          <dt>Host</dt>
          <dd className="mono">{info.host}</dd>
          <dt>{t('debug.identity')}</dt>
          <dd className="mono">
            uid={info.identity.uid} user={info.identity.username || t('debug.empty')} admin=
            {String(info.identity.is_admin)}
          </dd>
        </dl>
      )}

      {info && (
        <table className="table">
          <thead>
            <tr>
              <th>{t('debug.th_header')}</th>
              <th>{t('debug.th_count')}</th>
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
          {t('debug.forged_count').replace('{n}', String(forged.header_count))}
          {forged.duplicate_detected ? t('debug.forged_kept') : t('debug.forged_stripped')}
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

// rangeCases builds the probe list from a translator, at module scope so the
// component body stays free of nested-call purity noise.
function rangeCases(tr: (key: string) => string): { label: string; range: string }[] {
  const size = 64 << 20
  return [
    { label: tr('debug.range_head'), range: 'bytes=0-1023' },
    { label: tr('debug.range_middle'), range: `bytes=${size / 2}-${size / 2 + (1 << 20) - 1}` },
    { label: tr('debug.range_tail'), range: `bytes=${size - 1024}-${size - 1}` },
    { label: tr('debug.range_eof'), range: `bytes=${size + 1024}-${size + 2048}` },
  ]
}


// runRangeProbe times the Range requests from the module scope: the clock
// calls are probe logic, not render work, and keeping them out of the
// component avoids nested-async purity noise.
async function runRangeProbe(tr: (key: string) => string): Promise<RangeRow[]> {
  const size = 64 << 20
  const cases = rangeCases(tr)
  const out: RangeRow[] = []

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
  return out
}

function RangeCard() {
  const tr = makeT(getLang())
  const [rows, setRows] = useState<RangeRow[]>([])
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function run() {
    setRunning(true)
    setError(null)
    try {
      setRows(await runRangeProbe(tr))
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setRunning(false)
    }
  }

  return (
    <section className="card">
      <div className="card-header">
        <h2>{tr('debug.range_probe')}</h2>
        <button type="button" onClick={run} disabled={running}>
          {running ? tr('debug.testing') : tr('debug.test_64mib')}
        </button>
      </div>
      <p className="muted">
        {tr('debug.range_hint')}
      </p>
      {error && <p className="error">{error}</p>}
      {rows.length > 0 && (
        <table className="table">
          <thead>
            <tr>
              <th>{tr('debug.th_case')}</th>
              <th>{tr('about.th_code')}</th>
              <th>Content-Range</th>
              <th>{tr('debug.th_ttfb')}</th>
              <th>{tr('debug.th_total_ms')}</th>
              <th>{tr('debug.th_bytes')}</th>
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
  const t = makeT(getLang())
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
        <h2>{t('debug.probes')}</h2>
        <div className="actions">
          <button type="button" onClick={run} disabled={running}>
            {running ? t('debug.running') : t('debug.run_all')}
          </button>
          <a className="button" href="api/report" download>
            {t('debug.export_report')}
          </a>
        </div>
      </div>

      {error && <p className="error">{error}</p>}
      {!results && !error && <p className="muted">{t('debug.not_run')}</p>}
      {results && (
        <table className="table">
          <thead>
            <tr>
              <th>{t('debug.th_name')}</th>
              <th>{t('debug.th_result')}</th>
              <th>{t('about.th_desc')}</th>
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
                      <summary>{t('debug.th_data')}</summary>
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
  const t = makeT(getLang())
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
    const t = makeT(getLang())
    setError(null)
    if (sdk.isStandaloneWeb) {
      setError(t('debug.need_desktop_dir'))
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
        <h2>{t('debug.acl_probe')}</h2>
        <button type="button" onClick={pick} disabled={standalone}>
          {t('debug.pick_fnos_dir')}
        </button>
      </div>
      {standalone && <p className="muted">{t('debug.not_in_desktop')}</p>}
      {error && <p className="error">{error}</p>}
      {picked.length > 0 && (
        <p className="muted">
          {t('debug.picked')}<span className="mono">{picked.join(', ')}</span>
        </p>
      )}
      {result && (
        <>
          <h3>{t('debug.granted_dirs')}</h3>
          <ul className="mono">
            {result.folders.map((f) => (
              <li key={f}>{f}</li>
            ))}
          </ul>

          <h3>ACL</h3>
          <table className="table">
            <thead>
              <tr>
                <th>{t('debug.th_path')}</th>
                <th>{t('debug.th_readable')}</th>
                <th>{t('debug.th_writable')}</th>
                <th>{t('debug.th_deletable')}</th>
                <th>{t('debug.th_in_grants')}</th>
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

          <h3>{t('debug.semantic_paths')}</h3>
          {result.semantic_error && <p className="error">{result.semantic_error}</p>}
          <ul>
            {(result.semantic ?? []).map((s) => (
              <li key={s.path}>
                <span className="mono">{s.path}</span> → {s.semanticPath}
              </li>
            ))}
          </ul>

          <p className={result.writable_ok ? 'ok' : 'error'}>
            {result.writable_ok ? t('debug.writable_yes') : t('debug.writable_no').replace('{reason}', result.writable_error || '')}
          </p>
        </>
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
  const t = makeT(getLang())
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
      setError(t('debug.need_desktop_file'))
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
        <h2>{t('debug.upload_probe')}</h2>
      </div>
      {!bound && <p className="muted">{t('debug.bind_first')}</p>}
      {error && <p className="error">{error}</p>}
      {config && bound && (
        <>
          <label className="field">
            <span>{t('debug.parent_id_label')}</span>
            <input
              type="text"
              value={parentId}
              onChange={(e) => setParentId(e.target.value)}
              placeholder="folder id"
            />
          </label>
          <div className="actions">
            <button type="button" onClick={() => upload(config.generated_path)} disabled={busy}>
              {busy ? t('debug.uploading') : t('debug.upload_generated').replace('{mib}', String(Math.round(config.generated_size / 1048576)))}
            </button>
            <button type="button" onClick={pickAndUpload} disabled={busy || standalone}>
              {t('debug.upload_fnos_file')}
            </button>
          </div>
        </>
      )}
      {report && (
        <dl className="kv">
          <dt>{t('debug.dt_file')}</dt>
          <dd className="mono">{report.file?.name ?? report.local_path}</dd>
          <dt>{t('files.th_size')}</dt>
          <dd>{t('debug.byte_count').replace('{n}', String(report.size))}</dd>
          <dt>GCID</dt>
          <dd className="mono">{report.gcid}</dd>
          <dt>{t('debug.dt_gcid_ms')}</dt>
          <dd>{report.gcid_ms} ms</dd>
          <dt>{t('debug.dt_instant')}</dt>
          <dd>{report.instant ? t('debug.instant_yes') : t('debug.instant_no')}</dd>
          {report.provider && (
            <>
              <dt>Provider</dt>
              <dd className="mono">{report.provider}</dd>
              <dt>{t('debug.dt_param_keys')}</dt>
              <dd className="mono">{(report.param_keys ?? []).join(', ')}</dd>
            </>
          )}
          <dt>{t('debug.dt_upload_ms')}</dt>
          <dd>{report.upload_ms} ms</dd>
          <dt>{t('debug.dt_wait_ms')}</dt>
          <dd>{report.wait_ms} ms</dd>
          <dt>{t('debug.th_total_ms')}</dt>
          <dd>{report.total_ms} ms</dd>
          <dt>{t('debug.dt_final')}</dt>
          <dd className="mono">
            {report.file?.phase ?? '—'} {report.file?.id ? t('debug.id_paren').replace('{id}', report.file.id) : ''}
          </dd>
        </dl>
      )}
    </section>
  )
}

export default function Debug() {
  const t = makeT(getLang())
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

  // AccountSection no longer takes an onChange prop: the same refresh rides
  // the account-changed event the bind flow dispatches.
  useEffect(() => {
    const onAccount = () => {
      loadHealth()
      setRefresh((n) => n + 1)
    }
    window.addEventListener('pikpak:account-changed', onAccount)
    return () => window.removeEventListener('pikpak:account-changed', onAccount)
  }, [loadHealth])

  return (
    <main className="page">
      <header className="page-header">
        <h1>{t('debug.title')}</h1>
        <span className="muted">{t('debug.m0')}</span>
      </header>

      <section className="card">
        <h2>{t('debug.health')}</h2>
        {error && <p className="error">{error}</p>}
        {!error && !health && <p className="muted">{t('debug.checking')}</p>}
        {health && (
          <dl className="kv">
            <dt>{t('about.version')}</dt>
            <dd>{health.version}</dd>
            <dt>{t('debug.dt_uid')}</dt>
            <dd>{health.uid}</dd>
          </dl>
        )}
      </section>

      <AccountSection />
      <GatewayCard />
      <RangeCard />
      <UploadCard refresh={refresh} />
      <ACLCard />
      <ProbesCard />
    </main>
  )
}
