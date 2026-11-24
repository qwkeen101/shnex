# Shnex 内置代理节点池与主备容灾设计方案（方案 A · 纯 Go 用户态拨号）

> **目标**：让 Shnex 在桌面端与 Android 端拥有应用内代理节点池、订阅筛选管理、按主机配置主 ① / 备 ② 节点与低延迟择优、以及真实测速能力；用户无需开系统代理 / Android `VpnService`，不和系统梯子抢通道。

---

## 1. 底层矛盾与核心原则（第一性原理 + 少即是多）

| 底层矛盾 | 传统做法的隐性代价 | Shnex 本质解法 |
| --- | --- | --- |
| **SSH 工具本身只关心目标连接，不该接管整机流量** | 在 Android 申请 `VpnService` 接管全局或写本地 Clash TUN 守护，会直接顶掉用户系统里已有的 VPN，引入额外权限与跨进程生命周期灾难 | **应用内纯 Go 用户态拨号（方案 A）**：只把节点池封装为标准 `dialer.Dialer`，专供 SSH / SFTP / 跳板机连接使用，零 `VpnService`、零全局代理冲突 |
| **单条代理在弱网/敏感时段经常抽风或握手卡死** | 断线后弹窗让用户回设置换节点，或者直接卡死超过 30 秒才超时 | **主机级主 ① / 备 ② 双槽位 + 6s 故障转移 + 30ms 防抖择优**：每台主机最多选 2 个节点（默认兼容 `直连 / 系统代理 / Tailscale`），主节点 6 秒内连不上立即自动切备节点重连；开启「自动走延迟低的」后，备节点快于主节点至少 `30ms`（或主不可用）时优先走备节点 |
| **机场订阅动辄两三百条节点，但常用只有两三个** | 全量塞进节点池导致主页面与主机选择器被大量无效国家/倍率节点淹没，难以挑选 | **订阅导入默认预览筛选**：导入或粘贴订阅时先预览全部解析出的节点并支持批量/正则/关键字勾选，只保存用户勾选的常用节点，记录 `3 个节点 · 已去掉 285` 等统计 |
| **订阅刷新后节点改名或下线，容易把正在用该节点的主机弄断** | 刷新订阅直接物理删掉旧节点，导致主机主/备引用变成悬空 ID，连接突然回退直连或报错 | **下线孤儿保护（Tombstone）**：刷新订阅时，若某节点已从订阅源消失但仍被某台主机用作主/备，**不删除该节点**，保留原配置并标记 `gone = true`（UI 展示红色标签「订阅里已没有」）与 `used_by` 关联主机徽章（如 `阿新 主`、`阿雅 备`） |

---

## 2. 协议引擎选型（方案 A · 零 GPL · 纯 Go 跨平台）

为满足 Android `gomobile bind -target=android/arm64 -androidapi 26 ./mobilebridge`、Windows 与 Linux 单核心复用，协议栈必须**纯 Go、无 cgo、无 GPL 传染**：

1. **主流翻墙协议内核：`github.com/xtls/xray-core`（MPL-2.0）**
   - 仅作为 Go Library 引用其出站拨号能力（不启动本地 SOCKS/HTTP 监听端口，直接调用内部 `Dial` / Outbound Handler 返回 `net.Conn`）。
   - 覆盖协议：`VLESS`（含 `XTLS Vision` / `Reality` / `TLS` / `WS` / `gRPC` / `TCP`）、`Trojan`、`Shadowsocks`（AEAD / 2022）、`VMess`。
2. **QUIC 高丢包穿透补充：`github.com/apernet/hysteria/core/v2`（MIT）**
   - 覆盖协议：`Hysteria2`（`hysteria2://`、`hy2://`），原生适应跨境高丢包移动端场景。
