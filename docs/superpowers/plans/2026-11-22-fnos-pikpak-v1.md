# PikPak for fnOS v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 M0 验证版扩展成完整可用的 v1：文件浏览与管理、离线任务、上传/下载、回收站、在线播放，按飞牛用户隔离。

**Architecture:** 单个 Go 二进制经 Unix socket 接入飞牛网关，React 前端由 `go:embed` 打进二进制；PikPak 访问集中在 `internal/pikpak`，飞牛 Open API 集中在 `internal/fnos`，SQLite 存凭据与任务；传输与播放都由后端代理，浏览器不接触 PikPak 令牌。

**Tech Stack:** Go 1.27、`modernc.org/sqlite`、`github.com/aliyun/aliyun-oss-go-sdk/oss`、React 19 + Vite + TypeScript、`@trimjs/web-app`。

**Spec:** `docs/superpowers/specs/2026-11-21-fnos-pikpak-design.md`（下称"设计"）与 `docs/pikpak-api.md`。

## Global Constraints

- 身份只来自网关 `X-Trim-Userid`；缺失 401，重复 400；任何请求参数里的 uid 一律忽略。
- 不以 root 运行；socket、DB、密钥 0600；不新增 TCP 监听（`--listen` 仅调试）。
- 令牌、签名 URL、device_code 不进日志、前端与报告；日志走 `probe.Redact` 同等规则。
- 所有写接口要求 `Content-Type: application/json`（已有 `requireJSONBody`）。
- 前端不显示 `/vol1/...`，一律 `semanticPath`；外链域名白名单 `mypikpak.com`。
- 每个任务的完成门槛：`go test ./...` 全绿 + 该任务的验收步骤实际跑过。

## M0 已验证、直接作为前提的结论

| 结论 | 影响 |
|---|---|
| 列表接口不接受 `order`（8 种写法全 400） | 排序只在前端对已加载页做，UI 标注"仅已加载项"；`/api/files` 不再传 `order` |
| `?q=` 不搜索、filters 的 `name.contains` 被拒 | 不做全局搜索；`q` 只在已加载页过滤；不提供 `/api/search` |
| 上传全链路可用（GCID + 阿里云 OSS，秒传与真实上传都通过） | M3 的上传按设计实现，无降级 |
| 离线任务可创建、可轮询、可删除 | M4 按设计实现；完成速度取决于账号，UI 不承诺时间 |
| `trim.file.convertPath` 返回裸数组；`trim.system.getPlatformConfig` 403 | 语言取前端/默认值，不依赖平台接口 |
| ACL `readable/writable/deletable` 可用 | 文件授权按设计实现 |

仍未在真机验证、实现时必须做成可降级：网关前缀/伪造头/Origin（安全与路由）、Range 流式（播放）。

## Review Focus

1. 上游返回 200 但带 error envelope 的接口（列表、批量操作）必须识别为失败，不能当作空结果。
2. 跨用户隔离：A 的 file_id 在 B 的请求里必须 404，不能因为"上游能查到"就返回。
3. 批量操作返回 `task_id` 后的等待：超时必须是 `pending: true`，不能假装成功。
4. 下载写盘的路径穿越与符号链接：目标必须落在授权目录内，`.pikpak-part` 不能被指到别处。
5. 播放 token 伪造与过期：篡改 uid/exp 必须拒绝；过期返回 401 而不是透传上游。

---

## M1 基础（4 个任务）

### Task 1: 数据模型补齐

