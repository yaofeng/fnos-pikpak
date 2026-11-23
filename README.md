# PikPak for fnOS（飞牛）

飞牛 NAS 的原生 PikPak 应用：用飞牛账号打开网页，即可浏览/管理自己的 PikPak 文件、离线下载、上传下载与在线播放。每个飞牛用户绑定自己的 PikPak 账号，互相看不到对方的文件、任务与设置。

当前版本：**1.0.1**。

## 功能

- **文件**：浏览、搜索、类型筛选、新建文件夹、重命名、移动（拖拽）、复制、删除。
- **离线任务**：磁力/URL 新建任务，按状态筛选、重试、删除（可选同时删除文件）。
- **传输**：PikPak → 飞牛下载（只能写授权目录）、飞牛 → PikPak 上传；进度、速度、暂停/继续/取消、重名策略。
- **回收站**：还原、永久删除、清空。
- **播放**：内置播放器，Range 代理让拖动进度条直接由 CDN 响应；签名下载地址不出后端。不转码，编码不支持时提示下载后本地播放。
- **设置与诊断**：重名策略、播放模式、界面语言（中文/English）、全局并发、授权目录、版本与最近错误摘要。

## 安装

1. 从 [Releases](../../releases) 下载 `pikpak-<版本>-x86.fpk`，或本地 `make release`。
2. 飞牛 → 应用中心 → 手动安装。
3. 从桌面打开 **PikPak**，按提示绑定 PikPak 账号。

完整步骤、排障与**真机验证清单**见 [`docs/install.md`](docs/install.md)。

## 本地开发

```bash
make dev         # 构建前端 + 本机二进制，监听 http://127.0.0.1:8787
make test        # Go 测试
cd web && npm test   # 前端测试
make web         # 只构建前端（产物被 go:embed 进二进制）
```

`make dev` 用 `--dev-uid 1000` 模拟一个飞牛用户，绑定走真实的 OAuth 设备码流程。数据存在 `./.data/`（已 gitignore）。本机没有飞牛网关，所以：

- `fnos.*` 相关接口返回 503（飞牛文件服务未配置），文件列表/任务/播放/设置照常可用；
- 下载到本机时不再限制在授权目录（仅开发模式如此），但必须是绝对路径。

不带 `--listen` 时只监听 `${TRIM_APPDEST}/app.sock`，与生产一致。

## 发布

推送 `1.0.0` 或 `v1.0.0` 形式的 tag 会触发 [`.github/workflows/build-fpk.yml`](.github/workflows/build-fpk.yml)：

1. 从 tag 解析版本（也接受 `1.0.0-1`），校验格式；
2. `go test ./...`，构建前端与 linux/amd64 二进制；
3. 把版本写进 manifest 的 `version`、二进制 `--version`，包名为 `pikpak-<version>-x86.fpk`；
4. 校验包内版本后上传 Artifact，并创建 GitHub Release。

打包前请先在真实设备上走一遍 [`docs/install.md`](docs/install.md) 的真机验证清单。

CI 使用固定版本的 fnpack 并校验 SHA256。包默认未签名；在仓库 Secrets 配置 `FN_DEVELOPER_KEY` 与 `FN_DEVELOPER_CERT`（飞牛开发者证书 PEM）后即为签名包。本地构建同样可以指定版本：

```bash
make fpk VERSION=1.2.3                # 未签名
make fpk VERSION=1.2.3 FN_KEY=dev.key FN_CERT=dev.crt   # 签名
```

## 设计文档

- 设计：[`docs/superpowers/specs/2026-11-21-fnos-pikpak-design.md`](docs/superpowers/specs/2026-11-21-fnos-pikpak-design.md)
- v1 实现计划：[`docs/superpowers/plans/2026-11-22-fnos-pikpak-v1.md`](docs/superpowers/plans/2026-11-22-fnos-pikpak-v1.md)
- PikPak 接口记录：[`docs/pikpak-api.md`](docs/pikpak-api.md)
- 早期 M0 验证：[`docs/m0-report.md`](docs/m0-report.md)

## 结构

```
cmd/main              fnOS 生命周期脚本（start/stop/status）
cmd/pikpak            Go 入口（Unix socket + 内嵌前端）
internal/fnos         身份头、Open API 客户端、路径授权
internal/pikpak       PikPak 客户端（OAuth、文件、离线任务、上传、下载）
internal/store        SQLite + AES-256-GCM 凭据存储、任务与设置
internal/transfer     每用户 FIFO 传输队列
internal/playtoken    播放用 HMAC 令牌
internal/httpapi      HTTP 路由（文件、任务、传输、播放代理、设置、诊断）
web                   React + Vite 前端
```

## 安全约定

- 生产环境只监听 `${TRIM_APPDEST}/app.sock`（0600），不监听 TCP。
- 身份只来自飞牛统一网关注入的 `X-Trim-Userid`；缺失 401，重复 400，绝不从请求参数取 uid。
- 不以 root 运行（`run-as: package`）。
- 令牌、设备码、签名下载地址不进日志、前端或错误提示详情；数据库与密钥为 0600。
- 默认代理播放：签名地址只留在后端；切换为"跳转"模式时浏览器会直连 PikPak CDN。
- 下载只允许写入该飞牛用户有权限的目录；上传只允许读取这些目录。
