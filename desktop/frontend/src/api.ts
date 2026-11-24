export interface BridgeBootstrap {
  api_base: string;
  ws_base: string;
  token: string;
  platform: string;
}

export interface HostKeyPolicy {
  insecure_ignore?: boolean;
  pinned_sha256?: string;
  known_hosts_path?: string;
}

export interface RelayConfig {
  mode?: "none" | "auto" | "tmux" | "zellij" | "shnex_agent";
  session_name?: string;
  agent_path?: string;
  command?: string;
  /** tmux only: attach only if the session still exists (410 TMUX_SESSION_GONE otherwise). */
  must_exist?: boolean;
}

export interface HostProfile {
  id: string;
  name: string;
  protocol?: string;
  group?: string;
  host: string;
  port: number;
  user: string;
  icon?: string;
  badge_text?: string;
  color?: string;
  auth_mode: "auto" | "password" | "private_key" | "tailscale_ssh" | "secret_ref";
  credential_id?: string;
  secret_ref?: string;
  has_secret: boolean;
  default_dir?: string;
  file_protocol?: string;
  sftp_sudo?: boolean;
  tmux_session?: string;
  post_login_script?: string;
  metrics_bar?: "follow" | "on" | "off";
  terminal_encoding?: string;
  term_type?: string;
  auto_session_log?: boolean;
  favorite?: boolean;
  last_connected_at?: string;
  host_key: HostKeyPolicy;
  route: {
    use_tsnet?: boolean;
    proxy_url?: string;
  };
  relay: RelayConfig;
  updated_at: string;
}

export interface UpsertHostInput {
  id?: string;
  name: string;
  protocol?: string;
  group?: string;
  host: string;
  port: number;
  user: string;
  icon?: string;
  badge_text?: string;
  color?: string;
  auth_mode: "auto" | "password" | "private_key" | "tailscale_ssh";
  credential_id?: string;
  password?: string;
  private_key_pem?: string;
  default_dir?: string;
  file_protocol?: string;
  sftp_sudo?: boolean;
  tmux_session?: string;
  post_login_script?: string;
  metrics_bar?: "follow" | "on" | "off";
  terminal_encoding?: string;
  term_type?: string;
  auto_session_log?: boolean;
  favorite?: boolean;
  last_connected_at?: string;
  host_key: HostKeyPolicy;
  route: {
    use_tsnet?: boolean;
    proxy_url?: string;
  };
  relay: RelayConfig;
}

export interface SessionLogMeta {
  name: string;
  host_label: string;
  size_bytes: number;
  updated_at: string;
  active: boolean;
}

export interface SessionSummary {
  id: string;
  protocol: string;
  target: string;
  user: string;
  relay_mode?: string;
  workspace_name?: string;
  size: { cols: number; rows: number };
  created_at: string;
  attached_clients: number;
  last_rtt_micros?: number;
  closed: boolean;
}

export interface NetInterfaceStat {
  name: string;
  rx_bytes_per_sec: number;
  tx_bytes_per_sec: number;
  total_rx: number;
  total_tx: number;
}

export interface DiskStat {
  mount_point: string;
  used_bytes: number;
  total_bytes: number;
  used_percent: number;
}

export interface SystemStats {
  hostname: string;
  os: string;
  cpu_cores: number;
  cpu_percent: number;
  load_1: number;
  load_5: number;
  load_15: number;
  load_percent: number;
  mem_used_bytes: number;
  mem_total_bytes: number;
  mem_percent: number;
  swap_used_bytes: number;
  swap_total_bytes: number;
  swap_percent: number;
  total_rx_bytes_per_sec: number;
  total_tx_bytes_per_sec: number;
  interfaces: NetInterfaceStat[];
  disks: DiskStat[];
  rtt_ms: number;
  uptime_sec?: number;
  connected_sec: number;
  collected_at: string;
}

export interface FileEntry {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  mode: string;
  mod_time: string;
}

export interface Snippet {
  id?: string;
  title: string;
  command: string;
}

export interface TailnetStatus {
  running: boolean;
  backend_state: string;
  control_url: string;
  auth_url?: string;
  magic_dns_suffix?: string;
}

export interface TailnetNode {
  id: string;
  hostname: string;
  magic_dns_name: string;
  short_dns_name: string;
  tailscale_ips: string[];
  os: string;
  online: boolean;
  supports_tailscale_ssh: boolean;
}

export interface AIConfig {
  base_url: string;
  model: string;
  has_api_key: boolean;
}

