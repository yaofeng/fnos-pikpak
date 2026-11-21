# fnOS 原生 PikPak 应用 · 设计文档

- 日期：2026-11-21
- 状态：待你的确认（确认后才进入实现计划）
- 配套文档：[PikPak API 参考](../pikpak-api.md)
- 目标设备：飞牛 fnOS 1.2.0401 及以上（实际以 M0 在真机上验证的版本为准）

---

## 0. 一页摘要

做一个原生 fnOS 应用（`.fpk`，非 Docker）。每个飞牛用户在自己的账号下绑定一个 PikPak 账号，应用通过 OAuth 设备码拿到该用户自己的令牌，后端代理调用 PikPak REST 接口，前端嵌在 fnOS 桌面里使用。

能力范围：文件浏览/筛选/排序、文件夹增删改、批量移动/复制/重命名、删除到回收站、回收站恢复与清空、离线下载任务、从飞牛上传、下载到飞牛、在线播放（Range 代理，不转码）、配额与会员状态。

技术形态：单个 Go 静态二进制，监听 `${TRIM_APPDEST}/app.sock`，由 fnOS 统一网关暴露到 `/app/pikpak`；前端 React + Vite，构建产物 `go:embed` 进二进制；SQLite（纯 Go）存用户、凭据、任务；凭据 AES-256-GCM 加密后落盘；用户身份只信网关注入的 `X-Trim-Userid`。

---

## 1. 意图与背景

PikPak 官方只有 Web 和 App，没有官方的 NAS 客户端；社区方案（rclone / OpenList / alist）是挂载或文件管理器形态，缺少离线任务、回收站、播放、批量操作这些"官方 Web 端"的完整体验。

飞牛有原生应用体系：应用可以是常驻服务 + 嵌入式 Web UI，能做多用户、能做文件权限、能进桌面。用它来装一个 PikPak 客户端，得到的是：

- 一个入口，不用另开浏览器；
- 每个飞牛用户各自绑定 PikPak，互相看不到；
- 下载能直接落到自己有权限的飞牛共享目录；
- 播放、上传走本机，不依赖外部客户端。

个人使用起步，但数据模型和权限边界按多用户设计——这样以后分享给家里人不用重写。

---

## 2. 目标 / 非目标 / 反目标

### 2.1 目标

1. **一个飞牛用户 = 一个 PikPak 绑定**。A 的文件列表、任务、传输、凭据对 B 完全不可见，B 也无法通过构造请求访问 A 的数据。
2. **覆盖官方 Web 端的主要文件操作**：浏览、面包屑、排序、筛选、搜索、新建文件夹、重命名、移动、复制、删除到回收站、批量操作。
3. **离线下载**：磁力 / 种子 / HTTP 直链创建任务，查看进度，重试，删除；能选目标文件夹。
4. **双向传输**：PikPak → 飞牛（下载到用户有写权限的目录）、飞牛 → PikPak（上传文件/文件夹），有队列、进度、失败可见。
5. **回收站**：列出、恢复、永久删除、清空（依赖 OAuth 的 `files:purge`）。
6. **在线播放**：用 PikPak 的播放地址，经本机 Range 代理给 `<video>`，支持拖动进度条；不转码。
7. **账号状态**：配额、会员类型/到期、令牌失效时明确提示重新授权。

### 2.2 非目标（v1 不做）

- 分享创建/管理、转存别人的分享（接口已部分确认，但优先级低于核心闭环）。
- 收藏夹、标签、最近访问的完整语义（最近添加可做）。
- 图片/文档在线预览、字幕、音轨切换。
- 本地转码、HLS 切片、离线缓存播放。
- 多账号同时绑定（一个飞牛用户只绑一个 PikPak 账号；换绑 = 解绑再绑）。
- PikPak 账号注册、找回密码、会员购买。
- 移动端原生壳（Web UI 自适应手机浏览器即可）。

### 2.3 反目标（做到这些算失败）

- 为了"全功能"把令牌或签名下载地址暴露给前端 JS。
- 绕过飞牛文件权限：接受前端传来的任意路径直接写盘。
- 把不同用户的凭据放在同一个明文文件里，或用一个共享令牌代替用户身份。
- 为了兼容而引入 Docker、ffmpeg、外部数据库等重依赖，让安装变成运维任务。
- 假装 PikPak 有稳定公开 API：所有未验证接口都要有失败降级和清晰报错，而不是静默错误。

---

## 3. 约束

| 约束 | 影响 |
|---|---|
| fnOS 应用以 `package` 用户运行 | 写用户目录必须通过 `pickUserFile` 授权 + `checkUserACL` 校验，不能假设 root |
| 统一网关只转发到 `target` 下的 Unix socket | 服务不监听 TCP；开发模式另开 `--listen` |
| 网关注入 `X-Trim-Userid` 等身份头 | 身份只能来自请求头；socket 文件权限 0600，网关是唯一入口 |
| 前端嵌在 iframe 中，路径带 `/app/pikpak` 前缀 | 前端资源用相对路径，路由用 HashRouter，后端兼容带/不带前缀 |
| PikPak 无公开 REST 文档 | 客户端层集中封装，端点/错误码可一处修改；未验证项有降级 |
| OAuth access token 12 小时、refresh token 轮换 | 必须单飞刷新并原子持久化新 refresh token |
| PikPak 有验证码与风控 | 只读接口实测免验证码；写操作若要求验证码，给用户可执行的提示 |
| 不做本地转码 | 播放能力受浏览器解码能力限制，UI 要如实提示 |
| 单机、个人规模 | SQLite 足够；不做集群、不做多实例 |

---

## 4. 方案选择

### 4.1 交付形态

| 方案 | 说明 | 取舍 |
|---|---|---|
| A. Docker 应用 | 镜像里跑 Go 服务，fnOS 只做入口 | 部署简单，但不符合"原生"；文件权限要额外挂载和映射；镜像体积大 |
| **B. 原生 `.fpk` + 常驻网关服务** | Go 二进制 + 内嵌前端，走统一网关 | 需要自己处理打包、生命周期、权限；换来最轻的安装和最好的系统集成 |
| C. 包装官方 CLI | 用官方 `pikpak` CLI 做后端 | CLI 面向单账号、无结构化任务/回收站能力，长期不可控 |

**选择 B。** 你明确要原生应用，且我们需要按飞牛用户隔离状态——这正是常驻服务 + 网关身份头能做的。A 的部署优势在个人 NAS 场景不重要，反而多一层权限映射。

### 4.2 认证方式

