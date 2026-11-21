# fnOS PikPak · M0 验证版实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付一个可安装到 x86 fnOS 的 `.fpk`，在真机上验证设计里六个未决前提（网关行为、Range 流式、pickUserFile/ACL、PikPak 搜索、列表排序、REST 上传），并把结果导出成报告。

**Architecture:** 单个 Go 二进制（`CGO_ENABLED=0`），监听 `${TRIM_APPDEST}/app.sock`；前端 React + Vite 构建后 `go:embed`。M0 版本不实现完整产品，只实现承载探针的骨架：身份中间件、Open API 客户端、SQLite + AES-GCM 凭据存储、OAuth 设备码、PikPak 只读客户端、GCID 与上传探针、Range 探针、报告导出。每个探针在页面上显示 PASS/FAIL/WARN 和细节，并汇总为可复制的 JSON。

**Tech Stack:** Go 1.27（标准库 + `modernc.org/sqlite` + `github.com/aliyun/aliyun-oss-go-sdk/oss`）、React 18 + TypeScript + Vite、`@trimjs/web-app`、fnpack 1.3.3。

**Spec:** `docs/superpowers/specs/2026-11-21-fnos-pikpak-design.md`

**范围说明：** 本计划只覆盖设计里的 M0。M1–M6（完整文件管理、传输引擎、离线任务、播放器、打磨）在 M0 报告回来、六个未知项定论后另写计划。M0 的交付物本身是可安装、可测试的软件：一个诊断应用。

## Global Constraints

- 生产环境不监听 TCP；只监听 `${TRIM_APPDEST}/app.sock`，文件权限 0600。
- 身份只来自 `X-Trim-Userid`；缺失 → 401，重复 → 400；绝不从请求体/查询参数取 uid。
- 不以 root 运行：`config/privilege` 为 `{"defaults":{"run-as":"package"}}`。
- 目标平台 x86（linux/amd64）；`CGO_ENABLED=0`；二进制内嵌前端。
- 令牌、`device_code`、`Authorization`、签名下载地址不进日志、不进前端、不进报告。
- 数据库与密钥放 `${TRIM_PKGVAR}`；DB 0600，`secret.key` 0600。
- 默认界面语言 zh-CN。
- PikPak scope：`files:read files:write files:manage files:purge cloud_download share account`。
- GCID 算法固定为：块大小从 256 KiB 起，若 `size/blocksize > 512` 则翻倍，最大 2 MiB；对每块取 SHA-1，拼接所有块摘要后再取 SHA-1，小写十六进制；上报时转大写。空文件为 `da39a3ee5e6b4b0d3255bfef95601890afd80709`。
- 每个任务结束必须 `go test ./...` 通过并提交；提交信息用 `feat:` / `test:` / `chore:` 前缀。

## Review Focus

1. 伪造或重复的 `X-Trim-Userid` → 必须拒绝，绝不能把请求当成另一个用户。
2. 目标路径含 `..`、符号链接，或不在该用户授权集合内 → 写盘/读盘前必须拒绝。
3. Range 请求超出文件末尾或格式非法 → 必须 416/206 且不 panic。
4. 访问令牌在第 N 个请求过期、并发刷新 → 只刷新一次、只重试一次、不把用户踢下线。
5. 空文件与恰好落在块边界的文件 → GCID 正确、上传成功（`cid and file_size is required` 不得出现）。

每一条都在对应任务的测试里固定住。

---

## 文件结构

```
fnos-pikpak/
├── .gitignore
├── go.mod
├── Makefile
├── manifest
├── ICON.PNG                      # 由 tools/genicon 生成
├── ICON_256.PNG
├── cmd/
│   ├── main                      # fnOS 生命周期脚本
│   ├── install_init              # 空操作，exit 0
│   ├── install_callback
│   ├── upgrade_init
│   ├── upgrade_callback
│   ├── uninstall_init
│   ├── uninstall_callback
│   ├── config_init
│   ├── config_callback
│   └── pikpak/main.go            # Go 入口
├── app/ui/
│   ├── config
│   └── images/icon_{1,2,3}.png
├── config/
│   ├── privilege
│   └── resource
├── wizard/.gitkeep
├── target/                       # 构建产物（.gitignore）
├── tools/
│   ├── genicon/main.go
│   └── bin/                      # fnpack（.gitignore）
├── internal/
│   ├── version/version.go
│   ├── fnos/identity.go
│   ├── fnos/identity_test.go
│   ├── fnos/openapi.go
│   ├── fnos/openapi_test.go
│   ├── fnos/authorize.go
│   ├── fnos/authorize_test.go
│   ├── fnostest/gateway.go        # 假 fnOS 网关（Unix socket），供各包测试复用
│   ├── probe/probe.go
│   ├── probe/redact.go
│   ├── probe/redact_test.go
│   ├── probe/builtin.go
│   ├── store/store.go
│   ├── store/crypto.go
│   ├── store/migrate.go
│   ├── store/store_test.go
│   ├── stream/range.go
│   ├── stream/range_test.go
│   ├── pikpak/client.go
│   ├── pikpak/auth.go
│   ├── pikpak/gcid.go
│   ├── pikpak/gcid_test.go
│   ├── pikpak/upload.go
│   ├── pikpak/probes.go
│   └── pikpak/*_test.go
├── internal/httpapi/
│   ├── server.go
│   ├── routes_probe.go
│   ├── routes_auth.go
│   ├── routes_stream.go
│   ├── server_test.go
│   └── web.go
├── internal/webdist/embed.go
├── internal/webdist/dist/index.html   # 占位，随仓库提交；assets/ 构建生成
├── web/                          # React 源码
│   ├── package.json
│   ├── vite.config.ts
│   ├── index.html
│   └── src/...
└── docs/
    ├── pikpak-api.md
    ├── m0-report.md
    └── superpowers/{specs,plans}/...
```

职责：`fnos` 只做身份与 Open API；`pikpak` 只做上游协议；`store` 只做持久化与加密；`probe` 只做探针编排与脱敏；`httpapi` 只做路由与 DTO；`web` 只做展示。跨层不反向依赖。

---

### Task 1: 仓库与工具链

**Files:**
- Create: `.gitignore`, `go.mod`, `Makefile`, `internal/version/version.go`, `internal/version/version_test.go`, `cmd/pikpak/main.go`
- Test: `internal/version/version_test.go`

