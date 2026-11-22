import { describe, expect, test } from 'vitest'
import { pathsFrom } from './sdkPaths'

describe('pathsFrom', () => {
  test('takes the paths out of a successful bridge response', () => {
    expect(pathsFrom({ code: 0, data: ['/vol1/a', '/vol1/b'] })).toEqual(['/vol1/a', '/vol1/b'])
  })

  test('drops duplicates and non-absolute entries', () => {
    expect(pathsFrom({ code: 0, data: ['/vol1/a', '/vol1/a', 'relative', ''] })).toEqual(['/vol1/a'])
  })

  test('returns nothing when the user cancelled or the bridge failed', () => {
    expect(pathsFrom(undefined)).toEqual([])
    expect(pathsFrom({ code: 1, data: ['/vol1/a'] })).toEqual([])
    expect(pathsFrom({ code: 0 })).toEqual([])
  })
})
