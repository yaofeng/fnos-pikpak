import { useEffect, useState } from 'react'
import { apiGet } from './api'

type Health = { ok: boolean; version: string; uid: number }

export default function App() {
  const [health, setHealth] = useState<Health | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    apiGet<Health>('health').then(setHealth).catch((e: Error) => setError(e.message))
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

      <section className="card">
        <h2>探针</h2>
        <p className="muted">探针在后续任务接入。</p>
      </section>
    </main>
  )
}
