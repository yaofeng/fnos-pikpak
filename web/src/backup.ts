// A backup is plain JSON. These helpers keep the download filename and the
// restore summary out of the page, so both can be unit tested without a DOM.

type RestoreCounts = { added: number; skipped: number }
type RestoreGroupCounts = { added: number; merged: number }

export type RestoreResult = {
  settings_overwritten: number
  share_favorites: RestoreCounts
  video_favorites: RestoreCounts
  video_groups: RestoreGroupCounts
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n)
}

// backupFilename matches the server's Content-Disposition name, so a browser
// that ignores the header still saves the file under the same name.
export function backupFilename(date: Date): string {
  return `pikpak-backup-${date.getFullYear()}${pad2(date.getMonth() + 1)}${pad2(date.getDate())}.json`
}

// filenameFromDisposition reads the name a download response suggested. A
// missing or unreadable header falls back to the base name.
export function filenameFromDisposition(header: string | null): string {
  if (!header) return 'pikpak-backup.json'
  const match = /filename\*?=(?:UTF-8'')?"?([^";]+)"?/i.exec(header)
  const name = match?.[1]?.trim()
  return name || 'pikpak-backup.json'
}

// restoreSummary is the line shown after an import: every count the server
// reports, in the order the page lists the data.
export function restoreSummary(result: RestoreResult): string {
  return [
    `设置已覆盖 ${result.settings_overwritten} 项`,
    `分享收藏新增 ${result.share_favorites.added}、跳过 ${result.share_favorites.skipped}`,
    `视频收藏新增 ${result.video_favorites.added}、跳过 ${result.video_favorites.skipped}`,
    `分组新增 ${result.video_groups.added}、合并 ${result.video_groups.merged}`,
  ].join('；')
}
