import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  Activity,
  AlignLeft,
  AppWindow,
  Archive,
  ArrowLeftRight,
  Bot,
  Clipboard,
  Code2,
  Container,
  Download,
  FileText,
  Folder,
  Globe,
  History,
  Info,
  KeyRound,
  Keyboard,
  Lock,
  Minus,
  Palette,
  PanelRight,
  Pencil,
  Plug,
  Plus,
  Radio,
  RefreshCw,
  Search,
  Settings,
  ShieldCheck,
  ShieldEllipsis,
  Sparkles,
  Star,
  TerminalSquare,
  Trash2,
  Unlock,
  X,
  Zap,
} from "lucide-react";
import {
  AIProvider,
  AppSettings,
  KnownHostEntry,
  RightRailVisibility,
  addVaultCredential,
  buildWallpaperUrl,
  clearWallpaper,
  deleteAIProvider,
  deleteKnownHost,
  deleteNamedProxy,
  deleteVaultCredential,
  exportBackupBundle,
  importBackupBundle,
  importLocalSSHConfig,
  listKnownHosts,
  lockVault,
  openNativeFileDialog,
  saveSettings,
  sendTestNotification,
  setDefaultAIProvider,
  setMasterPassword,
  setTrayStyle,
  unlockVault,
  uploadWallpaperFile,
  uploadWallpaperFromLocalPath,
  importProxies,
  upsertAIProvider,
} from "../api";
import type { ProxyCandidate } from "../api";

export type SettingsCategory =
  | "general"
  | "security"
  | "appearance"
  | "terminal"
  | "monitor"
  | "files"
  | "ai"
  | "mcp"
  | "credentials"
  | "proxies"
  | "known_hosts"
  | "migrate"
  | "backup"
  | "shortcuts"
  | "about";

interface Props {
  settings: AppSettings;
  initialTab?: SettingsCategory;
  onChange: (next: AppSettings) => void;
  onOpenTailnetModal: () => void;
  onRefreshHosts: () => void;
  onShowToast?: (title: string, body: string) => void;
  onWallpaperChanged?: () => void;
}

const Switch: React.FC<{ checked: boolean; onChange: (v: boolean) => void }> = ({
  checked,
  onChange,
}) => (
  <button
    type="button"
    onClick={() => onChange(!checked)}
    className={`w-10 h-5 rounded-full transition-colors relative shrink-0 ${
      checked ? "bg-[#3b82f6]" : "bg-[#32353f]"
    }`}
  >
    <span
      className={`absolute top-0.5 w-4 h-4 rounded-full bg-white transition-transform ${
        checked ? "left-[22px]" : "left-0.5"
      }`}
    />
  </button>
);

const NumberStepper: React.FC<{
  value: number;
  step?: number;
  min?: number;
  max?: number;
  onChange: (v: number) => void;
}> = ({ value, step = 1, min = 0, max = 99999, onChange }) => {
  const [draft, setDraft] = useState<string>(String(value));

  useEffect(() => {
    setDraft(String(value));
  }, [value]);

  const commitDraft = () => {
    const parsed = Number(draft);
    if (Number.isNaN(parsed) || draft.trim() === "") {
      setDraft(String(value));
      return;
    }
    const clamped = Math.max(min, Math.min(max, Number(parsed.toFixed(2))));
    setDraft(String(clamped));
    if (clamped !== value) {
      onChange(clamped);
    }
  };

  return (
    <div className="flex items-center gap-1.5 shrink-0">
      <input
        type="text"
        inputMode="decimal"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commitDraft}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.currentTarget.blur();
          }
        }}
        className="w-20 px-2.5 py-1.5 rounded-lg bg-[#25272f] border border-[#323540] focus:border-[#3b82f6] text-white font-mono text-xs outline-none"
      />
      <button
        type="button"
        onClick={() => {
          const next = Math.max(min, Number((value - step).toFixed(2)));
          setDraft(String(next));
          onChange(next);
        }}
        className="w-7 h-7 rounded-lg bg-[#25272f] border border-[#323540] hover:bg-[#2f323c] flex items-center justify-center text-[#d1d5db]"
      >
        <Minus size={12} />
      </button>
      <button
        type="button"
        onClick={() => {
          const next = Math.min(max, Number((value + step).toFixed(2)));
          setDraft(String(next));
          onChange(next);
        }}
        className="w-7 h-7 rounded-lg bg-[#25272f] border border-[#323540] hover:bg-[#2f323c] flex items-center justify-center text-[#d1d5db]"
      >
        <Plus size={12} />
      </button>
    </div>
  );
};

const SegmentedControl = <T extends string>({
  value,
  options,
  onChange,
}: {
  value: T;
  options: readonly (readonly [T, string])[];
  onChange: (v: T) => void;
}) => (
  <div className="p-0.5 rounded-lg bg-[#25272f] border border-[#323540] flex items-center shrink-0">
    {options.map(([val, label]) => (
      <button
        key={val}
        type="button"
        onClick={() => onChange(val)}
        className={`px-3 py-1 rounded-md transition ${
          value === val
            ? "bg-[#383b47] text-white font-medium shadow-sm"
            : "text-[#9ca3af] hover:text-white"
        }`}
      >
        {label}
      </button>
    ))}
  </div>
);