**Files:**
- Modify: `internal/store/migrate.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces: `users`、`granted_folders`、`settings`、`user_settings` 表；`store.PutUser/GetUser`、`store.GrantFolder/ListGrantedFolders/RevokeFolder`、`store.GetSetting/SetSetting`、`store.GetUserSetting/SetUserSetting`。

- [ ] **Step 1: 写迁移测试**：全新库跑迁移后 `schema_migrations` 版本递增；重复跑幂等；`users` 插入后可按 uid 读回。
- [ ] **Step 2: 运行 `go test ./internal/store/`**，预期 fail。
- [ ] **Step 3: 实现**：新增 SQL（按设计 §8），沿用现有 `migrate` 机制；设置值做长度上限（1 KiB）。
- [ ] **Step 4: 运行测试**，预期 pass。
- [ ] **Step 5: Commit** `feat: extend schema for accounts, grants and settings`。

### Task 2: 账号接口

**Files:**
- Create: `internal/httpapi/routes_account.go`
- Test: `internal/httpapi/routes_account_test.go`
- Modify: `internal/httpapi/server.go`（注册）

**Interfaces:**
- Consumes: `store.GetUser`、`sessions.ClientFor`、`pikpak.About/VIP`。
- Produces: `GET /api/account` → `{quota_limit,quota_usage,vip_type,vip_expire,cached_at}`，60 秒内存缓存；`POST /api/auth/rebind` 清除 `reauth_required`；`POST /api/auth/flow/{id}/cancel`；`GET /api/me` 增加 `account`/`capabilities`。

- [ ] **Step 1: 写测试**：未绑定返回 401 `not_bound`；绑定后返回配额；60 秒内两次请求只打一次上游；rebind 把 status 改回 active。
- [ ] **Step 2: 运行**，预期 fail。
- [ ] **Step 3: 实现**，缓存用 `sync.Mutex + map[uid]entry`，按 uid 隔离。
- [ ] **Step 4: 运行**，预期 pass。
- [ ] **Step 5: Commit**。

### Task 3: 设置接口

**Files:**
- Create: `internal/httpapi/routes_settings.go`、`internal/httpapi/routes_settings_test.go`
- Modify: `internal/httpapi/server.go`

**Interfaces:**
- Produces: `GET/PUT /api/settings`、`GET/PUT /api/user-settings`；允许的键：`download_concurrency`(1–4)、`upload_concurrency`(1–4)、`conflict`(`rename|skip|overwrite`)、`play_mode`(`proxy|redirect`)、`language`(`zh-CN|en`)。未知键 → 400 `unknown_setting`。

- [ ] **Step 1: 测试**：合法值写入后可读；越界/未知键 400；不同 uid 的 user_settings 互不可见。
- [ ] **Step 2–4: 红 → 实现 → 绿**。
- [ ] **Step 5: Commit**。

### Task 4: 前端外壳

**Files:**
- Create: `web/src/pages/{Files,Recent,Tasks,Transfers,Trash,Settings,Bind}.tsx`、`web/src/router.tsx`、`web/src/i18n.ts`
- Modify: `web/src/App.tsx`（改为外壳 + `<Outlet>`）、`web/package.json`

**Interfaces:**
- Consumes: `@trimjs/web-app` 的主题/语言；现有 `api.ts`。
- Produces: HashRouter 路由 `/files/*`、`/recent`、`/tasks`、`/transfers`、`/trash`、`/settings`、`/bind`；M0 探针页移到 `/debug`。

- [ ] **Step 1: 测试**：`npm run build` 通过；路由常量与 API 前缀工具函数有单测（`web/src/*.test.ts`，沿用现有测试框架，无则用 `node --test`）。
- [ ] **Step 2: 实现外壳**：侧栏（我的文件/最近/任务/传输/回收站）、顶部（搜索框占位、上传、新建文件夹）、底部配额条。
- [ ] **Step 3: 移动端**：<768px 侧栏变底部 tab（纯 CSS）。
- [ ] **Step 4: 运行 `make dev`**，逐个路由手工打开确认不 404。
- [ ] **Step 5: Commit**。

---

## M2 文件（6 个任务）

### Task 5: PikPak 文件操作客户端

**Files:**
- Create: `internal/pikpak/ops.go`、`internal/pikpak/ops_test.go`

**Interfaces:**
- Produces: `CreateFolder(ctx, parentID, name) (File, error)`、`Rename(ctx, id, name) error`、`BatchMove/BatchCopy(ctx, ids, toParentID) (taskID string, err)`、`BatchTrash/BatchUntrash/BatchDelete(ctx, ids) (taskID, err)`、`EmptyTrash(ctx) (taskID, err)`、`Star/Unstar(ctx, ids)`、`FileDetail(ctx, id) (File, error)`、`WaitTaskSettled(ctx, taskID, timeout) (Task, bool, error)`（bool = 是否已完成）。
- 复用 Task 1 的 `Task` 类型与 `DeletePermanently`。

- [ ] **Step 1: 写表驱动测试**：每个方法断言路径/方法/请求体；批量接口解析 `task_id`。
- [ ] **Step 2–4: 红 → 实现 → 绿**。
- [ ] **Step 5: Commit**。

### Task 6: 列表接口

**Files:**
- Create: `internal/httpapi/routes_files.go`、`internal/httpapi/routes_files_test.go`
- Modify: `internal/httpapi/server.go`

**Interfaces:**
- Produces: `GET /api/files?parent_id=&page_token=&limit=&kind=&q=&trashed=` → `{items,next_page_token,filters}`；`kind` 透传为 `filters.kind`；`q` 在返回页内做不区分大小写过滤，并在响应里标 `"q_local": true`；`limit` 默认 100、上限 200。
- 不传 `order`（已验证无效）。

- [ ] **Step 1: 测试**：带 `kind` 时 filters JSON 正确；`q` 只过滤当前页且保留 `next_page_token`；上游 200+error envelope → 502；缺身份 401。
- [ ] **Step 2–4: 红 → 实现 → 绿**。
- [ ] **Step 5: Commit**。

### Task 7: 文件写操作路由

**Files:**
- Modify: `internal/httpapi/routes_files.go`、对应测试

**Interfaces:**
- Produces: `POST /api/folders`、`PATCH /api/files/{id}`、`POST /api/files/move|copy|trash|untrash|delete|star|unstar`；批量接口等待任务 ≤10 秒，超时返回 `{ok:true,pending:true}`。
- 参数校验：`ids` 1–200 个、去重、拒绝空串；`to_parent_id` 不能是被移动项自身（明显环）。

- [ ] **Step 1: 测试**：任务立即完成 → `pending:false`；任务一直 RUNNING → 10 秒后 `pending:true`；`ids` 为空 → 400。
- [ ] **Step 2–4: 红 → 实现 → 绿（等待时间在测试里注入）**。
- [ ] **Step 5: Commit**。

### Task 8: 回收站与最近添加

**Files:**
- Modify: `internal/httpapi/routes_files.go`、测试

**Interfaces:**
- Produces: `GET /api/recent`（`/drive/v1/events` 归一化）、`GET /api/trash?page_token=`、`POST /api/trash/empty`。
- 回收站列表 = `filters:{"trashed":{"eq":true}}`；创建/恢复后延迟 1–3 秒再查（设计已注明最终一致性）。

- [ ] **Step 1: 测试**：回收站请求带 trashed 过滤；`empty` 调 `trash:empty` 并等待任务。
- [ ] **Step 2–4: 红 → 实现 → 绿**。
- [ ] **Step 5: Commit**。

### Task 9: 文件页 UI

**Files:**
- Created in Task 4 的 `web/src/pages/Files.tsx` + `web/src/components/{FileTable,FileGrid,Breadcrumb,SelectionBar}.tsx`
- Test: `web/src/components/*.test.ts`

**Interfaces:**
- Consumes: Task 6/7/8 的接口。
- Produces: 面包屑、无限滚动、多选、右键菜单、拖拽移动（v1 只支持拖到面包屑上的文件夹）、排序（名称/大小/时间，客户端，标注"仅已加载项"）、类型筛选、网格/列表切换。

- [ ] **Step 1: 测试**：排序函数与名称过滤是纯函数，单测覆盖中文/大小写/目录优先。
- [ ] **Step 2: 实现组件与页面**。
- [ ] **Step 3: `make dev` 手工走一遍**：建夹 → 重命名 → 移动 → 删除 → 回收站恢复。
- [ ] **Step 4: Commit**。

### Task 10: 最近与回收站页面

**Files:**
- `web/src/pages/Recent.tsx`、`web/src/pages/Trash.tsx`

- [ ] **Step 1: 实现列表与批量操作**；清空回收站需输入"确认"。
- [ ] **Step 2: `make dev` 手工验证**。
- [ ] **Step 3: Commit**。

---

## M3 传输（5 个任务）

### Task 11: 下载引擎

**Files:**
- Create: `internal/transfer/download.go`、`internal/transfer/download_test.go`
- Modify: `internal/store/migrate.go`（`download_jobs`，设计 §8）

**Interfaces:**
- Produces: `Downloader.Run(ctx, job)`；`.pikpak-part/{job_id}` 写入、4 MiB 缓冲、每 2 秒落进度、`size` 校验、`fsync` + 原子 rename；链接 403/410 重取最多 3 次并续传；磁盘空间预检（≥ 1.05×）。

- [ ] **Step 1: 测试**：206 续传、服务端忽略 Range（200 从头写）、断流重试、字节数不符 → failed、ENOSPC → 明确错误码。
- [ ] **Step 2–4: 红 → 实现 → 绿**。
- [ ] **Step 5: Commit**。

### Task 12: 上传引擎

**Files:**
- Create: `internal/transfer/upload.go`、`internal/transfer/upload_test.go`

**Interfaces:**
- Consumes: `pikpak.GCIDFile`、`RequestUpload`、`CompleteUpload`。
- Produces: `Uploader.Run(ctx, job)`；冲突策略 `rename|skip|overwrite`（overwrite = 先 trash 旧文件，失败则 untrash 回滚）；进程内分片重试；完成后 `WaitFileComplete`。

- [ ] **Step 1: 测试**：三种冲突策略各一条；overwrite 上传失败时旧文件被恢复；秒传不产生 OSS 请求。
- [ ] **Step 2–4: 红 → 实现 → 绿**。
- [ ] **Step 5: Commit**。

### Task 13: 任务 API 与调度

**Files:**
- Create: `internal/httpapi/routes_transfers.go`、`internal/transfer/queue.go` + 测试
- Modify: `cmd/pikpak/main.go`（启动队列、退出时落盘）

**Interfaces:**
- Produces: `GET/POST /api/downloads`、`POST /api/downloads/{id}/{pause|resume|cancel}`、`DELETE /api/downloads/{id}`；上传同理；每用户 FIFO + 全局并发（来自设置）；启动时 `running → queued`。

- [ ] **Step 1: 测试**：暂停让出槽位、恢复续传、取消清理 `.part`、重启恢复。
- [ ] **Step 2–4: 红 → 实现 → 绿**。
- [ ] **Step 5: Commit**。

### Task 14: 飞牛文件系统路由

**Files:**
- Modify: `internal/httpapi/routes_fs.go`、测试

**Interfaces:**
- Produces: `GET /api/fs/folders`（含 semanticPath）、`POST /api/fs/authorize`、`GET /api/fs/check`、`GET /api/fs/list`（仅授权集合内，拒绝 `..` 与符号链接逃逸）。
- `deps.Fnos == nil`（本机调试）→ 503 `fnos_unavailable`。

- [ ] **Step 1: 测试**：非授权路径 403；`..`/相对路径 400；符号链接指向目录外 → 403。
- [ ] **Step 2–4: 红 → 实现 → 绿**。
- [ ] **Step 5: Commit**。

### Task 15: 传输页 UI

**Files:**
- `web/src/pages/Transfers.tsx`

- [ ] **Step 1: 实现下载/上传两个 tab**：进度、速度、剩余时间、暂停/继续/取消、失败原因、打开所在目录（无则隐藏）。
- [ ] **Step 2: `make dev` 用 20 MiB 文件走一遍**。
- [ ] **Step 3: Commit**。

---

## M4 离线任务（2 个任务）

### Task 16: 任务路由

**Files:**
- Create: `internal/httpapi/routes_tasks.go`、测试；Modify `internal/pikpak/tasks.go`（`ListTasks`、`RetryTask`、`ClearTasks`）

**Interfaces:**
- Produces: `GET /api/tasks?phase=&page_token=`、`POST /api/tasks`、`POST /api/tasks/{id}/retry`、`DELETE /api/tasks?ids=&delete_files=`、`POST /api/tasks/clear`。
- 上游错误码 11（每日额度）→ 429 `daily_limit_reached`，不重试。

- [ ] **Step 1: 测试**：创建带 `folder_type=DOWNLOAD`；删除透传 `delete_files`；错误码 11 映射。
- [ ] **Step 2–4: 红 → 实现 → 绿**。
- [ ] **Step 5: Commit**。

### Task 17: 任务页 UI

**Files:**
- `web/src/pages/Tasks.tsx`

- [ ] **Step 1: 实现**：粘贴链接/磁力新建、进度、重试、删除（勾选"同时删除文件"）、清理已完成；页面可见时 3 秒轮询，切走即停。
- [ ] **Step 2: `make dev` 手工验证**（用 1 MB 直链，任务可能停在 RUNNING，属已知账号限制）。
- [ ] **Step 3: Commit**。

---

## M5 播放（3 个任务）

### Task 18: 播放信息与令牌

**Files:**
- Create: `internal/httpapi/routes_play.go`、`internal/playtoken/playtoken.go` + 测试

**Interfaces:**
- Produces: `GET /api/play/{id}` → `{stream_url,mime,size}`；令牌 `base64url(HMAC-SHA256(secret,"uid|file|exp"))`，默认 6 小时；`GET /api/stream/{id}?t=` 校验 uid 与过期。

- [ ] **Step 1: 测试**：篡改 uid/exp 拒绝；过期 401；`stream_url` 不含上游签名地址。
- [ ] **Step 2–4: 红 → 实现 → 绿**。
- [ ] **Step 5: Commit**。

### Task 19: Range 代理

**Files:**
- Create: `internal/httpapi/stream_proxy.go` + 测试

**Interfaces:**
- Produces: 转发 `Range`/`If-Range`，回传 `206`/`Content-Range`/`Accept-Ranges`/`Content-Length`/`Content-Type`；只允许 GET/HEAD；签名地址缓存 10 分钟，403 时刷新一次。
- `play_mode=redirect`（设置）时改为 302 到签名地址；默认 `proxy`。

- [ ] **Step 1: 测试**：完整区间、开放区间、超出 EOF → 416、HEAD、上游 200（忽略 Range）→ 200。
- [ ] **Step 2–4: 红 → 实现 → 绿**。
- [ ] **Step 5: Commit**。

### Task 20: 播放器 UI

**Files:**
- `web/src/components/Player.tsx`、`web/src/pages/Files.tsx`（入口）

- [ ] **Step 1: 实现**：`<video controls>`、全屏、倍速、下载按钮；`canPlayType` 不支持时提示"浏览器不支持该编码，可下载后播放"（不转码）。
- [ ] **Step 2: `make dev` 用本地视频验证拖动**。
- [ ] **Step 3: Commit**。

---

## M6 打磨（3 个任务）

### Task 21: 设置页与诊断

**Files:**
- `web/src/pages/Settings.tsx`、`internal/httpapi/routes_diag.go` + 测试

- [ ] **Step 1: 实现设置页**（并发、重名策略、播放模式、语言、重新授权、解绑、关于）与诊断（版本、DB 大小、任务数、最近错误摘要）。
- [ ] **Step 2: 解绑二次确认文案说明会清除凭据与任务**。
- [ ] **Step 3: Commit**。

### Task 22: 错误提示与 i18n

**Files:**
- `web/src/i18n.ts`、`web/src/api.ts`（统一错误码 → 文案）

- [ ] **Step 1: 实现**：`pikpak_reauth_required` 常驻横幅；其余 toast + 可展开详情；中英文案表。
- [ ] **Step 2: Commit**。

### Task 23: 发布与真机清单

**Files:**
- Modify: `README.md`、`docs/install-m0.md`（改名/重写为 `docs/install.md`）、`manifest`（1.0.0）

- [ ] **Step 1: 真机验证清单**（需补验 4 项网关/Range 能力）：安装、启动、绑定、两用户隔离、授权目录写盘、Range 播放、升级保留数据。
- [ ] **Step 2: 打 tag `1.0.0`**，确认 CI 产物带版本号。
- [ ] **Step 3: Commit**。

---

## 执行顺序与门槛

1. 每个任务先写失败测试，再实现，再跑全量 `go test ./...`。
2. M1 结束：真机装一次，确认绑定 + 账号信息可用。
3. M2 结束：文件区可与官方 Web 对齐操作。
4. M3 结束：1 GB 双向传输成功，重启可恢复下载。
5. M4 结束：磁力任务可见（完成速度不作承诺）。
6. M5 结束：4 GB 视频拖动流畅，令牌不出后端。
7. M6 结束：无 P0/P1，文档更新，发布 1.0.0。