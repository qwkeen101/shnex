import React from "react";
import { Cpu, HardDrive, MemoryStick, Network, Server, X } from "lucide-react";
import {
  SystemStats,
  formatBytes,
  formatDurationSec,
  formatRate,
} from "../api";

interface Props {
  hostTitle: string;
  userTarget: string;
  stats: SystemStats | null;
  onClose: () => void;
}

export const SystemMonitorDrawer: React.FC<Props> = ({
  hostTitle,
  userTarget,
  stats,
  onClose,
}) => {
  const cores = Math.max(1, stats?.cpu_cores ?? 1);

  return (
    <div className="w-[292px] h-full bg-[#1b1c20] border-l border-[#282a32] flex flex-col text-xs shrink-0">
      <div className="h-9 px-3.5 border-b border-[#282a32] flex items-center justify-between shrink-0">
        <span className="font-semibold text-[#f3f4f6]">系统监控</span>
        <button
          onClick={onClose}
          className="p-1 rounded hover:bg-[#2a2d36] text-[#8b909a] hover:text-[#e5e7eb]"
        >
          <X size={14} />
        </button>
      </div>

      {!stats ? (
        <div className="p-6 text-center text-[#8b909a]">
          正在连接并采集远端主机实时指标...
        </div>
      ) : (
        <div className="p-2.5 flex-1 overflow-y-auto space-y-2.5">
          {/* Card 1: System */}
          <div className="p-3 rounded-xl bg-[#22242a] border border-[#2d3039] space-y-2.5">
            <div className="flex items-center gap-2 font-medium text-[#f3f4f6]">
              <Server size={14} className="text-[#f59e0b]" />
              <span>系统</span>
            </div>
            <div className="grid grid-cols-2 gap-y-2.5 gap-x-3">
              <div>
                <div className="text-[#8b909a] text-[11px]">主机地址</div>
                <div className="font-medium truncate mt-0.5">{hostTitle}</div>
              </div>
              <div>
                <div className="text-[#8b909a] text-[11px]">操作系统</div>
                <div className="font-mono truncate mt-0.5">{stats.os || "linux"}</div>
              </div>
              <div>
                <div className="text-[#8b909a] text-[11px]">延迟</div>
                <div className="font-mono text-[#3fb950] mt-0.5">
                  {stats.rtt_ms.toFixed(0)} 毫秒
                </div>
              </div>
              <div>
                <div className="text-[#8b909a] text-[11px]">已连接</div>
                <div className="font-mono mt-0.5">
                  {formatDurationSec(stats.connected_sec)}
                </div>
              </div>
            </div>
            <div>
              <div className="text-[#8b909a] text-[11px]">地址</div>
              <div className="font-mono text-[#d1d5db] truncate mt-0.5">
                {userTarget}
              </div>
            </div>
          </div>

          {/* Card 2: CPU */}
          <div className="p-3 rounded-xl bg-[#22242a] border border-[#2d3039] space-y-2.5">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2 font-medium text-[#f3f4f6]">
                <Cpu size={14} className="text-[#3fb950]" />
                <span>CPU</span>
              </div>
              <span className="text-[#8b909a] text-[11px]">{cores} 核</span>
            </div>

            <div className="flex items-center gap-3">
              <span className="text-xl font-semibold text-[#3b82f6] font-mono">
                {stats.cpu_percent.toFixed(0)}%
              </span>
              <div className="flex-1 space-y-1">
                <div className="text-[11px] text-[#8b909a]">平均使用率</div>
                <div className="h-1.5 rounded-full bg-[#16171b] overflow-hidden">
                  <div
                    className="h-full bg-[#3b82f6] transition-all duration-300"
                    style={{ width: `${Math.min(100, stats.cpu_percent)}%` }}
                  />
                </div>
              </div>
            </div>

            {/* Per-core visual slots */}
            <div className="grid grid-cols-2 gap-1.5">
              {Array.from({ length: Math.min(cores, 8) }).map((_, i) => (
                <div
                  key={i}
                  className="h-5 rounded bg-[#18191e] border border-[#2b2e37] overflow-hidden flex items-end"
                >
                  <div
                    className="w-full bg-[#3b82f6]/35 transition-all"
                    style={{ height: `${Math.max(8, stats.cpu_percent)}%` }}
                  />
                </div>
              ))}
            </div>

            <div className="flex items-center justify-between text-[11px] pt-0.5">
              <span className="text-[#8b909a]">平均负载</span>
              <span className="font-mono text-[#e5e7eb]">
                {stats.load_1.toFixed(2)} &nbsp;{stats.load_5.toFixed(2)} &nbsp;
                {stats.load_15.toFixed(2)}
              </span>
            </div>
          </div>

          {/* Card 3: Memory */}
          <div className="p-3 rounded-xl bg-[#22242a] border border-[#2d3039] space-y-2.5">
            <div className="flex items-center gap-2 font-medium text-[#f3f4f6]">
              <MemoryStick size={14} className="text-[#c084fc]" />
              <span>内存</span>
            </div>

            <div className="space-y-1">
              <div className="flex justify-between text-[11px]">
                <span className="text-[#d1d5db]">物理内存</span>
                <span className="font-mono text-[#60a5fa]">
                  {stats.mem_percent.toFixed(0)}%
                </span>
              </div>
              <div className="h-1.5 rounded-full bg-[#16171b] overflow-hidden">
                <div
                  className="h-full bg-[#3b82f6] transition-all duration-300"
                  style={{ width: `${Math.min(100, stats.mem_percent)}%` }}
                />
              </div>
              <div className="text-[11px] text-[#8b909a] font-mono">
                {formatBytes(stats.mem_used_bytes)} /{" "}
                {formatBytes(stats.mem_total_bytes)}
              </div>
            </div>

            <div className="space-y-1">
              <div className="flex justify-between text-[11px]">
                <span className="text-[#d1d5db]">交换分区</span>
                <span className="font-mono text-[#60a5fa]">
                  {stats.swap_percent.toFixed(0)}%
                </span>
              </div>
              <div className="h-1.5 rounded-full bg-[#16171b] overflow-hidden">
                <div
                  className="h-full bg-[#3b82f6] transition-all duration-300"
                  style={{ width: `${Math.min(100, stats.swap_percent)}%` }}
                />
              </div>
              <div className="text-[11px] text-[#8b909a] font-mono">
                {formatBytes(stats.swap_used_bytes)} /{" "}
                {formatBytes(stats.swap_total_bytes)}
              </div>
            </div>
          </div>

          {/* Card 4: Network Interfaces */}
          <div className="p-3 rounded-xl bg-[#22242a] border border-[#2d3039] space-y-2">
            <div className="flex items-center gap-2 font-medium text-[#f3f4f6]">
              <Network size={14} className="text-[#22d3ee]" />
              <span>网络</span>
            </div>
            <div className="space-y-1.5">
              {(stats.interfaces || []).map((nic) => (
                <div
                  key={nic.name}
                  className="px-2.5 py-1.5 rounded-lg bg-[#191a1f] border border-[#292b33] flex items-center justify-between font-mono text-[11px]"
                >
                  <span className="truncate text-[#e5e7eb] max-w-[105px]">
                    {nic.name}
                  </span>
                  <div className="flex items-center gap-2 shrink-0">
                    <span className="text-[#3fb950]">
                      ↑ {formatRate(nic.tx_bytes_per_sec)}
                    </span>
                    <span className="text-[#60a5fa]">
                      ↓ {formatRate(nic.rx_bytes_per_sec)}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          </div>

          {/* Card 5: Disks */}
          <div className="p-3 rounded-xl bg-[#22242a] border border-[#2d3039] space-y-2">
            <div className="flex items-center gap-2 font-medium text-[#f3f4f6]">
              <HardDrive size={14} className="text-[#f59e0b]" />
              <span>磁盘</span>
            </div>
            {(stats.disks || []).map((d) => (
              <div key={d.mount_point} className="space-y-1">
                <div className="flex justify-between font-mono text-[11px]">
                  <span>{d.mount_point}</span>
                  <span className="text-[#60a5fa]">
                    {d.used_percent.toFixed(0)}%
                  </span>
                </div>
                <div className="h-1.5 rounded-full bg-[#16171b] overflow-hidden">
                  <div
                    className="h-full bg-[#3b82f6]"
                    style={{ width: `${Math.min(100, d.used_percent)}%` }}
                  />
                </div>
                <div className="text-[11px] text-[#8b909a] font-mono">
                  {formatBytes(d.used_bytes)} / {formatBytes(d.total_bytes)}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
};