3. **复用 Shnex 现有拨号链**
   - `core/dialer/proxy.go`：继续复用 `SOCKS5Dialer`、`HTTPProxyDialer` 与 `NewProxyDialerFromURL`。
   - `core/dialer/tsnet.go`：内置 Tailscale 继续保留为主节点可选路线之一（备节点按语义仅允许选代理节点，不可把「不使用 / Tailscale」当成普通代理备份）。
4. **实例缓存与密钥隔离**
   - **实例缓存池**：按 `node.ID + node.ConfigHash` 缓存已构造好的用户态出站客户端实例，避免每次建连或测速重复初始化 TLS/XTLS/QUIC 上下文；节点更新、下线或删除时立即 `Close()` 并淘汰旧实例。
   - **敏感字段零明文落盘**：所有节点凭据（UUID、Password、PrivateKey、PublicKey/ShortID 之外的认证秘钥、Hysteria2 鉴权口令）以及订阅源 URL（常含机场 token），**全部存入 `PlatformBridge` 安全密钥库**，明文配置文件（`proxy_nodes.json` / `hosts.json`）中只保存公开展示元数据（节点名、协议类型、服务器地址脱敏摘要、延迟记录、`secret_ref`）。

---

## 3. 数据模型与持久化

### 3.1 新建核心包 `core/proxy`

在 `core/proxy/types.go` 定义节点、订阅与测速核心结构：

```go
type Protocol string

const (
	ProtoVLESS      Protocol = "vless"
	ProtoTrojan     Protocol = "trojan"
	ProtoHysteria2  Protocol = "hysteria2"
	ProtoSS         Protocol = "ss"
	ProtoVMess      Protocol = "vmess"
	ProtoSOCKS5     Protocol = "socks5"
	ProtoHTTP       Protocol = "http"
)

type Node struct {
	ID             string            `json:"id"`
	SubscriptionID string            `json:"subscription_id,omitempty"` // 空表示手动添加
	Name           string            `json:"name"`
	Flag           string            `json:"flag,omitempty"`
	Protocol       Protocol          `json:"protocol"`
	Server         string            `json:"server"`
	Port           int               `json:"port"`
	Summary        string            `json:"summary"`                  // 脱敏展示，如 "VLESS · [2406:..."
	SecretRef      string            `json:"secret_ref"`               // PlatformBridge 中的完整连接配置 JSON 引用
	ConfigHash     string            `json:"config_hash"`              // 变更检测与 Dialer 缓存失效用
	SourceRawHash  string            `json:"source_raw_hash,omitempty"`// 订阅刷新比对标识
	Gone           bool              `json:"gone,omitempty"`           // true = "订阅里已没有"
	LastLatencyMs  int               `json:"last_latency_ms"`          // -1 = 不通 / 超时, 0 = 未测速, >0 = ms
	LastCheckedAt  int64             `json:"last_checked_at,omitempty"`
	CreatedAt      int64             `json:"created_at"`
	UpdatedAt      int64             `json:"updated_at"`
}

type Subscription struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	URLSecretRef  string `json:"url_secret_ref"`   // 订阅地址进密钥库，不落明文 JSON
	ExpireAt      int64  `json:"expire_at,omitempty"`
	TrafficUsed   int64  `json:"traffic_used,omitempty"`
	TrafficTotal  int64  `json:"traffic_total,omitempty"`
	TotalRawCount int    `json:"total_raw_count"`  // 订阅原始节点总数，如 288
	KeptCount     int    `json:"kept_count"`       // 筛选保留数，如 3
	DroppedCount  int    `json:"dropped_count"`    // 已去掉数，如 285
	FilterHint    string `json:"filter_hint,omitempty"` // 记住用户的保留筛选规则，刷新时可复用/预览
	LastSyncAt    int64  `json:"last_sync_at,omitempty"`
	LastSyncError string `json:"last_sync_error,omitempty"`
}
```

### 3.2 扩展 `core/hosts/store.go` 的 `RouteProfile`

向后兼容既有 `UseTSNet` 和 `ProxyURL`，新增主机级主备双槽位字段：

