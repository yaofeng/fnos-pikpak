# PikPak M0 验证报告（模板）

把这份模板复制一份填写，结论与原始证据一起提交。原始证据来自两处：

- 页面 **导出报告** 生成的 `pikpak-m0-report.json`（探针结果，已脱敏）。
- 页面上手工触发的卡片结果：**网关信息**、**Range 探针**、**文件授权探针**、**上传探针**。

填写原则：每行只填一个结论；结论与证据不一致时，以证据为准并注明。

## 环境

| 项 | 值 |
|---|---|
| fnOS 版本 | |
| 设备型号 / CPU | x86 |
| 应用版本（服务状态卡片） | 0.1.3 |
| 安装是否被签名拦截 | 否 / 是（原文：） |
| 测试账号是否 VIP | |
| 测试目录（授权目录） | |

## 结论表

| 编号 | 探针 | 结论选项 | 证据从哪看 | 本次结论 | 备注 |
|---|---|---|---|---|---|
| 1 | 网关路径前缀 | 带前缀 / 不带前缀 | 网关信息 → 原始路径（`original_path`） | | |
| 2 | 伪造身份头 | 被剥离 / 重复出现（靠重复头拒绝） / 被覆盖 | 网关信息 → 伪造身份头测试 | | |
| 3 | 网关 Origin | 具体值 / 空 | 网关信息 → Origin | | |
| 4 | Range 流式 | 206 且流式 / 206 但被缓冲 / 失败 | Range 探针：状态、首字节 ms、总耗时 ms | | |
| 5 | pickUserFile + ACL | 路径形态、ACL 是否生效、semanticPath 是否可用 | 文件授权探针 | | |
| 6 | 搜索端点 | 命中候选 1/2/3/4 / 全部失败 | 报告 `pikpak.search` 的 `results[].path` 与 `status` | | |
| 7 | 列表排序 | 生效的 order 值 / 都不生效 | 报告 `pikpak.order` 的 `results[]` | | |
| 8 | REST 上传 | 秒传 / 分片成功 / 失败（失败点） | 上传探针：GCID、provider、各阶段耗时、最终 phase | | |

## 每项的判定说明

1. **网关路径前缀**：页面"网关信息"里的"原始路径"是否以 `/app/pikpak` 开头（`original_path` 是剥前缀前的值；`path` 是剥前缀后的值）。前端已按当前路径拼 API，两种都应能工作；此项用于确认资源前缀与 iframe 场景。
2. **伪造身份头**：页面向 `POST /api/gateway/forged` 额外发送一个 `X-Trim-Userid: 99999`。拿到 `header_count=2` 说明网关保留了客户端副本，服务端靠"重复头即拒绝"保护；`header_count=1` 说明网关覆盖或剥离。
3. **Origin**：记录值即可，若为空也要写明。
4. **Range 流式**：首字节耗时明显小于总耗时 = 流式；两者接近且总耗时随区间大小增长 = 被缓冲（网关整段读完再转发）。超出 EOF 的用例应返回 416。
5. **pickUserFile + ACL**：记录返回的路径形态（真实路径 / 语义路径）、`getUserAccessibleFolders` 的返回、ACL 是否可写、`convertPath` 是否给出语义路径。
6. **搜索**：看四个候选里有没有 `looks_like_file_list=true` 且 HTTP 200 的。没有则 v1 搜索降级为"当前目录内过滤"。
7. **列表排序**：`name`、`-size`、`-modified_time`、`-created_time` 里哪些返回 200 且首条名称随参数变化。都不生效则只对已加载页做前端排序，并在 UI 标注。
8. **REST 上传**：先看 GCID 是否算完、是否秒传；需要上传时看 provider 是否 `PROVIDER_ALIYUN`、分片是否完成、最终 `PHASE_TYPE_COMPLETE`。失败点写在备注（申请上传 / OSS 分片 / 完成分片 / 等待 COMPLETE）。

## 失败项的降级方案（填写时确认）

| 未知项 | 若失败 | 降级方案 | 是否接受 |
|---|---|---|---|
| 网关路径前缀异常 | 前端路径拼接不适用 | 按实际前缀调整 `basePath` 规则 | |
| 网关不剥离伪造头 | 依赖重复头拒绝 | 已实现：重复 `X-Trim-Userid` → 400 | |
| Range 被缓冲 | 播放拖拽体验差 | 改为 302 到 PikPak 签名地址（会暴露签名 URL）或自建分片中转 | |
| ACL / pickUserFile 不可用 | 无法确定用户可写目录 | 下载目标限制为应用自己的数据目录，再由用户手动移动 | |
| 搜索无端点 | 无法全盘搜索 | v1 只做当前目录过滤 | |
| 排序不生效 | 大目录排序不完整 | 前端对已加载页排序并标注 | |
| 上传失败 | 不能用 REST 上传 | 保留"从 PikPak 下载"能力，上传延后到 M1 再攻 | |

## 原始报告

把 `pikpak-m0-report.json` 的 `summary` 与关键 `results[].data` 粘在下面（不要贴 token；报告本身已脱敏）。

```json
{
  "summary": {},
  "results": []
}
```

