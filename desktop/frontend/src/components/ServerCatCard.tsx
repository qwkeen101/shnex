import React, { useState } from "react";
import { ArrowRight, Folder, MoreHorizontal } from "lucide-react";
import { SystemStats } from "../api";
import { HostBadge } from "./HostBadge";

interface Props {
  title: string;
  maskedUserTarget: string;
  currentDir: string;
  icon?: string;
  badgeText?: string;
  color?: string;
  stats: SystemStats | null;
  cpuHistory: number[];
  aiState: "running" | "approval" | "idle";
  blurPx?: number;
  onResume: () => void;
  onOpenSFTP: () => void;
  onEditHost?: () => void;
  onDisconnect: () => void;
}

function formatGiB(bytes?: number): string {
  if (!bytes || bytes <= 0) return "0.00";
  return (bytes / (1024 * 1024 * 1024)).toFixed(2);
}

function formatUptimeCN(sec?: number): string {
  if (!sec || sec <= 0) return "刚刚连接";
  const days = Math.floor(sec / 86400);
  const hours = Math.floor((sec % 86400) / 3600);
  const mins = Math.floor((sec % 3600) / 60);
  if (days > 0) {
    return `运行 ${days} 天 ${hours} 小时`;
  }
  if (hours > 0) {
    return `运行 ${hours} 小时 ${mins} 分`;
  }
  return `运行 ${Math.max(1, mins)} 分钟`;
}

