# 多 PikPak 账号绑定 — 设计文档

日期：2026-11-24
状态：已与用户对齐方向，待实施
前置文档：`docs/superpowers/specs/2026-11-21-fnos-pikpak-design.md`（v1 设计，单账号）

## 1. 意图与动机

现在每个飞牛（fnOS）用户只能绑定一个 PikPak 账号。用户希望绑定 **1 个或多个** PikPak 账号，用来：

- 摊薄 PikPak 按账号计算的离线任务每日额度与空间压力（账号几乎总是满的，完整播放靠自动清理腾地方）；
- 把多个账号的资产当作一个整体来看（最近、传输、离线任务合并显示）；
- 同时保留按账号浏览的能力（我的文件、回收站）。

## 2. 目标 / 非目标 / 反目标

### 目标

1. 一个 fnOS 用户可绑定 ≥1 个 PikPak 账号；绑定页管理账号列表（逐个解绑、逐个重新授权、追加绑定）。
2. 「我的文件」「回收站」：按所选账号浏览；左上角（返回按钮之前）有账号下拉切换器；仅绑定 ≥2 个账号时显示。
3. 「最近」「传输」「离线任务」：合并所有账号的结果一并显示；合并列表的每一行标明所属账号。
4. 新建离线任务：随机选一个**可用**账号创建。
5. 转存（分享浏览器的「转存」按钮）：随机选一个可用账号。
6. 「完整播放」与分享「下载」（两者都先转存到 Pack From Shared）：
   - **已转存判定跨账号**：任何一个账号里有匹配副本就算已转存，直接复用那份副本；
   - 没有副本时随机选一个可用账号转存、腾空间、改名，全部发生在该账号内。
7. 上传（飞牛 → PikPak）的目标账号 = 「我的文件」当前选中的账号（用户已确认）。
8. 随机 = 纯随机（用户已确认），不按剩余额度加权。
9. **单账号用户零感知**：不显示切换器，所有接口在缺省时自动落到第一个可用账号。
10. 老用户升级：已绑定的账号自动迁移为账号列表的第一项，凭据、令牌、传输任务全部保留。

### 非目标

- 不做跨账号的文件移动/复制（PikPak 账号之间搬文件）。
- 不做按额度加权的账号选择。
- 分享收藏夹、视频收藏夹仍按 fnOS 用户存储，不按账号拆分。
- 不改变 fnOS 身份模型（网关头、ACL、授权目录）。
- 不做转码（维持 v1 范围）。

### 反目标（出现即失败）

- 单账号用户的现有使用路径发生任何可见变化（接口参数缺失必须回退到默认账号）。
- 升级后老用户需要重新绑定、丢失传输任务或收藏。
- 跨账号令牌混淆：A 账号的播放令牌能读到 B 账号的文件。

## 3. 约束

- 身份来自 fnOS 网关头（`fnos.IdentityFrom`），永不由客户端提供 uid。
- SQLite 单连接；迁移只能追加（`internal/store/migrate.go`），不能改已应用的迁移。
- 凭据静态加密（`internal/store/crypto.go`），AAD 目前绑定 uid——需要换成绑定账号 id，老数据要重加密。
- 播放/流媒体走签名令牌（`internal/playtoken`），令牌格式加入账号维度后旧链接失效；令牌本身 6 小时过期，影响可忽略。
- 包为未签名 fpk，用户自行安装；升级走 `upgrade_callback`，数据库在启动时迁移，必须在 HTTP 服务前完成。
- PikPak 按账号限流：`SessionStore.limiter` 目前按 uid 分桶，改为按账号分桶后天然正确。

## 4. 方案对比

### 方案 A：账号成为一等实体（选定）

新表 `pikpak_accounts`，凭据、任务、令牌都挂账号 id；会话按 (uid, account) 管理；按账号接口加 `?account=` 参数。

- 优点：语义清晰；随机选择、跨账号检查、合并列表都有明确的账号集合可用；单账号路径可以完全向后兼容。
- 缺点：改动面大——~45 处 `boundClient` 调用点、两个任务表、playtoken、前端缓存键都要动。

### 方案 B：影子 uid（每个账号伪造一个内部 uid）

