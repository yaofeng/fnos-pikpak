import { describe, expect, it } from 'vitest'
import { appendResults, crumbFromURL, hasMorePages, shareCount } from './search'

describe('crumbFromURL', () => {
  it('shows the host and each path segment', () => {
    expect(crumbFromURL('https://mypikpak.com/s/VN1')).toBe('mypikpak.com › s › VN1')
  })

  it('keeps just the host for the site root', () => {
    expect(crumbFromURL('https://mypikpak.com/')).toBe('mypikpak.com')
  })

  it('falls back to the raw text when the URL does not parse', () => {
    expect(crumbFromURL('not a url')).toBe('not a url')
  })
})

describe('appendResults', () => {
  const hit = (url: string) => ({ title: url, url, snippet: '' })

  it('keeps the pages in the order they arrived', () => {
    const got = appendResults([hit('a'), hit('b')], [hit('c')])
    expect(got.map((r) => r.url)).toEqual(['a', 'b', 'c'])
  })

  it('drops a result the earlier pages already showed', () => {
    // A provider sometimes repeats a hit across pages; the list shows it once.
    const got = appendResults([hit('a'), hit('b')], [hit('b'), hit('c')])
    expect(got.map((r) => r.url)).toEqual(['a', 'b', 'c'])
  })

  it('shows a share once even when two PikPak hosts served it', () => {
    const got = appendResults(
      [hit('https://mypikpak.com/s/VN1')],
      [hit('https://mypikpak.net/s/VN1'), hit('https://mypikpak.com/s/VN2')],
    )
    expect(got.map((r) => r.url)).toEqual(['https://mypikpak.com/s/VN1', 'https://mypikpak.com/s/VN2'])
  })
})

describe('shareCount', () => {
  it('counts only the results the server marked as shares', () => {
    const got = shareCount([
      { title: '首页', url: 'https://mypikpak.com/', snippet: '' },
      { title: '分享', url: 'https://mypikpak.com/s/VN1', snippet: '', share: true },
      { title: '另一个分享', url: 'https://mypikpak.net/s/VN2', snippet: '', share: true },
    ])
    expect(got).toBe(2)
  })
})

describe('hasMorePages', () => {
  it('asks for another page while results keep coming', () => {
    expect(hasMorePages(1, 20, 10)).toBe(true)
  })

  it('stops at the provider’s last page', () => {
    expect(hasMorePages(10, 20, 10)).toBe(false)
  })

  it('stops when a page came back empty', () => {
    expect(hasMorePages(3, 0, 10)).toBe(false)
  })
})
