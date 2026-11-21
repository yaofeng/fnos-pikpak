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

## 发布

推送 `1.0.0` 或 `v1.0.0` 形式的 tag 会触发 [`.github/workflows/build-fpk.yml`](.github/workflows/build-fpk.yml)：

1. 从 tag 解析版本（也接受 `1.0.0-1`），校验格式；
2. `go test ./...`，构建前端与 linux/amd64 二进制；
3. 把版本写进 manifest 的 `version`、二进制 `--version`，包名为 `pikpak-<version>-x86.fpk`；
4. 校验包内版本后上传 Artifact，并创建 GitHub Release。

CI 使用固定版本的 fnpack 并校验 SHA256。包默认未签名；在仓库 Secrets 配置 `FN_DEVELOPER_KEY` 与 `FN_DEVELOPER_CERT`（飞牛开发者证书 PEM）后即为签名包。本地构建同样可以指定版本：

```bash
make m0 VERSION=1.2.3                # 未签名
make fpk VERSION=1.2.3 FN_KEY=dev.key FN_CERT=dev.crt   # 签名
```

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

不需要打包安装到 NAS，本机直接跑：

```bash
make dev        # 构建前端 + 本机二进制，监听 http://127.0.0.1:8787
```

打开这个地址，就能走完整的绑定流程（设备码）和 PikPak 探针（列表、配额、会员、排序、搜索、分页、并发限流、离线下载）。绑定的账号存在 `./.data/`（已 gitignore），重启后仍然有效；`POST /api/auth/logout` 可解绑。

离线下载探针会在你的网盘根目录创建一个约 1 KB 的任务（`https://example.com/index.html`，被拒时回退到 `https://speed.hetzner.de/1MB.bin`），轮询到完成后再删除任务和文件，报告里会显示清理是否成功。

上传探针默认上传一份内容固定的 20 MiB 文件，多数情况下会被秒传；请求体加 `"fresh": true` 会生成随机内容强制走真实上传，结束后自动永久删除该文件：

```bash
curl -s -X POST -H 'Content-Type: application/json' \
  -d "{\"local_path\":\"$(curl -s http://127.0.0.1:8787/api/upload/probe/config | python3 -c 'import json,sys;print(json.load(sys.stdin)["generated_path"])')\",\"fresh\":true}" \
  http://127.0.0.1:8787/api/upload/probe
```

本机没有飞牛网关，所以：

- `fnos.*` 探针显示 `skip`（Open API 客户端未配置）；
- 文件授权卡片返回 503（飞牛文件服务未配置）；
- 其余健康检查、探针、导出报告、前端全部可用。

不加 `--listen` 时只监听 `${TRIM_APPDEST}/app.sock`，与生产一致。`--dev-uid` 只在本地调试使用，生产不会设置。

```bash
curl -s http://127.0.0.1:8787/api/health
```
