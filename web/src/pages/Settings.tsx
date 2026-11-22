import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { apiGet, apiPost, apiPut } from '../api'
import { formatSize } from '../files'
import {
  changedSettings,
  globalSettingKeys,
  pickSettings,
  unbindWarning,
  userSettingKeys,
  type SettingValues,
} from '../settings'

type Me = { uid: number; bound: boolean; display_name: string }

type Folder = { path: string; semantic_path: string }

type Failure = {
  kind: string
  id: string
  error_code: string
  error_message: string
  updated_at: string
}

type Diag = {
  version: string
  uid: number
  bound: boolean
  db_bytes: number
  downloads: Record<string, number>
  uploads: Record<string, number>
  recent_failures: Failure[]
  granted_folders: number
  fnos_available: boolean
}

const conflictOptions = [
  { value: 'rename', label: '重命名（保留两份）' },
  { value: 'skip', label: '跳过（保留已有文件）' },
  { value: 'overwrite', label: '覆盖已有文件' },
]

const playModeOptions = [
  { value: 'proxy', label: '代理（推荐，签名地址不外泄）' },
  { value: 'redirect', label: '跳转（省带宽，浏览器直连 CDN）' },
]

const languageOptions = [
  { value: 'zh-CN', label: '中文' },
  { value: 'en', label: 'English' },
]

function optionLabel(options: { value: string; label: string }[], value: string): string {
  for (const o of options) {
    if (o.value === value) return o.label
  }
  return value
}

function kindLabel(kind: string): string {
  return kind === 'upload' ? '上传' : '下载'
}