const ServerCatIllustration: React.FC<{
  busy: boolean;
  approvalNeeded: boolean;
}> = ({ busy, approvalNeeded }) => {
  return (
    <svg
      width="106"
      height="74"
      viewBox="0 0 106 74"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className="shrink-0 select-none overflow-visible"
    >
      <style>
        {`
          @keyframes shnexCatBreath {
            0%, 100% { transform: scaleY(1); }
            50% { transform: scaleY(1.035); }
          }
          @keyframes shnexTailSway {
            0%, 100% { transform: rotate(0deg); }
            50% { transform: rotate(${busy ? "12deg" : "5deg"}); }
          }
          @keyframes shnexFloatZ1 {
            0% { opacity: 0; transform: translate(0px, 2px) scale(0.85); }
            45% { opacity: 0.9; }
            100% { opacity: 0; transform: translate(4px, -9px) scale(1.05); }
          }
          @keyframes shnexLedBlink {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.3; }
          }
        `}
      </style>

      {/* 2U Dark Server Chassis Stack */}
      <g>
        {/* Unit 1 (Top 1U Server) */}
        <rect
          x="18"
          y="43"
          width="74"
          height="12"
          rx="3"
          fill="#2b2e3b"
          stroke="#3b3f50"
          strokeWidth="1"
        />
        <circle
          cx="25"
          cy="49"
          r="1.7"
          fill="#4ade80"
          style={{
            animation: `shnexLedBlink ${busy ? "0.45s" : "2.1s"} infinite`,
          }}
        />
        <circle
          cx="31"
          cy="49"
          r="1.7"
          fill="#f472b6"
          style={{
            animation: `shnexLedBlink ${busy ? "0.6s" : "2.8s"} infinite 0.4s`,
          }}
        />
        <rect x="64" y="47" width="21" height="1.6" rx="0.8" fill="#1d1f28" />
        <rect x="64" y="50" width="21" height="1.6" rx="0.8" fill="#1d1f28" />

        {/* Unit 2 (Bottom 1U Server) */}
        <rect
          x="18"
          y="57"
          width="74"
          height="12"
          rx="3"
          fill="#252834"
          stroke="#363a4a"
          strokeWidth="1"
        />
        <circle cx="25" cy="63" r="1.7" fill="#4ade80" />
        <circle
          cx="31"
          cy="63"
          r="1.7"
          fill="#f472b6"
          style={{
            animation: `shnexLedBlink ${busy ? "0.5s" : "3.2s"} infinite 0.9s`,
          }}
        />
        <rect x="64" y="61" width="21" height="1.6" rx="0.8" fill="#191b23" />
        <rect x="64" y="64" width="21" height="1.6" rx="0.8" fill="#191b23" />
      </g>

      {/* Ginger Tabby Cat Lounging on Top of Server */}
      <g>
        {/* Hanging Swaying Tail */}
        <g
          style={{
            transformOrigin: "26px 38px",
            animation: `shnexTailSway ${busy ? "1.1s" : "3.2s"} ease-in-out infinite`,
          }}
        >
          <path
            d="M27 37 C14 37, 10 47, 16 55"
            stroke="#f2a65a"
            strokeWidth="5.5"
            strokeLinecap="round"
            fill="none"
          />
          <path
            d="M15.2 53.5 C16 55, 17 56, 17.8 56.5"
            stroke="#fde8d0"
            strokeWidth="5.2"
            strokeLinecap="round"
          />
        </g>

        {/* Breathing Cat Body */}
        <g
          style={{
            transformOrigin: "54px 43px",
            animation: "shnexCatBreath 3.4s ease-in-out infinite",
          }}
        >
          <path
            d="M25 43 C25 29, 43 27, 62 29 C71 30, 77 35, 77 43 Z"
            fill="#f4ac64"
          />
          {/* Tabby Stripes on Back */}
          <path
            d="M39 29.5 L37.5 35.5 M46 28.8 L45 35.2 M53 29.2 L52.5 35"
            stroke="#d87f37"
            strokeWidth="2.2"
            strokeLinecap="round"
          />
          {/* Soft Cream Front Paws */}
          <ellipse cx="63" cy="42.5" rx="5.5" ry="2.6" fill="#fde6ce" />
          <ellipse cx="76" cy="42.5" rx="4.8" ry="2.4" fill="#fde6ce" />
        </g>

        {/* Cat Head */}
        <g>
          {/* Left & Right Perked Ears */}
          <path d="M62 24 L64 13 L71 21 Z" fill="#f4ac64" />
          <path d="M64 22 L65.2 15.6 L69.5 20.6 Z" fill="#f28ca8" />
          <path d="M75 21 L82 13 L84 24 Z" fill="#f4ac64" />
          <path d="M76.5 20.6 L80.8 15.6 L82 22 Z" fill="#f28ca8" />

          {/* Round Head */}
          <ellipse cx="73" cy="29" rx="12" ry="10.5" fill="#f5b06b" />
          {/* Cream Cheeks / Muzzle */}
          <ellipse cx="73" cy="33.2" rx="7.2" ry="4.8" fill="#fde8d2" />

          {/* Forehead Tabby Marks */}
          <path
            d="M71 20.5 L71.5 23.5 M75 20.5 L74.5 23.5"
            stroke="#d87f37"
            strokeWidth="1.6"
            strokeLinecap="round"
          />

          {/* Eyes: Awake when busy/approval, peacefully sleeping curves when idle */}
          {busy || approvalNeeded ? (
            <>
              <circle cx="68.5" cy="29" r="1.9" fill="#232530" />
              <circle cx="69.1" cy="28.4" r="0.6" fill="#ffffff" />
              <circle cx="77.5" cy="29" r="1.9" fill="#232530" />
              <circle cx="78.1" cy="28.4" r="0.6" fill="#ffffff" />
            </>
          ) : (
            <>
              <path
                d="M66.2 29.2 Q68.5 31.4 70.8 29.2"
                stroke="#6b442a"
                strokeWidth="1.5"
                strokeLinecap="round"
                fill="none"
              />
              <path
                d="M75.2 29.2 Q77.5 31.4 79.8 29.2"
                stroke="#6b442a"
                strokeWidth="1.5"
                strokeLinecap="round"
                fill="none"
              />
            </>
          )}

          {/* Tiny Pink Nose & Mouth */}
          <polygon points="73,31.4 71.7,30.2 74.3,30.2" fill="#ec7a9a" />
          <path
            d="M73 31.6 Q71.5 33.2 70.2 32.4 M73 31.6 Q74.5 33.2 75.8 32.4"
            stroke="#8a5a3c"
            strokeWidth="1.1"
            strokeLinecap="round"
            fill="none"
          />
        </g>

        {/* Floating Sleep "z Z" OR Approval "!" Indicator */}
        {approvalNeeded ? (
          <g>
            <circle cx="91" cy="12" r="6.5" fill="#f59e0b" />
            <text
              x="91"
              y="15.5"
              textAnchor="middle"
              fill="#141519"
              fontSize="10"
              fontWeight="bold"
            >
              !
            </text>
          </g>
        ) : !busy ? (
          <>
            <text
              x="86"
              y="16"
              fill="#a78bfa"
              fontSize="8.5"
              fontWeight="bold"
              style={{ animation: "shnexFloatZ1 2.8s ease-out infinite" }}
            >
              z
            </text>
            <text
              x="92"
              y="9"
              fill="#c4b5fd"
              fontSize="10.5"
              fontWeight="bold"
              style={{
                animation: "shnexFloatZ1 2.8s ease-out infinite 1.2s",
              }}
            >
              z
            </text>
          </>
        ) : null}
      </g>
    </svg>
  );
};

