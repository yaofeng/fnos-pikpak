# PikPak API 参考（用于 fnOS 原生应用）

> 状态：初稿，2026-11-21  
> 目的：作为飞牛 PikPak 应用的接口依据。PikPak 没有公开的 REST API 文档，本文由官方 CLI 行为、官方帮助页和多个社区实现交叉整理。  
> 图例：✅ 已实测 / 官方明确 ｜ ◐ 多个独立实现一致 ｜ △ 单一来源 ｜ ❓ 待验证

---

## 1. 主机与基础约定

| 用途 | 主机 |
|---|---|
| 账号、OAuth、验证码 | `https://user.mypikpak.com` |
| 云盘、任务、分享 | `https://api-drive.mypikpak.com` |

社区实现中也出现过 `user.mypikpak.net`、`api-drive.mypikpak.net` 镜像，行为一致。应用建议使用 `.com`，并把主机做成可配置项。

### 1.1 常用请求头

| Header | 说明 |
|---|---|
| `Authorization: Bearer <access_token>` | 已登录的云盘请求 |
| `X-Device-Id: <32位十六进制>` | 设备标识，同一账号每台设备/每个飞牛用户应稳定且不同 |
| `X-Client-Id: <client_id>` | OAuth 客户端标识；官方 CLI 使用 `aal7sToC85zMXEpJ` ✅ |
| `X-Client-Version: 1.0.0` | 官方 CLI 发送的版本 ✅ |
| `Content-Type: application/json` | JSON 请求 |
| `User-Agent` | 官方 CLI 使用 Chrome UA；也可用 `PikPak-SDK-V1.0.0`（△） |
| `X-Captcha-Token` | 部分写操作/取链接要求验证码令牌，见 §3.5 |
| `Accept-Language` | 影响错误描述语言 |

### 1.2 响应与错误

成功响应直接是业务 JSON。错误响应通常是：

```json
{
  "error": "file_name_empty",
  "error_code": 2,
  "error_description": "File name is empty",
  "error_url": ""
}
```

部分错误带 `error_details` 或 `details` 数组。注意 **HTTP 状态码可能仍是 200**，必须以 `error`/`error_code` 判断。

### 1.3 分页

列表接口统一使用：

- `limit`：每页数量，建议 ≤ 500（rclone 使用 500，官方 CLI 默认 100）
- `page_token`：首页传空或不传，响应里的 `next_page_token` 作为下一页参数
- 当 `next_page_token` 为空或重复时结束

### 1.4 字段类型

- `size`、`quota.limit`、`quota.usage` 等大整数常以 **字符串** 返回（如 `"10995116277760"`）。
- 时间字段是 RFC3339 字符串，例如 `"2022-09-17T14:31:06.056+08:00"`。
- `kind`：`drive#file`、`drive#folder`、`drive#fileList`、`drive#task`、`drive#about` 等。

---

## 2. 认证

### 2.1 推荐：OAuth 设备码流程（已实测发起）

适用于 NAS、无浏览器后台服务。每个飞牛用户各自授权、各自保存 refresh token，互不可见。

#### 步骤 1：申请设备码 ✅

```http
POST https://user.mypikpak.com/v1/auth/device/code
Content-Type: application/json
X-Client-Id: aal7sToC85zMXEpJ
X-Client-Version: 1.0.0
X-Device-Id: 6cabc5868d6d2acd9b7736543c794d24
Accept: application/json
Accept-Language: en-US
Referer: https://user.mypikpak.com/v1
```

```json
{
  "client_id": "aal7sToC85zMXEpJ",
  "scope": "files:read files:write files:manage files:purge cloud_download share account"
}
```

响应：

```json
{
  "device_code": "<device_code>",
  "user_code": "<user_code>",
  "expires_in": 300,
  "interval": 2,
  "verification_url": "https://mypikpak.com/drive/activate?client_id=...&scope=...",
  "verification_uri_complete": "https://mypikpak.com/drive/activate?client_id=...&scope=...&user_code=..."
}
```

- `scope` 在请求体里用**空格分隔**。
- `expires_in` 为 300 秒，`interval` 为轮询间隔秒数。
- 把 `verification_uri_complete` 展示给用户，或展示 `verification_url` + `user_code`。
- 官方 CLI 的 `client_id` 为 `aal7sToC85zMXEpJ`；这是公开客户端标识，但属于官方 CLI，长期使用需关注官方变更。

#### 步骤 2：轮询换 token ✅

RFC 8628 标准设备码授权：

```http
POST https://user.mypikpak.com/v1/auth/token
Content-Type: application/json
X-Client-Id: aal7sToC85zMXEpJ
X-Client-Version: 1.0.0
X-Device-Id: <same device id>
```

```json
{
  "client_id": "aal7sToC85zMXEpJ",
  "grant_type": "urn:ietf:params:oauth:grant-type:device_code",
  "device_code": "<device_code>"
}
```

成功响应（已通过官方 CLI 完成授权；刷新流程已直连验证）：

```json
{
  "token_type": "Bearer",
  "access_token": "...",
  "refresh_token": "...",
  "expires_in": 43200,
  "sub": "<user_id>"
}
```

- `expires_in` 实测为 43200 秒（12 小时）。
- 授权响应里的 `scopes` 数组可能只列出“额外授权”的权限（本次为 `files:purge`、`cloud_download`、`share`、`account`），默认读写权限未重复列出；以实际接口调用是否成功为准。

轮询期间可能返回：

