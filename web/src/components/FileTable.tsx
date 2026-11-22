import type { MouseEvent } from 'react'
import { formatSize, isFolder, type FileItem } from '../files'
import { ChevronRight, FileIcon } from './icons'

type Props = {
  items: FileItem[]
  selected?: Set<string>
  selectable?: boolean
  onToggle?: (id: string) => void
  onOpen?: (f: FileItem) => void
  onMenu?: (e: MouseEvent, f: FileItem) => void
  onDragStart?: (f: FileItem) => void
  onDropOn?: (folderId: string) => void
}

// FileTable renders the list view the fnOS 文件管理 uses: chevron, checkbox,
// glyph, name, size, time. Every interaction is optional, so the 最近 and
// 回收站 pages can show the same table read-only or with their own actions.
export default function FileTable({
  items,
  selected,
  selectable = false,
  onToggle,
  onOpen,
  onMenu,
  onDragStart,
  onDropOn,
}: Props) {
  return (
    <table className="file-table">
      <thead>
        <tr>
          {selectable && <th className="col-check" />}
          <th className="col-open" />
          <th>文件名</th>
          <th className="col-size">大小</th>
          <th className="col-time">修改时间</th>
        </tr>
      </thead>
      <tbody>
        {items.map((f) => {
          const isSelected = selected?.has(f.id) ?? false
          return (
            <tr
              key={f.id}
              className={isSelected ? 'selected' : ''}
              draggable={Boolean(onDragStart)}
              onDragStart={() => onDragStart?.(f)}
              onDragOver={(e) => {
                if (onDropOn && isFolder(f)) e.preventDefault()
              }}
              onDrop={(e) => {
                if (!onDropOn || !isFolder(f)) return
                e.preventDefault()
                onDropOn(f.id)
              }}
              onClick={() => onToggle?.(f.id)}
              onDoubleClick={() => onOpen?.(f)}
              onContextMenu={(e) => onMenu?.(e, f)}
            >
              {selectable && (
                <td className="col-check">
                  <input
                    type="checkbox"
                    checked={isSelected}
                    onChange={() => onToggle?.(f.id)}
                    onClick={(e) => e.stopPropagation()}
                    aria-label={`选择 ${f.name}`}
                  />
                </td>
              )}
              <td className="col-open">
                {isFolder(f) && onOpen && (
                  <button
                    type="button"
                    className="row-open"
                    onClick={(e) => {
                      e.stopPropagation()
                      onOpen(f)
                    }}
                    aria-label={`打开 ${f.name}`}
                    title="打开"
                  >
                    <ChevronRight size={14} />
                  </button>
                )}
              </td>
              <td className="col-name">
                <span className="name-cell">
                  <span className="file-icon">
                    <FileIcon file={f} />
                  </span>
                  <span className="name-text">{f.name}</span>
                </span>
              </td>
              <td className="col-size muted">{isFolder(f) ? '—' : formatSize(f.size)}</td>
              <td className="col-time muted">{f.modified_time ? new Date(f.modified_time).toLocaleString() : '—'}</td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}
