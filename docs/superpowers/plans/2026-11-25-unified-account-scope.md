# 统一账号作用域（v2.1.0）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 五个内容页统一账号下拉框（固定「-全部帐号-」合并视图），服务端并行列目录合并、子目录按同名路径跨账号合并、合并行悬浮提示带「@帐号名」。

**Architecture:** 复用 merge.go 的合并基建（allAccountClients / 每账号并行拉取 / page-token 信封 / account_errors / 行账号戳）。后端每个列表路由在 `?account=all` 哨兵处分流到合并路径，单账号路径逐字节不动；子目录同名解析走新的 pathresolve.go（名字逐段精确匹配 + 5 分钟缓存）。前端 AccountContext 增加 scope，Files/Trash 的栈与请求按 scope 分支，行内操作按行上 `account_id` 执行。

**Tech Stack:** Go stdlib（net/http、sync）、React 19 + vitest（node 环境，无 DOM 测试）、SQLite store。

**Spec:** docs/superpowers/specs/2026-11-25-unified-account-scope-design.md

## Global Constraints

- 身份永远来自 `fnos.IdentityFrom`；`?account=` 只决定作用域。
- 不新增任何 Go/npm 依赖。
- 新增文案全部进 `web/src/i18n.ts`（zh-CN + en），无硬编码中文。
- 版本 2.0.0 → 2.1.0：`Makefile` `VERSION ?= 2.1.0`、manifest `version=2.1.0`、release `v2.1.0`。
- 每个任务结束：`go test ./...` 或 `npm test` 全绿；最终 `npx oxlint`（web/src）警告数不多于基线 22。
- 合并视图禁用：新建文件夹、上传、拖拽移动/复制、清空回收站。
- 单账号请求串与 2.0.0 行为逐字节一致（现有测试不许改语义）。

## Review Focus

1. **同路径解析串账号**：段名必须精确匹配且 `kind=drive#folder`；解析缓存键必须含 uid + 段的 JSON（防 `/` 名字歧义与跨用户串目录）→ Task 3 的 `TestResolveFolderPathExactMatch`、`TestPathCacheKeyIncludesUIDAndSegments`。
2. **`all` 哨兵漏改**：五个路由各自一条 handler 测试证明 `account=all` 不走 accountForParam（否则 404「账号不存在」）→ Task 2 `TestFilesRouteMergedAll`、Task 5 `TestTrashRouteMergedAll`、Task 6 `TestTasksRouteSingleAccountParam`、Task 7 `TestRecentRouteSingleAccountParam`（transfers 无后端改动，前端过滤在 Task 12 验证）。
3. **单账号漂移**：现有全部测试保持绿即证明；任何任务不许改单账号请求构造 → 每任务的全量测试步骤。
4. **合并分页重复行**：pageDone 哨兵在 files/trash 合并模式下防止已尽账号重启首页 → Task 2 `TestFilesMergedPaginationNoRepeat`、Task 5 同型 `TestTrashMergedPaginationNoRepeat`。
5. **合并视图禁用矩阵**：scope='all' 时新建/上传/移动/复制/清空全部不可达（disabled 且事件守护）→ Task 10 的前端逻辑测试 `deleteFlow`/组件级纯函数 + 冒烟清单条目。

---

### Task 1: pikpak.File.AccountName 字段 + stampFiles 打名戳

**Files:**
- Modify: `internal/pikpak/files.go`（File struct，AccountID 字段旁）
- Modify: `internal/httpapi/merge.go:87-95`（stampFiles）
- Test: `internal/httpapi/merge_test.go`

**Interfaces:**
- Consumes: 现有 `stampFiles(items []pikpak.File, acct namedClient) []pikpak.File`、`namedClient{Account store.PikPakAccount; C *pikpak.Client}`（Account.DisplayName 有值）。
- Produces: `pikpak.File.AccountName string json:"account_name,omitempty"`；`stampFiles` 同时写 `AccountID` 与 `AccountName`。Task 2/5/9/11 依赖行上两戳齐备。

