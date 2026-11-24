import React, { useEffect, useRef, useState } from "react";
import {
  Activity,
  AlignLeft,
  ArrowLeftRight,
  Bell,
  Bot,
  CheckSquare,
  ChevronDown,
  ChevronRight,
  ChevronsUpDown,
  Cloud,
  Code2,
  Command,
  Container,
  Download,
  Eye,
  EyeOff,
  FileText,
  FolderOpen,
  Gauge,
  Github,
  History,
  Info,
  Keyboard,
  ListChecks,
  Maximize2,
  MessageSquare,
  Minus,
  Monitor,
  MoreHorizontal,
  PanelBottom,
  PanelLeft,
  Pencil,
  Plus,
  Radio,
  RefreshCw,
  Search,
  Server,
  Settings,
  Square,
  TerminalSquare,
  Trash2,
  X,
  Zap,
} from "lucide-react";
import {
  ApiError,
  AppNotification,
  AppSettings,
  CompiledMarkdownDocument,
  FileEntry,
  HostProfile,
  PortForwardRule,
  RemoteTmuxSession,
  SessionLogMeta,
  SessionSummary,
  Snippet,
  SystemStats,
  TailnetNode,
  TailnetStatus,
  UpsertHostInput,
  buildWallpaperUrl,
  createRemoteTmuxSession,
  createSession,
  deleteHost,
  deleteSession,
  deleteSessionLog,
  deleteSnippet,
  execSessionCommand,
  formatBytes,
  formatDurationSec,
  getMarkdownSample,
  getSessionLog,
  getSettings,
  getTailnetStatus,
  importBackupBundle,
  importLocalSSHConfig,
  initBridge,
  killRemoteTmuxSession,
  listHosts,
  listNotifications,
  listRemoteTmuxSessions,
  listSessionForwards,
  listSessionLogs,
  listSnippets,
  listTailnetNodes,
  maskTargetAddress,
  openNativeFileDialog,
  pingAllHosts,
  readLocalMarkdownFile,
  renameRemoteTmuxSession,
  renderMarkdownNative,
  saveSettings,
  setTrayStyle,
  sftpRead,
  startSessionForward,
  stopSessionForward,
  tailnetDown,
  tailnetUp,
  upsertHost,
  upsertSnippet,
  windowAction,
} from "./api";
import { AIPane } from "./components/AIPane";
import { HostBadge } from "./components/HostBadge";
import { HostModal } from "./components/HostModal";
import { MarkdownDocSource, MarkdownTabView } from "./components/MarkdownTabView";
import { SFTPDrawer } from "./components/SFTPDrawer";
import { SettingsCategory, SettingsView } from "./components/SettingsView";
import { SystemMonitorDrawer } from "./components/SystemMonitorDrawer";
import { TerminalPane } from "./components/TerminalPane";

interface TermPaneItem {
  paneId: string;
  sessionId: string;
}

interface WorkspaceTab {
  tabId: string;
  protocol?: string;
  localShellKey?: string;
  hostId?: string;
  /** Remote tmux session this tab is attached to; set only for tmux hosts, whose top tabs are their tmux sessions. */
  tmuxId?: string;
  title: string;
  icon?: string;
  badgeText?: string;
  color?: string;
  rawUserTarget: string;
  splitDir: "row" | "col";
  panes: TermPaneItem[];
  focusedPaneId: string;
  aiState: "running" | "approval" | "idle";
  latestStats: SystemStats | null;
  cpuHistory?: number[];
  sftpPath: string;
  metricsBar?: "follow" | "on" | "off";
  terminalEncoding?: string;
  markdownData?: {
    content: string;
    doc: CompiledMarkdownDocument;
    source: MarkdownDocSource;
  };
}

interface HistoryEntry {
  title: string;
  userTarget: string;
  connectedAt: string;
  hostId?: string;
}

type RightPanelType =
  | "none"
  | "monitor"
  | "processes"
  | "hostinfo"
  | "sftp"
  | "docker"
  | "ai"
  | "tunnels"
  | "snippets"
  | "history"
  | "notifications";

type ActivePanelKey = Exclude<RightPanelType, "none">;