| 方案 | 说明 | 取舍 |
|---|---|---|
| A. 仅 PAT | 用户粘贴个人访问令牌 | 最简单，但 PAT 不含 `files:purge`，无法清空回收站；有有效期 |
| B. PAT + OAuth 双轨 | PAT 日常用，OAuth 仅用于清空 | 两套令牌、两套刷新/失效逻辑，复杂度翻倍 |
| **C. 仅 OAuth 设备码** | 应用内发起设备码，用户在浏览器/手机确认 | 一次授权拿到全部 scope，含 `files:purge`；refresh token 自动续期 |

**选择 C。** 你已经实测通过 OAuth 设备码并确认能调 REST；PAT 的唯一优势（不用跳浏览器）在 NAS 上不成立。凭据表保留 `auth_type` 字段，未来真要加 PAT 不用改表。

> 说明：PikPak 的个人访问令牌来自官方"连接应用"页面。若将来 OAuth 渠道被封，PAT 仍可作为降级入口，但不进入 v1 的 UI。

### 4.3 播放链路

| 方案 | 说明 | 取舍 |
|---|---|---|
| A. 302 到签名地址 | 后端返回 `web_content_link`，浏览器直连 | 省服务器带宽；但签名 URL 暴露给前端（含 userid/expire），跨域/防盗链/混合内容不可控，链接过期后播放器直接报错 |
| **B. 本机 Range 代理** | 前端只拿本应用 token，后端换签名地址并转发 Range | 签名地址不出后端；可统一处理 403/过期刷新；代价是流量过 NAS |

**选择 B。** 个人 NAS 场景带宽充足，安全与稳定性更重要。代理只做透传，不解码、不缓存整文件。

---

## 5. 总体架构

### 5.1 运行时视图

```
┌───────────────────────────── fnOS ─────────────────────────────┐
│                                                                │
│  fnOS 桌面 / 浏览器                                             │
│      │  GET /app/pikpak/...                                     │
│      ▼                                                          │
│  统一网关（注入 X-Trim-Userid / X-Trim-Username / X-Trim-Isadmin）│
│      │  HTTP over Unix socket                                  │
│      ▼                                                          │
│  pikpak 服务（以 package 用户运行）                              │
│   ├─ HTTP API /api/*        （身份中间件 → 用户上下文）           │
│   ├─ 静态资源（go:embed）                                        │
│   ├─ PikPak 客户端（每用户令牌、单飞刷新、限流、重试）             │
│   ├─ 传输引擎（下载 worker / 上传 worker / 恢复）                 │
│   ├─ 播放代理（Range 透传）                                      │
│   └─ SQLite + 密钥文件（$TRIM_PKGVAR）                          │
│      │                                                          │
└──────┼──────────────────────────────────────────────────────────┘
       │ HTTPS
       ▼
  user.mypikpak.com / api-drive.mypikpak.com / OSS
```

### 5.2 进程与并发模型

- 单进程。启动顺序：读环境变量 → 打开/迁移 DB → 加载/创建密钥 → 恢复未完成任务 → 启动 worker → 绑定 socket（先删残留 socket 文件）→ 写 PID。
- 每个用户一个 `UserSession`：令牌、刷新锁、限流器、正在进行的 flow。懒加载，闲置 30 分钟释放（DB 仍是真相）。
- 传输 worker：全局下载并发 2、上传并发 2（设置页可调 1–4），按用户公平轮转，避免一个用户的大文件饿死其他人。
- 前端轮询只用于任务/传输进度；不要为文件列表做秒级轮询。

### 5.3 请求链路（以列目录为例）

1. 浏览器 `GET /app/pikpak/api/files?parent_id=...`。
2. 网关补 `X-Trim-Userid: 1000` 后转发到 `app.sock`。
3. 身份中间件解析 uid；无 uid → 401（不读取 body/query 里的任何用户标识）。
4. Handler 取该 uid 的 `UserSession`；令牌过期则单飞刷新。
5. PikPak 客户端发请求；错误按 §7.10 映射。
6. 归一化成 DTO 返回；**绝不透传 PikPak 原始响应里的签名链接**。

### 5.4 代码结构

```
fnos-pikpak/
├── manifest
├── cmd/
│   ├── main                # fnOS 生命周期脚本（start/stop/status）
│   └── pikpak/             # Go 入口
├── app/ui/                 # fnOS 应用入口配置与图标（不是前端页面）
│   ├── config
│   └── images/
├── config/
│   ├── privilege
│   └── resource
├── internal/
│   ├── fnos/               # 环境变量、Open API 客户端、身份中间件
│   ├── pikpak/             # API 客户端、OAuth、GCID、错误映射
│   ├── store/              # SQLite、迁移、加密
│   ├── transfer/           # 下载/上传队列
│   ├── stream/             # Range 代理
│   ├── httpapi/            # 路由与 handler
│   └── webdist/            # go:embed 前端产物
├── web/                    # React + Vite 源码
├── docs/
├── Makefile
└── fnpack.json             # 打包配置（按 fnpack 版本调整）
```

`target/` 只放构建产物 `pikpak` 二进制；不放前端文件（已内嵌）。

---

## 6. fnOS 集成

### 6.1 manifest

```ini
appname=pikpak
version=1.0.0
display_name=PikPak
desc=在飞牛上浏览、传输和播放你的 PikPak 文件。
source=thirdparty
platform=x86
maintainer=<你的名字>
maintainer_url=<你的主页>
os_min_version=1.2.0401
desktop_uidir=ui
desktop_applaunchname=pikpak.main
ctl_stop=true
checkport=false
micro_app=true
disable_authorization_path=false
```

- `platform`：Go 二进制分架构，**x86 与 arm 分别出包**，不能用 `all`。
- `micro_app=true`：前端要用 `@trimjs/web-app` SDK（文件夹选择、主题），官方文档明确要求。
- `checkport=false`：不监听 TCP 端口。
- `disable_authorization_path=false`：保留用户在应用设置里配置授权目录的入口（与 `pickUserFile` 互补）。
- 不写 `service_port`。

### 6.2 app/ui/config

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

`gatewaySocket` 只写文件名，实际路径是 `${TRIM_APPDEST}/app.sock`。`gatewayPrefix` 一旦发布不再改。

### 6.3 config/privilege

```json
{
  "defaults": {
    "run-as": "package"
  }
}
```

不以 root 运行；需要写用户目录时靠系统授权，而不是提权。

### 6.4 config/resource

```json
{
  "api-scope": [
    "trim.file.userAccess",
    "trim.file.userAcl",
    "trim.file.path",
    "trim.system.getPlatformConfig"
  ]
}
```

- `trim.file.userAccess`：`pickUserFile` / `authorizeUserFile` / `getUserAccessibleFolders` / `delUserAccessibleFolder`。
- `trim.file.userAcl`：`checkUserACL`。
- `trim.file.path`：`convertPath`（把 `/vol1/...` 变成用户看得懂的路径）。
- `trim.system.getPlatformConfig`：语言、版本。

