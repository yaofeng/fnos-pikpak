import { useEffect, useState, type ReactNode } from 'react'
import { NavLink, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { apiGet } from './api'
import Toaster from './components/Toaster'
import { BookmarkIcon, ClockIcon, FolderIcon, GearIcon, GlobeIcon, MagnetIcon, StarIcon, TransferIcon, TrashIcon, WrenchIcon } from './components/icons'
import { detectLang, makeT, setLang, type Lang } from './i18n'
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

const titles: Record<string, string> = {
  '/files': 'nav.files',
  '/recent': 'nav.recent',
  '/tasks': 'nav.tasks',
  '/transfers': 'nav.transfers',
  '/trash': 'nav.trash',
  '/settings': 'nav.settings',
  '/favorites': 'nav.favorites',
  '/video-favorites': 'nav.videoFavorites',
  '/search': 'nav.search',
  '/bind': 'nav.bind',
  '/debug': 'nav.debug',
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

  const titleKey = Object.keys(titles).find((p) => location.pathname.startsWith(p))
  const title = titleKey ? t(titles[titleKey]) : 'PikPak'
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
          {me?.bound ? (
            <span className="account-chip" title={`UID ${me.uid}`}>
              {me.display_name || '已绑定'}
              {me.vip_type && me.vip_type !== 'novip' ? <em>{me.vip_type}</em> : null}
            </span>
          ) : (
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
        <header className="topbar">
          <strong>{title}</strong>
          {account && (
            <span className="quota-pill" title={`${pct.toFixed(1)}%`}>
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
        </header>

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