export interface AIExecutedStep {
  call_id: string;
  command: string;
  status: string;
  approval_note?: string;
  review_reason?: string;
  confidence?: number;
  stdout?: string;
  stderr?: string;
  exit_code: number;
  duration_ms: number;
}

export interface AIMessage {
  role: string;
  content?: string;
  tool_calls?: unknown[];
  tool_call_id?: string;
}

export interface AIChatResponse {
  reply: string;
  steps?: AIExecutedStep[];
  pending_approval?: AIExecutedStep;
  history: AIMessage[];
}

export interface AppNotification {
  title: string;
  body: string;
  category: string;
  session_id?: string;
  created_at: string;
}

export interface AIProvider {
  id: string;
  name: string;
  protocol: string;
  base_url: string;
  model: string;
  is_default: boolean;
  has_api_key: boolean;
  secret_ref: string;
}

export interface KnownHostEntry {
  line_number: number;
  hosts: string;
  key_type: string;
  fingerprint: string;
}

export interface PortForwardRule {
  id: string;
  local_addr: string;
  remote_addr: string;
  created_at: string;
}

export interface RightRailVisibility {
  monitor: boolean;
  processes: boolean;
  host_info: boolean;
  files: boolean;
  docker: boolean;
  ai: boolean;
  port_forward: boolean;
  snippets: boolean;
  cmd_history: boolean;
  broadcast: boolean;
  session_logs: boolean;
  quick_connect: boolean;
  import_ssh: boolean;
}

export interface AppSettings {
  general: {
    language: string;
    remember_layout: boolean;
    right_bar_mode: "replace" | "side_by_side";
    billing_reminder_enabled: boolean;
    close_action: "tray" | "quit";
    tray_icon_style: "color" | "mono";
    tray_click_action: "popup" | "main_window";
    tray_material: "translucent" | "opaque";
    auto_check_update: boolean;
    update_interval_hours: number;
    update_proxy: string;
    right_rail_items: RightRailVisibility;
    default_privacy_mode: boolean;
    confirm_before_close: boolean;
    startup_behavior: string;
  };
  security: {
    master_lock_enabled: boolean;
    auto_lock_minutes: number;
    vault_locked: boolean;
  };
  appearance: {
    theme: string;
    ui_font_size: number;
    compact_mode: boolean;
    has_wallpaper: boolean;
    wallpaper_mime?: string;
    wallpaper_dim: number;
    glass_blur_px: number;
  };
  terminal: {
    font_family: string;
    font_size: number;
    line_height: number;
    cursor_style: "block" | "bar" | "underline";
    cursor_blink: boolean;
    scrollback: number;
    default_relay_mode: string;
    default_shell: string;
    keepalive_sec: number;
    auto_reconnect: boolean;
    restore_dir_on_reconnect: boolean;
    term_type: string;
    right_click_action: "menu" | "paste" | "copy_or_paste";
    copy_on_select: boolean;
    osc52_mode: "allow" | "ask" | "deny";
    osc52_max_kib: number;
    osc52_notify: boolean;
    completion_enabled: boolean;
    completion_mode: "inline" | "list" | "hybrid";
    builtin_powerline: boolean;
  };
  monitor: {
    enabled: boolean;
    interval_sec: number;
    show_bottom_bar: boolean;
  };
  files: {
    default_path: string;
    show_hidden: boolean;
    max_download_mb: number;
  };
  ai: {
    default_agent: string;
    acp_command: string;
    context_lines: number;
    audit_retention_days: number;
    redact_secrets: boolean;
    confirmed_hosts: string[];
    default_permission: "ask" | "auto_review" | "allow_all";
    review_provider_id: string;
    review_strictness: "loose" | "standard" | "strict";
    providers: AIProvider[];
  };
  mcp_servers: {
    id: string;
    name: string;
    command: string;
    args?: string[];
    enabled: boolean;
  }[];
  credentials: {
    id: string;
    name: string;
    kind: string;
    username: string;
    secret_ref: string;
    created_at: string;
  }[];
  proxies: {
    id: string;
    name: string;
    scheme: string;
    host: string;
    port: number;
    username?: string;
    has_secret: boolean;
  }[];
}

let bridgeState: BridgeBootstrap | null = null;

