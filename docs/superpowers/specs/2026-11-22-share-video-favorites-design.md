# 视频收藏夹与 IINA 播放 设计

日期：2026-11-22
状态：待确认

## 目标

让"在分享里找到的视频"可以像本地影库一样处理：

- 把选中的视频收进**视频收藏夹**，按分享者分组。
- 用 **IINA** 预览，或"完整播放"——先把视频转存到网盘的 `我的视频/临时`，空间不够时自动永久清理，再用 IINA 播放。
- 分享级收藏的按钮改名为"收藏分享"，动作语义更清楚。
- 网格视图显示 PikPak 缩略图（直连 CDN）。

## 非目标

- 不做转码、字幕、剧集刮削。
- 不把分享视频接入「传输」队列（队列是"下载到 NAS"的语义）。
- 不做后台自动清理：删除只发生在用户点击「完整播放」之后。
- 不保证 Windows/手机上的 IINA（不存在），这些平台回退到应用内播放器。

## 反目标（即使能工作也算失败）

- 删除 `我的视频/临时` 和 `Pack From Shared` 之外的任何文件。
- 在没有用户动作时删除任何东西。
- 把分享密码、通行 token 或 CDN 签名地址写进日志、收藏列表响应或前端持久化。

## 已确认的约束

1. IINA 只存在于 macOS。网页只能通过 `iina://weblink?url=<签名地址>` 唤起，且唤起需要一次用户点击手势；转存是异步的，完成时手势已消失。
2. 永久删除用 `POST /drive/v1/files:batchDelete`，需要 `files:purge`，只有 OAuth 绑定具备（当前绑定即 OAuth）。删除是异步任务，要等任务结束后配额才会更新。
3. 分享接口的成功响应里不保证有分享者字段（文档 §8 标 ◐）。拿不到就按分享标题分组。
4. 分享转存接口只保证 `share_id`、`pass_code_token`、`file_ids`；落点默认是网盘根目录。是否接受 `parent_id` 未验证。因此流程按"转存 → 在根目录定位 → 移动到 `我的视频/临时`"设计；若接口接受 `parent_id`，实现时优先使用并跳过移动。
5. 应用可能被 Windows/手机浏览器打开，那里没有 IINA。

## 方案

### A. 后端编排 + 任务轮询（采用）

`POST /api/videos/prepare` 只登记任务并立即返回 `job_id`；worker 在后台：

1. 解析来源：分享会话（分享浏览页）或视频收藏（收藏夹页，服务端解密保存的密码重新打开分享）。
2. 取文件大小，读 `/drive/v1/about` 的 `quota.limit`/`quota.usage`。
3. 若 `我的视频/临时` 已有同名同大小的文件，直接进入第 8 步。
4. 空间足够（`limit - usage >= size`）→ 转存；不够 → 永久删除 `临时` 中最旧的文件 → 重读配额 → 仍不够 → 永久删除根目录 `Pack From Shared` 中最旧的文件 → 重读配额。
5. 两边都清空仍不够 → 任务失败："空间不足"。
6. 等转存任务完成，在根目录按"转存前快照 + 名称/大小"定位新文件，移动到 `我的视频/临时`（目录不存在则创建）。
7. 等移动任务完成。
8. 生成网盘文件的签名播放地址，任务状态置为 `ready`，返回文件 id、名称、大小、`stream_url`。

`GET /api/videos/jobs/{id}` 返回 `{state: running|ready|failed, stage, error, stream_url}`；任务只存在内存里（按 uid 隔离，TTL 30 分钟）。

前端轮询 1 秒一次，按钮显示"准备中…（转存 / 清理空间）"；`ready` 后 macOS 显示「用 IINA 播放」，非 macOS 直接用内置播放器打开。

**为什么选它**：永久删除与空间判断留在服务端，可测试、可审计；刷新页面不中断；失败点集中在一个 worker 里。缺点是多了任务轮询的代码，且一次准备可能跑几分钟。

### 备选 B：前端分步调接口

复用现有的转存、移动、删除接口，由页面串流程。改动小，但删除逻辑散在前端、刷新即断、失败恢复困难。不采用。

### 备选 C：并入传输队列

复用 worker 与进度展示，但队列的状态机面向 NAS 落盘，和"网盘内转存 + 清理"不同，改动最大。不采用。

## 页面与交互

### 分享浏览工具栏

