import { describe, expect, it } from 'vitest'
import { activePath, nextVisited, pageKey, restoreScroll, saveScroll } from './keepAlive'

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
describe('scrollStore', () => {
  it('存取同一页的偏移', () => {
    const store = new Map<string, number>()
    saveScroll(store, '/tasks', 800)
    expect(restoreScroll(store, '/tasks')).toBe(800)
  })
  it('未知页回 0，重复保存取最新', () => {
    const store = new Map<string, number>()
    expect(restoreScroll(store, '/trash')).toBe(0)
    saveScroll(store, '/trash', 120)
    saveScroll(store, '/trash', 640)
    expect(restoreScroll(store, '/trash')).toBe(640)
  })
  it('各 store 实例互不串扰', () => {
    const a = new Map<string, number>()
    const b = new Map<string, number>()
    saveScroll(a, '/recent', 300)
    expect(restoreScroll(b, '/recent')).toBe(0)
    expect(restoreScroll(a, '/settings')).toBe(0)
  })
})