**Interfaces:**
- Produces: `version.Version string`（默认 `"0.1.0-m0"`，可被 `-ldflags -X` 覆盖）；`cmd/pikpak` 支持 `--version` 并退出 0。

- [ ] **Step 1: 初始化仓库**

```bash
cd /Users/fengyao/workspace/popeye/fnos-pikpak
git init
git add docs
git commit -m "docs: pikpak api reference and design"
```

若仓库已存在则跳过 `git init`，只提交 `docs/` 的当前状态。

- [ ] **Step 2: 写失败测试**

`internal/version/version_test.go`：

```go
package version

import "testing"

func TestVersionNotEmpty(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `go test ./internal/version`
Expected: FAIL，`no Go files` / 包不存在。

- [ ] **Step 4: 创建 `go.mod` 与版本包**

`go.mod`：

```
module fnos-pikpak

go 1.27
```

`internal/version/version.go`：

```go
package version

var Version = "0.1.0-m0"
```

- [ ] **Step 5: 创建入口与 Makefile**

`cmd/pikpak/main.go` 解析 `--version` 与 `--help`，打印后退出；其余参数暂不处理。

`Makefile` 目标：`build`（本机 darwin 调试）、`build-linux`（`CGO_ENABLED=0 GOOS=linux GOARCH=amd64` → `target/pikpak`）、`test`、`genicon`（`go run ./tools/genicon`）、`fpk`（依赖 `build-linux genicon`，调用 `tools/bin/fnpack build` 并把产物复制到 `dist/`）。`web` 目标在 Task 4 加入，`fpk` 对它的依赖也在 Task 4 追加。

`.gitignore`：`target/`、`dist/`、`tools/bin/`、`internal/webdist/dist/assets/`、`web/node_modules/`、`*.fpk`、`.DS_Store`。注意**不要**忽略 `internal/webdist/dist/index.html`——它作为 `go:embed` 的占位文件必须存在，否则新克隆的仓库 `go build ./...` 会失败。

- [ ] **Step 6: 验证**

Run: `go test ./... && go run ./cmd/pikpak --version`
Expected: PASS，输出 `0.1.0-m0`。

- [ ] **Step 7: 提交**

```bash
git add .gitignore go.mod go.sum Makefile internal/version cmd/pikpak
git commit -m "chore: bootstrap go module and make targets"
```

---

### Task 2: fnOS 包骨架与 fnpack 构建

**Files:**
- Create: `manifest`, `cmd/main`, `cmd/install_init` 等 8 个空脚本、`config/privilege`、`config/resource`、`app/ui/config`、`wizard/.gitkeep`、`tools/genicon/main.go`
- Test: `make fpk` 产物检查（shell 断言）

**Interfaces:**
- Produces: `make fpk` 在 `dist/` 下产出 `.fpk`（fnpack 自己的命名规则），Makefile 复制/重命名为 `dist/pikpak-0.1.0-m0-x86.fpk`；包内含 `manifest`、`cmd/main`、`target/pikpak`。

- [ ] **Step 1: 安装 fnpack**

```bash
mkdir -p tools/bin
curl -fsSL -o tools/bin/fnpack https://static2.fnnas.com/fnpack/fnpack-1.3.3-darwin-arm64
chmod +x tools/bin/fnpack
tools/bin/fnpack --help
```

Expected: 打印用法。（Linux 开发机换成 `fnpack-1.3.3-linux-amd64`。）

- [ ] **Step 2: 写图标生成器**

`tools/genicon/main.go`：用 `image/png` 画 256×256 与 64/128/256 三档图标（深蓝底、白色播放三角、圆角），写 `ICON.PNG`、`ICON_256.PNG`、`app/ui/images/icon_1.png`~`icon_3.png`。无第三方依赖。

- [ ] **Step 3: 写 manifest 与配置**

`manifest`（值照抄）：

```ini
appname=pikpak
version=0.1.0-m0
display_name=PikPak
desc=PikPak 文件、离线下载与播放（M0 验证版）。
source=thirdparty
platform=x86
maintainer=popeye
maintainer_url=https://github.com/
os_min_version=1.2.0401
desktop_uidir=ui
desktop_applaunchname=pikpak.main
ctl_stop=true
checkport=false
micro_app=true
disable_authorization_path=false
```

`config/privilege`：

```json
{"defaults":{"run-as":"package"}}
```

`config/resource`：

```json
{"api-scope":["trim.file.userAccess","trim.file.userAcl","trim.file.path","trim.system.getPlatformConfig"]}
```

`app/ui/config`：

```json
{
  ".url": {
    "pikpak.main": {
      "title": "PikPak",
      "icon": "images/icon_{0}.png",
      "type": "iframe",
      "protocol": "",
      "gatewayPrefix": "/app/pikpak",
      "gatewaySocket": "app.sock",
      "url": "/app/pikpak",
      "allUsers": true
    }
  }
}
```

- [ ] **Step 4: 写生命周期脚本**

`cmd/main`（POSIX sh，`chmod +x`）：`start` 已在运行返回 0，否则后台启动 `${TRIM_APPDEST}/pikpak --socket ${TRIM_APPDEST}/app.sock`，日志追加到 `${TRIM_PKGVAR}/logs/app.log`，写 PID 到 `${TRIM_PKGVAR}/pikpak.pid`；`stop` 发 TERM、等 10 秒、KILL、删 PID 与残留 socket；`status` 进程不在 → exit 3。其余 `cmd/*` 一律 `#!/bin/sh\nexit 0` 并 `chmod +x`。

- [ ] **Step 5: 构建并检查产物**

Run:

```bash
make genicon build-linux fpk
ls dist/*.fpk
unzip -l dist/*.fpk | grep -E 'manifest|cmd/main|target/pikpak|app/ui/config'
```

Expected: 一个 `.fpk`；四行 grep 全部命中。

- [ ] **Step 6: 提交**

```bash
git add manifest cmd config app wizard tools/genicon Makefile
git commit -m "feat: fnos package skeleton and fpk build"
```

---

### Task 3: 服务、身份中间件与健康检查

**Files:**
- Create: `internal/fnos/identity.go`, `internal/fnos/identity_test.go`, `internal/httpapi/server.go`, `internal/httpapi/server_test.go`
- Modify: `cmd/pikpak/main.go`

**Interfaces:**
- Produces:
  - `type Identity struct { UID int; Username string; IsAdmin bool }`
  - `func ExtractIdentity(h http.Header) (Identity, error)`；错误值 `ErrNoIdentity`、`ErrDuplicateIdentity`、`ErrBadIdentity`。
  - `func NewMux(deps Deps) *http.ServeMux`，注册 `GET /api/health`；`type Deps struct { Version string }`（Task 9/10/11 再逐项扩展字段）。
  - `func Start(socketPath string, h http.Handler) (stop func() error, err error)`：删除残留 socket，`net.Listen("unix", ...)`，`os.Chmod(socketPath, 0o600)`，返回关闭函数。

- [ ] **Step 1: 写失败测试**

`internal/fnos/identity_test.go`：

```go
package fnos

import (
	"errors"
	"net/http"
	"testing"
)

func TestExtractIdentity(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		_, err := ExtractIdentity(http.Header{})
		if !errors.Is(err, ErrNoIdentity) { t.Fatalf("got %v", err) }
	})
	t.Run("duplicate", func(t *testing.T) {
		h := http.Header{}
		h.Add("X-Trim-Userid", "1000")
		h.Add("X-Trim-Userid", "1001")
		_, err := ExtractIdentity(h)
		if !errors.Is(err, ErrDuplicateIdentity) { t.Fatalf("got %v", err) }
	})
	t.Run("bad", func(t *testing.T) {
		h := http.Header{"X-Trim-Userid": {"abc"}}
		_, err := ExtractIdentity(h)
		if !errors.Is(err, ErrBadIdentity) { t.Fatalf("got %v", err) }
	})
	t.Run("ok", func(t *testing.T) {
		h := http.Header{"X-Trim-Userid": {"1000"}, "X-Trim-Username": {"me"}, "X-Trim-Isadmin": {"1"}}
		id, err := ExtractIdentity(h)
		if err != nil || id.UID != 1000 || id.Username != "me" || !id.IsAdmin { t.Fatalf("%+v %v", id, err) }
	})
}
```

`internal/httpapi/server_test.go`：`GET /api/health` 无身份 → 401；带 `X-Trim-Userid: 1000` → 200 且 body 含 `"ok":true`；带两个身份头 → 400。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/fnos ./internal/httpapi`
Expected: 编译失败（符号不存在）。

- [ ] **Step 3: 实现**

- `ExtractIdentity`：取 `Header.Values("X-Trim-Userid")`；0 个 → `ErrNoIdentity`；>1 个 → `ErrDuplicateIdentity`；`strconv.Atoi` 失败或 ≤0 → `ErrBadIdentity`。`IsAdmin` 接受 `"1"`/`"true"`。
- `NewMux`：包装一个 `withIdentity` 中间件，把 `Identity` 放进 `context`；handler 里用 `IdentityFrom(ctx)` 取。健康检查不走身份中间件，单独注册在 `/api/health`。
- `Start`：`os.Remove(socketPath)` 忽略不存在；`net.Listen("unix", socketPath)`；`chmod 0600`；`http.Server{Handler: h}` 在 goroutine 中 `Serve`；返回的 stop 做 `Shutdown` + `Close` + 删除 socket。

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/fnos ./internal/httpapi`
Expected: PASS。

- [ ] **Step 5: 接进入口**

`cmd/pikpak/main.go`：新增 `--socket`（默认 `${TRIM_APPDEST}/app.sock`，环境变量缺失时用 `./app.sock`）、`--listen`（开发用 TCP，仅当显式传入）、`--dev-uid`。收到 SIGTERM/SIGINT 调用 stop。启动成功打印 `listening on <addr>`。

- [ ] **Step 6: 手动冒烟**

```bash
go build -o /tmp/pikpak-m0 ./cmd/pikpak
/tmp/pikpak-m0 --listen 127.0.0.1:8787 --dev-uid 1000 &
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8787/api/health
curl -s -H 'X-Trim-Userid: 1000' http://127.0.0.1:8787/api/health
kill %1
```

Expected: 第一次 401；第二次 200 且 `{"ok":true,...}`。

- [ ] **Step 7: 提交**

```bash
git add internal/fnos internal/httpapi cmd/pikpak
git commit -m "feat: unix socket server with fnos identity middleware"
```

---

### Task 4: 前端外壳与内嵌

**Files:**
- Create: `web/package.json`, `web/vite.config.ts`, `web/index.html`, `web/src/main.tsx`, `web/src/App.tsx`, `web/src/api.ts`, `internal/webdist/embed.go`, `internal/httpapi/web.go`
- Modify: `Makefile`（`web` 目标先 `npm ci` 后 `npm run build`，输出到 `internal/webdist/dist`）

**Interfaces:**
- Produces: `webdist.FS embed.FS`；`httpapi` 在 `/` 与未知非 `/api` 路径返回 `index.html`。
- 前端 `api.ts` 导出 `apiGet<T>(path): Promise<T>`，基址为 `new URL('api/', document.baseURI)`，自动带 `Content-Type: application/json`。

- [ ] **Step 1: 初始化前端**

```bash
mkdir -p web && cd web
npm create vite@latest . -- --template react-ts
npm install
npm install @trimjs/web-app
```

删除模板里的示例 CSS/资源，只留最小页面。`vite.config.ts`：`base: './'`，`build.outDir: '../internal/webdist/dist'`，`emptyOutDir: true`。

- [ ] **Step 2: 写失败测试**

`internal/httpapi/web_test.go`：`GET /` 返回 200 且 `Content-Type` 含 `text/html`；`GET /assets/x.js` 不 panic 且状态为 404 或 200；`GET /api/nope` 返回 404 JSON 而不是 HTML。

（测试需要 `internal/webdist/dist/index.html` 存在；Makefile 的 `test` 目标先跑 `web`。）

- [ ] **Step 3: 实现嵌入**

先写占位文件 `internal/webdist/dist/index.html`（内容 `<!doctype html><title>pikpak</title>`），保证未构建前端时也能编译。

`internal/webdist/embed.go`：

```go
package webdist

import "embed"

//go:embed all:dist
var FS embed.FS
```

`web.go` 用 `fs.Sub(webdist.FS, "dist")` 提供静态文件；非 `/api/` 且文件不存在时回退 `index.html`（HashRouter 实际很少用到，但保留）。`index.html` 加 `<base href="./">`。

- [ ] **Step 4: 前端最小页面**

`App.tsx`：标题、`/api/health` 状态、探针列表占位。`main.tsx` 用 `HashRouter`。

- [ ] **Step 5: 验证**

Run: `make web && go test ./internal/httpapi`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add web internal/webdist internal/httpapi/web.go internal/httpapi/web_test.go Makefile web/package-lock.json
git commit -m "feat: embedded react shell"
```

（`package-lock.json` 在 `web/` 下。）

---

### Task 5: 探针框架与报告脱敏

**Files:**
- Create: `internal/probe/probe.go`, `internal/probe/redact.go`, `internal/probe/redact_test.go`, `internal/probe/builtin.go`, `internal/httpapi/routes_probe.go`
- Modify: `internal/httpapi/server.go`

**Interfaces:**
- Produces:
  - `type Status string`，常量 `Pass`、`Fail`、`Warn`、`Skip`。
  - `type Result struct { Name string; Status Status; Detail string; Data map[string]any; RanAt time.Time }`
  - `type Probe interface { Name() string; Run(ctx context.Context) Result }`
  - `type Registry struct{}`；`(r *Registry) Add(p Probe)`、`(r *Registry) Run(ctx) []Result`（顺序执行，单个 panic 转成 FAIL）。
  - `func Redact(s string) string`：去掉 URL 查询串、`Bearer` 值、`access_token`/`refresh_token`/`device_code`/`pass_code_token` 的值。
  - API：`GET /api/probes`（名称列表）、`POST /api/probes/run`（运行全部，返回 `[]Result`）、`GET /api/report`（`application/json` 附件，文件名 `pikpak-m0-report.json`）。

- [ ] **Step 1: 写失败测试**

`internal/probe/redact_test.go` 至少覆盖：

```go
func TestRedact(t *testing.T) {
	in := `GET https://dl-a10b.mypikpak.com/file?expire=1&sign=x&userid=2 Authorization: Bearer abc.def`
	out := Redact(in)
	for _, bad := range []string{"sign=x", "userid=2", "abc.def"} {
		if strings.Contains(out, bad) { t.Fatalf("leaked %q in %q", bad, out) }
	}
	if !strings.Contains(out, "dl-a10b.mypikpak.com/file") { t.Fatalf("lost host: %q", out) }
}
```

再测 `access_token="..."`、`device_code: xxx` 形态。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/probe`
Expected: 编译失败。

- [ ] **Step 3: 实现**

- `Redact`：正则替换 `(?i)(bearer\s+)[\w.\-]+` → `$1<redacted>`；URL 查询串整体替换为 `?<redacted>`；`(?i)(access_token|refresh_token|device_code|pass_code_token|authorization)("?\s*[:=]\s*"?)[^&\s",]+` → `$1$2<redacted>`。
- `Registry.Run`：`defer recover()`，panic 时 `Result{Status: Fail, Detail: "panic: ..."}`。
- 路由：`/api/probes/run` 需要身份（复用中间件）；`/api/report` 需要身份，body 为 `{"uid":..., "results":[...], "version":...}`，`Content-Disposition: attachment`。

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/probe ./internal/httpapi`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/probe internal/httpapi/routes_probe.go internal/httpapi/server.go
git commit -m "feat: probe registry and redacted report export"
```

---

### Task 6: 飞牛 Open API 客户端与文件授权探针

**Files:**
- Create: `internal/fnos/openapi.go`, `internal/fnos/openapi_test.go`, `internal/fnos/authorize.go`, `internal/fnos/authorize_test.go`, `internal/fnostest/gateway.go`, `internal/httpapi/routes_fs.go`
- Modify: `internal/probe/builtin.go`（注册探针）、`internal/httpapi/server.go`（`Deps` 增加 `Fnos *fnos.Client`）、`web/src/App.tsx`

**Interfaces:**
- Produces:
  - `type Client struct { SocketPath, AppName string; Token func() string }`
  - `func (c *Client) Call(ctx context.Context, req string, data, out any) error`
  - `func (c *Client) GetPlatformConfig(ctx context.Context) (PlatformConfig, error)`；`PlatformConfig{Language, Version string}`
  - `func (c *Client) GetUserAccessibleFolders(ctx context.Context, uid int) ([]string, error)`
  - `func (c *Client) CheckUserACL(ctx context.Context, uid int, paths []string) ([]ACLResult, error)`；`ACLResult{Path string; Readable, Writable, Deletable bool}`
  - `func (c *Client) ConvertPath(ctx context.Context, paths []string, lang string) ([]ConvertedPath, error)`
  - `func ValidateWritableACL(results []ACLResult) error`（任一 `Writable=false` 返回错误）
  - `func IsUnderAny(path string, roots []string) bool`：`path` 必须绝对、`filepath.Clean` 后不含 `..`，且等于某个 root 或以 `root+"/"` 开头（防止 `/vol1/a` 匹配 `/vol1/ab`）；root 为空数组时返回 false。若路径存在，先对路径和 roots 做 `filepath.EvalSymlinks` 再比较；解析失败按原路径比较。
  - API：`POST /api/fs/probe-acl`，body `{"paths":["/vol1/..."]}`，返回授权集合、ACL 结果、semanticPath、以及每个路径的 `IsUnderAny` 结果。

- [ ] **Step 1: 写 fake 服务**

`internal/fnostest/gateway.go`：`func StartGateway(t *testing.T, handler func(req map[string]any) (int, string)) (socketPath string)`。在 `t.TempDir()` 建 Unix socket，校验 `Authorization` 为 `Bearer test-token`，回写 fnOS 信封 `{"reqId":...,"code":0,"msg":"","data":...}`。供 openapi 测试与 httpapi 集成测试复用（独立包，避免 `testing` 进入生产包）。

`authorize_test.go` 覆盖 `IsUnderAny`：

```go
{ "/vol1/a/b", []string{"/vol1/a"}, true }
{ "/vol1/ab", []string{"/vol1/a"}, false }
{ "/vol1/a/../b", []string{"/vol1/a"}, false }
{ "relative/path", []string{"/vol1/a"}, false }
{ "/vol1/a", []string{"/vol1/a"}, true }
```

再加一个符号链接用例：`t.TempDir()` 下建 `root/`、`outside/secret.txt`，并 `os.Symlink("../outside", root/link)`；断言 `IsUnderAny(root/link/secret.txt, []string{root}) == false`（目标必须真实存在，`EvalSymlinks` 才有结果）。

- [ ] **Step 2: 写失败测试**

`openapi_test.go` 断言：

- `Call` 发送 `appName`、`req`、`data`，且 token 每次从 `Token()` 现取（改 token 后下一次请求用新值）。
- `code!=0` 时返回含 `msg` 的错误。
- 响应超时（fake 睡 2s，客户端 200ms）返回错误。
- `CheckUserACL` 传数组路径并解析三个布尔值。

- [ ] **Step 3: 运行确认失败**

Run: `go test ./internal/fnos`
Expected: 编译失败。

- [ ] **Step 4: 实现**

HTTP over unix：`http.Transport{DialContext: func(...) { return net.Dial("unix", socketPath) }}`，URL 固定 `http://localhost/api/v1/trimapp`。`Token` 为 nil 时读 `os.Getenv("TRIM_API_TOKEN")`；为空直接返回错误，不发请求。`reqId` 用 `crypto/rand` 16 字节 hex。超时 5 秒。

- [ ] **Step 5: 前端接入选择器**

`App.tsx` 增加"选择飞牛目录"按钮：

```ts
import { TrimApp } from '@trimjs/web-app';
const sdk = new TrimApp();
const res = await sdk.pickUserFile({ directory: true });
```

把 `res?.data` 路径 POST 到 `POST /api/fs/probe-acl`，展示返回的授权集合、ACL、semanticPath；`sdk.isStandaloneWeb` 为 true 时提示"请在飞牛桌面内打开"。

- [ ] **Step 6: 运行测试与构建**

Run: `go test ./... && make web`
Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add internal/fnos internal/httpapi/routes_fs.go web/src/App.tsx
git commit -m "feat: fnos open api client and acl probe"
```

---

### Task 7: 网关行为探针

**Files:**
- Create: `internal/httpapi/routes_gateway.go`
- Modify: `internal/httpapi/server.go`（记录原始请求元数据）、`web/src/App.tsx`

**Interfaces:**
- Produces: `GET /api/gateway/info` → `{path, raw_query, origin, host, forwarded_headers: {name: count}, identity: {...}}`，其中 header 只列 `x-trim-*` 与 `origin`/`referer`/`x-forwarded-*`，值经 `probe.Redact`。`POST /api/gateway/forged` → 记录本次请求是否出现重复身份头，返回 `{duplicate_detected, chosen_uid?}`；该接口永远 400 或 200，不执行业务。

- [ ] **Step 1: 写失败测试**

`server_test.go` 追加：带 `X-Trim-Userid: 1000` 请求 `/api/gateway/info`，JSON 的 `path` 等于请求路径，`forwarded_headers["X-Trim-Userid"]` 为 1；不带身份头 → 401。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/httpapi -run Gateway`
Expected: 404 或编译失败。

- [ ] **Step 3: 实现**

`/forged` 路由绕过身份中间件（注册在未包装的 mux 上），只统计 `Header.Values("X-Trim-Userid")` 的个数并返回，**不解析、不使用**其中的 uid。前端按钮用 `fetch('api/gateway/forged', {headers: {'X-Trim-Userid': '99999'}})` 触发，展示结果。

- [ ] **Step 4: 前端展示**

"网关信息"卡片显示 path、origin、每个头的出现次数；"伪造身份头测试"按钮显示 `duplicate_detected`。副本可一键复制。

- [ ] **Step 5: 验证**

Run: `go test ./... && make web`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/httpapi/routes_gateway.go internal/httpapi/server.go internal/httpapi/server_test.go web/src/App.tsx
git commit -m "feat: gateway behavior probes"
```

---

### Task 8: Range 流式探针

**Files:**
- Create: `internal/stream/range.go`, `internal/stream/range_test.go`, `internal/httpapi/routes_stream.go`
- Modify: `web/src/App.tsx`

**Interfaces:**
- Produces:
  - `func ParseRange(header string, size int64) (start, end int64, err error)`；`ErrRangeNotSatisfiable`、`ErrInvalidRange`。
  - `func ServeFile(w http.ResponseWriter, r *http.Request, path string, size int64)`：支持 GET/HEAD、单段 Range、206、416、`Accept-Ranges`。
  - `func EnsureProbeFile(dir string, size int64) (path string, err error)`：生成 `<dir>/probe.bin`，内容为按块写入的确定性字节，已存在且大小一致则复用。
  - API：`GET/HEAD /api/stream-probe`（文件大小 `?size=`，默认 512 MiB，上限 2 GiB）。

- [ ] **Step 1: 写失败测试**

`range_test.go` 表驱动：

```go
{ "bytes=0-1023", 1024, 0, 1023, nil }
{ "bytes=1024-", 4096, 1024, 4095, nil }
{ "bytes=-512", 4096, 3584, 4095, nil }
{ "bytes=5000-6000", 4096, 0, 0, ErrRangeNotSatisfiable }
{ "bytes=abc", 4096, 0, 0, ErrInvalidRange }
{ "", 4096, 0, 4095, nil }          // 无 Range 视为整段
```

再加 handler 测试：206 的 `Content-Range` 正确；416 带 `Content-Range: bytes */4096`；HEAD 无 body。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/stream`
Expected: 编译失败。

