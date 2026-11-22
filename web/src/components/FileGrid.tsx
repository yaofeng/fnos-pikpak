import { useState, type MouseEvent } from 'react'
import { formatSize, isFolder, type FileItem } from '../files'
import { FileIcon } from './icons'

type Props = {
  items: FileItem[]
  selected: Set<string>
  onSelect: (index: number, e: MouseEvent) => void
  onOpen: (f: FileItem) => void
  onMenu: (e: MouseEvent, f: FileItem) => void
  onDragStart: (f: FileItem) => void
  onDropOn: (folderId: string) => void
  // thumbSrc maps a file to a thumbnail URL. A tile falls back to its icon
  // when the request fails (no thumbnail, expired link, offline).
  thumbSrc?: (f: FileItem) => string
}

// FileGrid is the icon view of the Finder: no card around each item, the name
// highlighted when selected, and a single click selecting instead of a
// checkbox.
export default function FileGrid({ items, selected, onSelect, onOpen, onMenu, onDragStart, onDropOn, thumbSrc }: Props) {
  const [failed, setFailed] = useState<Set<string>>(() => new Set())
  return (
    <div className="file-grid" role="listbox" aria-multiselectable="true">
      {items.map((f, index) => (
        <div
          key={f.id}
          data-file-id={f.id}
          role="option"
          aria-selected={selected.has(f.id)}
          className={selected.has(f.id) ? 'tile selected' : 'tile'}
          draggable
          onDragStart={() => onDragStart(f)}
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
          </div>
          <div className="tile-label">
            <span className="tile-name" title={f.name}>
              {f.name}
            </span>
            <span className="tile-meta">{isFolder(f) ? '文件夹' : formatSize(f.size)}</span>
          </div>
        </div>
      ))}
    </div>
  )
}
