# 修复 Claude Code `/` 斜杠命令与终端智能补齐冲突方案

## 1. 底层矛盾

Shnex 的「内置智能补齐」本质上是 **Shell 提示符补全器**，但在 Claude Code 这类终端内嵌交互式 TUI 中未做上下文退让，直接触发了三重碰撞：

1. **提示符误识别**：[`desktop/frontend/src/components/TerminalPane.tsx`](../desktop/frontend/src/components/TerminalPane.tsx#L441-L518) 的 `extractCommandBeforeCursor` 使用 `/^.*?(?:❯|➜)\s/`（第 503 行）识别提示符，把 Claude Code 输入框前的 `❯ ` 直接当成 Shell 提示符（Android 端 [`mobile/android/app/src/main/java/dev/shnex/app/terminal/AnsiTerminalBuffer.kt`](../mobile/android/app/src/main/java/dev/shnex/app/terminal/AnsiTerminalBuffer.kt#L192-L213) 第 203 行存在同一问题）。
2. **首词斜杠误判为根目录路径**：[`core/completion/engine.go`](../core/completion/engine.go#L127-L131) 在首词位置（`len(pl.args) == 0 && !pl.hasTrailingSp`）只要检测到 `/` 就直接进入文件系统路径补全，把 `/`、`/model`、`/help`、`/resume` 等斜杠命令当成 Linux 根目录 `/` 下的路径补全。
3. **浮层遮挡并劫持按键**：[`desktop/frontend/src/components/TerminalPane.tsx`](../desktop/frontend/src/components/TerminalPane.tsx#L564-L575) 见 `cmdLine.includes("/")` 即在混合模式下自动展开列表（`listOpen = true`），紧贴光标下方渲染（第 1625–1632 行），正好遮盖 Claude Code 弹出的斜杠命令菜单，并在第 609–731 行拦截 `↑` / `↓` / `Tab` / `Enter` / `Esc`。

---

## 2. 表层修补的隐性代价

- **新增「Claude 模式 / 暂停补齐」开关**：强迫用户在每次进入或退出 Claude Code 时手动切换，增加多余概念与界面噪音，忘记切换时冲突依旧。
- **仅屏蔽单个 `/` 字符**：当用户继续输入 `/m`（`/model` → `/mnt/`）、`/h`（`/help` → `/home/`）、`/r`（`/resume` → `/root/`），或在 Claude 对话框内输入 `git `、`./src/` 时，依然会弹出 Shell 补全列表并劫持方向键与回车。

---

## 3. 最简本质方案（零新按钮、零新配置）

让系统顺应用户所处的真实上下文自动进退——**只在真实 Shell 提示符激活补全，首词斜杠命令不走根目录路径补全**。

### 3.1 前端：不猜 TUI，只清理旧提示符锚点

> 实现时修订：冲突只来自「首词是单段斜杠命令」，而不是「光标位于 Claude Code 输入框」。在 TUI 输入框内整体关闭补全，会误伤 Claude Code 里同样有用的 `cd /`、`./src/` 等路径补全，因此前端不再做框线 / 裸 `❯` 的 TUI 识别，斜杠命令的退让完全交给 3.2 的后端规则（候选为空 → 不弹浮层、不拦截按键）。

涉及文件：[`desktop/frontend/src/components/TerminalPane.tsx`](../desktop/frontend/src/components/TerminalPane.tsx)

1. **清理旧 `OSC 133;B` 锚点**：向 PTY 发送 `\r`/`\n`（Enter）或 `\x03`（Ctrl+C）时同步清空 `promptAnchorRef.current = null`，防止进入 TUI 后误命中旧 Shell 提示符行号。
2. **Android 端无需改动**：移动端补全读取的是输入框草稿（`WorkbenchViewModel.queryInputCompletions`），走同一个后端引擎；`AnsiTerminalBuffer.currentInputLineBeforeCursor` 当前无调用方。

### 3.2 后端引擎：首词斜杠命令不触发根目录路径补全

涉及文件：[`core/completion/engine.go`](../core/completion/engine.go)、[`core/completion/engine_test.go`](../core/completion/engine_test.go)

1. 在 `Engine.Suggest` Case 1（`len(pl.args) == 0 && !pl.hasTrailingSp`）增加判定辅助函数 `isFirstWordFilesystemPath(tok string) bool`：
   - 仅当 `tok` 以 `.` 开头（如 `./script`、`../run`）、包含 `\`，或包含 `/` 且**不是单一前导斜杠**（`!strings.HasPrefix(tok, "/") || strings.Count(tok, "/") >= 2`）时，才触发文件系统路径补全。
2. **行为边界**：
   - **静默返回空候选**：`/`、`/clear`、`/compact`、`/help`、`/model`、`/resume` 等单段斜杠命令。
   - **照常补全**：`cd /`、`ls /`、`cat /etc/...`（参数位）、`./script`、`../run`、`scripts/run.sh`（相对路径）、`/usr/bin/...`（两段及以上绝对路径）。
3. 在 [`core/completion/engine_test.go`](../core/completion/engine_test.go) 补充单元测试，验证首词 `/`、`/model` 返回空候选，同时 `cd /`、`./`、`/usr/bin` 等真实路径补全不受影响。

---

## 4. 验收标准

1. `cd core && go test ./...` 全部通过（含新增斜杠命令与路径边界单元测试）。
2. `cd desktop/frontend && npx tsc --noEmit` 类型检查零报错。
3. 在本地 PowerShell、远程 SSH Linux 及 `tmux` 分屏会话内启动 Claude Code：
   - 输入 `/`、`/model`、`/help`、`/resume` 时，Shnex 不弹出补全浮层、不显示灰字，也不拦截 `↑`/`↓`/`Tab`/`Enter`/`Esc`，Claude 原生命令列表正常展示与选择。
   - 输入 `cd /`、`./src/` 时，Shnex 照常给出路径补全。
   - 退出 Claude Code 回到 Shell 提示符后，`cd /`、`ls /`、`./`、`git checkout ` 等本地/远端智能补齐立即正常工作。
