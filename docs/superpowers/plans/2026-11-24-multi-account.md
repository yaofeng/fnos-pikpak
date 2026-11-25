# 多 PikPak 账号绑定 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 一个飞牛用户绑定多个 PikPak 账号：我的文件/回收站按账号浏览（左上角切换器），最近/传输/离线任务合并显示，离线任务/转存随机选号并失败换号重试，已转存判定跨账号。

**Architecture:** 账号成为一等实体（`pikpak_accounts` 表，凭据重建为 `credentials2`），会话按 (uid, accountID) 管理；按账号接口加 `?account=`（缺省=第一个 active 账号），合并接口服务端并行拉取、`next_page_token` 编码各账号分页状态；playtoken 与传输任务行都带账号维度；Go 侧一次性数据迁移让老用户无感升级。

**Tech Stack:** Go（stdlib + modernc.org/sqlite，不新增依赖）、React 19 + TypeScript + vitest（web/，不新增依赖）。

**Spec:** `docs/superpowers/specs/2026-11-24-multi-account-design.md`（本计划从它出发；两者一起读）

## Global Constraints

- 不新增 Go 或 npm 依赖；只用 stdlib（`crypto/rand`、`encoding/base64`、`encoding/json`、`sync`）。
- **每个任务结束时 `go test ./...` 必须全绿**：共享签名一律"新增新方法、旧名留薄包装"，包装的拆除集中在 Task 8；中间提交状态不部署，只需可测。
- 身份永远来自 `fnos.IdentityFrom(r.Context())`，客户端传入的 `account` 参数只做归属校验，不当身份。
- 用户可见文案一律中文，风格与现有路由一致；`i18n.ts` 的 errorText 表为新错误码补 zh-CN + en 条目。
- SQLite 迁移只能追加到 `internal/store/migrate.go` 的 `migrations` 切片末尾；绝不改动已有条目。
- 单账号用户零感知：按账号接口在 `?account=` 缺省时回退默认账号；切换器只在账号 ≥2 时渲染。
- `users` 表停用不删表；`GET /api/account` 与 `POST /api/auth/rebind` 删除。
- 测试命令：后端 `go test ./...`（单包 `go test ./internal/xxx/ -run Name`）；前端 `cd web && npm test`、`npm run lint`、`npx tsc -b`。
- 提交信息用仓库现有 conventional 风格（`feat:`/`refactor:`/`fix:`），不加 scope；后端提交前 `gofmt -l internal cmd` 为空。

## Review Focus

规格暗示但任务测试之外最容易咬人的五类输入；每行已把钉住它的测试放进拥有该代码的任务：

1. **伪造/他人账号 id**：`?account=<别人的id>` 必须 404 `not_found`，绝不静默回退默认账号执行操作。→ Task 5 `TestAccountParamRejectsForeignID`。
2. **升级前发出的旧播放令牌**（payload 无账号段）：必须 401 `invalid_token`（播放器会重取链接），不能 500 或误读字段。→ Task 3 `TestVerifyRejectsLegacyFormats` + Task 8 `TestStreamUsesTokenAccount`。
3. **数据迁移中途失败**（账号行已建、data_version 未写）：再次启动必须幂等——按 uid 跳过已迁移行，不重复建账号、不丢凭据。→ Task 1 `TestDataMigrationIdempotent`。
4. **全部账号 reauth_required 或未绑定**时的随机操作与合并列表：结构化 401 `no_active_account`/`not_bound`，不是 500、不是空列表装成功。→ Task 4 `TestRandomAccountWithoutActiveFails`。
5. **跨账号已转存匹配的误报**：name+size 匹配只算 `kind=="drive#file"` 且 size 精确相等（`size==0` 沿用现状"按名匹配"语义；文件夹绝不匹配）。→ Task 7 `TestPackCopiesMatchRules`。

---

### Task 1: Store — 迁移、账号表、凭据重建、Go 数据迁移（纯增量，仓库保持绿）

**Files:**
- Modify: `internal/store/migrate.go`（追加迁移条目）
- Modify: `internal/store/store.go`（Credential.AccountID 字段、凭据读写落到 credentials2）
- Create: `internal/store/accounts.go`（PikPakAccount CRUD、NewAccountID、CancelAccountFlows）
- Create: `internal/store/migrate_data.go`（migrateData + legacyAAD + data_version）
- Modify: `internal/store/jobs.go`（DownloadJob/UploadJob 增加 AccountID）
- Modify: `internal/store/crypto.go`（`aadFor` 改名 `legacyAAD`，仅 migrate_data.go 引用）
- Test: `internal/store/accounts_test.go`、`internal/store/migrate_data_test.go`

**Interfaces:**
- Produces（后续任务依赖的精确签名）:
  - `type PikPakAccount struct { ID string; UID int; PikPakSub, DisplayName, Avatar, VIPType, VIPExpire, Status string; BoundAt, UpdatedAt time.Time }`
  - `(s *Store) PutAccount(ctx, a PikPakAccount) error`（首写定 bound_at，之后保留）
  - `(s *Store) GetAccount(ctx, uid int, id string) (PikPakAccount, error)`（不存在或不属该 uid → `ErrNotFound`）
  - `(s *Store) ListAccounts(ctx, uid int) ([]PikPakAccount, error)`（bound_at ASC）
  - `(s *Store) SetAccountStatus(ctx, uid int, id, status string) error`（行不存在则插入仅含 uid/status 的行）
  - `(s *Store) DeleteAccount(ctx, uid int, id string) error`
  - `(s *Store) AccountIDBySub(ctx, uid int, sub string) (string, error)`（无 → `ErrNotFound`）
  - `(s *Store) CancelAccountFlows(ctx, uid int, accountID string) error`（把该账号 pending 流置 cancelled；accountID=="" → 该 uid 全部）
  - `NewAccountID() (string, error)`（`crypto/rand` 8 字节 hex）
  - `Credential` 增加 `AccountID string` 字段；`PutCredential(ctx, Credential)` 签名不变但 upsert 改 `ON CONFLICT(account_id)`、AAD = `"acct:"+accountID+":"+field`（`AccountID==""` 的行也成立，供未迁移的调用方过渡）
  - **`GetCredential(ctx, uid int)` 签名不变**，语义改为"该 uid 的（当前唯一）凭据"：`SELECT account_id FROM credentials2 WHERE uid=? LIMIT 1` 再按账号加载（迁移后每 uid 至多一行，精确成立）
  - `DeleteCredential(ctx, uid int)` 签名不变，删该 uid 全部行
  - `DownloadJob`/`UploadJob` 增加 `AccountID string \`json:"account_id"\``，Put/Get/List/Queued* 全列携带
  - `migrateData()`（包内私有）：`Open()` 在 `migrate()` 后调用；守护键 `settings.data_version == "2"`
  - 本任务**不改**：`DeriveDeviceID`、`CancelPendingFlows`、`PutAuthFlow/GetAuthFlow`、`Flow`（它们在 Task 4 接入）

- [ ] **Step 1: 写失败测试**

`internal/store/migrate_data_test.go` 的 legacy fixture 内嵌**旧 schema 的 CREATE TABLE**（只含迁移会触碰的表），并在 `schema_migrations` 把 1..12 标记为已应用，让新迁移与 `migrateData` 接管。fixture SQL（原样使用）：

