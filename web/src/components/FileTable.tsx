import type { MouseEvent } from 'react'
import { formatSize, isFolder, type FileItem } from '../files'
import { ChevronRight, FileIcon } from './icons'

type Props = {
  items: FileItem[]
  selected?: Set<string>
  onSelect?: (index: number, e: MouseEvent) => void
  onOpen?: (f: FileItem) => void
  onMenu?: (e: MouseEvent, f: FileItem) => void
  onDragStart?: (f: FileItem) => void
  onDropOn?: (folderId: string) => void
  // badge puts the local status before the icon, where a list keeps the eye.
  badge?: (f: FileItem) => { icon: string; title: string } | null
}

// FileTable renders the list view with the Finder's selection: a row is
// selected by clicking it, there are no checkboxes. Every interaction is
// optional, so the 最近 and 回收站 pages can show the same table read-only or
// with their own actions.
export default function FileTable({ items, selected, onSelect, onOpen, onMenu, onDragStart, onDropOn, badge }: Props) {
  return (
    <table className="file-table">
      <thead>
        <tr>
          <th className="col-open" />
          <th>文件名</th>
          <th className="col-size">大小</th>
          <th className="col-time">修改时间</th>
        </tr>
      </thead>
      <tbody>
        {items.map((f, index) => {
          const isSelected = selected?.has(f.id) ?? false
          const mark = badge?.(f)
          return (
            <tr
              key={f.id}
              data-file-id={f.id}
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
              onClick={(e) => onSelect?.(index, e)}
              onDoubleClick={() => onOpen?.(f)}
              onContextMenu={(e) => onMenu?.(e, f)}
            >
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
                  {mark && (
                    <span className="list-badge" title={mark.title} aria-label={mark.title}>
                      {mark.icon}
                    </span>
                  )}
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