export async function initBridge(): Promise<BridgeBootstrap> {
  if (bridgeState) return bridgeState;

  // Support URL hash override for standalone browser dev testing (#api=http://127.0.0.1:xxx&token=yyy)
  const hash = new URLSearchParams(window.location.hash.replace(/^#/, ""));
  const hashApi = hash.get("api");
  const hashToken = hash.get("token");
  if (hashApi && hashToken) {
    bridgeState = {
      api_base: hashApi,
      ws_base: hashApi.replace(/^http/, "ws"),
      token: hashToken,
      platform: "browser-dev",
    };
    return bridgeState;
  }

  const res = await fetch("/__shnex_desktop/bootstrap", { cache: "no-store" });
  if (!res.ok) {
    throw new Error(`Bootstrap bridge failed (${res.status})`);
  }
  bridgeState = (await res.json()) as BridgeBootstrap;
  return bridgeState;
}

export class ApiError extends Error {
  code?: string;
  constructor(message: string, code?: string) {
    super(message);
    this.code = code;
  }
}

async function apiRequest<T>(path: string, method = "GET", body?: unknown): Promise<T> {
  const b = await initBridge();
  const headers: Record<string, string> = {
    Authorization: `Bearer ${b.token}`,
  };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
  }
  const res = await fetch(`${b.api_base}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new ApiError(data.error || `HTTP ${res.status}`, data.code);
  }
  return data as T;
}

async function apiBinaryUpload<T>(
  path: string,
  blob: Blob,
  contentType = "application/octet-stream"
): Promise<T> {
  const b = await initBridge();
  const res = await fetch(`${b.api_base}${path}`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${b.token}`,
      "Content-Type": contentType || "application/octet-stream",
    },
    body: blob,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new ApiError(data.error || `HTTP ${res.status}`, data.code);
  }
  return data as T;
}

export interface NativeClipboardData {
  supported: boolean;
  kind: "image" | "image_path" | "text" | "empty";
  text?: string;
  local_path?: string;
  data_base64?: string;
  mime_type?: string;
  file_name?: string;
}

export async function readNativeClipboard(): Promise<NativeClipboardData> {
  try {
    const res = await fetch("/__shnex_desktop/clipboard/read", {
      method: "POST",
      cache: "no-store",
    });
    if (!res.ok) {
      return { supported: false, kind: "empty" };
    }
    const data = (await res.json()) as Omit<NativeClipboardData, "supported">;
    return { supported: true, ...data };
  } catch {
    return { supported: false, kind: "empty" };
  }
}

export async function openNativeFileDialog(
  kind: "image" | "markdown" | "any",
  multiple = false
): Promise<{ supported: boolean; canceled: boolean; paths: string[] }> {
  try {
    const res = await fetch(
      `/__shnex_desktop/dialog/open-file?kind=${encodeURIComponent(kind)}&multiple=${
        multiple ? "1" : "0"
      }`,
      { method: "POST" }
    );
    if (!res.ok) {
      return { supported: false, canceled: true, paths: [] };
    }
    const data = (await res.json()) as { canceled?: boolean; paths?: string[] };
    return {
      supported: true,
      canceled: Boolean(data.canceled),
      paths: data.paths || [],
    };
  } catch {
    return { supported: false, canceled: true, paths: [] };
  }
}

export function buildSessionWSUrl(sessionId: string): string {
  if (!bridgeState) throw new Error("Bridge not initialized");
  return `${bridgeState.ws_base}/api/v1/sessions/${encodeURIComponent(sessionId)}/ws?token=${encodeURIComponent(bridgeState.token)}`;
}

// Desktop Frameless Window & Tray Controls
export async function windowAction(
  action: "minimize" | "maximize" | "close" | "hide" | "quit"
): Promise<void> {
  try {
    await fetch(`/__shnex_desktop/window/${action}`, { method: "POST" });
  } catch {
    // Ignore when running outside Wails WebView
  }
}

export async function setTrayStyle(mode: "color" | "mono"): Promise<void> {
  try {
    await fetch(`/__shnex_desktop/tray/style?mode=${encodeURIComponent(mode)}`, {
      method: "POST",
    });
  } catch {
    // Ignore outside Wails WebView
  }
}

export async function triggerNetworkChanged(): Promise<void> {
  await apiRequest("/api/v1/network/changed", "POST", { reason: "desktop_manual_refresh" });
}

// Hosts
export async function listHosts(): Promise<HostProfile[]> {
  const res = await apiRequest<{ hosts: HostProfile[] }>("/api/v1/hosts");
  return res.hosts || [];
}

export async function upsertHost(input: UpsertHostInput): Promise<HostProfile> {
  return apiRequest<HostProfile>("/api/v1/hosts", "POST", input);
}

