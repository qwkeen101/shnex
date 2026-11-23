import React, { useMemo, useState } from "react";
import {
  Folder,
  FolderOpen,
  LayoutGrid,
  List,
  MoreHorizontal,
  Pencil,
  Plus,
  Search,
  Star,
} from "lucide-react";
import { HostProfile, SystemStats, maskTargetAddress } from "../api";
import { HostBadge } from "./HostBadge";
import { ServerCatCard } from "./ServerCatCard";

export interface ActiveHubSession {
  tabId: string;
  hostId?: string;
  title: string;
  maskedUserTarget: string;
  currentDir: string;
  icon?: string;
  badgeText?: string;
  color?: string;
  stats: SystemStats | null;
  cpuHistory: number[];
  aiState: "running" | "approval" | "idle";
}

interface Props {
  hosts: HostProfile[];
  activeSessions: ActiveHubSession[];
  totalSessionsCount: number;
  latencies: Record<string, number>;
  privacyMode: boolean;
  blurPx: number;
  connectingId: string | null;
  onConnectHost: (hp: HostProfile, openSftp?: boolean) => void;
  onResumeTab: (tabId: string) => void;
  onOpenTabSFTP: (tabId: string) => void;
  onDisconnectTab: (tabId: string) => void;
  onNewHost: () => void;
  onEditHost: (hp: HostProfile) => void;
  onToggleFavorite: (hp: HostProfile) => void;
  onQuickConnect: () => void;
  onQuickMigrate: () => void;
  onPingAll: () => void;
  onOpenLocalTerminal: () => void;
  onOpenLocalMarkdown: () => void;
  onOpenMarkdownDemo: () => void;
}

function getGreetingCN(): string {
  const hr = new Date().getHours();
  if (hr < 6) return "夜深了";
  if (hr < 12) return "上午好";
  if (hr < 14) return "中午好";
  if (hr < 18) return "下午好";
  return "晚上好";
}