## 附加观察

- 安装/启动过程中的报错原文：
- 页面上其它异常：
- 对 M1 的建议：

---

## 实测记录（2026-11-21，应用 0.1.0，真机 x86）

导出报告汇总：`pass 5 / warn 1 / fail 1`。逐项对应：

| 编号 | 本次结论 | 证据 |
|---|---|---|
| 1 网关路径前缀 | 未采集（该卡片不在导出报告里） | — |
| 2 伪造身份头 | 未采集 | — |
| 3 网关 Origin | 未采集 | — |
| 4 Range 流式 | 未采集 | — |
| 5 pickUserFile + ACL | ACL 可用：`/vol1/1001/private/pikpak` 读/写/删均为 true，授权集合内 true；`convertPath` 返回**裸数组**而不是 `{status,result}`，0.1.0 解析失败 | 文件授权探针卡片 |
| 6 搜索端点 | `?q=` 返回 200 但**未过滤**（结果含不含关键词的文件）；只带 `name` 的 filters 被 404 `file_invalid_file_filters`；`/drive/v1/search` 404；`/drive/v1/files:search` 400。0.1.0 的 `pass` 是误判 | 报告 `pikpak.search` |
| 7 列表排序 | `name`、`-size`、`-modified_time`、`-created_time` 四种裸值全部 400 `invalid_argument`，未拿到任何一种可用写法 | 报告 `pikpak.order` |
| 8 REST 上传 | 未采集 | — |
| 其它 | `fnos.platform` 403：飞牛不把 `trim.system.getPlatformConfig` 授权给三方应用；只影响语言/版本显示 | 报告 `fnos.platform` |

### 0.1.1 的改动（对应上面的发现）

- `convertPath` 同时接受裸数组与 `{status, result}`；字段名对不上时把原始形状回填到错误里，方便定位。
- `pikpak.order` 探针改试 JSON 字符串编码（`"name"`、`"-size"`…）和分享页词表（`name_asc`、`size_desc`、`mod_desc`）。
- `pikpak.search` 探针要求返回的每个文件名都包含关键词才算命中，并改用带 `phase`/`trashed` 的完整 `filters`。
- `fnos.platform` 的 403 记为 warn，不再计入失败。

### 0.1.1 需要复跑

5（显示路径一栏应出现语义路径）、6、7；以及尚未采集的 1–4、8。

## 复测记录（2026-11-22，应用 0.1.1，真机 x86）

导出报告汇总：`pass 5 / warn 2 / fail 0`。三条待复跑项的结论：

| 编号 | 结论 | 证据 |
|---|---|---|
| 5 pickUserFile + ACL | **通过**。`/vol1/1001/private/pikpak` → `存储空间 1/yaofeng 的文件/private/pikpak`，语义路径正常 | 报告 `fnos.folders` 的 `semantic` |
| 6 搜索端点 | **服务端搜索不可用**。补了 `phase`/`trashed` 的 filters 仍被 404 `file_invalid_file_filters`；`?q=` 返回的仍是目录列表，所有候选 `matched=false` | 报告 `pikpak.search` |
| 7 列表排序 | **服务端排序不可用**。8 种写法（裸值、JSON 字符串、分享页词表）全部 400 `invalid_argument` | 报告 `pikpak.order` |

另外两个探针判定缺陷也在这次复跑中暴露，0.1.2 已修：

- `pikpak.search` 仍按 `looks_like_file_list` 判 pass，虽然数据里 `matched=false`；现在只有 `matched=true` 才 pass，且探针改用不存在的名字作查询，空结果即代表服务端确实过滤了。
- `pikpak.order` 的详情写死"4 种"，实际试了 8 种；现在按实际候选数生成。

### 0.1.2 的其它修复

- 入口图标文件名改为 `icon_64.png` / `icon_256.png`（fnOS 会把 `icon_{0}.png` 的 `{0}` 替换成像素尺寸，原来的 `icon_1/2/3.png` 永远加载不到，桌面显示破图）。
- 请求缺失的静态文件时返回 404，不再回退成 `index.html`。

### 仍未采集

1–4（网关信息、Range 探针卡片）与 8（REST 上传），需要在页面手工触发后补进上面的结论表。

## 补充探针（0.1.3，本机可跑）

新增三个探针，不需要飞牛，`make dev` 绑定账号后直接跑：

| 探针 | 验证什么 | 判定 |
|---|---|---|
| `pikpak.pages` | 按 `next_page_token` 翻页是否终止、是否有重复条目 | token 循环或请求失败 → fail；达到 5 页上限或出现重复 → warn |
| `pikpak.burst` | 12 个并发列表请求是否都被接受 | 有失败 → warn，详情带状态码分布 |
| `pikpak.offline` | 创建离线任务 → 轮询到完成 → 删除任务和文件 | 完成且清理成功 → pass；30 秒未完成 → warn；任务报错 → fail |

离线探针会在网盘根目录临时创建一个约 1 KB 的文件（来源 `https://example.com/index.html`），任务删除时 `delete_files=true` 一并清掉；报告里 `cleaned` 字段说明清理结果。