export async function deleteHost(id: string): Promise<void> {
  await apiRequest(`/api/v1/hosts/${encodeURIComponent(id)}`, "DELETE");
}

export async function toggleFavoriteHost(id: string): Promise<HostProfile> {
  return apiRequest<HostProfile>(`/api/v1/hosts/${encodeURIComponent(id)}/favorite`, "POST", {});
}

export function buildWallpaperUrl(cacheBust = 0): string {
  if (!bridgeState) return "";
  return `${bridgeState.api_base}/api/v1/settings/wallpaper?token=${encodeURIComponent(bridgeState.token)}&v=${cacheBust}`;
}

export async function uploadWallpaper(dataBase64: string): Promise<AppSettings> {
  return apiRequest<AppSettings>("/api/v1/settings/wallpaper", "POST", {
    data_base64: dataBase64,
  });
}

export async function uploadWallpaperFile(
  file: Blob,
  fileName = ""
): Promise<AppSettings> {
  const q = fileName ? `?name=${encodeURIComponent(fileName)}` : "";
  return apiBinaryUpload<AppSettings>(
    `/api/v1/settings/wallpaper${q}`,
    file,
    file.type && file.type.startsWith("image/")
      ? file.type
      : "application/octet-stream"
  );
}

export async function uploadWallpaperFromLocalPath(
  localPath: string
): Promise<AppSettings> {
  return apiRequest<AppSettings>("/api/v1/settings/wallpaper", "POST", {
    local_path: localPath,
  });
}

export async function clearWallpaper(): Promise<AppSettings> {
  return apiRequest<AppSettings>("/api/v1/settings/wallpaper", "DELETE");
}

export async function importLocalSSHConfig(): Promise<{
  imported_count: number;
  hosts: HostProfile[];
}> {
  return apiRequest("/api/v1/hosts/import-ssh-config", "POST", {});
}

export async function pingAllHosts(): Promise<Record<string, number>> {
  const res = await apiRequest<{ latencies_ms: Record<string, number> }>(
    "/api/v1/hosts/ping",
    "POST",
    {}
  );
  return res.latencies_ms || {};
}

export function maskTargetAddress(raw: string, privacyMode: boolean): string {
  if (!privacyMode || !raw) return raw;
  let userPrefix = "";
  let hostPart = raw;
  const atIdx = raw.indexOf("@");
  if (atIdx >= 0) {
    userPrefix = raw.slice(0, atIdx + 1);
    hostPart = raw.slice(atIdx + 1);
  }
  let portSuffix = "";
  const colonIdx = hostPart.lastIndexOf(":");
  if (colonIdx > 0 && !hostPart.includes("]")) {
    portSuffix = hostPart.slice(colonIdx);
    hostPart = hostPart.slice(0, colonIdx);
  }
  // IPv4 masking: 8.134.56.76 -> 8.***.***.**6
  const ipv4 = hostPart.split(".");
  if (ipv4.length === 4 && ipv4.every((seg) => /^\d+$/.test(seg))) {
    const lastSeg = ipv4[3];
    const lastChar = lastSeg.slice(-1);
    return `${userPrefix}${ipv4[0]}.***.***.**${lastChar}${portSuffix}`;
  }
  // Domain / IPv6 masking
  if (hostPart.length <= 4) {
    return `${userPrefix}***${portSuffix}`;
  }
  return `${userPrefix}${hostPart.slice(0, 2)}***${hostPart.slice(-2)}${portSuffix}`;
}

// Sessions
export async function createSession(payload: {
  host_id?: string;
  protocol?: string;
  target?: string;
  user?: string;
  cols: number;
  rows: number;
  term?: string;
  default_dir?: string;
  keepalive_sec?: number;
  password?: string;
  relay?: RelayConfig;
  notify_on_bell?: boolean;
  notify_on_approval?: boolean;
  notify_on_idle?: boolean;
}): Promise<SessionSummary> {
  return apiRequest<SessionSummary>("/api/v1/sessions", "POST", {
    ...payload,
    auth: payload.password
      ? { mode: "password", password: payload.password, host_key: { insecure_ignore: true } }
      : undefined,
    notify_on_bell: payload.notify_on_bell ?? true,
    notify_on_approval: payload.notify_on_approval ?? true,
    notify_on_idle: payload.notify_on_idle ?? true,
  });
}

export async function deleteSession(id: string): Promise<void> {
  await apiRequest(`/api/v1/sessions/${encodeURIComponent(id)}`, "DELETE");
}