| error | 含义 | 处理 |
|---|---|---|
| `authorization_pending` | 用户尚未确认 | 按 `interval` 继续 |
| `slow_down` | 轮询过快 | 增大间隔 |
| `access_denied` | 用户拒绝 | 终止 |
| `expired_token` | 设备码过期 | 重新申请 |

官方 CLI 把凭据写到 `PIKPAK_DIR/.credentials.json`，设备 ID 写到 `.device.json`。

#### 步骤 3：刷新 access token ✅

```http
POST https://user.mypikpak.com/v1/auth/token
```

```json
{
  "client_id": "aal7sToC85zMXEpJ",
  "grant_type": "refresh_token",
  "refresh_token": "<refresh_token>"
}
```

响应包含新的 `access_token`、`refresh_token`、`expires_in`、`sub`。实测响应：

```json
{
  "token_type": "Bearer",
  "access_token": "...",
  "refresh_token": "...",
  "expires_in": 43200,
  "sub": "<user_id>"
}
```

access token 有效期实测 43200 秒（12 小时）；refresh token 会被轮换，必须保存新值。refresh token 失效返回 `invalid_grant`（错误码 4126），必须让用户重新授权。

### 2.2 OAuth scope

| scope | 用途 |
|---|---|
| `files:read` | 列目录、读文件信息、取下载/播放链接 |
| `files:write` | 上传、新建文件夹、重命名等写入 |
| `files:manage` | 移动、复制、删除进回收站、恢复 |
| `files:purge` | 永久删除、清空回收站 |
| `cloud_download` | 创建离线下载任务 |
| `share` | 创建/管理分享、转存 |
| `account` | 账号、配额、会员信息 |
| `invite` | 邀请；PAT 不提供，本应用不需要 |

### 2.3 个人访问令牌（PAT，可选回退）

官方帮助明确：

- 在“账号与安全 → 已连接的应用 → 个人访问令牌”创建。
- 有效期 30 天～1 年，默认 90 天，不提供永久令牌。
- 默认只有“读取文件”权限；**“永久删除”和“邀请”不可授予**。
- 令牌只在创建时显示一次；创建会发邮件通知。
- 官方定位是“自己的脚本、定时任务、NAS 等无人值守系统”。

PAT 是否能直接作为 `Authorization: Bearer` 调 REST，**没有官方文档**。官方 CLI 支持 `PIKPAK_TOKEN` 和 `pikpak auth token <token>`，说明 PAT 至少对 CLI 有效；对 REST 需要实测（❓）。如果 OAuth 设备码可用，本应用不必依赖 PAT。

> 结论：本项目以 OAuth 为主。PAT 只作为可选的兼容入口；若启用，永久删除功能仍然不可用。

### 2.4 设备 ID

- 32 位十六进制字符串，例如 `6cabc5868d6d2acd9b7736543c794d24`。
- 每个飞牛用户绑定一个独立的设备 ID，服务端持久化。
- 删除/更换设备 ID 会改变风控视角，可能触发验证或限速。

---

## 3. 验证码令牌（Captcha Token）

部分接口（登录、上传建文件、取下载链接等）会要求 `X-Captcha-Token`。OAuth 设备码登录本身不需要验证码 ✅，但后续云盘接口是否全部免验证码仍需实测 ❓。

### 3.1 获取

```http
POST https://user.mypikpak.com/v1/shield/captcha/init
Content-Type: application/json
```

```json
{
  "action": "GET:/drive/v1/files",
  "captcha_token": "",
  "client_id": "<client_id>",
  "device_id": "<device_id>",
  "meta": {
    "client_version": "1.0.0",
    "package_name": "mypikpak.com",
    "user_id": "<user_id>",
    "timestamp": "<毫秒时间戳>",
    "captcha_sign": "1.<签名>"
  },
  "redirect_uri": "xlaccsdk01://xbase.cloud/callback?state=harbor"
}
```

响应：

```json
{
  "captcha_token": "...",
  "expires_in": 300,
  "url": ""
}
```

- `action` 格式为 `METHOD:/path`，例如 `GET:/drive/v1/files`、`POST:/drive/v1/files`。
- 若 `url` 非空，表示需要网页人机验证，应把链接展示给用户。
- 错误码 9 / 4002 表示验证码无效或过期，刷新后重试。

### 3.2 签名

社区实现（OpenList）的算法：

```
str = client_id + client_version + package_name + device_id + timestamp
for each algorithm in platform_algorithms:
    str = MD5(str + algorithm)
captcha_sign = "1." + str
```

不同平台（web / android / pc）的 algorithm 常量不同。常量与完整实现见 OpenList `drivers/pikpak/util.go`。本应用若走官方 CLI 的 client_id，优先实测是否需要验证码；需要时再移植该算法。

---

## 4. 文件与文件夹

### 4.1 列出目录 ✅

```http
GET https://api-drive.mypikpak.com/drive/v1/files
  ?parent_id=<folder_id>
  &limit=100
  &thumbnail_size=SIZE_MEDIUM
  &with_audit=true
  &page_token=<token>
  &filters={"phase":{"eq":"PHASE_TYPE_COMPLETE"},"trashed":{"eq":false}}
```

- 根目录的 `parent_id` 为空字符串或不传。
- `filters` 是 JSON 字符串。
- 常用 `thumbnail_size`：`SIZE_SMALL` / `SIZE_MEDIUM` / `SIZE_LARGE`。

响应：

