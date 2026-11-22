# 视频收藏夹与 IINA 播放 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让分享里的视频可以收藏成分组影库，并在 macOS 上用 IINA 预览或"完整播放"（转存到 `我的视频/临时`，空间不足自动永久清理），其它平台回退内置播放器；网格视图显示经服务端代理的缩略图。

**Architecture:** 新增 `POST /api/videos/prepare` 登记后台准备任务，worker 完成转存、配额检查、按序永久删除、移动与签名；前端轮询 `GET /api/videos/jobs/{id}`。视频收藏单独存表并按分享者/标题分组；缩略图由 `GET /api/thumb` 代理并以内存缓存，CDN 地址不出服务端。

**Tech Stack:** Go 1.23、modernc.org/sqlite、net/http、React 19 + Vite、vitest。

**Spec:** `docs/superpowers/specs/2026-11-22-share-video-favorites-design.md`

## Global Constraints

- 身份只来自飞牛网关 `X-Trim-Userid`；所有新表与缓存按 `uid` 隔离。
- 分享密码、`pass_code_token`、CDN 签名地址不得出现在响应、日志、收藏列表或前端持久化。
- 永久删除只允许作用于 `我的视频/临时` 与根目录 `Pack From Shared` 里的文件；绝不删文件夹本身，删除只在 `POST /api/videos/prepare` 的 worker 里发生。
- 只有 OAuth 绑定具备 `files:purge`；失败要落成明确的 job 错误，不能假装成功。
- macOS 才用 `iina://weblink?url=<绝对签名地址>`；其它平台一律用应用内 `Player`。
- 迁移只追加，不改已应用的迁移；SQLite 连接串与现有 `migrate.go` 保持一致。
- 文案为简体中文，按钮名固定为：预览、完整播放、收藏分享、收藏视频。
- 收藏的重命名只影响本机显示（`display_name`/`display_title`），绝不改名 PikPak 上的文件或分享。

## Review Focus

- 删除后配额读数有延迟：清理循环必须重新读 `about`，并在删除任务完成后才判断；Task 4 的 `TestPrepareDeletesOldestUntilQuotaFits` 覆盖。
- 根目录已有同名文件时的转存落点：用转存前快照 + 名称/大小定位新文件；Task 4 的 `TestPreparePicksTheNewRootFileNotTheOldOne` 覆盖。
- 同一用户并发点「完整播放」：只能有一个任务；Task 4 的 `TestPrepareRefusesASecondJobForTheSameUser` 覆盖。
- 收藏的分享密码已变或分享失效：worker 失败并给出可读原因；Task 4 的 `TestPrepareFailsWhenTheSavedPassCodeIsWrong` 覆盖。
- IINA 不存在或平台不是 macOS：不得卡死，必须回退内置播放器；Task 6 的 `iinaURL`/`isMacOS` 测试与浏览器验证覆盖。

---

### Task 1: 缩略图代理

**Files:**
- Modify: `internal/pikpak/files.go` (`File`, `ListFiles`, `FileDetail`)
- Modify: `internal/pikpak/files_test.go`
- Modify: `internal/pikpak/share.go` (`shareFileInfo`, `ShareFileLink`, `GetShareFileLink`)
- Modify: `internal/pikpak/share_test.go`
- Create: `internal/httpapi/routes_thumb.go`
- Create: `internal/httpapi/routes_thumb_test.go`
- Modify: `internal/httpapi/server.go` (`Deps.Thumbs`, register)

**Interfaces:**
- Produces: `pikpak.File.ThumbnailLink string`; `pikpak.ShareFileLink.ThumbnailLink string`; `httpapi.newThumbCache() *thumbCache`; `GET /api/thumb?file_id=` 与 `GET /api/thumb?share_id=&file_id=`。

- [ ] **Step 1: 写失败测试（客户端）**

```go
func TestListFilesAsksForThumbnailsAndParsesThem(t *testing.T) {
    // upstream asserts thumbnail_size=SIZE_LARGE; returns one file with
    // "thumbnail_link":"https://img/x.jpg"; expect Item.ThumbnailLink set.
}
func TestShareFileLinkCarriesItsThumbnail(t *testing.T) {
    // share/file_info returns file_info.thumbnail_link; expect link.ThumbnailLink.
}
```