- [ ] **Step 3: 实现**

按上表实现；多段 Range（逗号）返回 416（M0 不需要 multipart/byteranges，浏览器一般发单段）。handler 用 `io.NewSectionReader` + `http.ServeContent` 或手写 `CopyN`；手写更容易保证 206 头。流式写，不 `ReadAll`。

- [ ] **Step 4: 前端测速面板**

"Range 探针"卡片：按钮顺序发 4 个请求（`bytes=0-1023`、中间 1 MiB、尾部 1 KiB、超出 EOF），显示状态码、`Content-Range`、首字节耗时（TTFB）、总耗时；再发一次不带 Range 的 HEAD。结果可复制。

- [ ] **Step 5: 验证**

Run: `go test ./internal/stream ./internal/httpapi && make web`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/stream internal/httpapi/routes_stream.go web/src/App.tsx
git commit -m "feat: range streaming probe"
```

---

### Task 9: SQLite 存储与凭据加密

**Files:**
- Create: `internal/store/store.go`, `internal/store/crypto.go`, `internal/store/migrate.go`, `internal/store/store_test.go`
- Modify: `cmd/pikpak/main.go`（打开 `${TRIM_PKGVAR}/pikpak.db`，失败则退出 1）、`internal/httpapi/server.go`（`Deps` 增加 `Store *store.Store`）

**Interfaces:**
- Produces:
  - `func Open(dbPath, keyPath string) (*Store, error)`；`(*Store).Close() error`
  - `func LoadOrCreateKey(path string) ([]byte, error)`（32 字节，0600，O_EXCL）
  - `func Encrypt(key, aad, plaintext []byte) ([]byte, error)` / `func Decrypt(key, aad, ciphertext []byte) ([]byte, error)`
  - `type Credential struct { UID int; AuthType, ClientID string; AccessToken, RefreshToken, DeviceID string; AccessExpiresAt, RefreshExpiresAt time.Time; Scopes string }`
  - `(*Store).PutCredential(ctx, Credential) error` / `(*Store).GetCredential(ctx, uid int) (Credential, error)`
  - `(*Store).PutAuthFlow(ctx, Flow) error` / `(*Store).GetAuthFlow(ctx, id string) (Flow, error)`
  - `type Flow struct { ID string; UID int; DeviceCode, UserCode, VerifyURL string; IntervalSec int; ExpiresAt time.Time; Status, Error string }`

- [ ] **Step 1: 写失败测试**

`store_test.go`：

- 同一 uid 写入两次 → 读到最后一次（UPSERT）。
- 不同 uid 互不影响。
- `GetCredential` 返回的明文与写入一致。
- 用错误 AAD（`uid=1001`）解密 → 失败。
- DB 文件权限 0600；key 文件 0600 且内容 32 字节。
- 迁移跑两次不报错，`schema_migrations` 只有一行。
- 关闭后重开，数据仍在。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/store`
Expected: 编译失败。