```json
{
  "kind": "drive#fileList",
  "files": [
    {
      "id": "...",
      "kind": "drive#file",
      "name": "example.mp4",
      "size": "1234567",
      "hash": "...",
      "parent_id": "...",
      "phase": "PHASE_TYPE_COMPLETE",
      "file_category": "VIDEO",
      "mime_type": "video/mp4",
      "created_time": "2026-01-01T00:00:00.000+08:00",
      "modified_time": "2026-01-01T00:00:00.000+08:00",
      "thumbnail_link": "https://...",
      "web_content_link": "https://...",
      "medias": [],
      "starred": false,
      "trashed": false
    }
  ],
  "next_page_token": "..."
}
```

`filters` 支持的字段（◐）：

| 字段 | 运算符 | 示例 |
|---|---|---|
| `phase` | `eq` / `in` | `{"phase":{"eq":"PHASE_TYPE_COMPLETE"}}` |
| `trashed` | `eq` | `{"trashed":{"eq":true}}` |
| `kind` | `eq` | `{"kind":{"eq":"drive#folder"}}` |
| `starred` | `eq` | `{"starred":{"eq":true}}` |
| `modified_time` | `gt` / `lt` | RFC3339 时间 |
| `system_tag` | `in` | `{"system_tag":{"in":"STAR"}}` |

#### 4.1.1 排序参数（0.1.0 实测）

2026-11-21 在真机用 `order=name`、`-size`、`-modified_time`、`-created_time` 请求 `/drive/v1/files`，四种写法**全部返回 400 `invalid_argument`**，说明裸值不是合法编码。分享页的排序参数用的是另一套词表（`name_asc` / `size_desc` / `mod_desc` 等，见 PikPak 帮助中心分享设置页），而 drive 列表接口是否接受同一套词表尚未验证。

0.1.1 的 `pikpak.order` 探针改为逐个尝试：

| 写法 | 含义 |
|---|---|
| `"name"` / `"-size"` / `"-modified_time"` / `"-created_time"` | JSON 字符串编码（`order=%22name%22`） |
| `name_asc` / `size_desc` / `mod_desc` | 分享页词表 |
| `name` | 裸值，保留作为对照（已知 400） |

在结果确认前，v1 的排序只在前端对已加载页生效，并在 UI 标注。

### 4.2 获取文件详情 / 下载链接 ◐

```http
GET https://api-drive.mypikpak.com/drive/v1/files/<file_id>
  ?usage=FETCH
  &_magic=2021
  &thumbnail_size=SIZE_LARGE
```

- 官方 Web 还会传 `unzip_password`（压缩包）和 `x-global-file-token` 请求头；普通文件不需要。
- `usage=FETCH`：普通下载链接。
- `usage=CACHE`：优先返回转码/缓存媒体的链接（OpenList 使用）。
- `_magic=2021`：社区实现固定传值，作用未知但被广泛使用。
- 部分情况下需要先取 captcha token，并把 `X-Captcha-Token` 带上。

关键响应字段：

| 字段 | 说明 |
|---|---|
| `web_content_link` | 原始文件下载地址 |
| `links["application/octet-stream"].url` | 另一种下载地址 |
| `medias[]` | 可播放/转码版本列表 |
| `medias[].link.url` | 播放/下载地址，带过期参数 |
| `medias[].video` | 宽高、时长、码率、视频/音频编码 |
| `medias[].resolution_name` | 清晰度名称 |
| `medias[].is_default` | 默认版本 |

链接通常带 `expire` 查询参数，过期后重新请求。播放/下载代理必须重新取链接，不能长期缓存。

### 4.3 新建文件夹 ✅

```http
POST https://api-drive.mypikpak.com/drive/v1/files
```

```json
{
  "kind": "drive#folder",
  "name": "新建文件夹",
  "parent_id": "<parent_folder_id>"
}
```

响应包含 `upload_type` 和 `file` 对象（含新文件夹 `id`）。实测返回：

```json
{
  "upload_type": "UPLOAD_TYPE_UNKNOWN",
  "file": { "id": "...", "kind": "drive#folder", "name": "...", "...": "..." }
}
```

### 4.4 重命名 ◐

```http
PATCH https://api-drive.mypikpak.com/drive/v1/files/<file_id>
```

```json
{ "name": "新名称" }
```

目前已知可改字段只有 `name`。

### 4.5 批量移动 / 复制 ◐

```http
POST https://api-drive.mypikpak.com/drive/v1/files:batchMove
POST https://api-drive.mypikpak.com/drive/v1/files:batchCopy
```

```json
{
  "ids": ["<id1>", "<id2>"],
  "to": { "parent_id": "<target_folder_id>" }
}
```

移动到根目录时 `to` 可为空对象或不传。

### 4.6 删除到回收站 / 恢复 / 永久删除 ◐

```http
POST https://api-drive.mypikpak.com/drive/v1/files:batchTrash
POST https://api-drive.mypikpak.com/drive/v1/files:batchUntrash
POST https://api-drive.mypikpak.com/drive/v1/files:batchDelete
```

请求体：

```json
{ "ids": ["<id1>", "<id2>"] }
```

