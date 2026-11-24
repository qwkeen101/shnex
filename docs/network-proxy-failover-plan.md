# Shnex 代理主备与节点池方案（精简版）

> 本版按代码评审重写，替代原"节点中心 + 测速择优"方案。范围：Go 核心 `core/`、桌面 `desktop/frontend/`、Android `mobile/android/`。项目开源，可以使用 GPLv3 依赖（xray-core 经 `sagernet/sing` 引入 GPLv3）。

## 1. 底层矛盾

一条路不通时，**新连接**要自动换另一条。只有这一件事。

- 已经建立的 SSH 连接无法在代理之间迁移，断线后只能靠重连恢复。tmux 主机靠 `must_exist` 能无损接回；普通 shell 会话会丢。
- 测速数字会过期（Wi-Fi 下测的"不通"到了 5G 不再成立），所以**不测速、不择优、不加开关**，让真实建连结果说话。

## 2. 与原方案相比砍掉的

| 原方案 | 处理 | 原因 |
| --- | --- | --- |
| 新建 `core/proxy` 节点池 | 改为扩展现有 `settings.Proxies` | 已有代理池、接口与桌面页面，再建一个是同义概念 |
| 6 秒顺序切换 + 测延迟 + 30ms 择优开关 | 改为错开竞速 | xray 的 `Dial` 立即返回，节点死活在 Dial 层看不到；延迟数字会过期 |
| `primary_node_id` / `backup_node_id` / `auto_lowest_latency` | 改为一个字段 `route.proxy_ids` | 顺序即主备 |
| `PATCH /hosts/{id}/route` | 不加 | 保存主机时两端完整回传 `route` |
| 网络中心、`used_by` 徽章、⇄ 对调、测速按钮、测本组延迟 | 不做 | 界面只在"走了备用"时提示一次 |
| 订阅预览 / 正则筛选 / 流量到期 / Tombstone | 推迟到第 4 步 | 导入就是粘贴，复制哪几条就是筛选 |
| 实例缓存池 | 不做 | 节点更新时关闭实例会切断正在用的会话 |

## 3. 设计

### 3.1 一个代理池
- 沿用 `settings.ProxyEntry`（`core/settings/store.go`），密码沿用 `shnex:proxy:<id>` 存入密钥库。
- **导入就是粘贴**：`POST /api/v1/settings/proxies/import {"text"}`，每行一个链接，如 `socks5://user:pass@host:1080#香港`、`http://host:7890`。`#` 后为名称，缺省用 `host:port`。逐行报错，不影响其他行。第 3 步起同一入口接受 `vless://` 等分享链接。
- 编辑不带密码时保留原密码；删除时同时清掉密钥。
- **被主机引用的代理拒绝删除**（409，列出主机名），不存在悬空引用。

### 3.2 主机只加一个字段
- `route.proxy_ids []string`：最多 2 项，顺序即 ①主 ②备。取值是代理池 ID，或 `direct`（直连）。所以"直连优先、代理兜底"也能表达。
- 优先级：`use_tsnet`（Tailscale 地址经公网代理不可达，此时忽略 `proxy_ids`）> `proxy_ids` > 旧 `proxy_url`（原样兼容）。
- 保存主机时两端完整回传 `route`（Android 以前只发 `use_tsnet`，会清掉其他字段）。

### 3.3 错开竞速（`core/dialer/race.go`）
1. 先拨 ①。**约 3 秒内没读到 SSH 服务端标识 `SSH-`**，或 ① 先失败，就立刻并行起 ②。谁先读到标识用谁，另一条关掉。
2. "连上"以**读到 `SSH-` 标识**为准，不以 `Dial` 返回为准；预读的字节原样回放给 SSH 握手。非 SSH 协议（telnet）不等标识。
3. **记住上次的赢家**（进程内存，5 分钟过期，不落盘）：同一组路线下次先拨上次赢的那条。主代理死后第一次重连多等约 3 秒，之后的重连直接走备用。
4. 单条超时 15 秒，总耗时不超过约 18 秒，低于 Android 客户端 35 秒的读超时。

### 3.4 界面
- **主机编辑**：网络区把"代理 URL"输入框换成两个下拉框 `代理 ①` `代理 ②`（选项：自动（系统代理）/ 直连 / 代理池各项）。① 为"自动"时 ② 不可选。旧主机的 `proxy_url` 原样保留显示。
- **代理池页**：桌面"代理与 Tailnet 组网"页的四格表单换成一个"粘贴链接"文本框；Android 设置页新增同样的代理列表与粘贴导入。
- **提示**：会话摘要新增 `route_via`（实际走的路线名）和 `route_fallback`（没走 ①）。走了备用时弹一次提示"主代理不通，已经 ② <名称> 连接"。不做常驻状态与测速。

## 4. 安全底线
- 链接与密码不进日志、错误信息与备份；备份导入后被引用但不存在的代理 ID 视为无效，连接时报"代理已删除"。
- 第 3 步接入加密协议时，默认拒绝 `allowInsecure` / `skip-cert-verify` 节点。

## 5. 实施顺序
1. **第 1 步（零新依赖）**：代理池粘贴导入 + `proxy_ids` + 错开竞速，仅 SOCKS5 / HTTP。核心、桌面、Android 一起交付。
2. **第 2 步（本地验证）**：在本地执行 `go get github.com/xtls/xray-core`、`go mod tidy`、`go build ./...`，确认它要求的 gvisor 版本能与 tailscale 锁定的版本共存，并打出 Android aar、看 APK 体积增量。开发机不装依赖，这一步只能在本地做。
3. **第 3 步**：接入 xray（VLESS-Reality、Trojan、Shadowsocks、VMess），作为竞速的一条候选；需要的话再接 Hysteria2（`apernet/hysteria`）。发布前在仓库根目录补 GPLv3 LICENSE。
4. **第 4 步（可选）**：订阅 URL。刷新只按名称更新参数，绝不自动删除节点。

## 6. 验证
- `cd core && go test ./...`：链接解析、`proxy_ids` 规整、竞速（①慢 ②快、①立即失败、全部失败、标识前有杂行、赢家记忆）。
- `cd desktop/frontend && npx tsc --noEmit`。
- Android 打包必须去掉 `-SkipCoreBind`。
- 实机：① 填一个不存在的代理端口、② 填可用代理，约 3 秒内连上并提示走了备用；对调后直接走 ①；删除被引用的代理被拒绝。