- [ ] **Step 3: 实现**

`go get modernc.org/sqlite`。`Open`：`sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")`，`SetMaxOpenConns(1)`，执行迁移，`os.Chmod(dbPath, 0o600)`。

迁移 1：创建 `schema_migrations`、`users`、`credentials`、`auth_flows`（字段与设计文档 §8 一致，M0 先不建传输表）。

`crypto.go`：AES-256-GCM；`nonce(12) || ciphertext`；AAD 由调用方拼 `strconv.Itoa(uid) + ":" + field`。

凭据写入时分别加密 `access_token`、`refresh_token`、`device_id` 三列。

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/store -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/store cmd/pikpak go.mod go.sum
git commit -m "feat: sqlite store with aes-gcm credential encryption"
```

---

### Task 10: OAuth 设备码

**Files:**
- Create: `internal/pikpak/client.go`（骨架）、`internal/pikpak/auth.go`、`internal/pikpak/auth_test.go`, `internal/httpapi/routes_auth.go`
- Modify: `internal/httpapi/server.go`（`Deps` 增加 `Sessions *pikpak.SessionStore`）、`web/src/App.tsx`

**Interfaces:**
- Produces:
  - `type Client struct { BaseUser, BaseDrive string; HTTP *http.Client; ClientID, ClientVersion, DeviceID string }`；`type ClientOptions struct { BaseUser, BaseDrive, ClientID, ClientVersion, DeviceID string; HTTP *http.Client }`；`func NewClient(o ClientOptions) *Client`。Task 11 在这个类型上加方法与 `Session` 字段。
  - `type DeviceFlow struct { DeviceCode, UserCode, VerifyURL string; Interval, ExpiresIn int }`
  - `func (c *Client) StartDeviceFlow(ctx) (DeviceFlow, error)`
  - `func (c *Client) PollDeviceToken(ctx, deviceCode string) (Token, error)`；`Token{AccessToken, RefreshToken, ExpiresIn int, Sub string}`；`ErrAuthorizationPending`、`ErrSlowDown`、`ErrAccessDenied`、`ErrExpiredToken`。
  - `func (c *Client) Refresh(ctx, refreshToken string) (Token, error)`
  - `type Session struct { mu sync.Mutex; ... }`；`(s *Session) Token(ctx) (string, error)` 单飞刷新；`(s *Session) ReauthRequired() bool`
  - `type SessionStore struct{}`；`func NewSessionStore(st *store.Store, client *Client) *SessionStore`；`(s *SessionStore) Get(ctx, uid int) (*Session, error)`（未绑定返回 `ErrNotBound`，已绑定则懒加载并复用）；`(s *SessionStore) ClientFor(ctx, uid int) (*Client, error)`（复制基础配置、绑定该 uid 的 Session，供 Task 11/12 使用）
  - API：`POST /api/auth/device`、`GET /api/auth/flow/{id}`、`POST /api/auth/logout`、`GET /api/me`。

- [ ] **Step 1: 写失败测试**

用 `httptest.Server` 模拟 `user.mypikpak.com`：

- `StartDeviceFlow` 解析 `device_code`/`user_code`/`verification_uri_complete`/`interval`/`expires_in`。
- `PollDeviceToken`：先返回 400 `authorization_pending` → `ErrAuthorizationPending`；再返回 400 `slow_down` → `ErrSlowDown`；再成功 → Token。
- `Refresh` 返回新 refresh token（轮换）。
- `Session.Token`：100 个 goroutine 并发、令牌剩余 1 分钟 → 上游只收到 1 次 refresh（fake 计数器）。
- 上游返回 `4126 invalid_grant` → `ReauthRequired()==true`，后续 `Token()` 返回 `ErrReauthRequired`。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/pikpak -run 'Auth|Session'`
Expected: 编译失败。

