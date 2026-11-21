# PikPak for fnOS（飞牛）

飞牛 NAS 的原生 PikPak 应用：用飞牛账号打开网页，即可浏览/管理自己的 PikPak 文件、离线下载、上传下载与在线播放。每个飞牛用户绑定自己的 PikPak 账号，互相看不到对方的文件。

当前状态：**M0 验证版**。M0 不提供完整文件管理，而是一个可安装的诊断应用，用来在真机上确认设计里六个未决前提（网关行为、Range 流式、文件授权、搜索、排序、REST 上传）。

## 快速开始

```bash
make m0          # 构建 dist/pikpak-<version>-x86.fpk
make test        # Go 测试
make web         # 只构建前端（产物被 go:embed 进二进制）
```

然后把 `.fpk` 装到飞牛（x86）上，按 [`docs/install-m0.md`](docs/install-m0.md) 操作，按 [`docs/m0-report.md`](docs/m0-report.md) 填结论。

## 设计文档

- 设计：[`docs/superpowers/specs/2026-11-21-fnos-pikpak-design.md`](docs/superpowers/specs/2026-11-21-fnos-pikpak-design.md)
- M0 实现计划：[`docs/superpowers/plans/2026-11-21-fnos-pikpak-m0.md`](docs/superpowers/plans/2026-11-21-fnos-pikpak-m0.md)
- PikPak 接口记录：[`docs/pikpak-api.md`](docs/pikpak-api.md)

## 结构

```
cmd/main              fnOS 生命周期脚本（start/stop/status）
cmd/pikpak            Go 入口（Unix socket + 内嵌前端）
internal/fnos         身份头、Open API 客户端、路径授权
internal/pikpak       PikPak 客户端（OAuth、只读接口、GCID、上传）
internal/store        SQLite + AES-256-GCM 凭据存储
internal/probe        探针与报告
internal/httpapi      HTTP 路由
internal/stream       Range 流式探针
web                   React + Vite 前端
```

## 安全约定

- 生产环境只监听 `${TRIM_APPDEST}/app.sock`（0600），不监听 TCP。
- 身份只来自飞牛统一网关注入的 `X-Trim-Userid`；缺失 401，重复 400，绝不从请求参数取 uid。
- 不以 root 运行（`run-as: package`）。
- 令牌、设备码、签名下载地址不进日志、前端或报告；数据库与密钥为 0600。
- 用户名下的下载只允许写入该用户在飞牛有权限的目录。

## 开发调试

```bash
make build
./dist/pikpak --listen 127.0.0.1:8787 --dev-uid 1000
curl -s -H 'X-Trim-Userid: 1000' http://127.0.0.1:8787/api/health
```

`--dev-uid` 只在本地调试使用，生产不会设置。
