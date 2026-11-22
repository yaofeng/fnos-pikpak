import { describe, expect, test } from 'vitest'
import { groupVideoFavorites, type VideoFavorite } from './videoFavorites'

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