export const SettingsView: React.FC<Props> = ({
  settings,
  initialTab = "terminal",
  onChange,
  onOpenTailnetModal,
  onRefreshHosts,
  onShowToast,
  onWallpaperChanged,
}) => {
  const [tab, setTab] = useState<SettingsCategory>(initialTab);
  const [searchQuery, setSearchQuery] = useState("");

  // Security Master Password & Unlock Modal
  const [masterInput, setMasterInput] = useState("");
  const [showUnlockModal, setShowUnlockModal] = useState(false);
  const [unlockInput, setUnlockInput] = useState("");
  const [unlockErr, setUnlockErr] = useState<string | null>(null);
  const [wallpaperBusy, setWallpaperBusy] = useState(false);
  const [wallpaperPreviewRev, setWallpaperPreviewRev] = useState<number>(() =>
    Date.now()
  );
  const wallpaperInputRef = useRef<HTMLInputElement | null>(null);

  const handleUploadWallpaperBlob = async (f: File) => {
    setWallpaperBusy(true);
    try {
      const next = await uploadWallpaperFile(f, f.name);
      onChange(next);
      setWallpaperPreviewRev(Date.now());
      onWallpaperChanged?.();
      onShowToast?.("壁纸已更新", `已应用背景图片「${f.name}」`);
    } catch (e) {
      alert("上传壁纸失败: " + (e as Error).message);
    } finally {
      setWallpaperBusy(false);
    }
  };

  const handleChooseWallpaperClick = async () => {
    if (wallpaperBusy) return;
    const dlg = await openNativeFileDialog("image", false);
    if (!dlg.supported) {
      wallpaperInputRef.current?.click();
      return;
    }
    if (dlg.canceled || dlg.paths.length === 0) return;
    setWallpaperBusy(true);
    try {
      const next = await uploadWallpaperFromLocalPath(dlg.paths[0]);
      onChange(next);
      setWallpaperPreviewRev(Date.now());
      onWallpaperChanged?.();
      onShowToast?.("壁纸已更新", "高清背景图片已保存并立即生效。");
    } catch (e) {
      alert("上传壁纸失败: " + (e as Error).message);
    } finally {
      setWallpaperBusy(false);
    }
  };

  // Known Hosts
  const [knownHostsPath, setKnownHostsPath] = useState("");
  const [knownHosts, setKnownHosts] = useState<KnownHostEntry[]>([]);

  // Migrate Feedback
  const [migrateStatus, setMigrateStatus] = useState<string | null>(null);

  // Provider Modal State
  const [editingProv, setEditingProv] = useState<Partial<AIProvider> | null>(null);
  const [provApiKey, setProvApiKey] = useState("");
  const [showACPModal, setShowACPModal] = useState(false);

  // Credential Form
  const [credName, setCredName] = useState("");
  const [credKind, setCredKind] = useState("password");
  const [credUser, setCredUser] = useState("root");
  const [credSecret, setCredSecret] = useState("");

  // Proxy pool: paste links to import
  const [prxLinks, setPrxLinks] = useState("");
  const [prxMsg, setPrxMsg] = useState("");
  const [prxCands, setPrxCands] = useState<ProxyCandidate[] | null>(null);
  const [prxPick, setPrxPick] = useState<Set<string>>(new Set());
  const runProxyImport = async (pick?: string[]) => {
    try {
      const res = await importProxies(prxLinks, pick);
      onChange(res.settings);
      if (!pick && res.candidates && res.candidates.length > 0) {
        setPrxCands(res.candidates);
        setPrxPick(new Set());
        setPrxMsg(res.errors.map((x) => x.error).join("；"));
        return;
      }
      setPrxCands(null);
      const done =
        `已导入 ${res.imported} 个` + (res.refreshed ? `，刷新 ${res.refreshed} 个` : "");
      if (res.errors.length === 0) {
        setPrxLinks("");
        setPrxMsg(done);
      } else {
        setPrxMsg(
          done +
            "；" +
            res.errors.map((x) => (x.line ? `第 ${x.line} 行：${x.error}` : x.error)).join("；")
        );
      }
    } catch (e) {
      setPrxMsg((e as Error).message);
    }
  };

  // MCP Form
  const [mcpName, setMcpName] = useState("");
  const [mcpCmd, setMcpCmd] = useState("");

  useEffect(() => {
    setTab(initialTab);
  }, [initialTab]);

  useEffect(() => {
    if (tab === "known_hosts") {
      listKnownHosts()
        .then((res) => {
          setKnownHostsPath(res.path || "");
          setKnownHosts(res.entries || []);
        })
        .catch(() => {});
    }
  }, [tab]);

  const patchSettings = async (mutator: (draft: AppSettings) => void) => {
    const clone: AppSettings = JSON.parse(JSON.stringify(settings));
    mutator(clone);
    onChange(clone);
    const saved = await saveSettings(clone);
    onChange(saved);
  };

  const navGroups: {
    group: string;
    items: {
      id: SettingsCategory;
      label: string;
      keywords: string;
      icon: React.ReactNode;
    }[];
  }[] = [
    {
      group: "偏好",
      items: [
        {
          id: "general",
          label: "通用",
          keywords:
            "语言 记忆布局 右侧栏工具 替换显示 并排显示 续费提醒 通知测试 窗口 托盘 关闭 更新 自动检查 右侧栏显示项 端口转发",
          icon: <Settings size={14} />,
        },
        {
          id: "security",
          label: "安全",
          keywords: "主密码 锁定 解锁 机密 security lock",
          icon: <Lock size={14} />,
        },
        {
          id: "appearance",
          label: "外观",
          keywords: "主题 颜色 纯黑 oled 界面字号 字体 缩放 壁纸 背景 毛玻璃 模糊",
          icon: <Palette size={14} />,
        },
        {
          id: "terminal",
          label: "终端",
          keywords:
            "默认 shell powershell wsl 保活 自动重连 文件夹 终端类型 term 剪贴板 右键 选中即复制 osc 52 大小上限 远程复制 智能补齐 行内补齐 列表补齐 混合补齐 powerline 提示符 灰字",
          icon: <TerminalSquare size={14} />,
        },
        {
          id: "monitor",
          label: "监控",
          keywords: "cpu 内存 状态栏 指标栏 间隔 proc",
          icon: <Activity size={14} />,
        },
        {
          id: "files",
          label: "文件",
          keywords: "sftp 隐藏文件 下载 传输",
          icon: <Folder size={14} />,
        },
        {
          id: "ai",
          label: "AI 助理",
          keywords: "claude acp 脱敏 审核 模型 服务商 openai",
          icon: <Bot size={14} />,
        },
        {
          id: "mcp",
          label: "MCP / Skill",
          keywords: "mcp skill 工具 插件",
          icon: <Plug size={14} />,
        },
      ],
    },
    {
      group: "管理",
      items: [
        {
          id: "credentials",
          label: "凭据",
          keywords: "密码 私钥 保险箱 密钥 dpapi",
          icon: <KeyRound size={14} />,
        },
        {
          id: "proxies",
          label: "代理",
          keywords: "socks5 http tailscale headscale 跳板",
          icon: <Globe size={14} />,
        },
        {
          id: "known_hosts",
          label: "已知主机",
          keywords: "known_hosts 指纹 hostkey sha256 公钥",
          icon: <ShieldCheck size={14} />,
        },
        {
          id: "migrate",
          label: "迁移",
          keywords: "导入 ssh config 迁移",
          icon: <Download size={14} />,
        },
        {
          id: "backup",
          label: "备份",
          keywords: "导出 恢复 json 备份",
          icon: <Archive size={14} />,
        },
      ],
    },
    {
      group: "帮助",
      items: [
        {
          id: "shortcuts",
          label: "快捷键",
          keywords: "热键 键盘 ctrl",
          icon: <Keyboard size={14} />,
        },
        {
          id: "about",
          label: "关于",
          keywords: "版本 开源 架构 shnex",
          icon: <Info size={14} />,
        },
      ],
    },
  ];

  const filteredNavGroups = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    if (!q) return navGroups;
    return navGroups
      .map((g) => ({
        ...g,
        items: g.items.filter(
          (it) =>
            it.label.toLowerCase().includes(q) ||
            it.keywords.toLowerCase().includes(q)
        ),
      }))
      .filter((g) => g.items.length > 0);
  }, [searchQuery]);

  const handleSearchChange = (val: string) => {
    setSearchQuery(val);
    const q = val.trim().toLowerCase();
    if (!q) return;
    for (const g of navGroups) {
      const hit = g.items.find(
        (it) =>
          it.label.toLowerCase().includes(q) ||
          it.keywords.toLowerCase().includes(q)
      );
      if (hit) {
        setTab(hit.id);
        break;
      }
    }
  };

  const handleSaveProvider = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!editingProv) return;
    try {
      const next = await upsertAIProvider({
        id: editingProv.id,
        name: editingProv.name || "openai",
        protocol: editingProv.protocol || "openai",
        base_url: editingProv.base_url || "https://api.openai.com/v1",
        model: editingProv.model || "gpt-4o",
        api_key: provApiKey || undefined,
        is_default: editingProv.is_default ?? settings.ai.providers.length === 0,
      });
      onChange(next);
      setEditingProv(null);
      setProvApiKey("");
    } catch (ex) {
      alert((ex as Error).message);
    }
  };

  const handleExportBackup = async () => {
    const bundle = await exportBackupBundle();
    const blob = new Blob([JSON.stringify(bundle, null, 2)], {
      type: "application/json",
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `shnex-backup-${new Date().toISOString().slice(0, 10)}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const handleImportBackup = (ev: React.ChangeEvent<HTMLInputElement>) => {
    const file = ev.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = async () => {
      try {
        const parsed = JSON.parse(String(reader.result));
        await importBackupBundle(parsed);
        onRefreshHosts();
        alert("配置与主机列表已成功导入！");
      } catch (e) {
        alert("导入失败: " + (e as Error).message);
      }
    };
    reader.readAsText(file);
    ev.target.value = "";
  };

  const handleUnlockSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setUnlockErr(null);
    try {
      const next = await unlockVault(unlockInput);
      onChange(next);
      setUnlockInput("");
      setShowUnlockModal(false);
    } catch (ex) {
      setUnlockErr((ex as Error).message);
    }
  };

  return (
    <div className="flex-1 flex min-h-0 bg-[#191a1d] text-xs select-none">
      {/* Left 15-Category Navigation Sidebar with Search (Matches Screenshot 8!) */}
      <aside className="w-[216px] bg-[#1e1f23] border-r border-[#2a2c33] p-3 flex flex-col gap-3 shrink-0">
        {/* Top Search Box */}
        <div className="relative">
          <Search
            size={13}
            className="absolute left-2.5 top-1/2 -translate-y-1/2 text-[#7c828e]"
          />
          <input
            value={searchQuery}
            onChange={(e) => handleSearchChange(e.target.value)}
            placeholder="搜索设置"
            className="w-full pl-8 pr-2.5 py-1.5 rounded-lg bg-[#27292f] border border-[#32353f] focus:border-[#3b82f6] text-white placeholder:text-[#7c828e] outline-none"
          />
        </div>

        <div className="flex-1 overflow-y-auto space-y-4 pr-0.5">
          {filteredNavGroups.map((g) => (
            <div key={g.group} className="space-y-0.5">
              <div className="px-2 pb-1 text-[11px] font-medium text-[#7c828e]">
                {g.group}
              </div>
              {g.items.map((it) => {
                const active = tab === it.id;
                return (
                  <button
                    key={it.id}
                    onClick={() => setTab(it.id)}
                    className={`w-full px-2.5 py-1.5 rounded-lg flex items-center gap-2.5 transition-colors ${
                      active
                        ? "bg-[#1f3656] text-white font-medium"
                        : "text-[#c9cdd6] hover:bg-[#262830]"
                    }`}
                  >
                    <span
                      className={`shrink-0 ${
                        active ? "text-[#4da2ff]" : "text-[#9ca3af]"
                      }`}
                    >
                      {it.icon}
                    </span>
                    <span>{it.label}</span>
                  </button>
                );
              })}
            </div>
          ))}
        </div>
      </aside>

      {/* Right Main Column (Optional Top Vault Lock Banner + Scrollable Content) */}
      <div className="flex-1 flex flex-col min-w-0 min-h-0">
        {settings.security?.vault_locked && (
          <div className="h-11 px-6 bg-[#1e1f24] border-b border-[#2c2e36] flex items-center justify-between shrink-0">
            <span className="text-[#9ca3af]">已保存的机密内容尚未解锁</span>
            <button
              onClick={() => {
                setUnlockErr(null);
                setUnlockInput("");
                setShowUnlockModal(true);
              }}
              className="px-3.5 py-1.5 rounded-full bg-[#2a2c33] hover:bg-[#343740] border border-[#3a3d47] text-[#e5e7eb] font-medium transition"
            >
              解锁已保存的机密内容
            </button>
          </div>
        )}

        <div className="flex-1 overflow-y-auto px-10 py-7">
          <div className="max-w-[680px] space-y-6">
            {/* ===================== TAB: 通用 (1:1 Match to Screenshots 9-11!) ===================== */}
            {tab === "general" && (
              <div className="space-y-6">
                {/* Header */}
                <div className="flex items-center gap-3.5">
                  <div className="w-9 h-9 rounded-xl bg-[#3b82f6]/15 flex items-center justify-center text-[#3b82f6]">
                    <Settings size={22} />
                  </div>
                  <div>
                    <h1 className="text-lg font-semibold text-white">通用</h1>
                    <p className="text-[#8b909a] mt-0.5">
                      适用于整个 Shnex 的设置：界面语言、布局、提醒、窗口与托盘，以及软件更新。
                    </p>
                  </div>
                </div>

                {/* Card 1: 语言 / 记忆布局 / 右侧栏工具 / 续费提醒 / 通知测试 */}
                <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                  <div className="px-4 py-3.5 flex items-center justify-between">
                    <span className="font-medium text-[#e5e7eb]">语言</span>
                    <select
                      value={settings.general?.language || "system"}
                      onChange={(e) =>
                        patchSettings((d) => {
                          d.general.language = e.target.value;
                        })
                      }
                      className="w-44 px-3 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white outline-none"
                    >
                      <option value="system">跟随系统</option>
                      <option value="zh-CN">简体中文</option>
                      <option value="en-US">English</option>
                    </select>
                  </div>

                  <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                    <div>
                      <div className="font-medium text-[#e5e7eb]">记忆布局</div>
                      <div className="text-[11px] text-[#7c828e] mt-0.5 leading-relaxed">
                        下次启动恢复窗口位置和大小、标签页与分屏并重新连接，不恢复会话内容；关闭后清除已保存布局
                      </div>
                    </div>
                    <Switch
                      checked={settings.general?.remember_layout ?? false}
                      onChange={(v) => {
                        if (!v) {
                          localStorage.removeItem("shnex_saved_layout");
                        }
                        patchSettings((d) => {
                          d.general.remember_layout = v;
                        });
                      }}
                    />
                  </div>

                  <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                    <div>
                      <div className="font-medium text-[#e5e7eb]">
                        右侧栏工具
                      </div>
                      <div className="text-[11px] text-[#7c828e] mt-0.5 leading-relaxed">
                        替换显示一次只显示一个工具，在原位置切换；并排显示会保留每个打开的工具，超过
                        3 个或宽度不够时，在已打开的工具下方打开
                      </div>
                    </div>
                    <SegmentedControl
                      value={settings.general?.right_bar_mode || "replace"}
                      options={
                        [
                          ["replace", "替换显示"],
                          ["side_by_side", "并排显示"],
                        ] as const
                      }
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.general.right_bar_mode = v;
                        })
                      }
                    />
                  </div>

                  <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                    <div>
                      <div className="font-medium text-[#e5e7eb]">
                        续费提醒通知
                      </div>
                      <div className="text-[11px] text-[#7c828e] mt-0.5">
                        在主机的订阅续费日前，按该主机设置的提前天数发送系统通知。
                      </div>
                    </div>
                    <Switch
                      checked={
                        settings.general?.billing_reminder_enabled ?? true
                      }
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.general.billing_reminder_enabled = v;
                        })
                      }
                    />
                  </div>

                  <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                    <div>
                      <div className="font-medium text-[#e5e7eb]">通知测试</div>
                      <div className="text-[11px] text-[#7c828e] mt-0.5">
                        发送一条系统通知，确认能否收到。
                      </div>
                    </div>
                    <button
                      type="button"
                      onClick={async () => {
                        const res = await sendTestNotification();
                        onShowToast?.(
                          res.notification.title,
                          res.notification.body
                        );
                        if (
                          "Notification" in window &&
                          Notification.permission === "granted"
                        ) {
                          new Notification(res.notification.title, {
                            body: res.notification.body,
                          });
                        } else if (
                          "Notification" in window &&
                          Notification.permission !== "denied"
                        ) {
                          Notification.requestPermission().then((perm) => {
                            if (perm === "granted") {
                              new Notification(res.notification.title, {
                                body: res.notification.body,
                              });
                            }
                          });
                        }
                      }}
                      className="px-3.5 py-1.5 rounded-full bg-[#2a2c34] hover:bg-[#343741] border border-[#3a3d47] text-[#e5e7eb] font-medium shrink-0 transition"
                    >
                      发送测试通知
                    </button>
                  </div>
                </div>

                {/* Section 2: 窗口与托盘 */}
                <div className="space-y-2.5">
                  <div className="flex items-center gap-2 font-semibold text-[#e5e7eb] text-[13px]">
                    <AppWindow size={15} className="text-[#3b82f6]" />
                    <span>窗口与托盘</span>
                  </div>

                  <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">
                          关闭窗口时
                        </div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5">
                          隐藏到托盘后，会话和后台服务继续运行
                        </div>
                      </div>
                      <select
                        value={settings.general?.close_action || "tray"}
                        onChange={(e) =>
                          patchSettings((d) => {
                            d.general.close_action = e.target.value as
                              | "tray"
                              | "quit";
                          })
                        }
                        className="w-44 px-3 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white outline-none shrink-0"
                      >
                        <option value="tray">隐藏到系统托盘</option>
                        <option value="quit">直接退出程序</option>
                      </select>
                    </div>

                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">
                          托盘图标
                        </div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5">
                          选择 Shnex 标志的单色或彩色版本
                        </div>
                      </div>
                      <select
                        value={settings.general?.tray_icon_style || "color"}
                        onChange={(e) => {
                          const mode = e.target.value as "color" | "mono";
                          setTrayStyle(mode);
                          patchSettings((d) => {
                            d.general.tray_icon_style = mode;
                          });
                        }}
                        className="w-44 px-3 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white outline-none shrink-0"
                      >
                        <option value="color">彩色标志</option>
                        <option value="mono">单色标志</option>
                      </select>
                    </div>

                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">
                          点击托盘图标时
                        </div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5">
                          选择点击托盘图标后打开主机弹窗还是主窗口
                        </div>
                      </div>
                      <select
                        value={settings.general?.tray_click_action || "popup"}
                        onChange={(e) =>
                          patchSettings((d) => {
                            d.general.tray_click_action = e.target.value as
                              | "popup"
                              | "main_window";
                          })
                        }
                        className="w-44 px-3 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white outline-none shrink-0"
                      >
                        <option value="popup">主机弹窗</option>
                        <option value="main_window">主窗口</option>
                      </select>
                    </div>

                    <div className="px-4 py-3.5 flex items-center justify-between gap-4 opacity-70">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">
                          托盘弹窗材质
                        </div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5">
                          半透明效果仅支持 macOS。
                        </div>
                      </div>
                      <div className="p-0.5 rounded-lg bg-[#25272f] border border-[#323540] flex items-center shrink-0 pointer-events-none">
                        <span className="px-3 py-1 text-[#7c828e]">半透明</span>
                        <span className="px-3 py-1 rounded-md bg-[#31343e] text-[#b0b5c0]">
                          不透明
                        </span>
                      </div>
                    </div>
                  </div>
                </div>

                {/* Section 3: 更新 */}
                <div className="space-y-2.5">
                  <div className="flex items-center gap-2 font-semibold text-[#e5e7eb] text-[13px]">
                    <Download size={15} className="text-[#3b82f6]" />
                    <span>更新</span>
                  </div>

                  <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                    <div className="px-4 py-3.5 flex items-center justify-between">
                      <span className="font-medium text-[#e5e7eb]">
                        自动检查更新
                      </span>
                      <Switch
                        checked={settings.general?.auto_check_update ?? true}
                        onChange={(v) =>
                          patchSettings((d) => {
                            d.general.auto_check_update = v;
                          })
                        }
                      />
                    </div>

                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">
                          检查频率
                        </div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5">
                          重启 Shnex 后生效。
                        </div>
                      </div>
                      <SegmentedControl
                        value={String(
                          settings.general?.update_interval_hours || 1
                        )}
                        options={
                          [
                            ["1", "1 小时"],
                            ["2", "2 小时"],
                            ["6", "6 小时"],
                            ["12", "12 小时"],
                            ["24", "24 小时"],
                          ] as const
                        }
                        onChange={(v) =>
                          patchSettings((d) => {
                            d.general.update_interval_hours = Number(v);
                          })
                        }
                      />
                    </div>

                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">代理</div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5">
                          系统代理跟随系统的网络设置，包括 PAC 自动配置。
                        </div>
                      </div>
                      <select
                        value={settings.general?.update_proxy || "system"}
                        onChange={(e) =>
                          patchSettings((d) => {
                            d.general.update_proxy = e.target.value;
                          })
                        }
                        className="w-44 px-3 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white outline-none shrink-0"
                      >
                        <option value="system">系统代理</option>
                        <option value="direct">直连 (不使用代理)</option>
                        {settings.proxies.map((p) => (
                          <option key={p.id} value={p.id}>
                            {p.name}
                          </option>
                        ))}
                      </select>
                    </div>
                  </div>
                </div>

                {/* Section 4: 右侧栏显示项 (All 14 Items from Screenshots 10-11!) */}
                <div className="space-y-2.5">
                  <div className="flex items-center gap-2 font-semibold text-[#e5e7eb] text-[13px]">
                    <PanelRight size={15} className="text-[#3b82f6]" />
                    <span>右侧栏显示项</span>
                  </div>

                  <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                    {(
                      [
                        ["monitor", "系统监控", <Activity size={14} />],
                        ["processes", "占用", <AlignLeft size={14} />],
                        ["host_info", "主机详情", <Info size={14} />],
                        ["files", "文件", <Folder size={14} />],
                        ["docker", "Docker 管理", <Container size={14} />],
                        ["ai", "AI助理", <Bot size={14} />],
                        [
                          "port_forward",
                          "新建端口转发",
                          <ArrowLeftRight size={14} />,
                        ],
                        ["snippets", "快捷指令", <Code2 size={14} />],
                        ["cmd_history", "历史指令", <History size={14} />],
                        ["broadcast", "广播指令", <Radio size={14} />],
                        ["session_logs", "会话日志", <FileText size={14} />],
                        ["quick_connect", "快速连接", <Zap size={14} />],
                        [
                          "import_ssh",
                          "从 SSH 配置导入",
                          <Download size={14} />,
                        ],
                      ] as [keyof RightRailVisibility, string, React.ReactNode][]
                    ).map(([key, label, iconNode]) => (
                      <div
                        key={key}
                        className="px-4 py-3 flex items-center justify-between"
                      >
                        <div className="flex items-center gap-3 text-[#e5e7eb]">
                          <span className="text-[#8b909a]">{iconNode}</span>
                          <span className="font-medium">{label}</span>
                        </div>
                        <Switch
                          checked={
                            settings.general?.right_rail_items?.[key] ?? true
                          }
                          onChange={(v) =>
                            patchSettings((d) => {
                              if (!d.general.right_rail_items) {
                                d.general.right_rail_items = {
                                  monitor: true,
                                  processes: true,
                                  host_info: true,
                                  files: true,
                                  docker: true,
                                  ai: true,
                                  port_forward: true,
                                  snippets: true,
                                  cmd_history: true,
                                  broadcast: true,
                                  session_logs: true,
                                  quick_connect: true,
                                  import_ssh: true,
                                };
                              }
                              d.general.right_rail_items[key] = v;
                            })
                          }
                        />
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            )}

            {/* ===================== TAB: 安全 ===================== */}
            {tab === "security" && (
              <div className="space-y-4">
                <div>
                  <h1 className="text-lg font-semibold text-white">安全</h1>
                  <p className="text-[#8b909a] mt-0.5">
                    管理凭据保险箱的主密码锁定保护与会话安全策略。
                  </p>
                </div>

                <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                  <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                    <div>
                      <div className="font-medium text-[#e5e7eb]">
                        当前机密保险箱状态
                      </div>
                      <div className="text-[11px] text-[#7c828e] mt-0.5">
                        {settings.security?.vault_locked
                          ? "当前已锁定，需验证主密码后解锁。"
                          : "当前处于已解锁可用状态（底层受 Windows DPAPI 保护）。"}
                      </div>
                    </div>
                    {settings.security?.vault_locked ? (
                      <button
                        onClick={() => setShowUnlockModal(true)}
                        className="px-3.5 py-1.5 rounded-lg bg-[#2563eb] text-white font-medium flex items-center gap-1.5 shrink-0"
                      >
                        <Unlock size={13} />
                        <span>立即解锁</span>
                      </button>
                    ) : (
                      <button
                        onClick={async () => onChange(await lockVault())}
                        className="px-3.5 py-1.5 rounded-lg bg-[#272931] border border-[#363945] hover:bg-[#31343e] text-[#e5e7eb] flex items-center gap-1.5 shrink-0"
                      >
                        <Lock size={13} />
                        <span>立即锁定机密内容</span>
                      </button>
                    )}
                  </div>

                  <div className="px-4 py-3.5 space-y-2.5">
                    <div className="font-medium text-[#e5e7eb]">
                      {settings.security?.master_lock_enabled
                        ? "更新或清除主密码"
                        : "设置启动主密码"}
                    </div>
                    <div className="text-[11px] text-[#7c828e]">
                      设置主密码后，每次启动软件时已保存的机密内容默认保持锁定状态。留空保存可关闭主密码。
                    </div>
                    <div className="flex items-center gap-2">
                      <input
                        type="password"
                        value={masterInput}
                        onChange={(e) => setMasterInput(e.target.value)}
                        placeholder={
                          settings.security?.master_lock_enabled
                            ? "输入新主密码（留空则停用主密码锁）"
                            : "输入要启用的主密码..."
                        }
                        className="flex-1 px-3 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white outline-none"
                      />
                      <button
                        onClick={async () => {
                          const next = await setMasterPassword(masterInput);
                          onChange(next);
                          setMasterInput("");
                          alert(
                            next.security.master_lock_enabled
                              ? "主密码已启用！"
                              : "已关闭主密码锁定。"
                          );
                        }}
                        className="px-4 py-1.5 rounded-lg bg-[#2563eb] hover:bg-[#3b82f6] text-white font-medium"
                      >
                        保存
                      </button>
                    </div>
                  </div>

                  <div className="px-4 py-3.5 flex items-center justify-between">
                    <div>
                      <div className="font-medium text-[#e5e7eb]">
                        空闲自动锁定 (分钟)
                      </div>
                      <div className="text-[11px] text-[#7c828e] mt-0.5">
                        设为 0 表示运行期间不自动锁定。
                      </div>
                    </div>
                    <NumberStepper
                      value={settings.security?.auto_lock_minutes ?? 30}
                      step={5}
                      min={0}
                      max={480}
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.security.auto_lock_minutes = v;
                        })
                      }
                    />
                  </div>
                </div>
              </div>
            )}

            {/* ===================== TAB: 终端 (1:1 Match to Screenshot 8!) ===================== */}
            {tab === "terminal" && (
              <div className="space-y-6">
                {/* Top Card: Typography & Default Local Shell */}
                <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                  <div className="px-4 py-3.5 flex items-center justify-between">
                    <span className="font-medium text-[#e5e7eb]">终端字号</span>
                    <NumberStepper
                      value={settings.terminal.font_size}
                      step={0.5}
                      min={9}
                      max={36}
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.terminal.font_size = v;
                        })
                      }
                    />
                  </div>

                  <div className="px-4 py-3.5 flex items-center justify-between">
                    <span className="font-medium text-[#e5e7eb]">光标样式</span>
                    <select
                      value={settings.terminal.cursor_style}
                      onChange={(e) =>
                        patchSettings((d) => {
                          d.terminal.cursor_style = e.target.value as
                            | "block"
                            | "bar"
                            | "underline";
                        })
                      }
                      className="w-48 px-3 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white outline-none"
                    >
                      <option value="block">实心方块 (Block)</option>
                      <option value="bar">竖线 (Bar)</option>
                      <option value="underline">下划线 (Underline)</option>
                    </select>
                  </div>

                  <div className="px-4 py-3.5 flex items-center justify-between">
                    <span className="font-medium text-[#e5e7eb]">光标闪烁</span>
                    <Switch
                      checked={settings.terminal.cursor_blink}
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.terminal.cursor_blink = v;
                        })
                      }
                    />
                  </div>

                  <div className="px-4 py-3.5 flex items-center justify-between">
                    <span className="font-medium text-[#e5e7eb]">
                      回滚缓存行数
                    </span>
                    <NumberStepper
                      value={settings.terminal.scrollback}
                      step={1000}
                      min={500}
                      max={50000}
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.terminal.scrollback = v;
                        })
                      }
                    />
                  </div>

                  <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                    <div>
                      <div className="font-medium text-[#e5e7eb]">默认 Shell</div>
                      <div className="text-[11px] text-[#7c828e] mt-0.5">
                        新建本地终端时运行的 shell，其他 shell 可在本地终端菜单中选择。
                      </div>
                    </div>
                      <select
                        value={settings.terminal.default_shell || "powershell"}
                        onChange={(e) =>
                          patchSettings((d) => {
                            d.terminal.default_shell = e.target.value;
                          })
                        }
                        className="w-48 px-3 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white outline-none shrink-0"
                      >
                        <option value="powershell">Windows PowerShell</option>
                        <option value="pwsh">PowerShell 7 (pwsh)</option>
                        <option value="cmd">命令提示符 (CMD)</option>
                        <option value="wsl">WSL (Linux 子系统)</option>
                        <option value="gitbash">Git Bash</option>
                      </select>
                    </div>
                </div>

                {/* Section: 智能补齐与提示符 */}
                <div className="space-y-2.5">
                  <div className="flex items-center gap-2 font-semibold text-[#e5e7eb] text-[13px]">
                    <Sparkles size={15} className="text-[#3b82f6]" />
                    <span>智能补齐</span>
                  </div>

                  <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">
                          启用内置智能补齐
                        </div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5 leading-relaxed">
                          从当前 Git 仓库、项目脚本、SSH 配置、WSL 发行版与窗格文件系统实时获取候选，无需安装 Shell 插件或发起 AI 请求，历史仅用于对已发现候选排序。
                        </div>
                      </div>
                      <Switch
                        checked={settings.terminal.completion_enabled ?? true}
                        onChange={(v) =>
                          patchSettings((d) => {
                            d.terminal.completion_enabled = v;
                          })
                        }
                      />
                    </div>

                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">
                          补齐模式
                        </div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5 leading-relaxed">
                          行内补齐按 Tab 接受灰字；列表补齐按 Tab 接受选中候选；混合补齐下按 → 直接接受灰字，按 Tab 打开或轮换候选列表，Enter 插入选中项但不执行命令。
                        </div>
                      </div>
                      <SegmentedControl
                        value={settings.terminal.completion_mode || "hybrid"}
                        options={
                          [
                            ["inline", "行内补齐"],
                            ["list", "列表补齐"],
                            ["hybrid", "混合补齐"],
                          ] as const
                        }
                        onChange={(v) =>
                          patchSettings((d) => {
                            d.terminal.completion_mode = v;
                          })
                        }
                      />
                    </div>

                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">
                          内置 Powerline 提示符
                        </div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5 leading-relaxed">
                          新建本地 PowerShell 终端时启用内置 TrueColor Powerline 提示符（显示目录与 Git 分支）并实时同步当前目录。
                        </div>
                      </div>
                      <Switch
                        checked={settings.terminal.builtin_powerline ?? true}
                        onChange={(v) =>
                          patchSettings((d) => {
                            d.terminal.builtin_powerline = v;
                          })
                        }
                      />
                    </div>
                  </div>
                </div>

                {/* Section: 连接 (Matches Screenshot 8!) */}
                <div className="space-y-2.5">
                  <div className="flex items-center gap-2 font-semibold text-[#e5e7eb] text-[13px]">
                    <RefreshCw size={15} className="text-[#3b82f6]" />
                    <span>连接</span>
                  </div>

                  <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                    <div className="px-4 py-3.5 flex items-center justify-between">
                      <span className="font-medium text-[#e5e7eb]">
                        保活间隔 (秒)
                      </span>
                      <NumberStepper
                        value={settings.terminal.keepalive_sec ?? 30}
                        step={5}
                        min={0}
                        max={600}
                        onChange={(v) =>
                          patchSettings((d) => {
                            d.terminal.keepalive_sec = v;
                          })
                        }
                      />
                    </div>

                    <div className="px-4 py-3.5 flex items-center justify-between">
                      <span className="font-medium text-[#e5e7eb]">自动重连</span>
                      <Switch
                        checked={settings.terminal.auto_reconnect ?? true}
                        onChange={(v) =>
                          patchSettings((d) => {
                            d.terminal.auto_reconnect = v;
                          })
                        }
                      />
                    </div>

                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">
                          重新连接后回到之前的文件夹
                        </div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5 leading-relaxed">
                          SSH 终端重新连接后，新的 shell 会回到旧 shell
                          报告的文件夹。附加 tmux 会话时不会切换。
                        </div>
                      </div>
                      <Switch
                        checked={
                          settings.terminal.restore_dir_on_reconnect ?? true
                        }
                        onChange={(v) =>
                          patchSettings((d) => {
                            d.terminal.restore_dir_on_reconnect = v;
                          })
                        }
                      />
                    </div>

                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">终端类型</div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5 leading-relaxed">
                          新建终端报告的 TERM，程序据此选择按键和颜色。选择 linux
                          时，F1–F5、Home 和 End 按 Linux 控制台的方式发送。
                        </div>
                      </div>
                      <select
                        value={settings.terminal.term_type || "xterm-256color"}
                        onChange={(e) =>
                          patchSettings((d) => {
                            d.terminal.term_type = e.target.value;
                          })
                        }
                        className="w-44 px-3 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white outline-none shrink-0"
                      >
                        <option value="xterm-256color">xterm-256color</option>
                        <option value="xterm">xterm</option>
                        <option value="tmux-256color">tmux-256color</option>
                        <option value="vt100">vt100</option>
                        <option value="vt220">vt220</option>
                        <option value="linux">linux</option>
                      </select>
                    </div>
                  </div>
                </div>

                {/* Section: 剪贴板 (Matches Screenshot 8!) */}
                <div className="space-y-2.5">
                  <div className="flex items-center gap-2 font-semibold text-[#e5e7eb] text-[13px]">
                    <Clipboard size={15} className="text-[#3b82f6]" />
                    <span>剪贴板</span>
                  </div>

                  <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">右键</div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5 leading-relaxed">
                          「复制或粘贴」在有选中文本时复制，否则粘贴。按住 Shift
                          右键始终打开菜单。
                        </div>
                      </div>
                      <SegmentedControl
                        value={settings.terminal.right_click_action || "menu"}
                        options={
                          [
                            ["menu", "菜单"],
                            ["paste", "粘贴"],
                            ["copy_or_paste", "复制或粘贴"],
                          ] as const
                        }
                        onChange={(v) =>
                          patchSettings((d) => {
                            d.terminal.right_click_action = v;
                          })
                        }
                      />
                    </div>

                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">
                          选中即复制
                        </div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5">
                          在终端中选中的文本会自动复制到剪贴板
                        </div>
                      </div>
                      <Switch
                        checked={settings.terminal.copy_on_select ?? false}
                        onChange={(v) =>
                          patchSettings((d) => {
                            d.terminal.copy_on_select = v;
                          })
                        }
                      />
                    </div>

                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">
                          远程剪贴板 (OSC 52)
                        </div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5">
                          远端读取剪贴板始终被拒绝。
                        </div>
                      </div>
                      <SegmentedControl
                        value={settings.terminal.osc52_mode || "ask"}
                        options={
                          [
                            ["allow", "允许"],
                            ["ask", "询问"],
                            ["deny", "禁止"],
                          ] as const
                        }
                        onChange={(v) =>
                          patchSettings((d) => {
                            d.terminal.osc52_mode = v;
                          })
                        }
                      />
                    </div>

                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">
                          大小上限 (KiB)
                        </div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5">
                          64–8192。超出的复制会被丢弃。
                        </div>
                      </div>
                      <NumberStepper
                        value={settings.terminal.osc52_max_kib ?? 1024}
                        step={128}
                        min={64}
                        max={8192}
                        onChange={(v) =>
                          patchSettings((d) => {
                            d.terminal.osc52_max_kib = v;
                          })
                        }
                      />
                    </div>

                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">
                          远程复制时提示
                        </div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5">
                          显示写入来源并可撤销。关闭后静默复制，不再弹出提示。
                        </div>
                      </div>
                      <Switch
                        checked={settings.terminal.osc52_notify ?? true}
                        onChange={(v) =>
                          patchSettings((d) => {
                            d.terminal.osc52_notify = v;
                          })
                        }
                      />
                    </div>
                  </div>
                </div>
              </div>
            )}

            {/* ===================== TAB: AI 助理 ===================== */}
            {tab === "ai" && (
              <>
                <div className="flex items-center gap-3.5">
                  <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-[#a855f7] to-[#7e22ce] flex items-center justify-center text-white shadow-lg">
                    <Bot size={22} />
                  </div>
                  <div>
                    <h1 className="text-lg font-semibold text-white">AI 助理</h1>
                    <p className="text-[#8b909a] mt-0.5">
                      AI 助手的代理、权限和模型服务商。
                    </p>
                  </div>
                </div>

                <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                  <div className="px-4 py-3.5 flex items-center justify-between">
                    <span className="font-medium text-[#e5e7eb]">默认代理</span>
                    <select
                      value={settings.ai.default_agent}
                      onChange={(e) =>
                        patchSettings((d) => {
                          d.ai.default_agent = e.target.value;
                        })
                      }
                      className="w-44 px-3 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white outline-none"
                    >
                      <option value="builtin">内置</option>
                      <option value="claude_cli">远端 Claude CLI 接力</option>
                      <option value="acp">外部本机 ACP</option>
                    </select>
                  </div>

                  <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                    <div>
                      <div className="font-medium text-[#e5e7eb]">外部 ACP</div>
                      <div className="text-[11px] text-[#7c828e] mt-0.5">
                        使用本机已安装、支持 ACP 的程序，发送消息前请在该程序中完成登录
                      </div>
                    </div>
                    <button
                      onClick={() => setShowACPModal((v) => !v)}
                      className="px-3.5 py-1.5 rounded-lg bg-[#272931] border border-[#353945] hover:bg-[#2f323d] text-[#e5e7eb] shrink-0"
                    >
                      配置 ACP 智能体
                    </button>
                  </div>

                  {showACPModal && (
                    <div className="px-4 py-3 bg-[#17181c] flex items-center gap-2">
                      <span className="text-[#8b909a]">本机命令:</span>
                      <input
                        value={settings.ai.acp_command}
                        onChange={(e) =>
                          patchSettings((d) => {
                            d.ai.acp_command = e.target.value;
                          })
                        }
                        placeholder="claude --acp"
                        className="flex-1 px-2.5 py-1 rounded bg-[#23252c] border border-[#323540] font-mono text-white outline-none"
                      />
                    </div>
                  )}

                  <div className="px-4 py-3.5 flex items-center justify-between">
                    <span className="font-medium text-[#e5e7eb]">
                      终端上下文行数
                    </span>
                    <NumberStepper
                      value={settings.ai.context_lines}
                      step={50}
                      min={0}
                      max={2000}
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.ai.context_lines = v;
                        })
                      }
                    />
                  </div>

                  <div className="px-4 py-3.5 flex items-center justify-between">
                    <span className="font-medium text-[#e5e7eb]">
                      审计保留天数
                    </span>
                    <NumberStepper
                      value={settings.ai.audit_retention_days}
                      step={15}
                      min={0}
                      max={3650}
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.ai.audit_retention_days = v;
                        })
                      }
                    />
                  </div>

                  <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                    <div>
                      <div className="font-medium text-[#e5e7eb]">脱敏</div>
                      <div className="text-[11px] text-[#7c828e] mt-0.5">
                        在发送给模型之前，遮蔽疑似私钥和令牌。
                      </div>
                    </div>
                    <Switch
                      checked={settings.ai.redact_secrets}
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.ai.redact_secrets = v;
                        })
                      }
                    />
                  </div>
                </div>

                <div className="space-y-2.5">
                  <div className="flex items-center gap-2 font-medium text-[#e5e7eb]">
                    <span className="w-5 h-5 rounded-md bg-[#22c55e] text-white flex items-center justify-center">
                      <ShieldCheck size={13} />
                    </span>
                    <span>已确认发送数据的主机</span>
                  </div>

                  <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                    {(!settings.ai.confirmed_hosts ||
                      settings.ai.confirmed_hosts.length === 0) && (
                      <div className="px-4 py-3.5 text-[#7c828e]">
                        暂无已授权发送终端上下文的主机（首次在主机 AI 面板提问时将提示确认）
                      </div>
                    )}
                    {(settings.ai.confirmed_hosts || []).map((hName) => (
                      <div
                        key={hName}
                        className="px-4 py-3 flex items-center justify-between"
                      >
                        <span className="font-medium text-white">{hName}</span>
                        <button
                          onClick={() =>
                            patchSettings((d) => {
                              d.ai.confirmed_hosts = d.ai.confirmed_hosts.filter(
                                (x) => x !== hName
                              );
                            })
                          }
                          className="p-1.5 rounded hover:bg-[#2c2f3a] text-[#8b909a] hover:text-[#f87171]"
                          title="撤销此主机的数据发送授权"
                        >
                          <Trash2 size={14} />
                        </button>
                      </div>
                    ))}
                  </div>
                </div>

                <div className="space-y-2.5">
                  <div className="flex items-center gap-2 font-medium text-[#e5e7eb]">
                    <span className="w-5 h-5 rounded-md bg-[#f59e0b] text-white flex items-center justify-center">
                      <ShieldEllipsis size={13} />
                    </span>
                    <span>权限审批</span>
                  </div>

                  <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                    <div className="px-4 py-3.5 flex items-center justify-between">
                      <span className="font-medium text-[#e5e7eb]">默认权限</span>
                      <SegmentedControl
                        value={settings.ai.default_permission}
                        options={
                          [
                            ["ask", "询问"],
                            ["auto_review", "自动审核"],
                            ["allow_all", "完全允许"],
                          ] as const
                        }
                        onChange={(v) =>
                          patchSettings((d) => {
                            d.ai.default_permission = v;
                          })
                        }
                      />
                    </div>

                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">审核模型</div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5 leading-relaxed">
                          在"自动审核"下，每个操作均交给所选模型审核；模型允许才执行，要求确认或审核失败时询问你
                        </div>
                      </div>
                      <select
                        value={settings.ai.review_provider_id}
                        onChange={(e) =>
                          patchSettings((d) => {
                            d.ai.review_provider_id = e.target.value;
                          })
                        }
                        className="w-52 px-3 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white outline-none shrink-0"
                      >
                        {settings.ai.providers.length === 0 ? (
                          <option value="">请先添加下方模型提供商</option>
                        ) : (
                          settings.ai.providers.map((p) => (
                            <option key={p.id} value={p.id}>
                              {p.name} - {p.model}
                            </option>
                          ))
                        )}
                      </select>
                    </div>

                    <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">严格程度</div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5">
                          决策模型判定操作安全所需的把握：宽松 80%、标准 90%、严格 97%。
                        </div>
                      </div>
                      <SegmentedControl
                        value={settings.ai.review_strictness}
                        options={
                          [
                            ["loose", "宽松"],
                            ["standard", "标准"],
                            ["strict", "严格"],
                          ] as const
                        }
                        onChange={(v) =>
                          patchSettings((d) => {
                            d.ai.review_strictness = v;
                          })
                        }
                      />
                    </div>
                  </div>
                </div>

                <div className="space-y-2.5">
                  <div className="flex items-center gap-2 font-medium text-[#e5e7eb]">
                    <span className="w-5 h-5 rounded-md bg-[#a855f7] text-white flex items-center justify-center">
                      <Sparkles size={13} />
                    </span>
                    <span>模型服务商</span>
                  </div>

                  <div className="rounded-xl bg-[#202126] border border-[#2d3039] p-4 space-y-3">
                    {settings.ai.providers.length > 0 && (
                      <div className="space-y-2">
                        {settings.ai.providers.map((p) => (
                          <div
                            key={p.id}
                            className="px-3.5 py-2.5 rounded-xl bg-[#262830] border border-[#323541] flex items-center justify-between"
                          >
                            <div className="flex items-center gap-3 min-w-0">
                              <div className="w-7 h-7 rounded-full bg-[#18191f] border border-[#323540] flex items-center justify-center font-semibold text-white">
                                {p.name.slice(0, 1).toUpperCase()}
                              </div>
                              <div className="min-w-0">
                                <div className="font-medium text-white truncate">
                                  {p.name} - {p.model}
                                </div>
                                <div className="text-[11px] text-[#7c828e] font-mono truncate">
                                  {p.base_url}
                                </div>
                              </div>
                            </div>

                            <div className="flex items-center gap-1.5 shrink-0">
                              <button
                                onClick={async () =>
                                  onChange(await setDefaultAIProvider(p.id))
                                }
                                className={`p-1.5 rounded-lg ${
                                  p.is_default
                                    ? "bg-[#1e3a5f] text-[#60a5fa]"
                                    : "text-[#7c828e] hover:bg-[#2f323d] hover:text-white"
                                }`}
                                title="设为默认服务商"
                              >
                                <Star
                                  size={13}
                                  fill={p.is_default ? "currentColor" : "none"}
                                />
                              </button>
                              <button
                                onClick={() => {
                                  setEditingProv(p);
                                  setProvApiKey("");
                                }}
                                className="p-1.5 rounded-lg text-[#8b909a] hover:bg-[#2f323d] hover:text-white"
                                title="编辑服务商"
                              >
                                <Pencil size={13} />
                              </button>
                              <button
                                onClick={async () =>
                                  onChange(await deleteAIProvider(p.id))
                                }
                                className="p-1.5 rounded-lg text-[#8b909a] hover:bg-[#ef4444]/20 hover:text-[#f87171]"
                                title="删除"
                              >
                                <Trash2 size={13} />
                              </button>
                            </div>
                          </div>
                        ))}
                      </div>
                    )}

                    <button
                      onClick={() => {
                        setEditingProv({
                          name: "openai",
                          protocol: "openai",
                          base_url: "https://api.openai.com/v1",
                          model: "gpt-4o",
                          is_default: settings.ai.providers.length === 0,
                        });
                        setProvApiKey("");
                      }}
                      className="px-4 py-1.5 rounded-full bg-[#2563eb] hover:bg-[#3b82f6] text-white font-medium transition"
                    >
                      添加提供商
                    </button>
                  </div>
                </div>
              </>
            )}

            {/* ===================== TAB: 外观 ===================== */}
            {tab === "appearance" && (
              <div className="space-y-4">
                <h1 className="text-lg font-semibold text-white">外观</h1>
                <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                  <div className="px-4 py-3.5 flex items-center justify-between">
                    <span>色彩主题</span>
                    <select
                      value={settings.appearance.theme}
                      onChange={(e) =>
                        patchSettings((d) => {
                          d.appearance.theme = e.target.value;
                        })
                      }
                      className="w-44 px-3 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white outline-none"
                    >
                      <option value="dark">深邃暗色 (默认)</option>
                      <option value="oled">纯黑 OLED</option>
                      <option value="slate">柔和岩灰 (Slate)</option>
                    </select>
                  </div>
                  <div className="px-4 py-3.5 flex items-center justify-between">
                    <div>
                      <div>界面字号 (px)</div>
                      <div className="text-[11px] text-[#7c828e] mt-0.5">
                        支持 11 ~ 24 px（输入数字后按回车或点击空白处立即缩放全局界面）
                      </div>
                    </div>
                    <NumberStepper
                      value={settings.appearance.ui_font_size}
                      min={11}
                      max={24}
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.appearance.ui_font_size = v;
                        })
                      }
                    />
                  </div>

                  {/* Custom Wallpaper & Frosted Glass Controls */}
                  <div
                    onDragOver={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                    }}
                    onDrop={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      const dropped = e.dataTransfer.files?.[0];
                      if (dropped) {
                        handleUploadWallpaperBlob(dropped);
                      }
                    }}
                    className="px-4 py-3.5 space-y-3"
                  >
                    <div className="flex items-center justify-between gap-4">
                      <div>
                        <div className="font-medium text-[#e5e7eb]">
                          连接大厅背景壁纸
                        </div>
                        <div className="text-[11px] text-[#7c828e] mt-0.5">
                          支持选择或直接拖入 PNG / JPG / WebP / AVIF / SVG 高清壁纸（最高 64 MiB）。
                        </div>
                      </div>
                      <div className="flex items-center gap-2 shrink-0">
                        <input
                          ref={wallpaperInputRef}
                          type="file"
                          accept="image/*"
                          className="hidden"
                          onChange={(ev) => {
                            const f = ev.target.files?.[0];
                            ev.target.value = "";
                            if (f) {
                              handleUploadWallpaperBlob(f);
                            }
                          }}
                        />
                        <button
                          type="button"
                          disabled={wallpaperBusy}
                          onClick={handleChooseWallpaperClick}
                          className="px-3.5 py-1.5 rounded-lg bg-[#2563eb] hover:bg-[#3b82f6] disabled:opacity-50 text-white font-medium transition"
                        >
                          {wallpaperBusy ? "正在导入..." : "选择图片..."}
                        </button>
                        {settings.appearance.has_wallpaper && (
                          <button
                            type="button"
                            onClick={async () => {
                              const next = await clearWallpaper();
                              onChange(next);
                              setWallpaperPreviewRev(Date.now());
                              onWallpaperChanged?.();
                            }}
                            className="px-3 py-1.5 rounded-lg bg-[#272931] border border-[#363945] hover:bg-[#323540] text-[#d1d5db]"
                          >
                            清除壁纸
                          </button>
                        )}
                      </div>
                    </div>

                    {settings.appearance.has_wallpaper && (
                      <div className="relative h-28 rounded-xl overflow-hidden border border-white/10">
                        <img
                          src={buildWallpaperUrl(wallpaperPreviewRev)}
                          alt="当前自定义壁纸预览"
                          className="w-full h-full object-cover"
                        />
                        <div
                          style={{
                            backgroundColor: `rgba(14, 15, 22, ${
                              Math.max(
                                15,
                                Math.min(
                                  90,
                                  settings.appearance.wallpaper_dim ?? 52
                                )
                              ) / 100
                            })`,
                          }}
                          className="absolute inset-0 flex items-end p-2.5"
                        >
                          <span className="px-2 py-0.5 rounded-md bg-black/45 backdrop-blur text-[11px] text-[#e5e7eb]">
                            实时预览（遮罩暗度 {settings.appearance.wallpaper_dim ?? 52}%）
                          </span>
                        </div>
                      </div>
                    )}
                  </div>

                  <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                    <div>
                      <div className="font-medium text-[#e5e7eb]">
                        背景遮罩暗度 (%)
                      </div>
                      <div className="text-[11px] text-[#7c828e] mt-0.5">
                        调节壁纸暗化比例（15% ~ 90%），保证白色文字清晰可读。
                      </div>
                    </div>
                    <NumberStepper
                      value={settings.appearance.wallpaper_dim ?? 52}
                      step={5}
                      min={15}
                      max={90}
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.appearance.wallpaper_dim = v;
                        })
                      }
                    />
                  </div>

                  <div className="px-4 py-3.5 flex items-center justify-between gap-4">
                    <div>
                      <div className="font-medium text-[#e5e7eb]">
                        卡片毛玻璃模糊度 (px)
                      </div>
                      <div className="text-[11px] text-[#7c828e] mt-0.5">
                        控制连接大厅主机卡片与搜索栏的毛玻璃模糊半径（0 ~ 32 px）。
                      </div>
                    </div>
                    <NumberStepper
                      value={settings.appearance.glass_blur_px ?? 16}
                      step={2}
                      min={0}
                      max={32}
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.appearance.glass_blur_px = v;
                        })
                      }
                    />
                  </div>
                </div>
              </div>
            )}

            {/* ===================== TAB: 监控 ===================== */}
            {tab === "monitor" && (
              <div className="space-y-4">
                <h1 className="text-lg font-semibold text-white">监控</h1>
                <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                  <div className="px-4 py-3.5 flex items-center justify-between">
                    <span>启用远端 /proc 实时资源监控</span>
                    <Switch
                      checked={settings.monitor.enabled}
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.monitor.enabled = v;
                        })
                      }
                    />
                  </div>
                  <div className="px-4 py-3.5 flex items-center justify-between">
                    <span>显示终端底部彩色指标栏</span>
                    <Switch
                      checked={settings.monitor.show_bottom_bar}
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.monitor.show_bottom_bar = v;
                        })
                      }
                    />
                  </div>
                  <div className="px-4 py-3.5 flex items-center justify-between">
                    <span>采集间隔 (秒)</span>
                    <NumberStepper
                      value={settings.monitor.interval_sec}
                      min={1}
                      max={60}
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.monitor.interval_sec = v;
                        })
                      }
                    />
                  </div>
                </div>
              </div>
            )}

            {/* ===================== TAB: 文件 ===================== */}
            {tab === "files" && (
              <div className="space-y-4">
                <h1 className="text-lg font-semibold text-white">文件 (SFTP)</h1>
                <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                  <div className="px-4 py-3.5 flex items-center justify-between">
                    <span>显示隐藏文件 (以 . 开头)</span>
                    <Switch
                      checked={settings.files.show_hidden}
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.files.show_hidden = v;
                        })
                      }
                    />
                  </div>
                  <div className="px-4 py-3.5 flex items-center justify-between">
                    <span>单文件下载保护上限 (MB)</span>
                    <NumberStepper
                      value={settings.files.max_download_mb}
                      step={10}
                      min={5}
                      max={500}
                      onChange={(v) =>
                        patchSettings((d) => {
                          d.files.max_download_mb = v;
                        })
                      }
                    />
                  </div>
                </div>
              </div>
            )}

            {/* ===================== TAB: MCP / Skill ===================== */}
            {tab === "mcp" && (
              <div className="space-y-4">
                <h1 className="text-lg font-semibold text-white">MCP / Skill</h1>
                <div className="rounded-xl bg-[#202126] border border-[#2d3039] p-4 space-y-3">
                  <div className="grid grid-cols-3 gap-2">
                    <input
                      value={mcpName}
                      onChange={(e) => setMcpName(e.target.value)}
                      placeholder="MCP 名称 (如 filesystem)"
                      className="px-2.5 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white outline-none"
                    />
                    <input
                      value={mcpCmd}
                      onChange={(e) => setMcpCmd(e.target.value)}
                      placeholder="启动命令 (如 npx -y @modelcontextprotocol/...)"
                      className="col-span-2 px-2.5 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white font-mono outline-none"
                    />
                  </div>
                  <button
                    onClick={() => {
                      if (!mcpName.trim() || !mcpCmd.trim()) return;
                      patchSettings((d) => {
                        d.mcp_servers.push({
                          id: `mcp-${Date.now()}`,
                          name: mcpName.trim(),
                          command: mcpCmd.trim(),
                          enabled: true,
                        });
                      });
                      setMcpName("");
                      setMcpCmd("");
                    }}
                    className="px-3.5 py-1.5 rounded-lg bg-[#2563eb] text-white font-medium"
                  >
                    添加 MCP 服务
                  </button>

                  <div className="space-y-2 pt-2">
                    {settings.mcp_servers.map((m) => (
                      <div
                        key={m.id}
                        className="px-3 py-2 rounded-lg bg-[#262830] border border-[#323541] flex items-center justify-between"
                      >
                        <div>
                          <div className="font-medium text-white">{m.name}</div>
                          <div className="font-mono text-[11px] text-[#7c828e]">
                            {m.command}
                          </div>
                        </div>
                        <button
                          onClick={() =>
                            patchSettings((d) => {
                              d.mcp_servers = d.mcp_servers.filter(
                                (x) => x.id !== m.id
                              );
                            })
                          }
                          className="text-[#8b909a] hover:text-[#f87171]"
                        >
                          <Trash2 size={13} />
                        </button>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            )}

            {/* ===================== TAB: 凭据 ===================== */}
            {tab === "credentials" && (
              <div className="space-y-4">
                <div>
                  <h1 className="text-lg font-semibold text-white">凭据保险箱</h1>
                  <p className="text-[#8b909a] mt-0.5">
                    所有密钥与密码均通过 Windows Credential Manager (DPAPI) 加密存储，不落盘明文文件。
                  </p>
                </div>
                <div className="rounded-xl bg-[#202126] border border-[#2d3039] p-4 space-y-3">
                  <div className="grid grid-cols-3 gap-2">
                    <input
                      value={credName}
                      onChange={(e) => setCredName(e.target.value)}
                      placeholder="凭据名称 (如 生产集群私钥)"
                      className="px-2.5 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white outline-none"
                    />
                    <select
                      value={credKind}
                      onChange={(e) => setCredKind(e.target.value)}
                      className="px-2.5 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white outline-none"
                    >
                      <option value="password">密码</option>
                      <option value="private_key">SSH 私钥 (PEM)</option>
                    </select>
                    <input
                      value={credUser}
                      onChange={(e) => setCredUser(e.target.value)}
                      placeholder="默认用户名 root"
                      className="px-2.5 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white font-mono outline-none"
                    />
                  </div>
                  <textarea
                    rows={2}
                    value={credSecret}
                    onChange={(e) => setCredSecret(e.target.value)}
                    placeholder="输入密码或粘贴私钥内容..."
                    className="w-full px-2.5 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white font-mono outline-none"
                  />
                  <button
                    onClick={async () => {
                      if (!credName.trim() || !credSecret.trim()) return;
                      const next = await addVaultCredential({
                        name: credName,
                        kind: credKind,
                        username: credUser,
                        secret: credSecret,
                      });
                      onChange(next);
                      setCredName("");
                      setCredSecret("");
                    }}
                    className="px-4 py-1.5 rounded-lg bg-[#2563eb] text-white font-medium"
                  >
                    存入系统凭据箱
                  </button>

                  <div className="space-y-2 pt-2">
                    {settings.credentials.map((c) => (
                      <div
                        key={c.id}
                        className="px-3.5 py-2.5 rounded-lg bg-[#262830] border border-[#323541] flex items-center justify-between"
                      >
                        <div>
                          <div className="font-medium text-white">{c.name}</div>
                          <div className="text-[11px] text-[#7c828e] font-mono">
                            {c.kind} | user={c.username || "-"}
                          </div>
                        </div>
                        <button
                          onClick={async () =>
                            onChange(await deleteVaultCredential(c.id))
                          }
                          className="text-[#8b909a] hover:text-[#f87171]"
                        >
                          <Trash2 size={14} />
                        </button>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            )}

            {/* ===================== TAB: 代理 ===================== */}
            {tab === "proxies" && (
              <div className="space-y-4">
                <div className="flex items-center justify-between">
                  <div>
                    <h1 className="text-lg font-semibold text-white">
                      代理与 Tailnet 组网
                    </h1>
                    <p className="text-[#8b909a] mt-0.5">
                      代理池（VLESS / Trojan / SS / VMess / Hysteria2 / SOCKS5 / HTTP，可粘贴订阅）与内置用户态 Tailscale / Headscale。主机在「网络」里选主备代理；重新粘贴同一订阅即刷新节点。
                    </p>
                  </div>
                  <button
                    onClick={onOpenTailnetModal}
                    className="px-3.5 py-1.5 rounded-lg bg-[#2563eb] text-white font-medium"
                  >
                    配置内置 Tailnet
                  </button>
                </div>

                <div className="rounded-xl bg-[#202126] border border-[#2d3039] p-4 space-y-3">
                  <textarea
                    value={prxLinks}
                    onChange={(e) => setPrxLinks(e.target.value)}
                    rows={3}
                    spellCheck={false}
                    placeholder={
                      "粘贴代理链接或订阅地址，每行一个\nvless:// trojan:// ss:// vmess:// hy2:// socks5:// http://\nhttps://机场域名/api/v1/client/subscribe?token=…"
                    }
                    className="w-full px-2.5 py-1.5 rounded-lg bg-[#272931] border border-[#343742] text-white font-mono outline-none resize-y"
                  />
                  <div className="flex items-center gap-3">
                    <button
                      disabled={!prxLinks.trim() || prxCands !== null}
                      onClick={() => void runProxyImport()}
                      className="px-4 py-1.5 rounded-lg bg-[#2563eb] text-white font-medium disabled:opacity-40"
                    >
                      导入
                    </button>
                    {prxMsg && <span className="text-[12px] text-[#9ca3af]">{prxMsg}</span>}
                  </div>

                  {prxCands && (
                    <div className="rounded-lg border border-[#343742] bg-[#1b1c21]">
                      <div className="px-3 py-2 flex items-center justify-between border-b border-[#2d3039]">
                        <span className="text-[12px] text-[#9ca3af]">
                          订阅里有 {prxCands.length} 个节点，勾选要用的；已导入的会按名称刷新
                        </span>
                        <div className="flex items-center gap-2">
                          <button
                            onClick={() => {
                              setPrxCands(null);
                              setPrxMsg("");
                            }}
                            className="px-3 py-1 rounded-md text-[#9ca3af] hover:text-white"
                          >
                            取消
                          </button>
                          <button
                            onClick={() => void runProxyImport([...prxPick])}
                            className="px-3 py-1 rounded-md bg-[#2563eb] text-white font-medium"
                          >
                            导入所选（{prxPick.size}）
                          </button>
                        </div>
                      </div>
                      <div className="max-h-72 overflow-y-auto py-1">
                        {prxCands.map((c) => (
                          <label
                            key={c.key}
                            className="px-3 py-1.5 flex items-center gap-2.5 hover:bg-[#25272e] cursor-pointer"
                          >
                            <input
                              type="checkbox"
                              disabled={c.existing}
                              checked={c.existing || prxPick.has(c.key)}
                              onChange={(e) => {
                                const next = new Set(prxPick);
                                if (e.target.checked) next.add(c.key);
                                else next.delete(c.key);
                                setPrxPick(next);
                              }}
                            />
                            <span className="text-white truncate">{c.name}</span>
                            <span className="text-[11px] text-[#7c828e] font-mono shrink-0">
                              {c.scheme}
                            </span>
                            {c.existing && (
                              <span className="text-[11px] text-[#4ade80] shrink-0">已导入</span>
                            )}
                          </label>
                        ))}
                      </div>
                    </div>
                  )}

                  <div className="space-y-2 pt-2">
                    {settings.proxies.map((p) => (
                      <div
                        key={p.id}
                        className="px-3.5 py-2.5 rounded-lg bg-[#262830] border border-[#323541] flex items-center justify-between"
                      >
                        <div>
                          <div className="font-medium text-white">{p.name}</div>
                          <div className="text-[11px] text-[#7c828e] font-mono">
                            {p.scheme}://{p.host}:{p.port}
                          </div>
                        </div>
                        <button
                          onClick={async () => {
                            try {
                              onChange(await deleteNamedProxy(p.id));
                            } catch (e) {
                              setPrxMsg((e as Error).message); // e.g. still used by a host
                            }
                          }}
                          className="text-[#8b909a] hover:text-[#f87171]"
                        >
                          <Trash2 size={14} />
                        </button>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            )}

            {/* ===================== TAB: 已知主机 ===================== */}
            {tab === "known_hosts" && (
              <div className="space-y-4">
                <div>
                  <h1 className="text-lg font-semibold text-white">已知主机</h1>
                  <p className="text-[#8b909a] mt-0.5">
                    管理本机信任的 SSH 服务器公钥指纹（{knownHostsPath || "~/.ssh/known_hosts"}）。重装远端系统后可在此移除旧指纹。
                  </p>
                </div>

                <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                  {knownHosts.length === 0 ? (
                    <div className="px-4 py-6 text-center text-[#7c828e]">
                      当前 known_hosts 中暂无记录
                    </div>
                  ) : (
                    knownHosts.map((kh) => (
                      <div
                        key={kh.line_number}
                        className="px-4 py-3 flex items-center justify-between gap-3"
                      >
                        <div className="min-w-0">
                          <div className="font-medium text-white truncate">
                            {kh.hosts}
                          </div>
                          <div className="text-[11px] text-[#7c828e] font-mono truncate mt-0.5">
                            {kh.key_type}  {kh.fingerprint}
                          </div>
                        </div>
                        <button
                          onClick={async () => {
                            const res = await deleteKnownHost(kh.line_number);
                            setKnownHosts(res.entries || []);
                          }}
                          className="p-1.5 rounded hover:bg-[#ef4444]/20 text-[#8b909a] hover:text-[#f87171] shrink-0"
                          title="移除此主机指纹"
                        >
                          <Trash2 size={14} />
                        </button>
                      </div>
                    ))
                  )}
                </div>
              </div>
            )}

            {/* ===================== TAB: 迁移 ===================== */}
            {tab === "migrate" && (
              <div className="space-y-4">
                <div>
                  <h1 className="text-lg font-semibold text-white">迁移</h1>
                  <p className="text-[#8b909a] mt-0.5">
                    从本机 OpenSSH 配置或其他终端工具快速迁移主机列表。
                  </p>
                </div>

                <div className="rounded-xl bg-[#202126] border border-[#2d3039] p-5 space-y-3">
                  <div className="font-medium text-white">
                    从本机 ~/.ssh/config 自动导入
                  </div>
                  <div className="text-[#8b909a] leading-relaxed">
                    自动扫描当前系统用户目录下的 OpenSSH 配置文件，导入主机地址、端口与用户名。
                  </div>
                  <button
                    onClick={async () => {
                      try {
                        const res = await importLocalSSHConfig();
                        onRefreshHosts();
                        setMigrateStatus(
                          res.imported_count > 0
                            ? `已成功导入 ${res.imported_count} 台主机！`
                            : "未发现新的可导入主机条目。"
                        );
                      } catch (e) {
                        setMigrateStatus("导入失败: " + (e as Error).message);
                      }
                    }}
                    className="px-4 py-2 rounded-lg bg-[#2563eb] hover:bg-[#3b82f6] text-white font-medium"
                  >
                    立即扫描并导入 ~/.ssh/config
                  </button>
                  {migrateStatus && (
                    <div className="text-[#60a5fa] pt-1">{migrateStatus}</div>
                  )}
                </div>
              </div>
            )}

            {/* ===================== TAB: 备份 ===================== */}
            {tab === "backup" && (
              <div className="space-y-4">
                <h1 className="text-lg font-semibold text-white">备份与恢复</h1>
                <div className="rounded-xl bg-[#202126] border border-[#2d3039] p-5 space-y-3">
                  <p className="text-[#9ca3af] leading-relaxed">
                    导出的 JSON 备份包含全部主机连接元数据、接力参数、代码片段与偏好设置（出于安全红线，系统凭据箱内的明文密码和私钥不会被导出）。
                  </p>
                  <div className="flex items-center gap-3 pt-1">
                    <button
                      onClick={handleExportBackup}
                      className="px-4 py-2 rounded-lg bg-[#2563eb] hover:bg-[#3b82f6] text-white font-medium"
                    >
                      导出配置备份 (.json)
                    </button>
                    <label className="px-4 py-2 rounded-lg bg-[#272931] border border-[#353945] hover:bg-[#2f323d] text-white cursor-pointer">
                      <span>从文件导入恢复</span>
                      <input
                        type="file"
                        accept=".json"
                        onChange={handleImportBackup}
                        className="hidden"
                      />
                    </label>
                  </div>
                </div>
              </div>
            )}

            {/* ===================== TAB: 快捷键 ===================== */}
            {tab === "shortcuts" && (
              <div className="space-y-4">
                <h1 className="text-lg font-semibold text-white">快捷键</h1>
                <div className="rounded-xl bg-[#202126] border border-[#2d3039] divide-y divide-[#2a2d35]">
                  {[
                    ["混合补齐：直接接受行内灰字", "→ (右方向键)"],
                    ["混合补齐：打开或轮换候选列表", "Tab / Shift + Tab"],
                    ["候选列表开启时：插入选中项但不执行命令", "Enter"],
                    ["强制打开终端右键菜单", "Shift + 右键"],
                    ["多行草稿框整段发送 (Bracketed Paste)", "Ctrl + Enter"],
                    ["终端内向下查找关键字", "Enter (搜索栏开启时)"],
                    ["关闭候选列表或终端搜索栏", "Esc"],
                    ["复制选中终端文本 / 发送中断", "Ctrl + Shift + C / Ctrl + C"],
                  ].map(([desc, key]) => (
                    <div
                      key={desc}
                      className="px-4 py-3 flex items-center justify-between"
                    >
                      <span>{desc}</span>
                      <kbd className="px-2.5 py-1 rounded bg-[#272931] border border-[#343742] font-mono text-[#d1d5db]">
                        {key}
                      </kbd>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* ===================== TAB: 关于 ===================== */}
            {tab === "about" && (
              <div className="space-y-4">
                <h1 className="text-lg font-semibold text-white">关于 Shnex</h1>
                <div className="rounded-xl bg-[#202126] border border-[#2d3039] p-5 space-y-2.5 leading-relaxed">
                  <div className="flex items-center gap-3">
                    <svg
                      viewBox="0 0 100 100"
                      className="w-9 h-9 shrink-0 drop-shadow"
                      aria-hidden="true"
                    >
                      <rect
                        x="5"
                        y="5"
                        width="90"
                        height="90"
                        rx="22"
                        fill="#16181f"
                        stroke="#f59e0b"
                        strokeOpacity="0.88"
                        strokeWidth="3.8"
                      />
                      <path
                        d="M26 33 L48 50 L26 67"
                        fill="none"
                        stroke="#fbbf24"
                        strokeWidth="10.4"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                      />
                      <line
                        x1="54"
                        y1="66"
                        x2="76"
                        y2="66"
                        stroke="#3fb950"
                        strokeWidth="9.6"
                        strokeLinecap="round"
                      />
                    </svg>
                    <div className="text-base font-semibold text-white">
                      Shnex Workbench v0.3.9
                    </div>
                  </div>
                  <div className="text-[#9ca3af]">
                    专为远程运行 Claude CLI / AI 编程优化的「桌面 + 手机接力工作台」。
                  </div>
                  <div className="font-mono text-[11px] text-[#7c828e] pt-2">
                    架构：纯 Go 单核引擎 (127.0.0.1 安全回环 + ConPTY 本机终端) + Wails v3 + xterm.js WebGL
                    <br />
                    开源合规：100% MIT / BSD / Apache-2.0 宽松许可证（零 GPL 依赖）
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Unlock Vault Modal */}
      {showUnlockModal && (
        <div className="fixed inset-0 z-50 bg-black/65 flex items-center justify-center p-4">
          <form
            onSubmit={handleUnlockSubmit}
            className="w-[390px] rounded-2xl bg-[#1f2024] border border-[#2f3138] shadow-2xl p-5 space-y-3.5 text-xs"
          >
            <div className="flex items-center justify-between">
              <h2 className="text-sm font-semibold text-white">
                解锁已保存的机密内容
              </h2>
              <button
                type="button"
                onClick={() => setShowUnlockModal(false)}
                className="p-1 rounded hover:bg-[#2a2d36] text-[#8b909a]"
              >
                <X size={15} />
              </button>
            </div>
            {unlockErr && (
              <div className="p-2 rounded-lg bg-[#ef4444]/15 border border-[#ef4444]/40 text-[#f87171]">
                {unlockErr}
              </div>
            )}
            <input
              type="password"
              required
              autoFocus
              value={unlockInput}
              onChange={(e) => setUnlockInput(e.target.value)}
              placeholder="输入主密码..."
              className="w-full px-3 py-2 rounded-lg bg-[#2a2b30] border border-[#363840] focus:border-[#2b7de9] text-white outline-none"
            />
            <div className="flex justify-end gap-2 pt-2 border-t border-[#2c2e35]">
              <button
                type="button"
                onClick={() => setShowUnlockModal(false)}
                className="px-4 py-1.5 rounded-full bg-[#2b2d32] text-[#d1d5db]"
              >
                取消
              </button>
              <button
                type="submit"
                className="px-5 py-1.5 rounded-full bg-[#1d7dfa] hover:bg-[#358bfa] text-white font-medium"
              >
                解锁
              </button>
            </div>
          </form>
        </div>
      )}

      {/* Add / Edit AI Provider Modal */}
      {editingProv && (
        <div className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-4">
          <form
            onSubmit={handleSaveProvider}
            className="w-[440px] rounded-2xl bg-[#1e2026] border border-[#30333d] shadow-2xl p-5 space-y-3.5 text-xs"
          >
            <div className="flex items-center justify-between">
              <h2 className="text-sm font-semibold text-white">
                {editingProv.id ? "编辑模型提供商" : "添加模型提供商"}
              </h2>
              <button
                type="button"
                onClick={() => setEditingProv(null)}
                className="p-1 rounded hover:bg-[#2a2d36] text-[#8b909a]"
              >
                <X size={15} />
              </button>
            </div>

            <div className="grid grid-cols-2 gap-2.5">
              <label className="space-y-1 text-[#9ca3af]">
                <span>提供商标识名称</span>
                <input
                  required
                  value={editingProv.name ?? ""}
                  onChange={(e) =>
                    setEditingProv({ ...editingProv, name: e.target.value })
                  }
                  placeholder="openai / deepseek / claude"
                  className="w-full px-2.5 py-1.5 rounded-lg bg-[#141519] border border-[#2e313b] text-white outline-none"
                />
              </label>

              <label className="space-y-1 text-[#9ca3af]">
                <span>模型名称 (Model ID)</span>
                <input
                  required
                  value={editingProv.model ?? ""}
                  onChange={(e) =>
                    setEditingProv({ ...editingProv, model: e.target.value })
                  }
                  placeholder="gpt-4o / deepseek-chat"
                  className="w-full px-2.5 py-1.5 rounded-lg bg-[#141519] border border-[#2e313b] text-white font-mono outline-none"
                />
              </label>
            </div>

            <label className="block space-y-1 text-[#9ca3af]">
              <span>接口地址 (OpenAI 兼容 Base URL)</span>
              <input
                required
                value={editingProv.base_url ?? ""}
                onChange={(e) =>
                  setEditingProv({ ...editingProv, base_url: e.target.value })
                }
                placeholder="https://api.openai.com/v1"
                className="w-full px-2.5 py-1.5 rounded-lg bg-[#141519] border border-[#2e313b] text-white font-mono outline-none"
              />
            </label>

            <label className="block space-y-1 text-[#9ca3af]">
              <span>
                API Key{" "}
                {editingProv.has_api_key
                  ? "(已加密存储，留空则保持不变)"
                  : "(加密存入系统凭据箱)"}
              </span>
              <input
                type="password"
                value={provApiKey}
                onChange={(e) => setProvApiKey(e.target.value)}
                placeholder="sk-..."
                className="w-full px-2.5 py-1.5 rounded-lg bg-[#141519] border border-[#2e313b] text-white font-mono outline-none"
              />
            </label>

            <div className="flex justify-end gap-2 pt-2 border-t border-[#2b2e37]">
              <button
                type="button"
                onClick={() => setEditingProv(null)}
                className="px-4 py-1.5 rounded-lg bg-[#272932] text-[#d1d5db]"
              >
                取消
              </button>
              <button
                type="submit"
                className="px-4 py-1.5 rounded-lg bg-[#2563eb] hover:bg-[#3b82f6] text-white font-medium"
              >
                保存提供商
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
};