不申请 `trim.file.sharedAccess`：v1 只处理当前用户自己的授权目录，不做管理员视角的全局目录。

### 6.5 cmd/main

生命周期脚本，遵循 fnOS 约定：

- `start`：若已在运行直接返回 0；后台启动 `target/pikpak`，日志重定向到 `${TRIM_PKGVAR}/logs/app.log`；写 `${TRIM_PKGVAR}/pikpak.pid`。启动成功返回 0，失败返回 1。
- `stop`：向 PID 发 TERM，最多等 10 秒，再 KILL；清理 PID 与残留 `app.sock`；成功 0。
- `status`：进程存在且 socket 可连 → 0；否则 → 3。

服务自身也处理 SIGTERM：停止接新请求、让传输任务落盘状态、关闭 socket、退出。

### 6.6 身份与信任边界

- 后端只认 `X-Trim-Userid`（数字），辅以 `X-Trim-Username` 仅用于展示。
- 任何请求体/查询参数/前端存储里的 `uid` 一律忽略。
- **重复身份头一律拒绝**：若 `X-Trim-Userid` 出现多次（客户端伪造 + 网关注入），返回 400，不做"取第一个/最后一个"的猜测。这是官方文档未承诺网关会剥离客户端同名头时的兜底。
- Unix socket 由服务创建为 `0600`，目录 `target` 由 fnOS 管理；网关是唯一能连上的进程。
- 若缺少 `X-Trim-Userid`：返回 401，不降级为"默认用户"。
- M0 必须实测：伪造 `X-Trim-Userid` 是否被网关覆盖/剥离，以及网关是否会把身份头透传给未授权请求；结论写回本节。
- 开发模式：`--listen 127.0.0.1:8787 --dev-uid 1000`，只绑定回环，需显式开启；生产二进制不读该参数以外的环境注入。

### 6.7 文件授权流程

用户点"下载到…"或"上传自…"时：

1. 前端 `sdk.pickUserFile({ directory: true })`（上传文件夹时同）。
2. 系统弹飞牛目录选择器，返回 `string[]` 路径；系统同时给应用用户授予该目录 ACL。
3. 前端把路径交给后端；后端 `POST /api/fs/authorize`：
   - `filepath.Clean`、必须是绝对路径、拒绝 `..`；
   - `trim.file.getUserAccessibleFolders(uid)` 校验该路径在该用户的授权集合内（刚授权的可能还没同步，允许 1 次直接 `checkUserACL`）；
   - `trim.file.checkUserACL(uid, path)` 要求 `writable=true`（下载）或 `readable=true`（上传源）。
4. 缓存到 `granted_folders(uid, path)`，之后列目录/启动传输都用缓存 + 实时 ACL 复核。
5. 展示用 `trim.file.convertPath` 转成 `semanticPath`，不直接显示 `/vol1/...`。

如果用户手动输入路径 → 不做"路径白名单"，只做 ACL 校验；ACL 通过即可用（这与 fnOS 自身的权限模型一致）。

**两层权限，缺一不可**（这是文件读写的核心模型，别混）：

| 层 | 主体 | 怎么来 | 我们怎么用 |
|---|---|---|---|
| 用户权限 | 飞牛用户（uid） | 用户自己在 fnOS 里的目录权限 | `checkUserACL(uid, path)` 返回 readable/writable/deletable；决定"允不允许这个用户做" |
| 应用权限 | `package` 用户 | `pickUserFile` / `authorizeUserFile` 时系统授予 | 决定 Go 进程 `open()` 会不会 EACCES；决定"进程能不能真的读写" |

- `getUserAccessibleFolders(uid)` 返回的是**该用户**授权给本应用的目录集合（目录级；文件级授权不在此列）。
- `pickUserFile` 同时做两件事：登记用户授权路径，并让系统给应用用户加 ACL。所以正常流程两层都满足。
- 只有用户权限、没有应用 ACL：进程打开文件失败 → 提示"请重新选择目录授权"。
- 只有应用 ACL、用户无权限（比如管理员手工加了路径）：必须拒绝，不能借应用 ACL 越权。
- ACL 生效可能有延迟；刚选择完的路径若 `checkUserACL` 暂时返回 false，最多重试 3 次（间隔 300ms），仍失败则要求重新选择，不缓存失败结果。
- 下载写盘前再检查一次目标目录 `writable`；上传读源文件前检查 `readable`。

浏览器独立页面（`sdk.isStandaloneWeb`）时用 `openAppAuth('pickUserFile', { redirectUri, state })`，回调页用 `parseAppAuthCallback` 解析并校验 `state`；回调结果交后端同样走本节的校验。

### 6.8 Open API 调用

后端统一封装 `fnos.Client`：

```
POST /var/run/trim_open_gateway_apiscope.socket /api/v1/trimapp
Authorization: Bearer ${TRIM_API_TOKEN}
{"reqId":"...","req":"trim.file.checkUserACL","appName":"pikpak","data":{...}}
```

- 每次调用现读 `TRIM_API_TOKEN`（会变），不落盘、不打日志。
- 响应 `code != 0` 转为内部错误；Open API 不可用时，文件授权相关功能返回明确错误（"飞牛文件服务不可用"），其余 PikPak 功能不受影响。
- `reqId` 用请求 ID，便于和日志对齐。

---

## 7. PikPak 客户端设计

> 端点细节以 [PikPak API 参考](../pikpak-api.md) 为准；本节定义客户端行为与边界。

### 7.1 基础约定

- 主机：`user.mypikpak.com`（认证/用户）、`api-drive.mypikpak.com`（网盘）。
- 公共头：`X-Client-Id`（默认官方 CLI 的 `aal7sToC85zMXEpJ`，构建期可覆盖）、`X-Client-Version: 1.0.0`、`X-Device-Id`（每用户 32 位十六进制，首次绑定时生成并加密保存）、`Authorization: Bearer <access_token>`、`Content-Type: application/json`、固定 `User-Agent`。
- `X-Device-Id` 每用户独立、长期稳定；不每次请求随机（避免风控）。
- 所有请求带 `X-Request-Id` 便于排查；日志脱敏。
- 客户端不做自动重定向；签名下载地址是独立请求，不带 Authorization。

### 7.2 OAuth 设备码流程

```
前端                     后端                         PikPak
 │ POST /api/auth/device   │                            │
 │────────────────────────>│ POST /v1/auth/device/code  │
 │                         │───────────────────────────>│
 │                         │<── device_code,user_code,  │
 │                         │    verification_uri_complete
 │<── flow_id,user_code,   │
 │    qr/link, expires_in  │  启动后台轮询 goroutine     │
 │                         │                            │
 │ 用户在某处确认           │ POST /v1/auth/token        │
 │                         │  grant_type=device_code    │
 │                         │───────────────────────────>│
 │                         │<── access/refresh,sub      │
 │                         │ 加密入库、建 UserSession    │
 │ GET /api/auth/flow/{id} │                            │
 │<── status=bound,account │                            │
```

