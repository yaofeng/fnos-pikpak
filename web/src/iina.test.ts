import { describe, expect, test } from 'vitest'
import { iinaURL, isMacOS } from './iina'

describe('isMacOS', () => {
  test('only macOS gets IINA', () => {
    expect(isMacOS('Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/537.36')).toBe(true)
    expect(isMacOS('Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36')).toBe(false)
    expect(isMacOS('Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36')).toBe(false)
  })
})

describe('iinaURL', () => {
  test('wraps a stream URL for IINA', () => {
    expect(iinaURL('http://nas/api/stream/f1?t=x')).toBe(
      'iina://weblink?url=http%3A%2F%2Fnas%2Fapi%2Fstream%2Ff1%3Ft%3Dx',
    )
  })
})
