import { useState, type MouseEvent, type ReactNode } from 'react'
import { formatSize, isFolder, type FileItem } from '../files'
import { FileIcon } from './icons'

type Props = {
  items: FileItem[]
  selected: Set<string>
  onSelect: (index: number, e: MouseEvent) => void
  onOpen: (f: FileItem) => void
  onMenu: (e: MouseEvent, f: FileItem) => void
  // Omitting onDragStart also drops the draggable attribute, the way
  // FileTable treats it: a read-only page passes neither.
  onDragStart?: (f: FileItem) => void
  onDropOn: (folderId: string) => void
  // thumbSrc maps a file to a thumbnail URL. A tile falls back to its icon
  // when the request fails (no thumbnail, expired link, offline).
  thumbSrc?: (f: FileItem) => string
  // badge draws a local status in the tile's bottom-left corner: a check for
  // 已转存, a star for a video in 视频收藏夹. The caller decides which one wins.
  badge?: (f: FileItem) => { icon: ReactNode; title: string; kind: 'restored' | 'star' } | null
  // accountOf names the account a merged row came from, shown as muted small
  // text under the tile name. Merged pages pass it; single-account views omit.
  accountOf?: (f: FileItem) => string
}

// FileGrid is the icon view of the Finder: no card around each item, the name
// highlighted when selected, and a single click selecting instead of a
// checkbox.
export default function FileGrid({ items, selected, onSelect, onOpen, onMenu, onDragStart, onDropOn, thumbSrc, badge, accountOf }: Props) {
  const [failed, setFailed] = useState<Set<string>>(() => new Set())
  return (
    <div className="file-grid" role="listbox" aria-multiselectable="true">
      {items.map((f, index) => {
        const mark = badge?.(f)
        return (
        <div
          key={f.id}
          data-file-id={f.id}
          role="option"
          aria-selected={selected.has(f.id)}
          className={selected.has(f.id) ? 'tile selected' : 'tile'}
          draggable={Boolean(onDragStart)}
          onDragStart={() => onDragStart?.(f)}
          onDragOver={(e) => {
            if (isFolder(f)) e.preventDefault()
          }}
          onDrop={(e) => {
            if (!isFolder(f)) return
            e.preventDefault()
            onDropOn(f.id)
          }}
          onClick={(e) => onSelect(index, e)}
          onDoubleClick={() => onOpen(f)}
          onContextMenu={(e) => onMenu(e, f)}
        >
          <div className="tile-icon">
            {thumbSrc && !isFolder(f) && !failed.has(f.id) ? (
              <img
                className="tile-thumb"
                src={thumbSrc(f)}
                alt=""
                loading="lazy"
                decoding="async"
                onError={() =>
                  setFailed((prev) => {
                    const next = new Set(prev)
                    next.add(f.id)
                    return next
                  })
                }
              />
            ) : (
              <FileIcon file={f} size={56} />
            )}
            {mark && (
              <span className={`tile-badge ${mark.kind}`} title={mark.title} aria-label={mark.title}>
                {mark.icon}
              </span>
            )}
            {!isFolder(f) && <span className="tile-size">{formatSize(f.size)}</span>}
          </div>
          <div className="tile-label">
            <span className="tile-name" title={f.account_name ? `${f.name}@${f.account_name}` : f.name}>
              {f.name}
            </span>
            {accountOf && <span className="muted small">{accountOf(f)}</span>}
          </div>
        </div>
        )
      })}
    </div>
  )
}