- [ ] **Step 1: 写失败测试**（merge_test.go 追加）

```go
func TestStampFilesCarriesAccountName(t *testing.T) {
	nc := namedClient{Account: store.PikPakAccount{ID: "acct-a", DisplayName: "主号"}}
	items := []pikpak.File{{ID: "f1"}}
	out := stampFiles(items, nc)
	if out[0].AccountID != "acct-a" || out[0].AccountName != "主号" {
		t.Fatalf("stamps = %q/%q", out[0].AccountID, out[0].AccountName)
	}
	// 空 id 的 legacy 账号保持不打戳
	keep := []pikpak.File{{ID: "f2"}}
	out2 := stampFiles(keep, namedClient{})
	if out2[0].AccountID != "" || out2[0].AccountName != "" {
		t.Fatalf("legacy account must stay unstamped")
	}
}
```

- [ ] **Step 2: 跑到红**：`go test ./internal/httpapi/ -run TestStampFilesCarriesAccountName` → FAIL（AccountName 未定义/未赋值）。
- [ ] **Step 3: 实现**：File struct 在 `AccountID` 后加 `AccountName string \`json:"account_name,omitempty"\``（注释：合并列表打的展示名戳，PikPak 不发送）；stampFiles 循环里 `items[i].AccountName = acct.Account.DisplayName`。
- [ ] **Step 4: 跑到绿**：`go test ./internal/httpapi/ ./internal/pikpak/`。
- [ ] **Step 5: 提交**：`git commit -m "feat: stamp merged file rows with the account display name"`

### Task 2: `GET /api/files?account=all` 合并根目录

**Files:**
- Modify: `internal/httpapi/merge.go`（新增 fetchListPages）
- Modify: `internal/httpapi/routes_files.go`（filesResponse 增加 AccountErrors；GET /api/files 顶部 all 分流）
- Test: `internal/httpapi/routes_files_test.go`

**Interfaces:**
- Consumes: `allAccountClients`、`fetchFilePages` 的并行模式、`mergeFilePages`、`nextPageToken`、`accountErrorList`、`pageDone`。
- Produces:
  - `fetchListPages(ctx context.Context, clients []namedClient, parentIDs []string, filters string, limit int, tokens map[string]string) (pages [][]pikpak.File, nexts []string, failures []error)` — 每账号并行 `ListFiles(ctx, parentIDs[i], ListOptions{PageToken: tokens[id], Limit: limit, Filters: filters})`，`tokens[id]==pageDone` 跳过。
  - filesResponse 新增 `AccountErrors []accountError json:"account_errors,omitempty"`。
  - 合并响应复用 `recentResponse` 的信封形状（Items/next_page_token/account_errors）。

- [ ] **Step 1: 写失败测试**（routes_files_test.go 追加；bearer 分流假上游照 `TestFilesRouteFollowsAccountParam`）

```go
func TestFilesRouteMergedAll(t *testing.T) {
	// 假上游按 bearer 回答 a.mkv / b.mkv（同 Task 既有模式）
	// GET /api/files?account=all → items 含两行，account_id/account_name 各归其主
	rec, body := getJSON(t, h, "/api/files?account=all", "1000")
	// 断言：len(items)==2；每行 account_id ∈ {acct-a, acct-b} 且 account_name=="账号"+id
	// 断言：response 里没有 account_errors 键
}
func TestFilesMergedPaginationNoRepeat(t *testing.T) {
	// 假上游第一次列目录返回 next_page_token="p2"，第二次返回空 token
	// 第一页 ?account=all&page_token= → next_page_token 非空
	// 第二页 ?account=all&page_token=<第一页的> → 只含第二页行
	// 第三页带同样的 token → items 为空、next_page_token 缺省（全部 pageDone）
}
func TestFilesMergedAccountErrorIsolated(t *testing.T) {
	// acct-b 的 bearer 回 500：acct-a 的行照常返回，account_errors 恰一条且
	// account_id=="acct-b"、error 非空
}
```

