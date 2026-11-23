import { apiPost, ApiError } from './api'

// queueDownloads hands the drive file ids of restored copies to the download
// queue and returns the notice the page shows: how many were added, and how
// many same-name files the configured conflict policy skipped.
export async function queueDownloads(fileIDs: string[]): Promise<string> {
  const res = await apiPost<{ created: unknown[]; skipped: string[] }>('downloads', {
    file_ids: fileIDs,
  })
  const skipped = res.skipped?.length ? `，跳过 ${res.skipped.length} 个同名文件` : ''
  return `已加入下载队列：${res.created.length} 个${skipped}。可在「传输」中查看进度。`
}

// downloadErrorMessage says what to do about a failed download request: a
// missing download folder is a setup step, not an error to read out.
export function downloadErrorMessage(err: unknown): string {
  return err instanceof ApiError && err.code === 'download_dir_unset'
    ? '还没有设置默认下载目录，请到「设置 → 传输」指定一个目录。'
    : (err as Error).message
}