- [ ] **Step 3: 实现**

- 设备码请求头：`X-Client-Id`、`X-Client-Version: 1.0.0`、`X-Device-Id`（32 hex，来自 store）、`Content-Type: application/json`。
- Scope 常量照抄 Global Constraints。
- 轮询在 `routes_auth.go` 的后台 goroutine：按 interval 睡，`expires_in` 到期停止；成功后 `store.PutCredential` + 建 `Session`，flow 状态置 `bound`；错误落 `error` 字段。
- `Session`：提前 10 分钟刷新；`singleflight` 用 mutex + `refreshing` channel 实现，不引库。
- 持久化顺序：先加密写 refresh token（事务），再启用新 access token。

- [ ] **Step 4: 前端绑定页**

`/api/me` 未绑定时显示"绑定 PikPak"按钮；点击后显示 `user_code`、可点击的 `verification_uri_complete`、倒计时；每 1.5 秒轮询 flow。二维码 M0 不做（链接足够）。

- [ ] **Step 5: 验证**

Run: `go test ./... && make web`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/pikpak internal/httpapi/routes_auth.go internal/httpapi/server.go web/src/App.tsx
git commit -m "feat: oauth device flow with single-flight refresh"
```

---

### Task 11: PikPak 只读客户端与上游探针

**Files:**
- Create: `internal/pikpak/probes.go`, `internal/pikpak/client_test.go`
- Modify: `internal/pikpak/client.go`（加 `Session TokenSource` 字段与只读方法）、`internal/probe/builtin.go`、`internal/httpapi/routes_probe.go`（注册 PikPak 探针需要用户会话）

**Interfaces:**
- Produces:
  - `type TokenSource interface { Token(ctx context.Context) (string, error) }`
  - 在 `Client` 上增加字段 `Session TokenSource`
  - `type Client struct { BaseDrive, BaseUser string; HTTP *http.Client; Session TokenSource; ClientID, ClientVersion, DeviceID string }`
  - `func (c *Client) ListFiles(ctx, parentID string, o ListOptions) (FilePage, error)`；`ListOptions{PageToken string; Limit int; Order string; Filters string}`；`FilePage{Items []File; NextPageToken string}`；`File{ID, ParentID, Name, Kind string; Size int64; Phase, Category, MimeType, ModifiedTime string}`
  - `func (c *Client) About(ctx) (About, error)`；`About{QuotaLimit, QuotaUsage int64; ExpiresAt string}`
  - `func (c *Client) VIP(ctx) (VIP, error)`；`VIP{Type, Expire string}`
  - `func (c *Client) Events(ctx, pageToken string) (FilePage, error)`
  - `func (c *Client) SearchCandidates(ctx, q string) []Result`（M0 专用：依次尝试候选端点，记录状态码与响应形状，不解析业务）
  - 探针名：`pikpak.list`、`pikpak.quota`、`pikpak.vip`、`pikpak.order`、`pikpak.search`

- [ ] **Step 1: 写失败测试**

- 列表请求 URL 含 `parent_id`、`limit`、`page_token`、`filters`（JSON 字符串原样）；响应解析 `files[].size` 字符串 → int64。
- 401 一次 → 触发 Session 刷新 → 重试成功（fake 计数）。
- 4126 → 不重试，`ErrReauthRequired`。
- 429 + `Retry-After: 0` → 退避后成功（测试里退避基数调成 1ms）。
- `order` 探针发 4 个请求，返回每种的 HTTP 状态与首条名称。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/pikpak -run 'Client|List'`
Expected: 编译失败。

