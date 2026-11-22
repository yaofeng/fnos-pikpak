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
}

export type SortKey = 'name' | 'size' | 'modified'
export type SortDir = 'asc' | 'desc'
export type KindFilter = 'all' | 'folder' | 'file'

export function isFolder(f: FileItem): boolean {
  return f.kind === 'drive#folder'
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
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
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
