// Deleting something from 我的文件 asks which delete the user means: a copy
// that waits in the trash, or a permanent purge that skips it. The two are
// different upstream routes, and picking the wrong one either leaves the file
// behind or destroys it — so the mapping lives here, with a test.
export type DeleteMode = 'trash' | 'delete'

const plans: Record<DeleteMode, { path: string; notice: string }> = {
  trash: { path: 'files/trash', notice: '已移入回收站' },
  delete: { path: 'files/delete', notice: '已永久删除' },
}

export function deletePlan(mode: DeleteMode): { path: string; notice: string } {
  return plans[mode]
}