function formatLastConnected(iso?: string): string {
  if (!iso) return "从未连接";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "从未连接";
  const diffSec = Math.floor((Date.now() - d.getTime()) / 1000);
  if (diffSec < 90) return "刚刚";
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)} 分钟前`;
  if (diffSec < 86400) {
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  }
  return `${d.getMonth() + 1}月${d.getDate()}日`;
}

export const ConnectionHub: React.FC<Props> = ({
  hosts,
  activeSessions,
  totalSessionsCount,
  latencies,
  privacyMode,
  blurPx,
  connectingId,
  onConnectHost,
  onResumeTab,
  onOpenTabSFTP,
  onDisconnectTab,
  onNewHost,
  onEditHost,
  onToggleFavorite,
  onQuickConnect,
  onQuickMigrate,
  onPingAll,
  onOpenLocalTerminal,
  onOpenLocalMarkdown,
  onOpenMarkdownDemo,
}) => {
  const [query, setQuery] = useState("");
  const [scope, setScope] = useState<"all" | "fav" | "recent">("all");
  const [selectedGroup, setSelectedGroup] = useState("");
  const [layoutMode, setLayoutMode] = useState<"list" | "grid">("list");
  const [moreMenuOpen, setMoreMenuOpen] = useState(false);

  const groups = useMemo(() => {
    const s = new Set<string>();
    for (const h of hosts) {
      if (h.group) s.add(h.group);
    }
    return Array.from(s);
  }, [hosts]);

  const activeHostIds = useMemo(() => {
    const s = new Set<string>();
    for (const a of activeSessions) {
      if (a.hostId) s.add(a.hostId);
    }
    return s;
  }, [activeSessions]);

  const visibleHosts = useMemo(() => {
    let list = hosts.filter((h) => {
      const q = query.trim().toLowerCase();
      if (
        q &&
        !h.name.toLowerCase().includes(q) &&
        !h.host.toLowerCase().includes(q) &&
        !h.user.toLowerCase().includes(q)
      ) {
        return false;
      }
      if (selectedGroup && (h.group || "") !== selectedGroup) {
        return false;
      }
      if (scope === "fav" && !h.favorite) {
        return false;
      }
      if (scope === "recent" && !h.last_connected_at) {
        return false;
      }
      return true;
    });

    if (scope === "recent") {
      list = [...list].sort((a, b) =>
        (b.last_connected_at || "").localeCompare(a.last_connected_at || "")
      );
    }
    return list;
  }, [hosts, query, scope, selectedGroup]);

  // Hosts not already shown in the top Active Hero Cards (when scope === "all" & no search)
  const tableHosts = useMemo(() => {
    if (query.trim() || scope !== "all") return visibleHosts;
    return visibleHosts.filter((h) => !activeHostIds.has(h.id));
  }, [visibleHosts, activeHostIds, query, scope]);

  const glassStyle: React.CSSProperties = {
    backdropFilter: `blur(${blurPx}px)`,
    WebkitBackdropFilter: `blur(${blurPx}px)`,
  };

  return (
    <div className="flex-1 overflow-y-auto px-8 py-7 select-none">
      <div className="max-w-[1160px] mx-auto space-y-5">
        {/* 1. Greeting & Live Summary */}
        <div>
          <h1 className="text-2xl font-bold text-white tracking-tight drop-shadow-sm">
            {getGreetingCN()}
          </h1>
          <div className="mt-1.5 flex items-center gap-3.5 text-xs text-[#b2b8c8]">
            <span>{hosts.length} 台主机</span>
            <span className="flex items-center gap-1.5">
              <span
                className={`w-2 h-2 rounded-full ${
                  activeSessions.length > 0 ? "bg-[#4ade80]" : "bg-[#6b7280]"
                }`}
              />
              <span>{activeSessions.length} 台在线</span>
            </span>
            <span>{totalSessionsCount} 个会话</span>
          </div>
        </div>

        {/* 2. Unified Frosted-Glass Search & Filter Bar */}
        <div className="flex flex-wrap items-center gap-2.5">
          {/* Search Input */}
          <div
            style={glassStyle}
            className="flex-1 min-w-[240px] h-9 px-3.5 rounded-full bg-[#1e202b]/72 border border-white/[0.11] flex items-center gap-2 focus-within:border-white/30 transition"
          >
            <Search size={14} className="text-[#8e95a8] shrink-0" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索主机或地址"
              className="flex-1 bg-transparent text-xs text-white placeholder:text-[#8a91a4] outline-none"
            />
          </div>

          {/* Scope Segmented Pills: 全部 / 收藏 / 最近 */}
          <div
            style={glassStyle}
            className="h-9 p-1 rounded-full bg-[#1e202b]/72 border border-white/[0.11] flex items-center gap-0.5 text-xs"
          >
            {(
              [
                ["all", "全部"],
                ["fav", "收藏"],
                ["recent", "最近"],
              ] as const
            ).map(([val, label]) => (
              <button
                key={val}
                onClick={() => setScope(val)}
                className={`px-3.5 h-full rounded-full transition ${
                  scope === val
                    ? "bg-[#46384d]/90 text-white font-semibold shadow-sm"
                    : "text-[#aeb4c4] hover:text-white"
                }`}
              >
                {label}
              </button>
            ))}
          </div>

          {/* Group Selector */}
          <div
            style={glassStyle}
            className="h-9 px-3.5 rounded-full bg-[#1e202b]/72 border border-white/[0.11] flex items-center gap-1.5 text-xs text-[#d5dae6]"
          >
            <Folder size={13} className="text-[#8e95a8]" />
            <select
              value={selectedGroup}
              onChange={(e) => setSelectedGroup(e.target.value)}
              className="bg-transparent text-[#e5e7eb] outline-none cursor-pointer"
            >
              <option value="" className="bg-[#1f212b]">
                全部分组
              </option>
              {groups.map((g) => (
                <option key={g} value={g} className="bg-[#1f212b]">
                  {g}
                </option>
              ))}
            </select>
          </div>

          {/* Grid / List View Switcher */}
          <div
            style={glassStyle}
            className="h-9 p-1 rounded-full bg-[#1e202b]/72 border border-white/[0.11] flex items-center gap-0.5 text-[#aeb4c4]"
          >
            <button
              onClick={() => setLayoutMode("grid")}
              className={`w-7 h-7 rounded-full flex items-center justify-center transition ${
                layoutMode === "grid"
                  ? "bg-white/15 text-white"
                  : "hover:text-white"
              }`}
              title="卡片网格"
            >
              <LayoutGrid size={13} />
            </button>
            <button
              onClick={() => setLayoutMode("list")}
              className={`w-7 h-7 rounded-full flex items-center justify-center transition ${
                layoutMode === "list"
                  ? "bg-[#46384d]/90 text-white"
                  : "hover:text-white"
              }`}
              title="列表视图"
            >
              <List size={14} />
            </button>
          </div>

          {/* More Utilities Menu */}
          <div className="relative">
            <button
              style={glassStyle}
              onClick={() => setMoreMenuOpen((v) => !v)}
              className="w-9 h-9 rounded-full bg-[#1e202b]/72 hover:bg-[#292c3a]/85 border border-white/[0.11] flex items-center justify-center text-[#d5dae6]"
              title="更多操作"
            >
              <MoreHorizontal size={15} />
            </button>

            {moreMenuOpen && (
              <div
                onMouseLeave={() => setMoreMenuOpen(false)}
                className="absolute right-0 top-11 z-40 w-44 py-1.5 rounded-xl bg-[#20222d]/95 border border-white/15 shadow-2xl text-xs text-[#e5e7eb]"
              >
                <button
                  onClick={() => {
                    setMoreMenuOpen(false);
                    onQuickConnect();
                  }}
                  className="w-full px-3.5 py-1.5 text-left hover:bg-white/10"
                >
                  快速连接 SSH...
                </button>
                <button
                  onClick={() => {
                    setMoreMenuOpen(false);
                    onOpenLocalTerminal();
                  }}
                  className="w-full px-3.5 py-1.5 text-left hover:bg-white/10"
                >
                  新建本机终端
                </button>
                <button
                  onClick={() => {
                    setMoreMenuOpen(false);
                    onOpenLocalMarkdown();
                  }}
                  className="w-full px-3.5 py-1.5 text-left hover:bg-white/10"
                >
                  打开本地 Markdown...
                </button>
                <button
                  onClick={() => {
                    setMoreMenuOpen(false);
                    onOpenMarkdownDemo();
                  }}
                  className="w-full px-3.5 py-1.5 text-left hover:bg-white/10"
                >
                  M↓ 数学公式排版示例
                </button>
                <button
                  onClick={() => {
                    setMoreMenuOpen(false);
                    onQuickMigrate();
                  }}
                  className="w-full px-3.5 py-1.5 text-left hover:bg-white/10"
                >
                  从 ~/.ssh/config 导入
                </button>
                <button
                  onClick={() => {
                    setMoreMenuOpen(false);
                    onPingAll();
                  }}
                  className="w-full px-3.5 py-1.5 text-left hover:bg-white/10"
                >
                  测量全部主机延迟
                </button>
              </div>
            )}
          </div>

          {/* Primary New Connection Pill */}
          <button
            onClick={onNewHost}
            className="h-9 px-4 rounded-full bg-gradient-to-r from-[#b84d7a] to-[#9b446c] hover:from-[#c95988] hover:to-[#ad4e7a] text-white text-xs font-semibold flex items-center gap-1.5 shadow-lg transition"
          >
            <Plus size={14} />
            <span>新建连接</span>
          </button>
        </div>

        {/* 3. Active Online Server Hero Cards (Server-Cat + Sparkline + Resume!) */}
        {activeSessions.length > 0 && (
          <div className="space-y-3">
            {activeSessions.map((sess) => {
              const hp = hosts.find((x) => x.id === sess.hostId);
              return (
                <ServerCatCard
                  key={sess.tabId}
                  title={sess.title}
                  maskedUserTarget={sess.maskedUserTarget}
                  currentDir={sess.currentDir}
                  icon={sess.icon}
                  badgeText={sess.badgeText}
                  color={sess.color}
                  stats={sess.stats}
                  cpuHistory={sess.cpuHistory}
                  aiState={sess.aiState}
                  blurPx={blurPx}
                  onResume={() => onResumeTab(sess.tabId)}
                  onOpenSFTP={() => onOpenTabSFTP(sess.tabId)}
                  onEditHost={hp ? () => onEditHost(hp) : undefined}
                  onDisconnect={() => onDisconnectTab(sess.tabId)}
                />
              );
            })}
          </div>
        )}

        {/* 4. Saved Hosts List / Grid */}
        {hosts.length === 0 ? (
          <div
            style={glassStyle}
            className="rounded-2xl bg-[#1e202b]/72 border border-white/[0.10] py-16 px-6 text-center space-y-4"
          >
            <div className="text-base font-semibold text-white">
              还没有保存的主机
            </div>
            <div className="text-xs text-[#9ca3b4]">
              添加第一台远程服务器，连接后即可在此看到实时状态卡片与一键接力入口。
            </div>
            <div className="flex items-center justify-center gap-2.5 pt-1">
              <button
                onClick={onQuickMigrate}
                className="px-4 py-2 rounded-full bg-[#1d7dfa] hover:bg-[#358bfa] text-white font-medium"
              >
                快速迁移
              </button>
              <button
                onClick={onQuickConnect}
                className="px-4 py-2 rounded-full bg-white/10 hover:bg-white/15 text-white font-medium"
              >
                快速连接
              </button>
              <button
                onClick={onNewHost}
                className="px-4 py-2 rounded-full bg-white/10 hover:bg-white/15 text-white font-medium"
              >
                新建连接
              </button>
            </div>
          </div>
        ) : tableHosts.length === 0 ? null : layoutMode === "list" ? (
          <div
            style={glassStyle}
            className="rounded-2xl bg-[#1e202b]/72 border border-white/[0.10] overflow-hidden shadow-xl"
          >
            <div className="grid grid-cols-12 px-5 py-3 border-b border-white/[0.07] text-[11px] text-[#8d94a8]">
              <div className="col-span-4">名称</div>
              <div className="col-span-4">地址</div>
              <div className="col-span-2">分组</div>
              <div className="col-span-2 flex justify-between">
                <span>最后连接</span>
              </div>
            </div>

            <div className="divide-y divide-white/[0.06]">
              {tableHosts.map((hp) => {
                const isConnecting = connectingId === hp.id;
                const rtt = latencies[hp.id];
                const maskedAddr = maskTargetAddress(
                  `${hp.user}@${hp.host}`,
                  privacyMode
                );
                return (
                  <div
                    key={hp.id}
                    onClick={() => onConnectHost(hp)}
                    className="grid grid-cols-12 items-center px-5 py-3 hover:bg-white/[0.06] cursor-pointer transition group"
                  >
                    <div className="col-span-4 flex items-center gap-3 min-w-0 pr-3">
                      <HostBadge
                        icon={hp.icon}
                        badgeText={hp.badge_text}
                        color={hp.color || "#374151"}
                        size="sm"
                      />
                      <span className="font-semibold text-white truncate">
                        {hp.name}
                      </span>
                      {isConnecting && (
                        <span className="text-[11px] text-[#60a5fa]">
                          连接中...
                        </span>
                      )}
                    </div>

                    <div className="col-span-4 font-mono text-xs text-[#c2c8d6] truncate pr-3">
                      {maskedAddr}
                      {rtt !== undefined && rtt >= 0 && (
                        <span className="ml-2 text-[11px] text-[#4ade80]">
                          {rtt}ms
                        </span>
                      )}
                    </div>

                    <div className="col-span-2 text-xs text-[#9ca3b4] truncate">
                      {hp.group || "未分组"}
                    </div>

                    <div className="col-span-2 flex items-center justify-between gap-2 text-xs text-[#9ca3b4]">
                      <span>{formatLastConnected(hp.last_connected_at)}</span>

                      <div
                        onClick={(e) => e.stopPropagation()}
                        className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity"
                      >
                        <button
                          onClick={() => onToggleFavorite(hp)}
                          className={`p-1 rounded hover:bg-white/10 ${
                            hp.favorite ? "text-[#fbbf24]" : "text-[#9ca3b4]"
                          }`}
                          title={hp.favorite ? "取消收藏" : "收藏主机"}
                        >
                          <Star
                            size={13}
                            fill={hp.favorite ? "currentColor" : "none"}
                          />
                        </button>
                        <button
                          onClick={() => onConnectHost(hp, true)}
                          className="p-1 rounded hover:bg-white/10 text-[#cbd5e1]"
                          title="连接并打开文件"
                        >
                          <FolderOpen size={13} />
                        </button>
                        <button
                          onClick={() => onEditHost(hp)}
                          className="p-1 rounded hover:bg-white/10 text-[#cbd5e1]"
                          title="编辑主机"
                        >
                          <Pencil size={13} />
                        </button>
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        ) : (
          <div className="grid grid-cols-3 gap-3.5">
            {tableHosts.map((hp) => (
              <div
                key={hp.id}
                style={glassStyle}
                onClick={() => onConnectHost(hp)}
                className="p-4 rounded-2xl bg-[#1e202b]/72 hover:bg-[#262938]/82 border border-white/[0.10] hover:border-white/25 cursor-pointer flex items-center justify-between gap-3 transition group"
              >
                <div className="flex items-center gap-3 min-w-0">
                  <HostBadge
                    icon={hp.icon}
                    badgeText={hp.badge_text}
                    color={hp.color}
                    size="sm"
                  />
                  <div className="min-w-0">
                    <div className="font-semibold text-white truncate">
                      {hp.name}
                    </div>
                    <div className="text-[11px] font-mono text-[#9ca3b4] truncate mt-0.5">
                      {maskTargetAddress(`${hp.user}@${hp.host}`, privacyMode)}
                    </div>
                  </div>
                </div>
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    onEditHost(hp);
                  }}
                  className="opacity-0 group-hover:opacity-100 p-1.5 rounded-lg hover:bg-white/10 text-[#9ca3b4] hover:text-white"
                >
                  <Pencil size={13} />
                </button>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};