- 设备码请求 scope：`files:read files:write files:manage files:purge cloud_download share account`。
  - `share` 为 v2 分享功能预留：现在不申请，以后加要重新授权；申请但不调用没有副作用。
- 轮询严格按返回的 `interval`（默认 2s），`expires_in`（300s）到期即停；处理 `authorization_pending` / `slow_down`。
- 轮询在**后端**做：令牌不经过浏览器；用户关掉页面也不影响绑定完成。
- flow 状态机：`pending → authorized → bound`；或 `expired` / `denied` / `error`。flow 记录保留 10 分钟供前端查询。
- 前端展示：`user_code`、可点击的 `verification_uri_complete`、二维码（本地生成，不外发）、倒计时、取消按钮。
- 绑定成功返回账号摘要（昵称、头像、VIP 类型、配额），前端直接进文件页。
- 同一 uid 重复绑定：覆盖旧凭据前先停止该用户的传输任务并提示。

### 7.3 令牌生命周期

- access token 12 小时。刷新策略：剩余 < 10 分钟时后台刷新；收到 401/4121/4122 时同步刷新后重试一次。
- **单飞**：每用户一把 `refreshMu`；并发请求只触发一次刷新，其余等待结果。
- refresh token 会轮换：新令牌必须在**同一事务**里落库后才对外使用；崩溃时宁可重新授权，也不能拿着已作废的 refresh token。
- `4126 invalid_grant` / 刷新失败：该用户标记 `reauth_required`，停止其传输与轮询，API 返回 `pikpak_reauth_required`，前端顶部横幅 + 一键重新授权。
- 不在前端保存任何 PikPak 令牌；`localStorage` 只放 UI 偏好。

### 7.4 文件操作能力矩阵

| 能力 | 端点（详见 API 文档） | 备注 |
|---|---|---|
| 列目录 | `GET /drive/v1/files` | 游标分页；只取 `PHASE_TYPE_COMPLETE`、`trashed=false` |
| 文件详情 | `GET /drive/v1/files/{id}?usage=FETCH` | 取 `web_content_link`、`medias`，仅后端使用 |
| 新建文件夹 | `POST /drive/v1/files` | `folder_type=NORMAL`，kind `drive#folder` |
| 重命名 | `PATCH /drive/v1/files/{id}` | 同名错误码 3 |
| 批量移动/复制 | `POST /drive/v1/files:batchMove` / `:batchCopy` | 目标文件夹校验 |
| 删除到回收站 | `POST /drive/v1/files:batchTrash` | 返回 `task_id`；轮询任务完成后列表仍有 1–3 秒最终一致性 |
| 恢复 | `POST /drive/v1/files:batchUntrash` | 同上 |
| 永久删除 | `POST /drive/v1/files:batchDelete` | 需 `files:purge`；仅回收站内条目 |
| 清空回收站 | `PATCH /drive/v1/files/trash:empty` | 需 `files:purge`；`{"task_id":""}` |
| 最近添加 | `GET /drive/v1/events` | 归一化为文件列表 |
| 星标 | `POST /drive/v1/files:star` / `:unstar`（`{ids}`） | v1 只做列表标记，不做收藏页 |
| 配额/会员 | `GET /drive/v1/about`、`/privilege/vip` | 缓存 60 秒 |

批量操作（trash / untrash / move / copy / delete）返回的是 `task_id`。后端等待任务到 `PHASE_TYPE_COMPLETE` 再响应（最多 10 秒），超时也返回成功但带 `pending: true`，前端延迟 1–2 秒再刷新，避免"操作成功但列表没变"。

排序：API 文档未记录 `order` 参数是否被支持（❓），M0 用真账号验证 `order=name` / `-size` / `-modified_time` / `-created_time` 是否生效。生效则透传；不生效则只对已加载页做客户端排序，并在 UI 标注"仅当前已加载项"，不假装全量有序。筛选里的 `kind` 与 `modified_time` 有文档依据，类型筛选优先走 `filters`，其余（扩展名细分）在页内过滤。

### 7.5 搜索

现状：官方 Web 有搜索，但静态 JS 和官方 CLI 都没暴露可用端点（❓）。

设计（两条路，M0 定夺）：

1. **服务端搜索**：如果 M0 从 Web 动态 chunk/抓包确认了端点，后端 `GET /api/files/search?q=` 直接代理，返回统一分页结构。
2. **降级搜索**：没有端点时，v1 提供"当前目录即时过滤"（前端，已加载数据内），并在 UI 标注"仅当前文件夹"。不做全盘遍历索引——大账号成本高且会触发限流。

无论哪条路，搜索接口的对外形状保持一致，前端不用改。若最终只有降级，设置页给出说明，不假装有全局搜索。

### 7.6 离线下载任务

- 创建：`POST /drive/v1/files`，`upload_type=UPLOAD_TYPE_URL`，`url.url` 支持 magnet / torrent URL / HTTP(S)，`parent_id` 为目标文件夹，`folder_type=DOWNLOAD`。
- 列表：`GET /drive/v1/tasks?type=offline&with=reference_resource`，前端在任务页可见时每 3 秒刷新；切走即停。
- 进度：列表里的 `progress` 足够；单任务明细 `GET /drive/v1/task/{id}/statuses` 仅在展开时按需取。
- 重试：`POST /drive/v1/task`，`create_type=RETRY`。
- 删除：`DELETE /drive/v1/tasks?task_ids=...&delete_files=false`（默认保留文件，UI 提供"同时删除文件"勾选）。
- 清理已完成/失败：`POST /drive/v1/tasks:clear`。
- 暂停/继续：接口未确认（❓），v1 不做，UI 不显示按钮。
- 每日限额（错误码 11）：明确提示"今日离线任务额度已用完"，不重试。

### 7.7 上传（飞牛 → PikPak）

流程：

1. 用户在飞牛侧选文件/文件夹（`pickUserFile` 或输入授权目录内路径）。
2. 后端校验路径在授权集合内且可读，枚举文件（文件夹递归）。
3. 每个文件流式计算 GCID（PikPak 自定义分块哈希，移植 OpenList `hash_extend.GCID`；用已知样本做向量测试）。空文件也要传 `hash` 和 `size=0`。
4. `POST /drive/v1/files` 申请上传（`UPLOAD_TYPE_RESUMABLE`）：
   - 返回 `file` → 秒传，直接完成；
   - 返回 `resumable.params` → 走 OSS。