- [ ] **Step 2: 运行并看它失败**

Run: `go test ./internal/pikpak -run 'TestListFilesAsksForThumbnailsAndParsesThem|TestShareFileLinkCarriesItsThumbnail' -v`
Expected: FAIL（字段/查询参数不存在）

- [ ] **Step 3: 实现客户端字段与参数**

`File` 增加 `ThumbnailLink string \`json:"-"\``，并在 `UnmarshalJSON` 的匿名结构解析 `thumbnail_link`；`ListFiles` 与 `FileDetail` 的查询加 `thumbnail_size=SIZE_LARGE`；`shareFileInfo` 加 `ThumbnailLink`，`GetShareFileLink` 的查询加 `thumbnail_size=SIZE_LARGE` 并填 `ShareFileLink.ThumbnailLink`。

- [ ] **Step 4: 运行客户端测试**

Run: `go test ./internal/pikpak -run 'Thumbnail' -v`
Expected: PASS

- [ ] **Step 5: 写失败测试（代理）**

```go
func TestThumbProxiesDriveImageBytes(t *testing.T) {
    // files list returns thumbnail_link to image server ("JPEG")
    // GET /api/thumb?file_id=f1 → 200, Content-Type image/jpeg, body "JPEG"
    // response body must not contain the image server URL
}
func TestThumbUsesTheShareSessionForShareFiles(t *testing.T) {
    // open share first, then GET /api/thumb?share_id=VOabc123&file_id=sf2
    // → bytes; without opening the share → 400 share_session_missing
}
func TestThumbReturns404WhenTheFileHasNoThumbnail(t *testing.T) {}
```

- [ ] **Step 6: 运行并看它失败**

Run: `go test ./internal/httpapi -run 'TestThumb' -v`
Expected: FAIL（404 / 未注册）

- [ ] **Step 7: 实现 `routes_thumb.go` 与缓存**

`thumbCache{mu, ttl, items map[string][]byte}` 键 `uid|share|file`，`get/put` 同 `shareSessions` 风格；handler 先查缓存，未命中时经 `boundClient` 取 `ThumbnailLink`（分享用 `shareToken`），用 `http.Client` 取图（`io.LimitReader` 5 MiB），校验 `image/` 前缀，写 `Cache-Control: private, max-age=600` 与上游 `Content-Type`。`Deps.Thumbs` 为空时用默认缓存，并在 `NewMux` 注册 `GET /api/thumb`。

- [ ] **Step 8: 运行并看它通过**

Run: `go test ./internal/httpapi -run 'TestThumb' -v`
Expected: PASS

- [ ] **Step 9: 提交**

```bash
git add internal/pikpak/files.go internal/pikpak/files_test.go internal/pikpak/share.go internal/pikpak/share_test.go internal/httpapi/routes_thumb.go internal/httpapi/routes_thumb_test.go internal/httpapi/server.go
git commit -m "feat(thumb): proxy PikPak thumbnails through the server"
```

---

### Task 2: 视频收藏存储与接口

**Files:**
- Create: `internal/store/video_favorites.go`
- Create: `internal/store/video_favorites_test.go`
- Modify: `internal/store/migrate.go`（追加迁移）
- Create: `internal/httpapi/routes_video_favorites.go`
- Create: `internal/httpapi/routes_video_favorites_test.go`
- Modify: `internal/httpapi/server.go`（注册）

**Interfaces:**
- Produces: `store.VideoFavorite{ID, UID, ShareID, FileID, Name, Size, Owner, ShareTitle, DisplayName, CreatedAt, PassCode}`；`AddVideoFavorite(ctx, uid int, fav VideoFavorite) (VideoFavorite, error)`、`ListVideoFavorites(ctx, uid int) ([]VideoFavorite, error)`、`GetVideoFavorite(ctx, uid int, id int64) (VideoFavorite, error)`、`RenameVideoFavorite(ctx, uid int, id int64, name string) (VideoFavorite, error)`、`DeleteVideoFavorite(ctx, uid int, id int64) error`；`GET/POST /api/video-favorites`、`PATCH /api/video-favorites/{id}`、`DELETE /api/video-favorites/{id}`。

