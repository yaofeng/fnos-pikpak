// Deleting something from 我的文件 asks which delete the user means: a copy
// that waits in the trash, or a permanent purge that skips it. The two are
// different upstream routes, and picking the wrong one either leaves the file
// behind or destroys it — so the mapping lives here, with a test. The notice
// copy comes from the i18n table; the process-wide language applies unless
// the caller passes one.
import { getLang, makeT, type Lang } from './i18n'

export type DeleteMode = 'trash' | 'delete'

const paths: Record<DeleteMode, { path: string; noticeKey: string }> = {
  trash: { path: 'files/trash', noticeKey: 'files.notice_trashed' },
  delete: { path: 'files/delete', noticeKey: 'files.notice_deleted' },
}

export function deletePlan(mode: DeleteMode, lang?: Lang): { path: string; notice: string } {
  const plan = paths[mode]
  return { path: plan.path, notice: makeT(lang ?? getLang())(plan.noticeKey) }
}
