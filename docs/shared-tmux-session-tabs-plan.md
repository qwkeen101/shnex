# Shnex 双端共享顶部会话标签方案（隐形 tmux 真源）

> 本版已按代码评审修订（评审要点见第 0 节）。范围：Go 核心 `core/`、桌面 `desktop/frontend/`、Android `mobile/android/`。

## 0. 评审修订要点

| # | 原方案 | 问题（代码证据） | 修订 |
| --- | --- | --- | --- |
| 1 | PATCH/DELETE 用 `SanitizeWorkspaceName` 清洗 `{name}` | 空或全非法输入会被改写成 `shnex-main`，`a.b` 会落到 `ab` 上（`core/relay/bootstrap.go:18-27`），会误杀主会话 | 清洗结果 ≠ 原值直接 400 |
| 2 | 按 `ActivityUnix` 或创建时间排序 | 活动时间随输出变化，标签顺序乱跳 | 固定按 `#{session_created}` 升序 |
| 3 | `list-sessions` 的 `#{pane_current_command}` 判 busy | 只反映当前窗口的活动 pane | `list-panes -a` 按会话汇总 |
| 4 | `-F` 里写 `'\t'`，中文标题直接存取 | 单引号内 `\t` 不会变成制表符；exec 通道不带任何环境变量（`core/ssh/monitor.go:63-81`），tmux 可能把非 ASCII 输出成 `_` | 一律 `tmux -u`；由 Go 在格式串中放真实制表符；标题放最后一列用 `SplitN` |
| 5 | 查列表后生成 `shnex-N` | 两端同时新建会撞名 | ID 用 `shnex-<base36 时间戳+随机>`，编号只体现在标题里 |
| 6 | `-t <name>` | tmux 目标是前缀/模式匹配，`shnex-1` 不存在时可能命中 `shnex-10` | 精确目标 `-t '=<id>'` |
| 7 | 直接 `new-session -A` 附着 | 被杀的会话会在另一端自动重连时被"复活"（桌面断线 2 秒自动重连：`TerminalPane.tsx:947-966`，且重连不带 relay：`App.tsx:952-959`） | 新增 `must_exist`：会话不存在返回 410，客户端移除标签 |
| 8 | 每个标签各自 `POST /sessions` | 每次都重新拨号认证（`core/ssh/ssh.go:118-155`）；未记住密码的主机每开一个标签都会弹密码框 | 保留"每个已打开的标签一条连接"，客户端在内存里复用本次输入的密码 |
| 9 | 顶部再加一条远端会话标签 | 桌面（`App.tsx:1242`）与 Android（`MainActivity.kt:746`）已有本地标签条，会出现两层标签 | 合并：tmux 主机的标签就是远端会话列表 |
| 10 | `×` 结束远端会话 | 现有 `×` 只关本地视图（`App.tsx:1050-1070`、`WorkbenchViewModel.kt:447-456`），语义被悄悄改变 | 明确 `×` = 结束对话，切走 = 后台保活 |
| 11 | 返回 `current` | `summary()` 返回未清洗的 `relayCfg.SessionName`，auto 模式下为空（`core/api/server.go:1045`） | 去掉 `current`，`summary()` 返回清洗后的 `workspace_name` |
| 12 | 7.1「云端自测 go test / tsc」 | 与工作流冲突：开发机不装依赖、不编译 | 单测随代码提交，由本地编译运行 |
| 13 | 「完全同步画面」 | `window-size latest`：尺寸跟随最近操作的一端，另一端会重排 | 验收改为"内容同步，尺寸跟随最近操作的一端" |

---

## 1. 底层矛盾与核心原则

