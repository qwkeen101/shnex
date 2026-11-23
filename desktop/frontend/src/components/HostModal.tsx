import React, { useMemo, useState } from "react";
import { ChevronDown, ChevronRight, FolderPlus } from "lucide-react";
import { AppSettings, HostProfile, UpsertHostInput } from "../api";
import {
  HOST_COLORS,
  HOST_ICONS,
  HostBadge,
  pickAutoColor,
  pickAutoIcon,
  renderHostVectorIcon,
} from "./HostBadge";

interface Props {
  initial?: HostProfile | null;
  existingGroups: string[];
  settings?: AppSettings | null;
  onSave: (input: UpsertHostInput) => Promise<void>;
  onClose: () => void;
}

export const HostModal: React.FC<Props> = ({
  initial,
  existingGroups,
  settings,
  onSave,
  onClose,
}) => {
  const [name, setName] = useState(initial?.name ?? "");
  const [protocol, setProtocol] = useState<"ssh" | "telnet" | "serial">(
    (initial?.protocol as "ssh" | "telnet" | "serial") ?? "ssh"
  );
  const [host, setHost] = useState(initial?.host ?? "");
  const [port, setPort] = useState(initial?.port ?? 22);
  const [user, setUser] = useState(initial?.user ?? "root");

  const [group, setGroup] = useState(initial?.group ?? "");
  const [customGroups, setCustomGroups] = useState<string[]>([]);

  // Appearance
  const [icon, setIcon] = useState(initial?.icon || "rocket");
  const [badgeText, setBadgeText] = useState(initial?.badge_text ?? "");
  const [color, setColor] = useState(initial?.color || "#f59e0b");

  // Auth
  const [authMode, setAuthMode] = useState<
    "auto" | "password" | "private_key" | "vault" | "tailscale_ssh"
  >(() => {
    if (initial?.credential_id) return "vault";
    if (
      initial?.auth_mode === "password" ||
      initial?.auth_mode === "private_key" ||
      initial?.auth_mode === "tailscale_ssh"
    ) {
      return initial.auth_mode;
    }
    return "auto";
  });
  const [password, setPassword] = useState("");
  const [privateKey, setPrivateKey] = useState("");
  const [credentialId, setCredentialId] = useState(
    initial?.credential_id ?? ""
  );

  // Collapsible Sections
  const [openNetwork, setOpenNetwork] = useState(false);
  const [openDefaultPath, setOpenDefaultPath] = useState(
    Boolean(initial?.default_dir)
  );

  // Network & Relay
  const [proxyUrl, setProxyUrl] = useState(initial?.route?.proxy_url ?? "");
  const [useTsnet, setUseTsnet] = useState(initial?.route?.use_tsnet ?? false);
  const [useShnexAgent, setUseShnexAgent] = useState(
    initial?.relay?.mode === "shnex_agent"
  );
  const [pinnedSha, setPinnedSha] = useState(
    initial?.host_key?.pinned_sha256 ?? ""
  );
  const [insecureHk, setInsecureHk] = useState(
    initial?.host_key?.insecure_ignore ?? true
  );

  // Default Path & File Options
  const [defaultDir, setDefaultDir] = useState(initial?.default_dir ?? "");
  const [fileProtocol, setFileProtocol] = useState<"auto" | "sftp" | "scp">(
    (initial?.file_protocol as "auto" | "sftp" | "scp") ?? "auto"
  );
  const [sftpSudo, setSftpSudo] = useState(initial?.sftp_sudo ?? false);

  // Tmux Session, Post-Login Script, Metrics Bar, Encoding, TERM, Session Log
  const [tmuxSession, setTmuxSession] = useState(() => {
    if (initial?.tmux_session !== undefined) return initial.tmux_session;
    if (initial?.relay?.mode === "tmux") return initial.relay.session_name ?? "";
    return "";
  });
  const [postLoginScript, setPostLoginScript] = useState(
    initial?.post_login_script ?? ""
  );
  const [metricsBar, setMetricsBar] = useState<"follow" | "on" | "off">(
    (initial?.metrics_bar as "follow" | "on" | "off") ?? "follow"
  );
  const [terminalEncoding, setTerminalEncoding] = useState(
    initial?.terminal_encoding || "utf-8"
  );
  const [termType, setTermType] = useState(initial?.term_type ?? "");
  const [autoSessionLog, setAutoSessionLog] = useState(
    initial?.auto_session_log ?? false
  );

  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const allGroups = useMemo(() => {
    const s = new Set<string>();
    for (const g of existingGroups) if (g) s.add(g);
    for (const g of customGroups) if (g) s.add(g);
    if (group) s.add(group);
    return Array.from(s);
  }, [existingGroups, customGroups, group]);

  const handleAddGroup = () => {
    const g = window.prompt("新建主机分组名称（如：生产环境 / 香港节点）:");
    if (!g?.trim()) return;
    const clean = g.trim();
    setCustomGroups((prev) => [...prev, clean]);
    setGroup(clean);
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!host.trim()) {
      setErr("请填写主机地址");
      return;
    }
    setSaving(true);
    setErr(null);
    try {
      const effectiveAuth = authMode === "vault" ? "auto" : authMode;
      const trimmedTmux = tmuxSession.trim();
      const relayMode: "none" | "tmux" | "shnex_agent" = useShnexAgent
        ? "shnex_agent"
        : trimmedTmux
        ? "tmux"
        : "none";

      await onSave({
        id: initial?.id,
        name: name.trim() || host.trim(),
        protocol,
        group: group.trim(),
        host: host.trim(),
        port: Number(port) || 22,
        user: user.trim() || "root",
        icon,
        badge_text: badgeText.trim().slice(0, 4),
        color,
        auth_mode: effectiveAuth,
        credential_id: authMode === "vault" ? credentialId : "",
        password: authMode === "password" ? password : "",
        private_key_pem: authMode === "private_key" ? privateKey : "",
        default_dir: defaultDir.trim(),
        file_protocol: fileProtocol,
        sftp_sudo: sftpSudo,
        tmux_session: trimmedTmux,
        post_login_script: postLoginScript,
        metrics_bar: metricsBar,
        terminal_encoding: terminalEncoding,
        term_type: termType,
        auto_session_log: autoSessionLog,
        host_key: {
          pinned_sha256: pinnedSha.trim(),
          insecure_ignore: insecureHk,
        },
        route: {
          use_tsnet: useTsnet || authMode === "tailscale_ssh",
          proxy_url: proxyUrl.trim(),
        },
        relay: {
          mode: relayMode,
          session_name: trimmedTmux || "shnex-main",
        },
      });
      onClose();
    } catch (ex) {
      setErr((ex as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const labelCls = "w-20 text-right text-[#a6abb6] shrink-0 select-none";
  const helpCls = "text-[11px] text-[#7c828f] leading-relaxed";

  return (
    <div
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      className="fixed inset-0 z-50 bg-black/65 flex items-center justify-center p-4"
    >
      <form
        onSubmit={handleSubmit}
        className="w-[560px] max-h-[92vh] flex flex-col rounded-2xl bg-[#1f2024] border border-[#2f3138] shadow-2xl text-xs overflow-hidden"
      >
        {/* Top Header with Live Dynamic HostBadge Preview */}
        <div className="px-6 pt-5 pb-3 flex items-center gap-3.5 shrink-0">
          <HostBadge
            icon={icon}
            badgeText={badgeText}
            color={color}
            size="lg"
          />
          <h2 className="text-base font-semibold text-white">
            {initial ? "编辑主机" : "新建主机"}
          </h2>
        </div>

        {/* Scrollable Form Body */}
        <div className="px-6 py-2 flex-1 overflow-y-auto space-y-4">
          {err && (
            <div className="p-2.5 rounded-lg bg-[#ef4444]/15 border border-[#ef4444]/40 text-[#f87171]">
              {err}
            </div>
          )}

          {/* Row: 名称 */}
          <div className="flex items-center gap-3.5">
            <label className={labelCls}>名称</label>
            <input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="web-01"
              className="flex-1 px-3 py-2 rounded-lg bg-[#2a2b30] border border-[#363840] focus:border-[#2b7de9] text-white placeholder:text-[#6e737e] outline-none transition-colors"
            />
          </div>

          {/* Row: 协议 */}
          <div className="flex items-center gap-3.5">
            <label className={labelCls}>协议</label>
            <div className="flex-1 p-1 rounded-lg bg-[#2a2b30] flex items-center gap-1">
              {(
                [
                  ["ssh", "SSH"],
                  ["telnet", "Telnet"],
                  ["serial", "串口"],
                ] as const
              ).map(([val, label]) => (
                <button
                  key={val}
                  type="button"
                  onClick={() => {
                    setProtocol(val);
                    if (val === "telnet" && port === 22) setPort(23);
                    if (val === "ssh" && port === 23) setPort(22);
                  }}
                  className={`px-3.5 py-1 rounded-md transition ${
                    protocol === val
                      ? "bg-[#1b1c20] text-white font-semibold shadow-sm"
                      : "text-[#8e939e] hover:text-white"
                  }`}
                >
                  {label}
                </button>
              ))}
            </div>
          </div>

          {/* Row: 主机地址 */}
          <div className="flex items-center gap-3.5">
            <label className={labelCls}>主机地址</label>
            <input
              required
              value={host}
              onChange={(e) => setHost(e.target.value)}
              placeholder="example.com"
              className="flex-1 px-3 py-2 rounded-lg bg-[#2a2b30] border border-[#363840] focus:border-[#2b7de9] text-white font-mono placeholder:text-[#6e737e] outline-none transition-colors"
            />
          </div>

          {/* Row: 端口 */}
          <div className="flex items-center gap-3.5">
            <label className={labelCls}>端口</label>
            <input
              type="number"
              value={port}
              onChange={(e) => setPort(Number(e.target.value))}
              className="w-28 px-3 py-2 rounded-lg bg-[#2a2b30] border border-[#363840] focus:border-[#2b7de9] text-white font-mono outline-none transition-colors"
            />
          </div>

          {/* Row: 用户名 */}
          <div className="flex items-center gap-3.5">
            <label className={labelCls}>用户名</label>
            <input
              value={user}
              onChange={(e) => setUser(e.target.value)}
              placeholder="root"
              className="flex-1 px-3 py-2 rounded-lg bg-[#2a2b30] border border-[#363840] focus:border-[#2b7de9] text-white font-mono placeholder:text-[#6e737e] outline-none transition-colors"
            />
          </div>

          {/* Row: 分组 */}
          <div className="flex items-center gap-3.5">
            <label className={labelCls}>分组</label>
            <div className="flex-1 flex items-center gap-2">
              <select
                value={group}
                onChange={(e) => setGroup(e.target.value)}
                className="flex-1 px-3 py-2 rounded-lg bg-[#2a2b30] border border-[#363840] text-white outline-none"
              >
                <option value="">不分组</option>
                {allGroups.map((g) => (
                  <option key={g} value={g}>
                    {g}
                  </option>
                ))}
              </select>
              <button
                type="button"
                onClick={handleAddGroup}
                className="p-2 rounded-lg bg-[#2a2b30] border border-[#363840] hover:bg-[#34363d] text-[#a6abb6] hover:text-white"
                title="新建分组"
              >
                <FolderPlus size={14} />
              </button>
            </div>
          </div>

          {/* Row: 外观 (22 Icons + Emoji/4-Char Input + 8 Color Dots + Auto Pills) */}
          <div className="flex items-start gap-3.5 pt-0.5">
            <label className={`${labelCls} pt-1.5`}>外观</label>
            <div className="flex-1 space-y-2.5">
              <div className="flex flex-wrap gap-1">
                {HOST_ICONS.map((ic) => {
                  const selected = badgeText.trim() === "" && icon === ic;
                  return (
                    <button
                      key={ic}
                      type="button"
                      onClick={() => {
                        setIcon(ic);
                        setBadgeText("");
                      }}
                      className={`w-7 h-7 rounded-lg flex items-center justify-center transition ${
                        selected
                          ? "bg-[#343844] text-white ring-1 ring-[#3b82f6]"
                          : "text-[#8e939e] hover:bg-[#2a2b30] hover:text-white"
                      }`}
                      title={ic}
                    >
                      {renderHostVectorIcon(ic, 14)}
                    </button>
                  );
                })}
              </div>

              <div className="flex items-center justify-between gap-3">
                <input
                  value={badgeText}
                  maxLength={4}
                  onChange={(e) => setBadgeText(e.target.value)}
                  placeholder="🚀 或最多 4 个字符"
                  className="w-52 px-3 py-1.5 rounded-lg bg-[#2a2b30] border border-[#363840] focus:border-[#2b7de9] text-white placeholder:text-[#6e737e] outline-none"
                />
                <button
                  type="button"
                  onClick={() => {
                    setBadgeText("");
                    setIcon(pickAutoIcon(name, host));
                  }}
                  className="px-3.5 py-1.5 rounded-full bg-[#1d7dfa] hover:bg-[#358bfa] text-white font-medium shrink-0 transition"
                >
                  自动图标
                </button>
              </div>

              <div className="flex items-center justify-between gap-3">
                <div className="flex items-center gap-2">
                  {HOST_COLORS.map((c) => (
                    <button
                      key={c}
                      type="button"
                      onClick={() => setColor(c)}
                      style={{ backgroundColor: c }}
                      className={`w-6 h-6 rounded-full transition-transform ${
                        color === c
                          ? "scale-110 ring-2 ring-white ring-offset-2 ring-offset-[#1f2024]"
                          : "opacity-85 hover:opacity-100"
                      }`}
                    />
                  ))}
                </div>
                <button
                  type="button"
                  onClick={() => setColor(pickAutoColor(name, host))}
                  className="px-3.5 py-1.5 rounded-full bg-[#1d7dfa] hover:bg-[#358bfa] text-white font-medium shrink-0 transition"
                >
                  自动颜色
                </button>
              </div>
            </div>
          </div>

          {/* Row: 认证方式 */}
          <div className="flex items-center gap-3.5">
            <label className={labelCls}>认证方式</label>
            <select
              value={authMode}
              onChange={(e) => setAuthMode(e.target.value as typeof authMode)}
              className="flex-1 px-3 py-2 rounded-lg bg-[#2a2b30] border border-[#363840] text-white outline-none"
            >
              <option value="auto">自动（本机密钥，失败再询问密码）</option>
              <option value="password">密码</option>
              <option value="private_key">私钥（PEM / OpenSSH）</option>
              <option value="vault">从凭据保险箱选择</option>
              <option value="tailscale_ssh">Tailscale SSH（免密）</option>
            </select>
          </div>

          {authMode === "password" && (
            <div className="flex items-center gap-3.5">
              <label className={labelCls}>密码</label>
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={
                  initial?.has_secret
                    ? "已存入凭据箱（留空保持不变）"
                    : "输入远端密码"
                }
                className="flex-1 px-3 py-2 rounded-lg bg-[#2a2b30] border border-[#363840] text-white outline-none"
              />
            </div>
          )}

          {authMode === "private_key" && (
            <div className="flex items-start gap-3.5">
              <label className={`${labelCls} pt-2`}>私钥内容</label>
              <textarea
                rows={3}
                value={privateKey}
                onChange={(e) => setPrivateKey(e.target.value)}
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                className="flex-1 px-3 py-2 rounded-lg bg-[#2a2b30] border border-[#363840] text-white font-mono outline-none"
              />
            </div>
          )}

          {authMode === "vault" && (
            <div className="flex items-center gap-3.5">
              <label className={labelCls}>选择凭据</label>
              <select
                value={credentialId}
                onChange={(e) => setCredentialId(e.target.value)}
                className="flex-1 px-3 py-2 rounded-lg bg-[#2a2b30] border border-[#363840] text-white outline-none"
              >
                <option value="">请选择已保存凭据...</option>
                {(settings?.credentials || []).map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name} ({c.kind})
                  </option>
                ))}
              </select>
            </div>
          )}

          {/* Collapsible Section: > 网络 */}
          <div className="pl-[94px] space-y-2.5">
            <button
              type="button"
              onClick={() => setOpenNetwork((v) => !v)}
              className="flex items-center gap-1.5 text-[#d1d5db] hover:text-white font-medium"
            >
              {openNetwork ? (
                <ChevronDown size={14} className="text-[#8e939e]" />
              ) : (
                <ChevronRight size={14} className="text-[#8e939e]" />
              )}
              <span>网络</span>
            </button>

            {openNetwork && (
              <div className="p-3 rounded-xl bg-[#25262b] border border-[#32343c] space-y-2.5">
                <div className="grid grid-cols-2 gap-2.5">
                  <label className="space-y-1 text-[#9ca3af]">
                    <span>代理 URL (SOCKS5 / HTTP)</span>
                    <input
                      value={proxyUrl}
                      onChange={(e) => setProxyUrl(e.target.value)}
                      placeholder="socks5://127.0.0.1:1080"
                      className="w-full px-2.5 py-1.5 rounded-lg bg-[#1b1c20] border border-[#32343c] text-white font-mono outline-none"
                    />
                  </label>
                  <label className="space-y-1 text-[#9ca3af]">
                    <span>固定指纹 (可选 SHA256:...)</span>
                    <input
                      value={pinnedSha}
                      onChange={(e) => setPinnedSha(e.target.value)}
                      placeholder="SHA256:..."
                      className="w-full px-2.5 py-1.5 rounded-lg bg-[#1b1c20] border border-[#32343c] text-white font-mono outline-none"
                    />
                  </label>
                </div>

                <div className="flex flex-wrap items-center gap-x-5 gap-y-2 pt-1">
                  <label className="flex items-center gap-2 cursor-pointer text-[#d1d5db]">
                    <input
                      type="checkbox"
                      checked={useTsnet}
                      onChange={(e) => setUseTsnet(e.target.checked)}
                    />
                    <span>走内置用户态 Tailscale 虚拟网卡</span>
                  </label>
                  <label className="flex items-center gap-2 cursor-pointer text-[#d1d5db]">
                    <input
                      type="checkbox"
                      checked={useShnexAgent}
                      onChange={(e) => setUseShnexAgent(e.target.checked)}
                    />
                    <span>启用远端 shnex-agent 守护多路复用</span>
                  </label>
                  <label className="flex items-center gap-2 cursor-pointer text-[#d1d5db]">
                    <input
                      type="checkbox"
                      checked={insecureHk}
                      onChange={(e) => setInsecureHk(e.target.checked)}
                    />
                    <span>信任未知 HostKey</span>
                  </label>
                </div>
              </div>
            )}
          </div>

          {/* Collapsible Section: > 默认路径 */}
          <div className="pl-[94px] space-y-2.5">
            <button
              type="button"
              onClick={() => setOpenDefaultPath((v) => !v)}
              className="flex items-center gap-1.5 text-[#d1d5db] hover:text-white font-medium"
            >
              {openDefaultPath ? (
                <ChevronDown size={14} className="text-[#8e939e]" />
              ) : (
                <ChevronRight size={14} className="text-[#8e939e]" />
              )}
              <span>默认路径</span>
            </button>

            {openDefaultPath && (
              <div className="p-3 rounded-xl bg-[#25262b] border border-[#32343c]">
                <label className="block space-y-1 text-[#9ca3af]">
                  <span>终端与文件面板打开时的默认远端目录</span>
                  <input
                    value={defaultDir}
                    onChange={(e) => setDefaultDir(e.target.value)}
                    placeholder="/root 或 /var/www"
                    className="w-full px-2.5 py-1.5 rounded-lg bg-[#1b1c20] border border-[#32343c] text-white font-mono outline-none"
                  />
                </label>
              </div>
            )}
          </div>

          {/* ==================== BOTTOM HALF (Matches Screenshot 7!) ==================== */}

          {/* Row: 文件协议 */}
          <div className="flex items-start gap-3.5">
            <label className={`${labelCls} pt-2`}>文件协议</label>
            <div className="flex-1 space-y-1.5">
              <div className="p-1 rounded-lg bg-[#2a2b30] flex items-center gap-1">
                {(
                  [
                    ["auto", "自动"],
                    ["sftp", "SFTP"],
                    ["scp", "SCP"],
                  ] as const
                ).map(([val, label]) => (
                  <button
                    key={val}
                    type="button"
                    onClick={() => setFileProtocol(val)}
                    className={`px-3.5 py-1 rounded-md transition ${
                      fileProtocol === val
                        ? "bg-[#1b1c20] text-white font-semibold shadow-sm"
                        : "text-[#8e939e] hover:text-white"
                    }`}
                  >
                    {label}
                  </button>
                ))}
              </div>
              <div className={helpCls}>
                文件面板使用 SFTP，服务器不支持 SFTP 时改用其 shell 命令与 scp。
              </div>
            </div>
          </div>

          {/* Row: 文件 (SFTP) sudo */}
          <div className="flex items-start gap-3.5">
            <label className={`${labelCls} pt-0.5`}>文件 (SFTP)</label>
            <div className="flex-1 space-y-1.5">
              <label className="inline-flex items-center gap-2 cursor-pointer text-[#e5e7eb]">
                <input
                  type="checkbox"
                  checked={sftpSudo}
                  onChange={(e) => setSftpSudo(e.target.checked)}
                  className="w-3.5 h-3.5 rounded accent-[#2b7de9]"
                />
                <span>使用 sudo 提权（以 root 身份）打开文件</span>
              </label>
              <div className={helpCls}>
                文件面板通过 sudo 运行服务器的 sftp-server，可读写任意文件。sudo
                使用登录密码应答，没有时会询问密码，仅在本次运行中保存在内存。
              </div>
            </div>
          </div>

          {/* Row: tmux 会话 */}
          <div className="flex items-start gap-3.5">
            <label className={`${labelCls} pt-2`}>tmux 会话</label>
            <div className="flex-1 space-y-1.5">
              <input
                value={tmuxSession}
                onChange={(e) => setTmuxSession(e.target.value)}
                placeholder="会话名称（留空不启用）"
                className="w-full px-3 py-2 rounded-lg bg-[#2a2b30] border border-[#363840] focus:border-[#2b7de9] text-white font-mono placeholder:font-sans placeholder:text-[#6e737e] outline-none transition-colors"
              />
              <div className={helpCls}>
                Shell 就绪后，此主机的每个终端都会附加到该 tmux
                会话（不存在时自动创建），重连后也会重新附加。
              </div>
            </div>
          </div>

          {/* Row: 登录后执行 */}
          <div className="flex items-start gap-3.5">
            <label className={`${labelCls} pt-2`}>登录后执行</label>
            <div className="flex-1 space-y-1.5">
              <textarea
                rows={3}
                value={postLoginScript}
                onChange={(e) => setPostLoginScript(e.target.value)}
                placeholder="source ~/.venv/bin/activate"
                className="w-full px-3 py-2 rounded-lg bg-[#2a2b30] border border-[#363840] focus:border-[#2b7de9] text-white font-mono placeholder:text-[#6e737e] outline-none transition-colors resize-y"
              />
              <div className={helpCls}>
                每次终端的 Shell 启动（包括重连）时逐行输入执行：在进入终端目录之后、附加
                tmux 之前。会读取输入的命令（如 sudo）会把其后的行当作输入。
              </div>
            </div>
          </div>

          {/* Row: 指标栏 */}
          <div className="flex items-center gap-3.5">
            <label className={labelCls}>指标栏</label>
            <div className="flex-1 p-1 rounded-lg bg-[#2a2b30] flex items-center gap-1">
              {(
                [
                  ["follow", "跟随全局"],
                  ["on", "开"],
                  ["off", "关"],
                ] as const
              ).map(([val, label]) => (
                <button
                  key={val}
                  type="button"
                  onClick={() => setMetricsBar(val)}
                  className={`px-3.5 py-1 rounded-md transition ${
                    metricsBar === val
                      ? "bg-[#1b1c20] text-white font-semibold shadow-sm"
                      : "text-[#8e939e] hover:text-white"
                  }`}
                >
                  {label}
                </button>
              ))}
            </div>
          </div>

          {/* Row: 终端编码 */}
          <div className="flex items-start gap-3.5">
            <label className={`${labelCls} pt-2`}>终端编码</label>
            <div className="flex-1 space-y-1.5">
              <select
                value={terminalEncoding}
                onChange={(e) => setTerminalEncoding(e.target.value)}
                className="w-full px-3 py-2 rounded-lg bg-[#2a2b30] border border-[#363840] text-white outline-none"
              >
                <option value="utf-8">UTF-8</option>
                <option value="gb18030">GBK / GB18030 (简体中文)</option>
                <option value="big5">Big5 (繁體中文)</option>
                <option value="shift_jis">Shift_JIS (日本語)</option>
                <option value="euc-kr">EUC-KR (한국어)</option>
                <option value="iso-8859-1">ISO-8859-1 (Latin-1)</option>
              </select>
              <div className={helpCls}>
                此主机新建的终端将按此编码解码输出。
              </div>
            </div>
          </div>

          {/* Row: 终端类型 */}
          <div className="flex items-start gap-3.5">
            <label className={`${labelCls} pt-2`}>终端类型</label>
            <div className="flex-1 space-y-1.5">
              <select
                value={termType}
                onChange={(e) => setTermType(e.target.value)}
                className="w-full px-3 py-2 rounded-lg bg-[#2a2b30] border border-[#363840] text-white outline-none"
              >
                <option value="">跟随全局 (xterm-256color)</option>
                <option value="xterm-256color">xterm-256color</option>
                <option value="xterm">xterm</option>
                <option value="tmux-256color">tmux-256color</option>
                <option value="vt100">vt100</option>
                <option value="vt220">vt220</option>
                <option value="linux">linux</option>
              </select>
              <div className={helpCls}>
                此主机新建的终端将报告此 TERM。
              </div>
            </div>
          </div>

          {/* Row: 会话日志 */}
          <div className="flex items-start gap-3.5 pb-2">
            <label className={`${labelCls} pt-0.5`}>会话日志</label>
            <div className="flex-1 space-y-1.5">
              <label className="inline-flex items-center gap-2 cursor-pointer text-[#e5e7eb]">
                <input
                  type="checkbox"
                  checked={autoSessionLog}
                  onChange={(e) => setAutoSessionLog(e.target.checked)}
                  className="w-3.5 h-3.5 rounded accent-[#2b7de9]"
                />
                <span>连接时自动记录此主机的终端</span>
              </label>
              <div className={helpCls}>
                此主机的每个终端在连接后都会开始记录会话日志，保存到「会话日志」窗格
              </div>
            </div>
          </div>
        </div>

        {/* Bottom Footer Buttons */}
        <div className="px-6 py-4 border-t border-[#2c2e35] flex items-center justify-end gap-2.5 shrink-0">
          <button
            type="button"
            onClick={onClose}
            className="px-5 py-1.5 rounded-full bg-[#2b2d32] hover:bg-[#353840] text-[#d1d5db] font-medium transition"
          >
            取消
          </button>
          <button
            type="submit"
            disabled={saving}
            className="px-5 py-1.5 rounded-full bg-[#1d7dfa] hover:bg-[#358bfa] text-white font-medium transition"
          >
            {saving ? "保存中..." : "保存"}
          </button>
        </div>
      </form>
    </div>
  );
};
