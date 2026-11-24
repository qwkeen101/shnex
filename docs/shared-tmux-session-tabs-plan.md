# Shnex 双端共享顶部会话标签方案（隐形 tmux 真源）

## 1. 底层矛盾与核心原则

1. **当前割裂原因**：桌面端与手机端目前都把「本地标签页」当成了会话单元，而把真正跑着 Claude Code 的远端 `tmux` 隐藏写死成单一默认会话（如 `main`）。桌面端右侧虽有旧版 `tmux` 抽屉，但点击切换时是直接向终端 PTY 写入 `tmux attach -t ...` 命令（[`desktop/frontend/src/App.tsx`](../desktop/frontend/src/App.tsx#L2313-L2316)），一旦前台正在运行 Claude Code 就会误注入对话框或触发 nested tmux 报错；手机端则完全看不到同台主机上的其他会话。
2. **核心原则（「少即是多」+ 目标主机唯一真源）**：
   - **零云端同步服务、零账号**：所有活着的会话本身就在目标服务器的 `tmux` 中，电脑与手机通过各自已有的 SSH 通道直查同一台主机的 `tmux`，天然 100% 一致。
   - **界面去 tmux 术语化**：不向用户暴露 `session / window / pane` 概念，电脑端和手机端统一在顶部平铺为直观的横向对话标签（如 `对话1`、`对话2`、`+`），一键秒切、不遮挡终端内容。
   - **淘汰桌面端旧右侧 `tmux` 抽屉**：彻底移除容易往 PTY 误输入命令的右侧 `tmux` 面板。

---

## 2. UTF-8 中文标题与安全会话 ID 解耦

为了既支持用户自由显示/修改中文标签名（如 `对话1`、`前端修复`），又保证远端 shell 拼接与 `relay.SanitizeWorkspaceName` 绝对安全：

1. **内部会话 ID（纯 ASCII `[a-zA-Z0-9_-]`）**：
   - 底层 `tmux` 的 `session_name` 始终使用安全字符集（经过 [`relay.SanitizeWorkspaceName`](../core/relay/bootstrap.go#L14-L29) 校验），如主机的默认会话名（`main` / `shnex-main`）以及新建时自动递增的 `shnex-2`、`shnex-3`……。
2. **外部展示标题（UTF-8，存于远端 `tmux` 用户选项 `@shnex_title`）**：
   - 借助 `tmux set-option -t <id> @shnex_title '<title>'` 将展示名称直接保存在远端 `tmux` 服务端内存里，并通过 `#{@shnex_title}` 随 `list-sessions` 一并返回。
   - 若某外部已存在的 `tmux` 会话尚未设置 `@shnex_title`，读取时自动回退显示其原始 `id`；电脑或手机任意一端改名，另一端刷新即可看到同一份 UTF-8 标题，无需额外同步文件。

---

## 3. Go 核心统一 API（桌面与 Android 共用）

涉及文件：
- **新增**：`core/api/tmux_handlers.go`
- **新增单测**：`core/api/tmux_handlers_test.go`
- **修改路由**：[`core/api/server.go`](../core/api/server.go#L271)（在 `buildMux()` 注册以下 4 个受 Bearer Token 保护的端点）

### 3.1 数据结构与繁忙态判定

```go
type RemoteTmuxSession struct {
    ID              string `json:"id"`               // 安全 ASCII tmux session_name
    Title           string `json:"title"`            // #{@shnex_title}，为空时回退 ID
    Command         string `json:"command"`          // #{pane_current_command}，如 claude / node / bash
    Busy            bool   `json:"busy"`             // 是否处于非空闲命令（用于防误杀确认）
    Path            string `json:"path"`             // #{pane_current_path}
    AttachedClients int    `json:"attached_clients"` // #{session_attached}
    Windows         int    `json:"windows"`          // #{session_windows}
    ActivityUnix    int64  `json:"activity_unix"`    // #{session_activity}
}
```

- **`Busy` 判定规则**：`Command` 转小写并去掉前导 `-`（login shell 如 `-bash`）后，属于空闲交互式 Shell 集合（`bash`、`zsh`、`sh`、`fish`、`dash`、`ash`、`ksh`）时为 `false`，其余（如 `claude`、`node`、`python`、`vim`、`nvim`、`ssh` 等）均为 `true`。

### 3.2 四个 REST 接口（基于现有 `ms.sess.(transport.CommandExecutor)`）

所有接口先通过 `s.getManagedSession(r.PathValue("id"))` 获取已建立的 SSH 会话并断言 `transport.CommandExecutor`；本地会话或未实现 `CommandExecutor` 返回明确状态码。

1. **`GET /api/v1/sessions/{id}/tmux`**
   - 执行远端探测与列表命令（按 `\t` 分隔解析，避免空格路径歧义）：
     `command -v tmux >/dev/null 2>&1 || { echo '__NO_TMUX__'; exit 0; }; tmux list-sessions -F '#{session_name}\t#{@shnex_title}\t#{pane_current_command}\t#{pane_current_path}\t#{session_attached}\t#{session_windows}\t#{session_activity}' 2>/dev/null || true`
   - 若远端未安装 `tmux`，返回 `{ "available": false, "current": "", "sessions": [] }`（不报错弹窗，前端平滑退化）。
   - 返回：`{ "available": true, "current": "<当前 managedSession 绑定的 workspace_name>", "sessions": [...] }`（按 `ActivityUnix` 或创建时间稳定排序，保证顶部标签顺序可预期）。

2. **`POST /api/v1/sessions/{id}/tmux`**
   - 请求体：`{ "title"?: string, "cwd"?: string }`
   - 查询现有会话列表，生成不冲突的安全 ASCII `id`（如 `shnex-2`、`shnex-3`……），默认展示标题为 `对话<N>`（若请求未传 `title`）。
   - 远端执行：`tmux new-session -d -s <PosixQuote(id)> [-c <PosixQuote(cwd)>]`，随后执行 `tmux set-option -t <PosixQuote(id)> @shnex_title <PosixQuote(title)>`（并将 `window-size latest` 设好）。
   - 返回新创建的 `RemoteTmuxSession` 对象，前端拿到 `id` 后直接通过 `POST /api/v1/sessions` 附着。

3. **`PATCH /api/v1/sessions/{id}/tmux/{name}`**
   - 请求体：`{ "title": string }`
   - 对路径参数 `{name}` 先用 `relay.SanitizeWorkspaceName` 清洗（拒绝空值或非法修改），再用 `relay.PosixQuote` 包裹执行：
     `tmux set-option -t <PosixQuote(cleanName)> @shnex_title <PosixQuote(trimmedTitle)>`
   - 零 Shell 注入风险，返回 `{ "id": cleanName, "title": trimmedTitle }`。

4. **`DELETE /api/v1/sessions/{id}/tmux/{name}`**
   - 对 `{name}` 用 `relay.SanitizeWorkspaceName` + `relay.PosixQuote` 执行：
     `tmux kill-session -t <PosixQuote(cleanName)>`
   - 立即终止该远端会话并释放服务端内存。

### 3.3 附着指定远端会话（无需新增后端代码）
[`core/api/workbench_handlers.go`](../core/api/workbench_handlers.go#L994-L1003) 中的 `applySavedHostProfile` 已规定：当请求体显式传入 `relay.mode != ""` 时直接保留请求中的 `relay` 配置（不会被主机默认 `tmux_session` 覆盖）。因此桌面端与手机端只需调用 `POST /api/v1/sessions` 并传入：
```json
{
  "host_id": "<hostId>",
  "relay": { "mode": "tmux", "session_name": "<remoteTmuxId>" }
}
```
即可直接附着到该主机上的任意指定远端会话。

---

## 4. 智能防误触关闭与后台保活语义

1. **后台保活（不杀进程）**：关闭桌面端/手机端 App、手机锁屏、或点击切换去其他顶部标签时，**绝不调用 `DELETE /tmux/{name}`**，远端 `tmux` 与 Claude Code 在服务器上天然继续运行。
2. **点击标签 `×` = 真正结束该远端对话并释放内存**：
   - **当 `!session.busy`**（空闲 Shell）：点 `×` **直接执行** `DELETE /api/v1/sessions/{id}/tmux/{name}`，并关闭本地对应的 WebSocket 会话（`DELETE /api/v1/sessions/{sessionId}`），零弹窗秒关。
   - **当 `session.busy`**（正在跑 `claude`、`node`、`python`、`vim` 等）：弹出极简一行确认：
     「**`<title>` 正在运行 `<command>`，确定结束并释放内存？**」→ `[取消]` / `[结束对话]`。
   - 结束成功后立即刷新当前主机远端会话列表，若关闭的是当前正在看的标签，自动切换到相邻会话；电脑和手机双端同步消失。

---

## 5. 桌面端改造

涉及文件：[`desktop/frontend/src/api.ts`](../desktop/frontend/src/api.ts)、[`desktop/frontend/src/App.tsx`](../desktop/frontend/src/App.tsx)、[`desktop/frontend/src/components/TerminalPane.tsx`](../desktop/frontend/src/components/TerminalPane.tsx)

1. **`api.ts`**：新增 `RemoteTmuxSession` 类型定义及 `listRemoteTmuxSessions`、`createRemoteTmuxSession`、`renameRemoteTmuxSession`、`killRemoteTmuxSession` 四个请求函数；确认 `createSession` 透传 `relay`。
2. **`App.tsx` 顶部横向标签与切换**：
   - 在当前已连接主机的面板顶部（紧跟 `SSH <主机名>`）或工作区顶栏横向平铺该主机的全部远端会话标签（`对话1`、`对话2`…… + 末尾 `+` 按钮）。
   - **一键秒切**：点击标签时，若本机已打开挂载到该 `workspaceName` 的标签/面板则直接聚焦；否则用 `createSession({ host_id, relay: { mode: "tmux", session_name: s.id } })` 懒附着并切入，**绝不往 PTY 输入框打字**。
   - **双击改名**：双击标签文字切换为内联输入框，`Enter` / 失焦调用 `renameRemoteTmuxSession` 提交，`Esc` 取消。
   - **`+` 一键新建**：点击 `+` 直接调用 `createRemoteTmuxSession` 并立即附着打开（无需弹窗打断）。
   - **状态指示**：待 Claude 审批时标签圆点亮琥珀黄，正常亮绿；当 `attached_clients > 1` 时显示轻量的双端附着小标识。
   - **智能 `×`**：按第 4 节规则结束远端会话并清理本地标签。
   - **自动同步刷新**：连接成功、每次增删改名、窗口重新获得焦点（`window.addEventListener("focus")`）时刷新；焦点期间每 ~10 秒轻量轮询一次，确保手机端新建/改名/结束的对话在电脑端自动出现/消失。
   - **移除旧 `tmux` 抽屉**：删除右侧工具栏的 `tmux` 按钮与旧抽屉面板（及相关 `tmuxSessionsOut` / `newTmuxName` 状态）。

---

## 6. 手机端改造

涉及文件：
- [`mobile/android/app/src/main/java/dev/shnex/app/data/CoreApiClient.kt`](../mobile/android/app/src/main/java/dev/shnex/app/data/CoreApiClient.kt)
- [`mobile/android/app/src/main/java/dev/shnex/app/ui/WorkbenchViewModel.kt`](../mobile/android/app/src/main/java/dev/shnex/app/ui/WorkbenchViewModel.kt)
- [`mobile/android/app/src/main/java/dev/shnex/app/MainActivity.kt`](../mobile/android/app/src/main/java/dev/shnex/app/MainActivity.kt)

1. **`CoreApiClient.kt`**：
   - 新增 `RemoteTmuxSession` 数据类与 4 个 `/tmux` REST 方法（`listRemoteTmux`、`createRemoteTmux`、`renameRemoteTmux`、`killRemoteTmux`）。
   - `createSession(...)` 增加可选参数 `tmuxSession: String? = null`：当非空时在请求 JSON 中写入 `relay = { "mode": "tmux", "session_name": tmuxSession }`。
2. **`WorkbenchViewModel.kt`**：
   - 按 `hostId` 维护当前主机的 `remoteTmuxSessions` 列表与映射（兼容每个会话的本地 `LiveTerminalTab`）。
   - 在连接成功、回到会话页 / App 回到前台（`ON_RESUME`）及每次新建/改名/结束后刷新列表，让电脑端开的对话手机打开就能看到。
   - 提供 `switchToRemoteSession(hostId, remoteSession)`（已有连接 tab 直接切，没有则懒附着打开）、`createAndAttachRemoteSession(hostId)`、`renameRemoteSession(...)`、`killRemoteSession(...)`。
3. **`MainActivity.kt`（`SessionsScreen`）**：
   - 在「会话」页顶部终端栏渲染当前主机的横向可滚动标签条（`LazyRow` / `horizontalScroll`）：展示 `对话1`、`对话2`……、审批黄点、末尾 `+` 新建按钮与每个标签上的 `×` 关闭按钮。
   - 长按标签弹出极简改名框；点击 `×` 时若 `session.busy == true` 弹出 Material 3 `AlertDialog` 确认释放内存，空闲会话直接结束。

---

## 7. 自测与验收标准

### 7.1 云端自测命令（Linux）
1. `cd core && go test ./...` 全部通过（含新增的 `core/api/tmux_handlers_test.go`：验证 `\t` 列表解析、`@shnex_title` 回退、空闲/忙碌 `busy` 判定、`__NO_TMUX__` 降级、非法会话名清洗防注入及 HTTP 接口路由）。
2. `cd desktop/frontend && npx tsc --noEmit` 类型检查零报错。

### 7.2 本地编译与实机验收要点（Windows + Android）
> [!IMPORTANT]
> 本次修改了 `core/api/`，本地编包 Android 时**必须去掉 `-SkipCoreBind`**，重新执行 `gomobile bind` 生成最新 `shnexcore.aar`。

1. **打包验证**：Windows 执行 `desktop\scripts\package-windows.ps1` 与 Android 执行 `mobile\scripts\package-android.ps1` 均退出码 `0`。
2. **双端联动实测**：
   - 电脑端连接同一台 Linux 主机后点击顶部 `+` 新建「对话2」并双击改名为 `api修复`，在其中启动 `claude`。
   - 手机端切回或打开同一台主机，无需手动下拉或打命令，顶部横向标签条直接显示原对话与 `api修复`，点一下 `api修复` 立刻看到与电脑完全同步的 Claude 画面。
   - 在任意一端点击空闲会话标签 `×`：直接秒关并从双方消失；点击正在运行 `claude` 的 `api修复` 标签 `×`：弹出确认框，确认后服务端 `tmux ls` 确认该会话已销毁且内存释放。