复用现有按 uid 隔离的全套代码，账号只是 uid 的别名。

- 优点：后端改动最小。
- 缺点：影子 uid 会泄漏进 fnOS 授权目录、设置、收藏等一切按 uid 的语义里，ACL 检查和 granted_folders 要逐处打补丁；"合并"要按真实 uid 反查聚合，比方案 A 更绕。否决。

### 方案 C：只做聚合，不做按账号浏览

所有页面都合并显示。

- 缺点：用户明确要求「我的文件」「回收站」按账号浏览与切换。否决。

## 5. 数据模型

### 新表与变更（SQL 迁移，追加一条）

```sql
CREATE TABLE pikpak_accounts (
  id           TEXT PRIMARY KEY,          -- 16 hex 随机，绑定流程发起时生成，永久不变
  uid          INTEGER NOT NULL,
  pikpak_sub   TEXT NOT NULL DEFAULT '',
  display_name TEXT NOT NULL DEFAULT '',
  avatar       TEXT NOT NULL DEFAULT '',
  vip_type     TEXT NOT NULL DEFAULT '',
  vip_expire   TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL DEFAULT 'active',   -- active | reauth_required | disabled
  bound_at     TEXT NOT NULL DEFAULT '',
  updated_at   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_pikpak_accounts_uid ON pikpak_accounts(uid, bound_at);

-- 凭据表按账号重建（PK 从 uid 换成 account_id，uid 保留用于归属校验）
CREATE TABLE credentials2 (
  account_id         TEXT PRIMARY KEY,
  uid                INTEGER NOT NULL,
  auth_type          TEXT NOT NULL DEFAULT 'oauth',
  client_id          TEXT NOT NULL,
  access_token_enc   BLOB NOT NULL,
  refresh_token_enc  BLOB NOT NULL,
  access_expires_at  TEXT NOT NULL,
  refresh_expires_at TEXT NOT NULL DEFAULT '',
  scopes             TEXT NOT NULL DEFAULT '',
  device_id_enc      BLOB NOT NULL,
  updated_at         TEXT NOT NULL
);

ALTER TABLE download_jobs ADD COLUMN account_id TEXT NOT NULL DEFAULT '';
ALTER TABLE upload_jobs   ADD COLUMN account_id TEXT NOT NULL DEFAULT '';
ALTER TABLE auth_flows    ADD COLUMN account_id TEXT NOT NULL DEFAULT '';
ALTER TABLE video_favorites ADD COLUMN restored_account_id TEXT NOT NULL DEFAULT '';
```

`users` 表停用但暂不删（回滚保险）；后续版本再出清理迁移。

### Go 侧数据迁移（一次性，`settings.data_version = 2` 守护）

SQL 迁移之后、HTTP 服务之前执行（`store.Open` 内）：

1. 遍历旧 `credentials` 每行：生成账号 id，插入 `pikpak_accounts`（display/vip/sub 从 `users` 同 uid 行抄写，没有就留空）；
2. 用**旧 AAD（uid 制）**解密、**新 AAD（账号制）**重加密写入 `credentials2`；设备 id 原样保留（老账号不掉设备指纹）；
3. 回填该 uid 名下 `download_jobs` / `upload_jobs` 的 `account_id`；
4. 成功后 `DROP TABLE credentials`，`SET data_version = 2`。

幂等性：按行检查 `credentials2` 是否已有同 uid 记录；整体由 `data_version` 短路。AAD 双方案共存期间，`crypto.go` 保留旧的 `aadFor(uid, kind)` 供迁移读取。

### 账号 id 的生成

`POST /api/auth/device` 发起绑定时生成（`crypto/rand` 8 字节 hex），写进 `auth_flows.account_id`；`pollAuthFlow` 成功时据此建账号行 + 设备 id（`DeriveDeviceID` 的 HMAC 输入改为 `"device:acct:"+accountID`）+ 凭据。同一 uid 下若已有同 `pikpak_sub` 的账号，绑定完成时报错「该 PikPak 账号已绑定」。

### 默认账号

