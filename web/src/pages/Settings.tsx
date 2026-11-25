import { useCallback, useEffect, useRef, useState } from 'react'
import { apiDownload, apiGet, apiPut, apiUploadJSON } from '../api'
import { restoreSummary, type RestoreResult } from '../backup'
import { formatSize } from '../files'
import { askPath, hostAvailable, pickDirectory } from '../fnosSdk'
import { changedSettings, globalSettingKeys, pickSettings, userSettingKeys, type SettingValues } from '../settings'
import { AccountSection } from '../components/AccountSection'
import { getLang, makeT } from '../i18n'

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

// Option labels are i18n keys, resolved with the page's t at render time.
const conflictOptions = [
  { value: 'rename', key: 'settings.conflict_rename' },
  { value: 'skip', key: 'settings.conflict_skip' },
  { value: 'overwrite', key: 'settings.conflict_overwrite' },
]

const playModeOptions = [
  { value: 'proxy', key: 'settings.play_proxy' },
  { value: 'redirect', key: 'settings.play_redirect' },
]

const playerOptions = [
  { value: 'builtin', key: 'settings.player_builtin' },
  { value: 'iina', key: 'settings.player_iina' },
]

const tileSizeOptions = [
  { value: 'small', key: 'settings.tile_small' },
  { value: 'medium', key: 'settings.tile_medium' },
  { value: 'large', key: 'settings.tile_large' },
]

// Language names stay in their own language by convention — a label like
// 「中文」 is not translated into English.
const languageOptions = [
  { value: 'zh-CN', label: '中文' },
  { value: 'en', label: 'English' },
]

const searchScopeOptions = [
  { value: 'share', key: 'settings.scope_share' },
  { value: 'all', key: 'settings.scope_all' },
]

function optionLabel(options: { value: string; key?: string; label?: string }[], value: string, t: (key: string) => string): string {
  for (const o of options) {
    if (o.value === value) return o.key ? t(o.key) : (o.label ?? value)
  }
  return value
}

function kindLabel(kind: string, t: (key: string) => string): string {
  return kind === 'upload' ? t('about.kind_upload') : t('about.kind_download')
}

