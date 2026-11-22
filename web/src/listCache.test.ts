import { describe, expect, test } from 'vitest'
import { dropCache, dropCachePrefix, readCache, resetCache, writeCache } from './listCache'

describe('listCache', () => {
  test('returns what was written', () => {
    resetCache()
    writeCache('files:', { items: [1, 2] })
    expect(readCache<{ items: number[] }>('files:')).toEqual({ items: [1, 2] })
    expect(readCache('nothing')).toBeUndefined()
  })

  test('drops one entry', () => {
    resetCache()
    writeCache('a', 1)
    writeCache('b', 2)
    dropCache('a')
    expect(readCache('a')).toBeUndefined()
    expect(readCache('b')).toBe(2)
  })

  test('drops every entry with a prefix', () => {
    resetCache()
    writeCache('files:root', 1)
    writeCache('files:abc', 2)
    writeCache('trash', 3)
    dropCachePrefix('files:')
    expect(readCache('files:root')).toBeUndefined()
    expect(readCache('files:abc')).toBeUndefined()
    expect(readCache('trash')).toBe(3)
  })
})