- `batchTrash`：进回收站，PAT 可用。✅ 实测返回 `{"task_id":"..."}`，任务很快完成。
- `batchUntrash`：从回收站恢复。✅ 实测返回 `task_id`，恢复后文件回到原目录。
- `batchDelete`：永久删除，**需要 `files:purge`**；PAT 无法授予该权限，必须用 OAuth。◐
- 这些批量操作返回 `task_id`，是异步任务，需要轮询 `/drive/v1/tasks/<task_id>` 直到 `PHASE_TYPE_COMPLETE`（✅ 实测任务立即完成）。
- 回收站列表有**最终一致性延迟**：`batchTrash` 的任务刚完成时列表可能仍为空，稍等 1～3 秒再查。

### 4.7 星标 ◐

```http
POST https://api-drive.mypikpak.com/drive/v1/files:star
POST https://api-drive.mypikpak.com/drive/v1/files:unstar
```

```json
{ "ids": ["<id>"] }
```

星标列表：

```http
GET /drive/v1/files?parent_id=*&filters={"system_tag":{"in":"STAR"},"trashed":{"eq":false},"phase":{"eq":"PHASE_TYPE_COMPLETE"}}
```

### 4.8 最近添加 ✅

```http
GET https://api-drive.mypikpak.com/drive/v1/events?thumbnail_size=SIZE_MEDIUM&limit=100&next_page_token=<token>
```

官方 Web 还会用 `filters={"type":{"in":"TYPE_PLAY"}}` 过滤播放记录，并通过 `POST /drive/v1/events` 上报播放事件：

```json
{
  "event": {
    "type": "TYPE_PLAY",
    "file_id": "<file_id>",
    "params": { "play_duration": 0, "play_seconds": 0 }
  }
}
```

另有 `POST /drive/v1/events:clear`、`POST /drive/v1/events:delete`（△）。

### 4.9 搜索 ❓

官方 Web 有搜索，但社区实现和官方 CLI 当前都不提供搜索命令（官方 CLI 文档注明搜索禁用）。可能的实现方式：

1. 发现 PikPak Web 的搜索接口（需要抓包/读前端 JS）。
2. 逐目录遍历 + 本地索引（大账号成本高）。
3. 使用 PikPak 官方 MCP/其他连接方式（不适合作为核心依赖）。

2026-11-21 真机实测四个候选：

| 请求 | 结果 |
|---|---|
| `/drive/v1/files?filters={"name":{"contains":"test"}}` | 404 `file_invalid_file_filters` |
| `/drive/v1/files?q=test` | **200，但返回的是目录列表**（结果里有不含 `test` 的文件） |
| `/drive/v1/search?q=test` | 404 `not_found` |
| `/drive/v1/files:search?q=test` | 400 `invalid_argument` |

结论：`q` 被忽略，200 不代表搜索生效——0.1.0 的探针把它误判成“可用”。0.1.1 改为要求**返回的每个文件名都包含关键词**才算命中，并新增带 `phase`/`trashed` 的完整 `filters` 候选（只带 `name` 的 filters 被 404，可能就是缺这两个必填项）。

本应用 v1 若无法确认搜索接口，先做“当前目录过滤 + 全局索引（可选后台任务）”，并在文档中标记为待验证。

---

## 5. 离线下载（云下载）任务

### 5.1 创建任务 ◐

```http
POST https://api-drive.mypikpak.com/drive/v1/files
```

```json
{
  "kind": "drive#file",
  "name": "<可选，留空则由服务端识别>",
  "upload_type": "UPLOAD_TYPE_URL",
  "url": { "url": "magnet:?xt=urn:btih:..." },
  "parent_id": "<目标文件夹id，空为根目录>",
  "folder_type": "DOWNLOAD"
}
```

- `url.url` 支持磁力、种子 URL、HTTP(S) 直链。
- 响应包含 `task` 对象和 `upload_type`。
- 官方 CLI 命令：`pikpak task add <url>`。

### 5.2 任务列表 ✅

```http
GET https://api-drive.mypikpak.com/drive/v1/tasks
  ?type=offline
  &thumbnail_size=SIZE_SMALL
  &limit=100
  &page_token=<token>
  &with=reference_resource
  &filters={"phase":{"in":"PHASE_TYPE_RUNNING,PHASE_TYPE_PENDING,PHASE_TYPE_ERROR"}}
```

`phase` 取值：

| 值 | 含义 |
|---|---|
| `PHASE_TYPE_PENDING` | 排队中 |
| `PHASE_TYPE_RUNNING` | 下载中 |
| `PHASE_TYPE_PAUSED` | 已暂停（官方 CLI 字符串中出现） |
| `PHASE_TYPE_COMPLETE` | 完成 |
| `PHASE_TYPE_ERROR` | 失败 |

任务字段（节选）：

```json
{
  "id": "...",
  "kind": "drive#task",
  "type": "offline",
  "name": "...",
  "file_id": "...",
  "file_name": "...",
  "file_size": "1234567",
  "phase": "PHASE_TYPE_RUNNING",
  "progress": 42,
  "message": "Saving",
  "created_time": "...",
  "updated_time": "...",
  "icon_link": "https://...",
  "params": { "url": "magnet:?xt=..." },
  "reference_resource": { "...": "文件对象" }
}
```

官方 Web 的列表请求还带 `with=file_ids&with=reference_resource`；单个任务的进度明细可查：

```http
GET /drive/v1/task/<task_id>/statuses?limit=<n>
```

### 5.3 单个任务 △

```http
GET https://api-drive.mypikpak.com/drive/v1/tasks/<task_id>
```

### 5.4 重试 ◐

```http
POST https://api-drive.mypikpak.com/drive/v1/task
```

```json
{
  "type": "offline",
  "create_type": "RETRY",
  "id": "<task_id>"
}
```