- [ ] **Step 1: 写失败测试（存储）**

```go
func TestVideoFavoritesSealThePassCode(t *testing.T) {
    // Add(share_id, file_id, name, size, owner, title, "7f3a")
    // List → PassCode == ""; Get → "7f3a"
}
func TestVideoFavoritesUpsertKeepsTheOldCode(t *testing.T) {}
func TestVideoFavoritesArePerUser(t *testing.T) {}
func TestVideoFavoriteRenameIsDisplayOnly(t *testing.T) {
    // Rename(id, "我的片单") → DisplayName set, Name unchanged; empty clears;
    // another uid's Get → ErrNotFound
}
```

- [ ] **Step 2: 运行并看它失败**

Run: `go test ./internal/store -run 'VideoFavorite' -v`
Expected: FAIL（类型不存在）

- [ ] **Step 3: 实现存储与迁移**

迁移追加 `video_favorites`（字段见规格，`UNIQUE(uid, share_id, file_id)`，`pass_code_enc BLOB NOT NULL DEFAULT x''`，`display_name TEXT NOT NULL DEFAULT ''`）；AAD 用 `aadFor(uid, "video_pass_code:"+shareID)`；空密码不覆盖已存密码，与 `share_favorites` 的 upsert 规则一致。改名只写 `display_name`，不动 `name`（原文件名始终保留）。

- [ ] **Step 4: 运行存储测试**

Run: `go test ./internal/store -run 'VideoFavorite' -v`
Expected: PASS

- [ ] **Step 5: 写失败测试（接口）**

```go
func TestVideoFavoritesRoundTrip(t *testing.T) {
    // POST {share_id:"VOabc123",file_id:"sf2",name:"a.mp4",size:9,owner:"张三",share_title:"资料",pass_code:"7f3a"}
    // → 201, no "7f3a" in body; GET → one item with owner "张三"; DELETE → empty
}
```

- [ ] **Step 6: 运行并看它失败**

Run: `go test ./internal/httpapi -run TestVideoFavoritesRoundTrip -v`
Expected: FAIL（404）

- [ ] **Step 7: 实现路由**

POST 校验 `share_id`、`file_id` 非空、`size >= 0`、名称 ≤ 255 字节；`pass_code` 只用于密封。响应使用 `store.VideoFavorite` 的 JSON（`PassCode` 标签为 `-`）。PATCH 只接受 `{"name": "..."}`（≤ 100 字符，空串清除显示名），按 `uid + id` 更新。

- [ ] **Step 8: 运行并看它通过**

Run: `go test ./internal/httpapi -run TestVideoFavoritesRoundTrip -v`
Expected: PASS

- [ ] **Step 9: 提交**

```bash
git add internal/store/video_favorites.go internal/store/video_favorites_test.go internal/store/migrate.go internal/httpapi/routes_video_favorites.go internal/httpapi/routes_video_favorites_test.go internal/httpapi/server.go
git commit -m "feat(video): store video favorites with sealed pass codes"
```

---

### Task 3: 分享者、转存任务号与分享缩略图字段

**Files:**
- Modify: `internal/pikpak/share.go`
- Modify: `internal/pikpak/share_test.go`
- Modify: `internal/httpapi/routes_share.go`（响应加 `owner`）
- Modify: `internal/httpapi/routes_share_test.go`

**Interfaces:**
- Produces: `pikpak.ShareInfo.Owner string`；`RestoreShare(ctx, shareID, passToken string, fileIDs []string) (taskID string, err error)`；`GET /api/share` 响应增加 `"owner"`。

- [ ] **Step 1: 写失败测试**

```go
func TestShareReportsItsCreator(t *testing.T) {
    // upstream returns {"share_creator":{"name":"张三"}}
    // expect info.Owner == "张三"; missing field → ""
}
func TestRestoreShareReturnsTheTaskID(t *testing.T) {
    // upstream returns {"task_id":"t1"}; expect "t1"
}
```

- [ ] **Step 2: 运行并看它失败**

Run: `go test ./internal/pikpak -run 'TestShareReportsItsCreator|TestRestoreShareReturnsTheTaskID' -v`
Expected: FAIL

- [ ] **Step 3: 实现解析与任务号**

