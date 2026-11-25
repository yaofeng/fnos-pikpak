import { describe, expect, it } from 'vitest'
import { activePath, nextVisited, pageKey } from './keepAlive'

describe('activePath', () => {
  it('/files 与子路径归 /files', () => {
    expect(activePath('/files')).toBe('/files')
    expect(activePath('/files/folder-1')).toBe('/files')
  })
  it('整路径相等归该页', () => {
    expect(activePath('/search')).toBe('/search')
    expect(activePath('/video-favorites')).toBe('/video-favorites')
  })
  it('未知路径与 / 返回 null', () => {
    expect(activePath('/')).toBeNull()
    expect(activePath('/unknown')).toBeNull()
  })
})
describe('nextVisited', () => {
  it('激活页加入且原集不被突变', () => {
    const prev = new Set(['/files'])
    const next = nextVisited(prev, '/search')
    expect(next.has('/search')).toBe(true)
    expect(prev.has('/search')).toBe(false)
  })
  it('已 visited 不新建', () => {
    const prev = new Set(['/files'])
    expect(nextVisited(prev, '/files')).toBe(prev)
  })
})
describe('pageKey', () => {
  it('换账号换 key，同参幂等', () => {
    expect(pageKey('/files', 'a')).not.toBe(pageKey('/files', 'b'))
    expect(pageKey('/files', 'a')).toBe(pageKey('/files', 'a'))
  })
})
