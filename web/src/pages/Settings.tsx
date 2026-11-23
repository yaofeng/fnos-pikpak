import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { apiDownload, apiGet, apiPost, apiPut, apiUploadJSON } from '../api'
import { restoreSummary, type RestoreResult } from '../backup'
import { formatSize } from '../files'
import { askPath, hostAvailable, pickDirectory } from '../fnosSdk'
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

const playerOptions = [
  { value: 'builtin', label: '内置播放器（应用内）' },
  { value: 'iina', label: 'IINA（仅 macOS，直接使用原始链接）' },
]

const tileSizeOptions = [
  { value: 'small', label: '小（默认）' },
  { value: 'medium', label: '中（1.5 倍）' },
  { value: 'large', label: '大（2 倍）' },
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
  const [backupBusy, setBackupBusy] = useState('')
  const [backupNote, setBackupNote] = useState('')
  const backupFile = useRef<HTMLInputElement>(null)
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

  // One 保存 button covers the whole page: each scope is sent only when one of
  // its keys changed, so a save never rewrites settings the user did not touch.
  async function saveAll() {
    setError('')
    setSaved('')
    const userPatch = changedSettings(baseline.current.user, pickSettings(userSettings, userSettingKeys))
    const globalPatch = changedSettings(baseline.current.global, pickSettings(globalSettings, globalSettingKeys))
    if (Object.keys(userPatch).length === 0 && Object.keys(globalPatch).length === 0) {
      setSaved('没有改动')
      return
    }
    setSaving('save')
    try {
      const notes: string[] = []
      if (Object.keys(userPatch).length > 0) {
        const next = await apiPut<SettingValues>('user-settings', userPatch)
        setUserSettings(next)
        baseline.current.user = next
        if (next.language) {
          window.dispatchEvent(new CustomEvent('pikpak:language', { detail: next.language }))
        }
        if (next.tile_size) {
          window.dispatchEvent(new CustomEvent('pikpak:tile-size', { detail: next.tile_size }))
        }
        notes.push('已保存')
      }
      if (Object.keys(globalPatch).length > 0) {
        const next = await apiPut<SettingValues>('settings', globalPatch)
        setGlobalSettings(next)
        baseline.current.global = next
        notes.push('已保存')
        if ('download_concurrency' in globalPatch) notes.push('并发数在服务重启后生效')
      }
      setSaved(notes.join('；'))
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

  async function chooseDownloadDir() {
    setError('')
    let dir = ''
    if (hostAvailable()) {
      const picked = await pickDirectory('选择默认下载目录')
      if (!picked.length) return
      dir = picked[0]
    } else {
      const asked = askPath('默认下载目录（绝对路径）')
      if (!asked) return
      dir = asked
    }
    editUser('download_dir', dir)
  }

  async function exportBackup() {
    setError('')
    setBackupNote('')
    setBackupBusy('export')
    try {
      const { blob, filename } = await apiDownload('backup')
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = filename
      link.style.display = 'none'
      document.body.appendChild(link)
      link.click()
      link.remove()
      // Revoking immediately can cancel the save in some browsers; give the
      // download a moment to start.
      setTimeout(() => URL.revokeObjectURL(url), 1000)
      setBackupNote(`已导出 ${filename}`)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBackupBusy('')
    }
  }

  async function restoreBackupFile(file: File) {
    setError('')
    setBackupNote('')
    setBackupBusy('restore')
    try {
      const result = await apiUploadJSON<RestoreResult>('restore', await file.text())
      setBackupNote(restoreSummary(result))
      // The restore may have overwritten settings this form is showing.
      await load()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBackupBusy('')
    }
  }

  function onBackupFilePicked(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    // Clear the input so picking the same file again still fires a change.
    e.target.value = ''
    if (file) void restoreBackupFile(file)
  }

  function editUser(key: string, value: string) {
    setUserSettings((prev) => ({ ...prev, [key]: value }))
    setSaved('')
  }

  function editGlobal(key: string, value: string) {
    setGlobalSettings((prev) => ({ ...prev, [key]: value }))
    setSaved('')
  }

  const dirty =
    Object.keys(changedSettings(baseline.current.user, pickSettings(userSettings, userSettingKeys))).length > 0 ||
    Object.keys(changedSettings(baseline.current.global, pickSettings(globalSettings, globalSettingKeys))).length > 0

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
          <h2>参数设置</h2>
          <div className="settings-bar">
            {dirty && <span className="muted small">有未保存的修改</span>}
            <button
              type="button"
              className={dirty ? 'primary' : undefined}
              disabled={saving === 'save'}
              onClick={() => void saveAll()}
            >
              {saving === 'save' ? '保存中…' : '保存'}
            </button>
          </div>
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
          <span>默认下载目录</span>
          <div className="dir-row">
            <input
              value={userSettings.download_dir ?? ''}
              placeholder="未设置，下载时会提示先设置"
              onChange={(e) => editUser('download_dir', e.target.value)}
            />
            <button type="button" onClick={() => void chooseDownloadDir()}>
              选择目录
            </button>
          </div>
        </label>
        <p className="muted small">
          「我的文件」里的下载直接保存到该目录；必须是已授权共享文件夹内的目录。留空表示不设置。
        </p>

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

        <label className="field">
          <span>全网搜索 API Key（全局）</span>
          <div className="dir-row">
            <input
              type="password"
              autoComplete="off"
              value={globalSettings.search_api_key ?? ''}
              placeholder={
                globalSettings.search_api_key_set === '1' ? '已设置，输入新值可替换' : '未设置（Serper API Key）'
              }
              onChange={(e) => editGlobal('search_api_key', e.target.value)}
            />
            {(globalSettings.search_api_key_set === '1' || (globalSettings.search_api_key ?? '') !== '') && (
              <button type="button" onClick={() => editGlobal('search_api_key', '')}>
                清除
              </button>
            )}
          </div>
        </label>
        <p className="muted small">
          「全网搜索」通过 Serper（Google 结果）搜索 mypikpak.com 站内分享页。Key 只存在本机数据库，读取设置时不会返回，
          也不会写进日志。可以在 serper.dev 免费申请。
        </p>

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
          <span>播放器</span>
          <select value={userSettings.player ?? 'builtin'} onChange={(e) => editUser('player', e.target.value)}>
            {playerOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </label>
        <p className="muted small">
          当前：{optionLabel(playerOptions, userSettings.player ?? 'builtin')}
          。选 IINA 后，预览和完整播放直接把原始链接交给 IINA，不再每次询问；IINA 只在 macOS 上有效。
        </p>

        <label className="field">
          <span>大图标尺寸</span>
          <select value={userSettings.tile_size ?? 'small'} onChange={(e) => editUser('tile_size', e.target.value)}>
            {tileSizeOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </label>
        <p className="muted small">当前：{optionLabel(tileSizeOptions, userSettings.tile_size ?? 'small')}。作用于所有大图标视图。</p>

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
          <h2>数据备份与恢复</h2>
        </div>
        <p className="muted">
          备份当前飞牛用户的视频收藏、分享收藏和配置参数，导出为一个 JSON 文件。恢复时收藏按条目合并（已存在的跳过，
          不覆盖本机内容），配置参数直接覆盖。
        </p>
        <div className="actions">
          <button type="button" disabled={backupBusy !== ''} onClick={() => void exportBackup()}>
            {backupBusy === 'export' ? '导出中…' : '导出备份'}
          </button>
          <button type="button" disabled={backupBusy !== ''} onClick={() => backupFile.current?.click()}>
            {backupBusy === 'restore' ? '恢复中…' : '选择备份文件'}
          </button>
          <input
            ref={backupFile}
            type="file"
            accept=".json,application/json"
            style={{ display: 'none' }}
            onChange={onBackupFilePicked}
          />
        </div>
        <p className="muted small">
          备份文件包含收藏（含分享密码）和配置参数，不含账号授权与全网搜索 API Key——恢复后需要重新填写搜索 Key。
          文件可被复制和传播，请妥善保管。
        </p>
        {backupNote && <p className="ok muted small">{backupNote}</p>}
      </section>

      <section className="card">
        <div className="card-header">
          <h2>已授权的共享文件夹</h2>
          <span className="muted small">{folders.length} 个</span>
        </div>
        {folders.length === 0 ? (
          <p className="muted">fnOS 没有返回可访问的共享文件夹，或当前不是从飞牛网关访问。</p>
        ) : (
          <ul className="folder-list">
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