接受 `share_creator.name`、`creator.name`、`share_creator.user_name` 中第一个非空值；`RestoreShare` 返回 `task_id`。更新 `routes_share.go` 的调用与响应（`owner`），现有测试保持通过。

- [ ] **Step 4: 运行并看它通过**

Run: `go test ./internal/pikpak ./internal/httpapi -run 'Share|Restore' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/pikpak/share.go internal/pikpak/share_test.go internal/httpapi/routes_share.go internal/httpapi/routes_share_test.go
git commit -m "feat(share): report share owner and restore task id"
```

---

### Task 4: 准备任务 worker 与接口

**Files:**
- Create: `internal/httpapi/video_jobs.go`
- Create: `internal/httpapi/video_prepare.go`
- Create: `internal/httpapi/routes_videos.go`
- Create: `internal/httpapi/routes_videos_test.go`
- Modify: `internal/httpapi/server.go`（`Deps.Videos`、注册）
- Modify: `internal/pikpak/files.go`（分页列出某个目录，复用 `ListFiles`）

**Interfaces:**
- Consumes: Task 2 的 `store.VideoFavorite`/`GetVideoFavorite`；Task 3 的 `RestoreShare`。
- Produces: `type videoPrepareRequest struct { ShareID, FileID string; FavoriteID int64 }`；`videoPrepareResult{FileID, Name, Size, StreamURL string}`；`videoJobs.Start(uid int, run func(context.Context) (videoPrepareResult, error)) (string, bool)` 与 `Get(uid int, id string, now time.Time) (videoJob, bool)`；`POST /api/videos/prepare`、`GET /api/videos/jobs/{id}`、`POST /api/videos/preview`（收藏预览，返回 `{stream_url}`，不转存）。

- [ ] **Step 1: 写失败测试（任务状态机）**

```go
func TestPrepareJobRunsInTheBackground(t *testing.T) {
    // Start with a run func that blocks on a channel; GET → state "running"
    // close channel → GET → state "ready" with stream_url
}
func TestPrepareRefusesASecondJobForTheSameUser(t *testing.T) {} // 409 video_busy
```

- [ ] **Step 2: 运行并看它失败**

Run: `go test ./internal/httpapi -run 'TestPrepare(Job|Refuses)' -v`
Expected: FAIL

- [ ] **Step 3: 实现 `video_jobs.go`**

内存表 `map[string]videoJob`，键 `id`（随机 16 hex），含 `uid`、`state`（`running|ready|failed`）、`stage`、`error`、结果字段与 `expires`（30 分钟）；`busy map[int]bool` 保证每用户一个任务；`Start` 启动 goroutine，结束时清 `busy`。`NewMux` 用默认实例。

- [ ] **Step 4: 写失败测试（真实流程）**

```go
func TestPrepareRestoresAndMovesToTemp(t *testing.T) {
    // upstream: about(limit 1000, usage 900), share/file_info size 50,
    // restore → task t1 → complete; root list initially has old.bin;
    // after restore root list includes new.mp4 (id df1);
    // folder list: root has 我的视频(f1)/Pack From Shared(f2); f1 has 临时(f3)
    // POST prepare → poll until ready; assert move to f3, stream_url begins /api/stream/df1?t=
}
func TestPrepareDeletesOldestUntilQuotaFits(t *testing.T) {
    // usage 990, size 50 → temp files [old1(60), old2(20)]; after delete old1
    // usage becomes 930 → fits; assert only old1 deleted
}
func TestPrepareFallsBackToPackFromShared(t *testing.T) {} // temp insufficient → delete f2 files
func TestPrepareFailsWhenSpaceStillShort(t *testing.T) {}
// 两边清空后仍不够：POST 返回 202 job_id，轮询到 state "failed"，
// error 文案包含 "空间不足"，不产生任何转存调用。
func TestPreviewReturnsASignedShareURL(t *testing.T) {
    // POST /api/videos/preview {video_favorite_id} → stream_url starts
    // /api/share/stream/<share>/<file>?t=，且响应不含 pass code
}
func TestPreparePicksTheNewRootFileNotTheOldOne(t *testing.T) {}
func TestPrepareFailsWhenTheSavedPassCodeIsWrong(t *testing.T) {} // favorite_id path
```