5. OSS：小文件单次 PutObject；大文件 multipart（分片 ≥ 6 MiB，目标 ≤ 1000 片，并发 3，带 `X-OSS-Security-Token`），完成后 `CompleteMultipartUpload`。容忍完成响应解析异常（文件名含 `&`/`<` 时的已知问题），以随后查询文件状态为准。
6. 上传后轮询文件详情直到 `PHASE_TYPE_COMPLETE`（最多 60 秒），再标记任务完成。
7. 同名冲突（错误码 3）：按设置处理——
   - `rename`（默认）：追加 ` (1)`，永不覆盖。
   - `skip`：跳过并计入结果。
   - `overwrite`：**先 `batchTrash` 旧文件 → 再上传**（旧文件进回收站，不是永久删除）。上传失败时把旧文件 `batchUntrash` 回原目录，保证"覆盖失败不丢文件"。上传成功后再询问/自动清空旧版本由设置决定。不做"先删后传"的永久删除路径。

v1 的恢复粒度：**进程内分片重试**；进程重启后该任务标记为失败，可一键重试（不持久化 OSS 会话）。理由：OSS 凭证会过期，跨进程恢复的收益小于复杂度。设置页说明这一点。

### 7.8 下载（PikPak → 飞牛）

1. 目标目录必须已通过 §6.7 校验。
2. `GET /drive/v1/files/{id}?usage=FETCH` 取签名链接；取不到 `web_content_link` 时尝试 `medias[].link.url`。
3. 流式写入 `{dest}/.pikpak-part/{job_id}`；按 4 MiB 缓冲；每 2 秒落一次进度。
4. 断点续传：`Range: bytes=<已下载>-`；服务端不支持 Range（返回 200）则从头写。
5. 完成校验：字节数等于 `size`（有 `hash` 时可选校验）；`fsync` 后原子 `rename` 到最终名。
6. 重名：默认追加 ` (1)`；设置可改为覆盖。
7. 链接过期（403/410）：重新取链接，最多 3 次，续传不重下。
8. 目录下载：递归创建本地目录结构，逐文件入队（并发仍受全局限制）。
9. 失败原因分类：网络、空间不足（ENOSPC）、权限（EACCES，提示重新授权目录）、PikPak 额度/限流、链接失效。

暂停/继续：暂停 = 取消当前 HTTP 请求并保留 `.part`；继续 = 按已有大小续传。取消 = 删除 `.part` 与目录（若空）。

### 7.9 播放代理

- `GET /api/stream/{file_id}?t=<token>`，token = `base64url(HMAC-SHA256(secret, uid|file_id|exp))`，有效期 6 小时。这样 `<video src>` 不需要自定义头。
- 后端取签名地址（按文件缓存 10 分钟，过期或上游 403 时刷新），转发 `Range` / `If-Range`，原样回 `206`、`Content-Range`、`Accept-Ranges`、`Content-Length`、`Content-Type`。
- 只允许 `GET`/`HEAD`；`Content-Disposition: inline`。
- 优先 `medias[]` 中 `is_default` 或最高分辨率的 `link.url`；无 `medias` 时用 `web_content_link`。
- 不做整文件缓存；支持多段并发 Range（浏览器自己会发）。
- 前端播放器：原生 `<video controls>` + 自定义倍速/画中画按钮；不支持 HEVC 等编码时提示"浏览器不支持该编码，可下载后播放"，不尝试转码。
- 下载到本地的入口与播放分开：`POST /api/downloads`（后台任务），不是浏览器另存为。
- **网关能力是前提**：M0 必须验证网关是否透传 `Range`/`206`、是否缓冲响应、是否有体积/时长上限、iframe 是否允许全屏。若网关不能流式转发，播放降级为 302 到签名地址（只有播放路径，下载仍走服务端），并在设置页说明；不为绕开网关另开 TCP 端口（HTTPS 混合内容与 NAT 都会出问题）。

### 7.10 限流、重试、错误映射

客户端内置每用户令牌桶：10 req/s，突发 20；传输 worker 单独计数。

| 上游情况 | 处理 |
|---|---|
| 401 / 16 / 4121 / 4122 | 刷新令牌，重试 1 次 |
| 4126 invalid_grant | 标记需重新授权，不重试 |
| 9 / 4002 captcha | 不自动破解；返回 `pikpak_captcha_required`，UI 提示稍后重试或去官方 Web 完成一次操作 |
| 10 / 429 | 指数退避 + 抖动，最多 4 次 |
| 5xx / 网络错误 | 最多 3 次，退避 0.5/1/2s |
| 3 同名 | 按上传冲突策略处理 |
| 8 空间不足 | 直接失败并提示 |
| 11 每日任务超限 | 直接失败并提示 |
| `file_rename_uncompleted` | 提示"文件仍在处理中，稍后重试" |

统一错误结构（后端 → 前端）：

```json
{"error":{"code":"pikpak_reauth_required","message":"PikPak 授权已失效，请重新绑定","detail":{"upstream_error_code":4126}}}
```

`code` 是稳定枚举，`message` 可本地化，`detail` 只放非敏感信息。

---

## 8. 本地数据模型

SQLite（`modernc.org/sqlite`，纯 Go，无 cgo），WAL 模式，`SetMaxOpenConns(1)` 避免写锁竞争；DB 文件 `${TRIM_PKGVAR}/pikpak.db`，权限 0600。