- [ ] **Step 2: 跑到红**：`go test ./internal/httpapi/ -run 'TestFilesRouteMergedAll|TestFilesMergedPagination|TestFilesMergedAccountError'` → FAIL（account=all 404 或缺字段）。
- [ ] **Step 3: 实现**：
  - merge.go 加 `fetchListPages`（照 fetchFilePages 逐行仿写，goroutine 里调 `nc.C.ListFiles`，`parentIDs[i]` 各自传）。
  - routes_files.go：GET /api/files handler 开头 `if r.URL.Query().Get("account") == "all" { handleMergedFiles(...) ; return }`；`handleMergedFiles` 内：现有 limit/kind/filters 逻辑提取共用（单账号路径不动，把 limit 与 filters 的构造提成 handler 内局部函数或直接在合并分支重算同一段代码，两处保持一致的校验文案）；`segs := r.URL.Query()["seg"]`，根目录时全传 `""`（Task 4 接管 segs）；`fetchListPages` → `mergeFilePages` → q 过滤（`q_local: true`）→ `writeJSON`。
  - filesResponse 加 `AccountErrors []accountError \`json:"account_errors,omitempty"\``。
- [ ] **Step 4: 跑到绿**：`go test ./internal/httpapi/`，随后 `go test ./...`。
- [ ] **Step 5: 提交**：`git commit -m "feat: merged all-accounts file listing under account=all"`

### Task 3: 同名路径解析器 + 缓存

**Files:**
- Create: `internal/httpapi/pathresolve.go`
- Test: `internal/httpapi/pathresolve_test.go`

**Interfaces:**
- Consumes: `namedClient`、`pikpak.Client.ListFiles`、`pikpak.ListOptions`。
- Produces（Task 4 消费）:

```go
// folder-id resolver: walk each account's tree segment by segment, exact
// name match on drive#folder only.
var errPathMissing = errors.New("path missing on this account")

func resolveFolderPath(ctx context.Context, uid string, nc namedClient, segs []string, cache *pathCache) (string, error)

type pathCache struct{ /* mu sync.Mutex; entries map[string]pathEntry; ttl 5*time.Minute */ }
func newPathCache() *pathCache
```

- 段过滤条件：`{"trashed":{"eq":false},"kind":{"eq":"drive#folder"}}`；匹配 `f.Kind == "drive#folder" && f.Name == seg` 精确相等。
- 缓存键：`uid + "\x00" + nc.Account.ID + "\x00" + string(json.Marshal(segs))`；命中且未过期直接返回 folderID。
- 任何上游错误 → 包成 `errPathMissing` 语义（调用方按"该账号缺席"跳过；resolver 返回的 error 不区分原因）。

- [ ] **Step 1: 写失败测试**

```go
func TestResolveFolderPathExactMatch(t *testing.T) {
	// 假上游根目录回答 [{id:"root1",kind:"drive#folder",name:"电影"},
	//   {id:"decoy",kind:"drive#folder",name:"电影续"},   ← 前缀不做匹配
	//   {id:"filetrap",kind:"drive#file",name:"电影"}]    ← 文件不做匹配
	// 第二层回答 [{id:"leaf",kind:"drive#folder",name:"动作"}]
	// resolveFolderPath(ctx,"1000",nc,[]string{"电影","动作"},cache)=="leaf"
}
func TestResolveFolderPathMissingSegment(t *testing.T) {
	// 第二层没有"动作" → errPathMissing
}
func TestPathCacheKeyIncludesUIDAndSegments(t *testing.T) {
	// 同 segs 不同 uid 命中不同条目：uid 1000 解析后，uid 1001 仍发起上游请求
	// （假上游计数请求次数）；["a/b"] 与 ["a","b"] 是不同键（计数两次）
}
func TestPathCacheHitsWithinTTL(t *testing.T) {
	// 同 uid 同 segs 第二次解析 → 上游计数不增长
}
```

