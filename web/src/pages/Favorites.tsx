import { useCallback, useEffect, useState, type MouseEvent } from 'react'
import StatusBar from '../components/StatusBar'
import { Link } from 'react-router-dom'
import { dateText } from '../dates'
import { apiDelete, apiGet, apiPatch, apiPost, basePath } from '../api'
import ShareBrowser from '../components/ShareBrowser'
import {
  BookmarkIcon,
  GlobeIcon,
  RefreshIcon,
  SearchIcon,
  SortIcon,
  TrashIcon,
} from '../components/icons'
import {
  favoriteCountText,
  favoriteSizeText,
  favoriteStatsHint,
  sortShareFavorites,
  type FavoriteSortDir,
  type FavoriteSortKey,
} from '../shares'
import { getLang, makeT } from '../i18n'
import { useFileSelection } from '../useFileSelection'

// A saved share, as the list route returns it. The pass code never comes back
// from the server; opening a favourite makes the server read the sealed one.
// The owner and the timestamps come from PikPak when the share was added or
// first visited; file_count/total_size are the recursive totals, counted in
// the background when the share is entered and cached for a day, with -1
// meaning "not counted or not countable".
type ShareFavorite = {
  id: number
  share_id: string
  title: string
  display_title: string
  protected: boolean
  created_at: string
  owner_name: string
  owner_avatar: string
  share_created_at: string
  expire_at: number
  file_count: number
  total_size: number
  stats_checked: boolean
}

function nameOf(fav: ShareFavorite): string {
  return fav.display_title || fav.title || fav.share_id
}

function expireText(expireAt: number, forever: string): string {
  // -1 is PikPak's "never expires"; 0 means the response did not say.
  if (expireAt === -1) return forever
  if (expireAt > 0) return new Date(expireAt * 1000).toLocaleString()
  return '—'
}