```sql
-- 迁移版本
CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);

-- 飞牛用户 ↔ PikPak 账号
-- 注意：pikpak_sub 不做唯一约束。同一个 PikPak 账号允许被多个飞牛用户分别绑定（家庭共用），
-- 每次绑定是独立的设备码会话/令牌；若 PikPak 侧限制并发会话，由用户自行承担"后绑挤掉先绑"的后果，
-- 我们不在绑定接口里泄露"该账号已被谁绑定"。
CREATE TABLE users (
  uid            INTEGER PRIMARY KEY,          -- 飞牛 uid
  username       TEXT NOT NULL DEFAULT '',     -- 仅展示
  pikpak_sub     TEXT NOT NULL,
  display_name   TEXT NOT NULL DEFAULT '',
  avatar         TEXT NOT NULL DEFAULT '',
  vip_type       TEXT NOT NULL DEFAULT '',     -- novip / platinum
  vip_expire     TEXT NOT NULL DEFAULT '',
  status         TEXT NOT NULL DEFAULT 'active', -- active / reauth_required / disabled
  bound_at       TEXT NOT NULL,
  updated_at     TEXT NOT NULL
);

-- 令牌（密文）
CREATE TABLE credentials (
  uid                 INTEGER PRIMARY KEY REFERENCES users(uid) ON DELETE CASCADE,
  auth_type           TEXT NOT NULL DEFAULT 'oauth',  -- 预留 pat
  client_id           TEXT NOT NULL,
  access_token_enc    BLOB NOT NULL,
  refresh_token_enc   BLOB NOT NULL,
  access_expires_at   TEXT NOT NULL,
  refresh_expires_at  TEXT NOT NULL DEFAULT '',
  scopes              TEXT NOT NULL DEFAULT '',
  device_id_enc       BLOB NOT NULL,
  updated_at          TEXT NOT NULL
);

-- 设备码绑定流程
CREATE TABLE auth_flows (
  id            TEXT PRIMARY KEY,      -- 随机 16 字节 hex
  uid           INTEGER NOT NULL,
  device_code_enc BLOB NOT NULL,
  user_code     TEXT NOT NULL,
  verify_url    TEXT NOT NULL,
  interval_sec  INTEGER NOT NULL,
  expires_at    TEXT NOT NULL,
  status        TEXT NOT NULL,         -- pending / bound / expired / denied / error
  error         TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL
);

-- 已授权目录（缓存，仍以实时 ACL 为准）
CREATE TABLE granted_folders (
  uid        INTEGER NOT NULL,
  path       TEXT NOT NULL,
  label      TEXT NOT NULL DEFAULT '',
  granted_at TEXT NOT NULL,
  PRIMARY KEY (uid, path)
);

-- 下载任务
CREATE TABLE download_jobs (
  id            TEXT PRIMARY KEY,
  uid           INTEGER NOT NULL,
  file_id       TEXT NOT NULL,
  file_name     TEXT NOT NULL,
  dest_dir      TEXT NOT NULL,
  dest_path     TEXT NOT NULL,
  size          INTEGER NOT NULL DEFAULT 0,
  downloaded    INTEGER NOT NULL DEFAULT 0,
  status        TEXT NOT NULL,   -- queued/running/paused/done/failed/canceled
  error_code    TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  link_expire_at TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
CREATE INDEX idx_download_jobs_uid_status ON download_jobs(uid, status);

-- 上传任务
CREATE TABLE upload_jobs (
  id            TEXT PRIMARY KEY,
  uid           INTEGER NOT NULL,
  local_path    TEXT NOT NULL,
  parent_id     TEXT NOT NULL,
  remote_name   TEXT NOT NULL,
  size          INTEGER NOT NULL DEFAULT 0,
  uploaded      INTEGER NOT NULL DEFAULT 0,
  status        TEXT NOT NULL,
  error_code    TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
CREATE INDEX idx_upload_jobs_uid_status ON upload_jobs(uid, status);

-- 设置
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE user_settings (
  uid INTEGER NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL,
  PRIMARY KEY (uid, key)
);
```

不存 PikPak 文件列表缓存（除了最近访问的路径栈可放前端）。签名下载地址不落库。

---

## 9. 密钥与凭据安全

- 首次启动生成 `${TRIM_PKGVAR}/secret.key`：32 字节随机，`0600`，O_EXCL 创建；已存在则复用。
- 加密：AES-256-GCM，随机 12 字节 nonce 前置，密文 = `nonce || ciphertext`。AAD 绑定 `uid + 字段名`，防止密文互换。
- `secret.key` 与 `pikpak.db` 分开存放；备份 DB 不泄露令牌。密钥丢失 → 所有用户需重新绑定（UI 给明确提示）。
- 日志红线：不打印 `Authorization`、`access_token`、`refresh_token`、`device_code`、`web_content_link`、`X-Trim-Userid` 之外的身份头。签名 URL 只在 debug 级别打印 host + path，去掉查询串。
- 前端 token（播放用）是 HMAC 派生、带 uid 与过期时间，伪造成本等于猜密钥；只用于读流。
- CSRF：网关不做 CORS，浏览器跨站发 `application/json` 会先预检且失败；所有写接口额外要求 `Content-Type: application/json`，并校验 `Origin`（存在时）与当前站点同源。嵌入 iframe 场景 `Origin` 可能是 fnOS 主站，因此采用"同源或为空则放行，跨源明确拒绝"的策略，M0 实测。
- 不在前端渲染 PikPak 原始 HTML/链接；所有外链经过白名单（`mypikpak.com` 域名）。
- 依赖最小化：Go 标准库 + `modernc.org/sqlite`；不引入大型 SDK。

---

## 10. 后端 HTTP API

统一前缀 `/api`，JSON；身份来自网关头。所有列表接口返回 `{items, next_page_token}`。

### 认证与账号

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/me` | 当前 uid、绑定状态、账号摘要、能力（can_purge 等） |
| POST | `/api/auth/device` | 发起绑定，返回 flow |
| GET | `/api/auth/flow/{id}` | 查询流程状态 |
| POST | `/api/auth/flow/{id}/cancel` | 取消 |
| POST | `/api/auth/rebind` | 重新授权（清除 reauth 状态） |
| POST | `/api/auth/logout` | 解绑并清除凭据/任务 |
| GET | `/api/account` | 配额、会员（缓存 60s） |

### 文件

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/files?parent_id=&page_token=&limit=&order=&kind=&q=` | 列目录；`q` 为目录内过滤 |
| GET | `/api/files/{id}` | 元数据（不含签名链接） |
| POST | `/api/folders` | `{parent_id,name}` |
| PATCH | `/api/files/{id}` | `{name}` 重命名 |
| POST | `/api/files/move` | `{ids,to_parent_id}` |
| POST | `/api/files/copy` | `{ids,to_parent_id}` |
| POST | `/api/files/trash` | `{ids}` |
| POST | `/api/files/untrash` | `{ids}` |
| POST | `/api/files/delete` | `{ids}` 永久删除 |
| POST | `/api/files/star` / `/api/files/unstar` | `{ids}` |
| GET | `/api/recent` | 最近添加 |
| GET | `/api/trash?page_token=` | 回收站列表 |
| POST | `/api/trash/empty` | 清空 |
| GET | `/api/search?q=&page_token=` | 全局搜索（是否可用取决于 M0） |

### 离线任务

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/tasks?phase=&page_token=` | 任务列表 |
| POST | `/api/tasks` | `{url,parent_id,name?}` |
| POST | `/api/tasks/{id}/retry` | 重试 |
| DELETE | `/api/tasks?ids=&delete_files=` | 删除任务 |
| POST | `/api/tasks/clear` | 清理已完成/失败 |

### 传输

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/downloads?status=` | 下载任务 |
| POST | `/api/downloads` | `{file_ids,dest_dir,conflict?}` |
| POST | `/api/downloads/{id}/pause` / `resume` / `cancel` | 控制 |
| DELETE | `/api/downloads/{id}` | 删除记录（可选删文件） |
| GET | `/api/uploads` | 上传任务 |
| POST | `/api/uploads` | `{local_paths,parent_id,conflict?}` |
| POST | `/api/uploads/{id}/pause` / `resume` / `cancel` | 控制 |

