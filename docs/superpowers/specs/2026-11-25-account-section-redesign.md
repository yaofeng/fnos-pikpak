# 设置页账号区重构 + 账号重命名 设计（短设计）

日期：2026-11-25　|　状态：待批准　|　基于 v2.0.0（main）

## 意图（用户原话归纳）

1. 设置页账号区直接改为原型图样式，不再经由 /bind 二级页面。
2. 「添加账号」按钮放在「PikPak 账号」标题行最右侧（标题行同时保留右侧「已绑定 N 个」徽章）。
3. 每个账号支持重命名；切换器等处只显示账号名称。

## 原型图映射

标题行：`PikPak 账号`（左）…… `已绑定 N 个`（绿色徽章）+ `添加账号`（最右，文字指令覆盖原图的底部按钮位）。
账号行：头像圆（avatar，空则灰底占位）+ 加粗名称 + `novip`/VIP 灰字 + 状态徽章（`正常`/`需重新授权`，绿/红）……行右侧 `重命名` `重新授权` `解绑` 三个链接操作。
行间细分隔线；零账号时列表区显示一行说明文案 + 标题行按钮照常可用。

## 后端（新增，小）

- Store：`SetAccountDisplayName(ctx, uid, accountID, name string) error` — `UPDATE pikpak_accounts SET display_name=? WHERE id=? AND uid=?`，检查 rows-affected（0 → ErrNotFound，即越权/不存在 404）；空字符串 → 置 NULL（回退 id 显示）。
- 路由：`POST /api/accounts/{id}/rename` body `{"display_name"}` — 身份来自 `fnos.IdentityFrom`，归属校验同现有账号路由；校验：去首尾空格后非空、≤50 字符，违规 → 400 中文文案（「账号名称不能为空」/「账号名称过长，最多 50 个字符」）；成功 → 200 `{item: AccountInfo 同形}`（重新探测该账号快照）。
- 重绑定不冲掉重命名：`PutAccount` 的 rebind 分支原样回写整行，display_name 保持用户改过的值（现状已如此，加一个测试钉住）。

## 前端

1. **组件拆分**：把 `BindCard` 拆成 `AccountSection`（原型图列表 + 标题行）与 `FlowPanel`（设备码流程进度：验证链接/用户码/轮询）。`AccountSection` 内嵌流程状态——添加或重新授权时在对应行下方展开 `FlowPanel`，完成后刷新列表。
2. **Settings 页**：账号区直接渲染 `AccountSection`（含零账号态）；现有 Settings 的重新授权/解绑逻辑并入行操作。
3. **/bind 收口**：路由重定向到 `/settings`；App 未绑定提示条与导航链接改指 `/settings`；Debug 页保留现样（调试页不动）。
4. **重命名交互**：行内「重命名」→ 名称变行内输入框（预填当前名，回车/失焦提交，Esc 取消）；提交调 rename 接口，成功后 `reload()`（Context 刷新，切换器随之更新）。重名允许（显示用标签，非唯一键）。
5. **切换器只显示名称**：`AccountSwitcher` 选项精简为名称一行（`accountLabel`：重命名名 → display_name → id 前 6 位 → 「默认账号」）；移除选项内的 VIP 标记、用量、状态徽章。账号状态信号由 App 顶部的逐账号 reauth 横幅承担（已有）。触发器（收起态）同样只显示当前账号名。
6. **i18n**：新增文案全部 zh + en 双条目（重命名、添加账号、已绑定 N 个等）。

## 非目标

- 头像上传/编辑（仅展示上游 avatar）。
- 账号排序调整（保持绑定顺序）。
- /bind 独立页保留（重定向收口）。

## 验证

- 后端：store 方法测试（归属 404、空名置 NULL、rebind 保名）；路由测试（越权 404、400 文案、成功回显）；`go test ./...` 全绿。
- 前端：纯逻辑单测（名称截断/回退已有 accountLabel 测试扩展 rename 后形态）；`npm test`、`npm run lint`、`npx tsc -b` 全绿。
- 冒烟：设置页直接添加第二个账号 → 切换器出现；重命名后切换器/徽章/合并页全部显示新名；重新授权不清掉重命名。

## 执行方式建议

改动跨 store/路由/三个前端组件，但接口都已明确、无设计分歧——建议**本会话直接实施**（不做子代理流水线），完成后重建 fpk 并更新 v2.0.0 release 附件。