export default function Favorites() {
  const t = makeT(getLang())
  const [items, setItems] = useState<ShareFavorite[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [openFav, setOpenFav] = useState<ShareFavorite | null>(null)
  const [adding, setAdding] = useState(false)
  const [newURL, setNewURL] = useState('')
  const [addError, setAddError] = useState('')
  const [saving, setSaving] = useState(false)
  const [q, setQ] = useState('')
  const [sortKey, setSortKey] = useState<FavoriteSortKey>('default')
  const [sortDir, setSortDir] = useState<FavoriteSortDir>('asc')
  const [busy, setBusy] = useState(false)
  // Bumped when a share closes: the server started counting it as it was
  // opened, so the list takes a few cached reads before settling.
  const [visitReload, setVisitReload] = useState(0)

  // The list only reads the totals the server has cached. A share counted
  // for the first time — or recounted a day later — is visited from here, so
  // the next look shows its numbers.
  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const body = await apiGet<{ items: ShareFavorite[] }>('share-favorites')
      setItems(body.items ?? [])
    } catch (err) {
      setError((err as Error).message)
      setItems([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  // Coming back from a share, its count may still be running in the
  // background. Re-read the cache a few times instead of leaving the row
  // blank until the next manual refresh; every read is cache-only.
  useEffect(() => {
    if (!visitReload) return
    let cancelled = false
    const tick = async (attempt: number) => {
      await new Promise((resolve) => setTimeout(resolve, 2000))
      if (cancelled || attempt > 3) return
      try {
        const body = await apiGet<{ items: ShareFavorite[] }>('share-favorites')
        if (cancelled) return
        const list = body.items ?? []
        setItems(list)
        if (list.some((fav) => !fav.stats_checked)) void tick(attempt + 1)
      } catch {
        // Keep the list as it stands.
      }
    }
    void tick(1)
    return () => {
      cancelled = true
    }
  }, [visitReload])

  const shown = sortShareFavorites(
    (items ?? []).filter((fav) => {
      if (!q.trim()) return true
      const needle = q.trim().toLowerCase()
      return (
        nameOf(fav).toLowerCase().includes(needle) ||
        (fav.owner_name || '').toLowerCase().includes(needle) ||
        fav.share_id.toLowerCase().includes(needle)
      )
    }),
    sortKey,
    sortDir,
  )
  const order = shown.map((fav) => String(fav.id))
  const { selected, select, clear } = useFileSelection(order)
  const picked = shown.filter((fav) => selected.has(String(fav.id)))
  const one = picked.length === 1 ? picked[0] : null

  async function addShare() {
    const url = newURL.trim()
    if (!url) return
    setSaving(true)
    setAddError('')
    try {
      await apiPost('share-favorites', { url })
      setNewURL('')
      setAdding(false)
      setNotice(t('favorites.added'))
      await load()
    } catch (err) {
      setAddError((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  // Renaming is local only: it changes how this list shows the share, never
  // the share itself. An empty prompt restores the share's own title.
  async function rename(fav: ShareFavorite) {
    const next = window.prompt(t('favorites.rename_prompt'), nameOf(fav))
    if (next === null) return
    setBusy(true)
    setError('')
    try {
      const updated = await apiPatch<ShareFavorite>(`share-favorites/${fav.id}`, { title: next.trim() })
      setItems((prev) => (prev ?? []).map((f) => (f.id === fav.id ? { ...f, display_title: updated.display_title } : f)))
      setNotice(next.trim() ? t('favorites.renamed') : t('favorites.name_restored'))
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  async function remove(list: ShareFavorite[]) {
    if (list.length === 0) return
    const label =
      list.length === 1
        ? t('favorites.one_label').replace('{name}', nameOf(list[0]))
        : t('favorites.count_label').replace('{n}', String(list.length))
    if (!window.confirm(t('favorites.confirm_remove').replace('{label}', label))) return
    setBusy(true)
    setError('')
    try {
      for (const fav of list) await apiDelete(`share-favorites/${fav.id}`)
      setItems((prev) => (prev ?? []).filter((f) => !list.some((gone) => gone.id === f.id)))
      clear()
      setNotice(t('favorites.removed').replace('{label}', label))
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  // Double click opens the share itself; the star beside the address bar in
  // that view adds or removes the favourite.
  function open(fav: ShareFavorite) {
    setOpenFav(fav)
  }

  function refresh() {
    setNotice('')
    void load()
  }

  if (openFav !== null) {
    // The share opens as the page itself, laid out exactly like 我的文件; the
    // list reloads when it closes, since the star can add or remove favourites.
    return (
      <ShareBrowser
        key={openFav.id}
        favoriteId={openFav.id}
        initialTitle={nameOf(openFav)}
        onHome={() => {
          setOpenFav(null)
          void load()
          setVisitReload((n) => n + 1)
        }}
      />
    )
  }

  return (
    <section className="file-page">
      <div className="fm-nav">
        <button
          className="icon-btn"
          type="button"
          onClick={refresh}
          disabled={loading}
          title={t('common.refresh')}
          aria-label={t('common.refresh')}
        >
          <RefreshIcon />
        </button>
        <div className="crumb-box">
          <span className="fm-title">{t('favorites.title')}</span>
        </div>
        <label className="fm-search">
          <SearchIcon />
          <input
            type="search"
            placeholder={t('favorites.filter_placeholder')}
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </label>
      </div>

      <div className="fm-actions">
        <button className="btn-icon" type="button" onClick={() => { setAdding((v) => !v); setAddError('') }}>
          <BookmarkIcon />
          {t('favorites.add')}
        </button>
        <Link to="/search" className="btn-icon">
          <GlobeIcon />
          {t('favorites.go_search')}
        </Link>
        <button
          className="btn-icon"
          type="button"
          disabled={!one || busy}
          onClick={() => one && void rename(one)}
        >
          {t('account.rename')}
        </button>
        <button
          className="btn-icon"
          type="button"
          disabled={picked.length === 0 || busy}
          onClick={() => void remove(picked)}
        >
          <TrashIcon />
          {t('common.delete')}
        </button>

        <span className="fm-spacer" />

        <label className="fm-pick">
          <SortIcon />
          <select
            value={`${sortKey}:${sortDir}`}
            onChange={(e) => {
              const [k, d] = e.target.value.split(':')
              setSortKey(k as FavoriteSortKey)
              setSortDir(d as FavoriteSortDir)
            }}
            aria-label={t('favorites.sort_label')}
          >
            <option value="default:asc">{t('favorites.sort_default')}</option>
            <option value="name:asc">{t('favorites.col_name')} ↑</option>
            <option value="name:desc">{t('favorites.col_name')} ↓</option>
            <option value="owner:asc">{t('favorites.col_owner')} ↑</option>
            <option value="owner:desc">{t('favorites.col_owner')} ↓</option>
            <option value="count:desc">{t('favorites.col_count')} ↓</option>
            <option value="count:asc">{t('favorites.col_count')} ↑</option>
            <option value="size:desc">{t('favorites.col_size')} ↓</option>
            <option value="size:asc">{t('favorites.col_size')} ↑</option>
            <option value="shared:desc">{t('favorites.col_shared')} ↓</option>
            <option value="shared:asc">{t('favorites.col_shared')} ↑</option>
            <option value="expire:asc">{t('favorites.col_expire')} ↑</option>
            <option value="expire:desc">{t('favorites.col_expire')} ↓</option>
          </select>
        </label>
      </div>

      {adding && (
        <form
          className="fav-add"
          onSubmit={(e) => {
            e.preventDefault()
            void addShare()
          }}
        >
          <input
            value={newURL}
            onChange={(e) => setNewURL(e.target.value)}
            placeholder={t('favorites.url_placeholder')}
            aria-label={t('favorites.url_label')}
          />
          <button type="submit" className="primary" disabled={!newURL.trim() || saving}>
            {saving ? t('settings.saving') : t('settings.save')}
          </button>
          <button type="button" onClick={() => { setAdding(false); setNewURL(''); setAddError('') }}>
            {t('common.cancel')}
          </button>
          {addError && <span className="error small">{addError}</span>}
        </form>
      )}

      <div className="file-body">
        {items === null || (loading && (items ?? []).length === 0) ? (
          <p className="muted">{t('common.loading')}</p>
        ) : (items ?? []).length === 0 ? (
          <div className="empty-state">
            <BookmarkIcon size={28} />
            <p>{t('favorites.empty')}</p>
            <p className="muted small">
              {t('favorites.empty_hint')}
            </p>
          </div>
        ) : shown.length === 0 ? (
          <p className="muted">{t('favorites.no_match')}</p>
        ) : (
          <table className="file-table fav-table">
            <thead>
              <tr>
                <th className="col-open" />
                <th>{t('favorites.col_name')}</th>
                <th>{t('favorites.col_owner')}</th>
                <th className="col-size">{t('favorites.col_count')}</th>
                <th className="col-size">{t('favorites.col_size')}</th>
                <th className="col-time">{t('favorites.col_shared')}</th>
                <th className="col-time">{t('favorites.col_expire')}</th>
              </tr>
            </thead>
            <tbody>
              {shown.map((fav, index) => (
                <tr
                  key={fav.id}
                  className={selected.has(String(fav.id)) ? 'selected' : ''}
                  onClick={(e: MouseEvent) => select(index, e)}
                  onDoubleClick={() => open(fav)}
                >
                  <td className="col-open" />
                  <td>
                    <span className="shine-name">
                      <BookmarkIcon size={16} />
                      <span title={nameOf(fav)}>{nameOf(fav)}</span>
                    </span>
                    {fav.protected && <span className="badge-lock">{t('favorites.protected')}</span>}
                  </td>
                  <td>
                    <span className="fav-owner">
                      {fav.owner_avatar ? (
                        <img
                          className="fav-avatar"
                          src={`${basePath}/api/share-favorites/${fav.id}/avatar`}
                          alt=""
                          loading="lazy"
                          onError={(e) => {
                            e.currentTarget.style.visibility = 'hidden'
                          }}
                        />
                      ) : (
                        <span className="fav-avatar blank" aria-hidden="true" />
                      )}
                      <span>{fav.owner_name || '—'}</span>
                    </span>
                  </td>
                  <td className="col-size muted small" title={favoriteStatsHint(fav) || undefined}>
                    {favoriteCountText(fav)}
                  </td>
                  <td className="col-size muted small" title={favoriteStatsHint(fav) || undefined}>
                    {favoriteSizeText(fav)}
                  </td>
                  <td className="col-time muted small">{dateText(fav.share_created_at)}</td>
                  <td className="col-time muted small">{expireText(fav.expire_at, t('favorites.forever'))}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <StatusBar
        selected={picked.length}
        total={(items ?? []).length}
        notice={notice}
        error={error}
      />
    </section>
  )
}
