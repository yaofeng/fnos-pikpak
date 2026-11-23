export type Lang = 'zh-CN' | 'en'

type Entry = { 'zh-CN': string; en: string }

const messages: Record<string, Entry> = {
  'nav.files': { 'zh-CN': '我的文件', en: 'My Files' },
  'nav.recent': { 'zh-CN': '最近', en: 'Recent' },
  'nav.tasks': { 'zh-CN': '离线任务', en: 'Offline Tasks' },
  'nav.transfers': { 'zh-CN': '传输', en: 'Transfers' },
  'nav.trash': { 'zh-CN': '回收站', en: 'Trash' },
  'nav.settings': { 'zh-CN': '设置', en: 'Settings' },
  'nav.search': { 'zh-CN': '全网搜索', en: 'Web search' },
  'nav.favorites': { 'zh-CN': '分享收藏夹', en: 'Share favorites' },
  'nav.videoFavorites': { 'zh-CN': '视频收藏夹', en: 'Video favorites' },
  'nav.debug': { 'zh-CN': '调试', en: 'Debug' },
  'state.unbound': { 'zh-CN': '尚未绑定 PikPak 账号', en: 'PikPak account not linked' },
  'state.reauth': {
    'zh-CN': 'PikPak 授权已失效，请重新绑定账号后继续。',
    en: 'Your PikPak authorization expired. Link the account again to continue.',
  },
  'action.bind': { 'zh-CN': '绑定账号', en: 'Link account' },
  'action.rebind': { 'zh-CN': '重新登录', en: 'Sign in again' },
  'error.details': { 'zh-CN': '详情', en: 'Details' },
  'error.dismiss': { 'zh-CN': '关闭', en: 'Dismiss' },
}

// errorMessages turns protocol error codes into copy. A code the table does not
// know falls back to the server's own message, which is already human-written.
const errorMessages: Record<string, Entry> = {
  pikpak_reauth_required: {
    'zh-CN': 'PikPak 授权已失效，请重新绑定账号',
    en: 'PikPak authorization expired. Link the account again.',
  },
  not_bound: { 'zh-CN': '尚未绑定 PikPak 账号', en: 'No PikPak account linked' },
  daily_limit_reached: {
    'zh-CN': '离线任务每日额度已用完，请明天再试',
    en: 'The daily offline-task quota is used up. Try again tomorrow.',
  },
  forbidden: { 'zh-CN': '没有权限执行该操作', en: 'Not allowed' },
  not_found: { 'zh-CN': '目标不存在或已被删除', en: 'Not found, or already deleted' },
  fnos_unavailable: { 'zh-CN': '飞牛文件服务不可用', en: 'fnOS file service unavailable' },
  pikpak_error: { 'zh-CN': 'PikPak 接口返回错误', en: 'PikPak returned an error' },
  fnos_error: { 'zh-CN': '飞牛接口返回错误', en: 'fnOS returned an error' },
  internal: { 'zh-CN': '服务内部错误，请稍后重试', en: 'Internal error. Try again later.' },
  unsupported_media_type: { 'zh-CN': '请求格式不受支持', en: 'Unsupported request format' },
  bad_json: { 'zh-CN': '请求内容不是合法的 JSON', en: 'Request body is not valid JSON' },
  invalid_setting: { 'zh-CN': '设置项的值不合法', en: 'Invalid setting value' },
  unknown_setting: { 'zh-CN': '未知的设置项', en: 'Unknown setting' },
  not_user_setting: { 'zh-CN': '该设置项属于全局设置', en: 'This setting is app-wide' },
  empty_query: { 'zh-CN': '请输入搜索关键词', en: 'Enter a keyword' },
  search_key_missing: {
    'zh-CN': '全网搜索需要先在参数设置里填写搜索 API Key',
    en: 'Web search needs a search API key in Settings',
  },
  search_key_rejected: {
    'zh-CN': '搜索 API Key 无效，请在参数设置里检查',
    en: 'The search API key was rejected. Check it in Settings.',
  },
  search_failed: {
    'zh-CN': '搜索服务暂时不可用，请稍后重试',
    en: 'The search service is unavailable. Try again later.',
  },
}

// setLang/getLang keep one process-wide language so modules outside the React
// tree (api.ts turns error codes into copy) agree with the shell.
let current: Lang = 'zh-CN'

export function setLang(lang: Lang) {
  current = lang
}

export function getLang(): Lang {
  return current
}

export function errorText(code: string, lang: Lang = current): string | undefined {
  return errorMessages[code]?.[lang]
}

type T = (key: keyof typeof messages | string) => string

export function makeT(lang: Lang): T {
  return (key: string) => messages[key]?.[lang] ?? key
}

// detectLang prefers the language fnOS reports, then the browser, then zh-CN.
export function detectLang(): Lang {
  const tag = document.documentElement.lang || navigator.language
  return tag.toLowerCase().startsWith('en') ? 'en' : 'zh-CN'
}