`defaultAccount(uid)` = 该 uid 的账号按 `bound_at` 升序第一个 `status='active'` 的行；没有 active 则取第一个。所有按账号接口在 `?account=` 缺省/非法时：缺省 → 默认账号；提供了但不属于该 uid → 404 `not_found`。

## 6. 会话层（`internal/pikpak/auth.go`）

- `SessionStore` 的 maps（sessions、limiters）键从 `uid` 改为 `uid|accountID`。
- `Get(ctx, uid, accountID)` / `ClientFor(ctx, uid, accountID)` / `Install(accountID, cred)` / `Drop(ctx, uid, accountID)`；新增 `Accounts(ctx, uid) ([]store.PikPakAccount, error)`、`DropAll(ctx, uid)` 备用。
- `Credential` 结构加 `AccountID` 字段；`aadFor` 换成账号制。
- `Session.markReauth` 写状态到 `pikpak_accounts.status`（按 accountID）。
- `SessionStore.Drop` 时取消该账号的 pending 授权流（`auth_flows.account_id`）。

`httpapi` 侧新增三个助手，替换 `boundClient`：

```go
// 按账号：解析 ?account=（缺省→默认账号），校验归属，401/404 语义与现在一致
func accountClient(w, r, deps) (*pikpak.Client, store.PikPakAccount, bool)
// 全部可用账号的客户端列表（合并接口用）
func allAccountClients(w, r, deps) ([]accountClient, bool)   // accountClient{ID, Name, Client}
// 随机可用账号；没有 active 账号 → 401 no_active_account
func randomAccountClient(w, r, deps) (*pikpak.Client, store.PikPakAccount, bool)
```

合并接口的并发：`min(len(accounts), 4)` 个 goroutine 并行拉取，单账号失败不拖垮整页——该账号的条目缺席，响应里加 `account_errors: [{account_id, code}]` 供前端提示。

## 7. playtoken 与流媒体

- `Claims` 加 `AccountID`；payload：驱动文件 `"uid|acct|file|exp"`，分享文件 `"s|uid|acct|share|file|exp"`。
- 签发方：驱动播放 = 文件所在账号（文件页选中账号 / 完整播放用的账号）；分享预览 = 打开分享用的账号；完整播放正片 = 副本所在账号。
- 校验方：`/api/stream/{id}`、`/api/share/stream/...` 用 claims 里的 (uid, accountID) 取客户端；旧格式令牌解析失败 → 401，前端重新取链接即可。
- `linkCache` 键加 accountID。

## 8. API 变更清单

### 新增

| 路由 | 说明 |
|---|---|
| `GET /api/accounts` | `{items:[{id, display_name, avatar, vip_type, vip_expire, status, quota_limit, quota_usage, bound_at}]}`；快照沿用 60s 缓存，按账号并行拉取，单账号失败在条目里带 `error` 而不是整体失败 |

`GET /api/account` 删除（内部应用无兼容负担）。`GET /api/me` 保持形状，`bound` = 任一账号存在；`status` = 所有账号状态里最坏的（有 reauth_required 即为它）；加 `accounts: number` 计数。顶栏横幅改为点名具体账号（「账号 X 需要重新授权」）。

### 按账号（加 `?account=<id>`，缺省 → 默认账号）

- `GET /api/files`、`POST /api/folders`、`PATCH /api/files/{id}`、`POST /api/files/{move|copy|trash|untrash|delete|star|unstar}`
- `GET /api/trash`、`POST /api/trash/empty`（回收站与我的文件一致，按账号）
- `POST /api/downloads`（下载 = 选中账号；见 §10 的 items 改造）、`POST /api/uploads`（上传 = 选中账号，用户已确认）
- `GET /api/play/{id}`、`GET /api/thumb/...`（缩略图跟文件页账号）
- `GET /api/share*` 打开/列表/单文件链接/预览：用**默认账号**（分享页没有切换器）；pass-code 会话键扩为 `(uid, accountID, shareID)`

### 合并（遍历该 uid 全部可用账号）

- `GET /api/recent`：各账号 events 并行拉一页，合并按 `modified_time` 降序稳定排序，条目加 `account_id`
- `GET /api/tasks`：合并，条目加 `account_id`
- `GET /api/downloads`、`GET /api/uploads`：本就是 uid 范围查询，补 `account_id` 字段即可（无需并行上游）
- `POST /api/tasks/clear`：循环对所有账号各清一次，汇总 `deleted`