const CpuSparkline: React.FC<{ points: number[] }> = ({ points }) => {
  const width = 88;
  const height = 22;
  const padY = 3;

  const series =
    points.length >= 2 ? points.slice(-18) : [4, 4, ...(points.length ? points : [5])];
  const stepX = width / Math.max(1, series.length - 1);

  const coords = series.map((val, idx) => {
    const clamped = Math.max(0, Math.min(100, val));
    const x = Number((idx * stepX).toFixed(1));
    const y = Number(
      (height - padY - (clamped / 100) * (height - padY * 2)).toFixed(1)
    );
    return { x, y };
  });

  const pathD = coords
    .map((pt, i) => `${i === 0 ? "M" : "L"}${pt.x},${pt.y}`)
    .join(" ");
  const last = coords[coords.length - 1];

  return (
    <svg width={width} height={height} className="overflow-visible mt-1">
      <path
        d={pathD}
        fill="none"
        stroke="#e8798d"
        strokeWidth="1.75"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <circle
        cx={last.x}
        cy={last.y}
        r="2.4"
        fill="#f472b6"
        stroke="#1e202b"
        strokeWidth="1"
      />
    </svg>
  );
};

export const ServerCatCard: React.FC<Props> = ({
  title,
  maskedUserTarget,
  currentDir,
  icon,
  badgeText,
  color,
  stats,
  cpuHistory,
  aiState,
  blurPx = 16,
  onResume,
  onOpenSFTP,
  onEditHost,
  onDisconnect,
}) => {
  const [menuOpen, setMenuOpen] = useState(false);

  const cpuPct = stats ? Math.round(stats.cpu_percent) : 0;
  const memPct = stats ? Math.round(stats.mem_percent) : 0;
  const rtt = stats ? Math.round(stats.rtt_ms) : null;
  const busy = cpuPct >= 35 || aiState === "running";

  const rttColor =
    rtt === null
      ? "text-[#9ca3b4]"
      : rtt < 90
      ? "text-[#4ade80]"
      : rtt < 200
      ? "text-[#fbbf24]"
      : "text-[#f87171]";

  return (
    <div
      style={{
        backdropFilter: `blur(${blurPx}px)`,
        WebkitBackdropFilter: `blur(${blurPx}px)`,
      }}
      className="relative rounded-2xl bg-[#20222d]/78 border border-white/[0.11] px-6 py-4 flex items-center justify-between gap-5 shadow-[0_14px_38px_rgba(0,0,0,0.36)] transition hover:border-white/[0.20]"
    >
      {/* 1. Left Server Identity */}
      <div className="flex items-center gap-4 min-w-[200px]">
        <HostBadge
          icon={icon || "ubuntu"}
          badgeText={badgeText}
          color={color || "#e5e7eb"}
          size="lg"
        />
        <div className="min-w-0 space-y-0.5">
          <div className="flex items-center gap-2">
            <span
              className={`w-2.5 h-2.5 rounded-full shrink-0 ${
                aiState === "approval"
                  ? "bg-[#f59e0b] animate-ping"
                  : "bg-[#4ade80] shadow-[0_0_8px_rgba(74,222,128,0.75)]"
              }`}
            />
            <span className="text-[15px] font-bold text-white truncate">
              {title}
            </span>
          </div>
          <div className="font-mono text-xs text-[#a0a7b8] truncate">
            {maskedUserTarget}
          </div>
          <div className="flex items-center gap-1.5 text-[11px] text-[#7f8699] font-mono truncate pt-0.5">
            <Folder size={11} className="shrink-0" />
            <span className="font-sans">当前目录</span>
            <span className="truncate">{currentDir || "/"}</span>
          </div>
        </div>
      </div>

      {/* 2. Center-Left Server-Cat Mascot + 3 Telemetry Columns */}
      <div className="flex items-center gap-6">
        <ServerCatIllustration
          busy={busy}
          approvalNeeded={aiState === "approval"}
        />

        {/* Col 1: CPU + Sparkline */}
        <div className="w-[96px]">
          <div className="text-[11px] text-[#8d94a8]">CPU</div>
          <div className="mt-0.5 flex items-baseline gap-0.5">
            <span className="text-xl font-bold text-white leading-none">
              {stats ? cpuPct : "--"}
            </span>
            <span className="text-xs text-[#9ca3b4]">%</span>
          </div>
          <CpuSparkline points={cpuHistory} />
        </div>

        <div className="h-12 w-[1px] bg-white/[0.08]" />

        {/* Col 2: Memory + Progress Bar + GiB */}
        <div className="w-[116px]">
          <div className="text-[11px] text-[#8d94a8]">内存</div>
          <div className="mt-0.5 flex items-baseline gap-0.5">
            <span className="text-xl font-bold text-white leading-none">
              {stats ? memPct : "--"}
            </span>
            <span className="text-xs text-[#9ca3b4]">%</span>
          </div>
          <div className="mt-2 h-1.5 w-full rounded-full bg-white/[0.10] overflow-hidden">
            <div
              style={{ width: `${Math.min(100, Math.max(4, memPct))}%` }}
              className="h-full rounded-full bg-[#a78bfa] transition-all duration-500"
            />
          </div>
          <div className="mt-1 text-[11px] text-[#848b9e] font-mono whitespace-nowrap">
            {stats
              ? `${formatGiB(stats.mem_used_bytes)} / ${formatGiB(
                  stats.mem_total_bytes
                )} GiB`
              : "-- / -- GiB"}
          </div>
        </div>

        <div className="h-12 w-[1px] bg-white/[0.08]" />

        {/* Col 3: Latency + Server Uptime */}
        <div className="w-[118px]">
          <div className="text-[11px] text-[#8d94a8]">延迟</div>
          <div className={`mt-0.5 flex items-baseline gap-0.5 ${rttColor}`}>
            <span className="text-xl font-bold leading-none">
              {rtt !== null ? rtt : "--"}
            </span>
            <span className="text-xs">ms</span>
          </div>
          <div className="mt-2.5 text-[11px] text-[#848b9e] whitespace-nowrap">
            {stats
              ? formatUptimeCN(stats.uptime_sec || stats.connected_sec)
              : "正在采集状态..."}
          </div>
        </div>
      </div>

      {/* 3. Far-Right Action Buttons */}
      <div className="flex items-center gap-2.5 shrink-0">
        <button
          onClick={onResume}
          className="px-4 py-2 rounded-full bg-[#433749]/85 hover:bg-[#56465e] border border-white/[0.14] text-white text-xs font-semibold flex items-center gap-1.5 shadow-md transition"
        >
          <span>继续会话</span>
          <ArrowRight size={13} />
        </button>

        <div className="relative">
          <button
            onClick={() => setMenuOpen((v) => !v)}
            className="w-9 h-9 rounded-full bg-white/[0.06] hover:bg-white/[0.14] border border-white/[0.10] flex items-center justify-center text-[#d1d5db] transition"
            title="更多操作"
          >
            <MoreHorizontal size={15} />
          </button>

          {menuOpen && (
            <div
              onMouseLeave={() => setMenuOpen(false)}
              className="absolute right-0 top-10 z-40 w-36 py-1 rounded-xl bg-[#22242e] border border-white/15 shadow-2xl text-xs text-[#e5e7eb]"
            >
              <button
                onClick={() => {
                  setMenuOpen(false);
                  onOpenSFTP();
                }}
                className="w-full px-3 py-1.5 text-left hover:bg-white/10"
              >
                打开文件面板
              </button>
              {onEditHost && (
                <button
                  onClick={() => {
                    setMenuOpen(false);
                    onEditHost();
                  }}
                  className="w-full px-3 py-1.5 text-left hover:bg-white/10"
                >
                  编辑主机配置
                </button>
              )}
              <button
                onClick={() => {
                  setMenuOpen(false);
                  onDisconnect();
                }}
                className="w-full px-3 py-1.5 text-left text-[#f87171] hover:bg-[#ef4444]/15"
              >
                断开当前会话
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