export default function Settings() {
  const navigate = useNavigate()
  const [me, setMe] = useState<Me | null>(null)
  const [globalSettings, setGlobalSettings] = useState<SettingValues>({})
  const [userSettings, setUserSettings] = useState<SettingValues>({})
  const [folders, setFolders] = useState<Folder[]>([])
  const [diag, setDiag] = useState<Diag | null>(null)
  const [saving, setSaving] = useState('')
  const [saved, setSaved] = useState('')
  const [error, setError] = useState('')
  const [confirmUnbind, setConfirmUnbind] = useState(false)
  // Baseline = what the server currently holds, so a save only sends edits.
  const baseline = useRef<{ global: SettingValues; user: SettingValues }>({ global: {}, user: {} })

  const load = useCallback(async () => {
    const results = await Promise.allSettled([
      apiGet<Me>('me'),
      apiGet<SettingValues>('settings'),
      apiGet<SettingValues>('user-settings'),
      apiGet<{ items: Folder[] }>('fs/folders'),
      apiGet<Diag>('diag'),
    ])
    const [gotMe, gotGlobal, gotUser, gotFolders, gotDiag] = results
    if (gotMe.status === 'fulfilled') setMe(gotMe.value)
    if (gotGlobal.status === 'fulfilled') {
      setGlobalSettings(gotGlobal.value)
      baseline.current.global = gotGlobal.value
    }
    if (gotUser.status === 'fulfilled') {
      setUserSettings(gotUser.value)
      baseline.current.user = gotUser.value
    }
    if (gotFolders.status === 'fulfilled') setFolders(gotFolders.value.items ?? [])
    if (gotDiag.status === 'fulfilled') setDiag(gotDiag.value)
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function save(scope: 'global' | 'user') {
    setError('')
    setSaved('')
    const current = scope === 'global' ? globalSettings : userSettings
    const keys = scope === 'global' ? globalSettingKeys : userSettingKeys
    const patch = changedSettings(baseline.current[scope], pickSettings(current, keys))
    if (Object.keys(patch).length === 0) {
      setSaved('没有改动')
      return
    }
    setSaving(scope)
    try {
      const path = scope === 'global' ? 'settings' : 'user-settings'
      const next = await apiPut<SettingValues>(path, patch)
      if (scope === 'global') setGlobalSettings(next)
      else setUserSettings(next)
      baseline.current[scope] = next
      if (next.language) {
        window.dispatchEvent(new CustomEvent('pikpak:language', { detail: next.language }))
      }
      setSaved(scope === 'global' ? '已保存（并发数在服务重启后生效）' : '已保存')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSaving('')
    }
  }

  async function unbind() {
    setError('')
    setSaving('unbind')
    try {
      await apiPost('auth/logout', {})
      setConfirmUnbind(false)
      window.dispatchEvent(new Event('pikpak:account-changed'))
      navigate('/bind')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSaving('')
    }
  }

  function editUser(key: string, value: string) {
    setUserSettings((prev) => ({ ...prev, [key]: value }))
    setSaved('')
  }

  function editGlobal(key: string, value: string) {
    setGlobalSettings((prev) => ({ ...prev, [key]: value }))
    setSaved('')
  }

  return (
    <>
      {error && <p className="error">{error}</p>}
      {saved && <p className="ok muted small">{saved}</p>}

      <section className="card">
        <div className="card-header">
          <h2>账号</h2>
          <span className={me?.bound ? 'tag tag-pass' : 'tag tag-warn'}>
            {me?.bound ? `已绑定${me.display_name ? '：' + me.display_name : ''}` : '未绑定'}
          </span>
        </div>
        <p className="muted">
          绑定只属于当前飞牛用户（UID {me?.uid ?? '—'}）。其他飞牛用户看不到这里的账号、文件和任务。
        </p>
        <div className="actions">
          <button type="button" onClick={() => navigate('/bind')}>
            重新授权
          </button>
          <button type="button" className="danger" disabled={!me?.bound} onClick={() => setConfirmUnbind(true)}>
            解绑
          </button>
        </div>
        <p className="muted small">
          重新授权会用新的 PikPak 账号替换当前绑定，传输任务记录随之清除；已下载的文件保留。
        </p>
      </section>

      <section className="card">
        <div className="card-header">
          <h2>传输</h2>
          <button type="button" disabled={saving === 'user'} onClick={() => void save('user')}>
            保存
          </button>
        </div>

        <label className="field">
          <span>重名策略</span>
          <select value={userSettings.conflict ?? 'rename'} onChange={(e) => editUser('conflict', e.target.value)}>
            {conflictOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </label>
        <p className="muted small">当前：{optionLabel(conflictOptions, userSettings.conflict ?? 'rename')}</p>

        <label className="field">
          <span>同时下载数（全局）</span>
          <select
            value={globalSettings.download_concurrency ?? '2'}
            onChange={(e) => editGlobal('download_concurrency', e.target.value)}
          >
            {['1', '2', '3', '4'].map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
        </label>
        <p className="muted small">所有用户共享这一上限，修改后需要重启服务才生效。</p>
        <div className="actions">
          <button type="button" disabled={saving === 'global'} onClick={() => void save('global')}>
            保存并发数
          </button>
        </div>
      </section>

      <section className="card">
        <div className="card-header">
          <h2>播放与语言</h2>
          <button type="button" disabled={saving === 'user'} onClick={() => void save('user')}>
            保存
          </button>
        </div>

        <label className="field">
          <span>播放方式</span>
          <select value={userSettings.play_mode ?? 'proxy'} onChange={(e) => editUser('play_mode', e.target.value)}>
            {playModeOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </label>
        <p className="muted small">当前：{optionLabel(playModeOptions, userSettings.play_mode ?? 'proxy')}</p>

        <label className="field">
          <span>界面语言</span>
          <select value={userSettings.language ?? 'zh-CN'} onChange={(e) => editUser('language', e.target.value)}>
            {languageOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </label>
      </section>

      <section className="card">
        <div className="card-header">
          <h2>已授权的共享文件夹</h2>
          <span className="muted small">{folders.length} 个</span>
        </div>
        {folders.length === 0 ? (
          <p className="muted">fnOS 没有返回可访问的共享文件夹，或当前不是从飞牛网关访问。</p>
        ) : (
          <ul className="dest-list">
            {folders.map((f) => (
              <li key={f.path}>
                <span className="mono">{f.path}</span>
                {f.semantic_path ? <span className="muted small">{f.semantic_path}</span> : null}
              </li>
            ))}
          </ul>
        )}
        <p className="muted small">下载与上传只允许写这些目录及其子目录。</p>
      </section>

      <section className="card">
        <div className="card-header">
          <h2>关于</h2>
          <button type="button" onClick={() => void load()}>
            刷新
          </button>
        </div>
        <dl className="kv">
          <dt>版本</dt>
          <dd>{diag?.version || '开发版'}</dd>
          <dt>数据库大小</dt>
          <dd>{formatSize(diag?.db_bytes ?? 0)}</dd>
          <dt>任务记录</dt>
          <dd>
            下载 {sumCounts(diag?.downloads)} · 上传 {sumCounts(diag?.uploads)}
          </dd>
          <dt>飞牛 Open API</dt>
          <dd>{diag ? (diag.fnos_available ? '可用' : '不可用（开发模式）') : '—'}</dd>
        </dl>

        <h3>最近错误</h3>
        {!diag || diag.recent_failures.length === 0 ? (
          <p className="muted">没有失败记录。</p>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>类型</th>
                <th>编号</th>
                <th>错误码</th>
                <th>说明</th>
              </tr>
            </thead>
            <tbody>
              {diag.recent_failures.map((f) => (
                <tr key={`${f.kind}-${f.id}`}>
                  <td>{kindLabel(f.kind)}</td>
                  <td className="mono">{f.id}</td>
                  <td className="mono">{f.error_code || '—'}</td>
                  <td>{f.error_message || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      {confirmUnbind && (
        <div className="modal-backdrop" onClick={() => setConfirmUnbind(false)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h2>确认解绑</h2>
            <p>{unbindWarning}</p>
            <div className="actions">
              <button type="button" className="danger" disabled={saving === 'unbind'} onClick={() => void unbind()}>
                确认解绑
              </button>
              <button type="button" onClick={() => setConfirmUnbind(false)}>
                取消
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  )
}

function sumCounts(counts: Record<string, number> | undefined): number {
  if (!counts) return 0
  let total = 0
  for (const n of Object.values(counts)) total += n
  return total
}