export const App: React.FC = () => {
  const [ready, setReady] = useState(false);
  const [bootErr, setBootErr] = useState<string | null>(null);

  const [hosts, setHosts] = useState<HostProfile[]>([]);
  const [hostLatencies, setHostLatencies] = useState<Record<string, number>>({});
  const [pingingAll, setPingingAll] = useState(false);

  const [appSettings, setAppSettings] = useState<AppSettings | null>(null);
  const [showSettings, setShowSettings] = useState(false);
  const [settingsInitTab, setSettingsInitTab] = useState<SettingsCategory>("ai");
  const [wallpaperRev, setWallpaperRev] = useState<number>(() => Date.now());

  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [sidebarWidth, setSidebarWidth] = useState<number>(() => {
    const saved = Number(localStorage.getItem("shnex_sidebar_width"));
    return Number.isFinite(saved) && saved >= 240 && saved <= 520 ? saved : 300;
  });
  const [searchBarOpen, setSearchBarOpen] = useState(false);
  const [searchHost, setSearchHost] = useState("");
  const [batchMode, setBatchMode] = useState(false);
  const [selectedHostIds, setSelectedHostIds] = useState<string[]>([]);

  // Privacy Mode: Masks all server IP addresses in UI (Matches Screenshot 5!)
  const [privacyMode, setPrivacyMode] = useState<boolean>(() => {
    return localStorage.getItem("shnex_privacy_ip") !== "0";
  });
  const [broadcastMode, setBroadcastMode] = useState(false);
  const [globalComposerOpen, setGlobalComposerOpen] = useState(false);

  const [tabs, setTabs] = useState<WorkspaceTab[]>([]);
  const [activeTabId, setActiveTabId] = useState<string | null>(null);
  const [connectingId, setConnectingId] = useState<string | null>(null);
  const connectingLockRef = useRef<Set<string>>(new Set());

  const [openPanels, setOpenPanels] = useState<ActivePanelKey[]>([]);
  const isPanelOpen = (p: ActivePanelKey) => openPanels.includes(p);
  const closePanel = (p: ActivePanelKey) =>
    setOpenPanels((prev) => prev.filter((x) => x !== p));

  // Port Forwards, Remote Tmux & Command History
  const [forwards, setForwards] = useState<PortForwardRule[]>([]);
  const [newFwdLocalPort, setNewFwdLocalPort] = useState("8080");
  const [newFwdRemoteAddr, setNewFwdRemoteAddr] = useState("127.0.0.1:3000");
  // Remote tmux conversations of tmux hosts (keyed by hostId): the source of their top tabs.
  const [remoteTmux, setRemoteTmux] = useState<Record<string, RemoteTmuxSession[]>>({});
  const [renamingTmux, setRenamingTmux] = useState<{
    hostId: string;
    tmuxId: string;
    value: string;
  } | null>(null);
  const renameCancelRef = useRef(false);
  // Passwords typed during this run (memory only), so each further tab of the host does not prompt again.
  const hostPasswordsRef = useRef<Record<string, string>>({});
  // Tabs whose tmux session is being ended: they must never auto-reconnect.
  const endingTmuxTabsRef = useRef<Set<string>>(new Set());
  const tabsRef = useRef<WorkspaceTab[]>([]);
  tabsRef.current = tabs;
  const [remoteCmdHistory, setRemoteCmdHistory] = useState<string[]>([]);

  // In-app Notification Toast Banner & Native Alert Modal (replaces browser wails.localhost alert)
  const [liveToast, setLiveToast] = useState<{
    title: string;
    body: string;
  } | null>(null);
  const [appAlertModal, setAppAlertModal] = useState<{
    title: string;
    message: string;
    isError: boolean;
  } | null>(null);
  const showToast = (title: string, body: string) => {
    setLiveToast({ title, body });
    window.setTimeout(() => setLiveToast(null), 4500);
  };
  const [hostModalTarget, setHostModalTarget] = useState<
    HostProfile | "new" | null
  >(null);
  const [hostMenuId, setHostMenuId] = useState<string | null>(null);
  const [collapsedGroups, setCollapsedGroups] = useState<Record<string, boolean>>({});
  const mdFileInputRef = useRef<HTMLInputElement | null>(null);

  // Password Fallback Prompt Modal (for AuthMode="auto")
  const [pwPromptHost, setPwPromptHost] = useState<{
    hp: HostProfile;
    openSftpAfter: boolean;
    retry?: (password: string) => Promise<void>;
  } | null>(null);
  const [pwPromptInput, setPwPromptInput] = useState("");
  const [pwPromptRemember, setPwPromptRemember] = useState(true);
  const [pwPromptBusy, setPwPromptBusy] = useState(false);

  // Quick Connect & Quick Migrate Modals
  const [showQuickConnect, setShowQuickConnect] = useState(false);
  const [qcAddress, setQcAddress] = useState("");
  const [qcPassword, setQcPassword] = useState("");
  const [qcSaveHost, setQcSaveHost] = useState(true);
  const [qcBusy, setQcBusy] = useState(false);

  const [showQuickMigrate, setShowQuickMigrate] = useState(false);
  const [migrateMsg, setMigrateMsg] = useState<string | null>(null);

  // Remote Inspector Output (Processes / Docker)
  const [inspectorOutput, setInspectorOutput] = useState("");
  const [inspectorBusy, setInspectorBusy] = useState(false);

  // Connection History
  const [connHistory, setConnHistory] = useState<HistoryEntry[]>([]);

  // Snippets
  const [snippets, setSnippets] = useState<Snippet[]>([]);
  const [newSnipTitle, setNewSnipTitle] = useState("");
  const [newSnipCmd, setNewSnipCmd] = useState("");
  const paneSendersRef = useRef<Record<string, (txt: string) => void>>({});

  // Tailnet modal
  const [showTailnet, setShowTailnet] = useState(false);
  const [tsStatus, setTsStatus] = useState<TailnetStatus | null>(null);
  const [tsNodes, setTsNodes] = useState<TailnetNode[]>([]);
  const [tsControlUrl, setTsControlUrl] = useState("");
  const [tsAuthKey, setTsAuthKey] = useState("");
  const [tsBusy, setTsBusy] = useState(false);

  // Notifications & Session Logs ("会话日志" Pane)
  const [notifications, setNotifications] = useState<AppNotification[]>([]);
  const [hasUnreadAlert, setHasUnreadAlert] = useState(false);
  const [logsSubTab, setLogsSubTab] = useState<"logs" | "alerts">("logs");
  const [sessionLogs, setSessionLogs] = useState<SessionLogMeta[]>([]);
  const [selectedLogName, setSelectedLogName] = useState<string | null>(null);
  const [selectedLogContent, setSelectedLogContent] = useState("");
  const [logsLoading, setLogsLoading] = useState(false);

  const refreshSessionLogs = async () => {
    setLogsLoading(true);
    try {
      const list = await listSessionLogs();
      setSessionLogs(list);
    } catch {
      // Ignore
    } finally {
      setLogsLoading(false);
    }
  };

  useEffect(() => {
    const nativeAlert = window.alert;
    window.alert = (msg?: unknown) => {
      const text = String(msg ?? "");
      const isError = /失败|错误|异常|error|timeout|refused|denied|invalid/i.test(text);
      setAppAlertModal({
        title: isError ? "连接与操作异常" : "Shnex 提示",
        message: text,
        isError,
      });
    };
    return () => {
      window.alert = nativeAlert;
    };
  }, []);

  useEffect(() => {
    if (!appAlertModal) return;
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key === "Escape" || ev.key === "Enter") {
        ev.preventDefault();
        setAppAlertModal(null);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [appAlertModal]);

  useEffect(() => {
    if (openPanels.includes("notifications")) {
      refreshSessionLogs();
    }
  }, [openPanels]);

  useEffect(() => {
    (async () => {
      try {
        await initBridge();
        const [hList, sList, cfgData, tStat] = await Promise.all([
          listHosts(),
          listSnippets(),
          getSettings(),
          getTailnetStatus().catch(() => null),
        ]);
        setHosts(hList);
        setSnippets(sList);
        setAppSettings(cfgData);
        if (cfgData.general?.tray_icon_style === "mono") {
          setTrayStyle("mono");
        }
        if (tStat) {
          setTsStatus(tStat);
          if (tStat.running) {
            listTailnetNodes()
              .then(setTsNodes)
              .catch(() => {});
          }
        }
        setReady(true);

        // Restore layout tabs if "记忆布局" is enabled
        if (cfgData.general?.remember_layout) {
          try {
            const rawSaved = localStorage.getItem("shnex_saved_layout");
            if (rawSaved) {
              const items = JSON.parse(rawSaved) as {
                protocol?: string;
                localShellKey?: string;
                hostId?: string;
              }[];
              for (const it of items.slice(0, 6)) {
                if (it.protocol === "local") {
                  openLocalTerminal(it.localShellKey);
                } else if (it.hostId) {
                  const hp = hList.find((h) => h.id === it.hostId);
                  if (hp) {
                    openHostWorkspace(hp);
                  }
                }
              }
            }
          } catch {
            // Ignore corrupt layout cache
          }
        }
      } catch (e) {
        setBootErr((e as Error).message);
      }
    })();
  }, []);

  // Persist open tabs layout whenever tabs change and "记忆布局" is ON
  useEffect(() => {
    if (!ready || !appSettings?.general?.remember_layout) return;
    const minimal = tabs
      .filter((t) => t.protocol !== "markdown")
      .map((t) => ({
        protocol: t.protocol || "ssh",
        localShellKey: t.localShellKey,
        hostId: t.hostId,
      }));
    localStorage.setItem("shnex_saved_layout", JSON.stringify(minimal));
  }, [tabs, ready, appSettings?.general?.remember_layout]);

  useEffect(() => {
    localStorage.setItem("shnex_privacy_ip", privacyMode ? "1" : "0");
  }, [privacyMode]);

  useEffect(() => {
    if (!appSettings) return;
    const theme = appSettings.appearance.theme || "dark";
    document.documentElement.setAttribute("data-theme", theme);
    const uiPx = Math.max(
      11,
      Math.min(24, appSettings.appearance.ui_font_size || 13)
    );
    document.documentElement.style.fontSize = `${(uiPx / 0.75).toFixed(2)}px`;
  }, [appSettings?.appearance.theme, appSettings?.appearance.ui_font_size]);

  useEffect(() => {
    if (!ready) return;
    const timer = window.setInterval(async () => {
      try {
        const list = await listNotifications();
        if (list.length > notifications.length) {
          setHasUnreadAlert(true);
        }
        setNotifications(list);
      } catch {
        // Ignore
      }
    }, 4000);
    return () => window.clearInterval(timer);
  }, [ready, notifications.length]);

  const activeTab = tabs.find((t) => t.tabId === activeTabId) || null;
  const isActiveSSHTab = Boolean(
    activeTab &&
      activeTab.protocol !== "local" &&
      activeTab.protocol !== "markdown"
  );
  const primarySessionId = activeTab?.panes[0]?.sessionId ?? null;
  const totalConnections = tabs.reduce((acc, t) => acc + t.panes.length, 0);

  // Auto-load Processes, Docker, Tmux, Port Forwards, or Remote Command History when opened
  useEffect(() => {
    if (!primarySessionId || !isActiveSSHTab) return;
    if (openPanels.includes("processes")) {
      setInspectorBusy(true);
      execSessionCommand(
        primarySessionId,
        "ps aux --sort=-%mem 2>/dev/null | head -n 20 || ps -ef | head -n 20"
      )
        .then((r) => setInspectorOutput(r.stdout || r.stderr))
        .catch((e) => setInspectorOutput((e as Error).message))
        .finally(() => setInspectorBusy(false));
    }
    if (openPanels.includes("docker")) {
      setInspectorBusy(true);
      execSessionCommand(
        primarySessionId,
        "docker ps -a --format 'table {{.Names}}\t{{.Status}}\t{{.Image}}' 2>&1"
      )
        .then((r) => setInspectorOutput(r.stdout || r.stderr))
        .catch((e) => setInspectorOutput((e as Error).message))
        .finally(() => setInspectorBusy(false));
    }
    if (openPanels.includes("tunnels")) {
      listSessionForwards(primarySessionId)
        .then(setForwards)
        .catch(() => setForwards([]));
    }
    if (openPanels.includes("history")) {
      execSessionCommand(
        primarySessionId,
        "tail -n 35 ~/.bash_history ~/.zsh_history 2>/dev/null | sed 's/^: [0-9]*:[0-9]*;//' | grep -v '^==>' || true"
      )
        .then((r) => {
          const lines = Array.from(
            new Set(
              (r.stdout || "")
                .split("\n")
                .map((x) => x.trim())
                .filter(Boolean)
                .reverse()
            )
          );
          setRemoteCmdHistory(lines.slice(0, 30));
        })
        .catch(() => setRemoteCmdHistory([]));
    }
  }, [openPanels, primarySessionId]);

  const recordHistory = (title: string, userTarget: string, hostId?: string) => {
    setConnHistory((prev) => [
      {
        title,
        userTarget,
        hostId,
        connectedAt: new Date().toLocaleTimeString(),
      },
      ...prev.slice(0, 29),
    ]);
  };

  const newTabIds = () => {
    const sfx = `${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
    return { paneId: `pane-${sfx}`, tabId: `tab-${sfx}` };
  };

  const makeHostTab = (
    hp: HostProfile,
    ids: { paneId: string; tabId: string },
    sessionId: string,
    title: string,
    tmuxId?: string
  ): WorkspaceTab => ({
    tabId: ids.tabId,
    hostId: hp.id,
    tmuxId,
    title,
    icon: hp.icon,
    badgeText: hp.badge_text,
    color: hp.color,
    rawUserTarget: `${hp.user}@${hp.host}`,
    splitDir: "row",
    panes: [{ paneId: ids.paneId, sessionId }],
    focusedPaneId: ids.paneId,
    aiState: "idle",
    latestStats: null,
    sftpPath: hp.default_dir || "/",
    metricsBar: hp.metrics_bar || "follow",
    terminalEncoding: hp.terminal_encoding || "utf-8",
  });

  // Any connected tab of a tmux host can run the tmux API; prefer one not attached to avoidTabId.
  const tmuxControlSession = (hostId: string, avoidTabId?: string) => {
    const hostTabs = tabsRef.current.filter(
      (t) => t.hostId === hostId && t.tmuxId && t.panes[0]
    );
    const pick = hostTabs.find((t) => t.tabId !== avoidTabId) ?? hostTabs[0];
    return pick?.panes[0].sessionId;
  };

  const refreshRemoteTmux = async (hostId: string) => {
    const sid = tmuxControlSession(hostId);
    if (!sid) return;
    try {
      const list = await listRemoteTmuxSessions(sid);
      if (list.available) {
        setRemoteTmux((prev) => ({ ...prev, [hostId]: list.sessions }));
      }
    } catch {
      // keep the last known list
    }
  };

  // The tmux session a fresh connection attached to, or undefined when the host is not a tmux host.
  const detectTmuxAttach = async (
    summary: SessionSummary
  ): Promise<{ tmuxId: string; sessions: RemoteTmuxSession[] } | undefined> => {
    const ws = summary.workspace_name || "";
    if (
      (summary.relay_mode !== "tmux" && summary.relay_mode !== "auto") ||
      !/^[A-Za-z0-9_-]{1,48}$/.test(ws)
    ) {
      return undefined;
    }
    try {
      const list = await listRemoteTmuxSessions(summary.id);
      return list.available ? { tmuxId: ws, sessions: list.sessions } : undefined;
    } catch {
      return undefined;
    }
  };

  // Display order of a tmux host's conversations: remote list (by creation), then attached tabs not listed yet.
  const tmuxGroupIds = (hostId: string) => {
    const ids = (remoteTmux[hostId] || []).map((s) => s.id);
    for (const t of tabsRef.current) {
      if (t.hostId === hostId && t.tmuxId && !ids.includes(t.tmuxId)) ids.push(t.tmuxId);
    }
    return ids;
  };

  const removeTabLocally = (tabId: string) => {
    const tab = tabsRef.current.find((t) => t.tabId === tabId);
    if (!tab) return;
    for (const p of tab.panes) {
      delete paneSendersRef.current[p.paneId];
      deleteSession(p.sessionId).catch(() => {});
    }
    setTabs((prev) => prev.filter((x) => x.tabId !== tabId));
    setActiveTabId((cur) => {
      if (cur !== tabId) return cur;
      return tabsRef.current.find((x) => x.tabId !== tabId)?.tabId ?? null;
    });
  };

  // Lazily attach to an existing remote conversation (never creates it: must_exist).
  const attachRemoteTmux = async (
    hostId: string,
    remote: RemoteTmuxSession,
    password?: string
  ) => {
    const already = tabsRef.current.find(
      (t) => t.hostId === hostId && t.tmuxId === remote.id
    );
    if (already) {
      setActiveTabId(already.tabId);
      setShowSettings(false);
      return;
    }
    const hp = hosts.find((h) => h.id === hostId);
    if (!hp) return;
    const lockKey = `${hostId}/${remote.id}`;
    if (connectingLockRef.current.has(lockKey)) return;
    connectingLockRef.current.add(lockKey);
    try {
      const summary = await createSession({
        host_id: hostId,
        cols: 110,
        rows: 34,
        password: password ?? hostPasswordsRef.current[hostId],
        relay: { mode: "tmux", session_name: remote.id, must_exist: true },
      });
      if (password) hostPasswordsRef.current[hostId] = password;
      const ids = newTabIds();
      setTabs((prev) => [...prev, makeHostTab(hp, ids, summary.id, remote.title, remote.id)]);
      setActiveTabId(ids.tabId);
      setShowSettings(false);
    } catch (e) {
      if (e instanceof ApiError && e.code === "TMUX_SESSION_GONE") {
        setRemoteTmux((prev) => ({
          ...prev,
          [hostId]: (prev[hostId] || []).filter((s) => s.id !== remote.id),
        }));
        refreshRemoteTmux(hostId);
      } else if (e instanceof ApiError && e.code === "AUTH_PASSWORD_NEEDED") {
        delete hostPasswordsRef.current[hostId];
        setPwPromptInput("");
        setPwPromptHost({
          hp,
          openSftpAfter: false,
          retry: (pw) => attachRemoteTmux(hostId, remote, pw),
        });
      } else {
        alert(`打开对话「${remote.title}」失败: ${(e as Error).message}`);
      }
    } finally {
      connectingLockRef.current.delete(lockKey);
    }
  };

  // "+" on a tmux host: create a conversation on the remote host and attach to it.
  const newRemoteTmux = async (hostId: string) => {
    const sid = tmuxControlSession(hostId);
    if (!sid) return;
    try {
      const created = await createRemoteTmuxSession(sid);
      setRemoteTmux((prev) => ({
        ...prev,
        [hostId]: [...(prev[hostId] || []).filter((s) => s.id !== created.id), created],
      }));
      await attachRemoteTmux(hostId, created);
      refreshRemoteTmux(hostId);
    } catch (e) {
      alert(`新建对话失败: ${(e as Error).message}`);
    }
  };

  const startTmuxRename = (hostId: string, tmuxId: string, title: string) => {
    renameCancelRef.current = false;
    setRenamingTmux({ hostId, tmuxId, value: title });
  };

  const commitTmuxRename = async () => {
    const r = renamingTmux;
    const cancelled = renameCancelRef.current;
    renameCancelRef.current = false;
    setRenamingTmux(null);
    if (!r || cancelled) return;
    const title = r.value.trim();
    const current = (remoteTmux[r.hostId] || []).find((s) => s.id === r.tmuxId);
    if (!title || title === current?.title) return;
    const sid = tmuxControlSession(r.hostId);
    if (!sid) return;
    try {
      const res = await renameRemoteTmuxSession(sid, r.tmuxId, title);
      setRemoteTmux((prev) => ({
        ...prev,
        [r.hostId]: (prev[r.hostId] || []).map((s) =>
          s.id === r.tmuxId ? { ...s, title: res.title } : s
        ),
      }));
      setTabs((prev) =>
        prev.map((t) =>
          t.hostId === r.hostId && t.tmuxId === r.tmuxId ? { ...t, title: res.title } : t
        )
      );
    } catch (e) {
      alert(`改名失败: ${(e as Error).message}`);
    }
    refreshRemoteTmux(r.hostId);
  };

  // "×" on a tmux host ends the conversation on the remote host (switching away keeps it running).
  const endRemoteTmux = async (hostId: string, tmuxId: string) => {
    const tab = tabsRef.current.find((t) => t.hostId === hostId && t.tmuxId === tmuxId);
    const sid = tmuxControlSession(hostId, tab?.tabId);
    if (!sid) return;
    let info = (remoteTmux[hostId] || []).find((s) => s.id === tmuxId);
    try {
      const list = await listRemoteTmuxSessions(sid);
      info = list.sessions.find((s) => s.id === tmuxId) ?? info;
    } catch {
      // fall back to the last known state
    }
    const title = info?.title || tab?.title || tmuxId;
    if (info?.busy && !window.confirm(`「${title}」正在运行 ${info.command}，确定结束？`)) {
      return;
    }
    const order = tmuxGroupIds(hostId);
    const idx = order.indexOf(tmuxId);
    const rest = order.filter((id) => id !== tmuxId);
    const nextId = rest.length > 0 ? rest[Math.min(Math.max(idx, 0), rest.length - 1)] : undefined;
    const wasActive = Boolean(tab && tab.tabId === activeTabId);
    const hostHasOtherTab = tabsRef.current.some(
      (t) => t.hostId === hostId && t.tmuxId && t.tmuxId !== tmuxId
    );

    if (tab) endingTmuxTabsRef.current.add(tab.tabId);
    try {
      await killRemoteTmuxSession(sid, tmuxId);
    } catch {
      // killing the session this connection is attached to drops the connection; the refreshed list decides
    }
    if (tab) removeTabLocally(tab.tabId);
    setRemoteTmux((prev) => ({
      ...prev,
      [hostId]: (prev[hostId] || []).filter((s) => s.id !== tmuxId),
    }));

    if (nextId && (wasActive || !hostHasOtherTab)) {
      const nextTab = tabsRef.current.find((t) => t.hostId === hostId && t.tmuxId === nextId);
      const nextRemote = (remoteTmux[hostId] || []).find((s) => s.id === nextId);
      if (nextTab) {
        setActiveTabId(nextTab.tabId);
      } else if (nextRemote) {
        await attachRemoteTmux(hostId, nextRemote);
      }
    }
    window.setTimeout(() => refreshRemoteTmux(hostId), 800);
  };

  // Refresh every tmux host's conversation list when the window regains focus.
  useEffect(() => {
    const onFocus = () => {
      const hostIds = new Set(
        tabsRef.current.filter((t) => t.tmuxId && t.hostId).map((t) => t.hostId as string)
      );
      hostIds.forEach((h) => refreshRemoteTmux(h));
    };
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, []);

  // Top tab bar: a tmux host's tabs are its remote conversations (created order), attached or not.
  type TopTabItem =
    | { kind: "tab"; tab: WorkspaceTab; remote?: RemoteTmuxSession }
    | { kind: "ghost"; hostId: string; remote: RemoteTmuxSession };
  const topTabItems: TopTabItem[] = [];
  {
    const doneHosts = new Set<string>();
    for (const tab of tabs) {
      if (!tab.tmuxId || !tab.hostId) {
        topTabItems.push({ kind: "tab", tab });
        continue;
      }
      if (doneHosts.has(tab.hostId)) continue;
      doneHosts.add(tab.hostId);
      const hostTabs = tabs.filter((t) => t.hostId === tab.hostId && t.tmuxId);
      const remote = remoteTmux[tab.hostId] || [];
      for (const r of remote) {
        const attached = hostTabs.find((t) => t.tmuxId === r.id);
        topTabItems.push(
          attached
            ? { kind: "tab", tab: attached, remote: r }
            : { kind: "ghost", hostId: tab.hostId, remote: r }
        );
      }
      for (const t of hostTabs) {
        if (!remote.some((r) => r.id === t.tmuxId)) topTabItems.push({ kind: "tab", tab: t });
      }
    }
  }

  const renderTmuxRenameInput = () => (
    <input
      autoFocus
      value={renamingTmux?.value ?? ""}
      maxLength={40}
      onClick={(e) => e.stopPropagation()}
      onDoubleClick={(e) => e.stopPropagation()}
      onChange={(e) => {
        const value = e.target.value;
        setRenamingTmux((r) => (r ? { ...r, value } : r));
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.currentTarget.blur();
        } else if (e.key === "Escape") {
          renameCancelRef.current = true;
          e.currentTarget.blur();
        }
      }}
      onBlur={commitTmuxRename}
      className="w-[110px] px-1 rounded bg-[#0f1013] border border-[#3b82f6] text-white outline-none"
    />
  );

  const openHostWorkspace = async (
    hp: HostProfile,
    openSftpAfter = false,
    overridePassword?: string
  ) => {
    // A tmux host is already represented by its conversation tabs: focus them instead of a duplicate client.
    const existingTmuxTab = tabsRef.current.find((t) => t.hostId === hp.id && t.tmuxId);
    if (existingTmuxTab) {
      setActiveTabId(existingTmuxTab.tabId);
      setShowSettings(false);
      if (openSftpAfter) setOpenPanels(["sftp"]);
      refreshRemoteTmux(hp.id);
      return;
    }
    if (connectingLockRef.current.has(hp.id)) return;
    connectingLockRef.current.add(hp.id);
    setConnectingId(hp.id);
    try {
      const summary = await createSession({
        host_id: hp.id,
        cols: 110,
        rows: 34,
        password: overridePassword ?? hostPasswordsRef.current[hp.id],
      });
      if (overridePassword) hostPasswordsRef.current[hp.id] = overridePassword;
      const tmux = await detectTmuxAttach(summary);
      if (tmux) {
        const dup = tabsRef.current.find(
          (t) => t.hostId === hp.id && t.tmuxId === tmux.tmuxId
        );
        if (dup) {
          deleteSession(summary.id).catch(() => {});
          setActiveTabId(dup.tabId);
          setShowSettings(false);
          return;
        }
        setRemoteTmux((prev) => ({ ...prev, [hp.id]: tmux.sessions }));
        // the relay may create the default session just after connecting
        window.setTimeout(() => refreshRemoteTmux(hp.id), 2000);
      }
      const ids = newTabIds();
      const rawUT = `${hp.user}@${hp.host}`;
      const tabTitle = tmux
        ? tmux.sessions.find((s) => s.id === tmux.tmuxId)?.title || tmux.tmuxId
        : hp.name;
      setTabs((prev) => [...prev, makeHostTab(hp, ids, summary.id, tabTitle, tmux?.tmuxId)]);
      setActiveTabId(ids.tabId);
      setShowSettings(false);
      recordHistory(hp.name, rawUT, hp.id);
      listHosts().then(setHosts).catch(() => {});
      if (openSftpAfter) {
        setOpenPanels(["sftp"]);
      }
    } catch (e) {
      if (e instanceof ApiError && e.code === "AUTH_PASSWORD_NEEDED") {
        delete hostPasswordsRef.current[hp.id];
        setPwPromptInput("");
        setPwPromptHost({ hp, openSftpAfter });
      } else {
        alert(`连接 ${hp.name} 失败: ${(e as Error).message}`);
      }
    } finally {
      connectingLockRef.current.delete(hp.id);
      setConnectingId(null);
    }
  };

  const handlePasswordPromptSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!pwPromptHost || !pwPromptInput) return;
    const { hp, openSftpAfter, retry } = pwPromptHost;
    setPwPromptBusy(true);
    try {
      if (pwPromptRemember) {
        await upsertHost({
          id: hp.id,
          name: hp.name,
          protocol: hp.protocol,
          group: hp.group,
          host: hp.host,
          port: hp.port,
          user: hp.user,
          icon: hp.icon,
          badge_text: hp.badge_text,
          color: hp.color,
          auth_mode: "auto",
          credential_id: hp.credential_id,
          password: pwPromptInput,
          default_dir: hp.default_dir,
          file_protocol: hp.file_protocol,
          sftp_sudo: hp.sftp_sudo,
          tmux_session: hp.tmux_session,
          post_login_script: hp.post_login_script,
          metrics_bar: hp.metrics_bar,
          terminal_encoding: hp.terminal_encoding,
          term_type: hp.term_type,
          auto_session_log: hp.auto_session_log,
          host_key: hp.host_key,
          route: hp.route,
          relay: hp.relay,
        });
        setHosts(await listHosts());
      }
      setPwPromptHost(null);
      if (retry) {
        await retry(pwPromptInput);
      } else {
        await openHostWorkspace(hp, openSftpAfter, pwPromptInput);
      }
    } catch (ex) {
      alert((ex as Error).message);
    } finally {
      setPwPromptBusy(false);
    }
  };

  const handleQuickConnectSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    let expr = qcAddress.trim().replace(/^ssh\s+/i, "");
    let port = 22;
    const portMatch = expr.match(/-p\s+(\d+)/);
    if (portMatch) {
      port = Number(portMatch[1]);
      expr = expr.replace(/-p\s+\d+/, "").trim();
    }
    let user = "root";
    let host = expr;
    if (expr.includes("@")) {
      const [u, h] = expr.split("@");
      user = u.trim() || "root";
      host = h.trim();
    }
    if (host.includes(":")) {
      const [hOnly, pStr] = host.split(":");
      host = hOnly;
      port = Number(pStr) || 22;
    }
    if (!host) return;

    setQcBusy(true);
    try {
      const saved = await upsertHost({
        name: host,
        host,
        port,
        user,
        auth_mode: "password",
        password: qcPassword,
        host_key: { insecure_ignore: true },
        route: {},
        relay: {
          mode:
            (appSettings?.terminal.default_relay_mode as
              | "auto"
              | "tmux"
              | "none") || "auto",
          session_name: "shnex-main",
        },
      });
      if (!qcSaveHost) {
        // Connect first, then remove from permanent list if ephemeral requested
        await openHostWorkspace(saved);
        await deleteHost(saved.id);
      } else {
        setHosts(await listHosts());
        await openHostWorkspace(saved);
      }
      setShowQuickConnect(false);
      setQcAddress("");
      setQcPassword("");
    } catch (ex) {
      alert("快速连接失败: " + (ex as Error).message);
    } finally {
      setQcBusy(false);
    }
  };

  const handleImportSSHConfigClick = async () => {
    try {
      const res = await importLocalSSHConfig();
      setHosts(res.hosts);
      setMigrateMsg(
        res.imported_count > 0
          ? `已从 ~/.ssh/config 成功导入 ${res.imported_count} 台主机！`
          : "未在 ~/.ssh/config 中发现新的未导入主机。"
      );
    } catch (e) {
      setMigrateMsg("读取 ~/.ssh/config 失败: " + (e as Error).message);
    }
  };

  const handlePingHostsClick = async () => {
    if (hosts.length === 0 || pingingAll) return;
    setPingingAll(true);
    try {
      const map = await pingAllHosts();
      setHostLatencies(map);
    } finally {
      setPingingAll(false);
    }
  };

  const openLocalTerminal = async (shellOverride?: string) => {
    const shellKey =
      shellOverride || appSettings?.terminal.default_shell || "powershell";
    try {
      const summary = await createSession({
        protocol: "local",
        target: shellKey,
        user: "local",
        cols: 110,
        rows: 34,
      });
      const paneId = `pane-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
      const tabId = `tab-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
      const newTab: WorkspaceTab = {
        tabId,
        protocol: "local",
        localShellKey: shellKey,
        title: summary.target || "本地终端",
        icon: "code",
        color: "#3b82f6",
        rawUserTarget: "本机",
        splitDir: "row",
        panes: [{ paneId, sessionId: summary.id }],
        focusedPaneId: paneId,
        aiState: "idle",
        latestStats: null,
        sftpPath: "/",
        metricsBar: "off",
        terminalEncoding: "utf-8",
      };
      setTabs((prev) => [...prev, newTab]);
      setActiveTabId(tabId);
      setShowSettings(false);
    } catch (e) {
      alert(`打开本地终端失败: ${(e as Error).message}`);
    }
  };

  const openSampleMarkdownTab = async () => {
    const existing = tabs.find(
      (t) =>
        t.protocol === "markdown" && t.markdownData?.source.kind === "sample"
    );
    if (existing) {
      setActiveTabId(existing.tabId);
      setShowSettings(false);
      return;
    }
    try {
      const res = await getMarkdownSample();
      const tabId = `md-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
      const mdTab: WorkspaceTab = {
        tabId,
        protocol: "markdown",
        title: res.title,
        rawUserTarget: "原生文档",
        splitDir: "row",
        panes: [],
        focusedPaneId: "",
        aiState: "idle",
        latestStats: null,
        sftpPath: "/",
        markdownData: {
          content: res.content,
          doc: res.document,
          source: { kind: "sample" },
        },
      };
      setTabs((prev) => [...prev, mdTab]);
      setActiveTabId(tabId);
      setShowSettings(false);
    } catch (e) {
      alert(`打开公式排版示例失败: ${(e as Error).message}`);
    }
  };

  const triggerLocalMarkdownPicker = async () => {
    const dlg = await openNativeFileDialog("markdown", false);
    if (!dlg.supported) {
      mdFileInputRef.current?.click();
      return;
    }
    if (dlg.canceled || dlg.paths.length === 0) return;
    const selectedPath = dlg.paths[0];
    const existing = tabs.find(
      (t) =>
        t.protocol === "markdown" &&
        t.markdownData?.source.kind === "local" &&
        t.markdownData?.source.localPath === selectedPath
    );
    if (existing) {
      setActiveTabId(existing.tabId);
      setShowSettings(false);
      return;
    }
    try {
      const res = await readLocalMarkdownFile(selectedPath);
      const tabId = `md-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
      const mdTab: WorkspaceTab = {
        tabId,
        protocol: "markdown",
        title: res.title,
        rawUserTarget: res.path,
        splitDir: "row",
        panes: [],
        focusedPaneId: "",
        aiState: "idle",
        latestStats: null,
        sftpPath: "/",
        markdownData: {
          content: res.content,
          doc: res.document,
          source: { kind: "local", localPath: res.path },
        },
      };
      setTabs((prev) => [...prev, mdTab]);
      setActiveTabId(tabId);
      setShowSettings(false);
    } catch (e) {
      alert(`打开本地 Markdown 失败: ${(e as Error).message}`);
    }
  };

  const handleLocalMarkdownSelected = async (
    ev: React.ChangeEvent<HTMLInputElement>
  ) => {
    const file = ev.target.files?.[0];
    ev.target.value = "";
    if (!file) return;
    try {
      const content = await file.text();
      const doc = await renderMarkdownNative(content);
      const tabId = `md-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
      const maybePath = (file as File & { path?: string }).path;
      const mdTab: WorkspaceTab = {
        tabId,
        protocol: "markdown",
        title: file.name,
        rawUserTarget: maybePath || "本机文档",
        splitDir: "row",
        panes: [],
        focusedPaneId: "",
        aiState: "idle",
        latestStats: null,
        sftpPath: "/",
        markdownData: {
          content,
          doc,
          source: { kind: "local", localPath: maybePath },
        },
      };
      setTabs((prev) => [...prev, mdTab]);
      setActiveTabId(tabId);
      setShowSettings(false);
    } catch (e) {
      alert(`解析本地 Markdown 失败: ${(e as Error).message}`);
    }
  };

  const openSftpMarkdownTab = async (sessId: string, entry: FileEntry) => {
    const existing = tabs.find(
      (t) =>
        t.protocol === "markdown" &&
        t.markdownData?.source.kind === "sftp" &&
        t.markdownData?.source.sftpSessionId === sessId &&
        t.markdownData?.source.sftpPath === entry.path
    );
    if (existing) {
      setActiveTabId(existing.tabId);
      setShowSettings(false);
      return;
    }
    try {
      const res = await sftpRead(sessId, entry.path);
      const bin = atob(res.content_base64);
      const bytes = new Uint8Array(bin.length);
      for (let i = 0; i < bin.length; i++) {
        bytes[i] = bin.charCodeAt(i);
      }
      const content = new TextDecoder().decode(bytes);
      const doc = await renderMarkdownNative(content);
      const tabId = `md-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
      const mdTab: WorkspaceTab = {
        tabId,
        protocol: "markdown",
        title: entry.name,
        rawUserTarget: entry.path,
        splitDir: "row",
        panes: [],
        focusedPaneId: "",
        aiState: "idle",
        latestStats: null,
        sftpPath: "/",
        markdownData: {
          content,
          doc,
          source: {
            kind: "sftp",
            sftpSessionId: sessId,
            sftpPath: entry.path,
          },
        },
      };
      setTabs((prev) => [...prev, mdTab]);
      setActiveTabId(tabId);
      setShowSettings(false);
    } catch (e) {
      alert(`打开远端 Markdown 失败: ${(e as Error).message}`);
    }
  };

  const reconnectPane = async (
    tabId: string,
    paneId: string,
    oldSessionId: string,
    hostId?: string,
    rawUserTarget?: string,
    lastReportedDir?: string
  ) => {
    const targetTab = tabsRef.current.find((t) => t.tabId === tabId);
    if (endingTmuxTabsRef.current.has(tabId)) return;
    deleteSession(oldSessionId).catch(() => {});
    try {
      if (targetTab?.protocol === "local") {
        const summary = await createSession({
          protocol: "local",
          target: targetTab.localShellKey || "powershell",
          user: "local",
          cols: 110,
          rows: 34,
        });
        setTabs((prev) =>
          prev.map((t) =>
            t.tabId === tabId
              ? {
                  ...t,
                  panes: t.panes.map((p) =>
                    p.paneId === paneId ? { ...p, sessionId: summary.id } : p
                  ),
                }
              : t
          )
        );
        return;
      }

      const hp = hosts.find((h) => h.id === hostId);
      const tmuxId = targetTab?.tmuxId;
      const hasTmux =
        Boolean(tmuxId) || Boolean(hp?.tmux_session?.trim()) || hp?.relay?.mode === "tmux";
      const shouldRestoreDir =
        (appSettings?.terminal.restore_dir_on_reconnect ?? true) &&
        !hasTmux &&
        Boolean(lastReportedDir);

      const summary = await createSession({
        host_id: hostId,
        target: hostId ? undefined : rawUserTarget?.split("@")[1],
        user: hostId ? undefined : rawUserTarget?.split("@")[0],
        default_dir: shouldRestoreDir ? lastReportedDir : undefined,
        cols: 110,
        rows: 34,
        password: hostId ? hostPasswordsRef.current[hostId] : undefined,
        // the conversation pane re-attaches only if it still exists; split panes stay plain shells
        relay: tmuxId
          ? targetTab?.panes[0]?.paneId === paneId
            ? { mode: "tmux", session_name: tmuxId, must_exist: true }
            : { mode: "none" }
          : undefined,
      });
      setTabs((prev) =>
        prev.map((t) =>
          t.tabId === tabId
            ? {
                ...t,
                panes: t.panes.map((p) =>
                  p.paneId === paneId ? { ...p, sessionId: summary.id } : p
                ),
              }
            : t
        )
      );
    } catch (e) {
      if (e instanceof ApiError && e.code === "TMUX_SESSION_GONE" && targetTab?.hostId) {
        // ended on another device: drop the tab instead of resurrecting an empty conversation
        const goneHost = targetTab.hostId;
        const goneId = targetTab.tmuxId;
        removeTabLocally(tabId);
        setRemoteTmux((prev) => ({
          ...prev,
          [goneHost]: (prev[goneHost] || []).filter((s) => s.id !== goneId),
        }));
        window.setTimeout(() => refreshRemoteTmux(goneHost), 800);
        return;
      }
      alert(`重连失败: ${(e as Error).message}`);
    }
  };

  const splitActiveTab = async (dir: "row" | "col") => {
    if (!activeTab || activeTab.protocol === "markdown") return;
    try {
      const summary =
        activeTab.protocol === "local"
          ? await createSession({
              protocol: "local",
              target: activeTab.localShellKey || "powershell",
              user: "local",
              cols: 90,
              rows: 30,
            })
          : await createSession({
              host_id: activeTab.hostId,
              target: activeTab.hostId
                ? undefined
                : activeTab.rawUserTarget.split("@")[1],
              user: activeTab.hostId
                ? undefined
                : activeTab.rawUserTarget.split("@")[0],
              cols: 90,
              rows: 30,
              relay: { mode: "none" },
            });
      const newPaneId = `pane-${Date.now()}`;
      setTabs((prev) =>
        prev.map((t) =>
          t.tabId === activeTab.tabId
            ? {
                ...t,
                splitDir: dir,
                panes: [
                  ...t.panes,
                  { paneId: newPaneId, sessionId: summary.id },
                ],
                focusedPaneId: newPaneId,
              }
            : t
        )
      );
    } catch (e) {
      alert(`分屏连接失败: ${(e as Error).message}`);
    }
  };

  const closePane = async (tabId: string, paneId: string, sessionId: string) => {
    delete paneSendersRef.current[paneId];
    deleteSession(sessionId).catch(() => {});

    setTabs((prev) => {
      const next: WorkspaceTab[] = [];
      for (const t of prev) {
        if (t.tabId !== tabId) {
          next.push(t);
          continue;
        }
        const rem = t.panes.filter((p) => p.paneId !== paneId);
        if (rem.length > 0) {
          next.push({
            ...t,
            panes: rem,
            focusedPaneId:
              t.focusedPaneId === paneId ? rem[0].paneId : t.focusedPaneId,
          });
        }
      }
      if (!next.find((x) => x.tabId === activeTabId)) {
        setActiveTabId(next[0]?.tabId ?? null);
      }
      return next;
    });
  };

  const closeWholeTab = (tab: WorkspaceTab) => {
    if (
      tab.protocol !== "local" &&
      tab.protocol !== "markdown" &&
      (appSettings?.general?.confirm_before_close ?? true)
    ) {
      if (!window.confirm(`确认关闭远程会话标签页「${tab.title}」？`)) {
        return;
      }
    }
    for (const p of tab.panes) {
      delete paneSendersRef.current[p.paneId];
      deleteSession(p.sessionId).catch(() => {});
    }
    setTabs((prev) => {
      const rem = prev.filter((x) => x.tabId !== tab.tabId);
      if (activeTabId === tab.tabId) {
        setActiveTabId(rem[0]?.tabId ?? null);
      }
      return rem;
    });
  };

  const handleSaveHost = async (input: UpsertHostInput) => {
    await upsertHost(input);
    setHosts(await listHosts());
  };

  const handleDeleteHost = async (id: string) => {
    if (!window.confirm("确认删除该主机配置？")) return;
    await deleteHost(id);
    setHosts(await listHosts());
  };

  const toggleRightTool = (tool: ActivePanelKey) => {
    if (tool === "notifications") setHasUnreadAlert(false);
    setShowSettings(false);
    const sideBySide = appSettings?.general?.right_bar_mode === "side_by_side";
    setOpenPanels((cur) => {
      if (cur.includes(tool)) {
        return cur.filter((x) => x !== tool);
      }
      if (!sideBySide) {
        return [tool];
      }
      return [...cur, tool];
    });
  };

  const handleTailnetUp = async () => {
    setTsBusy(true);
    try {
      const st = await tailnetUp({
        hostname: "shnex-desktop",
        control_url: tsControlUrl.trim(),
        auth_key: tsAuthKey.trim(),
      });
      setTsStatus(st);
      if (st.running) {
        setTsNodes(await listTailnetNodes());
      }
    } catch (e) {
      alert((e as Error).message);
    } finally {
      setTsBusy(false);
    }
  };

  const importTailnetNodeAsHost = async (n: TailnetNode) => {
    const addr = n.magic_dns_name || n.tailscale_ips?.[0] || n.hostname;
    const saved = await upsertHost({
      name: n.hostname,
      host: addr,
      port: 22,
      user: "root",
      auth_mode: n.supports_tailscale_ssh ? "tailscale_ssh" : "password",
      host_key: { insecure_ignore: true },
      route: { use_tsnet: true },
      relay: { mode: "auto", session_name: "shnex-main" },
    });
    setHosts(await listHosts());
    setShowTailnet(false);
    setHostModalTarget(saved);
  };

  const toggleBottomMonitorBar = async () => {
    if (!appSettings) return;
    const next: AppSettings = JSON.parse(JSON.stringify(appSettings));
    next.monitor.show_bottom_bar = !next.monitor.show_bottom_bar;
    setAppSettings(next);
    setAppSettings(await saveSettings(next));
  };

  const filteredHosts = hosts.filter(
    (h) =>
      h.name.toLowerCase().includes(searchHost.toLowerCase()) ||
      h.host.toLowerCase().includes(searchHost.toLowerCase()) ||
      h.user.toLowerCase().includes(searchHost.toLowerCase())
  );

  if (bootErr) {
    return (
      <div className="h-screen w-screen flex items-center justify-center bg-[#16171a] text-[#f87171] text-sm p-8">
        Shnex 本地核心桥接初始化失败: {bootErr}
      </div>
    );
  }

  if (!ready) {
    return (
      <div className="h-screen w-screen flex items-center justify-center bg-[#16171a] text-[#9ca3af] text-xs">
        正在启动 Shnex 本地安全核心...
      </div>
    );
  }

  const maskedActiveUserTarget = activeTab
    ? maskTargetAddress(activeTab.rawUserTarget, privacyMode)
    : "";

  const hasCustomWallpaper = Boolean(appSettings?.appearance?.has_wallpaper);
  const wallpaperDimAlpha =
    Math.max(15, Math.min(90, appSettings?.appearance?.wallpaper_dim ?? 52)) /
    100;

  return (
    <div className="relative h-screen w-screen flex flex-col bg-[#18191c] text-[#e5e7eb] text-xs overflow-hidden">
      {/* ==================== 1. TOP FRAMELESS TITLEBAR & WORKSPACE TABS ==================== */}
      <header className="relative z-20 h-10 bg-[#1f2023] border-b border-[#2b2d33] flex items-center justify-between shrink-0 wails-drag">
        <div className="flex items-center h-full min-w-0">
          {/* App Logo + Title (Click to return to Welcome view without closing tabs) */}
          <button
            onClick={() => {
              setActiveTabId(null);
              setShowSettings(false);
            }}
            className="px-4 h-full flex items-center gap-2.5 shrink-0 hover:bg-[#272930]/60 transition wails-no-drag"
            title="回到欢迎首页"
          >
            <svg
              viewBox="0 0 100 100"
              className="w-6 h-6 shrink-0 drop-shadow-sm"
              aria-hidden="true"
            >
              <defs>
                <linearGradient id="shnexBgTop" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="#1c1e26" />
                  <stop offset="100%" stopColor="#101116" />
                </linearGradient>
              </defs>
              <rect
                x="5"
                y="5"
                width="90"
                height="90"
                rx="22"
                fill="url(#shnexBgTop)"
                stroke="#f59e0b"
                strokeOpacity="0.88"
                strokeWidth="4.5"
              />
              <path
                d="M26 33 L48 50 L26 67"
                fill="none"
                stroke="#fbbf24"
                strokeWidth="10.8"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
              <line
                x1="54"
                y1="66"
                x2="76"
                y2="66"
                stroke="#3fb950"
                strokeWidth="9.8"
                strokeLinecap="round"
              />
            </svg>
            <span className="text-sm font-semibold tracking-tight text-[#f3f4f6]">
              Shnex
            </span>
          </button>

          {/* Workspace Tabs */}
          <div
            className={
              !showSettings
                ? "flex items-center gap-1 px-1.5 h-full overflow-x-auto wails-no-drag"
                : "hidden"
            }
          >
            {topTabItems.map((item) => {
              if (item.kind === "ghost") {
                // a conversation on the remote host not opened here yet: dim, attaches on click
                const ghostHost = hosts.find((h) => h.id === item.hostId);
                const renaming =
                  renamingTmux?.hostId === item.hostId &&
                  renamingTmux.tmuxId === item.remote.id;
                return (
                  <div
                    key={`ghost-${item.hostId}-${item.remote.id}`}
                    onClick={() => attachRemoteTmux(item.hostId, item.remote)}
                    onDoubleClick={() =>
                      startTmuxRename(item.hostId, item.remote.id, item.remote.title)
                    }
                    title={
                      item.remote.busy
                        ? `${item.remote.title} · 正在运行 ${item.remote.command}`
                        : item.remote.title
                    }
                    className="h-7 px-3 rounded-md flex items-center gap-2 cursor-pointer border border-transparent text-[#5d626d] hover:bg-[#272930] hover:text-[#aeb3bd]"
                  >
                    <span className="flex opacity-50">
                      <HostBadge
                        icon={ghostHost?.icon}
                        badgeText={ghostHost?.badge_text}
                        color={ghostHost?.color}
                        size="xs"
                      />
                    </span>
                    {renaming ? (
                      renderTmuxRenameInput()
                    ) : (
                      <span className="font-medium truncate max-w-[130px]">
                        {item.remote.title}
                      </span>
                    )}
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        endRemoteTmux(item.hostId, item.remote.id);
                      }}
                      className="p-0.5 rounded hover:bg-[#2e313b] text-[#6b7280] hover:text-white"
                      title="结束对话"
                    >
                      <X size={12} />
                    </button>
                  </div>
                );
              }
              const tab = item.tab;
              const isAct = tab.tabId === activeTabId;
              const isMd = tab.protocol === "markdown";
              const extraCount = isMd
                ? 0
                : tab.panes.length - 1 + (isAct && isPanelOpen("ai") ? 1 : 0);
              return (
                <div
                  key={tab.tabId}
                  onClick={() => {
                    setActiveTabId(tab.tabId);
                    setShowSettings(false);
                  }}
                  onDoubleClick={() => {
                    if (tab.tmuxId && tab.hostId) {
                      startTmuxRename(tab.hostId, tab.tmuxId, item.remote?.title ?? tab.title);
                    }
                  }}
                  className={`h-7 px-3 rounded-md flex items-center gap-2 cursor-pointer border transition-colors ${
                    isAct && !showSettings
                      ? "bg-[#141518] border-[#31343d] text-white"
                      : "bg-transparent border-transparent text-[#8b909a] hover:bg-[#272930] hover:text-[#d1d5db]"
                  }`}
                >
                  {isMd ? (
                    <span className="px-1 py-0.5 rounded bg-[#f3f4f6] text-[#111827] font-mono font-extrabold text-[9.5px] leading-none shrink-0">
                      M↓
                    </span>
                  ) : (
                    <HostBadge
                      icon={tab.icon}
                      badgeText={tab.badgeText}
                      color={tab.color}
                      size="xs"
                    />
                  )}
                  {renamingTmux &&
                  renamingTmux.hostId === tab.hostId &&
                  renamingTmux.tmuxId === tab.tmuxId ? (
                    renderTmuxRenameInput()
                  ) : (
                    <span className="font-medium truncate max-w-[130px]">
                      {item.remote?.title ?? tab.title}
                      {extraCount > 0 ? ` +${extraCount}` : ""}
                    </span>
                  )}
                  {!isMd && (
                    <span
                      className={`w-1.5 h-1.5 rounded-full ${
                        tab.aiState === "approval"
                          ? "bg-[#f59e0b] animate-ping"
                          : "bg-[#3fb950]"
                      }`}
                    />
                  )}
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      if (tab.tmuxId && tab.hostId) {
                        endRemoteTmux(tab.hostId, tab.tmuxId);
                      } else {
                        closeWholeTab(tab);
                      }
                    }}
                    className="p-0.5 rounded hover:bg-[#2e313b] text-[#8b909a] hover:text-white"
                    title={tab.tmuxId ? "结束对话" : undefined}
                  >
                    <X size={12} />
                  </button>
                </div>
              );
            })}

            {/* Hidden Native File Picker for Local Markdown Documents */}
            <input
              ref={mdFileInputRef}
              type="file"
              accept=".md,.markdown,.mdown,.mdx,.txt"
              className="hidden"
              onChange={handleLocalMarkdownSelected}
            />

            {tabs.length > 0 && (
              <button
                onClick={() => {
                  // on a tmux host "+" is a new conversation on that host
                  if (activeTab?.tmuxId && activeTab.hostId) {
                    newRemoteTmux(activeTab.hostId);
                  } else {
                    setShowQuickConnect(true);
                  }
                }}
                className="p-1.5 rounded hover:bg-[#272930] text-[#8b909a] hover:text-white"
                title={activeTab?.tmuxId ? "新建对话" : "快速打开远程 SSH 连接"}
              >
                <Plus size={14} />
              </button>
            )}
          </div>
        </div>

        {/* Top-Right Icon Cluster + Windows Controls (Matches Screenshot 5!) */}
        <div className="flex items-center h-full wails-no-drag text-[#9ca3af]">
          <button
            onClick={() => setShowTailnet(true)}
            className={`w-7 h-7 mx-0.5 rounded-lg flex items-center justify-center transition ${
              tsStatus?.running
                ? "text-[#4ade80] bg-[#3fb950]/10"
                : "hover:bg-[#2a2c33] hover:text-white"
            }`}
            title="Tailscale / Headscale 组网"
          >
            <Cloud size={15} />
          </button>

          <button
            onClick={() => {
              setSettingsInitTab("about");
              setShowSettings(true);
            }}
            className="w-7 h-7 mx-0.5 rounded-lg flex items-center justify-center hover:bg-[#2a2c33] hover:text-white transition"
            title="开源项目与架构信息"
          >
            <Github size={14} />
          </button>

          <button
            onClick={() => toggleRightTool("notifications")}
            className="w-7 h-7 mx-0.5 rounded-lg flex items-center justify-center hover:bg-[#2a2c33] hover:text-white relative transition"
            title="消息与审批通知"
          >
            <MessageSquare size={14} />
            {hasUnreadAlert && (
              <span className="absolute top-1 right-1 w-1.5 h-1.5 rounded-full bg-[#f59e0b]" />
            )}
          </button>

          <button
            onClick={() => setShowSettings((v) => !v)}
            className={`w-7 h-7 mx-1 rounded-lg flex items-center justify-center transition ${
              showSettings
                ? "bg-[#2563eb] text-white"
                : "hover:bg-[#2a2c33] hover:text-white"
            }`}
            title="全局设置"
          >
            <Settings size={14} />
          </button>

          <span className="h-4 w-[1px] bg-[#2e3037] mx-1" />

          <button
            onClick={() => windowAction("minimize")}
            className="h-full px-3.5 hover:bg-[#2a2c33] hover:text-white flex items-center"
            title="最小化"
          >
            <Minus size={14} />
          </button>
          <button
            onClick={() => windowAction("maximize")}
            className="h-full px-3.5 hover:bg-[#2a2c33] hover:text-white flex items-center"
            title="最大化/还原"
          >
            <Square size={11} />
          </button>
          <button
            onClick={() =>
              windowAction(
                appSettings?.general?.close_action === "quit" ? "quit" : "hide"
              )
            }
            className="h-full px-3.5 hover:bg-[#e81123] hover:text-white flex items-center"
            title={
              appSettings?.general?.close_action === "quit"
                ? "退出程序"
                : "隐藏到系统托盘"
            }
          >
            <X size={15} />
          </button>
        </div>
      </header>

      {/* Floating Notification Toast */}
      {liveToast && (
        <div className="fixed top-12 right-5 z-50 w-80 p-3.5 rounded-xl bg-[#21232b] border border-[#3b82f6]/60 shadow-2xl space-y-1 text-xs">
          <div className="font-semibold text-white flex items-center justify-between">
            <span>{liveToast.title}</span>
            <button
              onClick={() => setLiveToast(null)}
              className="text-[#8b909a] hover:text-white"
            >
              <X size={13} />
            </button>
          </div>
          <div className="text-[#b0b5c0] leading-relaxed">{liveToast.body}</div>
        </div>
      )}

      {/* ==================== 2. SETTINGS VIEW, CONNECTION HUB, OR WORKBENCH ==================== */}
      {showSettings && appSettings && (
        <SettingsView
          settings={appSettings}
          initialTab={settingsInitTab}
          onChange={setAppSettings}
          onOpenTailnetModal={() => setShowTailnet(true)}
          onRefreshHosts={async () => {
            setHosts(await listHosts());
            setAppSettings(await getSettings());
          }}
          onShowToast={showToast}
          onWallpaperChanged={() => setWallpaperRev(Date.now())}
        />
      )}

      <div
        className={
          showSettings ? "hidden" : "relative z-10 flex-1 flex min-h-0"
        }
      >
        {/* 2A. Left Host Sidebar (Resizable + Spacious Host Cards!) */}
        {sidebarOpen && (
          <aside
            style={{ width: sidebarWidth }}
            className="relative bg-[#1f2023] border-r border-[#2b2d33] flex flex-col justify-between shrink-0 select-none"
          >
            <div className="p-3 space-y-3 overflow-y-auto flex-1">
              {/* Compact Action Icon Row: [☑] [🔍] ... [⏲] [⤢] [+] */}
              <div className="flex items-center justify-between px-0.5 text-[#8b909a]">
                <div className="flex items-center gap-1">
                  <button
                    onClick={() => {
                      setBatchMode((v) => !v);
                      setSelectedHostIds([]);
                    }}
                    className={`p-1.5 rounded-lg hover:bg-[#2b2d34] ${
                      batchMode ? "text-[#3b82f6]" : "hover:text-white"
                    }`}
                    title="批量管理主机"
                  >
                    <CheckSquare size={16} />
                  </button>
                  <button
                    onClick={() => setSearchBarOpen((v) => !v)}
                    className={`p-1.5 rounded-lg hover:bg-[#2b2d34] ${
                      searchBarOpen ? "text-white bg-[#2b2d34]" : "hover:text-white"
                    }`}
                    title="搜索主机"
                  >
                    <Search size={16} />
                  </button>
                </div>

                <div className="flex items-center gap-1">
                  <button
                    onClick={handlePingHostsClick}
                    className={`p-1.5 rounded-lg hover:bg-[#2b2d34] ${
                      pingingAll
                        ? "text-[#3fb950] animate-pulse"
                        : "hover:text-white"
                    }`}
                    title="测试全部主机连接延迟"
                  >
                    <Gauge size={16} />
                  </button>
                  <button
                    onClick={() => setSearchHost("")}
                    className="p-1.5 rounded-lg hover:bg-[#2b2d34] hover:text-white"
                    title="展开全部"
                  >
                    <ChevronsUpDown size={16} />
                  </button>
                  <button
                    onClick={() => setHostModalTarget("new")}
                    className="p-1.5 rounded-lg hover:bg-[#2b2d34] text-[#d1d5db] hover:text-white"
                    title="新建主机"
                  >
                    <Plus size={17} />
                  </button>
                </div>
              </div>

              {/* Expandable Search Input */}
              {(searchBarOpen || hosts.length > 4) && (
                <div className="px-2.5 py-1.5 rounded-lg bg-[#27292f] border border-[#32353e] flex items-center gap-1.5">
                  <Search size={13} className="text-[#7c828e] shrink-0" />
                  <input
                    value={searchHost}
                    onChange={(e) => setSearchHost(e.target.value)}
                    placeholder="搜索主机..."
                    className="w-full bg-transparent outline-none text-[#e5e7eb] placeholder:text-[#7c828e]"
                    autoFocus={searchBarOpen}
                  />
                </div>
              )}

              {/* Batch Delete Action Bar */}
              {batchMode && selectedHostIds.length > 0 && (
                <div className="px-2.5 py-1.5 rounded-lg bg-[#ef4444]/15 border border-[#ef4444]/35 flex items-center justify-between">
                  <span className="text-[#fca5a5]">
                    已选 {selectedHostIds.length} 台
                  </span>
                  <button
                    onClick={async () => {
                      if (!window.confirm(`删除选中的 ${selectedHostIds.length} 台主机？`))
                        return;
                      for (const id of selectedHostIds) {
                        await deleteHost(id);
                      }
                      setSelectedHostIds([]);
                      setBatchMode(false);
                      setHosts(await listHosts());
                    }}
                    className="text-[#f87171] font-medium hover:underline"
                  >
                    批量删除
                  </button>
                </div>
              )}

              {/* Empty State vs Saved Hosts List */}
              {hosts.length === 0 ? (
                <div className="px-1.5 pt-3 space-y-1.5">
                  <div className="text-sm font-semibold text-white">
                    还没有主机
                  </div>
                  <div className="text-[#8b909a] leading-relaxed">
                    添加一台主机以通过 SSH 连接。
                  </div>
                </div>
              ) : (
                <div className="space-y-2">
                  {Array.from(
                    new Set(filteredHosts.map((h) => h.group || ""))
                  ).map((grpName) => {
                    const groupItems = filteredHosts.filter(
                      (h) => (h.group || "") === grpName
                    );
                    const isCollapsed = grpName
                      ? !!collapsedGroups[grpName]
                      : false;

                    return (
                      <div key={grpName || "__ungrouped"} className="space-y-1">
                        {grpName && (
                          <div
                            onClick={() => {
                              setCollapsedGroups((p) => ({
                                ...p,
                                [grpName]: !p[grpName],
                              }));
                            }}
                            className="px-1 text-[11px] font-medium text-[#7c828e] flex items-center gap-1 cursor-pointer hover:text-white"
                          >
                            {isCollapsed ? (
                              <ChevronRight size={12} />
                            ) : (
                              <ChevronDown size={12} />
                            )}
                            <span>{grpName}</span>
                          </div>
                        )}

                        {!isCollapsed &&
                          groupItems.map((hp) => {
                            const openTabsForHost = tabs.filter(
                              (t) => t.hostId === hp.id
                            );
                            const isConnected = openTabsForHost.length > 0;
                            const needsApproval = openTabsForHost.some(
                              (t) => t.aiState === "approval"
                            );
                            const isSelected = activeTab?.hostId === hp.id;
                            const isConnecting = connectingId === hp.id;
                            const lat = hostLatencies[hp.id];

                            return (
                              <div key={hp.id} className="relative group">
                                <div
                                  onClick={() => {
                                    if (batchMode) {
                                      setSelectedHostIds((prev) =>
                                        prev.includes(hp.id)
                                          ? prev.filter((x) => x !== hp.id)
                                          : [...prev, hp.id]
                                      );
                                      return;
                                    }
                                    if (openTabsForHost[0]) {
                                      setActiveTabId(openTabsForHost[0].tabId);
                                    } else {
                                      openHostWorkspace(hp);
                                    }
                                  }}
                                  className={`relative overflow-hidden px-3 py-2.5 rounded-xl flex items-center justify-between gap-2 cursor-pointer transition-all ${
                                    isConnecting
                                      ? "bg-[#252a36] text-white ring-1 ring-[#3b82f6]/45 shadow-sm"
                                      : isSelected
                                      ? "bg-[#2c2f37] text-white shadow-sm"
                                      : "hover:bg-[#26282f] text-[#d1d5db]"
                                  }`}
                                >
                                  <div className="flex items-center gap-3 min-w-0 flex-1">
                                    {batchMode && (
                                      <input
                                        type="checkbox"
                                        checked={selectedHostIds.includes(
                                          hp.id
                                        )}
                                        readOnly
                                      />
                                    )}
                                    <div className="relative shrink-0">
                                      <HostBadge
                                        icon={hp.icon}
                                        badgeText={hp.badge_text}
                                        color={hp.color}
                                        size="md"
                                      />
                                      {isConnecting && (
                                        <span className="pointer-events-none absolute -inset-1 rounded-[14px] border-2 border-[#60a5fa]/30 border-t-[#60a5fa] animate-spin" />
                                      )}
                                      {!isConnecting && isConnected && (
                                        <span
                                          className={`absolute -top-0.5 -left-0.5 w-2.5 h-2.5 rounded-full border-2 border-[#1f2023] ${
                                            needsApproval
                                              ? "bg-[#f59e0b]"
                                              : "bg-[#3fb950]"
                                          }`}
                                        />
                                      )}
                                    </div>
                                    <div className="min-w-0 flex-1">
                                      <div className="flex items-center gap-2 min-w-0">
                                        <span className="text-sm font-semibold text-[#f3f4f6] leading-snug truncate">
                                          {hp.name}
                                        </span>
                                        {isConnecting && (
                                          <span
                                            className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-full bg-[#3b82f6]/15 shrink-0"
                                            aria-label="正在连接"
                                          >
                                            <svg
                                              className="w-3 h-3 text-[#60a5fa] animate-spin"
                                              viewBox="0 0 24 24"
                                              fill="none"
                                            >
                                              <circle
                                                cx="12"
                                                cy="12"
                                                r="9"
                                                stroke="currentColor"
                                                strokeOpacity="0.25"
                                                strokeWidth="3"
                                              />
                                              <path
                                                d="M21 12a9 9 0 0 0-9-9"
                                                stroke="currentColor"
                                                strokeWidth="3"
                                                strokeLinecap="round"
                                              />
                                            </svg>
                                            <span
                                              style={{ animationDelay: "0ms" }}
                                              className="w-1 h-1 rounded-full bg-[#60a5fa] shnex-wave-dot"
                                            />
                                            <span
                                              style={{ animationDelay: "150ms" }}
                                              className="w-1 h-1 rounded-full bg-[#60a5fa] shnex-wave-dot"
                                            />
                                            <span
                                              style={{ animationDelay: "300ms" }}
                                              className="w-1 h-1 rounded-full bg-[#60a5fa] shnex-wave-dot"
                                            />
                                          </span>
                                        )}
                                      </div>
                                      <div className="text-xs text-[#9198a6] font-mono mt-0.5 truncate">
                                        {maskTargetAddress(
                                          `${hp.user}@${hp.host}`,
                                          privacyMode
                                        )}
                                        {lat !== undefined && (
                                          <span
                                            className={`ml-1.5 ${
                                              lat >= 0
                                                ? "text-[#3fb950]"
                                                : "text-[#f87171]"
                                            }`}
                                          >
                                            {lat >= 0 ? `${lat}ms` : "超时"}
                                          </span>
                                        )}
                                      </div>
                                    </div>
                                  </div>

                                  <div
                                    onClick={(e) => e.stopPropagation()}
                                    className={`${
                                      hostMenuId === hp.id
                                        ? "flex"
                                        : "hidden group-hover:flex"
                                    } items-center gap-0.5 shrink-0 text-[#9ca3af]`}
                                  >
                                    <button
                                      onClick={() => {
                                        if (openTabsForHost[0]) {
                                          setActiveTabId(
                                            openTabsForHost[0].tabId
                                          );
                                          setOpenPanels(["sftp"]);
                                        } else {
                                          openHostWorkspace(hp, true);
                                        }
                                      }}
                                      className="p-1.5 rounded-lg hover:bg-[#353945] hover:text-white"
                                      title="打开 SFTP"
                                    >
                                      <FolderOpen size={14} />
                                    </button>
                                    <button
                                      onClick={() =>
                                        setHostMenuId((cur) =>
                                          cur === hp.id ? null : hp.id
                                        )
                                      }
                                      className="p-1.5 rounded-lg hover:bg-[#353945] hover:text-white"
                                      title="更多"
                                    >
                                      <MoreHorizontal size={15} />
                                    </button>
                                  </div>

                                  {isConnecting && (
                                    <div className="pointer-events-none absolute inset-x-0 bottom-0 h-[2px] bg-[#3b82f6]/15 overflow-hidden">
                                      <div className="w-2/5 h-full bg-gradient-to-r from-transparent via-[#60a5fa] to-transparent shnex-shimmer-bar" />
                                    </div>
                                  )}
                                </div>

                                {hostMenuId === hp.id && (
                                  <div
                                    onMouseLeave={() => setHostMenuId(null)}
                                    className="absolute right-2 top-10 z-30 w-32 rounded-lg bg-[#252830] border border-[#343844] shadow-xl py-1"
                                  >
                                    <button
                                      onClick={() => {
                                        setHostMenuId(null);
                                        openHostWorkspace(hp);
                                      }}
                                      className="w-full px-3 py-1.5 text-left hover:bg-[#313540] flex items-center gap-2"
                                    >
                                      <Plus size={12} /> 新开工作区
                                    </button>
                                    <button
                                      onClick={() => {
                                        setHostMenuId(null);
                                        setHostModalTarget(hp);
                                      }}
                                      className="w-full px-3 py-1.5 text-left hover:bg-[#313540] flex items-center gap-2"
                                    >
                                      <Pencil size={12} /> 编辑主机
                                    </button>
                                    <button
                                      onClick={() => {
                                        setHostMenuId(null);
                                        handleDeleteHost(hp.id);
                                      }}
                                      className="w-full px-3 py-1.5 text-left hover:bg-[#ef4444]/20 text-[#f87171] flex items-center gap-2"
                                    >
                                      <Trash2 size={12} /> 删除
                                    </button>
                                  </div>
                                )}
                              </div>
                            );
                          })}
                      </div>
                    );
                  })}
                </div>
              )}

              {tsNodes.length > 0 && (
                <div className="space-y-1 pt-2 border-t border-[#2b2d33]">
                  <div className="px-1 text-[11px] font-medium text-[#4ade80]">
                    Tailnet 在线节点
                  </div>
                  {tsNodes.map((n) => (
                    <div
                      key={n.id}
                      onClick={() => importTailnetNodeAsHost(n)}
                      className="px-2.5 py-1.5 rounded-lg hover:bg-[#26282f] flex items-center justify-between cursor-pointer text-[#d1d5db]"
                    >
                      <span className="truncate font-mono">{n.hostname}</span>
                      <span className="text-[10px] text-[#3fb950]">{n.os}</span>
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Drag-to-Resize Handle on Right Edge of Host Sidebar */}
            <div
              onDoubleClick={() => {
                setSidebarWidth(300);
                localStorage.setItem("shnex_sidebar_width", "300");
              }}
              onMouseDown={(ev) => {
                ev.preventDefault();
                const startX = ev.clientX;
                const startW = sidebarWidth;
                const onMove = (me: MouseEvent) => {
                  const next = Math.max(
                    240,
                    Math.min(520, startW + (me.clientX - startX))
                  );
                  setSidebarWidth(next);
                  localStorage.setItem("shnex_sidebar_width", String(next));
                };
                const onUp = () => {
                  window.removeEventListener("mousemove", onMove);
                  window.removeEventListener("mouseup", onUp);
                };
                window.addEventListener("mousemove", onMove);
                window.addEventListener("mouseup", onUp);
              }}
              title="拖拽调节侧栏宽度（双击恢复默认）"
              className="absolute top-0 right-0 w-1.5 h-full cursor-col-resize hover:bg-[#3b82f6]/45 active:bg-[#3b82f6] transition-colors z-20"
            />
          </aside>
        )}

        {/* 2B. Center Stage (Dot-Matrix Welcome Screen OR Active Split Panes!) */}
        <main className="flex-1 flex min-w-0 min-h-0 bg-[#18191c]">
          {!activeTab ? (
            <div className="relative flex-1 shnex-dot-grid flex flex-col items-center justify-center p-8 text-center select-none overflow-hidden">
              {hasCustomWallpaper && (
                <>
                  <div
                    style={{
                      backgroundImage: `url("${buildWallpaperUrl(wallpaperRev)}")`,
                    }}
                    className="pointer-events-none absolute inset-0 bg-cover bg-center scale-[1.01]"
                  />
                  <div
                    style={{
                      backgroundColor: `rgba(20, 21, 24, ${wallpaperDimAlpha})`,
                    }}
                    className="pointer-events-none absolute inset-0"
                  />
                </>
              )}

              {(() => {
                const connectingHost = connectingId
                  ? hosts.find((h) => h.id === connectingId)
                  : null;
                if (connectingHost) {
                  return (
                    <div className="relative z-10 flex flex-col items-center">
                      <div className="relative mb-5 flex items-center justify-center">
                        <span className="pointer-events-none absolute -inset-3 rounded-[22px] border border-[#60a5fa]/25 animate-ping" />
                        <span className="pointer-events-none absolute -inset-2 rounded-[20px] border-2 border-[#60a5fa]/20 border-t-[#60a5fa] animate-spin" />
                        <HostBadge
                          icon={connectingHost.icon}
                          badgeText={connectingHost.badge_text}
                          color={connectingHost.color}
                          size="lg"
                        />
                      </div>
                      <div className="text-base font-semibold text-white mb-1.5">
                        {connectingHost.name}
                      </div>
                      <div className="text-xs font-mono text-[#9198a6] mb-4">
                        {maskTargetAddress(
                          `${connectingHost.user}@${connectingHost.host}`,
                          privacyMode
                        )}
                      </div>
                      <div className="w-40 h-1 rounded-full bg-[#2b2e38] overflow-hidden">
                        <div className="w-2/5 h-full rounded-full bg-gradient-to-r from-transparent via-[#60a5fa] to-transparent shnex-shimmer-bar" />
                      </div>
                    </div>
                  );
                }
                return (
                  <div className="relative z-10 flex flex-col items-center">
                <svg
                  viewBox="0 0 100 100"
                  className="w-16 h-16 drop-shadow-2xl mb-4"
                  aria-hidden="true"
                >
                  <defs>
                    <linearGradient id="shnexBgHero" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="#1c1e26" />
                      <stop offset="100%" stopColor="#101116" />
                    </linearGradient>
                  </defs>
                  <rect
                    x="5"
                    y="5"
                    width="90"
                    height="90"
                    rx="22"
                    fill="url(#shnexBgHero)"
                    stroke="#f59e0b"
                    strokeOpacity="0.88"
                    strokeWidth="3.2"
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

                <h1 className="text-xl font-bold text-white tracking-tight mb-2">
                  欢迎使用 Shnex
                </h1>
                <p className="text-sm text-[#9198a6] mb-7">
                  在左侧选择一台主机以打开会话。
                </p>

                {/* 3 Pill Action Buttons */}
                <div className="flex items-center gap-3">
                  <button
                    onClick={() => {
                      setMigrateMsg(null);
                      setShowQuickMigrate(true);
                    }}
                    className="px-5 py-2 rounded-full bg-[#1d7dfa] hover:bg-[#358bfa] text-sm text-white font-medium transition shadow-sm"
                  >
                    快速迁移
                  </button>
                  <button
                    onClick={() => setShowQuickConnect(true)}
                    className="px-5 py-2 rounded-full bg-[#2b2d32] hover:bg-[#353840] border border-[#363940] text-sm text-[#e5e7eb] font-medium transition"
                  >
                    快速连接
                  </button>
                  <button
                    onClick={() => setHostModalTarget("new")}
                    className="px-5 py-2 rounded-full bg-[#2b2d32] hover:bg-[#353840] border border-[#363940] text-sm text-[#e5e7eb] font-medium transition"
                  >
                    新建主机
                  </button>
                </div>
              </div>
                );
              })()}
            </div>
          ) : (
            <div className="flex-1 flex p-1.5 gap-1.5 min-w-0 min-h-0">
              {tabs.map((tabItem) => {
                const isTabVisible = tabItem.tabId === activeTab.tabId;
                if (tabItem.protocol === "markdown" && tabItem.markdownData) {
                  return (
                    <div
                      key={tabItem.tabId}
                      className={
                        isTabVisible
                          ? "flex-1 flex min-w-0 min-h-0"
                          : "hidden"
                      }
                    >
                      <MarkdownTabView
                        title={tabItem.title}
                        initialContent={tabItem.markdownData.content}
                        initialDoc={tabItem.markdownData.doc}
                        source={tabItem.markdownData.source}
                        onReloadRemote={
                          tabItem.markdownData.source.kind === "sftp" &&
                          tabItem.markdownData.source.sftpSessionId &&
                          tabItem.markdownData.source.sftpPath
                            ? async () => {
                                const sId =
                                  tabItem.markdownData!.source.sftpSessionId!;
                                const sPath =
                                  tabItem.markdownData!.source.sftpPath!;
                                const r = await sftpRead(sId, sPath);
                                const bin = atob(r.content_base64);
                                const bytes = new Uint8Array(bin.length);
                                for (let i = 0; i < bin.length; i++) {
                                  bytes[i] = bin.charCodeAt(i);
                                }
                                const txt = new TextDecoder().decode(bytes);
                                const compiled = await renderMarkdownNative(txt);
                                return { content: txt, doc: compiled };
                              }
                            : undefined
                        }
                        onOpenLocalPicker={triggerLocalMarkdownPicker}
                        onOpenMathDemo={openSampleMarkdownTab}
                        onClose={() => closeWholeTab(tabItem)}
                      />
                    </div>
                  );
                }

                const tabMaskedTarget =
                  tabItem.protocol === "local"
                    ? "本机"
                    : maskTargetAddress(tabItem.rawUserTarget, privacyMode);
                return (
                  <div
                    key={tabItem.tabId}
                    className={
                      isTabVisible
                        ? `flex-1 flex ${
                            tabItem.splitDir === "col" ? "flex-col" : "flex-row"
                          } gap-1.5 min-w-0 min-h-0`
                        : "hidden"
                    }
                  >
                    {tabItem.panes.map((pane) => (
                      <TerminalPane
                        key={pane.paneId}
                        paneId={pane.paneId}
                        sessionId={pane.sessionId}
                        protocol={tabItem.protocol}
                        hostTitle={tabItem.title}
                        userTarget={tabMaskedTarget}
                        focused={
                          isTabVisible &&
                          tabItem.focusedPaneId === pane.paneId &&
                          !isPanelOpen("ai")
                        }
                        terminalCfg={appSettings?.terminal}
                        monitorCfg={appSettings?.monitor}
                        hostMetricsBar={
                          hosts.find((h) => h.id === tabItem.hostId)
                            ?.metrics_bar ?? tabItem.metricsBar
                        }
                        terminalEncoding={
                          hosts.find((h) => h.id === tabItem.hostId)
                            ?.terminal_encoding ?? tabItem.terminalEncoding
                        }
                        effectiveTerm={
                          hosts.find((h) => h.id === tabItem.hostId)
                            ?.term_type ||
                          appSettings?.terminal.term_type ||
                          "xterm-256color"
                        }
                        themeMode={appSettings?.appearance.theme}
                        forceComposerOpen={globalComposerOpen}
                        onBroadcastData={
                          broadcastMode && tabItem.panes.length > 1
                            ? (txt) => {
                                for (const p of tabItem.panes) {
                                  paneSendersRef.current[p.paneId]?.(txt);
                                }
                              }
                            : undefined
                        }
                        onFocus={() =>
                          setTabs((prev) =>
                            prev.map((t) =>
                              t.tabId === tabItem.tabId
                                ? { ...t, focusedPaneId: pane.paneId }
                                : t
                            )
                          )
                        }
                        onSplitHorizontal={() => splitActiveTab("row")}
                        onSplitVertical={() => splitActiveTab("col")}
                        onOpenSFTP={() => toggleRightTool("sftp")}
                        onReconnect={(lastDir) =>
                          reconnectPane(
                            tabItem.tabId,
                            pane.paneId,
                            pane.sessionId,
                            tabItem.hostId,
                            tabItem.rawUserTarget,
                            lastDir
                          )
                        }
                        onClosePane={() =>
                          closePane(tabItem.tabId, pane.paneId, pane.sessionId)
                        }
                        onStatsUpdate={(st) =>
                          setTabs((prev) =>
                            prev.map((t) =>
                              t.tabId === tabItem.tabId
                                ? {
                                    ...t,
                                    latestStats: st,
                                    cpuHistory: [
                                      ...(t.cpuHistory || []).slice(-23),
                                      st.cpu_percent,
                                    ],
                                  }
                                : t
                            )
                          )
                        }
                        onAIStateChange={(aiState) =>
                          setTabs((prev) =>
                            prev.map((t) =>
                              t.tabId === tabItem.tabId ? { ...t, aiState } : t
                            )
                          )
                        }
                        registerSender={(pid, fn) => {
                          paneSendersRef.current[pid] = fn;
                        }}
                      />
                    ))}
                  </div>
                );
              })}

              {isPanelOpen("ai") && primarySessionId && appSettings && (
                <AIPane
                  hostId={activeTab.hostId}
                  hostTitle={activeTab.title}
                  userTarget={maskedActiveUserTarget}
                  primarySessionId={primarySessionId}
                  settings={appSettings}
                  onUpdateSettings={setAppSettings}
                  onOpenAISettings={() => {
                    setSettingsInitTab("ai");
                    setShowSettings(true);
                  }}
                  onClose={() => closePanel("ai")}
                  onStatsUpdate={(st) =>
                    setTabs((prev) =>
                      prev.map((t) =>
                        t.tabId === activeTab.tabId
                          ? { ...t, latestStats: st }
                          : t
                      )
                    )
                  }
                />
              )}
            </div>
          )}

          {/* Right Slide-in / Side-by-Side Drawers ("右侧栏工具：替换显示 / 并排显示") */}
          {openPanels.filter((p) => p !== "ai").length > 0 && (
            <div
              className={`h-full flex shrink-0 border-l border-[#282a32] ${
                openPanels.filter((p) => p !== "ai").length > 3
                  ? "flex-wrap max-w-[920px] overflow-y-auto"
                  : ""
              }`}
            >
              {activeTab && isPanelOpen("monitor") && (
                <SystemMonitorDrawer
                  hostTitle={activeTab.title}
                  userTarget={maskedActiveUserTarget}
                  stats={activeTab.latestStats}
                  onClose={() => closePanel("monitor")}
                />
              )}

              {activeTab && isPanelOpen("sftp") && primarySessionId && (
                <SFTPDrawer
                  sessionId={primarySessionId}
                  initialPath={activeTab.sftpPath}
                  showHidden={appSettings?.files.show_hidden ?? true}
                  onPathChange={(p) =>
                    setTabs((prev) =>
                      prev.map((t) =>
                        t.tabId === activeTab.tabId ? { ...t, sftpPath: p } : t
                      )
                    )
                  }
                  onOpenMarkdown={(entry) =>
                    openSftpMarkdownTab(primarySessionId, entry)
                  }
                  onClose={() => closePanel("sftp")}
                />
              )}

              {activeTab && isPanelOpen("processes") && (
                <div className="w-[300px] h-full bg-[#1b1c20] border-l border-[#282a32] flex flex-col shrink-0">
                  <div className="h-9 px-3 border-b border-[#282a32] flex items-center justify-between">
                    <span className="font-semibold">占用 (Top 内存进程)</span>
                    <button
                      onClick={() => closePanel("processes")}
                      className="p-1 rounded hover:bg-[#2a2d36] text-[#8b909a]"
                    >
                      <X size={14} />
                    </button>
                  </div>
                  <div className="flex-1 overflow-y-auto p-3 font-mono text-[11px] text-[#d1d5db]">
                    {inspectorBusy ? (
                      <div className="text-[#8b909a]">正在拉取远端数据...</div>
                    ) : (
                      <pre className="whitespace-pre-wrap">{inspectorOutput}</pre>
                    )}
                  </div>
                </div>
              )}

              {activeTab && isPanelOpen("hostinfo") && (
                <div className="w-[270px] h-full bg-[#1b1c20] border-l border-[#282a32] flex flex-col shrink-0">
                  <div className="h-9 px-3 border-b border-[#282a32] flex items-center justify-between">
                    <span className="font-semibold">主机详情</span>
                    <button
                      onClick={() => closePanel("hostinfo")}
                      className="p-1 rounded hover:bg-[#2a2d36] text-[#8b909a]"
                    >
                      <X size={14} />
                    </button>
                  </div>
                  <div className="flex-1 overflow-y-auto p-3 space-y-2 text-xs text-[#d1d5db]">
                    <div>主机名称：{activeTab.title}</div>
                    <div>目标地址：{maskedActiveUserTarget}</div>
                    <div>活跃分屏：{activeTab.panes.length} 个</div>
                    <div>当前路径：{activeTab.sftpPath}</div>
                    <div>广播指令：{broadcastMode ? "已开启" : "未开启"}</div>
                  </div>
                </div>
              )}

              {activeTab && isPanelOpen("docker") && (
                <div className="w-[310px] h-full bg-[#1b1c20] border-l border-[#282a32] flex flex-col shrink-0">
                  <div className="h-9 px-3 border-b border-[#282a32] flex items-center justify-between">
                    <span className="font-semibold">Docker 管理</span>
                    <button
                      onClick={() => closePanel("docker")}
                      className="p-1 rounded hover:bg-[#2a2d36] text-[#8b909a]"
                    >
                      <X size={14} />
                    </button>
                  </div>
                  <div className="flex-1 overflow-y-auto p-3 font-mono text-[11px] text-[#d1d5db]">
                    {inspectorBusy ? (
                      <div className="text-[#8b909a]">正在拉取容器列表...</div>
                    ) : (
                      <pre className="whitespace-pre-wrap">{inspectorOutput}</pre>
                    )}
                  </div>
                </div>
              )}

              {activeTab && isPanelOpen("tunnels") && primarySessionId && (
                <div className="w-[300px] h-full bg-[#1b1c20] border-l border-[#282a32] flex flex-col shrink-0">
                  <div className="h-9 px-3 border-b border-[#282a32] flex items-center justify-between">
                    <span className="font-semibold">新建端口转发 (-L)</span>
                    <button
                      onClick={() => closePanel("tunnels")}
                      className="p-1 rounded hover:bg-[#2a2d36] text-[#8b909a]"
                    >
                      <X size={14} />
                    </button>
                  </div>
                  <div className="p-2.5 space-y-2 border-b border-[#282a32]">
                    <div className="grid grid-cols-3 gap-1.5">
                      <input
                        type="number"
                        value={newFwdLocalPort}
                        onChange={(e) => setNewFwdLocalPort(e.target.value)}
                        placeholder="本地端口"
                        className="px-2 py-1 rounded bg-[#141519] border border-[#2d3039] font-mono outline-none"
                      />
                      <input
                        value={newFwdRemoteAddr}
                        onChange={(e) => setNewFwdRemoteAddr(e.target.value)}
                        placeholder="127.0.0.1:3000"
                        className="col-span-2 px-2 py-1 rounded bg-[#141519] border border-[#2d3039] font-mono outline-none"
                      />
                    </div>
                    <button
                      onClick={async () => {
                        try {
                          await startSessionForward(
                            primarySessionId,
                            Number(newFwdLocalPort) || 0,
                            newFwdRemoteAddr.trim()
                          );
                          setForwards(
                            await listSessionForwards(primarySessionId)
                          );
                        } catch (e) {
                          alert("创建端口转发失败: " + (e as Error).message);
                        }
                      }}
                      className="w-full py-1.5 rounded bg-[#2563eb] hover:bg-[#3b82f6] text-white font-medium"
                    >
                      添加本地端口转发
                    </button>
                  </div>
                  <div className="flex-1 overflow-y-auto p-2.5 space-y-1.5">
                    {forwards.length === 0 ? (
                      <div className="text-center text-[#6b7280] py-8">
                        当前会话暂无活跃端口转发
                      </div>
                    ) : (
                      forwards.map((f) => (
                        <div
                          key={f.id}
                          className="p-2.5 rounded-lg bg-[#23252c] border border-[#2e313b] flex items-center justify-between gap-2"
                        >
                          <div className="font-mono text-[11px] truncate">
                            <div className="text-[#4ade80]">{f.local_addr}</div>
                            <div className="text-[#8b909a]">
                              → {f.remote_addr}
                            </div>
                          </div>
                          <button
                            onClick={async () => {
                              await stopSessionForward(primarySessionId, f.id);
                              setForwards(
                                await listSessionForwards(primarySessionId)
                              );
                            }}
                            className="p-1 rounded hover:bg-[#ef4444]/20 text-[#9ca3af] hover:text-[#f87171]"
                            title="停止转发"
                          >
                            <Trash2 size={13} />
                          </button>
                        </div>
                      ))
                    )}
                  </div>
                </div>
              )}

              {isPanelOpen("snippets") && (
                <div className="w-[280px] h-full bg-[#1b1c20] border-l border-[#282a32] flex flex-col shrink-0">
                  <div className="h-9 px-3 border-b border-[#282a32] flex items-center justify-between">
                    <span className="font-semibold">快捷指令</span>
                    <button
                      onClick={() => closePanel("snippets")}
                      className="p-1 rounded hover:bg-[#2a2d36] text-[#8b909a]"
                    >
                      <X size={14} />
                    </button>
                  </div>
                  <div className="p-2.5 space-y-2 border-b border-[#282a32]">
                    <input
                      value={newSnipTitle}
                      onChange={(e) => setNewSnipTitle(e.target.value)}
                      placeholder="指令名称"
                      className="w-full px-2 py-1 rounded bg-[#141519] border border-[#2d3039] outline-none"
                    />
                    <div className="flex gap-1.5">
                      <input
                        value={newSnipCmd}
                        onChange={(e) => setNewSnipCmd(e.target.value)}
                        placeholder="命令内容..."
                        className="flex-1 px-2 py-1 rounded bg-[#141519] border border-[#2d3039] font-mono outline-none"
                      />
                      <button
                        onClick={async () => {
                          if (!newSnipTitle.trim() || !newSnipCmd.trim()) return;
                          const list = await upsertSnippet({
                            title: newSnipTitle.trim(),
                            command: newSnipCmd.endsWith("\n")
                              ? newSnipCmd
                              : newSnipCmd + "\n",
                          });
                          setSnippets(list);
                          setNewSnipTitle("");
                          setNewSnipCmd("");
                        }}
                        className="px-2.5 py-1 rounded bg-[#2563eb] text-white"
                      >
                        添加
                      </button>
                    </div>
                  </div>
                  <div className="flex-1 overflow-y-auto p-2 space-y-1.5">
                    {snippets.map((sn) => (
                      <div
                        key={sn.id}
                        onClick={() => {
                          if (!activeTab) return;
                          paneSendersRef.current[activeTab.focusedPaneId]?.(
                            sn.command
                          );
                        }}
                        className="p-2.5 rounded-lg bg-[#23252c] border border-[#2e313b] hover:border-[#3b82f6] cursor-pointer flex items-center justify-between gap-2 group"
                      >
                        <div className="min-w-0">
                          <div className="font-medium text-[#e5e7eb] truncate">
                            {sn.title}
                          </div>
                          <div className="text-[11px] text-[#8b909a] font-mono truncate">
                            {sn.command.trim()}
                          </div>
                        </div>
                        {sn.id && (
                          <button
                            onClick={async (e) => {
                              e.stopPropagation();
                              setSnippets(await deleteSnippet(sn.id!));
                            }}
                            className="opacity-0 group-hover:opacity-100 p-1 rounded hover:bg-[#ef4444]/20 text-[#9ca3af] hover:text-[#f87171]"
                          >
                            <Trash2 size={12} />
                          </button>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {isPanelOpen("history") && (
                <div className="w-[275px] h-full bg-[#1b1c20] border-l border-[#282a32] flex flex-col shrink-0">
                  <div className="h-9 px-3 border-b border-[#282a32] flex items-center justify-between">
                    <span className="font-semibold">历史指令与连接</span>
                    <button
                      onClick={() => closePanel("history")}
                      className="p-1 rounded hover:bg-[#2a2d36] text-[#8b909a]"
                    >
                      <X size={14} />
                    </button>
                  </div>
                  <div className="flex-1 overflow-y-auto p-2.5 space-y-2">
                    {remoteCmdHistory.length > 0 && (
                      <div className="space-y-1">
                        <div className="text-[10px] text-[#7c828e] px-1">
                          远端历史指令 (点击发送)
                        </div>
                        {remoteCmdHistory.map((cmd, idx) => (
                          <div
                            key={idx}
                            onClick={() => {
                              if (!activeTab) return;
                              paneSendersRef.current[activeTab.focusedPaneId]?.(
                                cmd + "\n"
                              );
                            }}
                            className="px-2.5 py-1.5 rounded-lg bg-[#23252c] border border-[#2e313b] hover:border-[#3b82f6] cursor-pointer font-mono text-[11px] text-[#e5e7eb] truncate"
                            title={cmd}
                          >
                            {cmd}
                          </div>
                        ))}
                      </div>
                    )}

                    <div className="space-y-1">
                      <div className="text-[10px] text-[#7c828e] px-1">
                        最近连接主机
                      </div>
                      {connHistory.length === 0 ? (
                        <div className="text-center text-[#6b7280] py-6">
                          本次运行尚无连接记录
                        </div>
                      ) : (
                        connHistory.map((item, i) => (
                          <div
                            key={i}
                            onClick={() => {
                              const hp = hosts.find((h) => h.id === item.hostId);
                              if (hp) openHostWorkspace(hp);
                            }}
                            className="p-2.5 rounded-lg bg-[#23252c] border border-[#2e313b] hover:border-[#3b82f6] cursor-pointer"
                          >
                            <div className="flex justify-between font-medium">
                              <span>{item.title}</span>
                              <span className="text-[10px] text-[#7c828e]">
                                {item.connectedAt}
                              </span>
                            </div>
                            <div className="text-[11px] text-[#8b909a] font-mono mt-0.5">
                              {maskTargetAddress(item.userTarget, privacyMode)}
                            </div>
                          </div>
                        ))
                      )}
                    </div>
                  </div>
                </div>
              )}

              {isPanelOpen("notifications") && (
                <div className="w-[330px] h-full bg-[#1b1c20] border-l border-[#282a32] flex flex-col shrink-0">
                  <div className="h-9 px-3 border-b border-[#282a32] flex items-center justify-between shrink-0">
                    <div className="flex items-center gap-1 p-0.5 rounded-lg bg-[#141519]">
                      <button
                        onClick={() => setLogsSubTab("logs")}
                        className={`px-2.5 py-0.5 rounded-md font-medium transition ${
                          logsSubTab === "logs"
                            ? "bg-[#25272e] text-white"
                            : "text-[#8b909a] hover:text-white"
                        }`}
                      >
                        会话日志
                      </button>
                      <button
                        onClick={() => {
                          setLogsSubTab("alerts");
                          setHasUnreadAlert(false);
                        }}
                        className={`px-2.5 py-0.5 rounded-md font-medium transition ${
                          logsSubTab === "alerts"
                            ? "bg-[#25272e] text-white"
                            : "text-[#8b909a] hover:text-white"
                        }`}
                      >
                        通知提醒
                      </button>
                    </div>
                    <div className="flex items-center gap-1 text-[#8b909a]">
                      {logsSubTab === "logs" && (
                        <button
                          onClick={refreshSessionLogs}
                          className="p-1 rounded hover:bg-[#2a2d36] hover:text-white"
                          title="刷新会话日志"
                        >
                          <RefreshCw
                            size={13}
                            className={logsLoading ? "animate-spin" : ""}
                          />
                        </button>
                      )}
                      <button
                        onClick={() => closePanel("notifications")}
                        className="p-1 rounded hover:bg-[#2a2d36] text-[#8b909a]"
                      >
                        <X size={14} />
                      </button>
                    </div>
                  </div>

                  {logsSubTab === "logs" ? (
                    selectedLogName ? (
                      <div className="flex-1 flex flex-col min-h-0">
                        <div className="px-3 py-2 border-b border-[#282a32] flex items-center justify-between gap-2">
                          <button
                            onClick={() => {
                              setSelectedLogName(null);
                              setSelectedLogContent("");
                            }}
                            className="text-[#60a5fa] hover:underline shrink-0"
                          >
                            ← 返回列表
                          </button>
                          <button
                            onClick={() => {
                              const blob = new Blob([selectedLogContent], {
                                type: "text/plain;charset=utf-8",
                              });
                              const url = URL.createObjectURL(blob);
                              const a = document.createElement("a");
                              a.href = url;
                              a.download = selectedLogName;
                              a.click();
                              URL.revokeObjectURL(url);
                            }}
                            className="flex items-center gap-1 px-2 py-0.5 rounded bg-[#25272e] hover:bg-[#31343d] text-[#e5e7eb]"
                          >
                            <Download size={12} />
                            <span>导出</span>
                          </button>
                        </div>
                        <pre className="flex-1 overflow-auto p-3 text-[11px] font-mono text-[#d1d5db] whitespace-pre-wrap break-words bg-[#131418]">
                          {selectedLogContent || "（日志暂无输出内容）"}
                        </pre>
                      </div>
                    ) : (
                      <div className="flex-1 overflow-y-auto p-2.5 space-y-1.5">
                        {sessionLogs.length === 0 ? (
                          <div className="text-center text-[#6b7280] py-8 px-3 leading-relaxed">
                            暂无已记录的终端会话日志。
                            <div className="mt-1 text-[11px]">
                              可在「新建/编辑主机 → 会话日志」勾选自动记录。
                            </div>
                          </div>
                        ) : (
                          sessionLogs.map((lg) => (
                            <div
                              key={lg.name}
                              onClick={async () => {
                                try {
                                  const res = await getSessionLog(lg.name);
                                  setSelectedLogName(lg.name);
                                  setSelectedLogContent(res.content);
                                } catch (e) {
                                  alert((e as Error).message);
                                }
                              }}
                              className="p-2.5 rounded-lg bg-[#23252c] border border-[#2e313b] hover:border-[#3b82f6] cursor-pointer flex items-center justify-between gap-2 group"
                            >
                              <div className="min-w-0">
                                <div className="flex items-center gap-1.5 font-medium text-[#e5e7eb] truncate">
                                  {lg.active && (
                                    <span
                                      className="w-1.5 h-1.5 rounded-full bg-[#3fb950] shrink-0"
                                      title="录制中"
                                    />
                                  )}
                                  <span className="truncate">
                                    {lg.host_label}
                                  </span>
                                </div>
                                <div className="text-[10px] text-[#8b909a] font-mono truncate mt-0.5">
                                  {formatBytes(lg.size_bytes)}{" "}
                                  {lg.name.slice(0, 15)}
                                </div>
                              </div>
                              <button
                                onClick={async (e) => {
                                  e.stopPropagation();
                                  await deleteSessionLog(lg.name);
                                  await refreshSessionLogs();
                                }}
                                className="opacity-0 group-hover:opacity-100 p-1 rounded hover:bg-[#ef4444]/20 text-[#9ca3af] hover:text-[#f87171]"
                                title="删除日志"
                              >
                                <Trash2 size={12} />
                              </button>
                            </div>
                          ))
                        )}
                      </div>
                    )
                  ) : (
                    <div className="flex-1 overflow-y-auto p-2.5 space-y-2">
                      {notifications.length === 0 ? (
                        <div className="text-center text-[#6b7280] py-8">
                          暂无历史通知
                        </div>
                      ) : (
                        notifications.map((n, idx) => (
                          <div
                            key={idx}
                            className={`p-2.5 rounded-lg border space-y-1 ${
                              n.category === "action_required"
                                ? "bg-[#252119] border-[#f59e0b]/50"
                                : "bg-[#23252c] border-[#2e313b]"
                            }`}
                          >
                            <div className="font-medium text-[#f3f4f6]">
                              {n.title}
                            </div>
                            <div className="text-[11px] text-[#9ca3af] font-mono break-words">
                              {n.body}
                            </div>
                          </div>
                        ))
                      )}
                    </div>
                  )}
                </div>
              )}
            </div>
          )}
        </main>

        {/* 2C. Far-Right 36px 14-Item Activity Rail (Controlled by 右侧栏显示项!) */}
        <div className="w-9 bg-[#1f2023] border-l border-[#2b2d33] py-2 flex flex-col items-center justify-between shrink-0 text-[#8b909a]">
          <div className="flex flex-col items-center gap-1 w-full">
            {/* Group 1: 系统监控 / 占用 / 主机详情 */}
            {(appSettings?.general?.right_rail_items?.monitor ?? true) && (
              <button
                disabled={!isActiveSSHTab}
                onClick={() => toggleRightTool("monitor")}
                className={`w-7 h-7 rounded-lg flex items-center justify-center transition disabled:opacity-35 ${
                  isPanelOpen("monitor")
                    ? "bg-[#2563eb] text-white"
                    : "hover:bg-[#2a2c33] hover:text-white"
                }`}
                title="系统监控"
              >
                <Activity size={14} />
              </button>
            )}
            {(appSettings?.general?.right_rail_items?.processes ?? true) && (
              <button
                disabled={!isActiveSSHTab}
                onClick={() => toggleRightTool("processes")}
                className={`w-7 h-7 rounded-lg flex items-center justify-center transition disabled:opacity-35 ${
                  isPanelOpen("processes")
                    ? "bg-[#2563eb] text-white"
                    : "hover:bg-[#2a2c33] hover:text-white"
                }`}
                title="占用"
              >
                <AlignLeft size={14} />
              </button>
            )}
            {(appSettings?.general?.right_rail_items?.host_info ?? true) && (
              <button
                disabled={!isActiveSSHTab}
                onClick={() => toggleRightTool("hostinfo")}
                className={`w-7 h-7 rounded-lg flex items-center justify-center transition disabled:opacity-35 ${
                  isPanelOpen("hostinfo")
                    ? "bg-[#2563eb] text-white"
                    : "hover:bg-[#2a2c33] hover:text-white"
                }`}
                title="主机详情"
              >
                <Info size={14} />
              </button>
            )}

            <div className="w-5 h-[1px] bg-[#2d3038] my-1" />

            {/* Group 2: 文件 / Docker 管理 / tmux 会话 / AI助理 / 新建端口转发 */}
            {(appSettings?.general?.right_rail_items?.files ?? true) && (
              <button
                disabled={!isActiveSSHTab}
                onClick={() => toggleRightTool("sftp")}
                className={`w-7 h-7 rounded-lg flex items-center justify-center transition disabled:opacity-35 ${
                  isPanelOpen("sftp")
                    ? "bg-[#2563eb] text-white"
                    : "hover:bg-[#2a2c33] hover:text-white"
                }`}
                title="文件"
              >
                <FolderOpen size={14} />
              </button>
            )}
            {(appSettings?.general?.right_rail_items?.docker ?? true) && (
              <button
                disabled={!isActiveSSHTab}
                onClick={() => toggleRightTool("docker")}
                className={`w-7 h-7 rounded-lg flex items-center justify-center transition disabled:opacity-35 ${
                  isPanelOpen("docker")
                    ? "bg-[#2563eb] text-white"
                    : "hover:bg-[#2a2c33] hover:text-white"
                }`}
                title="Docker 管理"
              >
                <Container size={14} />
              </button>
            )}
            {(appSettings?.general?.right_rail_items?.ai ?? true) && (
              <button
                disabled={!isActiveSSHTab}
                onClick={() => toggleRightTool("ai")}
                className={`w-7 h-7 rounded-lg flex items-center justify-center transition disabled:opacity-35 ${
                  isPanelOpen("ai")
                    ? "bg-[#2563eb] text-white"
                    : "hover:bg-[#2a2c33] hover:text-white"
                }`}
                title="AI助理"
              >
                <Bot size={14} />
              </button>
            )}
            {(appSettings?.general?.right_rail_items?.port_forward ?? true) && (
              <button
                disabled={!isActiveSSHTab}
                onClick={() => toggleRightTool("tunnels")}
                className={`w-7 h-7 rounded-lg flex items-center justify-center transition disabled:opacity-35 ${
                  isPanelOpen("tunnels")
                    ? "bg-[#2563eb] text-white"
                    : "hover:bg-[#2a2c33] hover:text-white"
                }`}
                title="新建端口转发"
              >
                <ArrowLeftRight size={14} />
              </button>
            )}

            <div className="w-5 h-[1px] bg-[#2d3038] my-1" />

            {/* Group 3: 快捷指令 / 历史指令 / 广播指令 / 会话日志 */}
            {(appSettings?.general?.right_rail_items?.snippets ?? true) && (
              <button
                onClick={() => toggleRightTool("snippets")}
                className={`w-7 h-7 rounded-lg flex items-center justify-center transition ${
                  isPanelOpen("snippets")
                    ? "bg-[#2563eb] text-white"
                    : "hover:bg-[#2a2c33] hover:text-white"
                }`}
                title="快捷指令"
              >
                <Code2 size={14} />
              </button>
            )}
            {(appSettings?.general?.right_rail_items?.cmd_history ?? true) && (
              <button
                onClick={() => toggleRightTool("history")}
                className={`w-7 h-7 rounded-lg flex items-center justify-center transition ${
                  isPanelOpen("history")
                    ? "bg-[#2563eb] text-white"
                    : "hover:bg-[#2a2c33] hover:text-white"
                }`}
                title="历史指令"
              >
                <History size={14} />
              </button>
            )}
            {(appSettings?.general?.right_rail_items?.broadcast ?? true) && (
              <button
                onClick={() => setBroadcastMode((v) => !v)}
                className={`w-7 h-7 rounded-lg flex items-center justify-center transition ${
                  broadcastMode
                    ? "bg-[#f59e0b] text-[#121316]"
                    : "hover:bg-[#2a2c33] hover:text-white"
                }`}
                title="广播指令"
              >
                <Radio size={14} />
              </button>
            )}
            {(appSettings?.general?.right_rail_items?.session_logs ?? true) && (
              <button
                onClick={() => toggleRightTool("notifications")}
                className={`w-7 h-7 rounded-lg flex items-center justify-center relative transition ${
                  isPanelOpen("notifications")
                    ? "bg-[#2563eb] text-white"
                    : "hover:bg-[#2a2c33] hover:text-white"
                }`}
                title="会话日志"
              >
                <FileText size={14} />
                {hasUnreadAlert && (
                  <span className="absolute top-1 right-1 w-1.5 h-1.5 rounded-full bg-[#f59e0b]" />
                )}
              </button>
            )}
          </div>

          {/* Bottom Utility Group: 快速连接 / 从 SSH 配置导入 */}
          <div className="flex flex-col items-center gap-1 w-full">
            {(appSettings?.general?.right_rail_items?.quick_connect ??
              true) && (
              <button
                onClick={() => setShowQuickConnect(true)}
                className="w-7 h-7 rounded-lg flex items-center justify-center hover:bg-[#2a2c33] hover:text-[#3fb950]"
                title="快速连接"
              >
                <Zap size={14} />
              </button>
            )}
            {(appSettings?.general?.right_rail_items?.import_ssh ?? true) && (
              <button
                onClick={() => setShowQuickMigrate(true)}
                className="w-7 h-7 rounded-lg flex items-center justify-center hover:bg-[#2a2c33] hover:text-[#60a5fa]"
                title="从 SSH 配置导入"
              >
                <Download size={14} />
              </button>
            )}
          </div>
        </div>
      </div>

      {/* ==================== 3. BOTTOM GLOBAL FOOTER BAR ==================== */}
      <footer className="h-7 bg-[#1b1c1f] border-t border-[#2b2d33] flex items-center justify-between text-xs text-[#8b909a] shrink-0">
        {/* Left Sidebar Status Cell */}
        <div className="flex items-center h-full min-w-0">
          <div
            style={{ width: sidebarOpen ? sidebarWidth : 220 }}
            className="h-full px-3.5 border-r border-[#2b2d33] flex items-center justify-between shrink-0"
          >
            <span>{hosts.length} 台主机</span>
            <div className="flex items-center gap-2">
              <span className="flex items-center gap-1">
                <span
                  className={`w-1.5 h-1.5 rounded-full ${
                    totalConnections > 0 ? "bg-[#3fb950]" : "bg-[#6b7280]"
                  }`}
                />
                {totalConnections} 个连接
              </span>
              <button
                onClick={() => setSidebarOpen((v) => !v)}
                className="p-0.5 rounded hover:bg-[#2b2d34] hover:text-white"
                title="折叠/展开左侧主机树"
              >
                <PanelLeft size={12} />
              </button>
            </div>
          </div>

          {/* Active Session Summary */}
          <div className="px-3 flex items-center gap-3 font-mono truncate">
            {!activeTab ? (
              <span className="font-sans text-[#7c828e]">没有活动会话</span>
            ) : (
              <>
                <span className="flex items-center gap-1.5 text-[#e5e7eb]">
                  <span className="w-1.5 h-1.5 rounded-full bg-[#3fb950]" />
                  {activeTab.title}
                </span>
                <span>{maskedActiveUserTarget}</span>
                {activeTab.latestStats && (
                  <span>
                    🕒 {formatDurationSec(activeTab.latestStats.connected_sec)}
                  </span>
                )}
                <span>📁 {activeTab.sftpPath}</span>
                <span>❐ {activeTab.panes.length} 个会话</span>
              </>
            )}
          </div>
        </div>

        {/* Bottom-Right Utility Bar with Privacy Mode IP Masking! */}
        <div className="px-2.5 flex items-center gap-1.5 shrink-0">
          <button
            onClick={toggleBottomMonitorBar}
            className="w-5 h-5 rounded flex items-center justify-center hover:bg-[#2b2d34] hover:text-white"
            title="显示/隐藏终端底部监控栏"
          >
            <PanelBottom size={12} />
          </button>

          <button
            onClick={() => {
              setSidebarOpen(false);
              setOpenPanels([]);
            }}
            className="w-5 h-5 rounded flex items-center justify-center hover:bg-[#2b2d34] hover:text-white"
            title="沉浸专注模式"
          >
            <Monitor size={12} />
          </button>

          {/* Privacy Mode Toggle with Floating Tooltip */}
          <div className="relative group">
            <button
              onClick={() => setPrivacyMode((v) => !v)}
              className={`w-5 h-5 rounded flex items-center justify-center transition ${
                privacyMode
                  ? "text-[#8b909a] hover:bg-[#2b2d34] hover:text-white"
                  : "text-[#60a5fa] hover:bg-[#2b2d34]"
              }`}
            >
              {privacyMode ? <EyeOff size={12} /> : <Eye size={12} />}
            </button>
            <div className="pointer-events-none opacity-0 group-hover:opacity-100 transition-opacity absolute bottom-7 right-0 whitespace-nowrap px-2.5 py-1 rounded-md bg-white text-[#121316] font-medium shadow-lg text-xs z-50">
              隐私模式：{privacyMode ? "隐藏 IP 地址" : "已显示完整 IP"}
            </div>
          </div>

          <button
            onClick={() => {
              setSettingsInitTab("shortcuts");
              setShowSettings(true);
            }}
            className="w-5 h-5 rounded flex items-center justify-center hover:bg-[#2b2d34] hover:text-white"
            title="快捷键一览"
          >
            <Command size={12} />
          </button>

          <button
            onClick={() => setGlobalComposerOpen((v) => !v)}
            className={`w-5 h-5 rounded flex items-center justify-center transition ${
              globalComposerOpen
                ? "text-[#60a5fa] bg-[#2b2d34]"
                : "hover:bg-[#2b2d34] hover:text-white"
            }`}
            title="多行防误触输入栏"
          >
            <Keyboard size={12} />
          </button>
        </div>
      </footer>

      {/* ==================== MODALS ==================== */}
      {hostModalTarget && (
        <HostModal
          initial={hostModalTarget === "new" ? null : hostModalTarget}
          existingGroups={Array.from(
            new Set(hosts.map((h) => h.group || "").filter(Boolean))
          )}
          settings={appSettings}
          onSave={handleSaveHost}
          onClose={() => setHostModalTarget(null)}
        />
      )}

      {/* Password Prompt Fallback Modal (Triggered when AuthMode="auto" needs interactive password) */}
      {pwPromptHost && (
        <div className="fixed inset-0 z-50 bg-black/65 flex items-center justify-center p-4">
          <form
            onSubmit={handlePasswordPromptSubmit}
            className="w-[400px] rounded-2xl bg-[#1f2024] border border-[#2f3138] shadow-2xl p-5 space-y-3.5 text-xs"
          >
            <div className="flex items-center gap-3">
              <HostBadge
                icon={pwPromptHost.hp.icon}
                badgeText={pwPromptHost.hp.badge_text}
                color={pwPromptHost.hp.color}
                size="sm"
              />
              <div>
                <h2 className="text-sm font-semibold text-white">
                  输入密码以连接 {pwPromptHost.hp.name}
                </h2>
                <div className="text-[#8b909a] font-mono mt-0.5">
                  {maskTargetAddress(
                    `${pwPromptHost.hp.user}@${pwPromptHost.hp.host}`,
                    privacyMode
                  )}
                </div>
              </div>
            </div>

            <input
              type="password"
              required
              autoFocus
              value={pwPromptInput}
              onChange={(e) => setPwPromptInput(e.target.value)}
              placeholder="输入 SSH 登录密码..."
              className="w-full px-3 py-2 rounded-lg bg-[#2a2b30] border border-[#363840] focus:border-[#2b7de9] text-white outline-none"
            />

            <label className="flex items-center gap-2 cursor-pointer text-[#d1d5db]">
              <input
                type="checkbox"
                checked={pwPromptRemember}
                onChange={(e) => setPwPromptRemember(e.target.checked)}
              />
              <span>记住密码并安全存入系统凭据箱</span>
            </label>

            <div className="flex justify-end gap-2 pt-2 border-t border-[#2c2e35]">
              <button
                type="button"
                onClick={() => setPwPromptHost(null)}
                className="px-4 py-1.5 rounded-full bg-[#2b2d32] text-[#d1d5db]"
              >
                取消
              </button>
              <button
                type="submit"
                disabled={pwPromptBusy}
                className="px-5 py-1.5 rounded-full bg-[#1d7dfa] hover:bg-[#358bfa] text-white font-medium"
              >
                {pwPromptBusy ? "连接中..." : "连接"}
              </button>
            </div>
          </form>
        </div>
      )}

      {/* Quick Connect Modal */}
      {showQuickConnect && (
        <div className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-4">
          <form
            onSubmit={handleQuickConnectSubmit}
            className="w-[420px] rounded-2xl bg-[#1e2026] border border-[#30333d] shadow-2xl p-5 space-y-3.5 text-xs"
          >
            <div className="flex items-center justify-between">
              <h2 className="text-sm font-semibold text-white">快速连接</h2>
              <button
                type="button"
                onClick={() => setShowQuickConnect(false)}
                className="p-1 rounded hover:bg-[#2a2d36] text-[#8b909a]"
              >
                <X size={15} />
              </button>
            </div>
            <label className="block space-y-1 text-[#9ca3af]">
              <span>SSH 地址或命令</span>
              <input
                required
                autoFocus
                value={qcAddress}
                onChange={(e) => setQcAddress(e.target.value)}
                placeholder="root@1.2.3.4:22 或 ssh ubuntu@host -p 2222"
                className="w-full px-3 py-2 rounded-lg bg-[#141519] border border-[#2e313b] text-white font-mono outline-none"
              />
            </label>
            <label className="block space-y-1 text-[#9ca3af]">
              <span>密码（若使用 Tailscale SSH 可留空）</span>
              <input
                type="password"
                value={qcPassword}
                onChange={(e) => setQcPassword(e.target.value)}
                placeholder="输入密码..."
                className="w-full px-3 py-2 rounded-lg bg-[#141519] border border-[#2e313b] text-white outline-none"
              />
            </label>
            <label className="flex items-center gap-2 cursor-pointer text-[#d1d5db]">
              <input
                type="checkbox"
                checked={qcSaveHost}
                onChange={(e) => setQcSaveHost(e.target.checked)}
              />
              <span>同时保存到左侧主机列表</span>
            </label>
            <div className="flex justify-end gap-2 pt-2 border-t border-[#2d3039]">
              <button
                type="button"
                onClick={() => setShowQuickConnect(false)}
                className="px-3.5 py-1.5 rounded-lg bg-[#272a33] text-[#d1d5db]"
              >
                取消
              </button>
              <button
                type="submit"
                disabled={qcBusy}
                className="px-4 py-1.5 rounded-lg bg-[#1d7dfa] hover:bg-[#358bfa] text-white font-medium"
              >
                {qcBusy ? "正在连接..." : "立即连接"}
              </button>
            </div>
          </form>
        </div>
      )}

      {/* Quick Migrate Modal */}
      {showQuickMigrate && (
        <div className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-4">
          <div className="w-[440px] rounded-2xl bg-[#1e2026] border border-[#30333d] shadow-2xl p-5 space-y-4 text-xs">
            <div className="flex items-center justify-between">
              <h2 className="text-sm font-semibold text-white">
                快速迁移主机配置
              </h2>
              <button
                onClick={() => setShowQuickMigrate(false)}
                className="p-1 rounded hover:bg-[#2a2d36] text-[#8b909a]"
              >
                <X size={15} />
              </button>
            </div>

            <div className="space-y-2.5">
              <button
                onClick={handleImportSSHConfigClick}
                className="w-full p-3.5 rounded-xl bg-[#252831] border border-[#343846] hover:border-[#3b82f6] text-left space-y-1 transition"
              >
                <div className="font-semibold text-white">
                  1. 自动扫描本机 ~/.ssh/config
                </div>
                <div className="text-[#8b909a]">
                  一键导入本机 OpenSSH 客户端配置的别名、端口、用户名，并将关联的私钥自动迁入系统安全凭据箱。
                </div>
              </button>

              <label className="block w-full p-3.5 rounded-xl bg-[#252831] border border-[#343846] hover:border-[#3b82f6] text-left space-y-1 cursor-pointer transition">
                <div className="font-semibold text-white">
                  2. 从 Shnex 备份文件 (.json) 导入
                </div>
                <div className="text-[#8b909a]">
                  选择之前导出的备份 JSON 文件恢复主机与工作区设置。
                </div>
                <input
                  type="file"
                  accept=".json"
                  className="hidden"
                  onChange={(ev) => {
                    const file = ev.target.files?.[0];
                    if (!file) return;
                    const r = new FileReader();
                    r.onload = async () => {
                      try {
                        await importBackupBundle(JSON.parse(String(r.result)));
                        setHosts(await listHosts());
                        setMigrateMsg("备份文件导入成功！");
                      } catch (e) {
                        setMigrateMsg("导入失败: " + (e as Error).message);
                      }
                    };
                    r.readAsText(file);
                  }}
                />
              </label>
            </div>

            {migrateMsg && (
              <div className="p-2.5 rounded-lg bg-[#1d7dfa]/15 border border-[#1d7dfa]/40 text-[#93c5fd]">
                {migrateMsg}
              </div>
            )}

            <div className="flex justify-end">
              <button
                onClick={() => setShowQuickMigrate(false)}
                className="px-4 py-1.5 rounded-lg bg-[#272a33] text-white"
              >
                完成
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Tailnet Management Modal */}
      {showTailnet && (
        <div className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-4">
          <div className="w-[440px] rounded-2xl bg-[#1e2026] border border-[#30333d] shadow-2xl p-5 space-y-3.5 text-xs">
            <div className="flex items-center justify-between">
              <h2 className="text-sm font-semibold text-white">
                内置用户态 Tailscale / Headscale (tsnet)
              </h2>
              <button
                onClick={() => setShowTailnet(false)}
                className="p-1 rounded hover:bg-[#2a2d36] text-[#8b909a]"
              >
                <X size={15} />
              </button>
            </div>

            <div className="p-2.5 rounded-lg bg-[#15161a] border border-[#2b2e37] space-y-1">
              <div>
                状态:{" "}
                <span className="font-mono text-[#4ade80]">
                  {tsStatus?.backend_state ?? "Stopped"}
                </span>
              </div>
              {tsStatus?.magic_dns_suffix && (
                <div>
                  MagicDNS:{" "}
                  <span className="font-mono">{tsStatus.magic_dns_suffix}</span>
                </div>
              )}
              {tsStatus?.auth_url && (
                <a
                  href={tsStatus.auth_url}
                  target="_blank"
                  rel="noreferrer"
                  className="block text-[#60a5fa] underline break-all pt-1"
                >
                  点击打开浏览器完成交互登录授权 -&gt;
                </a>
              )}
            </div>

            <label className="block space-y-1 text-[#9ca3af]">
              <span>ControlURL (留空使用官方 Tailscale，可填自建 Headscale)</span>
              <input
                value={tsControlUrl}
                onChange={(e) => setTsControlUrl(e.target.value)}
                placeholder="https://headscale.example.com"
                className="w-full px-2.5 py-1.5 rounded-lg bg-[#141519] border border-[#2e313b] text-white font-mono outline-none"
              />
            </label>

            <label className="block space-y-1 text-[#9ca3af]">
              <span>AuthKey (可选，留空走浏览器登录)</span>
              <input
                type="password"
                value={tsAuthKey}
                onChange={(e) => setTsAuthKey(e.target.value)}
                placeholder="tskey-auth-..."
                className="w-full px-2.5 py-1.5 rounded-lg bg-[#141519] border border-[#2e313b] text-white font-mono outline-none"
              />
            </label>

            <div className="flex justify-between pt-2 border-t border-[#2d3039]">
              <button
                onClick={async () => {
                  await tailnetDown();
                  setTsStatus(await getTailnetStatus());
                  setTsNodes([]);
                }}
                className="px-3 py-1.5 rounded-lg bg-[#ef4444]/15 text-[#f87171]"
              >
                停止 tsnet
              </button>
              <div className="flex gap-2">
                <button
                  onClick={async () => {
                    const st = await getTailnetStatus();
                    setTsStatus(st);
                    if (st.running) setTsNodes(await listTailnetNodes());
                  }}
                  className="px-3 py-1.5 rounded-lg bg-[#272a33] text-[#d1d5db]"
                >
                  刷新节点
                </button>
                <button
                  onClick={handleTailnetUp}
                  disabled={tsBusy}
                  className="px-4 py-1.5 rounded-lg bg-[#2563eb] text-white font-medium"
                >
                  {tsBusy ? "启动中..." : "启动连接"}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {appAlertModal && (
        <div
          className="fixed inset-0 z-[120] bg-black/65 backdrop-blur-xs flex items-center justify-center p-4"
          onClick={() => setAppAlertModal(null)}
        >
          <div
            className="w-full max-w-md rounded-xl bg-[#1b1d24] border border-[#2f3340] shadow-2xl p-5 text-[13px] space-y-3.5"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 font-semibold text-white text-[14px]">
                <span
                  className={`inline-block w-2 h-2 rounded-full ${
                    appAlertModal.isError ? "bg-[#ef4444]" : "bg-[#3b82f6]"
                  }`}
                />
                <span>{appAlertModal.title}</span>
              </div>
              <button
                onClick={() => setAppAlertModal(null)}
                className="p-1 rounded-md hover:bg-[#2a2d36] text-[#8b909a] hover:text-white transition"
              >
                <X size={15} />
              </button>
            </div>

            <div className="p-3 rounded-lg bg-[#13151a] border border-[#282b36] text-[#e5e7eb] font-mono text-[12px] leading-relaxed break-words select-text max-h-52 overflow-y-auto">
              {appAlertModal.message}
            </div>

            <div className="flex items-center justify-end gap-2 pt-1">
              <button
                onClick={() => {
                  navigator.clipboard?.writeText(appAlertModal.message).catch(() => {});
                  showToast("已复制", "提示内容已复制到剪贴板");
                }}
                className="px-3 py-1.5 rounded-lg bg-[#252833] hover:bg-[#2e3240] text-[#d1d5db] text-[12px] transition"
              >
                复制内容
              </button>
              <button
                autoFocus
                onClick={() => setAppAlertModal(null)}
                className="px-4 py-1.5 rounded-lg bg-[#2563eb] hover:bg-[#1d4ed8] text-white font-medium text-[12px] transition"
              >
                确定
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