// Remote tmux conversations: the top tabs of a tmux host, shared with Android.
export interface RemoteTmuxSession {
  id: string;
  title: string;
  command: string;
  busy: boolean;
  path: string;
  attached_clients: number;
  created_unix: number;
}

export interface RemoteTmuxList {
  available: boolean;
  sessions: RemoteTmuxSession[];
}

export async function listRemoteTmuxSessions(sessionId: string): Promise<RemoteTmuxList> {
  return apiRequest<RemoteTmuxList>(`/api/v1/sessions/${encodeURIComponent(sessionId)}/tmux`);
}

export async function createRemoteTmuxSession(
  sessionId: string,
  title?: string
): Promise<RemoteTmuxSession> {
  return apiRequest<RemoteTmuxSession>(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/tmux`,
    "POST",
    title ? { title } : {}
  );
}

export async function renameRemoteTmuxSession(
  sessionId: string,
  name: string,
  title: string
): Promise<{ id: string; title: string }> {
  return apiRequest(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/tmux/${encodeURIComponent(name)}`,
    "PATCH",
    { title }
  );
}

export async function killRemoteTmuxSession(sessionId: string, name: string): Promise<void> {
  await apiRequest(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/tmux/${encodeURIComponent(name)}`,
    "DELETE"
  );
}

export async function getSessionStats(id: string): Promise<SystemStats> {
  return apiRequest<SystemStats>(`/api/v1/sessions/${encodeURIComponent(id)}/stats`);
}

export async function execSessionCommand(id: string, command: string): Promise<{ stdout: string; stderr: string; exit_code: number }> {
  return apiRequest(`/api/v1/sessions/${encodeURIComponent(id)}/exec`, "POST", { command });
}

export interface CompletionCandidate {
  label: string;
  insert_text: string;
  ghost_suffix: string;
  replace_len: number;
  kind:
    | "command"
    | "subcommand"
    | "git_branch"
    | "npm_script"
    | "ssh_host"
    | "wsl_distro"
    | "dir"
    | "file"
    | "flag";
  source: string;
  detail?: string;
}

export interface CompletionSuggestResponse {
  token_prefix: string;
  replace_len: number;
  candidates: CompletionCandidate[];
}

export async function suggestSessionCompletion(
  sessionId: string,
  line: string,
  cwd?: string
): Promise<CompletionSuggestResponse> {
  return apiRequest<CompletionSuggestResponse>(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/complete`,
    "POST",
    { line, cwd }
  );
}

export async function acceptSessionCompletion(
  sessionId: string,
  kind: string,
  label: string
): Promise<void> {
  await apiRequest(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/complete/accept`,
    "POST",
    { kind, label }
  );
}

// Native Go Markdown & Vector TeX Formula Typesetting ("core/markdown" + "core/texmath")
export interface RenderedMathBox {
  tex: string;
  display: boolean;
  svg: string;
  width: number;
  height: number;
  ascent: number;
  descent: number;
}

export interface MarkdownInline {
  kind:
    | "text"
    | "bold"
    | "italic"
    | "strike"
    | "code"
    | "link"
    | "image"
    | "math_inline";
  text?: string;
  url?: string;
  alt?: string;
  children?: MarkdownInline[];
  math?: RenderedMathBox;
}

export interface MarkdownListItem {
  checked?: boolean;
  inlines: MarkdownInline[];
}

export interface MarkdownBlock {
  kind:
    | "heading"
    | "paragraph"
    | "math_block"
    | "code_block"
    | "blockquote"
    | "list"
    | "table"
    | "hr";
  id?: string;
  level?: number;
  inlines?: MarkdownInline[];
  math?: RenderedMathBox;
  lang?: string;
  code?: string;
  alert_type?: string;
  ordered?: boolean;
  items?: MarkdownListItem[];
  aligns?: string[];
  headers?: MarkdownInline[][];
  rows?: MarkdownInline[][][];
}

export interface MarkdownHeadingEntry {
  id: string;
  level: number;
  title: string;
}

export interface CompiledMarkdownDocument {
  outline: MarkdownHeadingEntry[];
  blocks: MarkdownBlock[];
}

export async function getMarkdownSample(): Promise<{
  title: string;
  content: string;
  document: CompiledMarkdownDocument;
}> {
  return apiRequest("/api/v1/markdown/sample");
}

export async function renderMarkdownNative(
  content: string
): Promise<CompiledMarkdownDocument> {
  return apiRequest("/api/v1/markdown/render", "POST", { content });
}

export async function readLocalMarkdownFile(path: string): Promise<{
  title: string;
  path: string;
  content: string;
  document: CompiledMarkdownDocument;
}> {
  return apiRequest("/api/v1/markdown/local/read", "POST", { path });
}

export async function writeLocalMarkdownFile(
  path: string,
  content: string
): Promise<{
  saved: boolean;
  path: string;
  document: CompiledMarkdownDocument;
}> {
  return apiRequest("/api/v1/markdown/local/write", "POST", { path, content });
}

export function buildMarkdownAssetUrl(
  src: string,
  opts: { localBase?: string; sftpSessionId?: string; sftpBase?: string }
): string {
  if (!src) return "";
  if (
    src.startsWith("http://") ||
    src.startsWith("https://") ||
    src.startsWith("data:") ||
    src.startsWith("blob:")
  ) {
    return src;
  }
  if (!bridgeState) return src;
  const params = new URLSearchParams({
    token: bridgeState.token,
    src,
  });
  if (opts.sftpSessionId) {
    params.set("session_id", opts.sftpSessionId);
    if (opts.sftpBase) params.set("sftp_base", opts.sftpBase);
  } else if (opts.localBase) {
    params.set("local_base", opts.localBase);
  }
  return `${bridgeState.api_base}/api/v1/markdown/asset?${params.toString()}`;
}

// Port Forwarding ("新建端口转发")
export async function listSessionForwards(sessionId: string): Promise<PortForwardRule[]> {
  const res = await apiRequest<{ forwards: PortForwardRule[] }>(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/forwards`
  );
  return res.forwards || [];
}

