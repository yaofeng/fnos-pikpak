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

export type ShareBadgeKind = 'star' | 'restored'

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