```go
type RouteProfile struct {
	UseTSNet          bool   `json:"use_tsnet,omitempty"`
	ProxyURL          string `json:"proxy_url,omitempty"`
	PrimaryNodeID     string `json:"primary_node_id,omitempty"`     // 空则按 UseTSNet / ProxyURL / 直连兼容
	BackupNodeID      string `json:"backup_node_id,omitempty"`      // 仅允许引用代理节点池 ID
	AutoLowestLatency bool   `json:"auto_lowest_latency,omitempty"` // 备用明显更快时新连/重连先走备用
}
```

> **兼容性**：老配置没有 `primary_node_id` 时行为与现网完全一致；若只配了手工 URL，也可继续工作或一键导入进节点池。

---

## 4. 订阅解析、筛选与孤儿节点保护

新建 `core/proxy/parser.go` 与 `core/proxy/subscription.go`：

1. **单链接 / 多行链接解析**
   - 支持：`vless://`、`trojan://`、`hysteria2://`、`hy2://`、`ss://`（含 SIP002 与传统 Base64）、`vmess://`（V2RayN Base64 JSON）、`socks5://`、`socks://`、`http://`、`https://`。
   - 对参数不完整、未知传输或高危错误给出逐条解析错误，但不影响同批合法节点入库。
2. **订阅拉取与格式识别**
   - 读取标准 HTTP Header（如 `Subscription-Userinfo` 解析到期时间 `expire` 与已用/总流量）。
   - 自动识别响应体为：
     1. Base64 编码的多行 URI 列表；
     2. 明文多行 URI 列表；
     3. Clash / Mihomo YAML（解析 `proxies:` 节点段，按类型映射为统一内部节点描述）。
3. **导入预览筛选机制**
   - `POST /api/v1/subscriptions/preview` 拉取订阅但不入库，返回完整节点预览列表与 `total_raw_count`。
   - 用户勾选保留的节点（如仅保留 `3` 条、去掉 `285` 条）随 `POST /api/v1/subscriptions` 提交；后端同时保存 `selected_source_hashes` / `FilterHint` 与统计数 `kept_count` / `dropped_count`。
4. **订阅刷新与孤儿节点保护（关键状态机）**
   - 拉取订阅新节点列表，按用户已选集合匹配：
     - **仍在订阅中且原节点仍存在**：更新参数、服务器地址、名称并置 `gone = false`。
     - **已从订阅消失，但被任意主机的 `primary_node_id` 或 `backup_node_id` 引用**：**保留节点不删**，将 `gone = true`（UI 标红 `订阅里已没有`），保证现有主机不会在刷新后突然断链或丢引用。
     - **已从订阅消失，且没有任何主机引用**：自动清理并释放关联密钥与拨号缓存。
   - **手动删除保护**：`DELETE /api/v1/network/nodes/{id}` 若发现节点仍被主机引用，默认返回引用列表提示或仅在用户确认/解绑后删除。

---

## 5. 主备容灾、择优拨号与真实测速

### 5.1 择优与故障转移状态机（`core/dialer/failover.go`）

实现包装器 `FailoverDialer`，对外仍严格满足 `dialer.Dialer` 接口（`Dial(ctx context.Context, addr string) (net.Conn, error)`）：

1. **选路决策（每次新建 SSH 会话 / 断线重连触发，不打断已建立的稳定连接）**：
   - 若只配主节点：直接拨主节点（受限于单次主超时）。
   - 若同时配了主 ① 与备 ②：
     - 默认顺序：`[primary, backup]`。
     - 若开启 `AutoLowestLatency`：
       - 若主节点已知状态为不通（`LastLatencyMs < 0`）且备节点可用，或主/备均有有效测速值且 `backup.LastLatencyMs + 30 <= primary.LastLatencyMs`（**防抖阈值 30ms**，避免两条线路延迟接近时来回抖动），调整当次尝试顺序为 `[backup, primary]`。
