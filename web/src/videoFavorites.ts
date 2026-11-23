// 视频收藏夹 groups saved videos under the name they show — the sharing user,
// a local group name, or the share title. Videos sharing a name are one group.

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
  // restored says a copy already sits in Pack From Shared; 完整播放 reuses it
  // instead of restoring again, and the tile shows ✅.
  restored: boolean
  restored_file_id: string
  created_at: string
}

export type VideoFavoriteGroup = { key: string; label: string; items: VideoFavorite[] }

const unknownLabel = '未知分享'

// groupLabelOf is the name a saved video shows under: the local label if one
// was chosen, else the sharing user, else the share title, else 未知分享. The
// server builds the same name.
export function groupLabelOf(item: VideoFavorite): string {
  return item.group_label.trim() || item.owner.trim() || item.share_title.trim() || unknownLabel
}

// groupKeyOf names the group a saved video sits in. It is built from the
// displayed name alone, so videos that happen to share a name are one group,
// and 重命名分组 addresses them all.
export function groupKeyOf(item: VideoFavorite): string {
  return 'label:' + groupLabelOf(item)
}

export function groupVideoFavorites(items: VideoFavorite[]): VideoFavoriteGroup[] {
  const groups: VideoFavoriteGroup[] = []
  const byKey = new Map<string, VideoFavoriteGroup>()
  for (const item of items) {
    const label = groupLabelOf(item)
    const key = 'label:' + label
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
