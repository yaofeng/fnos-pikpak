import { describe, expect, it, test } from 'vitest'
import {
  fileKind,
  filesQuery,
  filterFiles,
  formatSize,
  groupByAccount,
  isVideo,
  mergeFolderRows,
  sortFiles,
  thumbPath,
  trashQuery,
  type FileItem,
} from './files'

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

describe('trashQuery', () => {
	test('merged scope asks the backend for every account', () => {
		expect(trashQuery('all', '')).toBe('account=all')
		expect(trashQuery('all', 'tok-1')).toBe('account=all&page_token=tok-1')
	})
	test('single scope keeps the account param byte-identical to 2.0.0', () => {
		expect(trashQuery('acct-a', '')).toBe('account=acct-a')
		expect(trashQuery('acct-a', 'tok-2')).toBe('account=acct-a&page_token=tok-2')
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

describe('mergeFolderRows', () => {
  test('同名跨账号目录合并为一行且无戳', () => {
    // 截图场景：合并视图根目录下三个 My Pack 各占一行 → 应合并为一行，
    // 悬浮提示纯名字、无 @后缀（无单一目标账号，行内操作由守卫禁用）。
    const got = mergeFolderRows([
      folder('f1', 'My Pack'),
      { ...folder('f2', 'My Pack'), account_id: 'acct-a', account_name: '主号' },
      { ...folder('f3', 'My Pack'), account_id: 'acct-b', account_name: '副号' },
    ])
    expect(got).toHaveLength(1)
    expect(got[0].id).toBe('f1')
    expect(got[0].name).toBe('My Pack')
    expect(got[0].account_id).toBeUndefined()
    expect(got[0].account_name).toBeUndefined()
  })

  test('同名同账号目录保留戳与首行 id（行内操作照常可用）', () => {
    const got = mergeFolderRows([
      { ...folder('f1', 'test'), account_id: 'acct-a', account_name: '主号' },
      { ...folder('f5', 'test'), account_id: 'acct-a', account_name: '主号' },
    ])
    expect(got).toHaveLength(1)
    expect(got[0].id).toBe('f1')
    expect(got[0].account_id).toBe('acct-a')
    expect(got[0].account_name).toBe('主号')
  })

  test('同名文件不受影响（各占一行）', () => {
    const got = mergeFolderRows([
      { ...file('d1', 'a.mkv', { size: 10 }), account_id: 'acct-a', account_name: '主号' },
      { ...file('d2', 'a.mkv', { size: 20 }), account_id: 'acct-b', account_name: '副号' },
    ])
    expect(got.map((f) => f.id)).toEqual(['d1', 'd2'])
    expect(got[0].account_id).toBe('acct-a')
    expect(got[1].account_id).toBe('acct-b')
  })

  test('同名目录与同名文件互不合并', () => {
    const got = mergeFolderRows([
      folder('f1', '同名'),
      { ...file('d1', '同名'), account_id: 'acct-b' },
    ])
    expect(got.map((f) => f.id)).toEqual(['f1', 'd1'])
  })

  test('不同名目录不动', () => {
    const got = mergeFolderRows([
      { ...folder('f1', '电影'), account_id: 'acct-a' },
      { ...folder('f2', '剧集'), account_id: 'acct-b' },
    ])
    expect(got.map((f) => f.id)).toEqual(['f1', 'f2'])
    expect(got.every((f) => f.account_id)).toBe(true)
  })

  test('空数组与单行恒等，且不改动输入', () => {
    const single = { ...folder('f1', 'test'), account_id: 'acct-a' }
    expect(mergeFolderRows([])).toEqual([])
    expect(mergeFolderRows([single])).toEqual([single])
    const items = [
      { ...folder('f1', 'My Pack'), account_id: 'acct-a' },
      { ...folder('f2', 'My Pack'), account_id: 'acct-b' },
    ]
    mergeFolderRows(items)
    expect(items.map((f) => f.account_id)).toEqual(['acct-a', 'acct-b'])
  })

  test('合并行落在首个同名目录出现的位置', () => {
    const got = mergeFolderRows([
      { ...folder('f1', 'My Pack'), account_id: 'acct-a' },
      { ...file('d1', 'a.mkv'), account_id: 'acct-a' },
      { ...folder('f2', 'My Pack'), account_id: 'acct-b' },
    ])
    // 排序随后由 sortFiles 接管；函数自身保持首现顺序。
    expect(got.map((f) => f.id)).toEqual(['f1', 'd1'])
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