| 现在 | 改为 | 行为 |
| --- | --- | --- |
| 打开 | 预览 | 单个选中视频 → 直接 IINA（非 macOS 用内置播放器）；文件夹仍可双击进入 |
| 下载 | 完整播放 | 单个选中视频 → 走方案 A，就绪后播放 |
| 加入收藏 | 收藏分享 | 整条分享进分享收藏夹（含密码，服务端密封） |
| — | 收藏视频 | 选中的视频进视频收藏夹 |

`转存` 保留，仍是转存到根目录。

### 视频收藏夹页

侧栏顺序：分享收藏夹 → **视频收藏夹** → 全网搜索。

```
视频收藏夹
[预览] [完整播放] [移除收藏]            [刷新]
▸ 分享者 A（分享标题）
    🎬 视频 1   2.0 GiB
    🎬 视频 2   800 MiB
▸ 分享者 B（分享标题）
    🎬 视频 3   1.2 GiB
```

- 按 `分享者` 分组；没有分享者时用 `分享标题`；两者都空用"未知分享"。
- 选择规则、工具栏动作与分享浏览一致。
- 双击视频 = 预览。

### 缩略图（后端代理，与播放一致）

- 新增 `GET /api/thumb?file_id=<id>` 和 `GET /api/thumb?share_id=<id>&file_id=<id>`，服务端取 `thumbnail_link` 后把图片字节转发给浏览器；前端只认本站地址，CDN 签名地址不进入页面。
- `File` 增加 `thumbnail_link`（`json:"-"`），仅服务端使用；`ListFiles` 补 `thumbnail_size=SIZE_LARGE`，分享接口已经传了。
- 代理按 `uid + 来源 + file_id` 在内存里缓存图片字节（TTL 30 分钟，单图上限 5 MiB），未命中时用列表/分享接口返回的签名地址取图；签名过期则重新取一次列表再试。
- 响应 `Content-Type` 来自上游，`Cache-Control: private, max-age=600`。失败返回 404，前端回退现有文件图标。
- 前端 `FileItem` 增加 `thumb_url`（由 `file_id` 拼出，不需要列表响应携带）；网格视图 `<img loading="lazy" onError=回退图标>`，列表视图不变。

## 数据

```sql
CREATE TABLE video_favorites (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  uid INTEGER NOT NULL,
  share_id TEXT NOT NULL,
  file_id TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL DEFAULT 0,
  owner TEXT NOT NULL DEFAULT '',
  share_title TEXT NOT NULL DEFAULT '',
  pass_code_enc BLOB NOT NULL DEFAULT x'',
  created_at TEXT NOT NULL,
  UNIQUE(uid, share_id, file_id)
);
CREATE INDEX idx_video_favorites_uid ON video_favorites(uid, created_at DESC);
```

密码与分享收藏夹相同：AAD 绑定 `uid + field + share_id`，列表接口不返回密码。

接口：

- `GET /api/video-favorites` → 按 share_title/owner 分组由前端完成
- `POST /api/video-favorites` `{share_id, file_id, name, size, owner, share_title, pass_code}`
- `DELETE /api/video-favorites/{id}`
- `POST /api/videos/prepare` `{share_id, file_id}` 或 `{video_favorite_id}`
- `GET /api/videos/jobs/{id}`

## 自动清理规则（用户已确认的语义）

1. 只删文件，不删子目录。
2. `临时` 按修改时间从旧到新删；`Pack From Shared` 同样从旧到新，删够即停。
3. 两边都清完仍不够 → 失败，不转存。
4. 同一文件已在 `临时` → 直接播放，不重复转存。
5. 全程没有二次确认，因为删除发生在用户点击「完整播放」之后。

## 播放器选择

- `navigator.userAgent` 含 `Mac` → 显示/使用 IINA：`iina://weblink?url=<绝对签名地址>`。
- 其他平台 → 应用内 `Player`，分享视频走 `/api/share/file`，转存后的视频走 `/api/play/<file_id>`。
- IINA 未安装时浏览器不会有反馈；页面在唤起后提示"若 IINA 未打开，可能尚未安装"，并提供应用内播放作为备选。

## 留给实现的决定

- `share/restore` 是否接受 `parent_id` 的探测方式与回退。
- worker 并发：同一用户同一时间只允许一个"完整播放"准备任务。
- 轮询失败（任务过期/服务重启）时的文案与重试入口。
- 缩略图尺寸（`SIZE_MEDIUM` vs `SIZE_LARGE`）按实际流量再调。