2. **6 秒主切备容灾**：
   - 第一候选拨号施加独立超时上下文 `min(6*time.Second, remainingCtx)`。
   - 发生任意「连不上代理 / TLS 或 Reality 握手失败 / 6 秒未响应」，立即关闭半开连接并切换至第二候选重连。
   - 建连结果同步异步回写节点最近一次状态（`LastLatencyMs` / `LastCheckedAt`），供 UI 实时显示「可用 xx ms」或红色「不通」。
3. **接入 `core/api/server.go`**
   - 仅扩展 `buildSessionRoute(ctx context.Context, req CreateSessionRequest)`：
     - 先判断 `AuthTailscaleSSH`（保持原有逻辑）。
     - 若 `req.Route.PrimaryNodeID != ""` 或 `req.Route.BackupNodeID != ""`，从节点池与 `PlatformBridge` 构造对应的 `FailoverDialer`；若老字段 `UseTSNet` / `ProxyURL` 仍存在，保持原路径兼容。
     - 若配置了 `JumpHosts`，继续无缝串联在最外层代理拨号器之上。

### 5.2 真实可用性测速（`core/proxy/ping.go`）

拒绝虚假本地 ICMP Ping，所有测速必须真实走完代理拨号握手：

- **测速方式**：调用该节点对应的用户态拨号器向可配置探测目标（默认轻量 TLS/TCP 连通性探针或指定 SSH 端口连通）完成「代理握手 + 目标建连」，记录首包/建连总耗时 `ms`。
- **并发与超时保护**：全局或分组测速通过信号量限制最大并发为 **8**，单节点测速超时 **5 秒**（超过或握手失败计为 `-1`，UI 显示红色 `不通`）。
- **测速粒度**：支持单个节点测速、按订阅/手动分组测速（「测本组延迟」）、全量测速，以及主机编辑面板内点击右上角测速图标顺带测试「使用此代理连当前主机」。

---

## 6. Go 核心 REST API 设计（`core/api/network_handlers.go`）

在 `core/api/server.go` 的 `buildMux()` 中挂载以下路由：

| Method & Path | 用途 | 核心请求/响应字段 |
| --- | --- | --- |
| `GET /api/v1/network/overview` | 网络总览页全量数据 | `hosts_in_use`（含每台主机主/备节点与测速）、`subscriptions`（含 `total_raw_count`/`kept_count`/`dropped_count`）、`nodes`（含 `gone`、`last_latency_ms`、以及聚合计算出的 `used_by: [{host_id, host_name, slot: "primary"\|"backup"}]`） |
| `POST /api/v1/network/nodes/import` | 粘贴多行分享链接导入手动节点 | `{ "raw_text": "..." }` -> 返回导入节点列表与解析错误摘要 |
| `DELETE /api/v1/network/nodes/{id}` | 删除节点（同时清理密钥与缓存） | 查询参数 `force=true` 可解绑主机引用后强制删除 |
| `POST /api/v1/network/subscriptions/preview` | 订阅预览与关键词/正则过滤 | `{ "name": "...", "url": "..." }` -> 返回 `{ "total_raw_count", "expire_at", "preview_nodes": [...] }` |
| `POST /api/v1/network/subscriptions` | 保存订阅（仅写入勾选节点） | `{ "name", "url", "selected_hashes": [...], "filter_hint" }` |
| `POST /api/v1/network/subscriptions/{id}/refresh` | 刷新订阅并保留被引用下线节点 | 返回新增/更新/标记 `订阅里已没有` 的变更摘要 |
| `DELETE /api/v1/network/subscriptions/{id}` | 删除订阅（仍被引用的节点可选择保留为手动节点或解绑） | `{ "keep_in_use_nodes": true }` |
| `POST /api/v1/network/ping` | 单节点 / 分组 / 全量真实测速 | `{ "scope": "all"\|"subscription"\|"manual"\|"node", "target_id": "...", "probe_addr": "..." }` |
| `PATCH /api/v1/hosts/{id}/route` | 保存主机的网络与主备路线 | `{ "primary_node_id", "backup_node_id", "auto_lowest_latency", "use_tsnet", "proxy_url" }` |