- [ ] **Step 2: 跑到红**：`go test ./internal/httpapi/ -run 'TestResolveFolder|TestPathCache'` → FAIL（未定义）。
- [ ] **Step 3: 实现** pathresolve.go（walk + cache，单 mutex）。
- [ ] **Step 4: 跑到绿**：`go test ./internal/httpapi/`。
- [ ] **Step 5: 提交**：`git commit -m "feat: same-path folder resolver with per-account cache"`

### Task 4: 合并文件列表接 seg 子目录

**Files:**
- Modify: `internal/httpapi/routes_files.go`（handleMergedFiles 的 segs 分支）
- Test: `internal/httpapi/routes_files_test.go`

**Interfaces:**
- Consumes: Task 2 `fetchListPages`、Task 3 `resolveFolderPath`/`newPathCache`/`errPathMissing`。
- Produces: `GET /api/files?account=all&seg=电影&seg=动作` 返回各账号该同名路径下内容的合并；解析失败的账号不出现在 items 里、不计 account_errors。Deps 需要挂一个共享 `*pathCache`（Deps 结构加字段 `PathCache *pathCache`，NewMux 里 `if deps.PathCache == nil { deps.PathCache = newPathCache() }`）。

- [ ] **Step 1: 写失败测试**

```go
func TestFilesMergedSamePathSubdirectory(t *testing.T) {
	// acct-a 根目录有"电影"；acct-b 根目录没有
	// ?account=all&seg=电影 → 只含 acct-a 的电影子目录内容（两行各带两戳）
	// acct-b 在结果里零行、无 account_errors
}
func TestFilesMergedSubdirectoryMissingForAll(t *testing.T) {
	// 两账号都没有"电影" → items 空数组、HTTP 200
}
func TestFilesMergedSubdirectoryCacheHit(t *testing.T) {
	// 同 segs 第二次请求 → 假上游的"根目录"请求计数不增长（解析走查被缓存，
	// 子目录列表照常发生）
}
```

- [ ] **Step 2: 跑到红**。`- [ ] **Step 3: 实现**`：handleMergedFiles 里 `if len(segs) > 0` 对每个 client 调 resolveFolderPath，`errPathMissing` → 该账号 parentID 记 `""` 且其 token 预置 pageDone；否则 fetchListPages。**Step 4: 跑到绿** `go test ./...`。**Step 5: 提交**：`git commit -m "feat: merged file listing merges same-path subdirectories"`。

### Task 5: `GET /api/trash?account=all` 合并

**Files:**
- Modify: `internal/httpapi/routes_files.go`（GET /api/trash all 分流）
- Test: `internal/httpapi/routes_trash_test.go`（不存在则新建，遵循现有 files 测试的假上游模式；trashFilters 常量复用）

**Interfaces:**
- Consumes: Task 2 `fetchListPages`、`mergeFilePages`、`nextPageToken`、`accountErrorList`、`trashFilters`。
- Produces: 合并回收站（parent 全传 `""`、filters=`trashFilters`），响应形状与 Task 2 相同。

- [ ] **Step 1: 写失败测试**：`TestTrashRouteMergedAll`（两账号 trashed 行合并、两戳齐备）、`TestTrashMergedPaginationNoRepeat`（同 Task 2 型）、`TestTrashMergedAccountErrorIsolated`。单账号既有 trash 测试不动。
- [ ] **Step 2: 跑到红**。**Step 3: 实现**：trash handler 顶部 all 分流 → `handleMergedTrash`（fetchListPages + mergeFilePages，无 q/kind）。**Step 4: 跑到绿** `go test ./...`。**Step 5: 提交**：`git commit -m "feat: merged all-accounts trash listing"`。

### Task 6: 离线任务作用域（列表过滤 + 创建固定 + 清除按作用域）

**Files:**
- Modify: `internal/httpapi/routes_tasks.go`（GET /api/tasks、POST /api/tasks、POST /api/tasks/clear）
- Test: `internal/httpapi/routes_tasks_test.go`