```sql
CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
CREATE TABLE users (uid INTEGER PRIMARY KEY, username TEXT NOT NULL DEFAULT '',
  pikpak_sub TEXT NOT NULL DEFAULT '', display_name TEXT NOT NULL DEFAULT '',
  avatar TEXT NOT NULL DEFAULT '', vip_type TEXT NOT NULL DEFAULT '',
  vip_expire TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active',
  bound_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '');
CREATE TABLE credentials (uid INTEGER PRIMARY KEY, auth_type TEXT NOT NULL DEFAULT 'oauth',
  client_id TEXT NOT NULL, access_token_enc BLOB NOT NULL, refresh_token_enc BLOB NOT NULL,
  access_expires_at TEXT NOT NULL, refresh_expires_at TEXT NOT NULL DEFAULT '',
  scopes TEXT NOT NULL DEFAULT '', device_id_enc BLOB NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE granted_folders (uid INTEGER NOT NULL, path TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '', granted_at TEXT NOT NULL, PRIMARY KEY (uid, path));
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE user_settings (uid INTEGER NOT NULL, key TEXT NOT NULL,
  value TEXT NOT NULL, PRIMARY KEY (uid, key));
CREATE TABLE download_jobs (id TEXT PRIMARY KEY, uid INTEGER NOT NULL, file_id TEXT NOT NULL,
  file_name TEXT NOT NULL, dest_dir TEXT NOT NULL, dest_path TEXT NOT NULL,
  size INTEGER NOT NULL DEFAULT 0, downloaded INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL,
  error_code TEXT NOT NULL DEFAULT '', error_message TEXT NOT NULL DEFAULT '',
  link_expire_at TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE upload_jobs (id TEXT PRIMARY KEY, uid INTEGER NOT NULL, local_path TEXT NOT NULL,
  parent_id TEXT NOT NULL, remote_name TEXT NOT NULL, size INTEGER NOT NULL DEFAULT 0,
  uploaded INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL, error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE auth_flows (id TEXT PRIMARY KEY, uid INTEGER NOT NULL, device_code_enc BLOB NOT NULL,
  user_code TEXT NOT NULL, verify_url TEXT NOT NULL, interval_sec INTEGER NOT NULL,
  expires_at TEXT NOT NULL, status TEXT NOT NULL, error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL);
CREATE TABLE share_favorites (id INTEGER PRIMARY KEY AUTOINCREMENT, uid INTEGER NOT NULL,
  share_id TEXT NOT NULL, title TEXT NOT NULL DEFAULT '', pass_code_enc BLOB NOT NULL DEFAULT x'',
  created_at TEXT NOT NULL, display_title TEXT NOT NULL DEFAULT '',
  owner_name TEXT NOT NULL DEFAULT '', owner_avatar TEXT NOT NULL DEFAULT '',
  share_created_at TEXT NOT NULL DEFAULT '', expire_at INTEGER NOT NULL DEFAULT 0,
  file_count INTEGER NOT NULL DEFAULT -1, total_size INTEGER NOT NULL DEFAULT 0,
  stats_at TEXT NOT NULL DEFAULT '', UNIQUE (uid, share_id));
CREATE TABLE video_favorites (id INTEGER PRIMARY KEY AUTOINCREMENT, uid INTEGER NOT NULL,
  share_id TEXT NOT NULL, file_id TEXT NOT NULL, name TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL DEFAULT 0, owner TEXT NOT NULL DEFAULT '',
  share_title TEXT NOT NULL DEFAULT '', display_name TEXT NOT NULL DEFAULT '',
  group_label TEXT NOT NULL DEFAULT '', restored_file_id TEXT NOT NULL DEFAULT '',
  pass_code_enc BLOB NOT NULL DEFAULT x'', created_at TEXT NOT NULL,
  UNIQUE (uid, share_id, file_id));
-- 已应用标记必须数齐当前 migrate.go 的全部既有条目（写本计划时为 16 条；执行前先
-- `grep -c` 核对实际条数并同步），本次追加的新迁移不在其中，由迁移器自己跑。
INSERT INTO schema_migrations(version, applied_at) VALUES
 (1,'2026-01-01'),(2,'2026-01-01'),(3,'2026-01-01'),(4,'2026-01-01'),(5,'2026-01-01'),
 (6,'2026-01-01'),(7,'2026-01-01'),(8,'2026-01-01'),(9,'2026-01-01'),(10,'2026-01-01'),
 (11,'2026-01-01'),(12,'2026-01-01'),(13,'2026-01-01'),(14,'2026-01-01'),(15,'2026-01-01'),
 (16,'2026-01-01');
```

测试逻辑（`t.TempDir()` 建 db/key 文件：先 `sql.Open("sqlite", …)` 手工执行 fixture SQL，并用 `Encrypt(key, legacyAAD(1001,"access_token"), []byte("at-old"))` 等插入 uid=1001 的凭据 + users 行（display_name="老账号"）+ 一条 download_job；然后 `store.Open(dbPath, keyPath)`）：

```go
func TestLegacyDataMigrates(t *testing.T) {
    // 断言：
    // ListAccounts(ctx, 1001) 恰 1 行，DisplayName=="老账号"，Status=="active"
    // GetCredential(ctx, 1001) 解出 AccessToken=="at-old"（新 AAD 可解）
    // 该 download_job 行读回 AccountID == 账号行 ID
    // GetSetting(ctx, "data_version") == "2"
}
func TestDataMigrationIdempotent(t *testing.T) {
    // Open → Close → 再 Open：ListAccounts 仍恰 1 行，凭据仍可解密
}
func TestFreshInstallHasNoAccounts(t *testing.T) {
    // 空库直接 Open：ListAccounts 空、migrateData 不报错、data_version=="2"
}
```

`internal/store/accounts_test.go`：

```go
func TestAccountCRUDAndIsolation(t *testing.T) {
    // PutAccount 两账号（1001 的 a、b；1002 的 c）
    // ListAccounts(1001) 按 bound_at 升序恰 [a,b]；GetAccount(1001, c.ID) → ErrNotFound
    // SetAccountStatus(1001, a.ID, "reauth_required") 后 GetAccount 反映
    // DeleteAccount(1001, a.ID) 后 ListAccounts 恰 [b]
    // AccountIDBySub(1001, b 的 sub) == b.ID；未知 sub → ErrNotFound
    // CancelAccountFlows(1001, "") 后 pending 流全部 cancelled
    // NewAccountID() 两次不同、16 位 hex
}
func TestCredentialPerAccount(t *testing.T) {
    // 同 uid 两账号各 PutCredential（AccountID 分别 a、b，AccessToken 不同）
    // AccountIDBySub→GetAccount 后，用账号 id 直接查 credentials2 解出各自 token（测试内 SQL 或辅助查询）
    // DeleteCredential(ctx, uid) 删两行
}
func TestJobsCarryAccount(t *testing.T) {
    // PutDownloadJob/PutUploadJob 带 AccountID，Get/List/Queued* 读回一致
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/store/ -run 'TestLegacyDataMigrates|TestDataMigrationIdempotent|TestFreshInstall|TestAccount|TestCredentialPerAccount|TestJobsCarryAccount' -v`
Expected: 编译失败（PikPakAccount/NewAccountID 等未定义）。

- [ ] **Step 3: 实现**

