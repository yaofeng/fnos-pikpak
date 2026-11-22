// The line icons follow fnOS 文件管理: 16px, currentColor, 1.8px strokes, so
// buttons read the same next to native fnOS controls. Folder and file glyphs
// are filled instead, the way the native file list draws them.
import type { SVGProps } from 'react'
import { fileKind, type FileItem, type FileKind } from '../files'

type IconProps = { size?: number }

function stroke(size: number): SVGProps<SVGSVGElement> {
  return {
    width: size,
    height: size,
    viewBox: '0 0 24 24',
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.8,
    strokeLinecap: 'round',
    strokeLinejoin: 'round',
    'aria-hidden': true,
  }
}

export function ChevronLeft({ size = 16 }: IconProps) {
  return (
    <svg {...stroke(size)}>
      <path d="m15 5-7 7 7 7" />
    </svg>
  )
}

export function ChevronRight({ size = 16 }: IconProps) {
  return (
    <svg {...stroke(size)}>
      <path d="m9 5 7 7-7 7" />
    </svg>
  )
}

export function RefreshIcon({ size = 16 }: IconProps) {
  return (
    <svg {...stroke(size)}>
      <path d="M20 12a8 8 0 1 1-2.34-5.66" />
      <path d="M20 4v5h-5" />
    </svg>
  )
}

export function SearchIcon({ size = 16 }: IconProps) {
  return (
    <svg {...stroke(size)}>
      <circle cx="11" cy="11" r="6.5" />
      <path d="m16 16 4 4" />
    </svg>
  )
}

export function UploadIcon({ size = 16 }: IconProps) {
  return (
    <svg {...stroke(size)}>
      <path d="M12 16V4" />
      <path d="m7 9 5-5 5 5" />
      <path d="M4 16v3a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-3" />
    </svg>
  )
}

export function FolderPlusIcon({ size = 16 }: IconProps) {
  return (
    <svg {...stroke(size)}>
      <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z" />
      <path d="M12 11v5" />
      <path d="M9.5 13.5h5" />
    </svg>
  )
}

export function DownloadIcon({ size = 16 }: IconProps) {
  return (
    <svg {...stroke(size)}>
      <path d="M12 4v12" />
      <path d="m7 11 5 5 5-5" />
      <path d="M4 17v2a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-2" />
    </svg>
  )
}

export function RestoreIcon({ size = 16 }: IconProps) {
  return (
    <svg {...stroke(size)}>
      <path d="M4 12a8 8 0 1 0 2.34-5.66" />
      <path d="M4 4v5h5" />
    </svg>
  )
}

export function TrashIcon({ size = 16 }: IconProps) {
  return (
    <svg {...stroke(size)}>
      <path d="M4 7h16" />
      <path d="M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" />
      <path d="M6 7l1 12a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-12" />
    </svg>
  )
}

export function MoreIcon({ size = 16 }: IconProps) {
  return (
    <svg {...stroke(size)}>
      <circle cx="5" cy="12" r="1.1" fill="currentColor" />
      <circle cx="12" cy="12" r="1.1" fill="currentColor" />
      <circle cx="19" cy="12" r="1.1" fill="currentColor" />
    </svg>
  )
}

export function SortIcon({ size = 16 }: IconProps) {
  return (
    <svg {...stroke(size)}>
      <path d="M8 5v14" />
      <path d="m4.5 8.5 3.5-3.5 3.5 3.5" />
      <path d="M16 19V5" />
      <path d="m12.5 15.5 3.5 3.5 3.5-3.5" />
    </svg>
  )
}

export function ListIcon({ size = 16 }: IconProps) {
  return (
    <svg {...stroke(size)}>
      <path d="M9 6h11" />
      <path d="M9 12h11" />
      <path d="M9 18h11" />
      <path d="M4.5 6h.01" />
      <path d="M4.5 12h.01" />
      <path d="M4.5 18h.01" />
    </svg>
  )
}

export function GridIcon({ size = 16 }: IconProps) {
  return (
    <svg {...stroke(size)}>
      <rect x="4" y="4" width="6.5" height="6.5" rx="1" />
      <rect x="13.5" y="4" width="6.5" height="6.5" rx="1" />
      <rect x="4" y="13.5" width="6.5" height="6.5" rx="1" />
      <rect x="13.5" y="13.5" width="6.5" height="6.5" rx="1" />
    </svg>
  )
}

// FolderGlyph is the blue two-tone folder the fnOS list and grid both use.
export function FolderGlyph({ size = 22 }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true">
      <path
        d="M3 6.3c0-1 .8-1.8 1.8-1.8h4.4c.5 0 1 .2 1.3.6l1.5 1.4h7.2c1 0 1.8.8 1.8 1.8v9.4c0 1-.8 1.8-1.8 1.8H4.8c-1 0-1.8-.8-1.8-1.8V6.3Z"
        fill="#5a9ff3"
      />
      <path d="M3 9.2h18v8.5c0 1-.8 1.8-1.8 1.8H4.8c-1 0-1.8-.8-1.8-1.8V9.2Z" fill="#8ec2fa" />
    </svg>
  )
}

const tints: Record<Exclude<FileKind, 'folder'>, string> = {
  video: '#e2574c',
  audio: '#8e6bd9',
  image: '#3fa66a',
  archive: '#c98a2e',
  doc: '#4f9cf0',
  other: '#9aa0a6',
}

// FileGlyph is a paper sheet tinted by kind, with a mark that tells video,
// audio, image and archive apart at a glance.
export function FileGlyph({ kind, size = 22 }: { kind: FileKind; size?: number }) {
  if (kind === 'folder') return <FolderGlyph size={size} />
  const tint = tints[kind]
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true">
      <path
        d="M5.5 3.2h7.6L18.5 8.5v12.3H5.5V3.2Z"
        fill={tint}
        fillOpacity="0.14"
        stroke={tint}
        strokeWidth="1.3"
        strokeLinejoin="round"
      />
      <path d="M13.1 3.2v5.3h5.4" stroke={tint} strokeWidth="1.3" fill="none" strokeLinejoin="round" />
      {kind === 'video' && (
        <path d="M10.2 12.2v5l4.4-2.5-4.4-2.5Z" fill={tint} />
      )}
      {kind === 'audio' && (
        <path
          d="M10 17v-4.6l3.6-.8v4.4"
          stroke={tint}
          strokeWidth="1.3"
          fill="none"
          strokeLinecap="round"
        />
      )}
      {kind === 'image' && (
        <>
          <circle cx="10" cy="12.6" r="1.1" fill={tint} />
          <path d="m8 17 2.6-2.8 1.7 1.6 1.6-1.6L16 16.6" stroke={tint} strokeWidth="1.3" fill="none" strokeLinejoin="round" />
        </>
      )}
      {kind === 'archive' && (
        <path d="M10 11h4M10 13.4h4M10 15.8h4" stroke={tint} strokeWidth="1.2" strokeLinecap="round" />
      )}
      {kind === 'doc' && (
        <path d="M8.6 12.4h6.8M8.6 15h6.8M8.6 17.4h4.2" stroke={tint} strokeWidth="1.2" strokeLinecap="round" />
      )}
      {kind === 'other' && (
        <path d="M8.6 15h6.8" stroke={tint} strokeWidth="1.2" strokeLinecap="round" />
      )}
    </svg>
  )
}

export function FileIcon({ file, size = 22 }: { file: FileItem; size?: number }) {
  return <FileGlyph kind={fileKind(file)} size={size} />
}
