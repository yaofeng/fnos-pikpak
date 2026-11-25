import { apiPost, ApiError } from './api'
import { getLang, makeT, type Lang } from './i18n'

// DownloadItem is one queued file: which drive file, and under which bound
// account. An empty account_id falls back to the request's ?account= (and
// then to the default account), which is what share and favorites callers send.
export type DownloadItem = { file_id: string; account_id: string }

// queueDownloads hands the drive file ids of restored copies to the download
// queue and returns the notice the page shows: how many were added, and how
// many same-name files the configured conflict policy skipped. Copy follows
// the process-wide language unless the caller passes one.
export async function queueDownloads(items: DownloadItem[], lang?: Lang): Promise<string> {
  const res = await apiPost<{ created: unknown[]; skipped: string[] }>('downloads', { items })
  const t = makeT(lang ?? getLang())
  const skipped = res.skipped?.length
    ? t('download.queued_skipped').replace('{n}', String(res.skipped.length))
    : ''
  return (
    t('download.queued').replace('{n}', String(res.created.length)).replace('{skipped}', skipped)
  )
}

// downloadErrorMessage says what to do about a failed download request: a
// missing download folder is a setup step, not an error to read out.
export function downloadErrorMessage(err: unknown, lang?: Lang): string {
  const t = makeT(lang ?? getLang())
  return err instanceof ApiError && err.code === 'download_dir_unset'
    ? t('download.dir_unset')
    : (err as Error).message
}