- [ ] **Step 5: 运行并看它们失败**

Run: `go test ./internal/httpapi -run 'TestPrepare' -v`
Expected: FAIL（handler 未注册）

- [ ] **Step 6: 实现 `video_prepare.go` 与 `routes_videos.go`**

顺序：解析来源（`share_id+file_id` 用 `ShareSessions` 的 token；`video_favorite_id` 用 `GetVideoFavorite` 解密密码后 `GetShare` 取 token，并回写 owner/title）→ `GetShareFileLink` 取 `size`（缺失用 0 视为无需腾空间）→ `About` 算空闲 → 目标 `我的视频/临时` 已有同名同大小文件则直接签名返回 → 空间不足时循环：列 `临时` 文件（`ListFiles`，按 `modified_time` 升序）→ `BatchDelete` → `WaitTaskSettled` → 重读 `About`；仍不够则同样的循环作用于根目录 `Pack From Shared`（只取 `drive#file`）→ 仍不够返回 `ErrSpaceInsufficient`（job 置 failed，`error` 为"空间不足"）→ `RestoreShare` 得 task → 等任务 → 根目录快照差集找新文件 → `BatchMove` 到 `临时`（必要时 `CreateFolder` 建 `我的视频`/`临时`）→ 等任务 → `playtoken.Sign` 签名，返回结果。

`POST /api/videos/preview` 复用同一套来源解析，只做 `GetShareFileLink` + `playtoken.SignShare`，返回 `{stream_url}`；`video_favorite_id` 走解密密码 → `GetShare` 的路径。

- [ ] **Step 7: 运行并看它通过**

Run: `go test ./internal/httpapi -run 'TestPrepare' -v`
Expected: PASS

- [ ] **Step 8: 跑全量 Go 测试**

Run: `go test ./...`
Expected: 全绿（现有 351 项 + 新增）

- [ ] **Step 9: 提交**

```bash
git add internal/httpapi/video_jobs.go internal/httpapi/video_prepare.go internal/httpapi/routes_videos.go internal/httpapi/routes_videos_test.go internal/httpapi/server.go internal/pikpak/files.go
git commit -m "feat(video): prepare share videos for playback with automatic cleanup"
```

---

### Task 5: 网格缩略图（前端）

**Files:**
- Modify: `web/src/files.ts`（`thumbPath`）
- Modify: `web/src/files.test.ts`
- Modify: `web/src/components/FileGrid.tsx`（`thumbSrc`）
- Modify: `web/src/pages/Files.tsx`、`web/src/components/ShareBrowser.tsx`（传 `thumbSrc`）
- Modify: `web/src/index.css`（`.tile-thumb`）

**Interfaces:**
- Consumes: Task 1 的 `/api/thumb`。
- Produces: `thumbPath(fileID: string, shareID?: string): string` 返回 `thumb?share_id=…&file_id=…` 或 `thumb?file_id=…`（不含 `/api` 前缀）；`FileGrid` 新 prop `thumbSrc?: (f: FileItem) => string`。

- [ ] **Step 1: 写失败测试**

```ts
test('builds a drive thumbnail path', () => {
  expect(thumbPath('f1')).toBe('thumb?file_id=f1')
})
test('builds a share thumbnail path', () => {
  expect(thumbPath('sf2', 'VOabc123')).toBe('thumb?share_id=VOabc123&file_id=sf2')
})
```

- [ ] **Step 2: 运行并看它失败**

Run: `cd web && npx vitest run src/files.test.ts`
Expected: FAIL（`thumbPath` 未导出）

- [ ] **Step 3: 实现 `thumbPath` 与网格图**

`FileGrid` 在 `.tile-icon` 内先渲染 `<img className="tile-thumb" loading="lazy" src=… onError={隐藏自身}>`，`onError` 后回退现有 `FileIcon`；无 `thumbSrc` 时行为不变。`Files.tsx` 传 `${basePath}/api/${thumbPath(f.id)}`，`ShareBrowser` 传 `${basePath}/api/${thumbPath(f.id, shareId)}`。CSS：`.tile-thumb { width: 96px; height: 60px; object-fit: cover; border-radius: 6px; }`。

