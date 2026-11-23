import { formatSize, isFolder, type FileItem } from './files'

// onlyFolderEntry returns the folder a share browser should enter without
// showing the root, when the root holds exactly one entry and that entry is a
// folder: there is nothing to choose, so the extra level is pure navigation.
// A root that still has another page is never skipped, or its other entries
// would have no way back.
export function onlyFolderEntry(items: FileItem[], nextPageToken: string): FileItem | null {
  if (nextPageToken) return null
  if (items.length !== 1) return null
  return isFolder(items[0]) ? items[0] : null
}

type ShareBadgeKind = 'star' | 'restored'

// shareBadgeKind picks the one badge a share listing shows on a video. 已收藏
// wins over 已转存: the star is the user's own decision about the video, while
// a copy in Pack From Shared is only something the app did on their behalf.
export function shareBadgeKind(restored: boolean, favorited: boolean): ShareBadgeKind | null {
  if (favorited) return 'star'
  if (restored) return 'restored'
  return null
}

// 分享收藏夹 reads the totals the server remembered; it never asks for a
// count itself. A share visited for the first time (or after a day) has none
// yet, and shows a dash until the next look at the list.
type ShareTotals = { file_count: number; total_size: number; stats_checked: boolean }

export function favoriteCountText(fav: ShareTotals): string {
  return fav.file_count >= 0 ? fav.file_count.toLocaleString() : '—'
}

export function favoriteSizeText(fav: ShareTotals): string {
  if (fav.total_size > 0) return formatSize(fav.total_size)
  // A counted share with nothing in it is 0 B; an uncounted one is unknown.
  return fav.file_count === 0 ? '0 B' : '—'
}

export function favoriteStatsHint(fav: ShareTotals): string {
  if (fav.file_count >= 0) return ''
  return fav.stats_checked ? '无法统计这个分享' : '进入分享后自动统计'
}

// 分享收藏夹 sorts the cached list in the browser: there is nothing to ask
// the server for. 'default' keeps the order the list arrived in.
export type FavoriteSortKey = 'default' | 'name' | 'owner' | 'count' | 'size' | 'shared' | 'expire'
export type FavoriteSortDir = 'asc' | 'desc'

type FavoriteSortable = {
  display_title: string
  title: string
  share_id: string
  owner_name: string
  file_count: number
  total_size: number
  share_created_at: string
  expire_at: number
}

type SortValue = { v: number | string; unknown?: boolean }

function sortValue(fav: FavoriteSortable, key: Exclude<FavoriteSortKey, 'default'>): SortValue {
  switch (key) {
    case 'name':
      return { v: fav.display_title || fav.title || fav.share_id }
    case 'owner':
      return { v: fav.owner_name, unknown: fav.owner_name === '' }
    case 'count':
      return { v: fav.file_count, unknown: fav.file_count < 0 }
    case 'size':
      return { v: fav.total_size, unknown: fav.total_size < 0 }
    case 'shared': {
      const at = Date.parse(fav.share_created_at)
      return { v: Number.isNaN(at) ? 0 : at, unknown: Number.isNaN(at) }
    }
    case 'expire':
      // -1 is PikPak's "never expires": the farthest date, not an unknown.
      return fav.expire_at === -1
        ? { v: Number.MAX_SAFE_INTEGER }
        : { v: fav.expire_at, unknown: fav.expire_at === 0 }
  }
}

export function sortShareFavorites<T extends FavoriteSortable>(
  items: T[],
  key: FavoriteSortKey,
  dir: FavoriteSortDir,
): T[] {
  if (key === 'default') return items.slice()
  const sign = dir === 'asc' ? 1 : -1
  return items.slice().sort((a, b) => {
    const x = sortValue(a, key)
    const y = sortValue(b, key)
    // A missing total, creator or date sinks to the bottom either way; it is
    // not a small value that flips to the top when the order reverses.
    if (x.unknown || y.unknown) {
      if (x.unknown && y.unknown) return 0
      return x.unknown ? 1 : -1
    }
    if (typeof x.v === 'number' && typeof y.v === 'number') return (x.v - y.v) * sign
    return String(x.v).localeCompare(String(y.v), 'zh') * sign
  })
}