### 5.5 删除任务 ◐

```http
DELETE https://api-drive.mypikpak.com/drive/v1/tasks?task_ids=<id1>,<id2>&delete_files=false
```

`delete_files=true` 时同时删除已下载的文件。

批量清理任务（官方 Web）：

```http
POST https://api-drive.mypikpak.com/drive/v1/tasks:clear
```

```json
{
  "type": "offline",
  "phases": ["PHASE_TYPE_COMPLETE", "PHASE_TYPE_ERROR"],
  "client_id": ""
}
```

### 5.6 暂停 / 继续 ❓

官方 CLI 字符串包含 `PHASE_TYPE_PAUSED`，但未确认公开的暂停/继续接口。可能是 `PATCH /drive/v1/tasks/<id>` 或 `POST /drive/v1/task` 的某个 `create_type`。v1 先不做，或只做“删除/重试”。

---

## 6. 上传

上传是两步：先向 PikPak 申请上传会话，再直传阿里云 OSS。◐

### 6.1 计算 GCID

PikPak 使用自定义的 GCID（类似 SHA1 的分块哈希）。社区实现：

- OpenList：`hash_extend.GCID`
- rclone：`calcCid` / `getGcid`

本应用需要移植该算法，或在服务端流式计算。零字节文件也要提供 `hash` 和 `size`，否则返回 `cid and file_size is required`。

### 6.2 申请上传 ◐

```http
POST https://api-drive.mypikpak.com/drive/v1/files
```

```json
{
  "kind": "drive#file",
  "name": "example.mp4",
  "size": 1234567,
  "hash": "<GCID，大写>",
  "upload_type": "UPLOAD_TYPE_RESUMABLE",
  "objProvider": { "provider": "UPLOAD_TYPE_UNKNOWN" },
  "parent_id": "<target_folder_id>",
  "folder_type": "NORMAL"
}
```

响应有两种：

**秒传**：`resumable` 为空，直接返回 `file`，无需上传。

**需要上传**：

```json
{
  "upload_type": "UPLOAD_TYPE_RESUMABLE",
  "resumable": {
    "kind": "drive#resumable",
    "provider": "PROVIDER_ALIYUN",
    "params": {
      "access_key_id": "...",
      "access_key_secret": "...",
      "bucket": "...",
      "endpoint": "...",
      "expiration": "...",
      "key": "...",
      "security_token": "..."
    }
  }
}
```

### 6.3 上传到 OSS ◐

- 小文件（社区实现阈值 10 MB）可直接 `PutObject`。
- 大文件用 multipart：
  - 分片大小：1 MB ～ 5 GB；官方 CLI 默认 6 MB，OpenList 按文件大小分 100～1000 片。
  - 最多 10000 片。
  - 并发：官方 CLI 默认 3，OpenList 10。
  - 每个分片请求带 `X-OSS-Security-Token: <security_token>`。
  - 完成后 `CompleteMultipartUpload`。
- OSS endpoint 需要规范化：web 端返回 `mypikpak.net`，Android 端可能返回 `vip-lixian-07.mypikpak.net`；OpenList 在 Android 平台强制改为 `mypikpak.net`。
- 文件名含 `&` 或 `<` 时，完成分片的 XML 响应可能解析失败，但上传实际成功；需要容忍 `io.EOF`。

### 6.4 上传后处理

上传完成后文件可能短暂处于 `PHASE_TYPE_RUNNING`，需要轮询文件详情直到 `PHASE_TYPE_COMPLETE`。官方 CLI 的 `-w` 就是等待该状态。

### 6.5 实测（官方 CLI 上传 1 KB 文本）

CLI 命令：`pikpak upload <path> --name <remote> -p <folder_id> -F json`

响应：

```json
{
  "file_id": "...",
  "name": "...",
  "phase": "PHASE_TYPE_PENDING",
  "instant_upload": false,
  "task_id": "..."
}
```

- 新文件初始为 `PHASE_TYPE_PENDING`，随后很快变为 `PHASE_TYPE_COMPLETE`。
- `instant_upload` 为 `true` 表示命中秒传。
- 直接 REST 上传仍需按 §6.2 的 GCID + OSS 流程实现。

### 6.6 文件夹上传

递归创建远端文件夹结构，再逐个上传文件。远端名称冲突会返回 `file_duplicated_name`（错误码 3）。

---

## 7. 下载与播放

### 7.1 下载 ✅

1. `GET /drive/v1/files/<id>?usage=FETCH&_magic=2021` 取 `web_content_link`。
2. 对该 URL 发 Range 请求，服务端支持断点续传。
3. 大文件建议多连接分段下载；`medias[].link.url` 对并发 Range 更友好（rclone 注释）。
4. 链接会过期，长任务需要在过期前刷新。

实测（1 KB 文本文件）：

- `web_content_link` 与 `links["application/octet-stream"].url` 指向同一类签名地址，主机形如 `dl-a10b-1556.mypikpak.com`。
- 签名 URL 带 `expire`、`sign`、`userid` 等查询参数；**请求该 URL 不需要再带 Authorization**。
- `Range: bytes=0-1023` 返回 `206 Partial Content`，内容正确。
- 纯文本文件 `medias` 为空；视频文件才会有 `medias` 转码版本。

### 7.2 在线播放