- [ ] **Step 3: 实现**

- 统一 `do(ctx, method, url, body, out)`：加公共头、调用 `Session.Token`、按 §7.10 的错误码表处理、`User-Agent` 固定。
- 限流：每用户令牌桶 10/s 突发 20（`golang.org/x/time/rate`，或手写）。
- `SearchCandidates` 依次尝试：
  1. `GET /drive/v1/files?filters={"name":{"contains":"<q>"}}&limit=10`
  2. `GET /drive/v1/files?q=<q>&limit=10`
  3. `GET /drive/v1/search?q=<q>&limit=10`
  4. `GET /drive/v1/files:search?q=<q>&limit=10`
  记录 `{url_path, status, body_prefix(≤200 字符，脱敏), looks_like_file_list}`。M0 报告据此定 v2 搜索实现。

- [ ] **Step 4: 探针接入页面**

探针卡片显示：根目录前 10 项、配额/会员、4 种排序的第一条名称、4 个搜索候选的结果。未绑定账号的探针返回 `Skip` 并提示先绑定。

- [ ] **Step 5: 验证**

Run: `go test ./... && make web`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/pikpak internal/probe/builtin.go internal/httpapi/routes_probe.go go.mod go.sum
git commit -m "feat: pikpak read client and capability probes"
```

---

### Task 12: GCID 与上传探针

**Files:**
- Create: `internal/pikpak/gcid.go`, `internal/pikpak/gcid_test.go`, `internal/pikpak/upload.go`, `internal/pikpak/upload_test.go`, `internal/httpapi/routes_upload.go`
- Modify: `web/src/App.tsx`

**Interfaces:**
- Produces:
  - `func GCID(r io.Reader, size int64) (string, error)`：小写 hex；`func GCIDFile(path string) (string, error)`。
  - `type UploadTicket struct { File *File; Resumable *Resumable }`；`Resumable{Provider string; Params map[string]string}`
  - `func (c *Client) RequestUpload(ctx, in UploadRequest) (UploadTicket, error)`；`UploadRequest{Name string; Size int64; GCID, ParentID string}`
  - `func (c *Client) CompleteUpload(ctx, ticket UploadTicket, src io.Reader) error`：`PROVIDER_ALIYUN` 时用 OSS SDK；`Resumable==nil` 视为秒传。
  - `func (c *Client) WaitFileComplete(ctx, fileID string, timeout time.Duration) (File, error)`
  - API：`POST /api/upload/probe`，body `{local_path, parent_id}`；`GET /api/upload/probe/config` 返回测试目标文件夹 id 与可用本地路径。

- [ ] **Step 1: 写 GCID 失败测试**

```go
func TestGCIDVectors(t *testing.T) {
	cases := []struct{ name string; size int; want string }{
		{"empty", 0, "da39a3ee5e6b4b0d3255bfef95601890afd80709"},
		{"abc", 3, "0d3ced9bec10a777aec23ccc353a8c08a633045e"},
		{"256KiB-zeros", 256 * 1024, "706fd5159f317e97543268985d8945dbd223206c"},
		{"256KiB+1-zeros", 256*1024 + 1, "d00cafa958f037c1093d8f45c7675caf6eacacc1"},
	}
	...
}
```

再测 200 MiB 全零 → `f509949e535970831951d44769e16c672d0f17f2`（验证块大小翻倍到 512 KiB），用 `io.LimitReader(zeroReader{}, n)` 避免分配 200 MiB；测试文件里定义 `type zeroReader struct{}`，`Read(p []byte) (int, error)` 返回 `len(p), nil`。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/pikpak -run GCID`
Expected: 编译失败。