**Interfaces:**
- Consumes: `accountClient`、`rotatingAccounts`、`allAccountClients`、`fetchTaskPages`。
- Produces:
  - `GET /api/tasks?account=<id>` → 单账号列表（仍走 fetchTaskPages，clients 只一个）；缺省/`all` → 现状合并。
  - `POST /api/tasks?account=<id>`（非 `all`）→ 跳过轮换，固定该账号；`all`/缺省 → 现状轮换。
  - `POST /api/tasks/clear?account=<id>` → 只清该账号；`all`/缺省 → 现状全清。

- [ ] **Step 1: 写失败测试**

```go
func TestTasksRouteSingleAccountParam(t *testing.T) {
	// 两账号各有一条任务（假上游 ListTasks 按 bearer 回答）
	// ?account=acct-b → 恰 1 行且 account_id=="acct-b"；缺省 → 2 行（现状回归）
}
func TestTasksCreatePinnedToAccountParam(t *testing.T) {
	// ?account=acct-a POST → 仅 acct-a 的上游收到 CreateOffline（acct-b 计数 0）
	// 缺省 POST → 两个上游至少一个收到（轮换回归；不精确断言个数）
}
func TestTasksClearScopedToAccountParam(t *testing.T) {
	// ?account=acct-b POST /api/tasks/clear → 仅 acct-b 上游收到 ClearFinishedTasks
	// 缺省 → 两个上游都收到（现状回归）
}
```

- [ ] **Step 2: 跑到红**。**Step 3: 实现**：三个 handler 同型分支——`param := r.URL.Query().Get("account")`；`param != "" && param != "all"` 时构造单元素 clients（`accountClient` 后包 `namedClient{Account: acct, C: client}`）；列表走 fetchTaskPages、创建跳过 rotatingAccounts 直接单账号 CreateOffline（resp 的 account 字段照现状带 name）、clear 单账号 ClearFinishedTasks。**Step 4: 跑到绿** `go test ./...`。**Step 5: 提交**：`git commit -m "feat: offline tasks honor account scope for list, create and clear"`。

### Task 7: 最近列表 `?account=<id>` 过滤

**Files:**
- Modify: `internal/httpapi/routes_files.go`（GET /api/recent）
- Test: `internal/httpapi/routes_files_test.go`

**Interfaces:**
- Consumes: `allAccountClients`、`fetchFilePages`、`mergeFilePages`。
- Produces: `GET /api/recent?account=<id>` 单账号 events 列表（clients 一个，其余流程不动）；缺省/`all` → 现状合并。

- [ ] **Step 1: 写失败测试**：`TestRecentRouteSingleAccountParam`（events 假上游按 bearer 回答；`?account=acct-a` 恰 acct-a 的行；缺省合并回归）。
- [ ] **Step 2: 跑到红**。**Step 3: 实现**：与 Task 6 列表同型分支。**Step 4: 跑到绿** `go test ./...`。**Step 5: 提交**：`git commit -m "feat: recent listing honors single-account filter"`。

### Task 8: AccountContext scope + AccountSwitcher「-全部帐号-」

**Files:**
- Modify: `web/src/AccountContext.tsx`
- Modify: `web/src/components/AccountSwitcher.tsx`
- Modify: `web/src/i18n.ts`（新键）
- Test: `web/src/accounts.test.ts`（`resolveCurrentId` 对 `'all'` 的行为若受影响则在此补纯函数用例）

**Interfaces:**
- Consumes: `resolveCurrentId(items, currentId)`（accounts.ts，不改）。
- Produces（Task 9–12 消费）:
  - `AccountContextValue` 增加 `scope: string`（= `wanted || 'all'`；`'all'` 或具体 id）；`setCurrent(id: string)` 接受 `'all'`；`currentId` 语义不变（`wanted !== 'all'` 时走 `resolveCurrentId`，否则按 `''` 兜底到第一个账号，供仍需具体账号的旧消费者使用）。
  - AccountSwitcher props 变为 `{ accounts, scope, onChange(id: string) }`；选项首项 `-全部帐号-`（value `'all'`）+ 各账号；`accounts.length < 2` 仍返回 null。
  - i18n 键 `'account.all_accounts'`：zh-CN `-全部帐号-`，en `- All accounts -`。