- 优先使用 `medias[]` 中 `is_default` 或最高清晰度的 `link.url`。
- 视频元数据在 `medias[].video`：`width`、`height`、`duration`、`bit_rate`、`video_codec`（`h264`/`hevc`）、`audio_codec`、`video_type`（如 `mpegts`）。
- `medias[].need_more_quota` 表示需要更多流量/会员额度。
- 浏览器直接播放受编码/封装限制；后端代理并透传 Range 即可，不转码。
- 播放地址也是带签名的 URL，后端应每次播放前重新获取，避免把长期链接暴露给前端。

### 7.3 字幕 ❓

尚未确认字幕接口。官方 Web 播放器可能通过文件关联资源或 `medias` 之外的字段加载字幕；需要抓包验证。v1 先支持视频本身，字幕作为待验证项。

---

## 8. 分享

### 8.1 创建分享 ◐

```http
POST https://api-drive.mypikpak.com/drive/v1/share
```

```json
{
  "file_ids": ["<id1>", "<id2>"],
  "share_to": "publiclink",
  "expiration_days": -1,
  "pass_code_option": "NOT_REQUIRED"
}
```

- `share_to`：`publiclink` 或 `encryptedlink`
- `expiration_days`：`-1` 表示永久
- `pass_code_option`：`NOT_REQUIRED` / `REQUIRED`

响应：

```json
{
  "share_id": "...",
  "share_url": "https://mypikpak.com/s/...",
  "pass_code": "53fe",
  "share_text": "https://mypikpak.com/s/...",
  "share_list": []
}
```

### 8.2 获取分享信息 ◐

```http
GET https://api-drive.mypikpak.com/drive/v1/share
  ?share_id=<share_id>
  &pass_code=<可选密码>
  &thumbnail_size=SIZE_LARGE
  &limit=100
```

响应包含 `pass_code_token`、分享状态、文件列表。密码错误时 `share_status` 为 `PASS_CODE_EMPTY` / `PASS_CODE_ERROR`，先用密码换 `pass_code_token` 再重试。

### 8.3 浏览分享目录 ◐

```http
GET https://api-drive.mypikpak.com/drive/v1/share/detail
  ?share_id=<share_id>
  &parent_id=<folder_id>
  &pass_code_token=<token>
  &thumbnail_size=SIZE_LARGE
  &with_audit=true
  &limit=100
  &page_token=<token>
  &filters={"phase":{"eq":"PHASE_TYPE_COMPLETE"},"trashed":{"eq":false}}
```

### 8.4 分享文件下载链接 ◐

```http
GET https://api-drive.mypikpak.com/drive/v1/share/file_info
  ?share_id=<share_id>
  &file_id=<file_id>
  &pass_code_token=<token>
```

响应中的 `file_info.web_content_link` 或 `file_info.medias[].link.url` 可用于下载/播放。

批量版本（官方 Web 使用）：

```http
POST https://api-drive.mypikpak.com/drive/v1/share:batch_file_info
```

请求体与 `share/file_info` 类似，用于一次取多个文件的链接。

### 8.5 转存到自己网盘 ◐

```http
POST https://api-drive.mypikpak.com/drive/v1/share/restore
```

```json
{
  "share_id": "<share_id>",
  "pass_code_token": "<token>",
  "file_ids": ["<id1>", "<id2>"]
}
```

### 8.6 我的分享 / 删除分享 / 转存历史 ◐

列出我的分享（已实测）：

```http
GET https://api-drive.mypikpak.com/drive/v1/share/list?limit=10
```

响应结构：

```json
{ "data": [], "next_page_token": "" }
```

分享对象的具体字段待有分享数据后确认。删除分享（官方 Web）：

```http
POST https://api-drive.mypikpak.com/drive/v1/share:batchDelete
```

```json
{ "ids": ["<share_id>"] }
```

转存历史对应接口仍未确认；官方 CLI 提供 `share history`。

---

## 9. 账号、配额、会员

### 9.1 配额 ✅

```http
GET https://api-drive.mypikpak.com/drive/v1/about
```

```json
{
  "kind": "drive#about",
  "quota": {
    "kind": "drive#quota",
    "limit": "10995116277760",
    "usage": "5113157556024",
    "usage_in_trash": "1281564700871",
    "play_times_limit": "-1",
    "play_times_usage": "0",
    "is_unlimited": false
  },
  "expires_at": "",
  "quotas": {}
}
```

`usage_in_trash` 社区反馈“似乎不工作”（rclone 注释），不要完全依赖。

### 9.2 会员信息 ✅

```http
GET https://api-drive.mypikpak.com/drive/v1/privilege/vip
```

返回 `result`、`message`、`data.status`、`data.type`（`novip` / `platinum`）、`data.expire`。

### 9.3 用户信息 ✅

```http
GET https://user.mypikpak.com/v1/user/me
```

返回 `sub`、`name`、`picture`、`email`、`phone_number`、`created_at` 等。

### 9.4 传输额度 △

```http
GET https://api-drive.mypikpak.com/vip/v1/quantity/list?type=transfer
```

### 9.5 邀请码 △

```http
GET https://api-drive.mypikpak.com/vip/v1/activity/inviteCode
```

---

## 10. 回收站

### 10.1 列出回收站 ✅

```http
GET https://api-drive.mypikpak.com/drive/v1/files
  ?thumbnail_size=SIZE_LARGE
  &with_audit=true
  &limit=100
  &page_token=<token>
  &filters={"phase":{"eq":"PHASE_TYPE_COMPLETE"},"trashed":{"eq":true}}
```

不传 `parent_id`，表示跨目录列出所有已删除项。

