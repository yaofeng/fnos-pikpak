import { describe, expect, test } from 'vitest'
import { groupKeyOf, groupVideoFavorites, selectedFileIDs, type VideoFavorite } from './videoFavorites'

function fav(id: number, extra: Partial<VideoFavorite> = {}): VideoFavorite {
  return {
    id,
    share_id: 'VOabc123',
    file_id: 'sf' + id,
    name: 'v' + id + '.mp4',
    size: 1024,
    owner: '',
    share_title: '',
    display_name: '',
    group_label: '',
    created_at: '2026-11-20T10:00:00Z',
    ...extra,
  }
}

describe('groupVideoFavorites', () => {
  test('groups by owner, then share title, then unknown', () => {
    const groups = groupVideoFavorites([
      fav(1, { owner: 'alice' }),
      fav(2, { owner: '', share_title: '资料' }),
      fav(3),
    ])
    expect(groups.map((g) => g.label)).toEqual(['alice', '资料', '未知分享'])
  })

  test('keeps newest first inside a group', () => {
    const groups = groupVideoFavorites([
      fav(1, { owner: 'alice', created_at: '2026-11-01T00:00:00Z' }),
      fav(2, { owner: 'alice', created_at: '2026-11-20T00:00:00Z' }),
    ])
    expect(groups).toHaveLength(1)
    expect(groups[0].items.map((f) => f.id)).toEqual([2, 1])
  })

  test('keeps the owner groups in the order they first appear', () => {
    const groups = groupVideoFavorites([
      fav(1, { owner: 'bob' }),
      fav(2, { owner: 'alice' }),
      fav(3, { owner: 'bob' }),
    ])
    expect(groups.map((g) => g.label)).toEqual(['bob', 'alice'])
    expect(groups[0].items).toHaveLength(2)
  })
})

describe('selectedFileIDs', () => {
  test('keys the grid selection by file id, as FileGrid looks it up', () => {
    const picked = [fav(1, { file_id: 'sf1' }), fav(2, { file_id: 'sf2' })]
    expect([...selectedFileIDs(picked)]).toEqual(['sf1', 'sf2'])
  })

  test('is empty when nothing is picked', () => {
    expect(selectedFileIDs([]).size).toBe(0)
  })
})

describe('local groups', () => {
  test('an explicit group label wins over the sharing user', () => {
    const groups = groupVideoFavorites([
      fav(1, { owner: 'alice', group_label: '我的功夫片' }),
      fav(2, { owner: 'alice' }),
    ])
    expect(groups.map((g) => g.label)).toEqual(['我的功夫片', 'alice'])
  })

  test('groupKeyOf names the group by the name it shows', () => {
    expect(groupKeyOf(fav(1, { group_label: '甲' }))).toBe('label:甲')
    expect(groupKeyOf(fav(1, { group_label: '甲', owner: 'bob' }))).toBe('label:甲')
    expect(groupKeyOf(fav(1, { owner: 'alice' }))).toBe('label:alice')
    expect(groupKeyOf(fav(1, { share_title: '资料' }))).toBe('label:资料')
    expect(groupKeyOf(fav(1))).toBe('label:未知分享')
  })

  // A group is its displayed name: a video shared by bob and a video filed
  // under the local label "bob" are one group.
  test('the same group name is one group, whatever its source', () => {
    const groups = groupVideoFavorites([
      fav(1, { owner: 'bob', created_at: '2026-11-01T00:00:00Z' }),
      fav(2, { owner: 'alice', group_label: 'bob', created_at: '2026-11-10T00:00:00Z' }),
      fav(3, { share_title: 'bob', created_at: '2026-11-20T00:00:00Z' }),
    ])
    expect(groups).toHaveLength(1)
    expect(groups[0].label).toBe('bob')
    expect(groups[0].items.map((f) => f.id)).toEqual([3, 2, 1])
  })
})
