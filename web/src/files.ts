export type FileItem = {
  id: string
  parent_id: string
  name: string
  kind: string
  size: number
  phase?: string
  category?: string
  mime_type?: string
  modified_time?: string
  // account_id stamps merged cross-account listings (最近) with the account
  // that served the row; single-account views leave it unset.
  account_id?: string
  // account_name rides along on the same merged rows so the UI can say which
  // account a tile came from without a byId lookup; unset on single-account
  // listings.
  account_name?: string
  // Local badges the share browser and 视频收藏夹 draw on a tile: a copy
  // already sits in Pack From Shared, or the video is in 视频收藏夹.
  restored?: boolean
  favorited?: boolean
}

export type SortKey = 'name' | 'size' | 'modified'
export type SortDir = 'asc' | 'desc'
export type KindFilter = 'all' | 'folder' | 'file'

export function isFolder(f: FileItem): boolean {
  return f.kind === 'drive#folder'
}

// filesQuery builds the listing query for one scope: merged mode walks by
// path segments, single-account mode keeps the 2.0.0 parent_id form. limit
// and page_token are not here — fetchPage appends them, so the parameter set
// per mode stays the 2.0.0 one (parent_id, limit, page_token?, kind?, account).
export function filesQuery(scope: string, segs: string[], folderId: string, kind: KindFilter): string {
  const params = new URLSearchParams()
  if (scope === 'all') {
    params.set('account', 'all')
    for (const seg of segs) params.append('seg', seg)
  } else {
    params.set('parent_id', folderId)
    params.set('account', scope)
  }
  if (kind !== 'all') params.set('kind', kind === 'folder' ? 'drive#folder' : 'drive#file')
  return params.toString()
}

// trashQuery builds the trash listing query for one scope: merged mode asks
// for every account, single-account mode pins the 2.0.0 account param.
// page_token appends only when there is one, so a first page's request keeps
// the 2.0.0 parameter set (account, then the token when paging).
export function trashQuery(scope: string, pageToken: string): string {
  const params = new URLSearchParams({ account: scope })
  if (pageToken) params.set('page_token', pageToken)
  return params.toString()
}

// groupByAccount splits selected rows into per-account id lists so a merged
// view can route each row's mutation to the account that served it; rows
// without a stamp fall under the scope itself (single-account listings).
export function groupByAccount(items: FileItem[], scope: string): Map<string, string[]> {
  const groups = new Map<string, string[]>()
  for (const f of items) {
    const key = f.account_id || scope
    const ids = groups.get(key)
    if (ids) ids.push(f.id)
    else groups.set(key, [f.id])
  }
  return groups
}

// sortFiles orders folders first — that is what a file manager user expects
// regardless of the chosen key — then applies the key to both groups.
export function sortFiles(items: FileItem[], key: SortKey, dir: SortDir): FileItem[] {
  const sign = dir === 'asc' ? 1 : -1
  const compare = (a: FileItem, b: FileItem): number => {
    if (isFolder(a) !== isFolder(b)) return isFolder(a) ? -1 : 1
    switch (key) {
      case 'size':
        return sign * (a.size - b.size)
      case 'modified':
        return sign * (timeOf(a) - timeOf(b))
      default:
        return sign * a.name.localeCompare(b.name, 'zh-CN')
    }
  }
  return [...items].sort(compare)
}

function timeOf(f: FileItem): number {
  const t = f.modified_time ? Date.parse(f.modified_time) : NaN
  return Number.isNaN(t) ? 0 : t
}

// filterFiles is the local filter over already loaded pages: PikPak cannot
// search by name, so the UI says so and filters what it has.
export function filterFiles(items: FileItem[], q: string, kind: KindFilter): FileItem[] {
  const needle = q.trim().toLowerCase()
  return items.filter((f) => {
    if (kind === 'folder' && !isFolder(f)) return false
    if (kind === 'file' && isFolder(f)) return false
    if (!needle) return true
    return f.name.toLowerCase().includes(needle)
  })
}

export function formatSize(n: number): string {
  if (!n) return '—'
  const units = ['B', 'K', 'M', 'G', 'T']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

export function isVideo(f: FileItem): boolean {
  if (f.category === 'VIDEO') return true
  return (f.mime_type ?? '').startsWith('video/')
}

export type FileKind = 'folder' | 'video' | 'audio' | 'image' | 'archive' | 'doc' | 'other'

const videoExtensions = ['mkv', 'mp4', 'avi', 'mov', 'wmv', 'flv', 'webm', 'ts', 'm4v', 'mpg', 'mpeg']
const archiveExtensions = ['zip', 'rar', '7z', 'tar', 'gz', 'bz2', 'xz', 'iso']
const docExtensions = ['pdf', 'txt', 'md', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx', 'csv', 'epub']

function extensionOf(name: string): string {
  const dot = name.lastIndexOf('.')
  return dot < 0 ? '' : name.slice(dot + 1).toLowerCase()
}

// fileKind decides which icon a row shows, the way fnOS 文件管理 picks icons:
// the server's category and mime type first, then the extension.
export function fileKind(f: FileItem): FileKind {
  if (isFolder(f)) return 'folder'
  if (isVideo(f)) return 'video'
  const mime = f.mime_type ?? ''
  if (mime.startsWith('audio/') || f.category === 'AUDIO') return 'audio'
  if (mime.startsWith('image/') || f.category === 'IMAGE') return 'image'
  const ext = extensionOf(f.name)
  if (videoExtensions.includes(ext)) return 'video'
  if (archiveExtensions.includes(ext)) return 'archive'
  if (docExtensions.includes(ext)) return 'doc'
  return 'other'
}

// thumbPath addresses the server-side thumbnail proxy. A share file needs its
// share id because the bytes come through that share's session; the path has
// no /api prefix so callers can put it after whatever base they use. An
// account id scopes the proxy to that account's drive file.
export function thumbPath(fileID: string, shareID?: string, account?: string): string {
  const params = new URLSearchParams()
  if (shareID) params.set('share_id', shareID)
  params.set('file_id', fileID)
  if (account) params.set('account', account)
  return `thumb?${params.toString()}`
}
