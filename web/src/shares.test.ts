import { describe, expect, test } from 'vitest'
import { onlyFolderEntry, shareBadgeKind } from './shares'
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