**合并分页**：`next_page_token` = `base64url(JSON{accountID→pageToken})`，对前端不透明；下一页请求原样带回，服务端解码后继续各账号翻页，直到凑满或全部耗尽。

### 变更动作（写操作指定账号）

| 路由 | 账号来源 |
|---|---|
| `POST /api/tasks`（新建离线任务） | 随机可用账号；响应带 `account_id`/`account_name` 供提示 |
| `POST /api/tasks/{id}/retry`、`DELETE /api/tasks?ids=` | `?account=`（前端按行携带；批量删除按账号分组循环） |
| `POST /api/share/restore`（转存） | 随机可用账号；响应用该账号重新 `GetShare` 拿自己的 pass-code token，再 `RestoreShare`；响应带账号名 |
| `POST /api/videos/prepare`（完整播放） | 见 §10 |
| `POST /api/auth/logout` | 变为 `POST /api/auth/logout` + body `{account_id}` 解绑单个账号；解绑时取消该账号的传输任务与 pending 流，删除其任务记录（磁盘文件保留，语义不变） |

## 9. 随机选择策略

`randomAccountClient`：候选 = 该 uid 的 `status='active'` 账号；`crypto/rand` 均匀取一。候选为空（全部 reauth_required 或未绑定）→ `401 no_active_account`「没有可用的 PikPak 账号，请重新授权或绑定」。不按额度/用量加权（用户已确认）。

## 10. 跨账号转存判定与完整播放

### 已转存扫描（`packCopies`）

新助手：对该 uid 全部可用账号**并行**各拉一次 root + `Pack From Shared`（现有 `annotateShareFiles` 的逻辑推广到 N 账号），产出 `map[accountID]files`。两个消费方：

1. `annotateShareFiles`（分享列表 `restored` 徽章）：任一账号内按 name+size（或记忆 id）匹配即 `restored=true`；
2. `prepareVideo` 的复用检查。

成本：N 账号 × 2 次 ListAllFiles；账号数通常 ≤ 3~5，可接受；不做额外缓存（后续可加短 TTL）。

### prepareVideo 流程（改造后）

```
解析来源（收藏 or 分享会话，不变）
link := 打开分享所用账号取 GetShareFileLink        ← 预览与元数据不变
1. 扫描 packCopies（全部账号）找已有副本：
   命中 → 直接用副本所在账号播放（该账号令牌），更新收藏 restored_file_id/restored_account_id
2. 未命中 → 随机选账号 X：
   makeRoom(X) → RestoreShare（用 X 重新开分享拿 token）→ 等任务 → 找到新文件
   → 改名为收藏显示名 → 记录副本 → 用 X 的令牌播放
```

`video_favorites` 新增 `restored_account_id`；`restored_file_id` 语义改为"最近一次副本"（ID 提示仅用于加速查找，正确性靠实时扫描兜底——ID 在 PikPak 可能重发，扫描按 name+size 匹配的逻辑保持）。

### 分享「下载」多文件跨账号

`ShareBrowser.download` 现在收集 `file_id` 数组一次 POST。改为：`POST /api/downloads` 请求体从 `{file_ids: []}` 改为 `{items: [{file_id, account_id}]}`（prepare job 的结果里带账号）；服务端逐条按其账号取详情、建任务（任务行写各自 `account_id`）。冲突跳过/改名策略不变（都在 NAS 侧）。

## 11. 传输队列（`internal/transfer`）

- `ClientProvider` 签名 → `(ctx, uid, accountID) (*pikpak.Client, error)`；`pendingJob`/`runDownload`/`runUpload` 从任务行取 `account_id`。
- `Queue.CancelUser(uid)` → `CancelAccount(uid, accountID)`；解绑账号时调用。
- 公平调度的"每用户一轮"保持按 fnOS 用户（磁盘与带宽本来就共享，不按账号拆）。
- `store.DeleteUserJobs` 增加按账号过滤的变体供解绑使用。

## 12. 前端（`web/src`）

### 账号上下文