1. **割裂原因**：两端都把「本地标签页」当成会话单元，真正跑着 Claude Code 的远端 `tmux` 却被写死成单一默认会话。桌面右侧旧 `tmux` 抽屉是往终端 PTY 打 `tmux attach -t ...`（[`App.tsx`](../desktop/frontend/src/App.tsx#L2313-L2316)），前台跑着 Claude Code 时会把命令注入对话框或触发嵌套 tmux；手机端完全看不到同主机上的其他会话。
2. **核心原则**：
   - **目标主机是唯一真源**：会话活在远端 `tmux` 里，电脑和手机各自通过 SSH 直查同一台主机，零云端同步、零账号。
   - **一层标签**：tmux 主机的顶部标签就是该主机的远端对话列表（`对话1`、`对话2`…… + `+`），不暴露 session / window / pane 概念。
   - **从不往 PTY 打命令**：切换、新建、改名、结束全部走 API。
   - **适用范围**：仅限 relay 为 `tmux`、或 `auto` 且远端装有 tmux 的主机。全局默认 relay 是 `none`（`core/settings/store.go:240`），这类主机以及 zellij 主机保持现状。

---

## 2. 会话 ID 与中文标题解耦

1. **会话 ID**：纯 ASCII `[A-Za-z0-9_-]{1,48}`。
   - 新建时生成 `shnex-<base36 毫秒时间戳><2 位随机>`，不先查列表，天然无竞态。
   - 主机默认会话（如 `shnex-main`）照旧。
   - 所有 tmux 命令一律用精确目标：`-t '=<id>'`；`set-option` 用 `-t '=<id>:'`（需实机确认 tmux 版本兼容）。
2. **展示标题**：存在远端 tmux 会话的用户选项 `@shnex_title` 里，随列表一起返回。
   - 写入前去掉控制字符，限 40 个字符；默认标题为 `对话<N>`（N 取现有数量 + 1，重号无害）。
   - 未设置标题的会话显示其 ID。
   - 任一端改名，另一端刷新即可看到，无需同步文件。
   - 若实机发现 `tmux -u` 仍会改写中文，备选方案是标题以 hex 编码存储（纯 ASCII，零风险）。
3. **外部会话**：名字不满足 `[A-Za-z0-9_-]{1,48}` 的会话（如用户手建的 `my proj`、`a.b`）不进列表。否则附着时会被清洗成另一个名字，`-A` 还会新建出一个错误的会话。

---

## 3. Go 核心统一 API（桌面与 Android 共用）

涉及文件：
- **新增**：`core/api/tmux_handlers.go`、`core/api/tmux_handlers_test.go`
- **修改**：[`core/api/server.go`](../core/api/server.go#L271)（注册路由；`summary()` 返回清洗后的 `workspace_name`）、`core/transport/transport.go`（`RelayConfig` 增加 `must_exist`）、`core/ssh/ssh.go`（`must_exist` 检查）

### 3.1 数据结构

```go
type RemoteTmuxSession struct {
    ID              string `json:"id"`               // 精确 tmux session_name
    Title           string `json:"title"`            // @shnex_title，为空时回退 ID
    Command         string `json:"command"`          // 活动窗口活动 pane 的命令，用于确认文案
    Busy            bool   `json:"busy"`             // 任一 pane 运行着非 shell 命令
    Path            string `json:"path"`             // 活动 pane 的当前目录
    AttachedClients int    `json:"attached_clients"` // #{session_attached}
    CreatedUnix     int64  `json:"created_unix"`     // #{session_created}，排序依据
}
```

- **列表命令**（由 Go 拼接，分隔符为真实制表符字节，标题放最后一列，解析用 `SplitN`）：
  `tmux -u list-panes -a -F '<session_name>\t<session_created>\t<session_attached>\t<window_active>\t<pane_active>\t<pane_current_command>\t<pane_current_path>\t<@shnex_title>'`
- **Busy**：某会话任一 pane 的命令去掉前导 `-` 后不属于 `bash zsh sh fish dash ash ksh` 即为 busy。
  - 已知盲区：shell 里挂着后台任务（`&`、`nohup`）时前台仍是 shell，会被判为空闲。
- **排序**：按 `CreatedUnix` 升序，同值按 ID，保证两端顺序一致且不跳动。

### 3.2 四个接口（基于现有 `ms.sess.(transport.CommandExecutor)`）

所有接口先 `s.getManagedSession(r.PathValue("id"))` 并断言 `transport.CommandExecutor`；本地会话或不支持 exec 的会话返回明确状态码。

1. **`GET /api/v1/sessions/{id}/tmux`**
   - 先 `command -v tmux`，没有则返回 `{ "available": false, "sessions": [] }`，前端平滑退化。
   - 否则返回 `{ "available": true, "sessions": [...] }`。本连接所在会话由 `GET /api/v1/sessions` 中的 `workspace_name` 给出，不再单独返回 `current`。
2. **`POST /api/v1/sessions/{id}/tmux`**
   - 请求体：`{ "title"?: string }`（v1 不做 `cwd`）。
   - 生成随机 ID，执行 `tmux -u new-session -d -s <PosixQuote(id)>`，再执行 `tmux -u set-option -t '=<id>:' @shnex_title <PosixQuote(title)>`。
   - 返回新的 `RemoteTmuxSession`，前端随即按 3.3 附着。
3. **`PATCH /api/v1/sessions/{id}/tmux/{name}`**
   - 请求体：`{ "title": string }`。
   - `{name}` 必须满足 `SanitizeWorkspaceName(name) == name`，否则 400；标题去控制字符、限长后执行 `set-option`。
   - 返回 `{ "id", "title" }`。
4. **`DELETE /api/v1/sessions/{id}/tmux/{name}`**
   - 校验同上，执行 `tmux kill-session -t '=<name>'`。
   - 注意：若执行 kill 的连接本身就附着在该会话上，tmux 客户端退出会触发 `monitorWait` 关掉整条连接（`core/ssh/ssh.go:880-895`），响应可能丢失。客户端按第 4 节容错。

### 3.3 附着与防"复活"

1. **附着**：`applySavedHostProfile` 在请求显式带 `relay.mode` 时保留请求中的 relay（`core/api/workbench_handlers.go:994-1003`），所以附着指定会话只需：
   ```json
   { "host_id": "<hostId>", "relay": { "mode": "tmux", "session_name": "<id>", "must_exist": true } }
   ```
2. **`must_exist`**（新增）：
   - `OpenSession` 在启动 bootstrap 前，用同一连接执行 `tmux has-session -t '=<id>'`。
   - 不存在则返回 410 `TMUX_SESSION_GONE`，客户端移除该标签并刷新列表。
   - 从标签附着和自动重连都必须带 `must_exist: true`；只有 `+` 新建和主机默认连接允许创建会话。
3. **`window-size latest`**：当前在附着前用 `tmux start-server \; set-option -g ...` 设置（`bootstrap.go:94`、`ssh.go:383`），冷启动时可能丢失。v1 **不改**：tmux ≥ 3.1 默认即为 `latest`，而改成附着命令里串 `\; set-option` 在老版本 tmux 上有附着失败的风险。实机若发现尺寸不跟随，再改。

---

## 4. 切换、结束与后台保活

1. **后台保活**：关 App、锁屏、切到别的标签都**不**结束远端会话，tmux 与 Claude Code 在服务器上继续运行。
2. **`×` = 结束这个对话**（不再只是关闭本地视图）：
   - 空闲（`!busy`）：直接结束。
   - 忙（`busy`）：一行确认「`<title>` 正在运行 `<command>`，确定结束？」→ `取消` / `结束对话`。
3. **结束顺序**：
   1. 先把本地标签标记为"结束中"，禁用它的自动重连；
   2. 调 `DELETE .../tmux/{name}`；
   3. 关闭本地会话（`DELETE /api/v1/sessions/{sessionId}`）。
   - kill 之后出现的断线或 502 一律视为成功，以刷新后的列表为准。
   - 若关闭的是当前标签，自动切到相邻标签。
4. **另一端**：它附着的会话被杀后连接断开，重连时因 `must_exist` 收到 410，标签自动消失，不会复活成空会话。
5. **密码**：两端各维护一份仅存于内存的 `hostId → 本次输入的密码`，同主机后续附着自动带上，避免每个标签弹一次密码框。不新增持久化。复用走的是现有密码重试的同一条请求路径（含 `host_key.insecure_ignore`，`desktop/frontend/src/api.ts:647`、`CoreApiClient.kt:266`），即只作用于本次运行里用户已经输过密码的那台主机，不扩散到其他主机。

---

## 5. 桌面端改造

涉及文件：[`api.ts`](../desktop/frontend/src/api.ts)、[`App.tsx`](../desktop/frontend/src/App.tsx)、[`TerminalPane.tsx`](../desktop/frontend/src/components/TerminalPane.tsx)

1. **`api.ts`**：新增 `RemoteTmuxSession` 类型与 `listRemoteTmuxSessions` / `createRemoteTmuxSession` / `renameRemoteTmuxSession` / `killRemoteTmuxSession`；`createSession` 透传 `relay`（含 `must_exist`）。
2. **`WorkspaceTab` 增加 `tmuxId?: string`**（`App.tsx:114-138`）：
   - tmux 主机的顶栏标签就是远端会话列表。已附着的照常显示；未附着的显示为暗色，点击时懒附着。
   - 判重按 `(hostId, tmuxId)`。
   - 本地 shell、Markdown、非 tmux 主机的标签保持原样。
3. **交互**：
   - 双击标签就地改名，`Enter` / 失焦提交，`Esc` 取消。
   - `+` 直接新建并附着，无弹窗。
   - `×` 按第 4 节处理。
   - 审批琥珀点只对本机已附着的标签有效（检测来自各连接的 PTY 输出，`server.go:957-990`）。
4. **重连**：`reconnectPane`（`App.tsx:940-965`）必须带上该标签的 `tmuxId` 与 `must_exist: true`。收到 410 时移除标签，不再重试。
5. **刷新时机**：连接成功、窗口重新获得焦点、每次增删改之后。v1 不做 10 秒轮询。
6. **移除旧抽屉**：
   - `RightPanelType` 中的 `"tmux"`（`App.tsx:154`）
   - 自动加载与轮询（`:460-473`）
   - 抽屉面板（`:2271-2330`）及 `tmuxSessionsOut` / `newTmuxName` 状态
   - 右栏按钮与设置项 `right_rail_items.tmux`（`:2821-2830`、`api.ts:258`）

---

## 6. 手机端改造

涉及文件：`data/CoreApiClient.kt`、`ui/WorkbenchViewModel.kt`、`MainActivity.kt`

1. **`CoreApiClient.kt`**：新增 `RemoteTmuxSession` 与 4 个 `/tmux` 方法；`createSession(...)` 增加 `tmuxSession: String? = null`，非空时写入 `relay = { mode: "tmux", session_name, must_exist: true }`。
2. **`WorkbenchViewModel.kt`**：
   - `LiveTerminalTab` 增加 `tmuxId`；按 `hostId` 保存远端会话列表。
   - `connectHost` 现在"同主机已有标签就复用"（`WorkbenchViewModel.kt:334-340`），改为按 `(hostId, tmuxId)` 判重。
   - 连接成功、回到会话页、App 回到前台（`ON_RESUME`）、每次增删改后刷新。
   - Android 目前断线只把 `tab.connected` 置为 false（`WorkbenchViewModel.kt:433`）。点击断开的标签时按 `must_exist` 重新附着，收到 410 就移除标签。
   - 提供 `switchToRemoteSession` / `createAndAttachRemoteSession` / `renameRemoteSession` / `killRemoteSession`。
3. **`MainActivity.kt`（`SessionsScreen`）**：复用现有横向标签条（`MainActivity.kt:746-805`），不新增第二条。
   - tmux 主机的远端会话直接作为标签，未附着的显示为暗色。
   - 末尾加 `+`；长按改名；`×` 按第 4 节处理，忙时用 `AlertDialog` 确认。
   - 只懒附着，不预连所有会话。

---

## 7. 验证与验收

### 7.1 单元测试（随代码提交，本地运行）
开发机不装依赖、不编译，以下由本地执行：
1. `cd core && go test ./...`。新增 `core/api/tmux_handlers_test.go` 覆盖：
   - 真实制表符的列表解析、标题含制表符或中文时的 `SplitN`
   - `@shnex_title` 为空时回退 ID
   - 多 pane 的 busy 汇总、按创建时间排序
   - 不合规的会话名被过滤、`{name}` 清洗不一致返回 400
   - `__NO_TMUX__` 降级
   - 生成的 ID 合规
2. `cd desktop/frontend && npx tsc --noEmit`。

### 7.2 本地编译与实机验收（Windows + Android）
> [!IMPORTANT]
> 本次修改了 `core/`，打包 Android 时**必须去掉 `-SkipCoreBind`**，重新执行 `gomobile bind`。

1. **打包**：`desktop\scripts\package-windows.ps1` 与 `mobile\scripts\package-android.ps1` 均退出码 `0`。
2. **双端联动**：
   - 电脑连接一台 tmux 主机，点 `+` 新建，双击改名为 `api修复`，在其中启动 `claude`。
   - 手机打开同一主机：顶部标签直接出现原对话与 `api修复`（中文标题无乱码）；点开后内容与电脑一致，尺寸跟随最近操作的一端。
   - 未记住密码的主机：同一次运行中打开第二个标签不再弹密码框。
3. **结束与防复活**：
   - 空闲标签点 `×` 直接结束；运行 `claude` 的标签点 `×` 先确认。
   - 结束后另一端的同名标签自动消失；服务器上 `tmux ls` 确认会话已销毁，且没有被重连"复活"出空会话。
4. **精确匹配**：存在 `shnex-10`、不存在 `shnex-1` 时，对 `shnex-1` 的改名或结束不影响 `shnex-10`。