- [ ] **Step 1: 写失败测试**（accounts.test.ts，纯函数部分）

```ts
import { describe, expect, test } from 'vitest'
// AccountContext 的 scope 推导是组件内一行；可测的是它的组成材料：
// 'all' 不得被 resolveCurrentId 兜底吞掉。
test('resolveCurrentId ignores the all sentinel', () => {
	const items = [{ id: 'a' }, { id: 'b' }] as never[]
	// 既有语义回归：空串落到第一个账号
	expect(resolveCurrentId(items, '')).toBe('a')
})
```

- [ ] **Step 2: 跑到红/绿判定**：此测试当前应已绿（语义回归保护）。组件部分无 DOM 测试设施——以 `npx tsc -b` + `npm test` 全绿 + Task 12 冒烟为验证。**Step 3: 实现**：AccountContext 增加 scope 推导与 ContextValue 字段；AccountSwitcher 换 props 加首选项（label 走 `t('account.all_accounts')`，组件内 `makeT(getLang())`）；i18n.ts 加键。**Step 4: 全量验证**：`cd web && npx tsc -b && npm test && npx oxlint src`（警告 ≤ 22；switcher 的旧 props 消费者 Files/Trash 在 Task 9/11 同步改，此任务允许 tsc 暂时红于这两处调用点时**先只改类型再改调用点一起提交**——即本任务把 Files/Trash 的 `<AccountSwitcher …>` 调用点同步换成新 props，页面请求逻辑留给 Task 9/11）。**Step 5: 提交**：`git commit -m "feat: account scope in context with an all-accounts switcher option"`。

### Task 9: Files 页合并模式（列表 + 路径导航 + @后缀提示）

**Files:**
- Modify: `web/src/pages/Files.tsx`
- Modify: `web/src/components/FileTable.tsx`、`web/src/components/FileGrid.tsx`（title @后缀）
- Modify: `web/src/listCache.ts`（如需 folderKey 形状调整）
- Test: `web/src/files.test.ts`（若请求串拼装提成纯函数则测之）

**Interfaces:**
- Consumes: Task 8 `scope`；Task 2/4 的 `GET files?account=all[&seg=…][&page_token=…][&kind=…][&q=…]` 信封（items 行带 `account_id/account_name`、`account_errors`）。
- Produces:
  - 请求拼装纯函数（建议导出便于测试，放 `web/src/files.ts`）：

```ts
// filesQuery builds the listing query for one scope: merged mode walks by
// path segments, single-account mode keeps the 2.0.0 parent_id form.
export function filesQuery(scope: string, segs: string[], folderId: string, kind: KindFilter): string
```

  - 缓存键：`cacheKeys.files(scope, merged ? 'path:' + JSON.stringify(segs) : folderId, kind)`。
  - 栈：`type Crumb = { id: string; name: string }` 不变；合并模式 push `{ id: '', name: f.name }`，`onNavigate(i)` 两模式都 `setStack(stack.slice(0, i + 1))`（合并模式导航即截断名字数组）。
  - FileTable/FileGrid：行带 `account_name` 时 `title` 为 `` `${f.name}@${f.account_name}` ``，否则 `f.name`（列表视图名字单元格如无 title 则补上）。

- [ ] **Step 1: 写失败测试**（files.test.ts）

```ts
import { describe, expect, test } from 'vitest'
import { filesQuery } from './files'

describe('filesQuery', () => {
	test('merged scope walks by seg params, no parent_id', () => {
		expect(filesQuery('all', ['电影', '动作'], '', 'all')).toBe(
			'account=all&seg=' + encodeURIComponent('电影') + '&seg=' + encodeURIComponent('动作'),
		)
	})
	test('single scope keeps the parent_id form byte-identical to 2.0.0', () => {
		expect(filesQuery('acct-a', [], 'fld-1', 'all')).toBe('parent_id=fld-1&account=acct-a')
	})
	test('kind filter rides along in both modes', () => {
		expect(filesQuery('all', [], '', 'folder')).toContain('kind=drive%23folder')
	})
})
```