- [ ] **Step 4: 运行前端测试**

Run: `cd web && npx vitest run`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add web/src/files.ts web/src/files.test.ts web/src/components/FileGrid.tsx web/src/pages/Files.tsx web/src/components/ShareBrowser.tsx web/src/index.css
git commit -m "feat(web): show proxied thumbnails in the icon view"
```

---

### Task 6: 播放助手与分享工具栏

**Files:**
- Create: `web/src/iina.ts`、`web/src/iina.test.ts`
- Create: `web/src/videoPrepare.ts`、`web/src/videoPrepare.test.ts`
- Modify: `web/src/components/ShareBrowser.tsx`
- Modify: `web/src/pages/Search.tsx`（把 `owner` 存进 tab 状态）

**Interfaces:**
- Consumes: `/api/share` 的 `owner`；`/api/videos/prepare`、`/api/videos/jobs/{id}`。
- Produces: `isMacOS(ua: string): boolean`；`iinaURL(streamURL: string): string`；`waitForVideoJob(id: string, opts: { get: (id) => Promise<VideoJob>; sleep?: (ms) => Promise<void> }): Promise<VideoJob>`；`ShareState` 增加 `owner: string`。

- [ ] **Step 1: 写失败测试**

```ts
test('only macOS gets IINA', () => {
  expect(isMacOS('Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0)')).toBe(true)
  expect(isMacOS('Mozilla/5.0 (Windows NT 10.0)')).toBe(false)
})
test('wraps a stream URL for IINA', () => {
  expect(iinaURL('http://nas/api/stream/f1?t=x')).toBe('iina://weblink?url=http%3A%2F%2Fnas%2Fapi%2Fstream%2Ff1%3Ft%3Dx')
})
test('polls until the job is ready', async () => {
  // get returns running, running, ready; expect ready and 3 calls
})
```

- [ ] **Step 2: 运行并看它失败**

Run: `cd web && npx vitest run src/iina.test.ts src/videoPrepare.test.ts`
Expected: FAIL（模块不存在）

- [ ] **Step 3: 实现助手**

`waitForVideoJob` 每 1 秒调用一次 `get`，最多 15 分钟；`state === 'failed'` 时以服务端 `error` 拒绝；`ready` 返回结果。分享流地址用 `window.location.origin + basePath + stream_url`。

- [ ] **Step 4: 运行助手测试**

Run: `cd web && npx vitest run src/iina.test.ts src/videoPrepare.test.ts`
Expected: PASS

- [ ] **Step 5: 接进分享工具栏（无组件测试，浏览器验证）**

工具栏改为：`预览`（单个选中视频 → macOS 打开 `iinaURL(绝对分享流地址)`，否则 `Player`）、`完整播放`（POST prepare → `waitForVideoJob` → macOS 显示 `用 IINA 播放` 按钮，否则直接 `Player` 打开 `stream_url`）、`转存`（原样）、`收藏分享`（原“加入收藏”）、`收藏视频`（选中视频 POST `/api/video-favorites`，密码取当前 `passCode`，owner/title 取 `/api/share` 响应）。`ShareState` 增加 `owner`，`Search.tsx` 的 `tabStates` 一并保存。

- [ ] **Step 6: 类型检查与全量前端测试**

Run: `cd web && npx tsc --noEmit && npx vitest run`
Expected: PASS

- [ ] **Step 7: 浏览器验证**

用 fetch stub 走通：预览唤起（macOS UA）与非 macOS 回退、完整播放轮询 running→ready、收藏视频写入、收藏分享、按钮禁用条件。截图确认工具栏与状态栏无溢出。

- [ ] **Step 8: 提交**

```bash
git add web/src/iina.ts web/src/iina.test.ts web/src/videoPrepare.ts web/src/videoPrepare.test.ts web/src/components/ShareBrowser.tsx web/src/pages/Search.tsx
git commit -m "feat(web): preview and fully play share videos with IINA"
```

---

### Task 7: 视频收藏夹页面

**Files:**
- Create: `web/src/videoFavorites.ts`、`web/src/videoFavorites.test.ts`
- Create: `web/src/pages/VideoFavorites.tsx`
- Modify: `web/src/App.tsx`（nav、route、title）
- Modify: `web/src/i18n.ts`（`nav.videoFavorites`）
- Modify: `web/src/index.css`（`.vgroup`、`.vgroup-head`）

**Interfaces:**
- Consumes: `GET /api/video-favorites`、`DELETE /api/video-favorites/{id}`、Task 6 的 `waitForVideoJob`/`iinaURL`/`isMacOS`、`Player`。
- Produces: `groupVideoFavorites(items: VideoFavorite[]): { key: string; label: string; items: VideoFavorite[] }[]`，顺序：有 owner 的按 owner 首次出现，无 owner 的按 share_title，最后 `未知分享`；组内按 `created_at` 倒序。

- [ ] **Step 1: 写失败测试**

```ts
test('groups by owner, then share title, then unknown', () => {
  // three items: owner "张三", owner "", title "资料", both empty
  // expect labels ["张三", "资料", "未知分享"]
})
test('keeps newest first inside a group', () => {})
```

- [ ] **Step 2: 运行并看它失败**

Run: `cd web && npx vitest run src/videoFavorites.test.ts`
Expected: FAIL

- [ ] **Step 3: 实现分组与页面**

页面结构：工具栏 `[预览][完整播放][重命名][移除收藏][刷新]` + 按组渲染的 `.vgroup` 列表（每组标题为分享者/标题，行内显示 `display_name || name` 与大小）；选择规则复用 `useFileSelection`；`重命名` 用 `window.prompt('新名称', display_name || name)`，`apiPatch('video-favorites/<id>', {name})` 后更新本地列表（空串恢复原名）；`完整播放` 调 `prepare {video_favorite_id}` 再轮询；`预览` 需要打开分享取签名地址（`GET /api/share?favorite_id=` 不存在于视频收藏 → 用 `share_id` + 服务端密封密码：新增 `prepare` 的预览分支不可行，预览改为请求 `POST /api/videos/preview {video_favorite_id}` 返回 `stream_url`，在 Task 4 的路由中实现并加 `TestPreviewReturnsASignedShareURL`）。

> 实现提示：Task 4 已具备解析收藏密码的能力，`preview` 只做“打开分享 + `GetShareFileLink` + `SignShare`”，不转存。

- [ ] **Step 4: 运行前端测试**

Run: `cd web && npx tsc --noEmit && npx vitest run`
Expected: PASS

- [ ] **Step 5: 浏览器验证**

收藏夹分组、选择、预览、完整播放、重命名（只改显示名）、移除；空状态文案；侧栏顺序为 分享收藏夹 → 视频收藏夹 → 全网搜索。截图核对。

- [ ] **Step 6: 提交**

```bash
git add web/src/videoFavorites.ts web/src/videoFavorites.test.ts web/src/pages/VideoFavorites.tsx web/src/App.tsx web/src/i18n.ts web/src/index.css internal/httpapi/routes_videos.go internal/httpapi/routes_videos_test.go
git commit -m "feat(web): add the video favorites page"
```

---

### Task 8: 分享收藏夹手工新增与重命名

**Files:**
- Modify: `internal/store/favorites.go`
- Modify: `internal/store/favorites_test.go`
- Modify: `internal/store/migrate.go`（追加 `ALTER TABLE share_favorites ADD COLUMN display_title`）
- Modify: `internal/httpapi/routes_share.go`
- Modify: `internal/httpapi/routes_share_test.go`
- Modify: `web/src/pages/Favorites.tsx`
- Modify: `web/src/index.css`（`.fav-add` 行内表单）

**Interfaces:**
- Consumes: 现有 `POST /api/share-favorites`（`{url, title?, pass_code?}`，服务端负责解析分享链接与提取 `?pwd=`）。
- Produces: `store.ShareFavorite.DisplayTitle string \`json:"display_title"\``；`RenameShareFavorite(ctx, uid int, id int64, title string) (ShareFavorite, error)`；`PATCH /api/share-favorites/{id}`；分享收藏夹工具栏的 `新增收藏` 按钮、行内表单与行内 `重命名`。