### 飞牛文件系统

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/fs/folders` | 已授权目录（含 semanticPath） |
| POST | `/api/fs/authorize` | `{path}` 校验并缓存 |
| GET | `/api/fs/check?path=&need=read\|write` | 实时 ACL |
| GET | `/api/fs/list?path=` | 列本地目录（仅授权集合内） |
| GET | `/api/fs/browse?path=` | 上传源浏览（可选；也可完全依赖前端选择器） |

### 播放与设置

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/stream/{file_id}?t=` | Range 代理（无 /api 身份头依赖，靠 t） |
| GET | `/api/play/{file_id}` | 返回 `{stream_url, mime, size, duration, width, height}` |
| GET/PUT | `/api/settings` | 全局设置（并发数、冲突策略、语言） |
| GET/PUT | `/api/user-settings` | 每用户设置 |

HTTP 状态码：400 参数、401 无身份/未绑定、403 ACL 拒绝、404 不存在、409 冲突、429 上游限流、502 上游异常、503 飞牛 Open API 不可用。

分页：`limit` 默认 100、最大 200；`page_token` 原样透传 PikPak。

---

## 11. 前端设计

### 11.1 技术

React 18 + TypeScript + Vite；TanStack Query 管服务端状态；React Router（**HashRouter**，避免网关前缀下的深链 404）；CSS 变量做主题，不引入组件库（体积与风格统一）；`@trimjs/web-app` 做主题/语言/目录选择。

### 11.2 页面

```
┌──────────┬──────────────────────────────────────────────┐
│ PikPak   │  搜索…            上传  新建文件夹   头像 ▾    │
│          ├──────────────────────────────────────────────┤
│ 我的文件  │  根目录 / 电影 / 2024                         │
│ 最近添加  │  ┌────────────────────────────────────────┐  │
│ 离线任务  │  │ ☐ 名称            大小   修改时间   ⋯  │  │
│ 传输      │  │ ☐ 📁 电影                              │  │
│ 回收站    │  │ ☐ 🎬 a.mkv        4.2 GB  11-20    ⋯  │  │
│          │  │ ☐ 🎬 b.mp4        1.1 GB  11-19    ⋯  │  │
│ ─────    │  └────────────────────────────────────────┘  │
│ 配额 62% │  已选 2 项：下载 移动 复制 删除               │
│ 会员 到期 │                                              │
└──────────┴──────────────────────────────────────────────┘
```

- **我的文件**：面包屑、排序、类型筛选、网格/列表切换、多选、右键菜单、拖拽移动、无限滚动。
- **最近添加**：`/drive/v1/events` 归一化。
- **离线任务**：新建（粘贴磁力/链接）、进度条、重试、删除、清理；可见时 3 秒轮询。
- **传输**：下载/上传两个 tab，进度、速度、剩余时间、暂停/继续/取消、失败原因、打开所在目录（下载）。
- **回收站**：列表、恢复、永久删除、清空（二次确认，文案说明不可恢复）。
- **设置**：授权目录管理、并发数、重名策略、语言、重新授权、解绑、关于。
- **绑定页**：设备码 + 链接 + 二维码 + 倒计时；失败态给原因。
- **播放**：全屏弹层，`<video controls>`，标题、文件信息、下载按钮。

### 11.3 交互细节

- 所有破坏性操作二次确认；清空回收站要求输入"确认"或长按（移动端）。
- 错误统一 toast + 可展开详情；`pikpak_reauth_required` 用常驻横幅而不是 toast。
- 空状态区分"空文件夹"与"未绑定"。
- 移动端：侧栏收起为底部 tab；列表改为紧凑行；点击整行打开。
- 主题：优先 `sdk.$on('os/theme')`，独立浏览器页面回退 `prefers-color-scheme`。
- 语言：`zh-CN` 默认，`en` 次之；文案集中在一个 `i18n.ts`，不引重库。
- 不显示原始 `/vol1/...`，一律 `semanticPath`。

---

## 12. 传输引擎

- 任务状态持久化在 DB；启动时把 `running` 重置为 `queued`（下载可按 `.part` 续传）。
- 队列：每用户 FIFO，全局并发上限；`paused` 立即让出槽位。
- 下载 worker：取链接 → 打开 `.part` → Range 续传 → 进度（每 2s、每 4MiB）→ 完成校验 → rename。
- 上传 worker：GCID → 申请 → OSS 分片 → 完成 → 等待 `PHASE_TYPE_COMPLETE`。
- 取消：context cancel 贯穿 HTTP 请求；清理临时文件。
- 磁盘保护：开始前检查目标盘剩余空间 ≥ 文件大小 × 1.05；不足直接失败。
- 传输与 PikPak API 限流共用每用户令牌桶，但分片 PUT OSS 不计入。
- 服务退出：等待最多 10 秒让 worker 落盘进度，未完成的保留 `.part`；不强杀进行中的 `rename`。

---

## 13. 可观测性与运维

- 日志：`slog` JSON，写 `${TRIM_PKGVAR}/logs/app.log`；按 10 MiB 轮转，保留 5 个。
- 每条请求日志：`req_id`、`uid`、method、path（脱敏）、status、耗时、上游耗时。
- 健康检查：`GET /api/health`（不需要身份，返回版本、DB 可写、socket 正常）。
- 设置页"诊断"：版本、DB 大小、任务数、最近错误摘要、导出日志按钮（打包 zip 前脱敏）。
- 不做遥测上报；不向外部发送任何使用数据。

---

## 14. 测试策略

### 14.1 单元测试（Go）

- GCID：用 OpenList/rclone 的已知向量（空文件、1 字节、16 MiB 边界、多块）做表驱动测试。
- 路径安全：`..`、相对路径、符号链接、非授权目录、大小写、尾部斜杠。
- ACL 守卫：mock Open API，覆盖 readable/writable 组合与 Open API 失败。
- 令牌刷新：单飞（100 并发只刷一次）、轮换落库、4126 标记、崩溃点（写到一半）。
- 错误映射：上游码 → 内部码 → HTTP 状态。
- 加密：AAD 不匹配解密失败；密钥文件权限。
- 下载续传：`httptest` 假服务器，206/200/403/断流/大小不符。
- 上传分片：假 OSS，分片重试、完成响应 EOF 容错。
- 播放代理：Range 头解析、多段、HEAD、过期 token。

### 14.2 集成测试

- 假 PikPak + 假 fnOS Open API（Unix socket 或 httptest），跑完整用户旅程：绑定 → 列目录 → 建夹 → 上传 → 下载 → 删除 → 清空。
- 多用户隔离：A、B 各自数据，交叉请求必须 404/403；同一文件 id 在 B 的上下文里不可访问（我们的 handler 以 uid 隔离，天然满足；测试防回归）。

### 14.3 端到端