- [ ] **Step 2: 跑到红**：`npm test -- files.test.ts` → FAIL（未导出）。
- [ ] **Step 3: 实现**：filesQuery 导出；Files.tsx `const { scope, … } = useAccounts()`，`const merged = scope === 'all'`；fetchPage 用 filesQuery + limit=100（合并模式带 `page_token`）；合并响应读 `account_errors` 附加到现有错误/提示通道（对齐 Tasks 页的展示方式）；面包屑/点击文件夹按 Interfaces 所述；FileTable/FileGrid title @后缀；`q`/`kind`/`sortFiles`/无限滚动逻辑不动。
- [ ] **Step 4: 全量验证**：`npx tsc -b && npm test`。
- [ ] **Step 5: 提交**：`git commit -m "feat: files page merges all accounts with same-path navigation"`。

### Task 10: Files 合并模式操作语义（禁用矩阵 + 行内账号）

**Files:**
- Modify: `web/src/pages/Files.tsx`
- Test: `web/src/deleteFlow.test.ts` 或新 `web/src/filesOps.test.ts`（跨账号分组的纯函数）

**Interfaces:**
- Consumes: Task 9 的 `merged`、行上 `account_id`。
- Produces:

```ts
// groupByAccount splits selected rows into per-account id lists; rows without
// a stamp fall under the scope itself (single-account listings).
export function groupByAccount(items: FileItem[], scope: string): Map<string, string[]>
```

- [ ] **Step 1: 写失败测试**

```ts
test('groupByAccount splits merged rows and keeps unstamped under scope', () => {
	const rows = [
		{ id: '1', account_id: 'a' },
		{ id: '2', account_id: 'b' },
		{ id: '3' }, // 单账号列表的无戳行
	] as never[]
	const got = groupByAccount(rows as never, 'acct-a')
	expect(got.get('a')).toEqual(['1'])
	expect(got.get('b')).toEqual(['2'])
	expect(got.get('acct-a')).toEqual(['3'])
})
```

- [ ] **Step 2: 跑到红**。**Step 3: 实现**：
  - 禁用矩阵（`disabled={merged || …}` + `title={t('files.merged_readonly')}`）：新建文件夹、上传；右键菜单「移动/复制」项 `disabled={merged}`；`handleDrop`/`onDropOn` 守护 `if (merged) return`；拖拽入口（draggable 属性）合并模式置 false。
  - 删除弹窗确认后按 `groupByAccount` 逐账号发既有删除请求，聚合计数进现有提示。
  - 重命名：`apiPatch(withAccount(\`files/${f.id}\`, f.account_id || scope), …)`。
  - 下载入队：每行带 `account_id`（downloads.ts 的批量条目已支持逐条账号，行戳直接透传；如该函数签名需加透传字段，最小改动）。
  - i18n 键 `'files.merged_readonly'`：zh-CN `合并视图下不可用，请选择具体账号`，en `Not available in the all-accounts view. Pick a single account.`。
- [ ] **Step 4: 全量验证**：`npx tsc -b && npm test && npx oxlint src`（≤22）。**Step 5: 提交**：`git commit -m "feat: files merged view disables creation ops and routes row ops by account"`。

### Task 11: Trash 页合并模式

**Files:**
- Modify: `web/src/pages/Trash.tsx`
- Test: 既有 trash 相关纯函数测试保持绿；分页/请求如提成纯函数则补用例（与 Task 9 filesQuery 同型，trash 无 seg）

**Interfaces:**
- Consumes: Task 8 `scope`、Task 5 `GET trash?account=all` 信封、Task 10 `groupByAccount`。
- Produces: 回收站合并列表、还原/删除按行账号（`groupByAccount` 逐账号 POST 既有 `files/restore`/`files/delete`）、清空按钮 `disabled={merged}`（title 同 `files.merged_readonly`）、缓存键 `cacheKeys.trash(scope)`。

