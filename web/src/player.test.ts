import { describe, expect, test } from 'vitest'
import { formatClock, guessMime, videoExtensions } from './player'

describe('guessMime', () => {
  test('keeps a specific upstream mime', () => {
    expect(guessMime('a.mp4', 'video/mp4')).toBe('video/mp4')
  })

  test('replaces the generic octet-stream with an extension guess', () => {
    // PikPak answers application/octet-stream for some files; browsers cannot
    // decide whether they can play that.
    expect(guessMime('a.mp4', 'application/octet-stream')).toBe('video/mp4')
    expect(guessMime('b.mkv', '')).toBe('video/x-matroska')
    expect(guessMime('c.WEBM', 'application/octet-stream')).toBe('video/webm')
  })

  test('falls back to the upstream value for unknown extensions', () => {
    expect(guessMime('a.zzz', 'application/octet-stream')).toBe('application/octet-stream')
    expect(guessMime('a.zzz', '')).toBe('application/octet-stream')
  })
})

describe('videoExtensions', () => {
  test('lists the extensions the player knows', () => {
    expect(videoExtensions()).toContain('.mp4')
    expect(videoExtensions()).toContain('.mkv')
  })
})

describe('formatClock', () => {
  test('renders minutes and padded seconds', () => {
    expect(formatClock(0)).toBe('0:00')
    expect(formatClock(65)).toBe('1:05')
  })

  test('adds hours only when needed', () => {
    expect(formatClock(3725)).toBe('1:02:05')
  })

  test('treats garbage as zero', () => {
    expect(formatClock(Number.NaN)).toBe('0:00')
    expect(formatClock(-5)).toBe('0:00')
  })
})
