# 页面状态保活（Activity）设计 — 短设计

日期：2026-11-25　|　状态：待批准　|　分支：feature/multi-account（并入当前多账号交付）

## 意图

SPA 内切换页面后，回到原页面能恢复之前的 UI 状态：全网搜索的搜索词与结果集、分享收藏夹的浏览目录层级、各页筛选条件、滚动位置等。F5 刷新后不要求恢复（用户已确认）。

## 现状与根因

- `web/src/App.tsx` 用 react-router-dom v7 `<Routes>` 渲染页面——离开路由即卸载组件，组件级 UI 状态全部丢失。
- `web/src/listCache.ts` 已有页面级数据缓存（切标签不重查 PikPak），所以缺的只是组件状态与 DOM（滚动）。

## 方案（用户已选定 A）

React 19.2+ 的 `<Activity>` 组件包裹各路由页面，配合"首次访问才挂载"的 visited 集合：

1. `<Routes>` 的 URL 语义不变（NavLink 高亮、`/files/*` 路径导航、重定向），但每页 element 经 KeepAlive 包装渲染为 `<Activity mode={active ? "visible" : "hidden"}>`。
2. **visited 集合**：页面首次激活才挂载进 Activity——首屏只挂当前页，避免十页全量挂载；已访问页切走后保持挂载、状态与 DOM 全保留（滚动位置随之保留）。
3. **隐藏语义免费获得**：Activity hidden 时 Effects 自动卸载——Transfers/Tasks 等轮询页的定时器在隐藏时暂停；回显时 Effects 重跑，`listCache` 命中则立即回放数据、再静默刷新。
4. **切账号重置**：Activity 内的页面以 accountID 参与组件 key——切换账号时该页重新挂载，UI 状态（目录栈、搜索词）清空并按新账号重查，与 Task 10 的按账号缓存键自然衔接。
5. lazy 页（Debug）在首次激活时才 resolve，与 Suspense 兼容。

**保活页面**：Files、Recent、Tasks、Transfers、Trash、Favorites（含 ShareBrowser）、VideoFavorites、Search、Settings。
**不保活**：Bind（流程页）、Debug（调试页）。

## 非目标

- F5 / 关闭后恢复状态（明确不做，用户确认）。
- 页面切换动画（v1 不引入 view transition）。
- 后端改动：零——纯前端 `web/src`。

## 权衡过的方案

- **B：页面状态仓库**（每页卸载前 save、挂载时 restore）——逐页搬运十几个 useState，漏一个状态就丢一处，滚动位置还得单独存；侵入大，弃。
- **C：混合**——无必要，弃。

## 留给 builder 的

- visited 集合与包装组件的实现位置（App.tsx 内部）。
- 各页 useEffect 重跑行为确认（有 listCache 兜底，预期只读确认、不逐页改代码）。
- 测试锚点：`keepAlive.ts` 纯逻辑（路径归属、visited 集合、账号 key）走 vitest 单测（仓库无 DOM 测试栈、不新增依赖，组件级行为不单测）；路由切回状态保留、隐藏页轮询暂停、切账号目录栈重置归入 Task 12 的 dev 冒烟清单。现有 `npm test`、`npm run lint`、`npx tsc -b` 全绿；全局约束"每个任务 `go test ./...` 全绿"照常（后端未动）。

## 交付方式

作为新任务并入 `docs/superpowers/plans/2026-11-24-multi-account.md`（Task 13，在前端 Task 9–11 之后、最终验证 Task 12 之前执行，台账记录执行顺序）。
