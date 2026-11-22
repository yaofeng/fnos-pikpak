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
  // group_label files the video under a local group name instead of its
  // sharing user; empty means "derive it from the share".
  group_label: string
  created_at: string
}

export type VideoFavoriteGroup = { key: string; label: string; items: VideoFavorite[] }

const unknownLabel = '未知分享'

// groupKeyOf names the group a saved video sits in. The server builds the
// same key, so 重命名分组 can address one.
export function groupKeyOf(item: VideoFavorite): string {
  if (item.group_label.trim()) return 'label:' + item.group_label.trim()
  if (item.owner.trim()) return 'owner:' + item.owner.trim()
  if (item.share_title.trim()) return 'title:' + item.share_title.trim()
  return 'unknown'
}

export function groupVideoFavorites(items: VideoFavorite[]): VideoFavoriteGroup[] {
  const groups: VideoFavoriteGroup[] = []
  const byKey = new Map<string, VideoFavoriteGroup>()
  for (const item of items) {
    let key: string
    let label: string
    if (item.group_label.trim()) {
      key = 'label:' + item.group_label.trim()
      label = item.group_label.trim()
    } else if (item.owner.trim()) {
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

// FileGrid keys its tiles by file id, while the page selects favourites by
// their row id; this is the bridge between the two.
export function selectedFileIDs(picked: VideoFavorite[]): Set<string> {
  return new Set(picked.map((f) => f.file_id))
}