- [ ] **Step 3: 实现 GCID**

```go
func blockSize(size int64) int64 {
	p := int64(0x40000)
	for size/p > 0x200 && p < 0x200000 {
		p <<= 1
	}
	return p
}
```

按 Global Constraints 的算法；`GCIDFile` 流式读取，不整文件进内存。

- [ ] **Step 4: 写上传测试**

- `RequestUpload` 的 JSON 含 `upload_type=UPLOAD_TYPE_RESUMABLE`、`hash` 为大写、`folder_type=NORMAL`。
- 响应 `resumable==nil` 时 `CompleteUpload` 直接返回，不访问 OSS。
- 响应含 `resumable.params` 时，用 fake OSS 服务器（`httptest`）验证：PUT 带 `X-OSS-Security-Token`、对大文件按 ≥6 MiB 分片、`CompleteMultipartUpload` 的 XML 解析失败但 HTTP 200 视为成功。
- 大小 0 的文件：`size=0`、`hash` 非空。

（OSS 签名由 SDK 负责；fake 服务器只校验路径、头、方法，不校验签名。）

- [ ] **Step 5: 实现上传**

`go get github.com/aliyun/aliyun-oss-go-sdk/oss`。`PROVIDER_ALIYUN`：endpoint 规范化（若含 `vip-lixian` 等前缀，改写为 `https://mypikpak.net` 风格的主机，照 OpenList 规则），`oss.New` 传 STS token，按大小选 `PutObject` 或分片（6 MiB、并发 3）。其它 provider → 返回 `ErrUnsupportedProvider` 并把 params 键名（不含值）记入报告。

