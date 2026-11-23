import React, { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { WebglAddon } from "@xterm/addon-webgl";
import {
  ClipboardCheck,
  Columns2,
  FileCode2,
  FileText,
  FolderOpen,
  GitBranch,
  ImagePlus,
  Keyboard,
  RotateCcw,
  Rows2,
  Search,
  Send,
  Server,
  Sparkles,
  TerminalSquare,
  X,
  Zap,
} from "lucide-react";
import {
  AppSettings,
  CompletionCandidate,
  SystemStats,
  acceptSessionCompletion,
  buildSessionWSUrl,
  formatBytes,
  formatRate,
  getSessionStats,
  openNativeFileDialog,
  readNativeClipboard,
  suggestSessionCompletion,
  uploadSessionImage,
  uploadSessionImageBase64,
  uploadSessionImageFromLocalPath,
} from "../api";

const OP_DATA = 0x00;
const OP_RESIZE = 0x01;
const OP_PONG = 0x03;
const OP_EVENT = 0x04;

const encoder = new TextEncoder();
const decoder = new TextDecoder();

export interface TerminalPaneHandle {
  sendText: (text: string) => void;
}

interface Props {
  paneId: string;
  sessionId: string;
  protocol?: string;
  hostTitle: string;
  userTarget: string;
  focused: boolean;
  terminalCfg?: AppSettings["terminal"];
  monitorCfg?: AppSettings["monitor"];
  hostMetricsBar?: "follow" | "on" | "off";
  terminalEncoding?: string;
  effectiveTerm?: string;
  themeMode?: string;
  forceComposerOpen?: boolean;
  onBroadcastData?: (text: string) => void;
  onFocus: () => void;
  onSplitHorizontal: () => void;
  onSplitVertical: () => void;
  onOpenSFTP: () => void;
  onReconnect?: (lastReportedDir?: string) => void;
  onClosePane: () => void;
  onStatsUpdate: (stats: SystemStats) => void;
  onAIStateChange: (
    state: "running" | "approval" | "idle",
    snippet?: string
  ) => void;
  registerSender?: (paneId: string, sendFn: (txt: string) => void) => void;
}

export const TerminalPane: React.FC<Props> = ({
  paneId,
  sessionId,
  protocol = "ssh",
  hostTitle,
  userTarget,
  focused,
  terminalCfg,
  monitorCfg,
  hostMetricsBar,
  terminalEncoding,
  effectiveTerm,
  themeMode,
  forceComposerOpen,
  onBroadcastData,
  onFocus,
  onSplitHorizontal,
  onSplitVertical,
  onOpenSFTP,
  onReconnect,
  onClosePane,
  onStatsUpdate,
  onAIStateChange,
  registerSender,
}) => {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const termInstanceRef = useRef<Terminal | null>(null);
  const fitAddonInstanceRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const searchAddonRef = useRef<SearchAddon | null>(null);

  const broadcastRef = useRef(onBroadcastData);
  broadcastRef.current = onBroadcastData;
  const terminalCfgRef = useRef(terminalCfg);
  terminalCfgRef.current = terminalCfg;
  const effectiveTermRef = useRef(effectiveTerm || terminalCfg?.term_type || "xterm-256color");
  effectiveTermRef.current = effectiveTerm || terminalCfg?.term_type || "xterm-256color";
  const lastReportedDirRef = useRef<string>("");
  const reconnectRef = useRef(onReconnect);
  reconnectRef.current = onReconnect;

  const [connected, setConnected] = useState(true);
  const [autoReconnecting, setAutoReconnecting] = useState(false);
  const autoReconnectTimerRef = useRef<number | null>(null);

  const [stats, setStats] = useState<SystemStats | null>(null);
  const [rttMs, setRttMs] = useState<number | null>(null);
  const [approvalSnippet, setApprovalSnippet] = useState<string | null>(null);
  const [showSearch, setShowSearch] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [showComposer, setShowComposer] = useState(false);
  const [composerText, setComposerText] = useState("");

  // Right-click Context Menu
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number } | null>(null);

  // OSC 52 Clipboard Guard & Undo Toast
  const [pendingOsc52, setPendingOsc52] = useState<{
    text: string;
    byteLen: number;
  } | null>(null);
  const [osc52Toast, setOsc52Toast] = useState<{
    byteLen: number;
    prevText: string | null;
  } | null>(null);
  const osc52ToastTimerRef = useRef<number | null>(null);

  // Terminal / Claude CLI Image Upload State
  const [imgUploading, setImgUploading] = useState(false);
  const [imgToast, setImgToast] = useState<string | null>(null);
  const imgToastTimerRef = useRef<number | null>(null);
  const imgInputRef = useRef<HTMLInputElement | null>(null);
  const pasteGuardTsRef = useRef<number>(0);
  const smartPasteFnRef = useRef<(imageOnly?: boolean) => void>(() => {});

  const showImageUploadBanner = (msg: string) => {
    if (imgToastTimerRef.current) {
      window.clearTimeout(imgToastTimerRef.current);
    }
    setImgToast(msg);
    imgToastTimerRef.current = window.setTimeout(() => {
      setImgToast(null);
    }, 4500);
  };

  // Built-in Zero-Plugin Autocomplete State & Refs
  interface ActiveCompletionState {
    line: string;
    tokenPrefix: string;
    candidates: CompletionCandidate[];
    selectedIndex: number;
    listOpen: boolean;
    explicitlyNavigated: boolean;
    dismissedLine: string | null;
    x: number;
    y: number;
    cellH: number;
    paneW: number;
    paneH: number;
  }

  const emptyCompletionState: ActiveCompletionState = {
    line: "",
    tokenPrefix: "",
    candidates: [],
    selectedIndex: 0,
    listOpen: false,
    explicitlyNavigated: false,
    dismissedLine: null,
    x: 0,
    y: 0,
    cellH: 18,
    paneW: 640,
    paneH: 380,
  };

  const compRef = useRef<ActiveCompletionState>({ ...emptyCompletionState });
  const [compUI, setCompUI] = useState<ActiveCompletionState>({
    ...emptyCompletionState,
  });
  const promptAnchorRef = useRef<{ row: number; col: number } | null>(null);
  const compReqSeqRef = useRef(0);
  const lastQueriedLineRef = useRef("");
  const compRafRef = useRef<number | null>(null);
  const popupListRef = useRef<HTMLDivElement | null>(null);
  const isImeComposingRef = useRef(false);
  const syncImeAnchorFnRef = useRef<() => void>(() => {});

  const syncCompState = (patch: Partial<ActiveCompletionState>) => {
    compRef.current = { ...compRef.current, ...patch };
    setCompUI({ ...compRef.current });
  };

  const clearCompletionState = (dismissedLine: string | null = null) => {
    compReqSeqRef.current += 1;
    lastQueriedLineRef.current = "";
    compRef.current = {
      ...emptyCompletionState,
      dismissedLine,
    };
    setCompUI({ ...compRef.current });
  };

  const isLocal = protocol === "local";

  const sendRawBytes = (bytes: Uint8Array) => {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    const frame = new Uint8Array(1 + bytes.length);
    frame[0] = OP_DATA;
    frame.set(bytes, 1);
    ws.send(frame);
  };

  const sendTextToPTY = (text: string, bracketed = false) => {
    const payload = bracketed ? `\x1b[200~${text}\x1b[201~\r` : text;
    sendRawBytes(encoder.encode(payload));
    if (text.includes("\n") || text.includes("\r") || bracketed) {
      setApprovalSnippet(null);
      onAIStateChange("running");
    }
  };

  const performOsc52Write = async (text: string, byteLen: number) => {
    let prevText: string | null = null;
    try {
      prevText = await navigator.clipboard.readText();
    } catch {
      prevText = null;
    }
    try {
      await navigator.clipboard.writeText(text);
      if (terminalCfgRef.current?.osc52_notify ?? true) {
        if (osc52ToastTimerRef.current) {
          window.clearTimeout(osc52ToastTimerRef.current);
        }
        setOsc52Toast({ byteLen, prevText });
        osc52ToastTimerRef.current = window.setTimeout(() => {
          setOsc52Toast(null);
        }, 6500);
      }
    } catch {
      // Ignore clipboard write errors if window unfocused
    }
  };

  useEffect(() => {
    if (!ctxMenu) return;
    const close = () => setCtxMenu(null);
    window.addEventListener("mousedown", close);
    return () => window.removeEventListener("mousedown", close);
  }, [ctxMenu]);

  useEffect(() => {
    if (registerSender) {
      registerSender(paneId, (txt) => sendTextToPTY(txt, false));
    }
  }, [paneId, sessionId]);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    let unmounted = false;

    const term = new Terminal({
      cursorBlink: terminalCfg?.cursor_blink ?? true,
      cursorStyle: terminalCfg?.cursor_style ?? "block",
      scrollback: terminalCfg?.scrollback ?? 5000,
      allowProposedApi: true,
      fontSize: terminalCfg?.font_size ?? 13.5,
      lineHeight: terminalCfg?.line_height ?? 1.24,
      fontFamily:
        terminalCfg?.font_family ||
        '"Cascadia Mono", "JetBrains Mono", Consolas, "Courier New", monospace',
      theme: {
        background:
          themeMode === "oled"
            ? "#000000"
            : themeMode === "slate"
            ? "#171a21"
            : "#121316",
        foreground: "#e5e7eb",
        cursor: "#e5e7eb",
        selectionBackground: "rgba(59, 130, 246, 0.35)",
        black: "#18191d",
        red: "#f87171",
        green: "#4ade80",
        yellow: "#facc15",
        blue: "#60a5fa",
        magenta: "#c084fc",
        cyan: "#22d3ee",
        white: "#f3f4f6",
      },
    });

    const fitAddon = new FitAddon();
    const searchAddon = new SearchAddon();
    const linksAddon = new WebLinksAddon();
    termInstanceRef.current = term;
    fitAddonInstanceRef.current = fitAddon;
    searchAddonRef.current = searchAddon;

    term.loadAddon(fitAddon);
    term.loadAddon(searchAddon);
    term.loadAddon(linksAddon);
    term.open(el);

    try {
      const webgl = new WebglAddon();
      webgl.onContextLoss(() => {
        webgl.dispose();
      });
      term.loadAddon(webgl);
    } catch {
      // Fallback cleanly to default canvas renderer if GPU blocks WebGL2
    }

    // Linux Console TERM keybindings + Native Autocomplete (inline / list / hybrid)
    const acceptCandidate = (cand: CompletionCandidate) => {
      const prefix = compRef.current.tokenPrefix;
      clearCompletionState(null);
      acceptSessionCompletion(sessionId, cand.kind, cand.label).catch(() => {});

      const prefixRunes = Array.from(prefix).length;
      const out =
        cand.replace_len === prefixRunes && cand.insert_text.startsWith(prefix)
          ? cand.insert_text.slice(prefix.length)
          : "\x7f".repeat(Math.max(0, cand.replace_len)) + cand.insert_text;

      if (!out) return;
      if (broadcastRef.current) {
        broadcastRef.current(out);
      } else {
        sendRawBytes(encoder.encode(out));
      }
    };

    const measureCursorCoords = () => {
      const screenEl = el.querySelector(".xterm-screen") as HTMLElement | null;
      const hostRect = el.getBoundingClientRect();
      const screenRect = screenEl ? screenEl.getBoundingClientRect() : hostRect;
      const offsetX = Math.max(0, screenRect.left - hostRect.left);
      const offsetY = Math.max(0, screenRect.top - hostRect.top);
      const w = screenRect.width || el.clientWidth || 640;
      const h = screenRect.height || el.clientHeight || 380;
      const cellW = w / Math.max(1, term.cols);
      const cellH = h / Math.max(1, term.rows);
      return {
        x: offsetX + term.buffer.active.cursorX * cellW,
        y: offsetY + term.buffer.active.cursorY * cellH,
        cellH,
        paneW: el.clientWidth || w,
        paneH: el.clientHeight || h,
      };
    };

    // Proactively anchor xterm's hidden IME <textarea> and inline .composition-view
    // right on the live cursor cell so Windows TSF / IMM32 never sees an off-screen
    // (-9999em) or 0x0 caret rect on WM_IME_STARTCOMPOSITION.
    const syncImeCursorAnchor = () => {
      const textarea = term.textarea;
      const screenEl = el.querySelector(".xterm-screen") as HTMLElement | null;
      if (!textarea || !screenEl) return;
      const helpersEl = textarea.parentElement as HTMLElement | null;
      const screenRect = screenEl.getBoundingClientRect();
      const helpersRect = helpersEl
        ? helpersEl.getBoundingClientRect()
        : screenRect;
      const w = screenRect.width || el.clientWidth || 640;
      const h = screenRect.height || el.clientHeight || 380;
      const cellW = Math.max(7, w / Math.max(1, term.cols));
      const cellH = Math.max(16, h / Math.max(1, term.rows));
      const col = Math.min(
        Math.max(0, term.buffer.active.cursorX),
        Math.max(0, term.cols - 1)
      );
      const row = Math.min(
        Math.max(0, term.buffer.active.cursorY),
        Math.max(0, term.rows - 1)
      );
      const leftPx = Math.max(
        0,
        Math.round(screenRect.left - helpersRect.left + col * cellW)
      );
      const topPx = Math.max(
        0,
        Math.round(screenRect.top - helpersRect.top + row * cellH)
      );

      const compView = el.querySelector(
        ".composition-view"
      ) as HTMLElement | null;
      const isComposing =
        isImeComposingRef.current ||
        (compView?.classList.contains("active") ?? false);

      if (compView) {
        compView.style.left = `${leftPx}px`;
        compView.style.top = `${topPx}px`;
        compView.style.height = `${Math.round(cellH)}px`;
        compView.style.lineHeight = `${Math.round(cellH)}px`;
        compView.style.fontSize = `${
          terminalCfgRef.current?.font_size ?? 13.5
        }px`;
      }

      const compWidth =
        isComposing && compView ? Math.max(compView.offsetWidth, cellW) : cellW;
      textarea.style.left = `${leftPx}px`;
      textarea.style.top = `${topPx}px`;
      textarea.style.width = `${Math.max(10, Math.round(compWidth))}px`;
      textarea.style.height = `${Math.max(16, Math.round(cellH))}px`;
      textarea.style.lineHeight = `${Math.max(16, Math.round(cellH))}px`;
    };
    syncImeAnchorFnRef.current = syncImeCursorAnchor;
    requestAnimationFrame(syncImeCursorAnchor);

    const extractCommandBeforeCursor = (): string | null => {
      const buf = term.buffer.active;
      // In normal buffer, suppress completion while user scrolls up through history.
      // Allow alternate buffer (`buf.type === "alternate"`) so SSH sessions attached
      // to `tmux` / `screen` / `zellij` get full native completion on prompt lines!
      if (buf.type === "normal" && buf.viewportY !== buf.baseY) return null;
      const absRow = buf.baseY + buf.cursorY;
      const cursorX = buf.cursorX;
      const lineObj = buf.getLine(absRow);
      if (!lineObj) return null;

      // Do not show inline completion when text exists to the right of cursor
      // (ignoring tmux vertical split borders `│` / `┃`).
      const rightRaw = lineObj.translateToString(true, cursorX);
      const rightBeforePaneBorder = rightRaw.split(/[│┃]/)[0] ?? rightRaw;
      if (rightBeforePaneBorder.trim().length > 0) return null;

      const anchor = promptAnchorRef.current;
      if (
        buf.type === "normal" &&
        anchor &&
        anchor.row === absRow &&
        cursorX >= anchor.col
      ) {
        return lineObj.translateToString(false, anchor.col, cursorX);
      }

      const rawBefore = lineObj.translateToString(false, 0, cursorX);
      const psDirMatch = rawBefore.match(/^PS ([A-Za-z]:[^>]+)>/);
      if (psDirMatch?.[1]) {
        lastReportedDirRef.current = psDirMatch[1].trim();
      } else {
        const cmdDirMatch = rawBefore.match(/^([A-Za-z]:\\[^>]+)>/);
        if (cmdDirMatch?.[1]) {
          lastReportedDirRef.current = cmdDirMatch[1].trim();
        } else {
          const linuxDirMatch =
            rawBefore.match(
              /^(?:\([^)\r\n]+\)\s*)*[^\s@:\r\n]+@[^\s:\r\n]+:((?:~|\/)[^$#%\r\n]*)[$#%]\s/
            ) ||
            rawBefore.match(
              /^(?:\([^)\r\n]+\)\s*)*\[[^\]\r\n]+@[^\s\]\r\n]+\s+([^\]]+)\][$#%]\s/
            );
          if (linuxDirMatch?.[1]) {
            const p = linuxDirMatch[1].trim();
            if (p === "~") {
              lastReportedDirRef.current = ".";
            } else if (p.startsWith("~/")) {
              lastReportedDirRef.current = p.slice(2);
            } else if (p.startsWith("/")) {
              lastReportedDirRef.current = p;
            }
          }
        }
      }

      const strictPromptPatterns = [
        /^PS [A-Za-z]:[^>]*>\s?/,
        /^[A-Za-z]:\\[^>]*>/,
        /^(?:\([^)\r\n]+\)\s*)*[^\s@:\r\n]+@[^\s:\r\n]+:[^\r\n]*?[$#%]\s/,
        /^(?:\([^)\r\n]+\)\s*)*\[[^\]\r\n]+@[^\]\r\n]+\][^\r\n]*?[$#%]\s/,
        /^(?:bash|sh|zsh|fish)(?:-[0-9.]+)?[$#%]\s/,
        /^.*?(?:❯|➜)\s/,
      ];
      for (const re of strictPromptPatterns) {
        const m = rawBefore.match(re);
        if (m) {
          return rawBefore.slice(m[0].length);
        }
      }
      if (buf.type === "normal") {
        const fallback = rawBefore.match(/^[^\r\n]{1,80}?[$#%]\s/);
        if (fallback) {
          return rawBefore.slice(fallback[0].length);
        }
      }
      return null;
    };

    const scheduleCompletionRefresh = () => {
      if (compRafRef.current !== null) {
        window.cancelAnimationFrame(compRafRef.current);
      }
      compRafRef.current = window.requestAnimationFrame(async () => {
        compRafRef.current = null;
        if (unmounted) return;
        if (isImeComposingRef.current) return;
        if (!(terminalCfgRef.current?.completion_enabled ?? true)) {
          if (compRef.current.candidates.length > 0) clearCompletionState(null);
          return;
        }
        const cmdLine = extractCommandBeforeCursor();
        if (!cmdLine || !cmdLine.trim()) {
          if (compRef.current.candidates.length > 0) clearCompletionState(null);
          return;
        }

        const coords = measureCursorCoords();
        if (
          cmdLine === lastQueriedLineRef.current &&
          compRef.current.line === cmdLine
        ) {
          syncCompState(coords);
          return;
        }

        lastQueriedLineRef.current = cmdLine;
        const seq = ++compReqSeqRef.current;
        try {
          const res = await suggestSessionCompletion(
            sessionId,
            cmdLine,
            lastReportedDirRef.current || undefined
          );
          if (unmounted || seq !== compReqSeqRef.current) return;
          if (extractCommandBeforeCursor() !== cmdLine) return;
          const nextCoords = measureCursorCoords();
          const cands = res.candidates || [];
          if (cands.length === 0) {
            clearCompletionState(null);
            return;
          }
          const mode = terminalCfgRef.current?.completion_mode || "hybrid";
          const isArgOrPathCompletion =
            /\s/.test(cmdLine) ||
            cmdLine.startsWith(".") ||
            cmdLine.includes("/") ||
            cmdLine.includes("\\");
          syncCompState({
            line: cmdLine,
            tokenPrefix: res.token_prefix || "",
            candidates: cands,
            selectedIndex: 0,
            listOpen:
              mode === "list" || (mode === "hybrid" && isArgOrPathCompletion),
            explicitlyNavigated: false,
            dismissedLine:
              compRef.current.dismissedLine === cmdLine ? cmdLine : null,
            ...nextCoords,
          });
        } catch {
          if (!unmounted && seq === compReqSeqRef.current) {
            clearCompletionState(null);
          }
        }
      });
    };

    term.attachCustomKeyEventHandler((ev) => {
      // Never intercept keys while Windows / macOS / Linux IME is actively composing
      // (e.g. Microsoft Pinyin, Sogou, WeChat IME, Rime selecting candidates 1-9 / Space / Enter / Esc / Tab).
      if (
        isImeComposingRef.current ||
        ev.isComposing ||
        ev.keyCode === 229 ||
        ev.key === "Process"
      ) {
        return true;
      }
      if (ev.type !== "keydown") return true;

      const compEnabled = terminalCfgRef.current?.completion_enabled ?? true;
      const st = compRef.current;
      const hasActiveCompletion =
        compEnabled &&
        st.candidates.length > 0 &&
        st.dismissedLine !== st.line;

      if (hasActiveCompletion && !ev.ctrlKey && !ev.altKey && !ev.metaKey) {
        const mode = terminalCfgRef.current?.completion_mode || "hybrid";
        const len = st.candidates.length;
        const activeCand = st.candidates[st.selectedIndex] || st.candidates[0];

        if (ev.key === "Escape") {
          ev.preventDefault();
          if (mode === "hybrid" && st.listOpen) {
            syncCompState({ listOpen: false, selectedIndex: 0 });
          } else {
            syncCompState({ listOpen: false, dismissedLine: st.line });
          }
          return false;
        }

        if (mode === "inline") {
          if (ev.key === "Tab" && !ev.shiftKey && activeCand?.ghost_suffix) {
            ev.preventDefault();
            acceptCandidate(activeCand);
            return false;
          }
        } else if (mode === "list") {
          if (ev.key === "Tab" && !ev.shiftKey && activeCand) {
            ev.preventDefault();
            acceptCandidate(activeCand);
            return false;
          }
          if (ev.key === "ArrowDown" || (ev.key === "Tab" && ev.shiftKey)) {
            ev.preventDefault();
            const delta = ev.key === "ArrowDown" ? 1 : -1;
            syncCompState({
              selectedIndex: (st.selectedIndex + delta + len) % len,
              explicitlyNavigated: true,
            });
            return false;
          }
          if (ev.key === "ArrowUp") {
            ev.preventDefault();
            syncCompState({
              selectedIndex: (st.selectedIndex - 1 + len) % len,
              explicitlyNavigated: true,
            });
            return false;
          }
          if (ev.key === "Enter" && st.explicitlyNavigated && activeCand) {
            ev.preventDefault();
            acceptCandidate(activeCand);
            return false;
          }
        } else {
          // Hybrid Mode ("混合补齐：→ 接受灰字，Tab 打开列表，再次按 Tab 轮换候选，Enter 插入选中项但不执行命令")
          if (!st.listOpen) {
            if (
              ev.key === "ArrowRight" &&
              !ev.shiftKey &&
              activeCand?.ghost_suffix
            ) {
              ev.preventDefault();
              acceptCandidate(activeCand);
              return false;
            }
            if (ev.key === "Tab") {
              ev.preventDefault();
              syncCompState({
                listOpen: true,
                selectedIndex: 0,
                explicitlyNavigated: true,
              });
              return false;
            }
          } else {
            if (ev.key === "Tab") {
              ev.preventDefault();
              if (len === 1 && !ev.shiftKey && activeCand) {
                acceptCandidate(activeCand);
                return false;
              }
              const delta = ev.shiftKey ? -1 : 1;
              syncCompState({
                selectedIndex: (st.selectedIndex + delta + len) % len,
                explicitlyNavigated: true,
              });
              return false;
            }
            if (ev.key === "ArrowDown") {
              ev.preventDefault();
              syncCompState({
                selectedIndex: (st.selectedIndex + 1) % len,
                explicitlyNavigated: true,
              });
              return false;
            }
            if (ev.key === "ArrowUp") {
              ev.preventDefault();
              syncCompState({
                selectedIndex: (st.selectedIndex - 1 + len) % len,
                explicitlyNavigated: true,
              });
              return false;
            }
            if (ev.key === "ArrowRight" && !ev.shiftKey && activeCand) {
              ev.preventDefault();
              acceptCandidate(activeCand);
              return false;
            }
            const isBareRequiredArg =
              st.tokenPrefix === "" &&
              /^(?:cd|pushd|cat|nvim|vim|vi|nano|code|ssh|scp|sftp)\s+$/i.test(
                st.line
              );
            if (
              ev.key === "Enter" &&
              !ev.shiftKey &&
              activeCand &&
              (st.explicitlyNavigated || isBareRequiredArg)
            ) {
              ev.preventDefault();
              acceptCandidate(activeCand);
              return false;
            }
          }
        }
      }

      if (ev.key === "Enter" || (ev.ctrlKey && ev.key.toLowerCase() === "c")) {
        clearCompletionState(null);
      }

      // Intercept Ctrl+V / Ctrl+Shift+V (text OR screenshot) and Alt+V (Claude Code image paste)
      if (
        (ev.ctrlKey || ev.metaKey) &&
        !ev.altKey &&
        ev.key.toLowerCase() === "v"
      ) {
        ev.preventDefault();
        smartPasteFnRef.current(false);
        return false;
      }
      if (
        !ev.ctrlKey &&
        ev.altKey &&
        !ev.metaKey &&
        !ev.shiftKey &&
        ev.key.toLowerCase() === "v"
      ) {
        ev.preventDefault();
        smartPasteFnRef.current(true);
        return false;
      }

      if (effectiveTermRef.current === "linux" && !ev.ctrlKey && !ev.altKey && !ev.metaKey) {
        const linuxMap: Record<string, string> = {
          F1: "\x1b[[A",
          F2: "\x1b[[B",
          F3: "\x1b[[C",
          F4: "\x1b[[D",
          F5: "\x1b[[E",
          Home: "\x1b[1~",
          End: "\x1b[4~",
        };
        if (linuxMap[ev.key]) {
          sendRawBytes(encoder.encode(linuxMap[ev.key]));
          return false;
        }
      }
      return true;
    });

    const writeParsedDisp = term.onWriteParsed(() => {
      syncImeCursorAnchor();
      scheduleCompletionRefresh();
    });

    const cursorMoveDisp = term.onCursorMove(() => {
      syncImeCursorAnchor();
      scheduleCompletionRefresh();
    });

    const renderDisp = term.onRender(() => {
      syncImeCursorAnchor();
    });

    const scrollDisp = term.onScroll(() => {
      syncImeCursorAnchor();
    });

    const helperTextarea = term.textarea;
    const handleImeFocus = () => {
      syncImeCursorAnchor();
    };
    const handleImeStart = () => {
      isImeComposingRef.current = true;
      clearCompletionState(null);
      syncImeCursorAnchor();
      requestAnimationFrame(syncImeCursorAnchor);
    };
    const handleImeUpdate = () => {
      isImeComposingRef.current = true;
      syncImeCursorAnchor();
      requestAnimationFrame(syncImeCursorAnchor);
    };
    const handleImeEnd = () => {
      isImeComposingRef.current = false;
      requestAnimationFrame(() => {
        if (!unmounted) {
          syncImeCursorAnchor();
          scheduleCompletionRefresh();
        }
      });
    };
    helperTextarea?.addEventListener("focus", handleImeFocus);
    helperTextarea?.addEventListener("compositionstart", handleImeStart);
    helperTextarea?.addEventListener("compositionupdate", handleImeUpdate);
    helperTextarea?.addEventListener("compositionend", handleImeEnd);

    // OSC 133: Track exact semantic prompt-end marker (`OSC 133;B`) emitted by built-in Powerline prompt
    const osc133Disp = term.parser.registerOscHandler(133, (data) => {
      const code = data.split(";")[0]?.trim();
      if (code === "B") {
        promptAnchorRef.current = {
          row: term.buffer.active.baseY + term.buffer.active.cursorY,
          col: term.buffer.active.cursorX,
        };
      }
      return true;
    });

    // Copy on Select ("选中即复制")
    const selDisp = term.onSelectionChange(() => {
      if (terminalCfgRef.current?.copy_on_select && term.hasSelection()) {
        const selected = term.getSelection();
        if (selected) {
          navigator.clipboard.writeText(selected).catch(() => {});
        }
      }
    });

    // OSC 7: Track remote shell working directory for "重新连接后回到之前的文件夹"
    const osc7Disp = term.parser.registerOscHandler(7, (data) => {
      try {
        if (data.startsWith("file://")) {
          const u = new URL(data);
          const decodedPath = decodeURIComponent(u.pathname);
          if (decodedPath) {
            lastReportedDirRef.current = decodedPath;
          }
        } else if (data.startsWith("/")) {
          lastReportedDirRef.current = data.trim();
        }
      } catch {
        // Ignore malformed OSC 7 URI
      }
      return true;
    });

    // OSC 52: Remote Clipboard Guard ("远程剪贴板 (OSC 52)")
    const osc52Disp = term.parser.registerOscHandler(52, (data) => {
      const semiIdx = data.indexOf(";");
      if (semiIdx < 0) return true;
      const b64Payload = data.slice(semiIdx + 1).trim();
      // Security Redline: Remote read queries ("?") are ALWAYS rejected!
      if (!b64Payload || b64Payload === "?") {
        return true;
      }
      const mode = terminalCfgRef.current?.osc52_mode ?? "ask";
      if (mode === "deny") {
        return true;
      }
      try {
        const bin = atob(b64Payload.replace(/\s+/g, ""));
        const maxBytes =
          Math.max(64, Math.min(8192, terminalCfgRef.current?.osc52_max_kib ?? 1024)) *
          1024;
        if (bin.length > maxBytes) {
          return true; // Exceeds KiB cap -> discard
        }
        const bytes = new Uint8Array(bin.length);
        for (let i = 0; i < bin.length; i++) {
          bytes[i] = bin.charCodeAt(i);
        }
        const text = decoder.decode(bytes);
        if (mode === "allow") {
          performOsc52Write(text, bytes.length);
        } else {
          setPendingOsc52({ text, byteLen: bytes.length });
        }
      } catch {
        // Ignore invalid base64
      }
      return true;
    });

    const sendResize = (cols: number, rows: number) => {
      const ws = wsRef.current;
      if (!ws || ws.readyState !== WebSocket.OPEN) return;
      const buf = new ArrayBuffer(5);
      const dv = new DataView(buf);
      dv.setUint8(0, OP_RESIZE);
      dv.setUint16(1, cols, false);
      dv.setUint16(3, rows, false);
      ws.send(buf);
    };

    const ws = new WebSocket(buildSessionWSUrl(sessionId));
    ws.binaryType = "arraybuffer";
    wsRef.current = ws;

    ws.onopen = () => {
      setConnected(true);
      setAutoReconnecting(false);
      requestAnimationFrame(() => {
        try {
          fitAddon.fit();
          syncImeCursorAnchor();
          sendResize(term.cols, term.rows);
          term.focus();
          syncImeCursorAnchor();
        } catch {
          // Ignore if unmounted
        }
      });
    };

    let customDecoder: TextDecoder | null = null;
    const normEnc = (terminalEncoding || "utf-8").toLowerCase();
    if (normEnc && normEnc !== "utf-8" && normEnc !== "utf8") {
      try {
        customDecoder = new TextDecoder(normEnc);
      } catch {
        customDecoder = null;
      }
    }

    const triggerDisconnectHandling = () => {
      if (unmounted) return;
      setConnected(false);
      if (
        !isLocal &&
        (terminalCfgRef.current?.auto_reconnect ?? true) &&
        reconnectRef.current
      ) {
        if (autoReconnectTimerRef.current) {
          window.clearTimeout(autoReconnectTimerRef.current);
        }
        setAutoReconnecting(true);
        autoReconnectTimerRef.current = window.setTimeout(() => {
          if (!unmounted && reconnectRef.current) {
            setAutoReconnecting(false);
            reconnectRef.current(lastReportedDirRef.current || undefined);
          }
        }, 2000);
      }
    };

    ws.onmessage = (ev) => {
      if (!(ev.data instanceof ArrayBuffer) || ev.data.byteLength === 0) return;
      const u8 = new Uint8Array(ev.data);
      const op = u8[0];
      if (op === OP_DATA) {
        const payload = u8.subarray(1);
        if (customDecoder) {
          term.write(customDecoder.decode(payload, { stream: true }));
        } else {
          term.write(payload);
        }
      } else if (op === OP_PONG && ev.data.byteLength >= 17) {
        const dv = new DataView(ev.data);
        const micros = Number(dv.getBigUint64(9, false));
        setRttMs(Math.round(micros / 1000));
      } else if (op === OP_EVENT) {
        try {
          const sev = JSON.parse(decoder.decode(u8.subarray(1)));
          if (sev.type === "keepalive_rtt" && sev.rtt_ns) {
            setRttMs(Math.round(sev.rtt_ns / 1e6));
          } else if (sev.type === "ai_approval_required") {
            setApprovalSnippet(sev.prompt_snippet || "Claude CLI 等待确认");
            onAIStateChange("approval", sev.prompt_snippet);
          } else if (sev.type === "output_silence") {
            onAIStateChange("idle");
          } else if (sev.type === "disconnected") {
            triggerDisconnectHandling();
          }
        } catch {
          // Ignore malformed event JSON
        }
      }
    };

    ws.onclose = () => {
      triggerDisconnectHandling();
    };

    const dataDisp = term.onData((data) => {
      if (broadcastRef.current) {
        broadcastRef.current(data);
      } else {
        sendRawBytes(encoder.encode(data));
      }
      if (data.includes("\r") || data.includes("\n")) {
        setApprovalSnippet(null);
        onAIStateChange("running");
      }
    });

    const ro = new ResizeObserver(() => {
      try {
        fitAddon.fit();
        syncImeCursorAnchor();
        sendResize(term.cols, term.rows);
      } catch {
        // Ignore transient unmount resize
      }
    });
    ro.observe(el);

    return () => {
      unmounted = true;
      isImeComposingRef.current = false;
      if (autoReconnectTimerRef.current) {
        window.clearTimeout(autoReconnectTimerRef.current);
      }
      if (compRafRef.current !== null) {
        window.cancelAnimationFrame(compRafRef.current);
      }
      helperTextarea?.removeEventListener("focus", handleImeFocus);
      helperTextarea?.removeEventListener("compositionstart", handleImeStart);
      helperTextarea?.removeEventListener("compositionupdate", handleImeUpdate);
      helperTextarea?.removeEventListener("compositionend", handleImeEnd);
      ro.disconnect();
      writeParsedDisp.dispose();
      cursorMoveDisp.dispose();
      renderDisp.dispose();
      scrollDisp.dispose();
      osc133Disp.dispose();
      selDisp.dispose();
      osc7Disp.dispose();
      osc52Disp.dispose();
      dataDisp.dispose();
      ws.close();
      term.dispose();
    };
  }, [sessionId]);

  // Live-update xterm font, cursor & theme background when changed in Settings Center
  useEffect(() => {
    const t = termInstanceRef.current;
    if (!t) return;
    if (terminalCfg) {
      t.options.fontSize = terminalCfg.font_size;
      t.options.cursorBlink = terminalCfg.cursor_blink;
      t.options.cursorStyle = terminalCfg.cursor_style;
      t.options.scrollback = terminalCfg.scrollback;
    }
    const bg =
      themeMode === "oled"
        ? "#000000"
        : themeMode === "slate"
        ? "#171a21"
        : "#121316";
    t.options.theme = {
      ...t.options.theme,
      background: bg,
    };
    try {
      fitAddonInstanceRef.current?.fit();
      syncImeAnchorFnRef.current();
    } catch {
      // Ignore when hidden
    }
  }, [
    terminalCfg?.font_size,
    terminalCfg?.cursor_blink,
    terminalCfg?.cursor_style,
    terminalCfg?.scrollback,
    themeMode,
  ]);

  // Keep IME cursor anchor synced & restore terminal textarea focus when pane/window regains focus
  useEffect(() => {
    if (!focused) return;
    const ensureTerminalFocusAndImeAnchor = () => {
      const active = document.activeElement as HTMLElement | null;
      const isOtherEditable =
        active &&
        (active.tagName === "INPUT" ||
          active.tagName === "SELECT" ||
          (active.tagName === "TEXTAREA" &&
            !active.classList.contains("xterm-helper-textarea")) ||
          active.isContentEditable);
      if (!isOtherEditable) {
        termInstanceRef.current?.focus();
      }
      requestAnimationFrame(() => {
        syncImeAnchorFnRef.current();
      });
    };
    ensureTerminalFocusAndImeAnchor();
    window.addEventListener("focus", ensureTerminalFocusAndImeAnchor);
    return () => {
      window.removeEventListener("focus", ensureTerminalFocusAndImeAnchor);
    };
  }, [focused]);

  useEffect(() => {
    if (terminalCfg?.completion_enabled === false) {
      clearCompletionState(null);
      return;
    }
    const mode = terminalCfg?.completion_mode || "hybrid";
    if (compRef.current.candidates.length > 0) {
      syncCompState({
        listOpen: mode === "list" ? true : mode === "inline" ? false : compRef.current.listOpen,
      });
    }
  }, [terminalCfg?.completion_enabled, terminalCfg?.completion_mode]);

  useEffect(() => {
    if (!compUI.listOpen && (terminalCfg?.completion_mode || "hybrid") !== "list") {
      return;
    }
    const activeEl = popupListRef.current?.querySelector(
      '[data-comp-active="true"]'
    ) as HTMLElement | null;
    activeEl?.scrollIntoView({ block: "nearest" });
  }, [compUI.selectedIndex, compUI.listOpen, terminalCfg?.completion_mode]);

  const handleAcceptCompletionClick = (cand: CompletionCandidate) => {
    const prefix = compRef.current.tokenPrefix;
    clearCompletionState(null);
    acceptSessionCompletion(sessionId, cand.kind, cand.label).catch(() => {});
    const prefixRunes = Array.from(prefix).length;
    const out =
      cand.replace_len === prefixRunes && cand.insert_text.startsWith(prefix)
        ? cand.insert_text.slice(prefix.length)
        : "\x7f".repeat(Math.max(0, cand.replace_len)) + cand.insert_text;
    if (out) {
      if (broadcastRef.current) {
        broadcastRef.current(out);
      } else {
        sendRawBytes(encoder.encode(out));
      }
    }
    termInstanceRef.current?.focus();
  };

  const renderCandidateIcon = (kind: CompletionCandidate["kind"]) => {
    switch (kind) {
      case "git_branch":
        return <GitBranch size={12} className="text-[#a78bfa] shrink-0" />;
      case "npm_script":
        return <FileCode2 size={12} className="text-[#fbbf24] shrink-0" />;
      case "ssh_host":
        return <Server size={12} className="text-[#38bdf8] shrink-0" />;
      case "wsl_distro":
        return <TerminalSquare size={12} className="text-[#fb923c] shrink-0" />;
      case "dir":
        return <FolderOpen size={12} className="text-[#60a5fa] shrink-0" />;
      case "file":
        return <FileText size={12} className="text-[#9ca3af] shrink-0" />;
      default:
        return <Sparkles size={12} className="text-[#34d399] shrink-0" />;
    }
  };

  // Poll lightweight multiplexed /proc stats dynamically according to Monitor settings
  useEffect(() => {
    if (isLocal || (monitorCfg && !monitorCfg.enabled)) {
      return;
    }
    let cancelled = false;
    const poll = async () => {
      try {
        const s = await getSessionStats(sessionId);
        if (!cancelled) {
          setStats(s);
          setRttMs(Math.round(s.rtt_ms));
          onStatsUpdate(s);
        }
      } catch {
        // Non-Linux or disconnected
      }
    };
    poll();
    const intervalMs = Math.max(1000, (monitorCfg?.interval_sec ?? 3) * 1000);
    const timer = window.setInterval(poll, intervalMs);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [sessionId, isLocal, monitorCfg?.enabled, monitorCfg?.interval_sec]);

  const deliverUploadedImageToTerminal = (
    res: { path: string; shell_arg: string },
    intoComposer: boolean
  ) => {
    if (intoComposer) {
      setComposerText((prev) =>
        prev && !prev.endsWith(" ")
          ? `${prev} ${res.shell_arg} `
          : `${prev}${res.shell_arg} `
      );
      showImageUploadBanner(`已上传截图并插入草稿栏: ${res.path}`);
      return;
    }
    const term = termInstanceRef.current;
    if (term) {
      if (term.modes.bracketedPasteMode) {
        // Claude Code CLI / Ink TUI enables ?2004h Bracketed Paste Mode and automatically
        // recognizes a bracketed-pasted image path as [Image #1]!
        term.paste(res.path);
      } else {
        term.paste(`${res.shell_arg} `);
      }
      term.focus();
    } else {
      sendTextToPTY(`${res.shell_arg} `, false);
    }
    showImageUploadBanner(`已上传截图并传入终端: ${res.path}`);
  };

  const uploadAndInsertImageBlob = async (
    blob: Blob,
    nameHint = "clipboard.png",
    intoComposer = false
  ) => {
    setImgUploading(true);
    showImageUploadBanner("正在上传截图到当前会话...");
    try {
      const res = await uploadSessionImage(sessionId, blob, nameHint);
      deliverUploadedImageToTerminal(res, intoComposer);
    } catch (err) {
      setImgToast(null);
      alert(`上传图片失败: ${(err as Error).message}`);
    } finally {
      setImgUploading(false);
    }
  };

  const handleChooseImageClick = async () => {
    if (imgUploading) return;
    const dlg = await openNativeFileDialog("image", false);
    if (!dlg.supported) {
      imgInputRef.current?.click();
      return;
    }
    if (dlg.canceled || dlg.paths.length === 0) return;
    setImgUploading(true);
    showImageUploadBanner("正在上传图片到当前会话...");
    try {
      const res = await uploadSessionImageFromLocalPath(sessionId, dlg.paths[0]);
      deliverUploadedImageToTerminal(
        res,
        Boolean(showComposer || forceComposerOpen)
      );
    } catch (err) {
      setImgToast(null);
      alert(`上传图片失败: ${(err as Error).message}`);
    } finally {
      setImgUploading(false);
    }
  };

  const pasteClipboardTextOrImage = async (
    imageOnly = false,
    intoComposer = false
  ) => {
    const now = Date.now();
    if (now - pasteGuardTsRef.current < 160) return;
    pasteGuardTsRef.current = now;

    // 1. Native Win32 OS Clipboard (supports Win+Shift+S, WeChat Alt+A, QQ, PrintScreen, Explorer Ctrl+C!)
    const nativeClip = await readNativeClipboard();
    if (nativeClip.supported) {
      if (nativeClip.kind === "image" && nativeClip.data_base64) {
        setImgUploading(true);
        showImageUploadBanner("正在上传剪贴板截图...");
        try {
          const res = await uploadSessionImageBase64(
            sessionId,
            nativeClip.data_base64,
            nativeClip.file_name || "screenshot.png"
          );
          deliverUploadedImageToTerminal(res, intoComposer);
        } catch (err) {
          setImgToast(null);
          alert(`上传剪贴板截图失败: ${(err as Error).message}`);
        } finally {
          setImgUploading(false);
        }
        return;
      }
      if (nativeClip.kind === "image_path" && nativeClip.local_path) {
        setImgUploading(true);
        showImageUploadBanner("正在上传复制的图片文件...");
        try {
          const res = await uploadSessionImageFromLocalPath(
            sessionId,
            nativeClip.local_path
          );
          deliverUploadedImageToTerminal(res, intoComposer);
        } catch (err) {
          setImgToast(null);
          alert(`上传图片失败: ${(err as Error).message}`);
        } finally {
          setImgUploading(false);
        }
        return;
      }
      if (!imageOnly && nativeClip.kind === "text" && nativeClip.text) {
        if (intoComposer) {
          setComposerText((prev) => prev + nativeClip.text);
        } else if (termInstanceRef.current) {
          termInstanceRef.current.paste(nativeClip.text);
          termInstanceRef.current.focus();
        } else {
          sendTextToPTY(nativeClip.text, false);
        }
      }
      return;
    }

    // 2. Browser devserver fallback when outside Wails desktop shell
    if (!imageOnly) {
      try {
        const clip = await navigator.clipboard.readText();
        if (clip) {
          if (termInstanceRef.current) {
            termInstanceRef.current.paste(clip);
          } else {
            sendTextToPTY(clip, false);
          }
          return;
        }
      } catch {
        // Ignore when clipboard holds image
      }
    }
    try {
      if (navigator.clipboard.read) {
        const items = await navigator.clipboard.read();
        for (const item of items) {
          const imgType = item.types.find((t) => t.startsWith("image/"));
          if (imgType) {
            const blob = await item.getType(imgType);
            const ext = imgType.split("/")[1] || "png";
            await uploadAndInsertImageBlob(
              blob,
              `clipboard.${ext}`,
              intoComposer
            );
            return;
          }
        }
      }
    } catch {
      // Ignore
    }
  };

  smartPasteFnRef.current = (imageOnly = false) => {
    pasteClipboardTextOrImage(imageOnly, false);
  };

  const handleTerminalContextMenu = async (e: React.MouseEvent) => {
    e.preventDefault();
    onFocus();
    const action = terminalCfg?.right_click_action ?? "menu";
    if (e.shiftKey || action === "menu") {
      setCtxMenu({ x: e.clientX, y: e.clientY });
      return;
    }
    const term = termInstanceRef.current;
    if (action === "copy_or_paste" && term?.hasSelection()) {
      const sel = term.getSelection();
      if (sel) {
        await navigator.clipboard.writeText(sel).catch(() => {});
        term.clearSelection();
      }
      return;
    }
    await pasteClipboardTextOrImage();
  };

  const diskPct = stats?.disks?.[0]?.used_percent ?? 0;
  const compMode = terminalCfg?.completion_mode || "hybrid";
  const compVisible =
    (terminalCfg?.completion_enabled ?? true) &&
    compUI.candidates.length > 0 &&
    compUI.dismissedLine !== compUI.line;
  const activeCompletionCand =
    compUI.candidates[compUI.selectedIndex] || compUI.candidates[0] || null;
  const showInlineGhost =
    compVisible &&
    (compMode === "inline" || compMode === "hybrid") &&
    Boolean(activeCompletionCand?.ghost_suffix);
  const showCompletionPopup =
    compVisible &&
    (compMode === "list" || (compMode === "hybrid" && compUI.listOpen));
  const popupFlipUp =
    compUI.y + compUI.cellH + 215 > compUI.paneH && compUI.y > 215;
  const popupLeft = Math.max(
    8,
    Math.min(compUI.x, Math.max(8, compUI.paneW - 340))
  );

  const showBottomBar =
    !isLocal &&
    (hostMetricsBar === "on"
      ? true
      : hostMetricsBar === "off"
      ? false
      : monitorCfg?.show_bottom_bar ?? true);

  return (
    <div
      onMouseDown={onFocus}
      onContextMenu={handleTerminalContextMenu}
      onPasteCapture={(e) => {
        const plain = e.clipboardData?.getData("text/plain");
        if (plain) return;
        const items = e.clipboardData?.items;
        if (!items) return;
        for (let i = 0; i < items.length; i++) {
          const it = items[i];
          if (it.type.startsWith("image/")) {
            const blob = it.getAsFile();
            if (blob) {
              e.preventDefault();
              e.stopPropagation();
              const targetTag = (e.target as HTMLElement | null)?.tagName;
              uploadAndInsertImageBlob(
                blob,
                blob.name || "clipboard.png",
                targetTag === "TEXTAREA"
              );
              return;
            }
          }
        }
      }}
      onDragOver={(e) => {
        if (e.dataTransfer?.types?.includes("Files")) {
          e.preventDefault();
        }
      }}
      onDrop={(e) => {
        const files = e.dataTransfer?.files;
        if (!files || files.length === 0) return;
        e.preventDefault();
        e.stopPropagation();
        const first = files[0];
        if (first) {
          uploadAndInsertImageBlob(first, first.name, false);
        }
      }}
      className={`flex-1 flex flex-col rounded-lg overflow-hidden bg-[#121316] min-w-0 min-h-0 border transition-colors relative ${
        focused ? "border-[#3b82f6]" : "border-[#2a2c33]"
      }`}
    >
      <input
        ref={imgInputRef}
        type="file"
        accept="image/*"
        className="hidden"
        onChange={(ev) => {
          const f = ev.target.files?.[0];
          ev.target.value = "";
          if (f) {
            uploadAndInsertImageBlob(
              f,
              f.name,
              Boolean(showComposer || forceComposerOpen)
            );
          }
        }}
      />

      {/* Pane Top Header */}
      <div className="h-7 px-2.5 bg-[#1b1c20] border-b border-[#272930] flex items-center justify-between text-xs shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <span
            className={`w-2 h-2 rounded-full shrink-0 ${
              connected ? "bg-[#3fb950]" : "bg-[#ef4444]"
            }`}
          />
          <span
            className={`px-1.5 py-0.5 rounded font-semibold text-[10px] leading-none ${
              isLocal
                ? "bg-[#3b82f6]/20 text-[#60a5fa]"
                : "bg-[#f59e0b]/20 text-[#fbbf24]"
            }`}
          >
            {isLocal ? "本地" : "SSH"}
          </span>
          <span className="font-semibold text-[#f3f4f6] truncate">{hostTitle}</span>
          <span className="text-[#8b909a] font-mono truncate">{userTarget}</span>
        </div>

        <div className="flex items-center gap-1 text-[#8b909a] shrink-0">
          <button
            onClick={handleChooseImageClick}
            disabled={imgUploading}
            className={`p-1 rounded hover:bg-[#2a2d36] ${
              imgUploading
                ? "text-[#60a5fa] animate-pulse"
                : "hover:text-[#e5e7eb]"
            }`}
            title="上传图片并插入路径 (支持 Ctrl+V 粘贴截图或拖入图片，适配远程 Claude CLI)"
          >
            <ImagePlus size={13} />
          </button>
          <button
            onClick={() => setShowSearch((v) => !v)}
            className="p-1 rounded hover:bg-[#2a2d36] hover:text-[#e5e7eb]"
            title="终端内搜索"
          >
            <Search size={13} />
          </button>
          <button
            onClick={() => setShowComposer((v) => !v)}
            className={`p-1 rounded hover:bg-[#2a2d36] ${
              showComposer ? "text-[#60a5fa]" : "hover:text-[#e5e7eb]"
            }`}
            title="多行中文/代码防误触草稿栏"
          >
            <Keyboard size={13} />
          </button>
          {!isLocal && (
            <button
              onClick={onOpenSFTP}
              className="p-1 rounded hover:bg-[#2a2d36] hover:text-[#e5e7eb]"
              title="打开 SFTP 文件面板"
            >
              <FolderOpen size={13} />
            </button>
          )}
          <button
            onClick={onSplitHorizontal}
            className="p-1 rounded hover:bg-[#2a2d36] hover:text-[#e5e7eb]"
            title="向右分屏"
          >
            <Columns2 size={13} />
          </button>
          <button
            onClick={onSplitVertical}
            className="p-1 rounded hover:bg-[#2a2d36] hover:text-[#e5e7eb]"
            title="向下分屏"
          >
            <Rows2 size={13} />
          </button>
          <button
            onClick={onClosePane}
            className="p-1 rounded hover:bg-[#ef4444]/20 hover:text-[#f87171]"
            title="关闭面板"
          >
            <X size={13} />
          </button>
        </div>
      </div>

      {/* Optional Search Strip */}
      {showSearch && (
        <div className="px-2.5 py-1 bg-[#1e2026] border-b border-[#2a2c33] flex items-center gap-2 text-xs">
          <input
            value={searchQuery}
            onChange={(e) => {
              setSearchQuery(e.target.value);
              searchAddonRef.current?.findNext(e.target.value);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") searchAddonRef.current?.findNext(searchQuery);
              if (e.key === "Escape") setShowSearch(false);
            }}
            placeholder="查找终端输出 (Enter 下一个)..."
            className="flex-1 bg-[#121316] border border-[#2f323b] rounded px-2 py-0.5 outline-none text-[#e5e7eb]"
            autoFocus
          />
          <button
            onClick={() => setShowSearch(false)}
            className="text-[#8b909a] hover:text-[#e5e7eb]"
          >
            关闭
          </button>
        </div>
      )}

      {/* WebGL xterm.js Canvas + Floating Overlays */}
      <div className="relative flex-1 w-full min-h-0 overflow-hidden flex flex-col">
        <div ref={containerRef} className="flex-1 w-full min-h-0 overflow-hidden" />

        {/* Built-in Native Inline Ghost Text ("灰字") */}
        {showInlineGhost && activeCompletionCand && (
          <div
            style={{
              left: compUI.x,
              top: compUI.y,
              height: compUI.cellH,
              lineHeight: `${compUI.cellH}px`,
              fontSize: `${terminalCfg?.font_size ?? 13.5}px`,
              fontFamily:
                terminalCfg?.font_family ||
                '"Cascadia Mono", "JetBrains Mono", Consolas, "Courier New", monospace',
            }}
            className="absolute z-10 pointer-events-none select-none whitespace-pre text-[#6e7681]"
          >
            {activeCompletionCand.ghost_suffix}
          </div>
        )}

        {/* Built-in Cursor-Anchored Candidate List Popup */}
        {showCompletionPopup && (
          <div
            onMouseDown={(e) => {
              e.preventDefault();
              e.stopPropagation();
            }}
            style={{
              left: popupLeft,
              ...(popupFlipUp
                ? { bottom: Math.max(6, compUI.paneH - compUI.y + 4) }
                : { top: compUI.y + compUI.cellH + 3 }),
            }}
            className="absolute z-20 min-w-[270px] max-w-[420px] rounded-xl bg-[#1b1d24]/95 backdrop-blur-md border border-[#323644] shadow-2xl overflow-hidden text-xs"
          >
            <div
              ref={popupListRef}
              className="max-h-[184px] overflow-y-auto py-1 divide-y divide-[#252833]/50"
            >
              {compUI.candidates.map((cand, idx) => {
                const selected = idx === compUI.selectedIndex;
                return (
                  <div
                    key={`${cand.kind}-${cand.label}-${idx}`}
                    data-comp-active={selected ? "true" : "false"}
                    onMouseEnter={() => syncCompState({ selectedIndex: idx })}
                    onMouseDown={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      handleAcceptCompletionClick(cand);
                    }}
                    className={`px-2.5 py-1.5 flex items-center justify-between gap-3 cursor-pointer transition-colors ${
                      selected
                        ? "bg-[#2563eb]/25 text-white"
                        : "text-[#d1d5db] hover:bg-[#262934]"
                    }`}
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      {renderCandidateIcon(cand.kind)}
                      <span className="font-mono truncate font-medium">
                        {cand.label}
                      </span>
                      {cand.detail && (
                        <span className="text-[11px] text-[#7c828e] truncate max-w-[140px]">
                          {cand.detail}
                        </span>
                      )}
                    </div>
                    <span
                      className={`px-1.5 py-0.5 rounded text-[10px] shrink-0 ${
                        selected
                          ? "bg-[#2563eb]/35 text-[#93c5fd]"
                          : "bg-[#252833] text-[#8b909a]"
                      }`}
                    >
                      {cand.source}
                    </span>
                  </div>
                );
              })}
            </div>

            <div className="px-2.5 py-1 bg-[#15171d] border-t border-[#282b36] flex items-center justify-between gap-2 text-[10px] text-[#7c828e]">
              {compMode === "hybrid" ? (
                <div className="flex items-center gap-2.5 flex-wrap">
                  <span>
                    <kbd className="px-1 py-0.5 rounded bg-[#232630] text-[#cbd5e1] font-mono">
                      Tab
                    </kbd>{" "}
                    轮换
                  </span>
                  <span>
                    <kbd className="px-1 py-0.5 rounded bg-[#232630] text-[#cbd5e1] font-mono">
                      Enter
                    </kbd>{" "}
                    插入不执行
                  </span>
                  <span>
                    <kbd className="px-1 py-0.5 rounded bg-[#232630] text-[#cbd5e1] font-mono">
                      →
                    </kbd>{" "}
                    接受灰字
                  </span>
                  <span>
                    <kbd className="px-1 py-0.5 rounded bg-[#232630] text-[#cbd5e1] font-mono">
                      Esc
                    </kbd>{" "}
                    关闭
                  </span>
                </div>
              ) : (
                <div className="flex items-center gap-2.5">
                  <span>
                    <kbd className="px-1 py-0.5 rounded bg-[#232630] text-[#cbd5e1] font-mono">
                      ↑↓
                    </kbd>{" "}
                    选择
                  </span>
                  <span>
                    <kbd className="px-1 py-0.5 rounded bg-[#232630] text-[#cbd5e1] font-mono">
                      Tab
                    </kbd>{" "}
                    接受候选
                  </span>
                  <span>
                    <kbd className="px-1 py-0.5 rounded bg-[#232630] text-[#cbd5e1] font-mono">
                      Esc
                    </kbd>{" "}
                    关闭
                  </span>
                </div>
              )}
              <span className="font-mono text-[#6b7280] shrink-0">
                {compUI.selectedIndex + 1}/{compUI.candidates.length}
              </span>
            </div>
          </div>
        )}

        {/* Image Uploaded Toast */}
        {imgToast && (
          <div className="absolute top-2.5 left-3 z-30 px-3 py-1.5 rounded-xl bg-[#1d2027]/95 border border-[#3b82f6]/50 shadow-2xl flex items-center gap-2 text-xs text-[#e5e7eb]">
            <ImagePlus size={13} className="text-[#60a5fa] shrink-0" />
            <span className="font-mono text-[11px] truncate max-w-[360px]">
              {imgToast}
            </span>
          </div>
        )}

        {/* OSC 52 Ask Confirmation Card */}
        {pendingOsc52 && (
          <div className="absolute top-2.5 right-3 z-30 px-3.5 py-2.5 rounded-xl bg-[#1f2128]/95 border border-[#3b82f6]/60 shadow-2xl flex items-center gap-3 text-xs">
            <div>
              <div className="font-medium text-white">
                远端请求写入剪贴板 (OSC 52)
              </div>
              <div className="text-[11px] text-[#9ca3af]">
                来源：{hostTitle} ({formatBytes(pendingOsc52.byteLen)})
              </div>
            </div>
            <button
              onClick={() => {
                const { text, byteLen } = pendingOsc52;
                setPendingOsc52(null);
                performOsc52Write(text, byteLen);
              }}
              className="px-2.5 py-1 rounded-lg bg-[#2563eb] hover:bg-[#3b82f6] text-white font-medium"
            >
              允许
            </button>
            <button
              onClick={() => setPendingOsc52(null)}
              className="px-2.5 py-1 rounded-lg bg-[#2c2f38] hover:bg-[#383c47] text-[#d1d5db]"
            >
              拒绝
            </button>
          </div>
        )}

        {/* OSC 52 Copy Source & Undo Toast ("远程复制时提示：显示写入来源并可撤销") */}
        {osc52Toast && (
          <div className="absolute top-2.5 right-3 z-30 px-3 py-2 rounded-xl bg-[#1d2027]/95 border border-[#343844] shadow-2xl flex items-center gap-2.5 text-xs">
            <ClipboardCheck size={14} className="text-[#3fb950] shrink-0" />
            <span className="text-[#e5e7eb]">
              已从 <strong className="text-white">{hostTitle}</strong> 复制 (
              {formatBytes(osc52Toast.byteLen)})
            </span>
            {osc52Toast.prevText !== null && (
              <button
                onClick={async () => {
                  const prev = osc52Toast.prevText ?? "";
                  setOsc52Toast(null);
                  await navigator.clipboard.writeText(prev).catch(() => {});
                }}
                className="flex items-center gap-1 px-2 py-0.5 rounded bg-[#2b2e38] hover:bg-[#383c48] text-[#60a5fa] font-medium"
              >
                <RotateCcw size={11} />
                <span>撤销</span>
              </button>
            )}
          </div>
        )}

        {/* Disconnect / Auto-Reconnect Banner */}
        {!connected && onReconnect && (
          <div className="absolute bottom-3 right-3 px-3 py-1.5 rounded-lg bg-[#1e2026]/95 border border-[#ef4444]/50 shadow-xl flex items-center gap-2.5 text-xs">
            <span className="text-[#f87171]">
              {autoReconnecting ? "连接中断，正在自动重连..." : "会话已断开"}
            </span>
            {autoReconnecting && (
              <button
                onClick={() => {
                  if (autoReconnectTimerRef.current) {
                    window.clearTimeout(autoReconnectTimerRef.current);
                  }
                  setAutoReconnecting(false);
                }}
                className="px-2 py-1 rounded bg-[#2b2e38] text-[#d1d5db]"
              >
                取消
              </button>
            )}
            <button
              onClick={() => {
                if (autoReconnectTimerRef.current) {
                  window.clearTimeout(autoReconnectTimerRef.current);
                }
                setAutoReconnecting(false);
                onReconnect(lastReportedDirRef.current || undefined);
              }}
              className="px-2.5 py-1 rounded bg-[#2563eb] hover:bg-[#3b82f6] text-white font-medium"
            >
              重新连接
            </button>
          </div>
        )}
      </div>

      {/* Right-Click Context Menu ("右键：菜单 / 按住 Shift 右键始终打开菜单") */}
      {ctxMenu && (
        <div
          onMouseDown={(e) => e.stopPropagation()}
          style={{
            left: Math.min(ctxMenu.x, window.innerWidth - 160),
            top: Math.min(ctxMenu.y, window.innerHeight - 170),
          }}
          className="fixed z-50 w-36 py-1 rounded-xl bg-[#22242a] border border-[#343741] shadow-2xl text-xs text-[#e5e7eb]"
        >
          <button
            onClick={async () => {
              const sel = termInstanceRef.current?.getSelection();
              setCtxMenu(null);
              if (sel) {
                await navigator.clipboard.writeText(sel).catch(() => {});
              }
            }}
            className="w-full px-3 py-1.5 text-left hover:bg-[#2f323c] flex justify-between"
          >
            <span>复制</span>
            <span className="text-[#7c828e] font-mono text-[10px]">Ctrl+Shift+C</span>
          </button>
          <button
            onClick={async () => {
              setCtxMenu(null);
              await pasteClipboardTextOrImage();
            }}
            className="w-full px-3 py-1.5 text-left hover:bg-[#2f323c] flex justify-between"
          >
            <span>粘贴 (文本/图片)</span>
            <span className="text-[#7c828e] font-mono text-[10px]">Ctrl+V</span>
          </button>
          <button
            onClick={() => {
              setCtxMenu(null);
              termInstanceRef.current?.selectAll();
            }}
            className="w-full px-3 py-1.5 text-left hover:bg-[#2f323c]"
          >
            全选
          </button>
          <div className="my-1 border-t border-[#31343e]" />
          <button
            onClick={() => {
              setCtxMenu(null);
              setShowSearch(true);
            }}
            className="w-full px-3 py-1.5 text-left hover:bg-[#2f323c]"
          >
            查找输出...
          </button>
          <button
            onClick={() => {
              setCtxMenu(null);
              termInstanceRef.current?.clear();
            }}
            className="w-full px-3 py-1.5 text-left hover:bg-[#2f323c]"
          >
            清空回滚屏幕
          </button>
        </div>
      )}

      {/* Contextual Floating Approval Pill (Only visible when Claude CLI awaits confirmation) */}
      {approvalSnippet && (
        <div className="mx-2.5 mb-1.5 px-3 py-1.5 rounded-lg bg-[#1e2026] border border-[#f59e0b]/70 flex items-center justify-between gap-3 text-xs shadow-lg">
          <div className="truncate text-[#fbbf24]">
            <span className="font-semibold mr-1.5">⚠ Claude 等待确认:</span>
            <span className="font-mono text-[#e5e7eb]">{approvalSnippet}</span>
          </div>
          <div className="flex items-center gap-1.5 shrink-0">
            <button
              onClick={() => {
                sendTextToPTY("1\r");
                setApprovalSnippet(null);
              }}
              className="px-2.5 py-0.5 rounded bg-[#2563eb] hover:bg-[#3b82f6] text-white font-medium"
            >
              允许一次 (1)
            </button>
            <button
              onClick={() => {
                sendTextToPTY("2\r");
                setApprovalSnippet(null);
              }}
              className="px-2 py-0.5 rounded bg-[#2a2d36] hover:bg-[#353944] text-[#e5e7eb]"
            >
              始终 (2)
            </button>
            <button
              onClick={() => {
                sendTextToPTY("\x1b");
                setApprovalSnippet(null);
              }}
              className="px-2 py-0.5 rounded border border-[#ef4444]/50 text-[#f87171] hover:bg-[#ef4444]/15"
            >
              拒绝 (Esc)
            </button>
          </div>
        </div>
      )}

      {/* Optional Multi-line Bracketed Paste Prompt Composer */}
      {(showComposer || forceComposerOpen) && (
        <div className="mx-2 mb-1.5 p-1.5 rounded-lg bg-[#1b1c20] border border-[#2f323b] flex items-end gap-2 text-xs">
          <textarea
            value={composerText}
            onChange={(e) => setComposerText(e.target.value)}
            onPaste={async (e) => {
              const plain = e.clipboardData?.getData("text/plain");
              if (plain) return;
              e.preventDefault();
              await pasteClipboardTextOrImage(true, true);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
                e.preventDefault();
                if (composerText.trim()) {
                  sendTextToPTY(composerText, true);
                  setComposerText("");
                }
              }
            }}
            rows={2}
            placeholder="多行中文/代码草稿框（防回车误触，按 Ctrl+Enter 整段通过 Bracketed Paste 发送）..."
            className="flex-1 bg-transparent resize-none outline-none text-[#e5e7eb] placeholder:text-[#6b7280] font-sans"
          />
          <button
            onClick={() => {
              if (composerText.trim()) {
                sendTextToPTY(composerText, true);
                setComposerText("");
              }
            }}
            className="px-2.5 py-1 rounded bg-[#2563eb] hover:bg-[#3b82f6] text-white flex items-center gap-1 shrink-0"
          >
            <Send size={12} />
            <span>发送</span>
          </button>
        </div>
      )}

      {/* Bottom Inline Telemetry Strip */}
      {showBottomBar && (
        <div className="h-6 px-2.5 bg-[#18191d] border-t border-[#262830] flex items-center justify-between text-[11px] font-mono shrink-0">
          <div className="flex items-center gap-3 overflow-hidden whitespace-nowrap">
            <span className="text-[#3fb950]">
              CPU {stats ? `${stats.cpu_percent.toFixed(0)}%` : "--"}
            </span>
            <span className="text-[#f59e0b]">
              内存{" "}
              {stats
                ? `${formatBytes(stats.mem_used_bytes)}/${formatBytes(stats.mem_total_bytes)}`
                : "--"}
            </span>
            <span className="text-[#3fb950]">
              交换{" "}
              {stats && stats.swap_total_bytes > 0
                ? `${formatBytes(stats.swap_used_bytes)}/${formatBytes(stats.swap_total_bytes)}`
                : "0K/0G"}
            </span>
            <span className="text-[#4ade80]">
              磁盘 {stats ? `${diskPct.toFixed(0)}%` : "--"}
            </span>
            <span className="text-[#60a5fa]">
              ↑ {stats ? formatRate(stats.total_tx_bytes_per_sec) : "0B/s"} ↓{" "}
              {stats ? formatRate(stats.total_rx_bytes_per_sec) : "0B/s"}
            </span>
            <span className="text-[#4ade80]">
              负载 {stats ? `${stats.load_percent.toFixed(0)}%` : "--"}
            </span>
          </div>

          <div className="flex items-center gap-1 text-[#3fb950] shrink-0 pl-2">
            <Zap size={11} />
            <span>{rttMs !== null ? `${rttMs} 毫秒` : "--"}</span>
          </div>
        </div>
      )}
    </div>
  );
};