---

## 7. 三层交互体验与分阶段实施验收

### 7.1 三层交互结构（对齐 Android 原生卡片与桌面端设置）

1. **第一层：主机详情面板内的「网络」摘要卡**
   - 直接显示当前主机的 `①` 主节点与 `②` 备节点及状态（延迟 ms / 红色 `不通`）。
   - 右侧保留快速对调按钮（`⇄` 一键互换主备）、下方直接放「自动走延迟低的」开关与订阅到期提醒，点击顶部 `网络 >` 进入第二层。
2. **第二层：主机「网络 <主机名>」双槽位底部抽屉**
   - 顶部并排两个选中槽位卡：`① 主节点`（可设 `不使用` / 内置 Tailscale / 任意节点）与 `② 备节点`（只可选代理节点，带清除按钮 `×` 和中置对调按钮 `⇄`），底部附明晰规则提示（`主节点连不上（连不上代理、握手失败、6 秒没回应）时，马上改经备用节点再连一次。备用只能选代理节点。`）。
   - 下方单选列表：已从订阅消失但被保留的节点在名称下方紧跟红底白字 `订阅里已没有` 徽章；每行右侧均提供单点测速仪表图标；底部固定 `取消` / `保存` 操作条。
3. **第三层：全局「网络」中心**
   - 顶栏展示总节点数与订阅数（如 `7 个节点 · 1 条订阅`）及全量测速按钮。
   - **服务器在用**置顶区：快速查看哪些主机已配了 `①`/`②` 并直达编辑。
   - **代理节点**分组区：按「订阅卡片（如 `宝可梦 · 3 个节点 · 已去掉 285...`，带刷新与 `...` 菜单）」和「手动添加」分组；每组自带「测本组延迟」条；每条节点左侧展示绑定徽章（如白色 `阿新 主`、灰框 `阿新 备`、白色 `阿雅 主`）以及红色 `订阅里已没有` 状态。

### 7.2 分阶段实施顺序与验收标准

1. **Phase 1：Go 核心引擎与单元测试（`core/`）**
   - 引入 `github.com/xtls/xray-core` 与 `github.com/apernet/hysteria/core/v2` 到 `core/go.mod`，实现 `core/proxy`（解析、筛选、订阅刷新孤儿保护、真实测速、密钥存储）与 `core/dialer/failover.go`（6s 主切备 + 30ms 防抖择优），并接入 `core/api/server.go` 的 `buildSessionRoute` 与 `network_handlers.go`。
   - **核心验证**：
     - `cd core && go test ./...` 全部通过（覆盖订阅筛选、刷新标 `gone=true`、6s 主切备回退、30ms 防抖择优、密钥不落明文配置）。
     - 跨平台编译检查：`GOOS=windows GOARCH=amd64 go build ./...`、`GOOS=linux GOARCH=amd64 go build ./...`。
     - **Android gomobile 兼容验证**：后续修改 `core/` 后，Android 打包（参考 `mobile/scripts/package-android.ps1`）**必须去掉 `-SkipCoreBind`**，重新执行 `gomobile bind -target=android/arm64 -androidapi 26 ./mobilebridge` 生成 `shnexcore.aar`，并检查 APK 体积增量与 Release 构建是否通过。
2. **Phase 2：Android 端 UI（`mobile/`）**
   - 对齐上述三层交互实现主机卡片网络区、主备双槽位弹窗、全局网络中心、订阅预览勾选弹窗与导入链接弹窗，端到端接入 `/api/v1/network/*` 与 `/api/v1/hosts/{id}/route`。
3. **Phase 3：桌面端（`desktop/`）**
   - 复用同一套 `/api/v1/network/*` REST 接口，在桌面端设置/主机编辑弹窗提供等价的节点池管理、订阅筛选与主 ① / 备 ② 配置界面。