1. `migrate.go` 追加迁移条目（spec §5 SQL：`pikpak_accounts`+索引、`credentials2`、`download_jobs/upload_jobs/auth_flows` 加 `account_id TEXT NOT NULL DEFAULT ''`、`video_favorites` 加 `restored_account_id TEXT NOT NULL DEFAULT ''`）。
2. `crypto.go`：`aadFor` → `legacyAAD`。
3. `migrate_data.go`：`const dataVersionCurrent = "2"`；流程——查 `sqlite_master` 无 `credentials` 表 → `SetSetting("data_version","2")` 返回；否则遍历旧凭据行：`credentials2` 已有同 uid 行则跳过（幂等锚点），否则 `NewAccountID()` → 从 `users` 抄 display/vip/sub（无则空）`PutAccount`（Status active、BoundAt now）→ legacy AAD 解密 / `"acct:"+id` AAD 加密 → `PutCredential`（带 AccountID）→ `UPDATE download_jobs/upload_jobs SET account_id=? WHERE uid=? AND account_id=''` → 完成后 `DROP TABLE credentials` + `SetSetting`。`Open()` 里 `migrate()` 成功后调用。
4. `accounts.go`：上述 CRUD；`PutAccount` 的 `bound_at` 用 `CASE WHEN pikpak_accounts.bound_at='' THEN excluded.bound_at ELSE pikpak_accounts.bound_at END`（与 PutUser 同法）；`CancelAccountFlows`。
5. `store.go`：`Credential.AccountID`；`PutCredential/GetCredential/DeleteCredential` 落到 `credentials2`（Get 用 LIMIT 1 解析账号 id；Delete 按 uid 删全部）。
6. `jobs.go`：两个 struct 加 `AccountID`，INSERT/SELECT/Scan 补列。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./... && gofmt -l internal cmd`
Expected: 全部 PASS（pikpak/httpapi 包零改动所以仍绿），gofmt 空。

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): accounts table, per-account credentials and one-shot data migration"
```

---

### Task 2: pikpak — 会话按账号管理（新方法 + 旧签名薄包装）

**Files:**
- Modify: `internal/pikpak/auth.go`
- Test: `internal/pikpak/auth_test.go`

**Interfaces:**
- Consumes: Task 1 的 `PikPakAccount` CRUD、`AccountID` 凭据（`PutCredential` 的 Credential.AccountID）、`SetAccountStatus`、`CancelAccountFlows`。
- Produces:
  - **新方法**（账号维度，后续任务的主接口）:
    - `(s *SessionStore) GetAccountSession(ctx, uid int, accountID string) (*Session, error)`
    - `(s *SessionStore) ClientForAccount(ctx, uid int, accountID string) (*Client, error)`；`accountID==""` → 该 uid 默认账号（`ListAccounts` 里第一个 `active`，无 active 取第一行；空 → `ErrNotBound`）
    - `(s *SessionStore) InstallAccount(uid int, accountID string, cred store.Credential) *Session`
    - `(s *SessionStore) DropAccount(ctx, uid int, accountID string) error`（删该账号凭据 + `CancelAccountFlows` + disable 该 session）
    - `(s *SessionStore) HasAny(ctx, uid int) bool`
  - **旧名薄包装**（本任务把实现换到账号维度上，签名不变，httpapi 零改动）:
    - `Get(ctx, uid)` = `GetAccountSession(ctx, uid, "")`；`ClientFor(ctx, uid)` = `ClientForAccount(ctx, uid, "")`；`Install(uid, cred)` = `InstallAccount(uid, cred.AccountID, cred)`；`Drop(ctx, uid)` = 删该 uid 全部（遍历 `ListAccounts` 逐个 `DropAccount` + 兜底 `DeleteCredential(uid)`）——**这些包装在 Task 8 拆除**
  - 内部 maps/limiters 键 `"uid|accountID"`；`Session` 增加 `accountID` 字段（legacy 路径为 ""）
  - `refreshOnce` 持久化 `cred.AccountID = s.accountID`；`markReauth`：`accountID != ""` → `SetAccountStatus(uid, accountID, "reauth_required")`，否则维持写 users 行的现状

- [ ] **Step 1: 写失败测试**（沿用 auth_test.go 现有 fake-token-server 建法）

```go
func TestSessionStoreTwoAccountsIsolated(t *testing.T) {
    // 同一 uid InstallAccount 两个账号（不同 token）
    // ClientForAccount(uid, a) 换出的 token == a 的；b 同理
    // DropAccount(uid, a) 后：GetAccountSession(uid, a) → ErrNotBound；b 仍成功
}
func TestClientForEmptyMeansDefault(t *testing.T) {
    // 无账号 → ClientForAccount(uid, "") == ErrNotBound
    // 一个 reauth_required + 一个 active → 默认解析到 active 那个
}
func TestReauthMarksAccountRow(t *testing.T) {
    // fake 上游对 refresh 回 invalid_grant → Token() 报 ErrReauthRequired
    // 且 store.GetAccount(uid, acct.ID).Status == "reauth_required"
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/pikpak/ -run 'TestSessionStoreTwoAccounts|TestClientForEmpty|TestReauthMarks' -v`
Expected: 编译失败（新方法未定义）。

