export type Lang = 'zh-CN' | 'en'

type Entry = { 'zh-CN': string; en: string }

const messages: Record<string, Entry> = {
  'nav.files': { 'zh-CN': '我的文件', en: 'My Files' },
  'nav.recent': { 'zh-CN': '最近', en: 'Recent' },
  'nav.tasks': { 'zh-CN': '离线任务', en: 'Offline Tasks' },
  'nav.transfers': { 'zh-CN': '传输', en: 'Transfers' },
  'nav.trash': { 'zh-CN': '回收站', en: 'Trash' },
  'nav.settings': { 'zh-CN': '设置', en: 'Settings' },
  'nav.bind': { 'zh-CN': '账号', en: 'Account' },
  'nav.debug': { 'zh-CN': '调试', en: 'Debug' },
  'state.loading': { 'zh-CN': '加载中…', en: 'Loading…' },
  'state.unbound': { 'zh-CN': '尚未绑定 PikPak 账号', en: 'PikPak account not linked' },
  'action.bind': { 'zh-CN': '绑定账号', en: 'Link account' },
  'action.upload': { 'zh-CN': '上传', en: 'Upload' },
  'action.newFolder': { 'zh-CN': '新建文件夹', en: 'New folder' },
  'quota.used': { 'zh-CN': '已用', en: 'Used' },
}

export type T = (key: keyof typeof messages | string) => string

export function makeT(lang: Lang): T {
  return (key: string) => messages[key]?.[lang] ?? key
}

// detectLang prefers the language fnOS reports, then the browser, then zh-CN.
export function detectLang(): Lang {
  const tag = document.documentElement.lang || navigator.language
  return tag.toLowerCase().startsWith('en') ? 'en' : 'zh-CN'
}