- [ ] **Step 6: 前端上传探针**

提供两个按钮：
- "上传生成文件"：后端在 `${TRIM_PKGTMP}` 生成 20 MiB 确定性文件并上传到用户指定文件夹 id（输入框，默认根目录）。
- "上传飞牛文件"：`pickUserFile({directory:false})` 选文件，POST 路径。

显示阶段（GCID 耗时、秒传/需要上传、分片数、完成耗时、最终 phase、file id）。

路径规则：`/api/upload/probe` 收到用户选择的 `local_path` 时，先取 `getUserAccessibleFolders(uid)`，用 `IsUnderAny(local_path, roots)` 且 `checkUserACL(uid, local_path).Readable` 校验，任一失败返回 403；服务端自己生成的 20 MiB 文件放在 `${TRIM_PKGTMP}`，不走这两个校验（不是用户数据），只允许固定文件名 `pikpak-probe-20m.bin`。

- [ ] **Step 7: 验证**

Run: `go test ./... && make web`
Expected: PASS（fake OSS 与 GCID 向量）。

- [ ] **Step 8: 提交**

```bash
git add internal/pikpak/gcid.go internal/pikpak/gcid_test.go internal/pikpak/upload.go internal/pikpak/upload_test.go internal/httpapi/routes_upload.go web/src/App.tsx go.mod go.sum
git commit -m "feat: gcid and resumable upload probe"
```

---

### Task 13: M0 打包、安装说明与报告模板

**Files:**
- Create: `docs/m0-report.md`, `docs/install-m0.md`
- Modify: `Makefile`（`m0` 目标）、`README.md`（新建，指向文档）

**Interfaces:**
- Produces: `make m0` → `dist/pikpak-0.1.0-m0-x86.fpk`（安装包）+ `docs/m0-report.md`（人工填写模板）。

- [ ] **Step 1: 写安装说明**

`docs/install-m0.md`：如何把 `.fpk` 传到飞牛、在应用中心手动安装、打开应用、绑定 PikPak、运行全部探针、导出报告；如何看 `${TRIM_PKGVAR}/logs/app.log`（给出 fnOS 上的路径示例）；如何卸载。

- [ ] **Step 2: 写报告模板**

`docs/m0-report.md` 表格，逐项对应六个未知：

| 编号 | 探针 | 结论选项 | 证据字段 |
|---|---|---|---|
| 1 | 网关路径前缀 | 带前缀 / 不带前缀 | `gateway.path` |
| 2 | 网关身份头伪造 | 被剥离 / 重复出现 / 覆盖 | `gateway.forged` |
| 3 | 网关 Origin | 值 | `gateway.origin` |
| 4 | Range 流式 | 支持 206 / 缓冲 / 失败 | `stream.*` 的 TTFB 与状态 |
| 5 | pickUserFile + ACL | 路径形态、ACL 生效耗时、semanticPath | `fs.*` |
| 6 | 搜索端点 | 命中候选编号 / 全部失败 | `pikpak.search` |
| 7 | 列表排序 | 哪个 order 生效 | `pikpak.order` |
| 8 | REST 上传 | 秒传/分片成功/失败及失败点 | `upload.*` |

- [ ] **Step 3: 构建**

Run: `make m0`
Expected: 生成 fpk；`unzip -l` 含二进制；二进制 `--version` 输出 `0.1.0-m0`。

- [ ] **Step 4: 本机冒烟**

```bash
./target/pikpak --listen 127.0.0.1:8787 --dev-uid 1000  # 若为 darwin 调试构建
curl -s http://127.0.0.1:8787/api/health
```

Expected: `{"ok":true,...}`（无身份头返回 401 是预期的；用 `-H 'X-Trim-Userid: 1000'`）。

- [ ] **Step 5: 提交**

```bash
git add docs/install-m0.md docs/m0-report.md Makefile README.md
git commit -m "docs: m0 install guide and report template"
```

`dist/` 在 `.gitignore` 里，不提交 fpk。

---

## 交接给用户（M0 执行后的动作）

1. 把 `dist/pikpak-0.1.0-m0-x86.fpk` 交给用户安装（用户自行安装）。
2. 用户在应用内绑定 PikPak，点"运行全部探针"，导出 JSON 报告。
3. 把报告和 `docs/m0-report.md` 的结论填回设计文档 §17，然后写 M1 计划。

M0 的完成标准不是"功能可用"，而是：八个探针全部有明确结论，且每个 FAIL 都有可执行的降级方案。

---

## Self-Review 记录

- **Spec coverage（M0 相关）**：网关路径/身份头/Origin（Task 7）、Range 流式（Task 8）、pickUserFile/ACL（Task 6）、搜索（Task 11）、排序（Task 11）、REST 上传（Task 12）、打包安装（Task 2、13）。M1–M6 明确不在本计划内。
- **Step scan**：每个实现步骤给了文件、签名或算法；测试步骤给了断言；没有"处理边界情况"这类空话。
- **Type consistency**：`Identity`、`ACLResult`、`Result`、`Credential`、`Flow`、`Token`、`File`、`UploadTicket` 在首次定义后保持一致引用；`Session` 在 Task 10 定义、Task 11 消费。
- **Review Focus 对应**：重复身份头 → Task 3；路径/ACL 越权 → Task 6（`ValidateWritableACL`）+ Task 13 报告项；非法 Range → Task 8；并发刷新 → Task 10；GCID 边界与空文件 → Task 12。
- **Proportion**：计划约 13 个任务，每个 4–8 步；未展开完整实现代码，只固定算法、签名与测试值。