实测：`batchTrash` 后需要等 1～3 秒再查，否则可能返回空列表（最终一致性）。回收站文件的 `delete_time` 是预计清空时间，实测约为删除后 15 天。

### 10.2 恢复 ✅

```http
POST /drive/v1/files:batchUntrash
{ "ids": ["<id>"] }
```

### 10.3 清空回收站 / 永久删除 ✅

官方 Web 使用的清空接口：

```http
PATCH https://api-drive.mypikpak.com/drive/v1/files/trash:empty
```

- 无请求体。
- 实测 HTTP 200，响应 `{"task_id":""}`（没有异步任务，立即生效）。
- 需要 `files:purge`。
- 清空后回收站列表为空。

也可以对具体文件调用：

```http
POST https://api-drive.mypikpak.com/drive/v1/files:batchDelete
{ "ids": ["<id1>", "<id2>"] }
```

- 对回收站里的所有文件调用 `batchDelete` 也能清空。
- PAT 没有 `files:purge`，所以只能用 OAuth。

---

## 11. 错误码与重试

| error_code | error | 含义 | 处理 |
|---|---|---|---|
| 0 | — | 成功 | — |
| 3 | `file_duplicated_name` | 同名文件 | 换名或覆盖策略 |
| 8 | `file_space_not_enough` | 空间不足 | 提示扩容/清理 |
| 9 | `captcha_invalid` | 验证码失效 | 刷新 captcha token 后重试 |
| 10 | 操作频繁 | 限流 | 指数退避 |
| 11 | `task_daily_create_limit_vip` | 当日离线任务超限 | 提示次日再试 |
| 16 | `unauthenticated` | access token 无效 | 刷新 token |
| 4002 | `captcha_invalid` | 验证码无效 | 刷新 captcha token |
| 4121 | — | access token 无效 | 刷新 token |
| 4122 | — | access token 过期 | 刷新 token |
| 4126 | `invalid_grant` | refresh token 失效 | 重新授权 |
| — | `file_rename_uncompleted` | 文件尚未处理完，不能重命名 | 稍后重试 |
| — | `cid and file_size is required` | 上传缺少 hash/size | 补全参数 |

通用策略：

- 401/16/4121/4122：刷新 token 后重试一次。
- 9/4002：刷新 captcha token 后重试一次。
- 10/429：指数退避，带随机抖动。
- 5xx：最多重试 3 次。
- 4126：标记该用户需要重新授权，不自动重试。

---

## 12. 限制与风险

1. **PikPak 没有公开 REST 文档**：除设备码发起外，本文接口均来自社区逆向或官方 CLI 行为，官方可能随时调整。
2. **PAT 不能永久删除**：官方明确不提供该权限；清空回收站必须用 OAuth（`files:purge`）。
3. **PAT 有有效期**：最长 1 年，需要过期提醒。
4. **离线任务每日限额**：`task_daily_create_limit_vip` 会直接拒绝新任务。
5. **连接应用下载额度**：有第三方博客称 MCP/CLI/PAT/WebDAV 共享每月总流量的 25% 下载额度，官方未确认 ❓。官方 CLI 字符串也提示“下载和串流可能被限制到约 100 KB/s，直到额度重置”。
6. **验证码**：部分操作需要 `X-Captcha-Token`，可能需要网页人机验证；NAS 无人值守场景要能把验证链接展示给用户。
7. **设备 ID 风控**：频繁更换设备 ID 可能触发风控。
8. **官方 CLI 客户端标识**：`aal7sToC85zMXEpJ` 属于官方 CLI。自建应用长期依赖该标识有被识别/封禁的风险；目前没有公开的第三方应用注册入口。
9. **上传依赖阿里云 OSS SDK/协议**：需要处理分片、签名头、响应解析容错。
10. **搜索、字幕、任务暂停**：当前没有确认的公开接口，需抓包补齐。

---

## 13. 验证清单

已完成：

- [x] 设备码申请：`POST /v1/auth/device/code`，拿到 `device_code` / `user_code` / `verification_uri_complete`。
- [x] OAuth 授权完成，拿到含 `files:purge` 的令牌；refresh token 流程直连验证通过。
- [x] 官方 CLI 命令面：`ls`、`get`、`mkdir`、`cp`、`mv`、`rename`、`rm`、`trash`、`task`、`share`、`upload`、`download`、`play`、`star`。
- [x] 官方 CLI 明确说明：回收站只支持列出和恢复，不支持永久删除/清空。
- [x] 只读 REST：`/drive/v1/files`、`/drive/v1/about`、`/drive/v1/tasks`、回收站列表、`/drive/v1/events`、`/drive/v1/share/list`、`/v1/user/me`、`/drive/v1/privilege/vip`。
- [x] OAuth 调用这些只读接口不需要 `X-Captcha-Token`。
- [x] 从官方 Web 前端提取到：`PATCH /files/trash:empty`、`POST /share:batchDelete`、`POST /share:batch_file_info`、`POST /tasks:clear`、`GET /task/<id>/statuses` 等接口。

写入验证已完成：

- [x] 新建文件夹、上传 1 KB 文本、`GET /files/<id>?usage=FETCH` 链接结构、Range 206。
- [x] `batchTrash` / `batchUntrash`，回收站列表有 1～3 秒最终一致性。
- [x] `PATCH /files/trash:empty`，返回 `{"task_id":""}`，回收站立即清空。
- [x] 测试对象已永久删除，账号恢复原状。

