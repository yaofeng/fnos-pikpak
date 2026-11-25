import { describe, expect, it, test } from 'vitest'
import { cacheKeys, dropCachePrefix, readCache, resetCache, writeCache } from './listCache'

describe('listCache', () => {
  test('returns what was written', () => {
    resetCache()
    writeCache('files:', { items: [1, 2] })
    expect(readCache<{ items: number[] }>('files:')).toEqual({ items: [1, 2] })
    expect(readCache('nothing')).toBeUndefined()
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

  it('cacheKeys 含账号段', () => {
    expect(cacheKeys.files('a1', 'root', 'all')).toBe('files:a1:root|all')
    expect(cacheKeys.trash('a1')).toBe('trash:a1')
    expect(cacheKeys.recent('all')).toBe('recent:all')
    expect(cacheKeys.recent('a1')).toBe('recent:a1')
  })
})
