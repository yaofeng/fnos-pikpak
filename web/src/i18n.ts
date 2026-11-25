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
  'account.default_label': { 'zh-CN': '默认账号', en: 'Default account' },
  'account.vip_platinum': { 'zh-CN': '白金会员', en: 'Platinum member' },
  'account.vip_gold': { 'zh-CN': '黄金会员', en: 'Gold member' },
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

  // ---- 传输/任务通用 / Transfers & offline tasks ----
  'time.seconds': { 'zh-CN': '{n} 秒', en: '{n}s' },
  'time.min_sec': { 'zh-CN': '{m} 分 {ss} 秒', en: '{m}m {ss}s' },
  'time.hour_min': { 'zh-CN': '{h} 时 {mm} 分', en: '{h}h {mm}m' },
  'status.queued': { 'zh-CN': '排队中', en: 'Queued' },
  'transfer.running': { 'zh-CN': '传输中', en: 'Transferring' },
  'task.running': { 'zh-CN': '下载中', en: 'Downloading' },
  'transfer.paused': { 'zh-CN': '已暂停', en: 'Paused' },
  'status.done': { 'zh-CN': '已完成', en: 'Done' },
  'status.canceled': { 'zh-CN': '已取消', en: 'Canceled' },
  'transfer.skipped': { 'zh-CN': '已跳过（同名）', en: 'Skipped (same name)' },
  'status.failed': { 'zh-CN': '失败：{reason}', en: 'Failed: {reason}' },
  'status.failed_plain': { 'zh-CN': '失败', en: 'Failed' },

  // ---- 下载队列 / Download queue ----
  'download.queued': {
    'zh-CN': '已加入下载队列：{n} 个{skipped}。可在「传输」中查看进度。',
    en: 'Queued {n} downloads{skipped}. Track them under Transfers.',
  },
  'download.queued_skipped': {
    'zh-CN': '，跳过 {n} 个同名文件',
    en: ', {n} same-name file(s) skipped',
  },
  'download.dir_unset': {
    'zh-CN': '还没有设置默认下载目录，请到「设置 → 传输」指定一个目录。',
    en: 'No default download folder is set. Pick one under Settings → Transfers.',
  },

  // ---- 删除 / Delete notices ----
  'files.notice_trashed': { 'zh-CN': '已移入回收站', en: 'Moved to trash' },
  'files.notice_deleted': { 'zh-CN': '已永久删除', en: 'Permanently deleted' },

  // ---- 视频准备 / Video preparation ----
  'video.prepare_failed': { 'zh-CN': '视频准备失败', en: 'Video preparation failed' },
  'video.prepare_timeout': {
    'zh-CN': '视频准备超时，请稍后重试',
    en: 'Video preparation timed out. Try again later.',
  },

  // ---- 分享收藏 / Share favorites ----
  'shares.stats_unavailable': { 'zh-CN': '无法统计这个分享', en: 'This share could not be counted' },
  'shares.stats_pending': { 'zh-CN': '进入分享后自动统计', en: 'Counted automatically after you open the share' },
  'videoFavorites.unknown_share': { 'zh-CN': '未知分享', en: 'Unknown share' },
  'settings.ask_path_invalid': {
    'zh-CN': '请填写绝对路径（以 / 开头）',
    en: 'Enter an absolute path (starting with /)',
  },

  // ---- 通用 / Common ----
  'common.loading': { 'zh-CN': '加载中…', en: 'Loading…' },
  'common.download': { 'zh-CN': '下载', en: 'Download' },
  'common.close': { 'zh-CN': '关闭', en: 'Close' },
  'common.collapse': { 'zh-CN': '收起', en: 'Collapse' },
  'app.transfer_title': { 'zh-CN': '共 {n} 个传输任务', en: '{n} active transfers' },
  'app.transfer_line': { 'zh-CN': '{n} 个任务 ·', en: '{n} tasks ·' },

  // ---- 文件列表 / File list ----
  'files.th_name': { 'zh-CN': '文件名', en: 'Name' },
  'files.th_size': { 'zh-CN': '大小', en: 'Size' },
  'files.th_modified': { 'zh-CN': '修改时间', en: 'Modified' },
  'files.open': { 'zh-CN': '打开', en: 'Open' },
  'statusbar.total': { 'zh-CN': '共 {n} 项', en: '{n} items' },
  'statusbar.selected_of': { 'zh-CN': '选中 {n} 项（共 {total} 项）', en: '{n} of {total} selected' },

  // ---- 播放器 / Player ----
  'player.fullscreen': { 'zh-CN': '全屏', en: 'Fullscreen' },
  'player.loading': { 'zh-CN': '正在获取播放地址…', en: 'Fetching the stream URL…' },
  'player.unsupported': {
    'zh-CN': '浏览器不支持该编码（{mime}），可下载后使用本地播放器观看。',
    en: 'The browser cannot decode this format ({mime}). Download it and watch in a local player.',
  },
  'common.empty': { 'zh-CN': '暂无记录。', en: 'Nothing here yet.' },
  'common.delete': { 'zh-CN': '删除', en: 'Delete' },
  'common.upload': { 'zh-CN': '上传', en: 'Upload' },
  'common.submitted': {
    'zh-CN': '任务已提交，稍后自动刷新',
    en: 'Submitted — the list refreshes on its own',
  },

  // ---- 最近 / Recent ----
  'recent.title': { 'zh-CN': '最近', en: 'Recent' },
  'recent.hint': {
    'zh-CN': '最近添加来自 PikPak 的事件流，包含新增、恢复等操作。',
    en: 'Recent comes from the PikPak event feed: additions, restores, and similar actions.',
  },
  'recent.account_errors': {
    'zh-CN': '部分账号的最近记录没有加载：',
    en: "Some accounts' recent items did not load:",
  },
  'recent.account_error_line': { 'zh-CN': '账号 {name}：{error}', en: 'account {name}: {error}' },

  // ---- 回收站 / Trash ----
  'trash.title': { 'zh-CN': '回收站', en: 'Trash' },
  'trash.confirm_delete': {
    'zh-CN': '永久删除 {n} 项？此操作不可恢复。',
    en: 'Permanently delete {n} items? This cannot be undone.',
  },
  'trash.restore': { 'zh-CN': '恢复', en: 'Restore' },
  'trash.delete_forever': { 'zh-CN': '永久删除', en: 'Delete forever' },
  'trash.empty': { 'zh-CN': '清空回收站', en: 'Empty trash' },
  'trash.empty_state': { 'zh-CN': '回收站是空的。', en: 'The trash is empty.' },
  'trash.restored': { 'zh-CN': '已恢复', en: 'Restored' },
  'trash.confirm_empty_prompt': {
    'zh-CN': '清空回收站会永久删除全部文件，且不可恢复。输入"确认"继续：',
    en: 'Emptying the trash permanently deletes every file and cannot be undone. Type "CONFIRM" to continue:',
  },
  'trash.confirm_word': { 'zh-CN': '确认', en: 'CONFIRM' },
  'trash.empty_submitted': { 'zh-CN': '清空任务已提交', en: 'Emptying submitted' },
  'trash.emptied': { 'zh-CN': '回收站已清空', en: 'The trash is empty now.' },

  // ---- 离线任务 / Offline tasks ----
  'tasks.title': { 'zh-CN': '离线任务', en: 'Offline Tasks' },
  'tasks.created_via': {
    'zh-CN': '已通过账号 {name} 创建离线任务',
    en: 'Offline task created via account {name}',
  },
  'tasks.retry_submitted': { 'zh-CN': '已提交重试', en: 'Retry submitted' },
  'tasks.confirm_delete': {
    'zh-CN': '删除任务「{name}」{extra}？',
    en: 'Delete task "{name}"{extra}?',
  },
  'tasks.confirm_delete_extra': {
    'zh-CN': '，并删除已下载的文件',
    en: ' and its downloaded files',
  },
  'tasks.confirm_clear': {
    'zh-CN': '清理所有已结束的任务记录？已下载的文件会保留。',
    en: 'Clear every finished task record? Downloaded files are kept.',
  },
  'tasks.cleared': { 'zh-CN': '已清理 {n} 个已结束任务', en: 'Cleared {n} finished tasks' },
  'tasks.nothing_to_clear': {
    'zh-CN': '没有需要清理的已结束任务',
    en: 'No finished tasks to clear',
  },
  'tasks.active_count': { 'zh-CN': '{n} 个进行中', en: '{n} in progress' },
  'tasks.clear_finished': { 'zh-CN': '清理已结束', en: 'Clear finished' },
  'tasks.link_placeholder': {
    'zh-CN': '粘贴磁力链接或下载链接',
    en: 'Paste a magnet or download link',
  },
  'tasks.create': { 'zh-CN': '新建任务', en: 'New task' },
  'tasks.delete_files_flag': {
    'zh-CN': '删除任务时同时删除已下载的文件',
    en: 'Also delete downloaded files when removing a task',
  },
  'tasks.account_errors': {
    'zh-CN': '部分账号的任务没有加载：',
    en: "Some accounts' tasks did not load:",
  },
  'tasks.empty': {
    'zh-CN': '还没有离线任务。粘贴链接即可创建。',
    en: 'No offline tasks yet. Paste a link to create one.',
  },
  'tasks.type_offline': { 'zh-CN': '离线下载', en: 'Offline download' },
  'tasks.retry': { 'zh-CN': '重试', en: 'Retry' },
  'tasks.hint': {
    'zh-CN': '任务列表来自 PikPak，页面可见时每 3 秒刷新一次。',
    en: 'Tasks come from PikPak; the list refreshes every 3 s while this page is visible.',
  },

  // ---- 传输 / Transfers ----
  'transfers.confirm_delete_file': {
    'zh-CN': '同时删除已下载的文件「{name}」？\n取消则只移除任务记录。',
    en: 'Also delete the downloaded file "{name}"?\nCancel to remove just the task record.',
  },
  'transfers.hint_downloads': {
    'zh-CN': '在「我的文件」里选中文件后点「下载」创建任务。',
    en: 'Select files in My Files and click Download to create tasks.',
  },
  'transfers.hint_uploads': {
    'zh-CN': '在「我的文件」里点「上传」把飞牛上的文件传到 PikPak。',
    en: 'Click Upload in My Files to send files from this fnOS box to PikPak.',
  },
  'transfers.empty_downloads': { 'zh-CN': '还没有下载任务。', en: 'No download tasks yet.' },
  'transfers.empty_uploads': { 'zh-CN': '还没有上传任务。', en: 'No upload tasks yet.' },
  'transfers.eta_prefix': { 'zh-CN': '剩余', en: 'ETA' },
  'transfers.pause': { 'zh-CN': '暂停', en: 'Pause' },
  'transfers.resume': { 'zh-CN': '继续', en: 'Resume' },
  'transfers.open_folder': { 'zh-CN': '打开所在目录', en: 'Show in folder' },
  'transfers.remove_record': { 'zh-CN': '移除记录', en: 'Remove record' },

  // ---- 全网搜索 / Web search ----
  'search.added_favorite': { 'zh-CN': '已加入收藏夹', en: 'Added to favorites' },
  'search.copy_failed': {
    'zh-CN': '复制地址失败，请打开标签页后手动复制',
    en: 'Copy failed. Open the tab and copy the address manually.',
  },
  'search.tabs_label': { 'zh-CN': '搜索结果标签页', en: 'Search result tabs' },
  'search.results_tab': { 'zh-CN': '搜索结果', en: 'Results' },
  'search.close_tab': { 'zh-CN': '关闭标签页', en: 'Close tab' },
  'search.address_copied': { 'zh-CN': '已复制地址', en: 'Address copied' },
  'search.logo': { 'zh-CN': '搜索', en: 'Search' },
  'search.placeholder': {
    'zh-CN': '搜索 mypikpak.com 上的分享内容',
    en: 'Search shares on mypikpak.com',
  },
  'search.keyword_label': { 'zh-CN': '搜索关键词', en: 'Search keyword' },
  'search.searching': { 'zh-CN': '搜索中…', en: 'Searching…' },
  'search.search': { 'zh-CN': '搜索', en: 'Search' },
  'search.need_key_a': { 'zh-CN': '全网搜索需要先在', en: 'To use web search, fill in the search API key in' },
  'search.need_key_b': { 'zh-CN': '里填写搜索 API Key。', en: 'first.' },
  'search.loaded_count': { 'zh-CN': '已加载 {n} 条结果', en: '{n} results loaded' },
  'search.dropped_hint': {
    'zh-CN':
      '搜索引擎返回 {raw} 条原始结果，其中 {kept} 条是 PikPak 分享页或待解析的分享链接，其余不是分享页，已忽略。',
    en: 'The engine returned {raw} raw results; {kept} are PikPak share pages or links still to resolve. The rest are not share pages and were ignored.',
  },
  'search.all_hint': {
    'zh-CN':
      '搜索引擎返回 {raw} 条结果，其中 {shares} 条是 PikPak 分享页（带分享图标）；其余不是分享页，在浏览器中打开。',
    en: 'The engine returned {raw} results; {shares} are PikPak share pages (marked with an icon). The rest open in the browser.',
  },
  'search.no_results': {
    'zh-CN': '没有找到「{query}」相关的分享页。搜索引擎只收录公开分享，未分享的文件和文件夹搜不到。',
    en: 'No share pages match "{query}". The engine only indexes public shares; unshared files and folders cannot be found.',
  },
  'search.resolving': { 'zh-CN': '正在解析链接…', en: 'Resolving…' },
  'search.share_badge': { 'zh-CN': 'PikPak 分享页', en: 'PikPak share page' },
  'search.loading_more': { 'zh-CN': '正在加载更多…', en: 'Loading more…' },
  'search.no_more': { 'zh-CN': '没有更多结果了', en: 'No more results' },
  'search.copy_address': { 'zh-CN': '复制地址', en: 'Copy address' },
  'search.add_favorite': { 'zh-CN': '加入收藏', en: 'Add to favorites' },

  // ---- 分享收藏夹 / Share favorites ----
  'favorites.title': { 'zh-CN': '分享收藏夹', en: 'Share Favorites' },
  'favorites.added': { 'zh-CN': '已加入分享收藏夹', en: 'Added to share favorites' },
  'favorites.rename_prompt': {
    'zh-CN': '新名称（留空恢复原名）',
    en: 'New name (leave empty to restore the original)',
  },
  'favorites.renamed': { 'zh-CN': '已重命名（仅本机显示）', en: 'Renamed (this device only)' },
  'favorites.name_restored': { 'zh-CN': '已恢复原名', en: 'Original name restored' },
  'favorites.one_label': { 'zh-CN': '「{name}」', en: '"{name}"' },
  'favorites.count_label': { 'zh-CN': '{n} 个分享', en: '{n} shares' },
  'favorites.confirm_remove': {
    'zh-CN': '从分享收藏夹移除{label}？分享本身不会受影响。',
    en: 'Remove {label} from share favorites? The share itself is unaffected.',
  },
  'favorites.removed': { 'zh-CN': '已移除{label}', en: 'Removed {label}' },
  'favorites.filter_placeholder': {
    'zh-CN': '筛选分享名称或创建者',
    en: 'Filter by name or owner',
  },
  'favorites.add': { 'zh-CN': '新增收藏', en: 'Add share' },
  'favorites.go_search': { 'zh-CN': '去全网搜索', en: 'Find shares' },
  'favorites.sort_label': { 'zh-CN': '排序', en: 'Sort' },
  'favorites.sort_default': { 'zh-CN': '默认（收藏时间）', en: 'Default (time saved)' },
  'favorites.col_name': { 'zh-CN': '分享名称', en: 'Name' },
  'favorites.col_owner': { 'zh-CN': '创建者', en: 'Owner' },
  'favorites.col_count': { 'zh-CN': '总文件数', en: 'Files' },
  'favorites.col_size': { 'zh-CN': '总大小', en: 'Size' },
  'favorites.col_shared': { 'zh-CN': '分享时间', en: 'Shared' },
  'favorites.col_expire': { 'zh-CN': '过期时间', en: 'Expires' },
  'favorites.url_placeholder': {
    'zh-CN': '粘贴分享链接，例如 https://mypikpak.com/s/xxxx?pwd=1234',
    en: 'Paste a share link, e.g. https://mypikpak.com/s/xxxx?pwd=1234',
  },
  'favorites.url_label': { 'zh-CN': '分享链接', en: 'Share link' },
  'favorites.empty': { 'zh-CN': '分享收藏夹还是空的。', en: 'Share favorites is empty.' },
  'favorites.empty_hint': {
    'zh-CN': '点「新增收藏」粘贴分享链接，或者在全网搜索里右键标签页选「收藏分享」，之后可以在这里直接进入。',
    en: 'Click "Add share" to paste a link, or right-click a tab under web search and choose "Add to favorites"; saved shares open here directly.',
  },
  'favorites.no_match': { 'zh-CN': '没有匹配的分享。', en: 'No matching shares.' },
  'favorites.protected': { 'zh-CN': '有密码', en: 'Password' },
  'favorites.forever': { 'zh-CN': '永久', en: 'Never expires' },

  // ---- 视频收藏夹 / Video favorites ----
  'vfav.iina_hint': {
    'zh-CN': '已交给 IINA 打开；若没有打开，可能尚未安装 IINA（可在设置里改回内置播放器）',
    en: 'Handed to IINA. If nothing opened, IINA may not be installed (switch back to the built-in player in Settings).',
  },
  'vfav.opening_restored': {
    'zh-CN': '正在打开 Pack From Shared 中已转存的视频…',
    en: 'Opening the copy already in Pack From Shared…',
  },
  'vfav.restoring': {
    'zh-CN': '正在把视频转存到「Pack From Shared」…',
    en: 'Restoring the video into Pack From Shared…',
  },
  'vfav.prepare_no_stream': {
    'zh-CN': '视频准备失败，请稍后重试',
    en: 'Preparation finished without a stream. Try again later.',
  },
  'vfav.restoring_for_download': {
    'zh-CN': '正在转存 {n} 个视频以便下载…',
    en: 'Restoring {n} videos for download…',
  },
  'vfav.group_rename_prompt': {
    'zh-CN': '分组名称（仅本机显示）',
    en: 'Group name (this device only)',
  },
  'vfav.group_renamed': { 'zh-CN': '分组已改为「{label}」', en: 'Group renamed to "{label}"' },
  'vfav.moved_to': {
    'zh-CN': '「{name}」已移到「{target}」',
    en: '"{name}" moved to "{target}"',
  },
  'vfav.count_label': { 'zh-CN': '{n} 个视频', en: '{n} videos' },
  'vfav.confirm_remove': {
    'zh-CN': '从视频收藏夹移除{label}？分享本身不会受影响。',
    en: 'Remove {label} from video favorites? The share itself is unaffected.',
  },
  'vfav.preview': { 'zh-CN': '预览', en: 'Preview' },
  'vfav.fullplay_title': {
    'zh-CN': '转存到「Pack From Shared」后播放，空间不足时自动清理最早的视频',
    en: 'Plays a copy restored into Pack From Shared; the oldest videos are purged when space runs short',
  },
  'vfav.preparing': { 'zh-CN': '准备中…', en: 'Preparing…' },
  'vfav.fullplay': { 'zh-CN': '完整播放', en: 'Full playback' },
  'vfav.download_title': {
    'zh-CN': '先转存到「Pack From Shared」，再加入下载队列',
    en: 'Restore into Pack From Shared first, then queue the download',
  },
  'vfav.preparing_download': { 'zh-CN': '准备下载…', en: 'Preparing download…' },
  'vfav.remove_favorite': { 'zh-CN': '移除收藏', en: 'Remove favorite' },
  'vfav.view_label': { 'zh-CN': '视图', en: 'View' },
  'vfav.view_list': { 'zh-CN': '列表视图', en: 'List view' },
  'vfav.view_grid': { 'zh-CN': '大图标视图', en: 'Tile view' },
  'vfav.empty': { 'zh-CN': '视频收藏夹还是空的。', en: 'Video favorites is empty.' },
  'vfav.empty_hint': {
    'zh-CN': '在分享里选中视频后点「收藏视频」，之后可以在这里直接预览或完整播放。',
    en: 'Select videos in a share and click "Favorite videos"; they open here for preview or full playback.',
  },
  'vfav.group_count': { 'zh-CN': '{n} 个视频', en: '{n} videos' },
  'vfav.group_rename_title': {
    'zh-CN': '重命名这个分组（仅本机显示）',
    en: 'Rename this group (this device only)',
  },
  'vfav.restored_badge': {
    'zh-CN': '已转存到 Pack From Shared',
    en: 'Restored into Pack From Shared',
  },
  'vfav.col_favorited': { 'zh-CN': '收藏时间', en: 'Saved' },

  // ---- 分享浏览 / Share browser ----
  'share.badge_star': { 'zh-CN': '已在视频收藏夹', en: 'In video favorites' },
  'share.fallback_title': { 'zh-CN': '分享', en: 'Share' },
  'share.restored_one': {
    'zh-CN': '「{name}」已转存到账号 {account}',
    en: '"{name}" restored into account {account}',
  },
  'share.restored_many': {
    'zh-CN': '{n} 项已转存到账号 {account}',
    en: '{n} items restored into account {account}',
  },
  'share.unfavorited': { 'zh-CN': '已取消收藏', en: 'Removed from favorites' },
  'share.favorited': { 'zh-CN': '已收藏这个分享', en: 'Share favorited' },
  'share.vfav_one': {
    'zh-CN': '「{name}」已加入视频收藏夹',
    en: '"{name}" added to video favorites',
  },
  'share.vfav_many': {
    'zh-CN': '{n} 个视频已加入视频收藏夹',
    en: '{n} videos added to video favorites',
  },
  'share.need_pass': { 'zh-CN': '该分享需要访问密码', en: 'This share needs a pass code' },
  'share.pass_placeholder': { 'zh-CN': '访问密码', en: 'Pass code' },
  'share.opening': { 'zh-CN': '打开中…', en: 'Opening…' },
  'share.open': { 'zh-CN': '打开', en: 'Open' },
  'share.home_title': { 'zh-CN': '收藏夹主页', en: 'Favorites home' },
  'share.up_title': { 'zh-CN': '上一级', en: 'Up one level' },
  'share.crumbs_label': { 'zh-CN': '分享目录', en: 'Share path' },
  'share.root_title': { 'zh-CN': '分享根目录', en: 'Share root' },
  'share.unfavorite_title': {
    'zh-CN': '取消收藏这个分享',
    en: 'Remove this share from favorites',
  },
  'share.favorite_title': { 'zh-CN': '收藏这个分享', en: 'Favorite this share' },
  'share.filter_placeholder': { 'zh-CN': '在当前目录中筛选', en: 'Filter in this folder' },
  'share.restore': { 'zh-CN': '转存', en: 'Restore' },
  'share.favorite_videos': { 'zh-CN': '收藏视频', en: 'Favorite videos' },
  'share.sort_name': { 'zh-CN': '名称', en: 'Name' },
  'share.sort_time': { 'zh-CN': '时间', en: 'Time' },
  'share.empty': { 'zh-CN': '这个分享是空的。', en: 'This share is empty.' },

  // ---- 我的文件 / Files ----
  'files.folder_name_prompt': { 'zh-CN': '文件夹名称', en: 'Folder name' },
  'files.rename_prompt': { 'zh-CN': '新名称', en: 'New name' },
  'files.moved': { 'zh-CN': '已移动', en: 'Moved' },
  'files.copied': { 'zh-CN': '已复制', en: 'Copied' },
  'files.folder_download_unsupported': {
    'zh-CN': '文件夹暂不支持下载，请逐个选择其中的文件。',
    en: 'Folders cannot be downloaded; select the files inside them one by one.',
  },
  'files.pick_upload_files': { 'zh-CN': '选择要上传的文件', en: 'Choose files to upload' },
  'files.ask_upload_file': {
    'zh-CN': '要上传的文件（绝对路径）',
    en: 'File to upload (absolute path)',
  },
  'files.queued_uploads': {
    'zh-CN': '已加入上传队列：{n} 个。可在「传输」中查看进度。',
    en: 'Queued {n} uploads. Track them under Transfers.',
  },
  'files.move_into_self': {
    'zh-CN': '不能把文件夹移动到它自己里面',
    en: 'A folder cannot be moved into itself',
  },
  'files.upload': { 'zh-CN': '上传', en: 'Upload' },
  'files.new_folder': { 'zh-CN': '新建文件夹', en: 'New folder' },
  'files.more': { 'zh-CN': '更多', en: 'More' },
  'files.filter_label': { 'zh-CN': '筛选', en: 'Filter' },
  'files.kind_all': { 'zh-CN': '全部', en: 'All' },
  'files.kind_folder': { 'zh-CN': '文件夹', en: 'Folders' },
  'files.kind_file': { 'zh-CN': '文件', en: 'Files' },
  'files.empty_folder': { 'zh-CN': '这个文件夹是空的。', en: 'This folder is empty.' },
  'files.no_match': { 'zh-CN': '没有匹配的条目。', en: 'No matching entries.' },
  'files.move_to': { 'zh-CN': '移动到…', en: 'Move to…' },
  'files.copy_to': { 'zh-CN': '复制到…', en: 'Copy to…' },
  'files.play': { 'zh-CN': '播放', en: 'Play' },
  'files.trash_it': { 'zh-CN': '移入回收站', en: 'Move to trash' },
  'files.select_all': { 'zh-CN': '全选', en: 'Select all' },
  'files.clear_selection': { 'zh-CN': '取消选择', en: 'Clear selection' },
  'files.root_dest': { 'zh-CN': '我的文件（根目录）', en: 'My Files (root)' },
  'files.move_hint': {
    'zh-CN': '已选 {n} 项。也可以把它们拖到面包屑上的文件夹。',
    en: '{n} selected. You can also drag them onto a folder in the breadcrumb.',
  },
  'files.remove_ask': { 'zh-CN': '删除 {n} 项？', en: 'Delete {n} items?' },
  'files.remove_hint': {
    'zh-CN': '移入回收站后仍可找回；永久删除会跳过回收站，无法恢复。',
    en: 'Trashed items can still be restored; permanent deletion skips the trash and cannot be undone.',
  },

  // ---- 调试 / Debug ----
  'debug.title': { 'zh-CN': 'PikPak 调试', en: 'PikPak Debug' },
  'debug.m0': { 'zh-CN': 'M0 验证版', en: 'M0 verification build' },
  'debug.health': { 'zh-CN': '服务状态', en: 'Service health' },
  'debug.checking': { 'zh-CN': '检查中…', en: 'Checking…' },
  'debug.dt_uid': { 'zh-CN': '飞牛用户 UID', en: 'fnOS user UID' },
  'debug.gateway': { 'zh-CN': '网关信息', en: 'Gateway info' },
  'debug.forged_test': { 'zh-CN': '伪造身份头测试', en: 'Forge identity header test' },
  'debug.path': { 'zh-CN': '路径', en: 'Path' },
  'debug.query': { 'zh-CN': '查询串', en: 'Query string' },
  'debug.empty': { 'zh-CN': '（空）', en: '(empty)' },
  'debug.identity': { 'zh-CN': '身份', en: 'Identity' },
  'debug.th_header': { 'zh-CN': '请求头', en: 'Header' },
  'debug.th_count': { 'zh-CN': '出现次数', en: 'Count' },
  'debug.forged_count': {
    'zh-CN': 'X-Trim-Userid 出现 {n} 次，',
    en: 'X-Trim-Userid appeared {n} times — ',
  },
  'debug.forged_kept': {
    'zh-CN': '网关保留了客户端伪造的副本（需要依赖重复头拒绝）。',
    en: "the gateway kept the client's forged copy (duplicate-header rejection is required).",
  },
  'debug.forged_stripped': {
    'zh-CN': '网关覆盖或剥离了客户端伪造的头。',
    en: 'the gateway overwrote or stripped the forged header.',
  },
  'debug.range_head': { 'zh-CN': '开头 1 KiB', en: 'First 1 KiB' },
  'debug.range_middle': { 'zh-CN': '中间 1 MiB', en: 'Middle 1 MiB' },
  'debug.range_tail': { 'zh-CN': '尾部 1 KiB', en: 'Last 1 KiB' },
  'debug.range_eof': { 'zh-CN': '超出 EOF', en: 'Beyond EOF' },
  'debug.range_probe': { 'zh-CN': 'Range 探针', en: 'Range probe' },
  'debug.testing': { 'zh-CN': '测试中…', en: 'Testing…' },
  'debug.test_64mib': { 'zh-CN': '测试 64 MiB 文件', en: 'Test with a 64 MiB file' },
  'debug.range_hint': {
    'zh-CN': '判断网关是否原样转发 206/Range、是否缓冲响应。首字节耗时明显接近总耗时说明整段被缓冲。',
    en: 'Checks whether the gateway forwards 206/Range untouched and whether it buffers responses. A first-byte time close to the total means the whole body was buffered.',
  },
  'debug.th_case': { 'zh-CN': '用例', en: 'Case' },
  'debug.th_ttfb': { 'zh-CN': '首字节 ms', en: 'TTFB ms' },
  'debug.th_total_ms': { 'zh-CN': '总耗时 ms', en: 'Total ms' },
  'debug.th_bytes': { 'zh-CN': '字节', en: 'Bytes' },
  'debug.probes': { 'zh-CN': '探针', en: 'Probes' },
  'debug.running': { 'zh-CN': '运行中…', en: 'Running…' },
  'debug.run_all': { 'zh-CN': '运行全部探针', en: 'Run all probes' },
  'debug.export_report': { 'zh-CN': '导出报告', en: 'Export report' },
  'debug.not_run': { 'zh-CN': '尚未运行。', en: 'Not run yet.' },
  'debug.th_name': { 'zh-CN': '名称', en: 'Name' },
  'debug.th_result': { 'zh-CN': '结果', en: 'Result' },
  'debug.th_data': { 'zh-CN': '数据', en: 'Data' },
  'debug.need_desktop_dir': {
    'zh-CN': '请在飞牛桌面内打开本应用后再选择目录。',
    en: 'Open the app inside the fnOS desktop before picking a folder.',
  },
  'debug.acl_probe': { 'zh-CN': '文件授权探针', en: 'File grant probe' },
  'debug.pick_fnos_dir': { 'zh-CN': '选择飞牛目录', en: 'Pick an fnOS folder' },
  'debug.not_in_desktop': {
    'zh-CN': '当前不在飞牛桌面内，目录选择不可用。',
    en: 'Not inside the fnOS desktop; folder picking is unavailable.',
  },
  'debug.picked': { 'zh-CN': '已选择：', en: 'Picked: ' },
  'debug.granted_dirs': {
    'zh-CN': '该用户的授权目录',
    en: "This user's granted folders",
  },
  'debug.th_path': { 'zh-CN': '路径', en: 'Path' },
  'debug.th_readable': { 'zh-CN': '可读', en: 'Read' },
  'debug.th_writable': { 'zh-CN': '可写', en: 'Write' },
  'debug.th_deletable': { 'zh-CN': '可删', en: 'Delete' },
  'debug.th_in_grants': { 'zh-CN': '授权集合内', en: 'In grants' },
  'debug.semantic_paths': { 'zh-CN': '显示路径', en: 'Display paths' },
  'debug.writable_yes': {
    'zh-CN': '可写：是（可以下载到这个目录）',
    en: 'Writable: yes (downloads may target this folder)',
  },
  'debug.writable_no': { 'zh-CN': '可写：否（{reason}）', en: 'Writable: no ({reason})' },
  'debug.need_desktop_file': {
    'zh-CN': '请在飞牛桌面内打开本应用后再选择文件。',
    en: 'Open the app inside the fnOS desktop before picking a file.',
  },
  'debug.upload_probe': { 'zh-CN': '上传探针', en: 'Upload probe' },
  'debug.bind_first': {
    'zh-CN': '先绑定 PikPak 账号再上传。',
    en: 'Bind a PikPak account before uploading.',
  },
  'debug.parent_id_label': {
    'zh-CN': '目标文件夹 id（留空为根目录）',
    en: 'Target folder id (empty = root)',
  },
  'debug.uploading': { 'zh-CN': '上传中…', en: 'Uploading…' },
  'debug.upload_generated': {
    'zh-CN': '上传生成文件（{mib} MiB）',
    en: 'Upload the generated file ({mib} MiB)',
  },
  'debug.upload_fnos_file': { 'zh-CN': '上传飞牛文件', en: 'Upload an fnOS file' },
  'debug.dt_file': { 'zh-CN': '文件', en: 'File' },
  'debug.byte_count': { 'zh-CN': '{n} 字节', en: '{n} bytes' },
  'debug.dt_gcid_ms': { 'zh-CN': 'GCID 耗时', en: 'GCID time' },
  'debug.dt_instant': { 'zh-CN': '秒传', en: 'Instant upload' },
  'debug.instant_yes': { 'zh-CN': '是（无需上传）', en: 'Yes (nothing to upload)' },
  'debug.instant_no': { 'zh-CN': '否', en: 'No' },
  'debug.dt_param_keys': { 'zh-CN': 'params 键', en: 'params keys' },
  'debug.dt_upload_ms': { 'zh-CN': '上传耗时', en: 'Upload time' },
  'debug.dt_wait_ms': { 'zh-CN': '等待完成', en: 'Wait for completion' },
  'debug.dt_final': { 'zh-CN': '最终状态', en: 'Final state' },
  'debug.id_paren': { 'zh-CN': '（{id}）', en: ' ({id})' },
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
