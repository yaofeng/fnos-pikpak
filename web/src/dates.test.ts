import { describe, expect, it } from 'vitest'
import { dateText } from './dates'

describe('dateText', () => {
  it('shows a dash when there is no timestamp', () => {
    expect(dateText('')).toBe('—')
    expect(dateText(undefined)).toBe('—')
  })

  it('renders a timestamp in the reader locale', () => {
    const at = new Date('2026-11-22T08:00:00Z')
    expect(dateText('2026-11-22T08:00:00Z')).toBe(at.toLocaleString())
  })

  it('shows an unparseable value as it arrived', () => {
    expect(dateText('not a date')).toBe('not a date')
  })
})
