import { Activity, lazy, Suspense, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { NavLink, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { apiGet } from './api'
import { AccountProvider, useAccounts } from './AccountContext'
import { sumQuota } from './accounts'
import Toaster from './components/Toaster'
import { BookmarkIcon, ClockIcon, FolderIcon, GearIcon, GlobeIcon, MagnetIcon, StarIcon, TransferIcon, TrashIcon, WrenchIcon } from './components/icons'
import { detectLang, makeT, setLang, type Lang } from './i18n'
import { activePath, KEEP_ALIVE_PATHS, nextVisited, pageKey, restoreScroll, saveScroll } from './keepAlive'
import { resetCache } from './listCache'
import { formatSize } from './files'
import { isActive, speedOf, transferred, type TransferJob } from './transfers'
import Bind from './pages/Bind'
import Favorites from './pages/Favorites'
import Files from './pages/Files'
import Search from './pages/Search'
import Recent from './pages/Recent'
import Settings from './pages/Settings'
import Tasks from './pages/Tasks'
import Transfers from './pages/Transfers'
import Trash from './pages/Trash'
import VideoFavorites from './pages/VideoFavorites'

// The debug page builds the Trim SDK and every probe at module scope, so it
// is loaded only when someone opens it.
const Debug = lazy(() => import('./pages/Debug'))

// Me is the app shell's first response: who the fnOS user is, whether any
// PikPak account is linked, and what this session may do. `accounts` is the
// count; the rows themselves come from the account context.
type Me = {
  uid: number
  bound: boolean
  status: string
  display_name: string
  vip_type: string
  accounts: number
  debug?: boolean
  capabilities: Record<string, boolean>
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

// formatBytes is the same scale the file lists use; a real zero reads as 0 B
// here, where the lists show a dash for a missing size.
function formatBytes(n: number): string {
  return n ? formatSize(n) : '0 B'
}

function formatSpeed(bytesPerSec: number): string {
  return `${formatBytes(Math.round(bytesPerSec))}/s`
}

// keepAliveElement maps a keep-alive page path to its component, keyed by
// page|account: switching the PikPak account changes the key, which remounts
// the page so its UI state (folder stack, search terms) resets and the data
// is re-queried for the new account.
function keepAliveElement(page: string, currentId: string): ReactNode {
  switch (page) {
    case '/files':
      return <Files key={pageKey(page, currentId)} />
    case '/recent':
      return <Recent key={pageKey(page, currentId)} />
    case '/tasks':
      return <Tasks key={pageKey(page, currentId)} />
    case '/transfers':
      return <Transfers key={pageKey(page, currentId)} />
    case '/trash':
      return <Trash key={pageKey(page, currentId)} />
    case '/favorites':
      return <Favorites key={pageKey(page, currentId)} />
    case '/video-favorites':
      return <VideoFavorites key={pageKey(page, currentId)} />
    case '/search':
      return <Search key={pageKey(page, currentId)} />
    case '/settings':
      return <Settings key={pageKey(page, currentId)} />
    default:
      return null
  }
}

// The provider sits above the shell so the sidebar, the banners and every
// page read one account list.
export default function App() {
  return (
    <AccountProvider>
      <Shell />
    </AccountProvider>
  )
}

function Shell() {
  const { accounts, reload, currentId } = useAccounts()
  const [lang, setLanguage] = useState<Lang>(() => detectLang())
  const t = makeT(lang)
  setLang(lang)
  const location = useLocation()
  const navigate = useNavigate()
  const [me, setMe] = useState<Me | null>(null)
  const [transfer, setTransfer] = useState<transferSummary | null>(null)
  // The previous poll's active bytes, turned into a rate by the next one.
  const transferSample = useRef<{ at: number; down: number; up: number } | null>(null)

  // Keep-alive: the page owning the current pathname, and the set of pages
  // that have ever been active. A page mounts into its Activity on first
  // activation only (the first paint mounts just the entry page), then stays
  // mounted — hidden while another page is up — so its UI state, DOM and
  // scroll position survive navigation. F5 starts a fresh session by design.
  const page = activePath(location.pathname)
  const [visited, setVisited] = useState<ReadonlySet<string>>(() => (page ? new Set([page]) : new Set()))

  useEffect(() => {
    if (page) setVisited((prev) => nextVisited(prev, page))
  }, [page])

  // Scroll memory for the five plain pages whose scroller is <main> itself
  // (recent, tasks, transfers, trash, settings): hiding such a page's
  // Activity removes its height and the browser clamps main.scrollTop to 0,
  // so the offset is recorded while scrolling and replayed on return. The
  // restore runs in a layout effect, before paint: a returning page's
  // content is already mounted (hidden Activities keep their DOM) and a
  // first-visit page's content mounts in the same commit, so the subtree is
  // always measurable here — no requestAnimationFrame needed. Writing is
  // guarded by `page`, which is only ever a keep-alive path.
  const mainRef = useRef<HTMLElement | null>(null)
  const scrollStore = useRef(new Map<string, number>())

  useLayoutEffect(() => {
    const main = mainRef.current
    if (!main) return
    const onScroll = () => {
      if (page) saveScroll(scrollStore.current, page, main.scrollTop)
    }
    main.addEventListener('scroll', onScroll)
    if (page) {
      const top = restoreScroll(scrollStore.current, page)
      if (main.scrollTop !== top) main.scrollTop = top
    }
    return () => main.removeEventListener('scroll', onScroll)
  }, [page])

  useEffect(() => {
    const load = () => {
      apiGet<Me>('me')
        .then(setMe)
        .catch(() => setMe(null))
    }
    load()
    // Binding, rebinding or unbinding changes whose files everything is. The
    // page caches are module-global, so they would otherwise keep showing the
    // previous PikPak account's listing until something forced a refresh.
    // The account context hears the same event and refetches the list.
    const onAccount = () => {
      resetCache()
      load()
    }
    window.addEventListener('pikpak:account-changed', onAccount)
    return () => window.removeEventListener('pikpak:account-changed', onAccount)
  }, [])

  // The quota pill follows the accounts: a restore, download or upload on the
  // NAS changes usage, so the list is re-read every 10 seconds while bound.
  // The server serves cached per-account snapshots, so the poll is cheap.
  useEffect(() => {
    if (!me?.bound) return
    const timer = window.setInterval(() => void reload(), 10_000)
    return () => window.clearInterval(timer)
  }, [me?.bound, reload])

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
        if (prev) {
          downSpeed = speedOf({ bytes: prev.down, at: prev.at }, { bytes: downBytes, at: now })
          upSpeed = speedOf({ bytes: prev.up, at: prev.at }, { bytes: upBytes, at: now })
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

  // Pages draw their own menus, so the browser's default context menu (reload,
  // save image, back…) is never right-clicked into view. Form fields keep it,
  // where paste and spell-check still matter.
  useEffect(() => {
    const onContextMenu = (e: MouseEvent) => {
      const target = e.target as HTMLElement | null
      if (target?.closest('input, textarea, select, [contenteditable="true"]')) return
      e.preventDefault()
    }
    window.addEventListener('contextmenu', onContextMenu)
    return () => window.removeEventListener('contextmenu', onContextMenu)
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

  // Every account's quota together: rows with a failed probe carry no
  // trustworthy numbers and stay out of the total.
  const { usage, limit } = sumQuota(accounts)
  const pct = limit > 0 ? Math.min(100, (usage / limit) * 100) : 0
  const reauthAccounts = accounts.filter((a) => a.status === 'reauth_required')

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
          {accounts.length > 0 && (
            <span className="quota-pill sidebar-quota" title={`${pct.toFixed(1)}%`}>
              <span
                className="quota-fill"
                aria-hidden="true"
                style={{ width: pct > 0 ? `max(${pct}%, 4px)` : '0' }}
              />
              <span className="quota-text">
                {formatBytes(usage)} / {formatBytes(limit)}
              </span>
            </span>
          )}
          {accounts.length === 0 && (
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

        {/* One banner per account whose authorization died; the bind page
            re-authorizes rows one by one. */}
        {reauthAccounts.map((a) => (
          <div className="notice danger" key={a.id}>
            <span>{t('state.reauth_account').replace('{name}', a.display_name || a.id)}</span>
            <button type="button" className="notice-action" onClick={() => navigate('/bind')}>
              {t('action.rebind')}
            </button>
          </div>
        ))}

        {/* These pages fill the window: their file areas scroll, not the page. */}
        <main
          ref={mainRef}
          className={
            location.pathname === '/search' ||
            location.pathname.startsWith('/files') ||
            location.pathname.startsWith('/favorites') ||
            location.pathname.startsWith('/video-favorites')
              ? 'page page-fill'
              : 'page'
          }
        >
          {/* Kept-alive pages live outside <Routes>: each visited page stays
              mounted in an Activity, visible only while it owns the current
              path. Hidden Activities unmount their effects, so polling pages
              pause while out of sight and replay from listCache on return.
              The `kept === page` arm mounts a never-visited page in the same
              render that activates it — no blank frame before the visited
              effect commits. */}
          {KEEP_ALIVE_PATHS.map((kept) => {
            if (!(visited.has(kept) || kept === page)) return null
            return (
              <Activity key={kept} mode={kept === page ? 'visible' : 'hidden'}>
                {keepAliveElement(kept, currentId)}
              </Activity>
            )
          })}
          {/* Placeholder routes keep the URL semantics (NavLink highlighting,
              "/" and catch-all redirects) without mounting pages here; only
              truly unknown paths reach the catch-all. */}
          <Routes>
            <Route path="/" element={<Navigate to="/files" replace />} />
            <Route path="/files/*" element={null} />
            <Route path="/recent" element={null} />
            <Route path="/tasks" element={null} />
            <Route path="/transfers" element={null} />
            <Route path="/trash" element={null} />
            <Route path="/favorites" element={null} />
            <Route path="/video-favorites" element={null} />
            <Route path="/search" element={null} />
            <Route path="/settings" element={null} />
            <Route path="/bind" element={<Bind />} />
            {me?.debug && (
              <Route
                path="/debug"
                element={
                  <Suspense fallback={<p className="muted">加载中…</p>}>
                    <Debug />
                  </Suspense>
                }
              />
            )}
            <Route path="*" element={<Navigate to="/files" replace />} />
          </Routes>
        </main>

        <Toaster />

      </div>
    </div>
  )
}
