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
  'state.reauth_account': {
    'zh-CN': '账号 {name} 需要重新授权',
    en: 'Account {name} needs to be authorized again',
  },
  'action.bind': { 'zh-CN': '绑定账号', en: 'Link account' },
  'action.rebind': { 'zh-CN': '重新登录', en: 'Sign in again' },
  'error.details': { 'zh-CN': '详情', en: 'Details' },
  'error.dismiss': { 'zh-CN': '关闭', en: 'Dismiss' },

  // ---- 账号卡片 / Account section ----
  'account.title': { 'zh-CN': 'PikPak 账号', en: 'PikPak account' },
  'account.bound_count': { 'zh-CN': '已绑定 {n} 个', en: '{n} linked' },
  'account.unbound': { 'zh-CN': '未绑定', en: 'Not linked' },
  'account.empty_hint': {
    'zh-CN': '尚未绑定。绑定后本应用才能访问你的 PikPak 文件。',
    en: 'No account linked yet. Only a linked account lets this app reach your PikPak files.',
  },
  'account.add': { 'zh-CN': '添加账号', en: 'Add account' },
  'account.requesting': { 'zh-CN': '申请中…', en: 'Requesting…' },
  'account.rename': { 'zh-CN': '重命名', en: 'Rename' },
  'account.reauthorize': { 'zh-CN': '重新授权', en: 'Re-authorize' },
  'account.unbind': { 'zh-CN': '解绑', en: 'Unlink' },
  'account.unbind_confirm_title': { 'zh-CN': '确认解绑', en: 'Confirm unlink' },
  'account.unbind_warning': {
    'zh-CN':
      '解绑会删除本机保存的该账号 PikPak 凭据，并清除该账号的传输任务记录（其他账号不受影响，已下载的文件保留）。解绑后需要重新授权才能继续使用。',
    en: 'Unlinking deletes this account\'s PikPak credentials stored on this device and clears its transfer records (other accounts are unaffected; downloaded files are kept). Re-authorize after unlinking to use it again.',
  },
  'account.name_label': { 'zh-CN': '账号名称', en: 'Account name' },
  'account.name_empty': { 'zh-CN': '账号名称不能为空', en: 'Account name cannot be empty' },
  'account.name_too_long': {
    'zh-CN': '账号名称过长，最多 50 个字符',
    en: 'Account name is too long; 50 characters at most',
  },
  'account.switch_label': { 'zh-CN': '切换账号', en: 'Switch account' },
  'status.ok': { 'zh-CN': '正常', en: 'OK' },
  'status.reauth': { 'zh-CN': '需重新授权', en: 'Re-auth required' },

  // ---- 设备码流程 / Device-code flow ----
  'flow.open_link': {
    'zh-CN': '在浏览器中打开下面的链接，输入或确认用户码：',
    en: 'Open the link below in a browser and enter or confirm the user code:',
  },
  'flow.copy_link': { 'zh-CN': '复制链接', en: 'Copy link' },
  'flow.copy_failed': {
    'zh-CN': '复制失败，请手动选中链接复制',
    en: 'Copy failed. Select the link and copy it manually.',
  },
  'flow.waiting': { 'zh-CN': '剩余 {time}，等待授权…', en: '{time} left, waiting for authorization…' },
  'flow.retry': { 'zh-CN': '重试', en: 'Retry' },
  'common.cancel': { 'zh-CN': '取消', en: 'Cancel' },

  // ---- 设置页 / Settings page ----
  'settings.privacy_line': {
    'zh-CN': '绑定只属于当前飞牛用户（UID {uid}）。其他飞牛用户看不到你的账号、文件和任务。',
    en: 'Accounts belong to the current fnOS user (UID {uid}) only. Other fnOS users cannot see your accounts, files, or tasks.',
  },
  'settings.title': { 'zh-CN': '参数设置', en: 'Preferences' },
  'settings.dirty': { 'zh-CN': '有未保存的修改', en: 'Unsaved changes' },
  'settings.save': { 'zh-CN': '保存', en: 'Save' },
  'settings.saving': { 'zh-CN': '保存中…', en: 'Saving…' },
  'settings.no_changes': { 'zh-CN': '没有改动', en: 'No changes' },
  'settings.saved': { 'zh-CN': '已保存', en: 'Saved' },
  'settings.concurrency_restart': {
    'zh-CN': '并发数在服务重启后生效',
    en: 'The concurrency change takes effect after the service restarts',
  },
  'settings.conflict': { 'zh-CN': '重名策略', en: 'Conflict policy' },
  'settings.conflict_rename': { 'zh-CN': '重命名（保留两份）', en: 'Rename (keep both)' },
  'settings.conflict_skip': { 'zh-CN': '跳过（保留已有文件）', en: 'Skip (keep existing files)' },
  'settings.conflict_overwrite': { 'zh-CN': '覆盖已有文件', en: 'Overwrite existing files' },
  'settings.current_prefix': { 'zh-CN': '当前：{label}', en: 'Current: {label}' },
  'settings.download_dir': { 'zh-CN': '默认下载目录', en: 'Default download directory' },
  'settings.download_dir_placeholder': {
    'zh-CN': '未设置，下载时会提示先设置',
    en: 'Not set; downloads will prompt for one',
  },
  'settings.pick_dir': { 'zh-CN': '选择目录', en: 'Choose folder' },
  'settings.pick_dir_dialog': { 'zh-CN': '选择默认下载目录', en: 'Choose the default download folder' },
  'settings.ask_dir': { 'zh-CN': '默认下载目录（绝对路径）', en: 'Default download folder (absolute path)' },
  'settings.download_dir_hint': {
    'zh-CN': '「我的文件」里的下载直接保存到该目录；必须是已授权共享文件夹内的目录。留空表示不设置。',
    en: 'Downloads from My Files save directly into this folder; it must sit inside an authorized shared folder. Leave empty to leave it unset.',
  },
  'settings.concurrency': { 'zh-CN': '同时下载数（全局）', en: 'Simultaneous downloads (global)' },
  'settings.concurrency_hint': {
    'zh-CN': '所有用户共享这一上限，修改后需要重启服务才生效。',
    en: 'All users share this limit; it takes effect after the service restarts.',
  },
  'settings.search_key': { 'zh-CN': '全网搜索 API Key（全局）', en: 'Web search API key (global)' },
  'settings.search_key_set': { 'zh-CN': '已设置，输入新值可替换', en: 'Set — enter a new value to replace it' },
  'settings.search_key_unset': { 'zh-CN': '未设置（Serper API Key）', en: 'Not set (Serper API key)' },
  'settings.clear': { 'zh-CN': '清除', en: 'Clear' },
  'settings.search_key_hint_a': {
    'zh-CN':
      '「全网搜索」通过 Serper（Google 结果）搜索 mypikpak.com 站内分享页。Key 只存在本机数据库，读取设置时不会返回，也不会写进日志。可以在 serper.dev 免费申请。搜索结果里的 google.com/goto 跳转链接在列表里直接显示，',
    en: 'Web search uses Serper (Google results) to find shared pages on mypikpak.com. The key is stored only in this device\'s database — never returned when settings are read, and never written to logs. You can apply for one free at serper.dev. google.com/goto redirects in the results are shown as-is in the list, and only ',
  },
  'settings.search_key_hint_strong': { 'zh-CN': '点开时', en: 'when opened' },
  'settings.search_key_hint_b': {
    'zh-CN': '才换回真实地址（优先用搜索服务的 /resolve，不消耗额度），所以搜索本身不会因此变慢。',
    en: ' are they resolved to the real address (preferably via the search service\'s /resolve, which costs no quota), so search itself does not slow down.',
  },
  'settings.search_scope': { 'zh-CN': '全网搜索范围', en: 'Web search scope' },
  'settings.scope_share': { 'zh-CN': '只显示分享页（默认）', en: 'Share pages only (default)' },
  'settings.scope_all': { 'zh-CN': '显示全部结果（分享页带图标）', en: 'Show all results (share pages marked)' },
  'settings.scope_hint': {
    'zh-CN':
      '选「只显示分享页」时，不是分享页的结果直接忽略；选「显示全部结果」时，搜索服务返回的结果都会列出（包括不是 PikPak 站内的页面），分享页后面带分享图标，只有分享页在应用内打开，其它页面交给浏览器。',
    en: 'With "Share pages only", results that are not share pages are ignored; with "Show all results", everything the search service returns is listed (including pages outside PikPak), share pages carry a share icon, and only share pages open inside the app while the rest go to the browser.',
  },
  'settings.play_mode': { 'zh-CN': '播放方式', en: 'Playback' },
  'settings.play_proxy': { 'zh-CN': '代理（推荐，签名地址不外泄）', en: 'Proxy (recommended; signed URLs stay private)' },
  'settings.play_redirect': {
    'zh-CN': '跳转（省带宽，浏览器直连 CDN）',
    en: 'Redirect (saves bandwidth; the browser hits the CDN directly)',
  },
  'settings.player': { 'zh-CN': '播放器', en: 'Player' },
  'settings.player_builtin': { 'zh-CN': '内置播放器（应用内）', en: 'Built-in player (in-app)' },
  'settings.player_iina': { 'zh-CN': 'IINA（仅 macOS，直接使用原始链接）', en: 'IINA (macOS only; uses the raw URL)' },
  'settings.player_hint': {
    'zh-CN': '选 IINA 后，预览和完整播放直接把原始链接交给 IINA，不再每次询问；IINA 只在 macOS 上有效。',
    en: 'With IINA, preview and full playback hand the raw URL to IINA directly without asking each time; IINA only works on macOS.',
  },
  'settings.tile_size': { 'zh-CN': '大图标尺寸', en: 'Tile size' },
  'settings.tile_small': { 'zh-CN': '小（默认）', en: 'Small (default)' },
  'settings.tile_medium': { 'zh-CN': '中（1.5 倍）', en: 'Medium (1.5×)' },
  'settings.tile_large': { 'zh-CN': '大（2 倍）', en: 'Large (2×)' },
  'settings.tile_hint': { 'zh-CN': '作用于所有大图标视图。', en: 'Applies to every tile view.' },
  'settings.language': { 'zh-CN': '界面语言', en: 'Language' },

  // ---- 备份 / Backup ----
  'backup.title': { 'zh-CN': '数据备份与恢复', en: 'Backup & restore' },
  'backup.hint': {
    'zh-CN':
      '备份当前飞牛用户的视频收藏、分享收藏和配置参数，导出为一个 JSON 文件。恢复时收藏按条目合并（已存在的跳过，不覆盖本机内容），配置参数直接覆盖。',
    en: 'Backs up the current fnOS user\'s video favorites, share favorites, and preferences into one JSON file. On restore, favorites merge entry by entry (existing ones are skipped; nothing on this device is overwritten), while preferences are replaced outright.',
  },
  'backup.export': { 'zh-CN': '导出备份', en: 'Export backup' },
  'backup.exporting': { 'zh-CN': '导出中…', en: 'Exporting…' },
  'backup.restore': { 'zh-CN': '选择备份文件', en: 'Choose backup file' },
  'backup.restoring': { 'zh-CN': '恢复中…', en: 'Restoring…' },
  'backup.warning': {
    'zh-CN':
      '备份文件包含收藏（含分享密码）和配置参数，不含账号授权与全网搜索 API Key——恢复后需要重新填写搜索 Key。文件可被复制和传播，请妥善保管。',
    en: 'The backup file contains favorites (including share pass-codes) and preferences — not account authorizations or the web-search API key; the key must be re-entered after a restore. The file can be copied around, so keep it safe.',
  },
  'backup.exported': { 'zh-CN': '已导出 {name}', en: 'Exported {name}' },
  'backup.summary_settings': { 'zh-CN': '设置已覆盖 {n} 项', en: '{n} settings overwritten' },
  'backup.summary_share': {
    'zh-CN': '分享收藏新增 {a}、跳过 {s}',
    en: 'share favorites: {a} added, {s} skipped',
  },
  'backup.summary_video': {
    'zh-CN': '视频收藏新增 {a}、跳过 {s}',
    en: 'video favorites: {a} added, {s} skipped',
  },
  'backup.summary_groups': {
    'zh-CN': '分组新增 {a}、合并 {m}',
    en: 'groups: {a} added, {m} merged',
  },

  // ---- 共享文件夹 / Shared folders ----
  'folders.title': { 'zh-CN': '已授权的共享文件夹', en: 'Authorized shared folders' },
  'folders.count': { 'zh-CN': '{n} 个', en: '{n}' },
  'folders.empty': {
    'zh-CN': 'fnOS 没有返回可访问的共享文件夹，或当前不是从飞牛网关访问。',
    en: 'fnOS returned no accessible shared folders, or this app is not being reached through the fnOS gateway.',
  },
  'folders.hint': {
    'zh-CN': '下载与上传只允许写这些目录及其子目录。',
    en: 'Downloads and uploads may only write these folders and their subfolders.',
  },

  // ---- 关于 / About ----
  'about.title': { 'zh-CN': '关于', en: 'About' },
  'common.refresh': { 'zh-CN': '刷新', en: 'Refresh' },
  'about.version': { 'zh-CN': '版本', en: 'Version' },
  'about.dev': { 'zh-CN': '开发版', en: 'Development build' },
  'about.db_size': { 'zh-CN': '数据库大小', en: 'Database size' },
  'about.task_records': { 'zh-CN': '任务记录', en: 'Task records' },
  'about.download_short': { 'zh-CN': '下载', en: 'downloads' },
  'about.upload_short': { 'zh-CN': '上传', en: 'uploads' },
  'about.fnos_api': { 'zh-CN': '飞牛 Open API', en: 'fnOS Open API' },
  'about.available': { 'zh-CN': '可用', en: 'Available' },
  'about.unavailable_dev': { 'zh-CN': '不可用（开发模式）', en: 'Unavailable (development mode)' },
  'about.recent_errors': { 'zh-CN': '最近错误', en: 'Recent errors' },
  'about.no_failures': { 'zh-CN': '没有失败记录。', en: 'No failure records.' },
  'about.th_kind': { 'zh-CN': '类型', en: 'Kind' },
  'about.th_id': { 'zh-CN': '编号', en: 'ID' },
  'about.th_code': { 'zh-CN': '错误码', en: 'Code' },
  'about.th_desc': { 'zh-CN': '说明', en: 'Detail' },
  'about.kind_upload': { 'zh-CN': '上传', en: 'Upload' },
  'about.kind_download': { 'zh-CN': '下载', en: 'Download' },
}

// errorMessages turns protocol error codes into copy. A code the table does not
// know falls back to the server's own message, which is already human-written.
const errorMessages: Record<string, Entry> = {
  pikpak_reauth_required: {
    'zh-CN': 'PikPak 授权已失效，请重新绑定账号',
    en: 'PikPak authorization expired. Link the account again.',
  },
  no_active_account: {
    'zh-CN': '没有可用的 PikPak 账号，请重新授权或绑定',
    en: 'No usable PikPak account. Authorize or link one again.',
  },
  // The server's message for this code lists each account's own reason; the
  // toast shows this summary and keeps that list under 详情.
  all_accounts_failed: {
    'zh-CN': '所有账号都无法完成该操作',
    en: 'None of the linked accounts could complete this action.',
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
