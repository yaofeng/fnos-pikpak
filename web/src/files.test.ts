import { describe, expect, test } from 'vitest'
import { filterFiles, formatSize, isVideo, sortFiles, type FileItem } from './files'

function file(id: string, name: string, extra: Partial<FileItem> = {}): FileItem {
  return { id, parent_id: '', name, kind: 'drive#file', size: 0, ...extra }
}

function folder(id: string, name: string): FileItem {
  return { id, parent_id: '', name, kind: 'drive#folder', size: 0 }
}

describe('sortFiles', () => {
  test('keeps folders before files whatever the sort key', () => {
    const items = [
      file('1', 'aaa.mkv', { size: 10 }),
      folder('2', 'zzz'),
      file('3', 'bbb.mkv', { size: 20 }),
      folder('4', 'aaa'),
    ]
    for (const key of ['name', 'size', 'modified'] as const) {
      const sorted = sortFiles(items, key, 'asc')
      expect(sorted.slice(0, 2).every((f) => f.kind === 'drive#folder')).toBe(true)
    }
  })

  test('orders Chinese names with a zh collation', () => {
    const items = [file('1', '文件十'), file('2', '文件二'), file('3', '文件一')]
    const names = sortFiles(items, 'name', 'asc').map((f) => f.name)
    expect(names).toEqual([...names].sort((a, b) => a.localeCompare(b, 'zh-CN')))
  })

  test('sorts numbers and reverses direction', () => {
    const items = [file('1', 'a', { size: 5 }), file('2', 'b', { size: 99 }), file('3', 'c', { size: 50 })]
    expect(sortFiles(items, 'size', 'desc').map((f) => f.size)).toEqual([99, 50, 5])
  })

  test('does not mutate the input', () => {
    const items = [file('2', 'b'), file('1', 'a')]
    sortFiles(items, 'name', 'asc')
    expect(items.map((f) => f.name)).toEqual(['b', 'a'])
  })
})

describe('filterFiles', () => {
  test('matches names case-insensitively', () => {
    const items = [file('1', 'Holiday.MKV'), file('2', 'notes.txt'), file('3', 'holiday-list.txt')]
    expect(filterFiles(items, 'holiday', 'all').map((f) => f.id)).toEqual(['1', '3'])
  })

  test('filters folders and files separately', () => {
    const items = [folder('1', 'dir'), file('2', 'a.txt')]
    expect(filterFiles(items, '', 'folder').map((f) => f.id)).toEqual(['1'])
    expect(filterFiles(items, '', 'file').map((f) => f.id)).toEqual(['2'])
  })
})

describe('helpers', () => {
  test('formats sizes', () => {
    expect(formatSize(0)).toBe('—')
    expect(formatSize(999)).toBe('999 B')
    expect(formatSize(1536)).toBe('1.5 KiB')
    expect(formatSize(20 << 20)).toBe('20.0 MiB')
  })

  test('detects video by category or mime', () => {
    expect(isVideo(file('1', 'a', { category: 'VIDEO' }))).toBe(true)
    expect(isVideo(file('2', 'a', { mime_type: 'video/mp4' }))).toBe(true)
    expect(isVideo(file('3', 'a', { mime_type: 'text/plain' }))).toBe(false)
  })
})
