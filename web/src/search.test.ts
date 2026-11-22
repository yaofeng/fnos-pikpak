import { describe, expect, it } from 'vitest'
import { crumbFromURL } from './search'

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
