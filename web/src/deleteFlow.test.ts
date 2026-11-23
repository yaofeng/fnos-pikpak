import { describe, expect, test } from 'vitest'
import { deletePlan } from './deleteFlow'

describe('deletePlan', () => {
  test('移入回收站 goes to the trash route', () => {
    expect(deletePlan('trash')).toEqual({ path: 'files/trash', notice: '已移入回收站' })
  })

  test('永久删除 purges through the delete route', () => {
    expect(deletePlan('delete')).toEqual({ path: 'files/delete', notice: '已永久删除' })
  })
})
