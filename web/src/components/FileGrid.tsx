import type { MouseEvent } from 'react'
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
}

// FileGrid is the icon view of the Finder: no card around each item, the name
// highlighted when selected, and a single click selecting instead of a
// checkbox.
export default function FileGrid({ items, selected, onSelect, onOpen, onMenu, onDragStart, onDropOn }: Props) {
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
            <FileIcon file={f} size={56} />
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
