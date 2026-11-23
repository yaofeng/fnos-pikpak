import { describe, expect, test } from 'vitest'
import { favoriteCountText, favoriteSizeText, favoriteStatsHint, onlyFolderEntry, shareBadgeKind } from './shares'
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
    expect(favoriteSizeText(fav)).toBe('1.0 GiB')
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
