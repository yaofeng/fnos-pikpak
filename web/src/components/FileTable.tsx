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

export default function FileTable({ items, selected, onToggle, onOpen, onMenu, onDragStart, onDropOn }: Props) {
  return (
    <table className="file-table">
      <thead>
        <tr>
          <th className="col-check" />
          <th>名称</th>
          <th className="col-size">大小</th>
          <th className="col-time">修改时间</th>
        </tr>
      </thead>
      <tbody>
        {items.map((f) => (
          <tr
            key={f.id}
            className={selected.has(f.id) ? 'selected' : ''}
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
            <td className="col-check">
              <input
                type="checkbox"
                checked={selected.has(f.id)}
                onChange={() => onToggle(f.id)}
                onClick={(e) => e.stopPropagation()}
              />
            </td>
            <td>
              <span className={`file-icon ${isFolder(f) ? 'folder' : isVideo(f) ? 'video' : ''}`}>
                {isFolder(f) ? '📁' : isVideo(f) ? '🎬' : '📄'}
              </span>
              {f.name}
            </td>
            <td className="col-size muted">{isFolder(f) ? '—' : formatSize(f.size)}</td>
            <td className="col-time muted">{f.modified_time ? new Date(f.modified_time).toLocaleString() : '—'}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