仍待验证：

- [ ] 搜索接口（当前静态 JS 中未找到，可能在动态加载的 chunk）。
- [ ] 分享详情/转存历史接口。
- [ ] 直接 REST 上传（GCID + OSS 分片）——目前只验证了官方 CLI 上传。
- [ ] PAT 是否能直接调 REST（可选）。
---

## 14. 参考来源

- PikPak 官方帮助：个人访问令牌  
  https://mypikpak.net/zh-CN/help-center/connected_apps/personal_access_tokens/what_is_personal_access_token
- PikPak 官方帮助：创建个人访问令牌  
  https://mypikpak.net/zh-CN/help-center/connected_apps/personal_access_tokens/create_personal_access_token
- PikPak 官方 CLI（v0.6.0）  
  https://download.mypikpak.com/cli/install.sh  
  https://download.mypikpak.com/cli/release/v0.6.0/pikpak_linux_amd64
- Quan666/PikPakAPI（Python）  
  https://github.com/Quan666/PikPakAPI
- OpenList PikPak 驱动  
  https://github.com/OpenListTeam/OpenList/tree/main/drivers/pikpak
- OpenList PikPak 分享驱动  
  https://github.com/OpenListTeam/OpenList/tree/main/drivers/pikpak_share
- rclone PikPak 后端  
  https://github.com/rclone/rclone/tree/master/backend/pikpak
- chris-lsn/pikpak-js-sdk API 参考  
  https://deepwiki.com/chris-lsn/pikpak-js-sdk/3-api-reference
- pikpak-go 错误码  
  https://pkg.go.dev/github.com/akshardp/pikpak-go
- PikPak 官方 Web 前端（drive 静态 JS）  
  https://mypikpak.com/drive

---

## 15. 实测记录（2026-11-21）

- 账号：Blue Ice（user_id `Z7A4R428EYCaNIMt`，仅用于确认令牌归属；本文档不保存任何令牌）。
- 授权方式：OAuth 设备码，scope 为 `files:read files:write files:manage files:purge cloud_download share account`。
- 授权结果中的 `scopes`：`files:purge`、`cloud_download`、`share`、`account`（读写权限未重复列出，但列表调用成功）。
- Token 交换/刷新：`POST https://user.mypikpak.com/v1/auth/token`，刷新响应 `token_type=Bearer`、`expires_in=43200`、`sub=Z7A4R428EYCaNIMt`；refresh token 会轮换。
- 只读接口结果（均 HTTP 200）：
  - `GET /drive/v1/about`：配额 6,442,450,944 字节，已用 2,304,881,117 字节。
  - `GET /drive/v1/files`（根目录）：2 个文件夹，`My Pack`、`Pack From Shared`。
  - `GET /drive/v1/files`（回收站 filters `trashed=true`）：0 条。
  - `GET /drive/v1/tasks?type=offline`：1 条，`PHASE_TYPE_ERROR`。
  - `GET /drive/v1/events`：3 条 `TYPE_RESTORE` 事件。
  - `GET /drive/v1/share/list`：`{"data":[],"next_page_token":""}`。
  - `GET https://user.mypikpak.com/v1/user/me`：成功。
  - `GET /drive/v1/privilege/vip`：成功。
- 请求头：`Authorization: Bearer`、`X-Client-Id`、`X-Client-Version: 1.0.0`、`X-Device-Id`、`Content-Type`、`User-Agent`。只读请求未带 `X-Captcha-Token`。
- 官方 Web 前端静态资源：从 `https://mypikpak.com/drive` 下载的 JS bundles 中确认了 `PATCH /files/trash:empty`、`POST /share:batchDelete`、`POST /share:batch_file_info`、`POST /tasks:clear`、`GET /task/<id>/statuses`、`/files/pack`、`/files/unpack`。

### 写入验证（2026-11-21 15:00–15:03）

| 步骤 | 接口 | 结果 |
|---|---|---|
| 建临时文件夹 | `POST /drive/v1/files` | 200，返回 `upload_type` + `file.id` |
| 上传 1 KB 文本 | 官方 CLI `pikpak upload` | `file_id`、`phase=PHASE_TYPE_PENDING`、`instant_upload=false`、`task_id`；随后变为 `PHASE_TYPE_COMPLETE` |
| 取链接 | `GET /drive/v1/files/<id>?usage=FETCH&_magic=2021&thumbnail_size=SIZE_LARGE` | `web_content_link` 和 `links["application/octet-stream"].url` 可用；无 `medias`（文本文件） |
| Range | 签名 URL + `Range: bytes=0-1023` | `206`，1024 字节，内容正确；不需要 Authorization |
| 删除进回收站 | `POST /files:batchTrash` | `task_id`，任务立即完成 |
| 查回收站 | `GET /files` + `filters={"trashed":{"eq":true}}` | 1～3 秒后出现；`delete_time` 约为 15 天后 |
| 恢复 | `POST /files:batchUntrash` | `task_id`，文件回到原目录 |
| 清空回收站 | `PATCH /files/trash:empty` | 200，`{"task_id":""}`，回收站立即为空 |
| 清理 | `batchTrash` 文件夹 + `trash:empty` | 根目录恢复为 `My Pack`、`Pack From Shared`；回收站为空 |

测试对象 `_fnos_pikpak_test_20261121` 和 `_fnos_pikpak_test_07321117.txt` 已永久删除。临时 OAuth 凭据、CLI 二进制和测试文件已从 `/tmp` 清理。
