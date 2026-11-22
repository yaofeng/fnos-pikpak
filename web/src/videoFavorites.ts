// 视频收藏夹 groups saved videos by the person who shared them. When PikPak
// does not report an owner the share title stands in, and when that is missing
// too the group is simply 未知分享.

export type VideoFavorite = {
  id: number
  share_id: string
  file_id: string
  name: string
  size: number
  owner: string
  share_title: string
  display_name: string
  created_at: string
}

export type VideoFavoriteGroup = { key: string; label: string; items: VideoFavorite[] }

const unknownLabel = '未知分享'

export function groupVideoFavorites(items: VideoFavorite[]): VideoFavoriteGroup[] {
  const groups: VideoFavoriteGroup[] = []
  const byKey = new Map<string, VideoFavoriteGroup>()
  for (const item of items) {
    let key: string
    let label: string
    if (item.owner.trim()) {
      key = 'owner:' + item.owner.trim()
      label = item.owner.trim()
    } else if (item.share_title.trim()) {
      key = 'title:' + item.share_title.trim()
      label = item.share_title.trim()
    } else {
      key = 'unknown'
      label = unknownLabel
    }
    let group = byKey.get(key)
    if (!group) {
      group = { key, label, items: [] }
      byKey.set(key, group)
      groups.push(group)
    }
    group.items.push(item)
  }
  for (const group of groups) {
    group.items.sort((a, b) => (a.created_at < b.created_at ? 1 : a.created_at > b.created_at ? -1 : 0))
  }
  return groups
}