- [ ] **Step 1: 写失败测试（重命名）**

```go
func TestShareFavoriteRenameIsDisplayOnly(t *testing.T) {
    // Rename(id, "网课") → DisplayTitle "网课", Title 不变；空串清除；
    // 另一个 uid Rename → ErrNotFound
}
func TestShareFavoritesPatch(t *testing.T) {
    // PATCH /api/share-favorites/1 {"title":"网课"} → 200；
    // 列表返回 display_title，且不含密码；空 title → 恢复原显示
}
```

- [ ] **Step 2: 运行并看它失败**

Run: `go test ./internal/store ./internal/httpapi -run 'Rename|FavoritesPatch' -v`
Expected: FAIL

- [ ] **Step 3: 实现重命名**

迁移追加 `ALTER TABLE share_favorites ADD COLUMN display_title TEXT NOT NULL DEFAULT ''`（新迁移，不改已应用迁移）；`scanShareFavorite` 多扫一列；`RenameShareFavorite` 按 `uid + id` 更新，返回 `ErrNotFound`；`PATCH /api/share-favorites/{id}` 接受 `{"title": "..."}`（≤ 100 字符，空串清除）。列表 JSON 增加 `display_title`。

- [ ] **Step 4: 运行 Go 测试**

Run: `go test ./internal/store ./internal/httpapi -run 'Rename|FavoritesPatch' -v`
Expected: PASS

