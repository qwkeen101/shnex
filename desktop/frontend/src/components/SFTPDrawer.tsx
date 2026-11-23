import React, { useEffect, useRef, useState } from "react";
import {
  ArrowUp,
  Download,
  File,
  Folder,
  FolderPlus,
  RefreshCw,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import {
  ApiError,
  FileEntry,
  formatBytes,
  openNativeFileDialog,
  sftpList,
  sftpMkdir,
  sftpRead,
  sftpRemove,
  sftpSetSudoPassword,
  sftpUploadFile,
  sftpUploadLocalPaths,
} from "../api";

interface Props {
  sessionId: string;
  initialPath?: string;
  showHidden?: boolean;
  onPathChange?: (path: string) => void;
  onOpenMarkdown?: (entry: FileEntry) => void;
  onClose: () => void;
}

const isMarkdownFilename = (name: string) =>
  /\.(md|markdown|mdown|mdx)$/i.test(name);

export const SFTPDrawer: React.FC<Props> = ({
  sessionId,
  initialPath = "",
  showHidden = true,
  onPathChange,
  onOpenMarkdown,
  onClose,
}) => {
  const [curPath, setCurPath] = useState("");
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [errMsg, setErrMsg] = useState<string | null>(null);
  const [needSudoPw, setNeedSudoPw] = useState(false);
  const [sudoPwInput, setSudoPwInput] = useState("");
  const [dragOver, setDragOver] = useState(false);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const handleSftpErr = (e: unknown) => {
    if (e instanceof ApiError && e.code === "SUDO_PASSWORD_NEEDED") {
      setNeedSudoPw(true);
      setErrMsg(null);
      return;
    }
    setErrMsg((e as Error).message);
  };

  const loadDir = async (targetPath: string) => {
    setLoading(true);
    setErrMsg(null);
    try {
      const res = await sftpList(sessionId, targetPath);
      setNeedSudoPw(false);
      setCurPath(res.path);
      setEntries(res.entries || []);
      onPathChange?.(res.path);
    } catch (e) {
      handleSftpErr(e);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadDir(initialPath === "/" ? "" : initialPath);
  }, [sessionId]);

  const goParent = () => {
    if (!curPath || curPath === "/") return;
    const idx = curPath.lastIndexOf("/");
    const parent = idx <= 0 ? "/" : curPath.slice(0, idx);
    loadDir(parent);
  };

  const uploadBrowserFiles = async (files: FileList | File[]) => {
    const arr = Array.from(files);
    if (arr.length === 0) return;
    setLoading(true);
    setErrMsg(null);
    try {
      for (const file of arr) {
        const baseDir = curPath || ".";
        const dest = baseDir.endsWith("/")
          ? `${baseDir}${file.name}`
          : `${baseDir}/${file.name}`;
        await sftpUploadFile(sessionId, dest, file);
      }
      await loadDir(curPath);
    } catch (e) {
      handleSftpErr(e);
    } finally {
      setLoading(false);
    }
  };

  const handleUploadButtonClick = async () => {
    if (loading) return;
    const dlg = await openNativeFileDialog("any", true);
    if (!dlg.supported) {
      fileInputRef.current?.click();
      return;
    }
    if (dlg.canceled || dlg.paths.length === 0) return;
    setLoading(true);
    setErrMsg(null);
    try {
      await sftpUploadLocalPaths(sessionId, curPath || ".", dlg.paths);
      await loadDir(curPath);
    } catch (e) {
      handleSftpErr(e);
    } finally {
      setLoading(false);
    }
  };

  const handleUpload = async (ev: React.ChangeEvent<HTMLInputElement>) => {
    const files = ev.target.files;
    if (files && files.length > 0) {
      await uploadBrowserFiles(files);
    }
    ev.target.value = "";
  };

  const handleDownload = async (entry: FileEntry) => {
    try {
      const res = await sftpRead(sessionId, entry.path);
      const bin = atob(res.content_base64);
      const bytes = new Uint8Array(bin.length);
      for (let i = 0; i < bin.length; i++) {
        bytes[i] = bin.charCodeAt(i);
      }
      const blob = new Blob([bytes]);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = entry.name;
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      handleSftpErr(e);
    }
  };

  const handleNewFolder = async () => {
    const name = window.prompt("新建远端文件夹名称:");
    if (!name?.trim()) return;
    const dest = curPath.endsWith("/")
      ? `${curPath}${name.trim()}`
      : `${curPath}/${name.trim()}`;
    try {
      await sftpMkdir(sessionId, dest);
      await loadDir(curPath);
    } catch (e) {
      handleSftpErr(e);
    }
  };

  const handleDelete = async (entry: FileEntry) => {
    if (!window.confirm(`确认删除远端路径 ${entry.path} ?`)) return;
    try {
      await sftpRemove(sessionId, entry.path);
      await loadDir(curPath);
    } catch (e) {
      handleSftpErr(e);
    }
  };

  return (
    <div
      onDragOver={(e) => {
        e.preventDefault();
        e.stopPropagation();
        if (!dragOver) setDragOver(true);
      }}
      onDragLeave={(e) => {
        e.preventDefault();
        if (e.currentTarget === e.target) setDragOver(false);
      }}
      onDrop={(e) => {
        e.preventDefault();
        e.stopPropagation();
        setDragOver(false);
        if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
          uploadBrowserFiles(e.dataTransfer.files);
        }
      }}
      className={`w-[310px] h-full bg-[#1b1c20] border-l flex flex-col text-xs shrink-0 relative transition-colors ${
        dragOver ? "border-[#3b82f6] ring-1 ring-[#3b82f6]/60" : "border-[#282a32]"
      }`}
    >
      <div className="h-9 px-3 border-b border-[#282a32] flex items-center justify-between shrink-0">
        <span className="font-semibold text-[#f3f4f6]">SFTP 文件管理</span>
        <div className="flex items-center gap-1 text-[#8b909a]">
          <button
            onClick={handleUploadButtonClick}
            className="p-1 rounded hover:bg-[#2a2d36] hover:text-[#e5e7eb]"
            title="上传图片或任意文件 (支持拖拽多文件到此面板)"
          >
            <Upload size={13} />
          </button>
          <button
            onClick={handleNewFolder}
            className="p-1 rounded hover:bg-[#2a2d36] hover:text-[#e5e7eb]"
            title="新建文件夹"
          >
            <FolderPlus size={13} />
          </button>
          <button
            onClick={() => loadDir(curPath)}
            className="p-1 rounded hover:bg-[#2a2d36] hover:text-[#e5e7eb]"
            title="刷新"
          >
            <RefreshCw size={13} className={loading ? "animate-spin" : ""} />
          </button>
          <button
            onClick={onClose}
            className="p-1 rounded hover:bg-[#2a2d36] hover:text-[#e5e7eb]"
          >
            <X size={14} />
          </button>
        </div>
      </div>

      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={handleUpload}
      />

      {/* Path Bar */}
      <div className="px-2.5 py-1.5 border-b border-[#282a32] flex items-center gap-1.5">
        <button
          onClick={goParent}
          className="p-1 rounded bg-[#23252c] border border-[#2f323c] hover:text-white text-[#9ca3af]"
          title="上级目录"
        >
          <ArrowUp size={12} />
        </button>
        <input
          value={curPath}
          onChange={(e) => setCurPath(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") loadDir(curPath);
          }}
          className="flex-1 px-2 py-1 rounded bg-[#141519] border border-[#2d3039] font-mono text-[11px] outline-none"
        />
      </div>

      {needSudoPw && (
        <form
          onSubmit={async (e) => {
            e.preventDefault();
            if (!sudoPwInput) return;
            try {
              setLoading(true);
              await sftpSetSudoPassword(sessionId, sudoPwInput);
              setSudoPwInput("");
              setNeedSudoPw(false);
              await loadDir(curPath);
            } catch (ex) {
              handleSftpErr(ex);
            } finally {
              setLoading(false);
            }
          }}
          className="p-3 bg-[#242119] border-b border-[#f59e0b]/40 space-y-2"
        >
          <div className="text-[#fbbf24] font-medium">
            需要 sudo 密码以提权打开文件
          </div>
          <div className="text-[11px] text-[#9ca3af]">
            仅在本次运行中保存在内存，关闭会话即自动清除。
          </div>
          <div className="flex gap-1.5">
            <input
              type="password"
              autoFocus
              required
              value={sudoPwInput}
              onChange={(e) => setSudoPwInput(e.target.value)}
              placeholder="输入 sudo 密码..."
              className="flex-1 px-2 py-1 rounded bg-[#141519] border border-[#3a3628] text-white outline-none"
            />
            <button
              type="submit"
              className="px-2.5 py-1 rounded bg-[#2563eb] text-white font-medium"
            >
              确定
            </button>
          </div>
        </form>
      )}

      {errMsg && (
        <div className="px-3 py-1.5 bg-[#ef4444]/15 text-[#f87171] border-b border-[#ef4444]/30">
          {errMsg}
        </div>
      )}

      <div className="flex-1 overflow-y-auto divide-y divide-[#24262d]">
        {entries
          .filter((it) => showHidden || !it.name.startsWith("."))
          .map((item) => (
          <div
            key={item.path}
            className="px-3 py-2 flex items-center justify-between gap-2 hover:bg-[#24262e] group"
          >
            <div
              onClick={() => {
                if (item.is_dir) {
                  loadDir(item.path);
                } else if (isMarkdownFilename(item.name) && onOpenMarkdown) {
                  onOpenMarkdown(item);
                }
              }}
              className={`flex items-center gap-2 min-w-0 flex-1 ${
                item.is_dir || isMarkdownFilename(item.name) ? "cursor-pointer" : ""
              }`}
            >
              {item.is_dir ? (
                <Folder size={14} className="text-[#60a5fa] shrink-0" />
              ) : isMarkdownFilename(item.name) ? (
                <span className="px-1 py-0.5 rounded bg-[#2563eb]/20 border border-[#3b82f6]/40 text-[#93c5fd] font-mono font-bold text-[9px] leading-none shrink-0">
                  M↓
                </span>
              ) : (
                <File size={14} className="text-[#9ca3af] shrink-0" />
              )}
              <div className="min-w-0">
                <div className="truncate text-[#e5e7eb]">{item.name}</div>
                <div className="text-[10px] text-[#6b7280] font-mono">
                  {item.is_dir ? item.mode : `${formatBytes(item.size)}  ${item.mode}`}
                </div>
              </div>
            </div>

            <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity shrink-0">
              {!item.is_dir && isMarkdownFilename(item.name) && onOpenMarkdown && (
                <button
                  onClick={() => onOpenMarkdown(item)}
                  className="px-1.5 py-0.5 rounded bg-[#2563eb]/25 hover:bg-[#2563eb] text-[#93c5fd] hover:text-white font-mono font-bold text-[10px]"
                  title="在新标签页打开 Markdown 原生预览"
                >
                  M↓
                </button>
              )}
              {!item.is_dir && (
                <button
                  onClick={() => handleDownload(item)}
                  className="p-1 rounded hover:bg-[#2f333e] text-[#9ca3af] hover:text-white"
                  title="下载"
                >
                  <Download size={12} />
                </button>
              )}
              <button
                onClick={() => handleDelete(item)}
                className="p-1 rounded hover:bg-[#ef4444]/20 text-[#9ca3af] hover:text-[#f87171]"
                title="删除"
              >
                <Trash2 size={12} />
              </button>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
};