- Playwright 跑 dev 模式（`--listen --dev-uid`），覆盖关键路径与移动端视口。
- 真机矩阵（手动，发布前）：
  - fnOS 1.2.0401 / 最新版安装、启动、停止、重启；
  - 每个用户绑定，互相不可见；
  - 授权目录 → 下载成功；撤销授权 → 下载失败且提示正确；
  - 视频 Range 拖动、长视频（>4 GB）；
  - 服务重启后任务恢复；
  - 卸载后残留（DB/密钥）处理符合预期。

### 14.4 真实账号 smoke（手动/可选 CI）

`PIKPAK_SMOKE=1` 时运行：临时文件夹 → 上传 1 KB → 取链接 → Range → 删除 → 清空 → 清理。沿用 API 文档里的实测脚本思路，不把令牌写进仓库。

---

## 15. 打包与发布

- `make web` → Vite 构建到 `internal/webdist/dist`。
- `make build-x86` / `build-arm` → `CGO_ENABLED=0 GOOS=linux GOARCH=amd64|arm64 go build -trimpath -ldflags "-s -w"`。
- `make fpk-x86` / `fpk-arm` → `fnpack build` 产出 `pikpak_1.0.0_x86.fpk`、`pikpak_1.0.0_arm.fpk`。
- 安装包内容：`manifest`、`cmd/main`、`app/ui/*`、`config/*`、`target/pikpak`。
- 版本号语义化；`manifest` 的 `version` 与二进制内版本一致；升级保留 `${TRIM_PKGVAR}`（DB/密钥/日志）。
- 卸载：默认删除应用目录，保留用户数据目录由 fnOS 决定；文档说明如何彻底清理。
- 发布：先本地安装验证，再考虑上架；不自动上传任何地方。

---

## 16. 里程碑

| 阶段 | 内容 | 完成标准 |
|---|---|---|
| **M0 验证**（1–2 天） | 搜索端点；列表 `order`；REST 上传全链路（GCID+OSS）；最小 fnOS 包跑通网关/socket/身份头；`pickUserFile` + ACL 真机验证；网关 Range 流式播放；iframe 下 Origin/资源前缀 | 七项都有结论，写回文档；失败项有明确降级 |
| M1 骨架 | 打包、生命周期、DB/迁移/加密、身份中间件、SDK 接入、OAuth 绑定 | 真机安装，用户能绑定并看到账号信息 |
| M2 文件 | 列表/面包屑/排序/筛选、建夹/重命名/移动/复制、批量删除、回收站 | 与官方 Web 的文件区体验对齐 |
| M3 传输 | 下载队列/续传、上传（GCID+OSS）、冲突策略、进度 | 1 GB 文件双向成功，重启可恢复下载 |
| M4 任务 | 离线任务创建/列表/重试/删除/清理 | 磁力任务从创建到完成可见 |
| M5 播放 | 签名链接获取、Range 代理、播放器、编码提示 | 4 GB 视频拖动流畅，令牌不出后端 |
| M6 打磨 | 搜索（按 M0 结论）、最近添加、设置、诊断、i18n、E2E、真机矩阵 | 无 P0/P1 缺陷，文档齐全 |

每个里程碑结束都做一次 `superpowers:requesting-code-review` 与真机验证，不攒到最后。

---

## 17. 风险与待验证

| 风险 | 影响 | 应对 |
|---|---|---|
| 搜索端点找不到 | 无全局搜索 | 目录内过滤 + 明确文案；M0 优先解决 |
| REST 上传细节偏差（GCID/OSS） | 上传失败 | M0 用真账号打通；保留"仅下载"降级发布 |
| 官方变更非公开接口 | 功能失效 | 客户端集中封装；错误可读；快速发版 |
| 网关缓冲响应或限制长连接 | 播放不可用 | M0 用 4 GB 文件实测 Range/206/多段/全屏；不能流式则播放降级为 302 签名地址 |
| 使用官方 CLI 的 client_id | 被风控/封禁 | 可构建期替换；请求特征尽量接近官方；不高频轮询 |
| 验证码 | 写操作被拦 | 明确提示；不在 NAS 上做人机验证 |
| 下载额度限制（未证实） | 速度受限 | 文档说明；UI 显示实际速度，不承诺满速 |
| fnOS 版本差异（SDK/Open API） | 部分功能不可用 | `os_min_version` 从严；运行时探测并降级 |
| 单二进制体积（含前端） | 安装包偏大 | 前端按需拆分，控制在 ~30 MB 内 |
| 大目录性能 | 列表卡顿 | 游标分页 + 虚拟滚动；不做全量预取 |

仍未验证、需要在 M0 收口的事项：

1. PikPak 搜索端点。
2. 列表 `order` 参数与类型筛选是否生效。
3. REST 上传（GCID 算法与 OSS 分片）端到端。
4. 网关是否原样转发 `/app/pikpak` 前缀；`Origin` 在 iframe 下的值；客户端伪造 `X-Trim-Userid` 是否被剥离；是否透传 `Range`/`206` 并流式转发大响应。
5. `pickUserFile` 返回路径、ACL 生效延迟、`semanticPath` 形态。
6. fnOS 目标版本与设备架构（需要你提供一台测试机信息）。
7. 分享详情/转存历史（v2 才需要）。

---

## 18. 我做的判断（你可以逐条推翻）

1. **不做 Docker**，走原生 `.fpk`。
2. **只做 OAuth**，不提供 PAT 输入框；表结构预留。
3. **播放走本机代理**，不 302。
4. **搜索先降级**为目录内过滤，M0 找到端点再升级为全局。
5. **SQLite + 纯 Go 驱动**，不引外部数据库，不用 cgo。
6. **上传不做跨进程断点续传**，只做进程内分片重试。
7. **分享/收藏/字幕/预览不进 v1**。
8. **前端 React + Vite + HashRouter**，不引组件库。
9. **每用户独立 `X-Device-Id`**，长期稳定。
10. **申请 `share` scope 但 v1 不调用**，避免以后重新授权。
11. **一个飞牛用户只绑一个 PikPak 账号**，换绑需解绑。
12. **不向外部发送任何遥测**。
13. **允许同一个 PikPak 账号被多个飞牛用户分别绑定**（家庭共用），不做唯一性校验，也不告诉用户"这账号别人绑过"。
14. **上传是 v1 的硬性范围**：M0 若证明 REST 上传不可行，不静默删功能，而是带着证据回来重新定范围。
15. **批量文件操作同步等待任务完成**（最多 10 秒），超时返回 `pending: true` 由前端延迟刷新。

---

## 19. 留给实现者的空间

- 具体 UI 视觉（间距、图标、动效）在实现时定，但必须走 fnOS 主题变量、支持深浅色、移动端可用。
- 列表的虚拟滚动实现方式（自研 vs 轻量库）。
- 日志轮转是用库还是自写。
- 下载并发与分片大小的默认值调优。
- 错误文案的具体措辞。
- 是否在 v1.1 加"分享给我"的浏览入口。
