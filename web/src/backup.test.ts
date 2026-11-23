import { describe, expect, test } from 'vitest'
import { backupFilename, filenameFromDisposition, restoreSummary, type RestoreResult } from './backup'

describe('backupFilename', () => {
  test('stamps the local date in YYYYMMDD', () => {
    expect(backupFilename(new Date(2026, 10, 23))).toBe('pikpak-backup-20261123.json')
    expect(backupFilename(new Date(2026, 0, 5))).toBe('pikpak-backup-20260105.json')
  })
})

describe('filenameFromDisposition', () => {
  test('reads a quoted filename', () => {
    expect(filenameFromDisposition('attachment; filename="pikpak-backup-20261123.json"')).toBe(
      'pikpak-backup-20261123.json',
    )
  })

  test('reads an unquoted filename', () => {
    expect(filenameFromDisposition('attachment; filename=pikpak-backup.json')).toBe('pikpak-backup.json')
  })

  test('falls back when the header is missing or empty', () => {
    expect(filenameFromDisposition(null)).toBe('pikpak-backup.json')
    expect(filenameFromDisposition('attachment')).toBe('pikpak-backup.json')
  })
})

describe('restoreSummary', () => {
  const result: RestoreResult = {
    settings_overwritten: 4,
    share_favorites: { added: 2, skipped: 1 },
    video_favorites: { added: 3, skipped: 5 },
    video_groups: { added: 1, merged: 2 },
  }

  test('names every count the restore reports', () => {
    const text = restoreSummary(result)
    expect(text).toContain('设置已覆盖 4 项')
    expect(text).toContain('分享收藏新增 2、跳过 1')
    expect(text).toContain('视频收藏新增 3、跳过 5')
    expect(text).toContain('分组新增 1、合并 2')
  })
})
