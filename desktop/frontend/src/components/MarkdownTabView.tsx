import React, { useEffect, useRef, useState } from "react";
import {
  BookOpen,
  Check,
  Columns2,
  Copy,
  FolderOpen,
  ListTree,
  Moon,
  RefreshCw,
  Save,
  Sparkles,
  Sun,
  X,
} from "lucide-react";
import {
  CompiledMarkdownDocument,
  MarkdownBlock,
  MarkdownInline,
  buildMarkdownAssetUrl,
  renderMarkdownNative,
  sftpUploadFile,
  writeLocalMarkdownFile,
} from "../api";

export interface MarkdownDocSource {
  kind: "sample" | "local" | "sftp";
  localPath?: string;
  sftpSessionId?: string;
  sftpPath?: string;
}

interface Props {
  title: string;
  initialContent: string;
  initialDoc: CompiledMarkdownDocument;
  source: MarkdownDocSource;
  onReloadRemote?: () => Promise<{ content: string; doc: CompiledMarkdownDocument }>;
  onOpenLocalPicker: () => void;
  onOpenMathDemo: () => void;
  onClose: () => void;
}

export const MarkdownTabView: React.FC<Props> = ({
  title,
  initialContent,
  initialDoc,
  source,
  onReloadRemote,
  onOpenLocalPicker,
  onOpenMathDemo,
  onClose,
}) => {
  const [content, setContent] = useState(initialContent);
  const [doc, setDoc] = useState<CompiledMarkdownDocument>(initialDoc);
  const [paperMode, setPaperMode] = useState<"light" | "dark">("light");
  const [viewMode, setViewMode] = useState<"read" | "split">("read");
  const [showOutline, setShowOutline] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveDone, setSaveDone] = useState(false);
  const [copiedIdx, setCopiedIdx] = useState<number | null>(null);

  const scrollRef = useRef<HTMLDivElement | null>(null);
  const renderTimerRef = useRef<number | null>(null);

  useEffect(() => {
    setContent(initialContent);
    setDoc(initialDoc);
    setDirty(false);
  }, [initialContent, initialDoc]);

  const handleEditorChange = (next: string) => {
    setContent(next);
    setDirty(true);
    setSaveDone(false);
    if (renderTimerRef.current) {
      window.clearTimeout(renderTimerRef.current);
    }
    renderTimerRef.current = window.setTimeout(async () => {
      try {
        const compiled = await renderMarkdownNative(next);
        setDoc(compiled);
      } catch {
        // Ignore transient parse error
      }
    }, 100);
  };

  const canSave =
    (source.kind === "local" && Boolean(source.localPath)) ||
    (source.kind === "sftp" &&
      Boolean(source.sftpSessionId) &&
      Boolean(source.sftpPath));

  const handleSave = async () => {
    if (!canSave || saving) return;
    setSaving(true);
    try {
      if (source.kind === "local" && source.localPath) {
        const res = await writeLocalMarkdownFile(source.localPath, content);
        setDoc(res.document);
      } else if (
        source.kind === "sftp" &&
        source.sftpSessionId &&
        source.sftpPath
      ) {
        const blob = new Blob([content], { type: "text/markdown;charset=utf-8" });
        await sftpUploadFile(source.sftpSessionId, source.sftpPath, blob);
        setDoc(await renderMarkdownNative(content));
      }
      setDirty(false);
      setSaveDone(true);
      window.setTimeout(() => setSaveDone(false), 2200);
    } catch (e) {
      alert("保存 Markdown 失败: " + (e as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const isLight = paperMode === "light";

  const renderInlines = (inlines?: MarkdownInline[]): React.ReactNode => {
    if (!inlines || inlines.length === 0) return null;
    return inlines.map((inl, idx) => {
      switch (inl.kind) {
        case "text":
          return <React.Fragment key={idx}>{inl.text}</React.Fragment>;
        case "bold":
          return (
            <strong key={idx} className="font-semibold">
              {renderInlines(inl.children)}
            </strong>
          );
        case "italic":
          return (
            <em key={idx} className="italic">
              {renderInlines(inl.children)}
            </em>
          );
        case "strike":
          return (
            <del key={idx} className="line-through opacity-75">
              {renderInlines(inl.children)}
            </del>
          );
        case "code":
          return (
            <code
              key={idx}
              className={`px-1.5 py-0.5 rounded font-mono text-[0.88em] ${
                isLight
                  ? "bg-[#f1f5f9] text-[#0f172a] border border-[#e2e8f0]"
                  : "bg-[#252834] text-[#93c5fd] border border-[#323646]"
              }`}
            >
              {inl.text}
            </code>
          );
        case "link":
          return (
            <a
              key={idx}
              href={inl.url}
              target="_blank"
              rel="noreferrer"
              className={
                isLight
                  ? "text-[#2563eb] hover:underline font-medium"
                  : "text-[#60a5fa] hover:underline font-medium"
              }
            >
              {renderInlines(inl.children)}
            </a>
          );
        case "image":
          return (
            <img
              key={idx}
              src={buildMarkdownAssetUrl(inl.url || "", {
                localBase: source.localPath,
                sftpSessionId: source.sftpSessionId,
                sftpBase: source.sftpPath,
              })}
              alt={inl.alt || ""}
              className="max-w-full rounded-lg my-3 border border-black/10"
            />
          );
        case "math_inline":
          if (!inl.math) return null;
          return (
            <span
              key={idx}
              title={inl.math.tex}
              className="inline-block mx-0.5 select-none"
              dangerouslySetInnerHTML={{ __html: inl.math.svg }}
            />
          );
        default:
          return null;
      }
    });
  };

  const renderBlock = (b: MarkdownBlock, idx: number): React.ReactNode => {
    switch (b.kind) {
      case "heading": {
        const lvl = b.level || 2;
        if (lvl === 1) {
          return (
            <h1
              key={idx}
              id={b.id}
              className={`text-2xl font-bold tracking-tight pb-3 mb-5 border-b ${
                isLight
                  ? "text-[#111827] border-[#e5e7eb]"
                  : "text-white border-[#2c303d]"
              }`}
            >
              {renderInlines(b.inlines)}
            </h1>
          );
        }
        if (lvl === 2) {
          return (
            <h2
              key={idx}
              id={b.id}
              className={`text-[19px] font-bold tracking-tight mt-8 pb-2 mb-4 border-b ${
                isLight
                  ? "text-[#111827] border-[#eceff3]"
                  : "text-[#f3f4f6] border-[#2a2d38]"
              }`}
            >
              {renderInlines(b.inlines)}
            </h2>
          );
        }
        return (
          <h3
            key={idx}
            id={b.id}
            className={`text-base font-semibold mt-6 mb-2.5 ${
              isLight ? "text-[#1f2937]" : "text-[#e5e7eb]"
            }`}
          >
            {renderInlines(b.inlines)}
          </h3>
        );
      }

      case "paragraph":
        return (
          <p
            key={idx}
            className={`text-[15px] leading-[1.8] my-3.5 ${
              isLight ? "text-[#1f2937]" : "text-[#d1d5db]"
            }`}
          >
            {renderInlines(b.inlines)}
          </p>
        );

      case "math_block":
        if (!b.math) return null;
        return (
          <div
            key={idx}
            title={b.math.tex}
            className={`my-5 py-2.5 px-4 flex justify-center overflow-x-auto select-none ${
              isLight ? "text-[#111827]" : "text-[#f3f4f6]"
            }`}
            dangerouslySetInnerHTML={{ __html: b.math.svg }}
          />
        );

      case "code_block":
        return (
          <div
            key={idx}
            className={`my-4 rounded-xl overflow-hidden border text-xs ${
              isLight
                ? "bg-[#f8fafc] border-[#e2e8f0] text-[#0f172a]"
                : "bg-[#13151b] border-[#2b2f3c] text-[#e5e7eb]"
            }`}
          >
            <div
              className={`px-3.5 py-1.5 flex items-center justify-between border-b font-mono text-[11px] ${
                isLight
                  ? "bg-[#f1f5f9] border-[#e2e8f0] text-[#64748b]"
                  : "bg-[#1b1e26] border-[#2b2f3c] text-[#8b909a]"
              }`}
            >
              <span>{b.lang || "text"}</span>
              <button
                onClick={() => {
                  navigator.clipboard.writeText(b.code || "").catch(() => {});
                  setCopiedIdx(idx);
                  window.setTimeout(() => setCopiedIdx(null), 1500);
                }}
                className="flex items-center gap-1 hover:text-[#2563eb]"
              >
                {copiedIdx === idx ? <Check size={12} /> : <Copy size={12} />}
                <span>{copiedIdx === idx ? "已复制" : "复制"}</span>
              </button>
            </div>
            <pre className="p-3.5 overflow-x-auto font-mono leading-relaxed">
              <code>{b.code}</code>
            </pre>
          </div>
        );

      case "blockquote": {
        const alertStyles: Record<
          string,
          { label: string; light: string; dark: string }
        > = {
          NOTE: {
            label: "说明",
            light: "bg-[#eff6ff] border-[#3b82f6] text-[#1e3a8a]",
            dark: "bg-[#1e3a8a]/20 border-[#3b82f6] text-[#bfdbfe]",
          },
          TIP: {
            label: "提示",
            light: "bg-[#f0fdf4] border-[#22c55e] text-[#14532d]",
            dark: "bg-[#14532d]/25 border-[#22c55e] text-[#bbf7d0]",
          },
          IMPORTANT: {
            label: "重要",
            light: "bg-[#faf5ff] border-[#a855f7] text-[#581c87]",
            dark: "bg-[#581c87]/25 border-[#a855f7] text-[#e9d5ff]",
          },
          WARNING: {
            label: "注意",
            light: "bg-[#fffbeb] border-[#f59e0b] text-[#78350f]",
            dark: "bg-[#78350f]/25 border-[#f59e0b] text-[#fde68a]",
          },
          CAUTION: {
            label: "警告",
            light: "bg-[#fef2f2] border-[#ef4444] text-[#7f1d1d]",
            dark: "bg-[#7f1d1d]/25 border-[#ef4444] text-[#fecaca]",
          },
        };
        const cfg = b.alert_type ? alertStyles[b.alert_type] : undefined;
        if (cfg) {
          return (
            <div
              key={idx}
              className={`my-4 px-4 py-3 rounded-r-xl border-l-4 text-[14px] leading-relaxed ${
                isLight ? cfg.light : cfg.dark
              }`}
            >
              <div className="font-semibold text-xs uppercase tracking-wider mb-1">
                {cfg.label}
              </div>
              <div>{renderInlines(b.inlines)}</div>
            </div>
          );
        }
        return (
          <blockquote
            key={idx}
            className={`my-4 pl-4 py-1 border-l-4 italic text-[14.5px] ${
              isLight
                ? "border-[#cbd5e1] text-[#475569]"
                : "border-[#4b5563] text-[#9ca3af]"
            }`}
          >
            {renderInlines(b.inlines)}
          </blockquote>
        );
      }

      case "list": {
        const ListTag = b.ordered ? "ol" : "ul";
        return (
          <ListTag
            key={idx}
            className={`my-3.5 pl-6 space-y-1.5 text-[15px] leading-relaxed ${
              b.ordered ? "list-decimal" : "list-disc"
            } ${isLight ? "text-[#1f2937]" : "text-[#d1d5db]"}`}
          >
            {(b.items || []).map((it, liIdx) => (
              <li
                key={liIdx}
                className={it.checked !== undefined ? "list-none -ml-5 flex items-start gap-2" : ""}
              >
                {it.checked !== undefined && (
                  <input
                    type="checkbox"
                    checked={it.checked}
                    readOnly
                    className="mt-1.5 rounded"
                  />
                )}
                <span>{renderInlines(it.inlines)}</span>
              </li>
            ))}
          </ListTag>
        );
      }

      case "table":
        return (
          <div key={idx} className="my-5 overflow-x-auto">
            <table
              className={`w-full border-collapse text-[14px] ${
                isLight ? "text-[#1f2937]" : "text-[#e5e7eb]"
              }`}
            >
              <thead>
                <tr
                  className={
                    isLight
                      ? "bg-[#f8fafc] border-b-2 border-[#e2e8f0]"
                      : "bg-[#222530] border-b-2 border-[#323646]"
                  }
                >
                  {(b.headers || []).map((h, ci) => (
                    <th
                      key={ci}
                      style={{
                        textAlign: (b.aligns?.[ci] as "left" | "center" | "right") || "left",
                      }}
                      className="px-3.5 py-2 font-semibold"
                    >
                      {renderInlines(h)}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {(b.rows || []).map((r, ri) => (
                  <tr
                    key={ri}
                    className={
                      isLight
                        ? "border-b border-[#eef2f6]"
                        : "border-b border-[#282c38]"
                    }
                  >
                    {r.map((cell, ci) => (
                      <td
                        key={ci}
                        style={{
                          textAlign: (b.aligns?.[ci] as "left" | "center" | "right") || "left",
                        }}
                        className="px-3.5 py-2"
                      >
                        {renderInlines(cell)}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        );

      case "hr":
        return (
          <hr
            key={idx}
            className={`my-6 ${
              isLight ? "border-[#e5e7eb]" : "border-[#2c303d]"
            }`}
          />
        );

      default:
        return null;
    }
  };

  const sourceSubtitle =
    source.kind === "local"
      ? source.localPath || "本机文件"
      : source.kind === "sftp"
      ? `SSH 远端: ${source.sftpPath || ""}`
      : "内置原生排版示例 (core/texmath)";

  return (
    <div className="flex-1 flex flex-col rounded-lg overflow-hidden bg-[#16181e] border border-[#2a2d37] min-w-0 min-h-0">
      {/* Top Markdown Tab Toolbar */}
      <div className="h-8 px-3 bg-[#1c1e25] border-b border-[#282b36] flex items-center justify-between gap-2 text-xs shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <span className="px-1.5 py-0.5 rounded bg-[#111827] border border-[#3b4050] font-mono font-bold text-[10px] text-white tracking-tight shrink-0">
            M↓
          </span>
          <span className="font-semibold text-[#f3f4f6] truncate">{title}</span>
          {dirty && (
            <span className="text-[10px] px-1.5 py-0.5 rounded bg-[#f59e0b]/20 text-[#fbbf24]">
              已修改
            </span>
          )}
          <span className="text-[11px] text-[#7c828e] font-mono truncate hidden sm:inline">
            {sourceSubtitle}
          </span>
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
          <button
            onClick={onOpenLocalPicker}
            className="px-2 py-1 rounded hover:bg-[#2a2d38] text-[#cbd5e1] flex items-center gap-1"
            title="打开本机 .md 文档"
          >
            <FolderOpen size={12} />
            <span>打开本地</span>
          </button>

          {source.kind !== "sample" && (
            <button
              onClick={onOpenMathDemo}
              className="px-2 py-1 rounded hover:bg-[#2a2d38] text-[#93c5fd] flex items-center gap-1"
              title="打开内置数学公式排版演示"
            >
              <Sparkles size={12} />
              <span>公式示例</span>
            </button>
          )}

          {source.kind === "sftp" && onReloadRemote && (
            <button
              onClick={async () => {
                const fresh = await onReloadRemote();
                setContent(fresh.content);
                setDoc(fresh.doc);
                setDirty(false);
              }}
              className="p-1 rounded hover:bg-[#2a2d38] text-[#cbd5e1]"
              title="从远端服务器重新读取"
            >
              <RefreshCw size={12} />
            </button>
          )}

          {canSave && (
            <button
              onClick={handleSave}
              disabled={!dirty || saving}
              className={`px-2.5 py-1 rounded flex items-center gap-1 font-medium transition ${
                saveDone
                  ? "bg-[#16a34a] text-white"
                  : dirty
                  ? "bg-[#2563eb] hover:bg-[#3b82f6] text-white"
                  : "bg-[#252833] text-[#6b7280] cursor-not-allowed"
              }`}
            >
              {saveDone ? <Check size={12} /> : <Save size={12} />}
              <span>{saveDone ? "已保存" : "保存"}</span>
            </button>
          )}

          <span className="w-[1px] h-3.5 bg-[#2f3341] mx-0.5" />

          <button
            onClick={() =>
              setViewMode((m) => (m === "read" ? "split" : "read"))
            }
            className={`px-2 py-1 rounded flex items-center gap-1 ${
              viewMode === "split"
                ? "bg-[#2563eb]/25 text-[#93c5fd]"
                : "hover:bg-[#2a2d38] text-[#cbd5e1]"
            }`}
            title="切换纯阅读或实时源码对照编辑"
          >
            {viewMode === "split" ? (
              <BookOpen size={12} />
            ) : (
              <Columns2 size={12} />
            )}
            <span>{viewMode === "split" ? "纯阅读" : "源码对照"}</span>
          </button>

          <button
            onClick={() => setShowOutline((v) => !v)}
            className={`p-1 rounded ${
              showOutline
                ? "bg-[#2563eb]/25 text-[#93c5fd]"
                : "hover:bg-[#2a2d38] text-[#cbd5e1]"
            }`}
            title="目录大纲"
          >
            <ListTree size={13} />
          </button>

          <button
            onClick={() =>
              setPaperMode((p) => (p === "light" ? "dark" : "light"))
            }
            className="p-1 rounded hover:bg-[#2a2d38] text-[#cbd5e1]"
            title={isLight ? "切换深色护眼纸张" : "切换亮白学术纸张"}
          >
            {isLight ? <Moon size={13} /> : <Sun size={13} />}
          </button>

          <button
            onClick={onClose}
            className="p-1 rounded hover:bg-[#ef4444]/20 text-[#9ca3af] hover:text-[#f87171]"
            title="关闭文档标签"
          >
            <X size={13} />
          </button>
        </div>
      </div>

      {/* Main Split Body */}
      <div className="flex-1 flex min-h-0 overflow-hidden">
        {/* Optional Live Markdown + TeX Source Editor */}
        {viewMode === "split" && (
          <div className="w-1/2 h-full border-r border-[#2a2d37] flex flex-col bg-[#12141a]">
            <div className="px-3 py-1.5 border-b border-[#232632] text-[11px] text-[#7c828e] flex items-center justify-between">
              <span>Markdown / TeX 源码（修改后由 Go 核心引擎实时排版）</span>
              <span className="font-mono">{content.length} 字符</span>
            </div>
            <textarea
              value={content}
              onChange={(e) => handleEditorChange(e.target.value)}
              spellCheck={false}
              className="flex-1 w-full p-4 bg-transparent text-[#e5e7eb] font-mono text-[12.5px] leading-relaxed resize-none outline-none"
            />
          </div>
        )}

        {/* Native Document Canvas (Matches Screenshot 1 Clean Paper Page!) */}
        <div
          ref={scrollRef}
          className={`flex-1 h-full overflow-y-auto transition-colors ${
            isLight ? "bg-[#f3f4f6] p-5 sm:p-7" : "bg-[#13151b] p-5 sm:p-7"
          }`}
        >
          <article
            className={`max-w-[840px] mx-auto rounded-2xl px-8 sm:px-12 py-9 transition-colors ${
              isLight
                ? "bg-white text-[#111827] shadow-sm border border-[#e5e7eb]"
                : "bg-[#1a1d26] text-[#e5e7eb] border border-[#2b2f3d]"
            }`}
          >
            {doc.blocks && doc.blocks.length > 0 ? (
              doc.blocks.map((blk, i) => renderBlock(blk, i))
            ) : (
              <div className="text-sm text-[#9ca3af] py-12 text-center">
                文档内容为空
              </div>
            )}
          </article>
        </div>

        {/* Optional Collapsible Outline Sidebar */}
        {showOutline && doc.outline && doc.outline.length > 0 && (
          <aside className="w-56 h-full bg-[#191b22] border-l border-[#292c37] p-3 overflow-y-auto text-xs shrink-0">
            <div className="font-semibold text-[#e5e7eb] mb-2 px-1.5">
              文档目录
            </div>
            <div className="space-y-0.5">
              {doc.outline.map((h) => (
                <button
                  key={h.id}
                  onClick={() => {
                    const el = scrollRef.current?.querySelector(`#${h.id}`);
                    el?.scrollIntoView({ behavior: "smooth", block: "start" });
                  }}
                  style={{ paddingLeft: `${Math.max(6, (h.level - 1) * 12)}px` }}
                  className="w-full py-1.5 pr-2 rounded text-left text-[#b0b6c3] hover:bg-[#262936] hover:text-white truncate"
                >
                  {h.title}
                </button>
              ))}
            </div>
          </aside>
        )}
      </div>
    </div>
  );
};