`App.tsx` 建 `AccountContext`：挂载时与 `pikpak:account-changed` 时拉 `/api/accounts`，暴露 `{accounts, byId, current, setCurrent, reload}`。`current` 默认 `accounts[0]`，仅存在于内存（刷新回落到第一个）。`/api/me` 的轮询逻辑不变；额度胶囊改为所有账号 `quota_usage` 求和 / `quota_limit` 求和。

### 切换器

`AccountSwitcher` 组件：下拉，选项 = 头像/名称 + VIP 标记 + 个人用量 + 状态（reauth_required 的账号置灰并标注）；只在 `accounts.length ≥ 2` 时渲染。位置：`Files`、`Trash` 工具栏最左（返回按钮之前）。切换 → `setCurrent` + `resetCache()` + 清空目录栈/选择并重新拉取。

### 缓存键（`listCache.ts`）

```
files: (acct, folderId, kind) => `files:${acct}:${folderId}|${kind}`
trash: (acct) => `trash:${acct}`
recent: () => 'recent:merged'
```

### 页面改造

- **Files**： consuming context 的 current；上传/下载/一切操作请求带 `?account=`（或 body 字段）；面包屑根显示「我的文件」。
- **Trash**：同 Files；`trash/empty` 带账号。
- **Recent / Tasks / Transfers**：不加切换器；行内加账号小徽章（名称，取 `byId`）；Tasks 新建成功的提示写明「已通过账号 X 创建」；Tasks 的重试/删除逐行带该行 `account_id`（删除多选按账号分组循环调用）；Transfers 行操作不需要账号参数（服务端从任务行取）。
- **Bind 页**：改为账号列表卡片（头像/名称/VIP/状态/解绑/重新授权）+「添加账号」按钮（现有设备码流程，支持多账号并存，多个流程同时 pending 互不干扰——flow 已按 id 隔离）。
- **ShareBrowser**：转存/完整播放成功提示带账号名；下载逻辑按 §10 传 items；其余不变（分享浏览用默认账号）。
- **downloads.ts**：`queueDownloads` 改为传 `items`。
- **i18n**：新增切换器、徽章、提示文案（zh-CN + en）。

### 通知事件

现有 `pikpak:account-changed` 事件语义保留（绑定/解绑后全量刷新）；账号切换不发这个事件（各页面自己响应 `setCurrent`）。

## 13. 升级与兼容

- fpk 升级 → 启动 → SQL 迁移（追加一条）→ Go 数据迁移 → 服务。老用户：一个账号，行为与升级前完全一致（无切换器、接口缺省参数即默认账号）。
- 升级瞬间正在播放的旧令牌失效 → 播放器重新取链接，用户感知为一瞬重载。
- 传输中的任务升级：`ResetRunning*Jobs` 逻辑不变，account_id 已在数据迁移里回填，重启后续传走对应账号。

## 14. 测试要点

- **store**：老库 fixture（credentials/users/jobs 有数据）→ 迁移后账号行、凭据可解密、任务回填正确；data_version 幂等（跑两遍）。
- **pikpak**：SessionStore 双账号并存、互不串令牌；Drop 单账号不影响另一个；per-account limiter。
- **httpapi**：`?account=` 归属校验（伪造他账号 id → 404）；缺省回退默认账号；合并接口的多账号 page_token 往返；随机选择（注入固定 rand）排除 reauth 账号；`packCopies` 任一账号命中即复用（不再触发 restore）；uploads/downloads 的 items 改造。
- **transfer**：任务行 account_id 驱动客户端选择；CancelAccount 只停该账号。
- **前端**（vitest）：context 默认值与切换重置；缓存键含账号；Tasks 行携带 account_id 的分组请求；queueDownloads 的 items 形状。

## 15. 留给构建者的决定

- 合并排序的平局细则（同 modified_time 时的次级键）。
- 合并接口每账号每页拉取量的取舍（默认 100/账号）。
- `/api/accounts` 快照缓存的分账号失败重试节奏。
- 测试 fixture 的组织方式（沿用现有 `_test.go` 风格）。
- AccountSwitcher 的具体样式细节（遵循现有 .icon-btn / 下拉样式语言）。