export async function startSessionForward(
  sessionId: string,
  localPort: number,
  remoteAddr: string
): Promise<PortForwardRule> {
  return apiRequest<PortForwardRule>(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/forwards`,
    "POST",
    { local_port: localPort, remote_addr: remoteAddr }
  );
}

export async function stopSessionForward(sessionId: string, fwdId: string): Promise<void> {
  await apiRequest(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/forwards/${encodeURIComponent(fwdId)}`,
    "DELETE"
  );
}

// SFTP
export async function sftpList(sessionId: string, dirPath = ""): Promise<{ path: string; entries: FileEntry[] }> {
  const q = dirPath ? `?path=${encodeURIComponent(dirPath)}` : "";
  return apiRequest(`/api/v1/sessions/${encodeURIComponent(sessionId)}/sftp/list${q}`);
}

export async function sftpRead(sessionId: string, remotePath: string): Promise<{ path: string; size: number; content_base64: string }> {
  return apiRequest(`/api/v1/sessions/${encodeURIComponent(sessionId)}/sftp/read?path=${encodeURIComponent(remotePath)}`);
}

export async function sftpWrite(sessionId: string, remotePath: string, contentBase64: string): Promise<void> {
  await apiRequest(`/api/v1/sessions/${encodeURIComponent(sessionId)}/sftp/write`, "POST", {
    path: remotePath,
    content_base64: contentBase64,
  });
}

export async function sftpUploadFile(
  sessionId: string,
  remotePath: string,
  file: Blob
): Promise<void> {
  await apiBinaryUpload(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/sftp/upload?path=${encodeURIComponent(
      remotePath
    )}`,
    file,
    "application/octet-stream"
  );
}

export async function sftpUploadLocalPaths(
  sessionId: string,
  destDir: string,
  localPaths: string[]
): Promise<string[]> {
  const res = await apiRequest<{ uploaded: string[] }>(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/sftp/upload-local`,
    "POST",
    { dest_dir: destDir, local_paths: localPaths }
  );
  return res.uploaded || [];
}

export async function uploadSessionImage(
  sessionId: string,
  file: Blob,
  fileName = "paste.png"
): Promise<{ path: string; shell_arg: string; size: number }> {
  return apiBinaryUpload(
    `/api/v1/sessions/${encodeURIComponent(
      sessionId
    )}/upload-image?name=${encodeURIComponent(fileName)}`,
    file,
    file.type && file.type.startsWith("image/")
      ? file.type
      : "application/octet-stream"
  );
}

export async function uploadSessionImageFromLocalPath(
  sessionId: string,
  localPath: string
): Promise<{ path: string; shell_arg: string; size: number }> {
  return apiRequest(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/upload-image`,
    "POST",
    { local_path: localPath }
  );
}

export async function uploadSessionImageBase64(
  sessionId: string,
  dataBase64: string,
  fileName = "screenshot.png"
): Promise<{ path: string; shell_arg: string; size: number }> {
  return apiRequest(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/upload-image`,
    "POST",
    { data_base64: dataBase64, name: fileName }
  );
}

