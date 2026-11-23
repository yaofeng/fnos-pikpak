import { useEffect, useRef, useState, type ReactNode } from 'react'
import { NavLink, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { apiGet } from './api'
import Toaster from './components/Toaster'
import { BookmarkIcon, ClockIcon, FolderIcon, GearIcon, GlobeIcon, MagnetIcon, StarIcon, TransferIcon, TrashIcon, WrenchIcon } from './components/icons'
import { detectLang, makeT, setLang, type Lang } from './i18n'
import { isActive, transferred, type TransferJob } from './transfers'
import Bind from './pages/Bind'
import Debug from './pages/Debug'
import Favorites from './pages/Favorites'
import Files from './pages/Files'
import Search from './pages/Search'
import Recent from './pages/Recent'
import Settings from './pages/Settings'
import Tasks from './pages/Tasks'
import Transfers from './pages/Transfers'
import Trash from './pages/Trash'
import VideoFavorites from './pages/VideoFavorites'

type Me = {
  uid: number
  bound: boolean
  status: string
  display_name: string
  vip_type: string
  debug?: boolean
  capabilities: Record<string, boolean>
}

type Account = {
  quota_limit: number
  quota_usage: number
  vip_type: string
  vip_expire: string
  cached_at: string
}

// applyTileSize scales the icon view through one CSS variable; the option is
// stored per fnOS user and read back on every load.
function applyTileSize(size?: string) {
  const value = size === 'medium' || size === 'large' ? size : 'small'
  document.documentElement.dataset.tileSize = value
}

const navItems = [
  { to: '/files', key: 'nav.files' },
  { to: '/recent', key: 'nav.recent' },
  { to: '/trash', key: 'nav.trash' },
  { to: '/transfers', key: 'nav.transfers' },
  { to: '/tasks', key: 'nav.tasks' },
  { to: '/favorites', key: 'nav.favorites' },
  { to: '/video-favorites', key: 'nav.videoFavorites' },
  { to: '/search', key: 'nav.search' },
]

const navIcons: Record<string, ReactNode> = {
  '/files': <FolderIcon size={17} />,
  '/recent': <ClockIcon size={17} />,
  '/trash': <TrashIcon size={17} />,
  '/transfers': <TransferIcon size={17} />,
  '/tasks': <MagnetIcon size={17} />,
  '/favorites': <BookmarkIcon size={17} />,
  '/video-favorites': <StarIcon size={17} />,
  '/search': <GlobeIcon size={17} />,
  '/settings': <GearIcon size={17} />,
  '/debug': <WrenchIcon size={17} />,
}

// transferSummary is what the sidebar reports while transfers run: the bytes
// of every active job together, how many jobs there are, and the total speed
// of each direction.
type transferSummary = { active: number; bytes: number; size: number; down: number; up: number; rates: boolean }

function summarise(items: TransferJob[]): { active: number; bytes: number; size: number } {
  let active = 0
  let bytes = 0
  let size = 0
  for (const job of items) {
    if (!isActive(job)) continue
    active++
    bytes += transferred(job)
    size += job.size || 0
  }
  return { active, bytes, size }
}

function formatSpeed(bytesPerSec: number): string {
  return `${formatBytes(Math.round(bytesPerSec))}/s`
}

function formatBytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

export default function App() {
  const [lang, setLanguage] = useState<Lang>(() => detectLang())
  const t = makeT(lang)
  setLang(lang)
  const location = useLocation()
  const navigate = useNavigate()
  const [me, setMe] = useState<Me | null>(null)
  const [account, setAccount] = useState<Account | null>(null)
  const [transfer, setTransfer] = useState<transferSummary | null>(null)
  // The previous poll's active bytes, turned into a rate by the next one.
  const transferSample = useRef<{ at: number; down: number; up: number } | null>(null)

  useEffect(() => {
    const load = () => {
      apiGet<Me>('me')
        .then((m) => {
          setMe(m)
          if (m.bound) {
            apiGet<Account>('account').then(setAccount).catch(() => setAccount(null))
          } else {
            setAccount(null)
          }
        })
        .catch(() => setMe(null))
    }
    load()
    window.addEventListener('pikpak:account-changed', load)
    return () => window.removeEventListener('pikpak:account-changed', load)
  }, [])

  // The quota pill follows the account: a restore, download or upload on the
  // NAS changes it, so it is re-read every 10 seconds while bound.
  useEffect(() => {
    if (!me?.bound) return
    const timer = window.setInterval(() => {
      apiGet<Account>('account').then(setAccount).catch(() => undefined)
    }, 10_000)
    return () => window.clearInterval(timer)
  }, [me?.bound])

  // 传输 reports every running upload and download, so the sidebar can show
  // one combined progress and both speeds from any page. It polls only while
  // a user is bound, and disappears once nothing is active.
  useEffect(() => {
    if (!me?.bound) {
      setTransfer(null)
      transferSample.current = null
      return
    }
    let stopped = false
    let timer: number | undefined
    const tick = async () => {
      try {
        const [downPage, upPage] = await Promise.all([
          apiGet<{ items: TransferJob[] }>('downloads'),
          apiGet<{ items: TransferJob[] }>('uploads'),
        ])
        if (stopped) return
        const down = summarise(downPage.items ?? [])
        const up = summarise(upPage.items ?? [])
        const now = Date.now()
        const prev = transferSample.current
        const downBytes = down.bytes
        const upBytes = up.bytes
        let downSpeed = 0
        let upSpeed = 0
        // The first poll only establishes a baseline; a rate needs two.
        if (prev && now > prev.at) {
          const dt = (now - prev.at) / 1000
          downSpeed = Math.max(0, downBytes - prev.down) / dt
          upSpeed = Math.max(0, upBytes - prev.up) / dt
        }
        transferSample.current = { at: now, down: downBytes, up: upBytes }
        const active = down.active + up.active
        setTransfer(
          active > 0
            ? {
                active,
                bytes: downBytes + upBytes,
                size: down.size + up.size,
                down: downSpeed,
                up: upSpeed,
                rates: Boolean(prev),
              }
            : null,
        )
        // Idle pages poll slowly: the bar only has to appear soon after a
        // transfer starts, not within two seconds.
        if (!stopped) timer = window.setTimeout(tick, active > 0 ? 2000 : 8000)
        return
      } catch {
        // A quota or session error already shows elsewhere; the summary just
        // keeps its last value.
      }
      if (!stopped) timer = window.setTimeout(tick, 8000)
    }
    void tick()
    return () => {
      stopped = true
      window.clearTimeout(timer)
    }
  }, [me?.bound])

  // The stored language and tile size belong to the fnOS user; the browser
  // only supplies the starting guess for the language.
  useEffect(() => {
    apiGet<Record<string, string>>('user-settings')
      .then((s) => {
        if (s.language === 'en' || s.language === 'zh-CN') setLanguage(s.language)
        applyTileSize(s.tile_size)
      })
      .catch(() => undefined)
    const onLanguage = (e: Event) => {
      const detail = (e as CustomEvent<string>).detail
      if (detail === 'en' || detail === 'zh-CN') setLanguage(detail)
    }
    const onTileSize = (e: Event) => applyTileSize((e as CustomEvent<string>).detail)
    window.addEventListener('pikpak:language', onLanguage)
    window.addEventListener('pikpak:tile-size', onTileSize)
    return () => {
      window.removeEventListener('pikpak:language', onLanguage)
      window.removeEventListener('pikpak:tile-size', onTileSize)
    }
  }, [])

  const pct = account && account.quota_limit > 0
    ? Math.min(100, (account.quota_usage / account.quota_limit) * 100)
    : 0

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="brand">PikPak</div>
        <nav className="nav">
          {navItems.map((item) => (
            <NavLink key={item.to} to={item.to} className={({ isActive }) => (isActive ? 'nav-item active' : 'nav-item')}>
              {navIcons[item.to]}
              <span>{t(item.key)}</span>
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-foot">
          {transfer && (
            <div className="sidebar-transfer" title={`共 ${transfer.active} 个传输任务`}>
              <div className="sidebar-transfer-bar">
                <span
                  style={{
                    width: transfer.size > 0 ? `${Math.min(100, (transfer.bytes / transfer.size) * 100)}%` : '0',
                  }}
                />
              </div>
              <div className="sidebar-transfer-line">
                <span>
                  {transfer.active} 个任务 ·{' '}
                  {transfer.size > 0
                    ? `${Math.min(100, Math.floor((transfer.bytes / transfer.size) * 100))}%`
                    : '—'}
                </span>
              </div>
              <div className="sidebar-transfer-line muted">
                <span>↓ {transfer.rates ? formatSpeed(transfer.down) : '—'}</span>
                <span>↑ {transfer.rates ? formatSpeed(transfer.up) : '—'}</span>
              </div>
            </div>
          )}
          {account && (
            <span className="quota-pill sidebar-quota" title={`${pct.toFixed(1)}%`}>
              <span
                className="quota-fill"
                aria-hidden="true"
                style={{ width: pct > 0 ? `max(${pct}%, 4px)` : '0' }}
              />
              <span className="quota-text">
                {formatBytes(account.quota_usage)} / {formatBytes(account.quota_limit)}
              </span>
            </span>
          )}
          {!me?.bound && (
            <NavLink to="/bind" className="nav-item">
              {t('action.bind')}
            </NavLink>
          )}
          <NavLink to="/settings" className={({ isActive }) => (isActive ? 'nav-item active' : 'nav-item')}>
            {navIcons['/settings']}
            <span>{t('nav.settings')}</span>
          </NavLink>
          {me?.debug && (
            <NavLink to="/debug" className="nav-item subtle">
              {navIcons['/debug']}
              <span>{t('nav.debug')}</span>
            </NavLink>
          )}
        </div>
      </aside>

      <div className="content">

        {me && !me.bound && location.pathname !== '/bind' && (
          <div className="notice">
            {t('state.unbound')} · <NavLink to="/bind">{t('action.bind')}</NavLink>
          </div>
        )}

        {me?.bound && me.status === 'reauth_required' && (
          <div className="notice danger">
            <span>{t('state.reauth')}</span>
            <button type="button" className="notice-action" onClick={() => navigate('/bind')}>
              {t('action.rebind')}
            </button>
          </div>
        )}

        {/* These pages fill the window: their file areas scroll, not the page. */}
        <main
          className={
            location.pathname === '/search' ||
            location.pathname.startsWith('/files') ||
            location.pathname.startsWith('/favorites')
              ? 'page page-fill'
              : 'page'
          }
        >
          <Routes>
            <Route path="/" element={<Navigate to="/files" replace />} />
            <Route path="/files/*" element={<Files />} />
            <Route path="/recent" element={<Recent />} />
            <Route path="/tasks" element={<Tasks />} />
            <Route path="/transfers" element={<Transfers />} />
            <Route path="/trash" element={<Trash />} />
            <Route path="/favorites" element={<Favorites />} />
            <Route path="/video-favorites" element={<VideoFavorites />} />
            <Route path="/search" element={<Search />} />
            <Route path="/settings" element={<Settings />} />
            <Route path="/bind" element={<Bind />} />
            {me?.debug && <Route path="/debug" element={<Debug />} />}
            <Route path="*" element={<Navigate to="/files" replace />} />
          </Routes>
        </main>

        <Toaster />

      </div>
    </div>
  )
}
