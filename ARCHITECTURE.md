# Shnex 架构设计与工程约束 (ARCHITECTURE.md)

> **产品定位**：跨平台开源 SSH 客户端（专为远程运行 Claude CLI / AI 编程工作流深度优化），打造「桌面 + 手机接力工作台」。演进路线为先落地 Windows 桌面端，随后接入 Android 移动端。

---

## 1. 单核多壳架构（路线 B）

仓库采用 Monorepo 组织，严格隔离纯 Go 核心网络/终端引擎与各端原生 UI 壳层：

```text
shnex/
├── go.work                # Go Workspace 多模块协同配置
├── ARCHITECTURE.md        # 架构设计与工程红线约束（本文档）
├── core/                  # 纯 Go 核心引擎（零 UI / 零平台 GUI 依赖）
│   ├── platform/          # PlatformBridge 钥匙串、通知与网络切换生命周期抽象
│   ├── dialer/            # 可组合的统一拨号层（直连 / SOCKS5 / HTTP 代理 / 多级跳板机 / tsnet）
│   ├── tailnet/           # 用户态 Tailscale (tsnet) 与 Headscale 节点管理、Tailscale SSH 预留
│   ├── relay/             # 远端会话持久化接力层（tmux / zellij 自动引导 + shnex-agent 二进制流协议）
│   ├── transport/         # 统一交互式终端会话接口、心跳保活、BEL / 静默及 Claude 待审批尾行识别引擎
│   ├── ssh/               # 基于 golang.org/x/crypto/ssh 的 PTY / Exec 接力会话传输实现
│   ├── api/               # 127.0.0.1 本地 HTTP 控制面 + WebSocket 二进制数据面服务
│   ├── cmd/devserver/     # 独立本地联调服务器与浏览器单页测试终端
│   └── cmd/shnex-agent/   # 超轻量单文件远端持久化守护进程（支持 Linux/macOS 静态交叉编译）
├── desktop/               # 桌面端壳层（Milestone 2+：Wails v3 + xterm.js WebGL，当前留空）
├── mobile/android/        # 移动端壳层（Milestone 3+：Kotlin + Compose + termlib，当前留空）
└── docs/                  # 协议规范与设计文档
```

### 强边界约束
1. **`core/` 纯净度红线**：`core/` 必须保持纯 Go 实现，**严禁引入** Wails、Android SDK/NDK、CGO GUI 库或任何前端/UI 依赖，确保可同时被桌面进程内嵌、独立守护进程编译或通过 `gomobile` / 本地子进程供 Android 调用。
2. **壳层职责最小化**：
   - `desktop/`（Wails v3 + xterm.js WebGL）与 `mobile/android/`（Kotlin + Compose + termlib）仅负责窗口承载、终端字符网格渲染、系统钥匙串/通知桥接与生命周期上报。
   - 所有拨号链路、Tailnet 组网、SSH 握手、心跳探测、断线感知与 AI CLI 完工检测统一收敛在 `core/`。

---

## 2. 开源许可证红线

- **允许引入的依赖协议**：`MIT`、`Apache-2.0`、`BSD-2-Clause`、`BSD-3-Clause`、`ISC` 等宽松开源协议。
- **严禁引入的依赖协议**：**严禁引入任何 `GPL`、`LGPL`、`AGPL` 传染性协议依赖**（包括直接依赖与编译期传递依赖），保障未来桌面端分发与移动端应用商店合规上架。
- **核心依赖清单（Milestone 1）**：
  - `golang.org/x/crypto`：`BSD-3-Clause`
  - `golang.org/x/net`：`BSD-3-Clause`
  - `tailscale.com` (`tsnet`)：`BSD-3-Clause`
  - `github.com/coder/websocket`：`ISC`（与 MIT 等价宽松协议）
  - `github.com/creack/pty`：`MIT`（用于远端 `shnex-agent` 守护进程本地 PTY 托管）

---

## 3. 进程内本地服务桥接协议

`core` 在宿主进程内启动本地桥接服务，桌面壳与移动壳通过统一的回环协议与 `core` 通信：

### 3.1 监听与鉴权安全边界
- **强制本地回环绑定**：仅允许监听 `127.0.0.1:0`（操作系统随机分配空闲端口），**严禁监听 `0.0.0.0`、`::` 或任何非回环网卡地址**，从根源避免局域网暴露与触发 Windows 防火墙弹窗。`core/api` 在代码层强制校验 `ip.IsLoopback()`。
- **动态随机鉴权令牌**：每次实例启动通过 `crypto/rand` 生成 32 字节（256-bit，编码为 64 位十六进制字符串）随机 `authToken`，使用恒定时间比较（`subtle.ConstantTimeCompare`）校验。
- **跨站劫持防护（CSWSH）**：WebSocket 握手强制校验 `Origin`（仅允许无 Origin 的原生客户端或同端口 `http://127.0.0.1:<port>`）并验证 `authToken`。

