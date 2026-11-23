import React, { useEffect, useRef, useState } from "react";
import {
  Bot,
  ChevronDown,
  ChevronRight,
  Send,
  Settings,
  ShieldAlert,
  ShieldCheck,
  Sparkles,
  TerminalSquare,
  Wrench,
  X,
} from "lucide-react";
import {
  AIExecutedStep,
  AIMessage,
  AppSettings,
  SystemStats,
  aiApprove,
  aiChat,
  createSession,
  deleteSession,
  getSessionScrollback,
  saveSettings,
} from "../api";
import { TerminalPane } from "./TerminalPane";

interface ChatTurnItem {
  id: string;
  role: "user" | "assistant";
  text: string;
  steps?: AIExecutedStep[];
  pending?: AIExecutedStep;
}

interface Props {
  hostId?: string;
  hostTitle: string;
  userTarget: string;
  primarySessionId: string;
  settings: AppSettings;
  onUpdateSettings: (s: AppSettings) => void;
  onOpenAISettings: () => void;
  onClose: () => void;
  onStatsUpdate: (s: SystemStats) => void;
}

function renderSimpleMarkdown(md: string): React.ReactNode {
  if (!md) return null;
  const lines = md.split("\n");
  const blocks: React.ReactNode[] = [];
  let i = 0;

  const formatInline = (text: string): React.ReactNode[] => {
    const parts = text.split(/(`[^`]+`|\*\*[^*]+\*\*)/g);
    return parts.map((part, idx) => {
      if (part.startsWith("`") && part.endsWith("`")) {
        return (
          <code
            key={idx}
            className="px-1.5 py-0.5 rounded bg-[#16171b] border border-[#2d3039] font-mono text-[11px] text-[#e5e7eb]"
          >
            {part.slice(1, -1)}
          </code>
        );
      }
      if (part.startsWith("**") && part.endsWith("**")) {
        return (
          <strong key={idx} className="font-semibold text-white">
            {part.slice(2, -2)}
          </strong>
        );
      }
      return part;
    });
  };

  while (i < lines.length) {
    const line = lines[i];

    if (line.trim().startsWith("```")) {
      const codeLines: string[] = [];
      i++;
      while (i < lines.length && !lines[i].trim().startsWith("```")) {
        codeLines.push(lines[i]);
        i++;
      }
      i++;
      blocks.push(
        <pre
          key={`code-${i}`}
          className="my-2 p-2.5 rounded-lg bg-[#141519] border border-[#2b2e37] font-mono text-[11px] overflow-x-auto text-[#d1d5db]"
        >
          {codeLines.join("\n")}
        </pre>
      );
      continue;
    }

    if (
      line.includes("|") &&
      i + 1 < lines.length &&
      /^\s*\|?[\s:-]+\|[\s|:-]*$/.test(lines[i + 1])
    ) {
      const parseCells = (row: string) =>
        row
          .trim()
          .replace(/^\||\|$/g, "")
          .split("|")
          .map((c) => c.trim());

      const headers = parseCells(line);
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length && lines[i].includes("|") && lines[i].trim() !== "") {
        rows.push(parseCells(lines[i]));
        i++;
      }

      blocks.push(
        <div
          key={`tbl-${i}`}
          className="my-2 rounded-lg border border-[#2e313a] overflow-hidden bg-[#1e2026]"
        >
          <table className="w-full text-xs border-collapse">
            <thead>
              <tr className="bg-[#252830] border-b border-[#2e313a]">
                {headers.map((h, hIdx) => (
                  <th
                    key={hIdx}
                    className={`px-3 py-1.5 font-medium text-[#d1d5db] ${
                      hIdx === headers.length - 1 ? "text-right" : "text-left"
                    }`}
                  >
                    {formatInline(h)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-[#282b33]">
              {rows.map((r, rIdx) => (
                <tr key={rIdx}>
                  {r.map((cell, cIdx) => (
                    <td
                      key={cIdx}
                      className={`px-3 py-1.5 text-[#e5e7eb] ${
                        cIdx === r.length - 1 ? "text-right font-mono" : "text-left"
                      }`}
                    >
                      {formatInline(cell)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
      continue;
    }

    if (/^\s*[-*]\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^\s*[-*]\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\s*[-*]\s+/, ""));
        i++;
      }
      blocks.push(
        <ul key={`ul-${i}`} className="my-1.5 pl-4 list-disc space-y-1 text-[#d1d5db]">
          {items.map((it, idx) => (
            <li key={idx}>{formatInline(it)}</li>
          ))}
        </ul>
      );
      continue;
    }

    if (line.trim() !== "") {
      blocks.push(
        <p key={`p-${i}`} className="my-1.5 leading-relaxed text-[#e5e7eb]">
          {formatInline(line)}
        </p>
      );
    }
    i++;
  }

  return <>{blocks}</>;
}

export const AIPane: React.FC<Props> = ({
  hostId,
  hostTitle,
  userTarget,
  primarySessionId,
  settings,
  onUpdateSettings,
  onOpenAISettings,
  onClose,
  onStatsUpdate,
}) => {
  const [mode, setMode] = useState<"builtin" | "claude_pty">(
    settings.ai.default_agent === "claude_cli" ? "claude_pty" : "builtin"
  );
  const [policy, setPolicy] = useState<"ask" | "auto_review" | "allow_all">(
    settings.ai.default_permission || "auto_review"
  );
  const providers = settings.ai.providers || [];
  const defaultProv =
    providers.find((p) => p.is_default) || providers[0] || null;
  const [selectedProvId, setSelectedProvId] = useState<string>(
    defaultProv?.id ?? ""
  );

  const [turns, setTurns] = useState<ChatTurnItem[]>([]);
  const [rawHistory, setRawHistory] = useState<AIMessage[]>([]);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [expandedSteps, setExpandedSteps] = useState<Record<string, boolean>>({});
  const scrollRef = useRef<HTMLDivElement | null>(null);

  const [claudeSessionId, setClaudeSessionId] = useState<string | null>(null);
  const [claudeLaunching, setClaudeLaunching] = useState(false);

  useEffect(() => {
    if (!selectedProvId && defaultProv) {
      setSelectedProvId(defaultProv.id);
    }
  }, [defaultProv, selectedProvId]);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [turns, busy]);

  const switchToClaudePTY = async () => {
    setMode("claude_pty");
    if (claudeSessionId || claudeLaunching) return;
    setClaudeLaunching(true);
    try {
      const s = await createSession({
        host_id: hostId,
        target: hostId ? undefined : userTarget.split("@")[1],
        user: hostId ? undefined : userTarget.split("@")[0],
        cols: 90,
        rows: 30,
        relay: {
          mode: "auto",
          session_name: "claude-ai",
        },
      });
      setClaudeSessionId(s.id);
    } catch (e) {
      alert("启动远端 Claude 接力终端失败: " + (e as Error).message);
      setMode("builtin");
    } finally {
      setClaudeLaunching(false);
    }
  };

  useEffect(() => {
    if (mode === "claude_pty" && !claudeSessionId && !claudeLaunching) {
      switchToClaudePTY();
    }
  }, [mode]);

  useEffect(() => {
    return () => {
      if (claudeSessionId) {
        deleteSession(claudeSessionId).catch(() => {});
      }
    };
  }, [claudeSessionId]);

  const ensureHostConfirmed = async (): Promise<boolean> => {
    const confirmed = settings.ai.confirmed_hosts || [];
    if (confirmed.includes(hostTitle)) return true;
    const ok = window.confirm(
      `确认向所选模型服务商发送主机「${hostTitle}」的数据吗？\n（当前敏感凭据自动脱敏：${
        settings.ai.redact_secrets ? "已开启" : "未开启"
      }，可在 设置 -> AI 助理 中随时撤销授权）`
    );
    if (!ok) return false;
    const next: AppSettings = JSON.parse(JSON.stringify(settings));
    next.ai.confirmed_hosts = [...confirmed, hostTitle];
    const saved = await saveSettings(next);
    onUpdateSettings(saved);
    return true;
  };

  const handleSend = async () => {
    const q = input.trim();
    if (!q || busy) return;

    if (providers.length === 0) {
      onOpenAISettings();
      return;
    }
    if (!(await ensureHostConfirmed())) {
      return;
    }

    setInput("");
    setBusy(true);

    const userMsg: AIMessage = { role: "user", content: q };
    const nextMsgs = [...rawHistory, userMsg];
    const turnId = `t-${Date.now()}`;

    setTurns((prev) => [...prev, { id: turnId, role: "user", text: q }]);

    try {
      let termCtx = "";
      if (settings.ai.context_lines > 0) {
        termCtx = await getSessionScrollback(
          primarySessionId,
          settings.ai.context_lines
        ).catch(() => "");
      }
      const activeProv =
        providers.find((p) => p.id === selectedProvId) || defaultProv;

      const res = await aiChat(
        primarySessionId,
        nextMsgs,
        policy,
        activeProv?.id,
        activeProv?.model,
        termCtx
      );
      setRawHistory(res.history);
      setTurns((prev) => [
        ...prev,
        {
          id: `${turnId}-reply`,
          role: "assistant",
          text: res.reply,
          steps: res.steps,
          pending: res.pending_approval,
        },
      ]);
    } catch (e) {
      setTurns((prev) => [
        ...prev,
        {
          id: `${turnId}-err`,
          role: "assistant",
          text: `**错误：** ${(e as Error).message}`,
        },
      ]);
    } finally {
      setBusy(false);
    }
  };

  const handleDecision = async (
    turnId: string,
    pending: AIExecutedStep,
    approved: boolean
  ) => {
    setBusy(true);
    try {
      const activeProv =
        providers.find((p) => p.id === selectedProvId) || defaultProv;
      const res = await aiApprove(primarySessionId, {
        call_id: pending.call_id,
        command: pending.command,
        approved,
        history: rawHistory,
        policy,
        model: activeProv?.model,
      });
      setRawHistory(res.history);
      setTurns((prev) =>
        prev.map((t) =>
          t.id === turnId
            ? {
                ...t,
                text: res.reply,
                steps: res.steps,
                pending: res.pending_approval,
              }
            : t
        )
      );
    } catch (e) {
      alert((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const cyclePolicy = () => {
    setPolicy((cur) =>
      cur === "auto_review"
        ? "ask"
        : cur === "ask"
        ? "allow_all"
        : "auto_review"
    );
  };

  const policyLabel =
    policy === "auto_review"
      ? "自动审核"
      : policy === "ask"
      ? "每次询问"
      : "完全允许";

  return (
    <div className="w-[46%] min-w-[340px] h-full rounded-lg border border-[#2a2c33] bg-[#18191d] flex flex-col overflow-hidden text-xs shrink-0">
      {/* Top Header */}
      <div className="h-7 px-2.5 bg-[#1b1c20] border-b border-[#272930] flex items-center justify-between shrink-0">
        <div className="flex items-center gap-1.5 font-medium text-[#e5e7eb]">
          <Bot size={13} className="text-[#f59e0b]" />
          <span>AI · {hostTitle}</span>
        </div>

        <div className="flex items-center gap-1.5">
          <button
            onClick={() =>
              mode === "builtin" ? switchToClaudePTY() : setMode("builtin")
            }
            className="px-2 py-0.5 rounded bg-[#24262d] border border-[#31343e] text-[#d1d5db] hover:text-white flex items-center gap-1 text-[11px]"
          >
            {mode === "builtin" ? (
              <>
                <span>内置</span>
                <ChevronDown size={11} />
              </>
            ) : (
              <>
                <TerminalSquare size={11} className="text-[#3fb950]" />
                <span>远端 Claude CLI</span>
                <ChevronDown size={11} />
              </>
            )}
          </button>

          <button
            onClick={onOpenAISettings}
            className="p-1 rounded hover:bg-[#2a2d36] text-[#8b909a] hover:text-white"
            title="打开「设置 -> AI 助理」配置模型服务商与审核策略"
          >
            <Settings size={13} />
          </button>
          <button
            onClick={onClose}
            className="p-1 rounded hover:bg-[#ef4444]/20 text-[#8b909a] hover:text-[#f87171]"
          >
            <X size={13} />
          </button>
        </div>
      </div>

      {mode === "claude_pty" ? (
        <div className="flex-1 flex flex-col min-h-0 p-1">
          {claudeSessionId ? (
            <TerminalPane
              paneId={`claude-${claudeSessionId}`}
              sessionId={claudeSessionId}
              hostTitle={`${hostTitle} (claude-ai)`}
              userTarget={userTarget}
              focused={false}
              terminalCfg={settings.terminal}
              monitorCfg={settings.monitor}
              onFocus={() => {}}
              onSplitHorizontal={() => {}}
              onSplitVertical={() => {}}
              onOpenSFTP={() => {}}
              onClosePane={() => setMode("builtin")}
              onStatsUpdate={onStatsUpdate}
              onAIStateChange={() => {}}
            />
          ) : (
            <div className="flex-1 flex items-center justify-center text-[#8b909a]">
              正在挂载远端持久化接力工作区 (tmux/zellij: claude-ai)...
            </div>
          )}
        </div>
      ) : (
        <>
          <div
            ref={scrollRef}
            className="flex-1 p-3 overflow-y-auto space-y-3 select-text"
          >
            {turns.length === 0 && (
              <div className="h-full flex flex-col items-center justify-center text-center text-[#6b7280] space-y-2.5 px-6">
                <Bot size={26} className="text-[#4b5563]" />
                {providers.length === 0 ? (
                  <div className="space-y-2">
                    <div>尚未配置模型提供商。</div>
                    <button
                      onClick={onOpenAISettings}
                      className="px-3.5 py-1.5 rounded-full bg-[#2563eb] text-white font-medium"
                    >
                      前往「设置 -&gt; AI 助理」添加提供商
                    </button>
                  </div>
                ) : (
                  <div>
                    直接询问当前主机（自动携带最近 {settings.ai.context_lines} 行脱敏上下文），或点右上角切至{" "}
                    <span className="text-[#d1d5db]">远端 Claude CLI</span>。
                  </div>
                )}
              </div>
            )}

            {turns.map((turn) =>
              turn.role === "user" ? (
                <div key={turn.id} className="flex justify-end">
                  <div className="max-w-[85%] px-3 py-1.5 rounded-2xl bg-[#1e3a5f] text-[#e5e7eb] leading-relaxed">
                    {turn.text}
                  </div>
                </div>
              ) : (
                <div key={turn.id} className="space-y-2">
                  {(turn.steps || []).map((st) => {
                    const open = !!expandedSteps[st.call_id];
                    return (
                      <div
                        key={st.call_id}
                        className="rounded-lg bg-[#22242b] border border-[#2e313a] overflow-hidden"
                      >
                        <div
                          onClick={() =>
                            setExpandedSteps((p) => ({
                              ...p,
                              [st.call_id]: !open,
                            }))
                          }
                          className="px-3 py-2 flex items-center justify-between cursor-pointer hover:bg-[#272a33]"
                        >
                          <div className="flex items-center gap-2 min-w-0">
                            {open ? (
                              <ChevronDown size={13} className="text-[#8b909a]" />
                            ) : (
                              <ChevronRight size={13} className="text-[#8b909a]" />
                            )}
                            <Wrench size={12} className="text-[#9ca3af]" />
                            <span className="font-medium text-[#d1d5db]">
                              运行命令
                            </span>
                            <code className="text-[11px] font-mono text-[#9ca3af] truncate max-w-[155px]">
                              {st.command}
                            </code>
                          </div>
                          <div className="flex items-center gap-2 text-[11px] shrink-0">
                            <span className="text-[#8b909a]">
                              {st.approval_note || "已批准 · 审核模型"}
                            </span>
                            <span
                              className={
                                st.status === "executed"
                                  ? "text-[#3fb950]"
                                  : st.status === "pending_approval"
                                  ? "text-[#f59e0b]"
                                  : "text-[#f87171]"
                              }
                            >
                              {st.status === "executed"
                                ? "执行成功"
                                : st.status === "pending_approval"
                                ? "等待确认"
                                : "执行失败"}
                            </span>
                          </div>
                        </div>

                        {open && (
                          <div className="px-3 py-2 bg-[#15161a] border-t border-[#2b2e37] font-mono text-[11px] space-y-1">
                            <div className="text-[#60a5fa]">$ {st.command}</div>
                            {st.stdout && (
                              <pre className="whitespace-pre-wrap text-[#d1d5db] max-h-40 overflow-y-auto">
                                {st.stdout}
                              </pre>
                            )}
                            {st.stderr && (
                              <pre className="whitespace-pre-wrap text-[#f87171]">
                                {st.stderr}
                              </pre>
                            )}
                          </div>
                        )}
                      </div>
                    );
                  })}

                  {turn.pending && (
                    <div className="p-3 rounded-lg bg-[#23201b] border border-[#f59e0b]/60 space-y-2">
                      <div className="text-[#fbbf24] font-medium">
                        {turn.pending.review_reason
                          ? `审核提示（${turn.pending.review_reason}）：是否允许执行？`
                          : "AI 请求在远端主机执行以下命令："}
                      </div>
                      <pre className="p-2 rounded bg-[#141519] font-mono text-[11px] text-[#e5e7eb] overflow-x-auto">
                        {turn.pending.command}
                      </pre>
                      <div className="flex items-center gap-2 justify-end">
                        <button
                          onClick={() =>
                            handleDecision(turn.id, turn.pending!, false)
                          }
                          className="px-2.5 py-1 rounded border border-[#3a3d47] text-[#9ca3af] hover:text-white"
                        >
                          拒绝
                        </button>
                        <button
                          onClick={() =>
                            handleDecision(turn.id, turn.pending!, true)
                          }
                          className="px-3 py-1 rounded bg-[#2563eb] hover:bg-[#3b82f6] text-white font-medium"
                        >
                          批准并运行
                        </button>
                      </div>
                    </div>
                  )}

                  {turn.text && <div>{renderSimpleMarkdown(turn.text)}</div>}
                </div>
              )
            )}

            {busy && (
              <div className="text-[#8b909a] animate-pulse">
                正在审核并交互执行中...
              </div>
            )}
          </div>

          <div className="p-2.5 border-t border-[#272930] bg-[#1a1b1f]">
            <div className="rounded-xl bg-[#22242b] border border-[#30333d] p-2.5 space-y-2">
              <textarea
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.shiftKey) {
                    e.preventDefault();
                    handleSend();
                  }
                }}
                rows={2}
                placeholder="询问这台主机"
                className="w-full bg-transparent resize-none outline-none text-[#e5e7eb] placeholder:text-[#6b7280]"
              />

              <div className="flex items-center justify-between pt-1">
                <button
                  onClick={cyclePolicy}
                  className={`px-2 py-0.5 rounded-md border text-[11px] flex items-center gap-1 ${
                    policy === "auto_review"
                      ? "bg-[#f59e0b]/15 border-[#f59e0b]/40 text-[#fbbf24]"
                      : policy === "allow_all"
                      ? "bg-[#ef4444]/15 border-[#ef4444]/40 text-[#f87171]"
                      : "bg-[#2a2d36] border-[#3a3e4a] text-[#d1d5db]"
                  }`}
                  title="点击切换：自动审核 -> 每次询问 -> 完全允许"
                >
                  {policy === "auto_review" ? (
                    <ShieldAlert size={11} />
                  ) : policy === "ask" ? (
                    <ShieldCheck size={11} />
                  ) : (
                    <Sparkles size={11} />
                  )}
                  <span>{policyLabel}</span>
                </button>

                <div className="flex items-center gap-2">
                  {providers.length > 0 ? (
                    <select
                      value={selectedProvId}
                      onChange={(e) => setSelectedProvId(e.target.value)}
                      className="px-2 py-0.5 rounded bg-[#2a2d36] text-[#d1d5db] font-mono text-[11px] outline-none max-w-[160px]"
                    >
                      {providers.map((p) => (
                        <option key={p.id} value={p.id}>
                          {p.model}
                        </option>
                      ))}
                    </select>
                  ) : (
                    <button
                      onClick={onOpenAISettings}
                      className="px-2 py-0.5 rounded bg-[#2a2d36] text-[#60a5fa] text-[11px]"
                    >
                      + 添加提供商
                    </button>
                  )}

                  <button
                    onClick={handleSend}
                    disabled={busy || !input.trim()}
                    className="p-1.5 rounded-lg bg-[#2563eb] hover:bg-[#3b82f6] disabled:opacity-40 text-white"
                  >
                    <Send size={12} />
                  </button>
                </div>
              </div>
            </div>
          </div>
        </>
      )}
    </div>
  );
};
