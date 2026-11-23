import { describe, expect, test } from 'vitest'
import { favoriteCountText, favoriteSizeText, favoriteStatsHint, onlyFolderEntry, shareBadgeKind, sortShareFavorites } from './shares'
import type { FileItem } from './files'

function item(id: string, kind: string): FileItem {
  return { id, parent_id: '', name: id, kind, size: 0 }
}

describe('onlyFolderEntry', () => {
  test('skips a root that holds one folder', () => {
    expect(onlyFolderEntry([item('A', 'drive#folder')], '')?.id).toBe('A')
  })

  test('does not skip a root holding one file', () => {
    expect(onlyFolderEntry([item('f', 'drive#file')], '')).toBeNull()
  })

  test('does not skip a root with more than one entry', () => {
    expect(onlyFolderEntry([item('A', 'drive#folder'), item('B', 'drive#folder')], '')).toBeNull()
  })

  // The first page can hold one folder while the root holds more; skipping
  // would hide entries with no way back.
  test('does not skip while the root has another page', () => {
    expect(onlyFolderEntry([item('A', 'drive#folder')], 'tok')).toBeNull()
  })

  test('does not skip an empty root', () => {
    expect(onlyFolderEntry([], '')).toBeNull()
  })
})

describe('shareBadgeKind', () => {
  test('a saved video keeps its star even when a copy also exists', () => {
    expect(shareBadgeKind(true, true)).toBe('star')
  })

  test('a restored video without a favourite shows the check', () => {
    expect(shareBadgeKind(true, false)).toBe('restored')
  })

  test('a favourite without a copy shows the star', () => {
    expect(shareBadgeKind(false, true)).toBe('star')
  })

  test('neither flag shows nothing', () => {
    expect(shareBadgeKind(false, false)).toBeNull()
  })
})

describe('分享收藏夹 totals come from the cache alone', () => {
  test('a share never counted shows a dash', () => {
    const fav = { file_count: -1, total_size: -1, stats_checked: false }
    expect(favoriteCountText(fav)).toBe('—')
    expect(favoriteSizeText(fav)).toBe('—')
    expect(favoriteStatsHint(fav)).toBe('进入分享后自动统计')
  })

  test('a counted share shows its totals', () => {
    const fav = { file_count: 1234, total_size: 1073741824, stats_checked: true }
    expect(favoriteCountText(fav)).toBe('1,234')
    expect(favoriteSizeText(fav)).toBe('1.0 G')
    expect(favoriteStatsHint(fav)).toBe('')
  })

  test('an empty share is counted as zero, not unknown', () => {
    const fav = { file_count: 0, total_size: 0, stats_checked: true }
    expect(favoriteCountText(fav)).toBe('0')
    expect(favoriteSizeText(fav)).toBe('0 B')
  })

  test('a share that could not be counted says so', () => {
    const fav = { file_count: -1, total_size: -1, stats_checked: true }
    expect(favoriteCountText(fav)).toBe('—')
    expect(favoriteSizeText(fav)).toBe('—')
    expect(favoriteStatsHint(fav)).toBe('无法统计这个分享')
  })
})

describe('sortShareFavorites', () => {
  function fav(over: Record<string, unknown> = {}) {
    return {
      display_title: '',
      title: '',
      share_id: '',
      owner_name: '',
      file_count: 0,
      total_size: 0,
      share_created_at: '',
      expire_at: 0,
      ...over,
    }
  }

  test('default keeps the order the server sent', () => {
    const items = [fav({ share_id: 'b' }), fav({ share_id: 'a' })]
    expect(sortShareFavorites(items, 'default', 'asc').map((f) => f.share_id)).toEqual(['b', 'a'])
  })

  test('name sorts by the title shown, falling back to the share id', () => {
    const items = [
      fav({ share_id: 'S1', title: 'Beta' }),
      fav({ share_id: 'S2', title: 'Alpha' }),
      fav({ share_id: 'S3', display_title: 'Aardvark' }),
      fav({ share_id: 'A0' }),
    ]
    expect(sortShareFavorites(items, 'name', 'asc').map((f) => f.share_id)).toEqual(['A0', 'S3', 'S2', 'S1'])
  })

  test('size sorts numerically and keeps unknown totals last', () => {
    const items = [
      fav({ share_id: 'small', total_size: 10 }),
      fav({ share_id: 'unknown', total_size: -1 }),
      fav({ share_id: 'large', total_size: 900 }),
    ]
    expect(sortShareFavorites(items, 'size', 'desc').map((f) => f.share_id)).toEqual(['large', 'small', 'unknown'])
    expect(sortShareFavorites(items, 'size', 'asc').map((f) => f.share_id)).toEqual(['small', 'large', 'unknown'])
  })

  test('file count compares as a number', () => {
    const items = [
      fav({ share_id: 'ten', file_count: 10 }),
      fav({ share_id: 'two', file_count: 2 }),
      fav({ share_id: 'unknown', file_count: -1 }),
    ]
    expect(sortShareFavorites(items, 'count', 'asc').map((f) => f.share_id)).toEqual(['two', 'ten', 'unknown'])
  })

  test('share time compares instants, not strings', () => {
    const items = [
      fav({ share_id: 'local', share_created_at: '2026-01-01T00:00:00+08:00' }),
      fav({ share_id: 'utc', share_created_at: '2025-12-31T20:00:00Z' }),
      fav({ share_id: 'unknown', share_created_at: '' }),
    ]
    expect(sortShareFavorites(items, 'shared', 'desc').map((f) => f.share_id)).toEqual(['utc', 'local', 'unknown'])
  })

  test('expiry puts 永久 and unknown last', () => {
    const items = [
      fav({ share_id: 'soon', expire_at: 1000 }),
      fav({ share_id: 'later', expire_at: 2000 }),
      fav({ share_id: 'never', expire_at: -1 }),
      fav({ share_id: 'unknown', expire_at: 0 }),
    ]
    expect(sortShareFavorites(items, 'expire', 'asc').map((f) => f.share_id)).toEqual(['soon', 'later', 'never', 'unknown'])
  })

  test('an unknown creator sorts last', () => {
    const items = [
      fav({ share_id: 'b', owner_name: 'Bob' }),
      fav({ share_id: 'none', owner_name: '' }),
      fav({ share_id: 'a', owner_name: 'Alice' }),
    ]
    expect(sortShareFavorites(items, 'owner', 'asc').map((f) => f.share_id)).toEqual(['a', 'b', 'none'])
  })
})
