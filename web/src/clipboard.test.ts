import { describe, expect, test, vi } from 'vitest'
import { copyText } from './clipboard'

describe('copyText', () => {
  test('writes through the clipboard API', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    await expect(copyText('https://mypikpak.com/device')).resolves.toBe(true)
    expect(writeText).toHaveBeenCalledWith('https://mypikpak.com/device')
    vi.unstubAllGlobals()
  })

  test('reports failure when nothing can copy', async () => {
    // No clipboard API and no document (the fallback needs a DOM).
    vi.stubGlobal('navigator', {})
    await expect(copyText('x')).resolves.toBe(false)
    vi.unstubAllGlobals()
  })
})