export async function sftpMkdir(sessionId: string, remotePath: string): Promise<void> {
  await apiRequest(`/api/v1/sessions/${encodeURIComponent(sessionId)}/sftp/mkdir`, "POST", { path: remotePath });
}

export async function sftpRemove(sessionId: string, remotePath: string): Promise<void> {
  await apiRequest(`/api/v1/sessions/${encodeURIComponent(sessionId)}/sftp/remove?path=${encodeURIComponent(remotePath)}`, "DELETE");
}

export async function sftpSetSudoPassword(sessionId: string, password: string): Promise<void> {
  await apiRequest(`/api/v1/sessions/${encodeURIComponent(sessionId)}/sftp/sudo-password`, "POST", {
    password,
  });
}

// Session Logs ("会话日志" Pane)
export async function listSessionLogs(): Promise<SessionLogMeta[]> {
  const res = await apiRequest<{ logs: SessionLogMeta[] }>("/api/v1/session-logs");
  return res.logs || [];
}

export async function getSessionLog(name: string): Promise<{ name: string; content: string }> {
  return apiRequest(`/api/v1/session-logs/${encodeURIComponent(name)}`);
}

export async function deleteSessionLog(name: string): Promise<void> {
  await apiRequest(`/api/v1/session-logs/${encodeURIComponent(name)}`, "DELETE");
}

// Snippets
export async function listSnippets(): Promise<Snippet[]> {
  const res = await apiRequest<{ snippets: Snippet[] }>("/api/v1/snippets");
  return res.snippets || [];
}

export async function upsertSnippet(sn: Snippet): Promise<Snippet[]> {
  const res = await apiRequest<{ snippets: Snippet[] }>("/api/v1/snippets", "POST", sn);
  return res.snippets || [];
}

export async function deleteSnippet(id: string): Promise<Snippet[]> {
  const res = await apiRequest<{ snippets: Snippet[] }>(`/api/v1/snippets/${encodeURIComponent(id)}`, "DELETE");
  return res.snippets || [];
}

// Tailnet
export async function getTailnetStatus(): Promise<TailnetStatus> {
  return apiRequest<TailnetStatus>("/api/v1/tailnet/status");
}

export async function tailnetUp(cfg: { hostname?: string; control_url?: string; auth_key?: string }): Promise<TailnetStatus> {
  return apiRequest<TailnetStatus>("/api/v1/tailnet/up", "POST", cfg);
}

export async function tailnetDown(): Promise<void> {
  await apiRequest("/api/v1/tailnet/down", "POST", {});
}

export async function listTailnetNodes(): Promise<TailnetNode[]> {
  const res = await apiRequest<{ nodes: TailnetNode[] }>("/api/v1/tailnet/nodes?online=true");
  return res.nodes || [];
}

// AI Ops Assistant
export async function getAIConfig(): Promise<AIConfig> {
  return apiRequest<AIConfig>("/api/v1/ai/config");
}

export async function updateAIConfig(cfg: { base_url: string; model: string; api_key?: string }): Promise<AIConfig> {
  return apiRequest<AIConfig>("/api/v1/ai/config", "POST", cfg);
}

export async function aiChat(
  sessionId: string,
  messages: AIMessage[],
  policy: "ask" | "auto_review" | "allow_all",
  providerId?: string,
  model?: string,
  terminalContext?: string
): Promise<AIChatResponse> {
  return apiRequest<AIChatResponse>(`/api/v1/sessions/${encodeURIComponent(sessionId)}/ai/chat`, "POST", {
    messages,
    policy,
    provider_id: providerId,
    model,
    terminal_context: terminalContext,
  });
}

export async function aiApprove(
  sessionId: string,
  payload: {
    call_id: string;
    command: string;
    approved: boolean;
    history: AIMessage[];
    policy: "ask" | "auto_review" | "allow_all";
    model?: string;
  }
): Promise<AIChatResponse> {
  return apiRequest<AIChatResponse>(`/api/v1/sessions/${encodeURIComponent(sessionId)}/ai/approve`, "POST", payload);
}