export default function Settings() {
  const lang = getLang()
  const t = makeT(lang)
  const [me, setMe] = useState<Me | null>(null)
  const [globalSettings, setGlobalSettings] = useState<SettingValues>({})
  const [userSettings, setUserSettings] = useState<SettingValues>({})
  const [folders, setFolders] = useState<Folder[]>([])
  const [diag, setDiag] = useState<Diag | null>(null)
  const [saving, setSaving] = useState('')
  const [saved, setSaved] = useState('')
  const [error, setError] = useState('')
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
      setSaved(t('settings.no_changes'))
      return
    }
    setSaving('save')
    try {
      // The note follows the language this save just persisted — the same save
      // can flip the UI language, and the closure's t still speaks the old one
      // because App re-renders (and re-syncs setLang) only after this tick.
      let savedLang = lang
      const notes: string[] = []
      if (Object.keys(userPatch).length > 0) {
        const next = await apiPut<SettingValues>('user-settings', userPatch)
        setUserSettings(next)
        baseline.current.user = next
        if (next.language === 'en' || next.language === 'zh-CN') {
          savedLang = next.language
          window.dispatchEvent(new CustomEvent('pikpak:language', { detail: next.language }))
        }
        if (next.tile_size) {
          window.dispatchEvent(new CustomEvent('pikpak:tile-size', { detail: next.tile_size }))
        }
        notes.push(makeT(savedLang)('settings.saved'))
      }
      if (Object.keys(globalPatch).length > 0) {
        const next = await apiPut<SettingValues>('settings', globalPatch)
        setGlobalSettings(next)
        baseline.current.global = next
        notes.push(makeT(savedLang)('settings.saved'))
        if ('download_concurrency' in globalPatch) notes.push(makeT(savedLang)('settings.concurrency_restart'))
      }
      setSaved(notes.join(savedLang === 'en' ? '; ' : '；'))
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
      const picked = await pickDirectory(t('settings.pick_dir_dialog'))
      if (!picked.length) return
      dir = picked[0]
    } else {
      const asked = askPath(t('settings.ask_dir'))
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
      setBackupNote(t('backup.exported').replace('{name}', filename))
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
      setBackupNote(restoreSummary(result, lang))
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

      <p className="muted small">{t('settings.privacy_line').replace('{uid}', String(me?.uid ?? '—'))}</p>
      <AccountSection />

      <section className="card">
        <div className="card-header">
          <h2>{t('settings.title')}</h2>
          <div className="settings-bar">
            {dirty && <span className="muted small">{t('settings.dirty')}</span>}
            <button
              type="button"
              className={dirty ? 'primary' : undefined}
              disabled={saving === 'save'}
              onClick={() => void saveAll()}
            >
              {saving === 'save' ? t('settings.saving') : t('settings.save')}
            </button>
          </div>
        </div>

        <label className="field">
          <span>{t('settings.conflict')}</span>
          <select value={userSettings.conflict ?? 'rename'} onChange={(e) => editUser('conflict', e.target.value)}>
            {conflictOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {t(o.key)}
              </option>
            ))}
          </select>
        </label>
        <p className="muted small">
          {t('settings.current_prefix').replace('{label}', optionLabel(conflictOptions, userSettings.conflict ?? 'rename', t))}
        </p>

        <label className="field">
          <span>{t('settings.download_dir')}</span>
          <div className="dir-row">
            <input
              value={userSettings.download_dir ?? ''}
              placeholder={t('settings.download_dir_placeholder')}
              onChange={(e) => editUser('download_dir', e.target.value)}
            />
            <button type="button" onClick={() => void chooseDownloadDir()}>
              {t('settings.pick_dir')}
            </button>
          </div>
        </label>
        <p className="muted small">{t('settings.download_dir_hint')}</p>

        <label className="field">
          <span>{t('settings.concurrency')}</span>
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
        <p className="muted small">{t('settings.concurrency_hint')}</p>

        <label className="field">
          <span>{t('settings.search_key')}</span>
          <div className="dir-row">
            <input
              type="password"
              autoComplete="off"
              value={globalSettings.search_api_key ?? ''}
              placeholder={
                globalSettings.search_api_key_set === '1' ? t('settings.search_key_set') : t('settings.search_key_unset')
              }
              onChange={(e) => editGlobal('search_api_key', e.target.value)}
            />
            {(globalSettings.search_api_key_set === '1' || (globalSettings.search_api_key ?? '') !== '') && (
              <button type="button" onClick={() => editGlobal('search_api_key', '')}>
                {t('settings.clear')}
              </button>
            )}
          </div>
        </label>
        <p className="muted small">
          {t('settings.search_key_hint_a')}
          <strong>{t('settings.search_key_hint_strong')}</strong>
          {t('settings.search_key_hint_b')}
        </p>

        <label className="field">
          <span>{t('settings.search_scope')}</span>
          <select
            value={userSettings.search_scope ?? 'share'}
            onChange={(e) => editUser('search_scope', e.target.value)}
          >
            {searchScopeOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {t(o.key)}
              </option>
            ))}
          </select>
        </label>
        <p className="muted small">
          {t('settings.current_prefix').replace('{label}', optionLabel(searchScopeOptions, userSettings.search_scope ?? 'share', t))}{' '}
          {t('settings.scope_hint')}
        </p>

        <label className="field">
          <span>{t('settings.play_mode')}</span>
          <select value={userSettings.play_mode ?? 'proxy'} onChange={(e) => editUser('play_mode', e.target.value)}>
            {playModeOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {t(o.key)}
              </option>
            ))}
          </select>
        </label>
        <p className="muted small">
          {t('settings.current_prefix').replace('{label}', optionLabel(playModeOptions, userSettings.play_mode ?? 'proxy', t))}
        </p>

        <label className="field">
          <span>{t('settings.player')}</span>
          <select value={userSettings.player ?? 'builtin'} onChange={(e) => editUser('player', e.target.value)}>
            {playerOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {t(o.key)}
              </option>
            ))}
          </select>
        </label>
        <p className="muted small">
          {t('settings.current_prefix').replace('{label}', optionLabel(playerOptions, userSettings.player ?? 'builtin', t))}{' '}
          {t('settings.player_hint')}
        </p>

        <label className="field">
          <span>{t('settings.tile_size')}</span>
          <select value={userSettings.tile_size ?? 'small'} onChange={(e) => editUser('tile_size', e.target.value)}>
            {tileSizeOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {t(o.key)}
              </option>
            ))}
          </select>
        </label>
        <p className="muted small">
          {t('settings.current_prefix').replace('{label}', optionLabel(tileSizeOptions, userSettings.tile_size ?? 'small', t))}{' '}
          {t('settings.tile_hint')}
        </p>

        <label className="field">
          <span>{t('settings.language')}</span>
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
          <h2>{t('backup.title')}</h2>
        </div>
        <p className="muted">{t('backup.hint')}</p>
        <div className="actions">
          <button type="button" disabled={backupBusy !== ''} onClick={() => void exportBackup()}>
            {backupBusy === 'export' ? t('backup.exporting') : t('backup.export')}
          </button>
          <button type="button" disabled={backupBusy !== ''} onClick={() => backupFile.current?.click()}>
            {backupBusy === 'restore' ? t('backup.restoring') : t('backup.restore')}
          </button>
          <input
            ref={backupFile}
            type="file"
            accept=".json,application/json"
            style={{ display: 'none' }}
            onChange={onBackupFilePicked}
          />
        </div>
        <p className="muted small">{t('backup.warning')}</p>
        {backupNote && <p className="ok muted small">{backupNote}</p>}
      </section>

      <section className="card">
        <div className="card-header">
          <h2>{t('folders.title')}</h2>
          <span className="muted small">{t('folders.count').replace('{n}', String(folders.length))}</span>
        </div>
        {folders.length === 0 ? (
          <p className="muted">{t('folders.empty')}</p>
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
        <p className="muted small">{t('folders.hint')}</p>
      </section>

      <section className="card">
        <div className="card-header">
          <h2>{t('about.title')}</h2>
          <button type="button" onClick={() => void load()}>
            {t('common.refresh')}
          </button>
        </div>
        <dl className="kv">
          <dt>{t('about.version')}</dt>
          <dd>{diag?.version || t('about.dev')}</dd>
          <dt>{t('about.db_size')}</dt>
          <dd>{formatSize(diag?.db_bytes ?? 0)}</dd>
          <dt>{t('about.task_records')}</dt>
          <dd>
            {t('about.download_short')} {sumCounts(diag?.downloads)} · {t('about.upload_short')}{' '}
            {sumCounts(diag?.uploads)}
          </dd>
          <dt>{t('about.fnos_api')}</dt>
          <dd>{diag ? (diag.fnos_available ? t('about.available') : t('about.unavailable_dev')) : '—'}</dd>
        </dl>

        <h3>{t('about.recent_errors')}</h3>
        {!diag || diag.recent_failures.length === 0 ? (
          <p className="muted">{t('about.no_failures')}</p>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>{t('about.th_kind')}</th>
                <th>{t('about.th_id')}</th>
                <th>{t('about.th_code')}</th>
                <th>{t('about.th_desc')}</th>
              </tr>
            </thead>
            <tbody>
              {diag.recent_failures.map((f) => (
                <tr key={`${f.kind}-${f.id}`}>
                  <td>{kindLabel(f.kind, t)}</td>
                  <td className="mono">{f.id}</td>
                  <td className="mono">{f.error_code || '—'}</td>
                  <td>{f.error_message || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </>
  )
}

function sumCounts(counts: Record<string, number> | undefined): number {
  if (!counts) return 0
  let total = 0
  for (const n of Object.values(counts)) total += n
  return total
}
