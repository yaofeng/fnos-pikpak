import { useEffect, useState } from 'react'
import { NavLink, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { apiGet } from './api'
import Toaster from './components/Toaster'
import { detectLang, makeT, setLang, type Lang } from './i18n'
import Bind from './pages/Bind'
import Debug from './pages/Debug'
import Files from './pages/Files'
import Recent from './pages/Recent'
import Settings from './pages/Settings'
import Tasks from './pages/Tasks'
import Transfers from './pages/Transfers'
import Trash from './pages/Trash'

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

const navItems = [
  { to: '/files', key: 'nav.files' },
  { to: '/recent', key: 'nav.recent' },
  { to: '/tasks', key: 'nav.tasks' },
  { to: '/transfers', key: 'nav.transfers' },
  { to: '/trash', key: 'nav.trash' },
  { to: '/settings', key: 'nav.settings' },
]

const titles: Record<string, string> = {
  '/files': 'nav.files',
  '/recent': 'nav.recent',
  '/tasks': 'nav.tasks',
  '/transfers': 'nav.transfers',
  '/trash': 'nav.trash',
  '/settings': 'nav.settings',
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

  // The stored language belongs to the fnOS user; the browser's is only the
  // starting guess.
  useEffect(() => {
    apiGet<Record<string, string>>('user-settings')
      .then((s) => {
        if (s.language === 'en' || s.language === 'zh-CN') setLanguage(s.language)
      })
      .catch(() => undefined)
    const onLanguage = (e: Event) => {
      const detail = (e as CustomEvent<string>).detail
      if (detail === 'en' || detail === 'zh-CN') setLanguage(detail)
    }
    window.addEventListener('pikpak:language', onLanguage)
    return () => window.removeEventListener('pikpak:language', onLanguage)
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
              {t(item.key)}
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
          {me?.debug && (
            <NavLink to="/debug" className="nav-item subtle">
              {t('nav.debug')}
            </NavLink>
          )}
        </div>
      </aside>

      <div className="content">
        <header className="topbar">
          <strong>{title}</strong>
          {account && (
            <span className="quota-pill" title={`${pct.toFixed(1)}%`}>
              <span>
                {formatBytes(account.quota_usage)} / {formatBytes(account.quota_limit)}
              </span>
              <span className="quota-meter" aria-hidden="true">
                <span style={{ width: `${pct}%` }} />
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

        <main className="page">
          <Routes>
            <Route path="/" element={<Navigate to="/files" replace />} />
            <Route path="/files/*" element={<Files />} />
            <Route path="/recent" element={<Recent />} />
            <Route path="/tasks" element={<Tasks />} />
            <Route path="/transfers" element={<Transfers />} />
            <Route path="/trash" element={<Trash />} />
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