### 3.2 控制面（HTTP REST + Bearer Token）
所有 `/api/v1/*` 接口要求请求头携带 `Authorization: Bearer <authToken>`：
- `GET /api/v1/health`：核心状态、运行时间、活跃会话数与网络世代号。
- `POST /api/v1/network/changed`：宿主触发网络环境切换/休眠唤醒通知。
- `GET /api/v1/sessions` / `POST /api/v1/sessions` / `DELETE /api/v1/sessions/{id}`：终端会话生命周期管理。
- `POST /api/v1/sessions/{id}/resize`：HTTP 控制面窗口尺寸同步兜底。
- `GET /api/v1/tailnet/status` / `POST /api/v1/tailnet/up` / `POST /api/v1/tailnet/down` / `GET /api/v1/tailnet/nodes`：Tailscale / Headscale 组网与在线节点发现。

### 3.3 数据面（WebSocket 二进制帧协议）
终端数据通道连接 `ws://127.0.0.1:<port>/api/v1/sessions/{id}/ws`，全程采用首字节 Opcode 的紧凑二进制帧，避免 JSON/Base64 编解码损耗及多字节 UTF-8（中文、Claude TUI 边框字符）截断乱码：

| Opcode | 名称 | 方向 | 负载格式 | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| `0x00` | `FrameData` | 双向 | `[0x00][Raw PTY Bytes...]` | 终端原始字节流（输入键击 / 输出渲染） |
| `0x01` | `FrameResize` | 客户端 -> 服务端 | `[0x01][cols:uint16 BE][rows:uint16 BE]`（可选附带像素宽高共 9 字节） | 动态调整远端 PTY 窗口行列数 |
| `0x02` | `FramePing` | 客户端 -> 服务端 | `[0x02][clientTs:uint64 BE]` | 触发会话保活探测并测量端到端/SSH RTT |
| `0x03` | `FramePong` | 服务端 -> 客户端 | `[0x03][clientTs:uint64 BE][sshRttMicros:uint64 BE]` | 返回心跳响应及底层 SSH 链路实测时延 |
| `0x04` | `FrameEvent` | 服务端 -> 客户端 | `[0x04][UTF-8 JSON Event]` | 推送 `bell`、`output_silence`、`disconnected` 等会话事件 |

---

## 4. 安全、平台解耦与 AI 工作流扩展点

1. **`PlatformBridge` 接口解耦**（`core/platform`）：
   - `GetSecret(ctx, key)` / `SetSecret(ctx, key, value)`：对接 Windows Credential Manager、Android Keystore，核心层不落盘明文凭据。
   - `Notify(ctx, notification)`：对接各端系统级通知，区分 `bell`、`action_required`（Claude 待人工审批）与 `task_idle`（普通长任务完工静默）。
2. **Claude CLI 完工与权限确认精准识别**（`core/transport`）：
   - **ANSI 过滤独立 `BEL`** + **流式突发后静默检测**（`output_silence`）。
   - **终端尾行可见文本解析器**：实时剥离 CSI/OSC 转义序列并处理回车重绘（`\r`）与退格，匹配 Claude Code / AI CLI 权限审批特征（如 `Do you want to proceed?`、`Allow once` / `Allow always`、`Yes, allow` / `Deny`、`[y/N]`），以更短防抖延迟立即发出 `ai_approval_required` 高优事件并提取上下文摘要，与普通完工等待输入精准分流。
3. **双引擎「桌面 + 手机接力」远端持久化**（`core/relay` & `core/cmd/shnex-agent`）：
   - **零侵入引导模式（`tmux` / `zellij` / `auto`）**：无需远端安装私有组件，自动创建或接管同名持久化工作区（默认配置 `tmux window-size latest` 以适配桌面宽屏与手机窄屏切换）；`auto` 模式按 `shnex-agent -> tmux -> zellij -> login shell` 顺序自动探测降级。
   - **单文件守护模式（`shnex-agent`）**：可静态交叉编译部署至远端服务器，基于用户私有权限（`0700`）Unix Socket 在后台持久化托管 PTY 进程池、128 KiB 内存回滚环缓与多端实时镜像，SSH 闪断或切换设备零中断。
4. **网络切换主动感知**（`NotifyNetworkChanged()`）：
   - 供宿主在笔记本开盖唤醒、Wi-Fi/热点切换、移动端 5G/Wi-Fi 漫游时调用。
   - 触发所有活跃会话立即发送紧急保活探测，秒级识别半开死连接并驱动重连状态机，无需等待系统 TCP 超时。
5. **可插拔传输层与拨号器**：
   - `core/dialer.Dialer` 支持任意组合：`Direct`、`SOCKS5`、`HTTP CONNECT`、多级 `SSH Jump Host` 链式跳板以及用户态 `tsnet` 虚拟网卡拨号。
   - `core/transport.Transport` 抽象交互式终端会话，当前默认提供标准 `SSH PTY` 与 `SSH + shnex-agent` 增强管道实现。
6. **版本控制安全卫生**：
   - `.gitignore` 默认拦截所有私钥（`*.pem`、`*.key`、`id_ed25519*` 等）、`.env*` 敏感配置与 `tsnet` 状态目录，严禁任何身份凭据进入代码仓库。
