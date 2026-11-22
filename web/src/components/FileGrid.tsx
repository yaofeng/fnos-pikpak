import type { MouseEvent } from 'react'
import { formatSize, isFolder, isVideo, type FileItem } from '../files'

type Props = {
  items: FileItem[]
  selected: Set<string>
  onToggle: (id: string) => void
  onOpen: (f: FileItem) => void
  onMenu: (e: MouseEvent, f: FileItem) => void
  onDragStart: (f: FileItem) => void
  onDropOn: (folderId: string) => void
}

export default function FileGrid({ items, selected, onToggle, onOpen, onMenu, onDragStart, onDropOn }: Props) {
  return (
    <div className="file-grid">
      {items.map((f) => (
        <div
          key={f.id}
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
          onClick={() => onToggle(f.id)}
          onDoubleClick={() => onOpen(f)}
          onContextMenu={(e) => onMenu(e, f)}
        >
          <div className={`tile-icon ${isFolder(f) ? 'folder' : isVideo(f) ? 'video' : ''}`}>
            {isFolder(f) ? '📁' : isVideo(f) ? '🎬' : '📄'}
          </div>
          <div className="tile-name" title={f.name}>
            {f.name}
          </div>
          <div className="tile-meta muted">{isFolder(f) ? '文件夹' : formatSize(f.size)}</div>
        </div>
      ))}
    </div>
  )
}
