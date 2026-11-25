import { describe, expect, it, test } from 'vitest'
import { fileKind, filesQuery, filterFiles, formatSize, groupByAccount, isVideo, sortFiles, thumbPath, type FileItem } from './files'

function file(id: string, name: string, extra: Partial<FileItem> = {}): FileItem {
  return { id, parent_id: '', name, kind: 'drive#file', size: 0, ...extra }
}

function folder(id: string, name: string): FileItem {
  return { id, parent_id: '', name, kind: 'drive#folder', size: 0 }
}

describe('filesQuery', () => {
	test('merged scope walks by seg params, no parent_id', () => {
		expect(filesQuery('all', ['电影', '动作'], '', 'all')).toBe(
			'account=all&seg=' + encodeURIComponent('电影') + '&seg=' + encodeURIComponent('动作'),
		)
	})
	test('single scope keeps the parent_id form byte-identical to 2.0.0', () => {
		expect(filesQuery('acct-a', [], 'fld-1', 'all')).toBe('parent_id=fld-1&account=acct-a')
	})
	test('kind filter rides along in both modes', () => {
		expect(filesQuery('all', [], '', 'folder')).toContain('kind=drive%23folder')
	})
})

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

describe('groupByAccount', () => {
  test('groupByAccount splits merged rows and keeps unstamped under scope', () => {
    const rows = [
      { id: '1', account_id: 'a' },
      { id: '2', account_id: 'b' },
      { id: '3' }, // 单账号列表的无戳行
    ] as never[]
    const got = groupByAccount(rows as never, 'acct-a')
    expect(got.get('a')).toEqual(['1'])
    expect(got.get('b')).toEqual(['2'])
    expect(got.get('acct-a')).toEqual(['3'])
  })
})

describe('helpers', () => {
  test('formats sizes', () => {
    expect(formatSize(0)).toBe('—')
    expect(formatSize(999)).toBe('999 B')
    expect(formatSize(1536)).toBe('1.5 K')
    expect(formatSize(20 << 20)).toBe('20.0 M')
  })

  test('detects video by category or mime', () => {
    expect(isVideo(file('1', 'a', { category: 'VIDEO' }))).toBe(true)
    expect(isVideo(file('2', 'a', { mime_type: 'video/mp4' }))).toBe(true)
    expect(isVideo(file('3', 'a', { mime_type: 'text/plain' }))).toBe(false)
  })

  test('builds a drive thumbnail path', () => {
    expect(thumbPath('f1')).toBe('thumb?file_id=f1')
  })

  test('builds a share thumbnail path', () => {
    expect(thumbPath('sf2', 'VOabc123')).toBe('thumb?share_id=VOabc123&file_id=sf2')
  })

  it('thumbPath 追加 account', () => {
    expect(thumbPath('f1', undefined, 'a2')).toBe('thumb?file_id=f1&account=a2')
  })

  test('fileKind picks the icon the file manager shows', () => {
    expect(fileKind(folder('1', 'dir'))).toBe('folder')
    expect(fileKind(file('2', 'a', { category: 'VIDEO' }))).toBe('video')
    expect(fileKind(file('3', 'a.mp3', { mime_type: 'audio/mpeg' }))).toBe('audio')
    expect(fileKind(file('4', 'a.png', { mime_type: 'image/png' }))).toBe('image')
    expect(fileKind(file('5', 'a.zip'))).toBe('archive')
    expect(fileKind(file('6', 'a.MKV'))).toBe('video')
    expect(fileKind(file('7', 'a.pdf'))).toBe('doc')
    expect(fileKind(file('8', 'a.bin'))).toBe('other')
  })
})