- [ ] **Step 1: 写失败测试**：`groupByAccount` 已在 Task 10 测过；此处新增 trash 查询串纯函数用例（若有），否则 RED 步骤改为：现有测试跑一遍记录绿基线（语义回归保护），合并行为由 Task 13 冒烟验证。
- [ ] **Step 2: 实现**：fetchPage/缓存键/操作拆分/禁用矩阵，模式照 Task 9/10。**Step 3: 全量验证**：`npx tsc -b && npm test && npx oxlint src`（≤22）。**Step 4: 提交**：`git commit -m "feat: trash page merges all accounts with per-row account ops"`。

### Task 12: Recent / Tasks / Transfers 挂下拉框

**Files:**
- Modify: `web/src/pages/Recent.tsx`、`web/src/pages/Tasks.tsx`、`web/src/pages/Transfers.tsx`
- Modify: `web/src/listCache.ts`（`recent: (scope: string) => \`recent:${scope}\``；tasks/transfers 如有缓存键同理）

**Interfaces:**
- Consumes: Task 8 `scope`/`AccountSwitcher`、Task 6/7 后端参数。
- Produces:
  - Recent：`GET recent${scope !== 'all' ? `?account=${scope}` : ''}`；缓存键按 scope。
  - Tasks：列表同型；`POST tasks`（scope 具体时带 `?account=${scope}`，all 时不带→轮换）；`POST tasks/clear` 同型。创建成功的「已通过 {name} 创建」提示现状不动。
  - Transfers：fetch 不变；展示层 `scope === 'all' ? jobs : jobs.filter(j => j.account_id === scope)`（downloads 与 uploads 两列表都过滤）。
  - 三页工具栏各挂 `<AccountSwitcher accounts scope onChange />`。

- [ ] **Step 1: 写失败测试**：Transfers 的过滤是纯逻辑——若内联，提成导出函数 `visibleJobs(jobs, scope)` 并补用例（按 account_id 过滤、无戳行仅 scope 具体时隐藏；`'all'` 全保留）。Recent/Tasks 的请求串改动以既有 vitest 套件回归。
- [ ] **Step 2: 跑到红**（visibleJobs 用例）。**Step 3: 实现**三页接线。**Step 4: 全量验证**：`npx tsc -b && npm test && npx oxlint src`（≤22）。**Step 5: 提交**：`git commit -m "feat: recent, tasks and transfers join the unified account switcher"`。

### Task 13: 版本 2.1.0 + 全量验证 + 交付

**Files:**
- Modify: `Makefile`（`VERSION ?= 2.1.0`）、`manifest`（`version=2.1.0`）
- Modify: `docs/smoke-checklist-multi-account.md`（冒烟清单追加合并视图条目）

- [ ] **Step 1: 版本与清单**：`make set-version VERSION=2.1.0`（或手改两处）；冒烟清单追加：合并视图默认选中、跨账号同名子目录合并、单账号失败隔离（account_errors）、禁用矩阵五项、@后缀提示、离线任务按作用域创建/清除。
- [ ] **Step 2: 全量验证**：`go test ./...`、`cd web && npm test && npx tsc -b && npx oxlint src`（≤22）。
- [ ] **Step 3: 构建交付**：`make fpk` → `cp dist/pikpak-2.1.0-x86.fpk /Users/fengyao/remote/fn-private/` → `gh release create v2.1.0 dist/pikpak-2.1.0-x86.fpk --title … --notes …` → `git push`。
- [ ] **Step 4: 提交**：`git commit -m "chore: bump version to 2.1.0"`（若 set-version 未自动生成提交）。

---

## 执行注意（对所有实施者）

- 每个任务的 commit message 以 `Co-Authored-By: Claude Code <noreply@anthropic.com>` 结尾。
- 后端测试假上游的既有模式：`newAuthTestDeps(t, srv.URL)` + `bindAccount(t, st, 1000, "acct-a", "sub-a", "active")` + bearer `at-acct-a` 分流 + `getJSON(t, h, path, "1000")`。
- 前端合并行为（页面渲染、下拉框交互）无 DOM 测试设施，最终以 Task 13 冒烟清单 + ego-browser 实测验收；单测覆盖所有提成纯函数的逻辑。