- [ ] **Step 5: 加新增按钮与表单**

工具栏 `[刷新] [去全网搜索]` 后加 `[新增收藏]`；点击后展开一行表单：

```
[ 粘贴分享链接，例如 https://mypikpak.com/s/xxxx?pwd=1234 ] [保存] [取消]
```

`保存` POST `share-favorites`，`url` 为空时禁用；成功后清空并 `load()` 刷新列表，失败把服务端文案显示在表单下方（`bad_share_link` → "这不是 PikPak 分享链接"）；`取消` 收起表单。表单不承诺标题，列表以服务端返回的 title/share_id 显示；带密码的链接在打开时照常要密码（收藏本身不存密码，除非用户在打开后点过收藏分享）。

- [ ] **Step 2: 类型检查与测试**

Run: `cd web && npx tsc --noEmit && npx vitest run`
Expected: PASS

- [ ] **Step 3: 浏览器验证**

空列表页点 `新增收藏` → 粘贴 `https://mypikpak.com/s/VOabc123?pwd=7f3a` → 保存 → 列表出现该分享；粘贴 `https://example.com/x` → 显示"这不是 PikPak 分享链接"且列表不变；取消可收起。选中一行点 `重命名` → 输入"网课" → 列表显示"网课"而分享编号不变；空输入恢复原显示。截图确认工具栏与"我的文件"一致。

- [ ] **Step 4: 提交**

```bash
git add internal/store/favorites.go internal/store/favorites_test.go internal/store/migrate.go internal/httpapi/routes_share.go internal/httpapi/routes_share_test.go web/src/pages/Favorites.tsx web/src/index.css
git commit -m "feat(favorites): add shares from a pasted link and rename them locally"
```

---

### Task 9: 文档、版本与打包

**Files:**
- Modify: `docs/install.md`
- Modify: `docs/pikpak-api.md`（§8 补 owner/thumbnail/restore 观察）
- Modify: `.superpowers/sdd/2026-11-22-fnos-pikpak-v1/progress.md`（任务裁定）

- [ ] **Step 1: 更新文档**

写入：视频收藏夹入口与分组规则；分享收藏夹的 `新增收藏`（粘贴链接）与两个收藏夹的本地重命名；预览/完整播放差异；`我的视频/临时` 与 `Pack From Shared` 的自动永久删除规则（明确不可恢复）；IINA 仅 macOS 与手势限制；缩略图经 `/api/thumb` 代理。

- [ ] **Step 2: 全量验证**

Run: `cd web && npx vitest run && npx tsc --noEmit`
Run: `go vet ./... && go test ./...`
Expected: 全绿

- [ ] **Step 3: 打包并复制**

Run: `make release`
Then: `cp dist/pikpak-1.0.0-x86.fpk /Users/fengyao/remote/fn-private/`

- [ ] **Step 4: 提交**

```bash
git add docs/install.md docs/pikpak-api.md
git commit -m "docs: video favorites, IINA playback and automatic cleanup"
```