- [ ] **Step 3: 实现**：按 Interfaces 改造。既有单账号测试不动（走包装，行为等价）。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./...`
Expected: 全绿（包装保证 httpapi 不受影响）。

- [ ] **Step 5: Commit**

```bash
git add internal/pikpak/
git commit -m "feat(pikpak): account-scoped sessions with default-account fallback"
```

---

### Task 3: playtoken — 令牌带账号维度（调用点机械适配）

**Files:**
- Modify: `internal/playtoken/playtoken.go`
- Modify: `internal/httpapi/routes_play.go`、`routes_share.go`、`video_prepare.go`、`share_stream.go`（仅机械适配，账号接入在 Task 8）
- Test: `internal/playtoken/playtoken_test.go`

**Interfaces:**
- Produces:
  - `type Claims struct { UID int; AccountID string; FileID string; ShareID string; ExpiresAt time.Time }`
  - `Sign(secret []byte, uid int, accountID, fileID string, ttl time.Duration, now time.Time) string`，payload `"uid|acct|file|unix_expiry"`
  - `SignShare(secret []byte, uid int, accountID, shareID, fileID string, ttl time.Duration, now time.Time) string`，payload `"s|uid|acct|share|file|unix_expiry"`
  - `Verify`：按段数严格解析；旧格式（3 段 drive / 5 段 share）→ `ErrInvalid`

- [ ] **Step 1: 写失败测试**

```go
func TestSignVerifyRoundTrip(t *testing.T) {
    // Sign → Verify：UID/AccountID/FileID/ExpiresAt 全对
    // SignShare → Verify：ShareID 命中、AccountID 对
}
func TestVerifyRejectsLegacyFormats(t *testing.T) {
    // 手拼旧 payload "1001|file|9999999999" + 正确签名 → ErrInvalid
    // 手拼旧 share payload "s|1001|share|file|9999999999" + 正确签名 → ErrInvalid
}
func TestVerifyRejectsWrongSignature(t *testing.T) {
    // 正确 payload、换 secret 签名 → ErrInvalid
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/playtoken/ -v`
Expected: 编译失败（Sign 签名未变）。

- [ ] **Step 3: 实现**：playtoken 按 Interfaces 改；httpapi 四个文件的全部 `Sign/SignShare` 调用点机械传 `""`（注释注明 Task 8 接入真实账号）。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./... && gofmt -l internal`
Expected: 全绿。

- [ ] **Step 5: Commit**

```bash
git add internal/playtoken/ internal/httpapi/
git commit -m "feat(playtoken): bind stream tokens to a pikpak account"
```

---

### Task 4: httpapi — 账号解析助手、/api/accounts、/api/me、auth 路由多账号化

**Files:**
- Create: `internal/httpapi/accounts.go`（助手 + /api/accounts 路由）
- Modify: `internal/httpapi/routes_auth.go`（device 带 account_id、logout 按账号、删 rebind）
- Modify: `internal/httpapi/routes_account.go`（删 GET /api/account 与 rebind；meResponse 改造；accountCache 键改 accountID）
- Modify: `internal/store/store.go`（`DeriveDeviceID(accountID string)`——参数语义从 uid 改为 accountID，HMAC 消息 `"device:acct:"+accountID`；唯一调用方 routes_auth 在本任务同改）
- Modify: `internal/transfer/queue.go`（`runState` 增加 `accountID string` 字段，`reserve()` 暂填 `""`；新增 `CancelAccount(uid int, accountID string)`——按 `st.uid==uid` 且（`accountID==""` 或 `st.accountID==accountID`）过滤；Task 6 真实化 accountID）
- Modify: `cmd/pikpak/main.go`、`internal/store/store_test.go`、`internal/fnos/*`（DeriveDeviceID 既有测试适配）
- Test: `internal/httpapi/accounts_test.go`、既有 `routes_auth_test.go`/`routes_account_test.go` 适配

**Interfaces:**
- Consumes: Task 1/2 全部产出。
- Produces:
  - `type namedClient struct { Account store.PikPakAccount; C *pikpak.Client }`
  - `(deps Deps) resolveAccount(ctx, uid int, raw string) (store.PikPakAccount, error)`：raw=="" → 默认账号；非空 → `GetAccount`（`ErrNotFound` 原样上抛）
  - `accountClient(w, r, deps) (*pikpak.Client, store.PikPakAccount, bool)`：读 `?account=`；失败已写 401 `not_bound` / 404 `not_found` / 500，返回 false
  - `allAccountClients(w, r, deps) ([]namedClient, bool)`：全部 `status=='active'` 账号；一个都没有 → 401 `not_bound`
  - `randomAccount(w, r, deps) (namedClient, bool)`：无 active → 401 `no_active_account`「没有可用的 PikPak 账号，请重新授权或绑定」
  - `shuffledActives(accounts []store.PikPakAccount) []store.PikPakAccount`（`crypto/rand` 洗牌，Task 6/7/8 复用）
  - `GET /api/accounts` → `{"items":[{id, display_name, avatar, vip_type, vip_expire, status, quota_limit, quota_usage, error}]}`；快照复用 `accountCache`（键 accountID），每账号一个 goroutine 并行拉取（账号数是个位量级，不需要信号量），单账号上游失败 → 条目 `error` = `probe.Redact` 后消息、quota 为 0
  - `meResponse`：`bound = len(accounts)>0`；`status` = 有任一 `reauth_required` 即 `"reauth_required"`，否则 `active`/`unbound`；`display_name`/`vip_type` 取默认账号行；新增 `"accounts": len(accounts)`
  - `POST /api/auth/device` body 增加可选 `account_id`：有 → flow 落到该行（`GetAccount` 校验归属，无 → 404）；完成时授权到的 sub 与账号行不一致 → flow `error`「授权的账号与原账号不一致，请解绑后重新添加」；一致 → 凭据写回同 accountID、设备 id 沿用原凭据解密值、`PutAccount` 刷新 sub/display/vip 且 status 复位 active（bound_at 不变）
  - 新账号 flow 完成时：`AccountIDBySub(uid, sub)` 命中 → flow `error`「该 PikPak 账号已绑定」；否则建行 + `DeriveDeviceID(accountID)` + `InstallAccount`
  - `Flow.AccountID`（store 本任务加字段与列读写；`PutAuthFlow/GetAuthFlow` 补该列；`POST /api/auth/device` 创建 flow 时写入）
  - `POST /api/auth/logout` body `{"account_id":"…"}`：`DropAccount`、`DeleteAccount`、`DeleteAccountJobs`（本任务在 store 新增：`store.DeleteAccountJobs(ctx, uid, accountID)`，删两类任务表该账号行）、`Queue.CancelAccount`、`Accounts.drop(accountID)`；空 account_id → 400 `bad_request`「请指定要解绑的账号」
  - **删除** `GET /api/account`、`POST /api/auth/rebind`

- [ ] **Step 1: 写失败测试**

```go
func TestResolveAccountOwnership(t *testing.T) {
    // uid 1001 有账号 a；?account=a → OK
    // ?account=不存在 → 404 {"error":{"code":"not_found"}} 且返回 false
    // 无 ?account= → 默认账号
}
func TestRandomAccountWithoutActiveFails(t *testing.T) {
    // 唯一账号 reauth_required → randomAccount 写 401 no_active_account
    // 未绑定 → 同样 401 no_active_account
}
func TestShuffledActivesCoversAll(t *testing.T) {
    // 5 个账号洗牌 50 次：每次都是同一集合的排列（排序后相等）
}
func TestAccountsEndpointListsAll(t *testing.T) {
    // 两账号，其一上游不可达 → items 两行；可达行有 quota_limit；不可达行 error != ""
}
func TestLogoutSingleAccount(t *testing.T) {
    // 两账号各绑；logout {account_id: a} → a 的凭据/账号行/任务行消失，b 完好
    // a 的 pending 流 cancelled
}
func TestRebindFlowTargetsExistingRow(t *testing.T) {
    // POST /api/auth/device {"account_id": a} → 完成后凭据写回 a.ID
    // fake 上游返回不同 sub → flow.Status=="error" 且 error 含「不一致」
}
func TestNewBindRejectsDuplicateSub(t *testing.T) {
    // 已有 sub=S 的账号；新 flow 授权同 sub → flow error「已绑定」
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/httpapi/ -run 'TestResolveAccount|TestRandomAccount|TestShuffledActives|TestAccountsEndpoint|TestLogoutSingle|TestRebindFlow|TestNewBindRejects' -v`
Expected: 编译失败。

- [ ] **Step 3: 实现**：按 Interfaces 建 `accounts.go` 与路由改造。`meResponse` 的 display/vip 从默认账号取；`Flow.AccountID` 的 store 列读写一并完成。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./... && gofmt -l internal cmd`
Expected: 全绿。

- [ ] **Step 5: Commit**

```bash
git add internal/ cmd/
git commit -m "feat(httpapi): account helpers, /api/accounts and multi-account auth flows"
```

---

### Task 5: httpapi — 按账号浏览路由（我的文件 / 回收站 / 文件操作）

**Files:**
- Modify: `internal/httpapi/routes_files.go`（GET /api/files、GET /api/trash、POST /api/trash/empty；**删除 `boundClient`**）
- Modify: `internal/httpapi/routes_files_ops.go`（folders/rename/batch/star/unstar）
- Modify: 其余仍引用 `boundClient` 的路由（tasks/transfers/thumb/play/share）暂时改调 `accountClient`（语义等价：默认账号），Task 6/7/8 细化各自行为
- Test: `routes_files_test.go`、`routes_files_ops_test.go`、`routes_trash_test.go` 扩展

**Interfaces:**
- Consumes: Task 4 `accountClient`。
- Produces: 上述路由全部 `client, acct, ok := accountClient(w, r, deps)`；本任务结束 `grep -rn boundClient internal/` 为 0。

- [ ] **Step 1: 写失败测试**

```go
func TestFilesRouteFollowsAccountParam(t *testing.T) {
    // 两个账号、两个 fake 上游返回不同文件名集合
    // GET /api/files?account=a → 集合 A；account=b → 集合 B；缺省 → 集合 A
}
func TestAccountParamRejectsForeignID(t *testing.T) {
    // uid 1001 请求 ?account=<另一 uid 的账号 id> → 404 not_found，且两个 fake 上游零调用
}
func TestTrashAndOpsFollowAccount(t *testing.T) {
    // GET /api/trash、POST /api/trash/empty、POST /api/files/trash、POST /api/folders
    //   带 account= 时打到对应上游（fake server 断言收到请求）
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/httpapi/ -run 'TestFilesRouteFollows|TestAccountParamRejects|TestTrashAndOps' -v`
Expected: FAIL。

- [ ] **Step 3: 实现**：逐 handler 换 `accountClient`；无 `?account=` 的既有用例不改（默认兜底）。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./... && ! grep -rn boundClient internal/`
Expected: 全绿；grep 无输出。

- [ ] **Step 5: Commit**

```bash
git add internal/httpapi/
git commit -m "feat(httpapi): file browse routes accept an account parameter"
```

---

### Task 6: 合并列表（recent/tasks/downloads/uploads）+ 队列按账号执行

**Files:**
- Create: `internal/httpapi/merge.go`（page token 编解码、合并排序、盖章、失败分类）
- Modify: `internal/pikpak/files.go`、`internal/pikpak/tasks.go`（`File`/`Task` 增加 `AccountID string \`json:"account_id,omitempty"\``）
- Modify: `internal/httpapi/routes_files.go`（/api/recent 合并）
- Modify: `internal/httpapi/routes_tasks.go`（GET 合并；POST 随机+换号；retry/delete `?account=`；clear 循环）
- Modify: `internal/httpapi/routes_transfers.go`（POST /api/downloads 改 `items`；POST /api/uploads 带账号；GET 确认 AccountID 随行）
- Modify: `internal/transfer/queue.go`（`ClientProvider` 带 accountID；`runState.accountID` 真实化）
- Modify: `cmd/pikpak/main.go`（Queue.Clients 闭包签名）
- Test: `internal/httpapi/merge_test.go`、`routes_tasks_test.go`、`routes_transfers_test.go`、`internal/transfer/queue_test.go` 扩展

**Interfaces:**
- Consumes: Task 4 的 `allAccountClients`/`randomAccount`/`shuffledActives`/`namedClient`；Task 1 任务行 AccountID。
- Produces:
  - `encodePageToken(m map[string]string) string` / `decodePageToken(s string) (map[string]string, error)`：`base64.RawURLEncoding(JSON)`；空串 → 空 map 无错；非法 → error
  - `mergeFilePages(pages []([]pikpak.File), srcs []namedClient)`：按 `ModifiedTime` 降序稳定排序（解析失败排最前，与 `modifiedSortKey` 现状一致），每条盖章对应 `account_id`
  - `stampFiles(items []pikpak.File, acct namedClient)` / `stampTasks(items []pikpak.Task, acct namedClient)`
  - `isAccountLevelErr(err error) bool`：`*pikpak.APIError` 的 `Code==11`、`Status==429/5xx`、网络错误、以及（本包的）`errSpaceInsufficient` → true；`ErrPassCodeRequired/Wrong/ShareUnavailable`、其他 → false
  - `POST /api/tasks` 响应 `{"task":…, "account_id":"…", "account_name":"…"}`；洗牌逐账号 `CreateOffline`，账号级失败换下一个，耗尽 → 429 `all_accounts_failed`，message 形如「账号 A：原因；账号 B：原因」
  - `DELETE /api/tasks?ids=…&account=X`、`POST /api/tasks/{id}/retry?account=X`：无 account → 400 `bad_request`「请指定任务所属账号」（合并列表行必带 account_id，前端总是携带）
  - `POST /api/tasks/clear`：循环所有 active 账号，`deleted` 求和
  - `POST /api/downloads` body `{"items":[{"file_id":"…","account_id":"…"?}], "dest_dir"?, "conflict"?}`：条目缺 `account_id` 时取 `?account=`；逐条用其账号 `FileDetail` 建任务（行写各自 AccountID）；响应 `created`/`skipped` 形状不变
  - `POST /api/uploads`：`?account=` 指定目标账号（缺省默认），任务行写该账号
  - `ClientProvider = func(ctx context.Context, uid int, accountID string) (*pikpak.Client, error)`；`pendingJob`/`runState` 携带 accountID（任务行来）；`Queue.CancelAccount` 精确按 (uid, accountID) 取消

- [ ] **Step 1: 写失败测试**

`merge_test.go`：

```go
func TestPageTokenRoundTrip(t *testing.T) {
    // encode→decode 等值；decode("!!!!") → error；decode("") → 空 map 无错
}
func TestMergeFilePagesSortsModifiedDesc(t *testing.T) {
    // 两账号页交错输入 → 输出按 ModifiedTime 降序；解析失败时间排最前；每条带正确 account_id
}
func TestIsAccountLevelErr(t *testing.T) {
    // APIError{Code:11}、APIError{Status:429}、APIError{Status:500}、errSpaceInsufficient → true
    // ErrPassCodeRequired、ErrShareUnavailable、errors.New("x") → false
}
```

`routes_tasks_test.go`：

```go
func TestCreateTaskRotatesAccounts(t *testing.T) {
    // 两账号；第一个 fake 上游 CreateOffline 回 code 11，第二个成功
    // → 201，body.account_id == 第二个账号；上游 1 恰 1 次调用，上游 2 恰 1 次
}
func TestCreateTaskAllAccountsFail(t *testing.T) {
    // 两账号都回 code 11 → 429 all_accounts_failed，message 含两个账号名
}
func TestTaskMutationsRequireAccount(t *testing.T) {
    // DELETE /api/tasks?ids=x（无 account）→ 400 bad_request；带 account= → 打到对应上游
}
```

`routes_transfers_test.go`：

```go
func TestDownloadItemsCarryAccounts(t *testing.T) {
    // items 两条分别 a、b → 两个 fake 上游各收到对应 FileDetail；两条 job 行 account_id 正确
    // 条目缺 account_id 时落 ?account=
}
```

`queue_test.go`：

```go
func TestQueueUsesJobAccount(t *testing.T) {
    // fake Clients 记录每次调用的 accountID；两个不同账号的 queued job
    // → 各 worker 以自己行的 accountID 取客户端
}
func TestCancelAccountScoped(t *testing.T) {
    // a、b 各一个 running job；CancelAccount(uid, a) 只取消 a 的
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/httpapi/ ./internal/transfer/ -run 'TestPageToken|TestMergeFilePages|TestIsAccountLevel|TestCreateTask|TestTaskMutations|TestDownloadItems|TestQueueUsesJob|TestCancelAccount' -v`
Expected: 编译失败。

- [ ] **Step 3: 实现**

1. `merge.go` 四件套。
2. `/api/recent`：`allAccountClients` → 每账号 `Events` 拉一页（token map 没有的账号从头开始）→ stamp → 排序 → `next_page_token`（还有余页的账号写进 map；全耗尽则省略字段）；单账号失败跳过并加 `account_errors`。
3. `/api/tasks` GET 同法（不排序，账号顺序拼接，过滤逻辑不变）；POST 换号循环；retry/delete 校验 `?account=`；clear 循环。
4. `routes_transfers.go`：downloads 改 items（struct `{FileID, AccountID string}`，逐条解析账号：自带 → `resolveAccount` 校验归属；否则 `?account=`）；uploads 的 POST 用 `?account=` 的账号建任务行。
5. `queue.go` + `main.go` 闭包。
6. 前端旧 body 形状不兼容（同分支内 Task 10/11 一起交付，无需过渡兼容层）。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./... && gofmt -l internal cmd`
Expected: 全绿。

- [ ] **Step 5: Commit**

```bash
git add internal/ cmd/
git commit -m "feat: merged cross-account listings, rotating offline tasks, account-aware queue"
```

---

### Task 7: 分享路由 — 跨账号 packCopies、转存换号、会话带密码

**Files:**
- Modify: `internal/httpapi/share_session.go`（键加 accountID、存 passCode）
- Create: `internal/httpapi/pack_copies.go`
- Modify: `internal/httpapi/routes_share.go`（open/files/file 用默认账号；restore 换号；annotate 用 packCopies）
- Test: `routes_share_test.go`、`pack_copies_test.go`、`share_session_test.go`（新）

**Interfaces:**
- Consumes: Task 4 助手、Task 6 `isAccountLevelErr`。
- Produces:
  - `shareSessions.put(uid int, accountID, shareID, token, passCode string, now)` / `get(uid, accountID, shareID, now) (token, passCode string, ok bool)`；键 `uid|accountID|shareID`
  - `type packListing struct { AccountID string; Root, Pack []pikpak.File }`
  - `packCopies(ctx, deps, uid int) []packListing`：全部 active 账号并行各拉 root + `Pack From Shared`（`findFolder(root, packFromSharedName)`；无该文件夹 → Pack 空），单账号失败跳过；**不返回 error**
  - `findCopyAcross(listings []packListing, rememberedID, targetName, originalName string, size int64) (pikpak.File, string /*accountID*/, bool)`：id 匹配在该账号 root+Pack 内；name+size 匹配只在该账号 Pack 列表内、仅 `kind=="drive#file"` 且 `size` 精确相等（`size==0` 按名）；文件夹永不匹配
  - `openShareFor(w, r, deps, uid int, acct store.PikPakAccount, shareID, passCode string) (string, *pikpak.ShareInfo, bool)`：`ClientForAccount(uid, acct.ID).GetShare`，成功后 `put` 该账号 token+passCode；分享级错误已写响应
  - `POST /api/share/restore`：洗牌逐账号 `openShareFor` → `RestoreShare`；账号级失败换下一个；分享级失败立即原样返回；成功 → `{"ok":true,"account_id","account_name"}`；耗尽 → 429 `all_accounts_failed`
  - `GET /api/share*`（open/files/file）用默认账号；pass-code 会话读写带默认账号 id；open 成功时 `put` 存明文 passCode（query 传入或收藏带的）
  - `annotateShareFiles(ctx, deps, uid, shareID, files)`（不再收 client）：restored = `findCopyAcross(packCopies(...), "", f.Name, f.Name, f.Size)` 命中；favorited 逻辑不变

- [ ] **Step 1: 写失败测试**

```go
func TestShareSessionStoresPassCode(t *testing.T) {
    // put(uid, acct, share, "tok", "123", now) → get 取回两值
    // 不同 accountID 的同 share 互不可见；过期取不到
}
func TestPackCopiesMatchRules(t *testing.T) {
    // 账号 a 的 Pack 有 (name=X,size=100) 文件；账号 b 有同名 size=200 文件和一个 name=X 的 folder
    // findCopyAcross(..., "X", "X", 100) → 命中 a 的文件
    // size=0 → 按名命中 a 的文件，绝不命中 folder
    // rememberedID 指向 b 的 root 文件 → 返回 b
}
func TestRestoreRotatesAccounts(t *testing.T) {
    // 账号 a 的 fake 上游 RestoreShare 回 500 → 换 b 成功 → 200 且 body.account_id==b；a 恰被调 1 次
    // a 回 pass_code_wrong（分享级）→ 立即 400，b 零调用
}
func TestAnnotateUsesAnyAccount(t *testing.T) {
    // 默认账号 Pack 无副本、第二账号有 → items[0].restored == true
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/httpapi/ -run 'TestShareSession|TestPackCopies|TestRestoreRotates|TestAnnotateUses' -v`
Expected: 编译失败。

- [ ] **Step 3: 实现**：按 Interfaces 改造；`resolveShareTarget` 收藏路径的 `fav.PassCode` 与 tab 路径 query 密码都写进 `put`。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/httpapi/ -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/httpapi/
git commit -m "feat(httpapi): cross-account restored-copy detection and rotating share restores"
```

---

### Task 8: 完整播放/播放/缩略图/直传 + 旧包装拆除

**Files:**
- Modify: `internal/httpapi/video_prepare.go`（prepareVideo 换号 + 跨账号复用；preview 默认账号）
- Modify: `internal/httpapi/video_jobs.go`（`videoPrepareResult` 增加 `AccountID string \`json:"account_id"\``，job 结果携带）
- Modify: `internal/httpapi/routes_videos.go`、`routes_play.go`、`stream_proxy.go`、`share_stream.go`、`routes_thumb.go`、`routes_upload.go`、`routes_video_favorites.go`、`share_stats.go`、`probes_pikpak.go`
- Modify: `internal/store/video_favorites.go`（`SetVideoFavoriteRestoredID(ctx, uid, id, fileID, accountID)`）
- Modify: `internal/pikpak/auth.go`、`internal/store/store.go`（**拆除 Task 2/1 留下的旧包装**：`SessionStore.Get/ClientFor/Install/Drop` 四个旧名、`store.GetCredential(ctx, uid)` 单参版、`store.DeleteCredential(ctx, uid)` 单参版——残余调用点全部改走账号版；`probes_pikpak.go`/`share_stats.go`/`routes_upload.go` 直传探针用 `ClientForAccount(ctx, uid, "")`）
- Test: `video_prepare_test.go`、`routes_play_test.go`、`stream_proxy_test.go`、`routes_thumb_test.go`、`routes_video_favorites_test.go`、`internal/pikpak/auth_test.go` 适配

**Interfaces:**
- Consumes: Task 3 playtoken、Task 4 `shuffledActives`、Task 6 `isAccountLevelErr`、Task 7 `packCopies`/`findCopyAcross`/`openShareFor`。
- Produces:
  - `prepareVideo` 新流程（spec §10）：解析来源与 share link（默认账号，不变）→ `findCopyAcross(packCopies(...), restoredID, targetName, link.Name, link.Size)` 命中 → 用**副本账号**播放、回写 `SetVideoFavoriteRestoredID(..., fileID, accountID)` → 未命中 → `shuffledActives` 逐账号：`makeRoom`（账号级失败换下）→ `openShareFor(该账号)` → `RestoreShare` → `waitTask`；**restore 任务落定前**失败换下一个，落定后失败只报告；耗尽 → `errAllAccountsFailed`
  - `errAllAccountsFailed`：`writeVideoError` 映射 429 `all_accounts_failed`
  - playtoken 调用点接入真实账号：play=文件账号；preview=openShare 的账号（默认）；prepare 正片=副本/转存账号；`shareStream` 用 `claims.AccountID`
  - `(deps Deps) linkFor(ctx, uid int, accountID, fileID string, refresh bool)`；`linkCache` 键带 accountID；`proxyStream(w, r, uid, accountID, fileID)`
  - `GET /api/play/{id}?account=`、`GET /api/thumb?...&account=`（缺省默认）；`thumbCache` 键带 accountID
  - `markRestoredCopies`（视频收藏列表）用 `packCopies` 跨账号判定 `Restored`

- [ ] **Step 1: 写失败测试**

```go
func TestPrepareReusesAnyAccountCopy(t *testing.T) {
    // 账号 b 的 Pack 已有 name+size 匹配副本 → prepare 不调任何 RestoreShare
    // 返回 stream_url 令牌 Verify 后 AccountID==b；收藏行 restored_file_id/restored_account_id 指向 b
}
func TestPrepareRotatesOnInsufficientSpace(t *testing.T) {
    // 账号 a quota 满且清不动（fake 固定 usage）→ errSpaceInsufficient → 换 b 成功
    // 结果令牌 AccountID==b；结果 JSON 含 account_id==b
}
func TestPrepareFailsWhenAllExhausted(t *testing.T) {
    // 两账号都腾不出 → 429 all_accounts_failed
}
func TestStreamUsesTokenAccount(t *testing.T) {
    // 账号 b 的令牌请求 /api/stream/{fileID} → linkFor 查 b（fake 断言）
    // 旧格式令牌（Task 3 手拼）→ 401 invalid_token
}
func TestThumbFollowsAccountParam(t *testing.T) {
    // ?account=b → 打到 b 上游
}
func TestMarkRestoredUsesAnyAccount(t *testing.T) {
    // 副本只在第二账号 → 该收藏 Restored == true
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/httpapi/ -run 'TestPrepare|TestStreamUses|TestThumbFollows|TestMarkRestored' -v`
Expected: FAIL。

- [ ] **Step 3: 实现**：按 Interfaces 逐文件改造；`makeRoom`/`tempFolderID`/`purgeOldest` 已收 client 参数，签名不动。最后拆除旧包装（见 Files 清单），全仓编译必须绿。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./... && go vet ./... && gofmt -l internal cmd`
Expected: 全绿。

- [ ] **Step 5: Commit**

```bash
git add internal/
git commit -m "feat(httpapi): full-play rotation, account-bound streaming; drop legacy session wrappers"
```

---

### Task 9: 前端 — 账号模块、Context、App 壳、绑定页

**Files:**
- Create: `web/src/accounts.ts`、`web/src/AccountContext.tsx`
- Modify: `web/src/App.tsx`（Provider、额度合计、逐账号 reauth 横幅）、`web/src/pages/Bind.tsx`、`web/src/components/BindCard.tsx`（账号列表卡片）、`web/src/i18n.ts`
- Test: `web/src/accounts.test.ts`（Context 的可测逻辑拆成 accounts.ts 纯函数）

**Interfaces:**
- Consumes: 后端 Task 4 的 `/api/accounts`、`/api/me`、`/api/auth/*` 新形状。
- Produces:
  - `export type AccountInfo = { id: string; display_name: string; avatar: string; vip_type: string; vip_expire: string; status: string; quota_limit: number; quota_usage: number; error?: string }`
  - `sumQuota(items: AccountInfo[]): { usage: number; limit: number }`（`error` 条目不计入）
  - `worstStatus(items: AccountInfo[]): string`（任一 `reauth_required` 即它；全 active → `active`；空 → `unbound`）
  - `AccountContextValue = { accounts: AccountInfo[]; byId: Map<string, AccountInfo>; currentId: string; current?: AccountInfo; setCurrent(id: string): void; reload(): Promise<void> }`；`useAccounts(): AccountContextValue`；Provider 挂载与 `pikpak:account-changed` 时 `apiGetQuiet('accounts')`；`currentId` 失效（账号被解绑）回落第一个
  - `BindCard`：账号卡片（头像/名称/VIP/状态徽章/解绑/重新授权）+「添加账号」；解绑 `apiPost('auth/logout', {account_id})` 后 `reload()` + 派发 `pikpak:account-changed`；重新授权与添加都走 `POST /api/auth/device`，前者带 `{account_id}`；轮询沿用现有实现，完成后同样 reload + 派发
  - `i18n.ts` errorText 增加 `no_active_account`、`all_accounts_failed` 的 zh + en 条目

- [ ] **Step 1: 写失败测试**（`accounts.test.ts`）

```ts
describe('sumQuota', () => {
  it('跳过带 error 的账号', () => {
    expect(sumQuota([
      { ...base, quota_usage: 10, quota_limit: 100 },
      { ...base, id: 'b', error: 'x', quota_usage: 5, quota_limit: 50 },
    ])).toEqual({ usage: 10, limit: 100 })
  })
})
describe('worstStatus', () => {
  it('任一 reauth_required 即返回它', () => {
    expect(worstStatus([{ ...base, status: 'active' }, { ...base, id: 'b', status: 'reauth_required' }])).toBe('reauth_required')
    expect(worstStatus([])).toBe('unbound')
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd web && npm test -- accounts`
Expected: FAIL（模块不存在）。

- [ ] **Step 3: 实现**：`accounts.ts` → 通过；再实现 `AccountContext.tsx`、App、BindCard。App 额度胶囊数据源改为 context 的 `sumQuota`；「绑定」导航条件 `accounts.length === 0`；reauth 横幅逐账号列出：「账号 {display_name || id} 需要重新授权」。

- [ ] **Step 4: 运行确认通过**

Run: `cd web && npm test && npm run lint && npx tsc -b`
Expected: 全绿。

- [ ] **Step 5: Commit**

```bash
git add web/src/
git commit -m "feat(web): account context, combined quota pill and multi-account bind page"
```

---

### Task 10: 前端 — 我的文件/回收站切换器与账号化请求

**Files:**
- Create: `web/src/components/AccountSwitcher.tsx`
- Modify: `web/src/listCache.ts`、`web/src/pages/Files.tsx`、`web/src/pages/Trash.tsx`、`web/src/files.ts`、`web/src/downloads.ts`
- Test: `web/src/listCache.test.ts`、`web/src/downloads.test.ts`、`web/src/files.test.ts` 扩展

**Interfaces:**
- Consumes: Task 9 `useAccounts`。
- Produces:
  - `cacheKeys.files(acct: string, folderId: string, kind: string)` = `` `files:${acct}:${folderId}|${kind}` ``；`cacheKeys.trash(acct)`；`cacheKeys.recent()` = `'recent:merged'`（Task 11 的页面共用该函数，行为随之切换）
  - `AccountSwitcher({accounts, currentId, onChange})`：`accounts.length < 2` 返回 `null`；下拉选项 = display_name（空则 id 前 6 位）+ VIP 标记 + 个人用量（`formatSize`）+ `reauth_required` 标注「需重新授权」（仍可选）
  - `thumbPath(fileID: string, shareID?: string, account?: string)`：account 存在时追加 `account` 参数
  - `queueDownloads(items: {file_id: string; account_id: string}[])`：POST `downloads` body `{items}`；返回提示文案不变
  - Files/Trash 全部请求带账号（query 或 body items）；`current?.id` 变化的 useEffect 里：`resetCache()` + 清目录栈/选择 + 重新拉取

- [ ] **Step 1: 写失败测试**

```ts
// listCache.test.ts
it('cacheKeys 含账号段', () => {
  expect(cacheKeys.files('a1', 'root', 'all')).toBe('files:a1:root|all')
  expect(cacheKeys.trash('a1')).toBe('trash:a1')
  expect(cacheKeys.recent()).toBe('recent:merged')
})
// downloads.test.ts
it('queueDownloads 发送 items 数组', async () => {
  // mock fetch，断言 body 为 {"items":[{file_id,account_id}]}
})
// files.test.ts
it('thumbPath 追加 account', () => {
  expect(thumbPath('f1', undefined, 'a2')).toBe('thumb?file_id=f1&account=a2')
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd web && npm test -- listCache downloads files`
Expected: FAIL。

- [ ] **Step 3: 实现**：组件与两页面改造；上传（POST `uploads?account=`）、下载（items 带当前账号）、新建/重命名/移动/复制/删除/star 全部带账号参数。

- [ ] **Step 4: 运行确认通过**

Run: `cd web && npm test && npm run lint && npx tsc -b`
Expected: 全绿。

- [ ] **Step 5: Commit**

```bash
git add web/src/
git commit -m "feat(web): account switcher for files and trash, account-scoped requests"
```

---

### Task 11: 前端 — 合并页面（最近/离线任务/传输）与分享浏览器

**Files:**
- Modify: `web/src/pages/Recent.tsx`、`web/src/pages/Tasks.tsx`、`web/src/pages/Transfers.tsx`、`web/src/pages/ShareBrowser.tsx`
- Modify: `web/src/tasks.ts`、`web/src/files.ts`（`FileItem` 增加 `account_id?: string`）、`web/src/transfers.ts`（`TransferJob` 增加 `account_id?: string`）
- Modify: `web/src/components/FileTable.tsx`、`web/src/components/FileGrid.tsx`（可选 `accountOf?: (f: FileItem) => string` prop：名称行 muted 小字显示账号名）
- Test: `web/src/tasks.test.ts` 扩展

**Interfaces:**
- Consumes: Task 9 context、Task 6 后端合并响应、Task 8 prepare 响应的 `account_id`。
- Produces:
  - `groupByAccount(tasks: OfflineTask[]): { account_id: string; ids: string[] }[]`（保序去重；`OfflineTask` 增加 `account_id?: string`）
  - Tasks：新建成功提示用响应 `account_name`「已通过账号 X 创建离线任务」；删除按 `groupByAccount` 分组循环 `DELETE tasks?ids=…&account=…`；retry 带 `?account=`
  - Recent/Tasks/Transfers 每行显示账号名（`useAccounts().byId.get(account_id)`，未知 id 显示前 6 位）；Transfers 行操作不带账号参数（服务端从任务行取）
  - ShareBrowser：转存成功提示带响应 `account_name`（「已转存到账号 X」）；`download()` 收集 prepare 结果的 `{file_id, account_id}` 传 `queueDownloads`

- [ ] **Step 1: 写失败测试**（`tasks.test.ts`）

```ts
it('groupByAccount 保序分组', () => {
  expect(groupByAccount([
    { id: '1', account_id: 'a' }, { id: '2', account_id: 'b' }, { id: '3', account_id: 'a' },
  ])).toEqual([{ account_id: 'a', ids: ['1', '3'] }, { account_id: 'b', ids: ['2'] }])
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd web && npm test -- tasks`
Expected: FAIL。

- [ ] **Step 3: 实现**：按 Interfaces 改造各页面与类型。

- [ ] **Step 4: 运行确认通过**

Run: `cd web && npm test && npm run lint && npx tsc -b`
Expected: 全绿。

- [ ] **Step 5: Commit**

```bash
git add web/src/
git commit -m "feat(web): merged recent/tasks/transfers with account badges, share browser account notices"
```

---

### Task 12: 全量验证与构建产物

**Files:** 无代码修改（修复测试暴露的问题除外；修复走对应任务的测试重跑）。

- [ ] **Step 1: 后端全量**

Run: `go test ./... && go vet ./... && gofmt -l internal cmd`
Expected: 全 PASS，vet/gofmt 干净。

- [ ] **Step 2: 前端全量**

Run: `cd web && npm test && npm run lint && npx tsc -b && npm run build`
Expected: 全绿，`internal/webdist/dist/assets/` 更新。

- [ ] **Step 3: 构建 fpk 并复制产物**

```bash
make fpk
cp dist/pikpak-*-x86.fpk /Users/fengyao/remote/fn-private/
```
Expected: `dist/pikpak-<VERSION>-x86.fpk` 生成并出现在目标目录（版本号不变，覆盖同版本旧包）。

- [ ] **Step 4: dev 模式冒烟（真机项留给用户）**

Run: `make dev` 后浏览器打开 `127.0.0.1:8787`，按 spec §2 核对：
1. 单账号：无切换器、一切如常（反目标自检）；
2. 绑定第二个账号 → 切换器出现，切换后列表/缓存隔离；
3. 最近/传输/离线任务合并显示与账号徽章；
4. 新建离线任务提示命中的账号；
5. 分享完整播放：副本在任一账号即复用，否则转存进随机账号且提示账号名；
6. 旧数据升级：用 Task 1 fixture 思路人工造一份 `.data` 再启动，账号自动出现。
Expected: 全部符合；发现的问题按 systematic-debugging 处理后重跑对应任务测试。

- [ ] **Step 5: 汇报（如有修复则提交）**

```bash
git add -A && git commit -m "fix: multi-account smoke fixes"   # 仅当有修复
```

---

### Task 13: 前端页面状态保活（Activity）

**执行顺序：Task 11 完成后、Task 12 之前执行**（Task 12 的全量验证与构建产物顺延覆盖本任务；规格：`docs/superpowers/specs/2026-11-25-page-keepalive-design.md`）。

**Files:**
- Create: `web/src/keepAlive.ts`（纯逻辑：路径归属、visited 集合、页面 key）
- Modify: `web/src/App.tsx`（保活页移出 `<Routes>`、`<Activity>` 包装、visited 集合、账号 key）
- Test: `web/src/keepAlive.test.ts`

**Interfaces:**
- Consumes: Task 9 `useAccounts()` 的 `currentId`；既有 `listCache`（不改动）。
- Produces:
  - `activePath(pathname: string): string | null` — pathname 归属的保活页路径（`/files` 与 `/files/*` 前缀匹配 → `'/files'`；其余 8 页整路径相等；未知 → `null`）
  - `nextVisited(visited: ReadonlySet<string>, pathname: string): Set<string>` — 激活页加入集合；不可变更新（原 Set 不被突变；已 visited 则返回原集）
  - `pageKey(page: string, accountID: string): string` — `page + '|' + accountID`；切账号换 key 即重挂载重置 UI 状态
  - `KEEP_ALIVE_PATHS: readonly string[]` — 9 页清单（`/files`、`/recent`、`/tasks`、`/transfers`、`/trash`、`/favorites`、`/video-favorites`、`/search`、`/settings`）
- 全局约束：不新增 npm 依赖（`<Activity>` 是 react 19 内置导出）；`npm test`、`npm run lint`、`npx tsc -b`、`go test ./...` 全绿。

- [ ] **Step 1: 写失败测试**（`web/src/keepAlive.test.ts`）

```ts
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
```

- [ ] **Step 2: 运行确认失败**

Run: `cd web && npx vitest run keepAlive`
Expected: FAIL（模块不存在）。

- [ ] **Step 3: 实现 `keepAlive.ts` 四件套**

- [ ] **Step 4: App.tsx 改造**

1. 9 个保活页组件移出 `<Routes>`，按 `activePath(location.pathname)` 判定 `<Activity mode="visible"|"hidden">`；每个保活页 `key={pageKey(page, currentId)}`（Task 9 `useAccounts()`）。
2. visited 集合（useState）：location 变化的 effect 里 `setVisited(nextVisited(...))`；未 visited 的保活页不渲染（首屏只挂当前页，首次激活后才进入保活）。
3. `<Routes>` 为 9 个保活路径保留 `element={null}` 的占位路由（`/files/*` 用通配），使 catch-all 只捕真正未知路径；`/bind`、`/debug`（lazy+Suspense 不动）与 `"/"→"/files"` 重定向保持原样。
4. 只读确认（不逐页改代码）：Transfers/Tasks 轮询页 hidden 时 Effects 卸载、定时器暂停，回显重跑由 `listCache` 兜底。

- [ ] **Step 5: 运行确认通过**

Run: `cd web && npm test && npm run lint && npx tsc -b && cd .. && go test ./...`
Expected: 全绿。

- [ ] **Step 6: Commit**

```bash
git add web/src/
git commit -m "feat: keep page state alive across navigation"
```

---

## Self-Review 记录

- **Spec 覆盖**：§5→T1+T4(Flow/DeriveDeviceID)；§6→T2+T4；§7→T3+T8；§8 新增/按账号/合并/写操作→T4/T5/T6；§9→T4(shuffle)+T6(分类)+T8(prepare)；§10→T7+T8；§11→T6；§12→T9/T10/T11；§13 迁移→T1；§14 测试点分布各任务 Step 1。备份（share/video favorites）账号无关——spec §2 非目标，无任务。
- **步骤单一性**：每个实现步骤只决定一个文件群的一个行为；测试步骤给出决定性断言，其余用例以名字列出。
- **类型一致**：`namedClient`（T4 定义，T6/T7/T8 用）；`ClientProvider(ctx, uid, accountID)`（T6 定义，main.go 同任务适配）；`SetVideoFavoriteRestoredID(ctx, uid, id, fileID, accountID)`（T8 定义并使用）；`openShareFor`/`findCopyAcross`/`packCopies`（T7 定义，T8 用）；`isAccountLevelErr`（T6 定义，T7/T8 用）；`DeleteAccountJobs`（T4 定义，T4 logout 用）。
- **编译绿性**：T1 纯增量；T2 旧名薄包装；T3 机械传 `""`；T8 统一拆除包装——每个任务边界 `go test ./...` 都能绿。
- **Review Focus**：五条各有钉住测试（T5/T3+T8/T1/T4/T7）。
- **比例**：计划约 700 行 vs 规格 320 行；无成段实现代码，仅签名、测试断言与流程编号。