export async function getSessionScrollback(sessionId: string, lines: number): Promise<string> {
  const res = await apiRequest<{ text: string }>(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/scrollback?lines=${lines}`
  );
  return res.text || "";
}

export async function getSettings(): Promise<AppSettings> {
  return apiRequest<AppSettings>("/api/v1/settings");
}

export async function saveSettings(s: AppSettings): Promise<AppSettings> {
  return apiRequest<AppSettings>("/api/v1/settings", "PUT", s);
}

export async function setMasterPassword(password: string): Promise<AppSettings> {
  return apiRequest<AppSettings>("/api/v1/settings/security/set-master", "POST", {
    password,
  });
}

export async function lockVault(): Promise<AppSettings> {
  return apiRequest<AppSettings>("/api/v1/settings/security/lock", "POST", {});
}

export async function unlockVault(password: string): Promise<AppSettings> {
  return apiRequest<AppSettings>("/api/v1/settings/security/unlock", "POST", {
    password,
  });
}

export async function listKnownHosts(): Promise<{ path: string; entries: KnownHostEntry[] }> {
  return apiRequest("/api/v1/known-hosts");
}

export async function deleteKnownHost(lineNumber: number): Promise<{ path: string; entries: KnownHostEntry[] }> {
  return apiRequest(`/api/v1/known-hosts/${lineNumber}`, "DELETE");
}

export async function upsertAIProvider(input: {
  id?: string;
  name: string;
  protocol: string;
  base_url: string;
  model: string;
  api_key?: string;
  is_default: boolean;
}): Promise<AppSettings> {
  return apiRequest<AppSettings>("/api/v1/settings/ai/providers", "POST", input);
}

export async function setDefaultAIProvider(id: string): Promise<AppSettings> {
  return apiRequest<AppSettings>(
    `/api/v1/settings/ai/providers/${encodeURIComponent(id)}/default`,
    "POST",
    {}
  );
}

export async function deleteAIProvider(id: string): Promise<AppSettings> {
  return apiRequest<AppSettings>(
    `/api/v1/settings/ai/providers/${encodeURIComponent(id)}`,
    "DELETE"
  );
}

export async function addVaultCredential(input: {
  name: string;
  kind: string;
  username: string;
  secret: string;
}): Promise<AppSettings> {
  return apiRequest<AppSettings>("/api/v1/settings/credentials", "POST", input);
}

export async function deleteVaultCredential(id: string): Promise<AppSettings> {
  return apiRequest<AppSettings>(
    `/api/v1/settings/credentials/${encodeURIComponent(id)}`,
    "DELETE"
  );
}

export async function upsertNamedProxy(input: {
  id?: string;
  name: string;
  scheme: string;
  host: string;
  port: number;
  username?: string;
  password?: string;
}): Promise<AppSettings> {
  return apiRequest<AppSettings>("/api/v1/settings/proxies", "POST", input);
}

export async function deleteNamedProxy(id: string): Promise<AppSettings> {
  return apiRequest<AppSettings>(
    `/api/v1/settings/proxies/${encodeURIComponent(id)}`,
    "DELETE"
  );
}

export async function exportBackupBundle(): Promise<unknown> {
  return apiRequest("/api/v1/settings/backup/export");
}

export async function importBackupBundle(bundle: unknown): Promise<void> {
  await apiRequest("/api/v1/settings/backup/import", "POST", bundle);
}

export async function listNotifications(): Promise<AppNotification[]> {
  const res = await apiRequest<{ notifications: AppNotification[] }>("/api/v1/notifications");
  return res.notifications || [];
}

export async function sendTestNotification(): Promise<{ sent: boolean; notification: AppNotification }> {
  return apiRequest("/api/v1/notifications/test", "POST", {});
}

// Formatting Helpers
export function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return "0B";
  const units = ["B", "K", "M", "G", "T"];
  let idx = 0;
  let val = bytes;
  while (val >= 1024 && idx < units.length - 1) {
    val /= 1024;
    idx++;
  }
  return `${val >= 10 || idx === 0 ? val.toFixed(0) : val.toFixed(1)}${units[idx]}`;
}

export function formatRate(bytesPerSec: number): string {
  if (!bytesPerSec || bytesPerSec <= 0) return "0B/s";
  return `${formatBytes(bytesPerSec)}/s`;
}

export function formatDurationSec(sec: number): string {
  if (!sec || sec < 60) return `${Math.max(1, Math.floor(sec || 0))} 秒`;
  const mins = Math.floor(sec / 60);
  if (mins < 60) return `${mins} 分钟`;
  const hrs = Math.floor(mins / 60);
  return `${hrs} 小时 ${mins % 60} 分`;
}
