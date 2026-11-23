import React from "react";
import {
  Apple,
  Bot,
  Box,
  Building2,
  Cloud,
  Code2,
  Cpu,
  Database,
  FlaskConical,
  Gamepad2,
  GitBranch,
  Globe,
  Grid2x2,
  Home,
  Laptop,
  Monitor,
  Rocket,
  Server,
  Shield,
  Smartphone,
  Terminal,
  Wifi,
} from "lucide-react";

export const HOST_COLORS = [
  "#22c55e", // Green
  "#f59e0b", // Amber
  "#22d3ee", // Cyan
  "#c084fc", // Purple
  "#6366f1", // Indigo
  "#f43f5e", // Rose
  "#ef4444", // Red
  "#0ea5e9", // Sky Blue
] as const;

export const HOST_ICONS = [
  "server",
  "database",
  "cloud",
  "monitor",
  "laptop",
  "wifi",
  "smartphone",
  "box",
  "code",
  "rocket",
  "globe",
  "flask",
  "git",
  "home",
  "building",
  "shield",
  "gamepad",
  "linux",
  "grid",
  "apple",
  "windows",
  "android",
] as const;

export type HostIconName = (typeof HOST_ICONS)[number];

export function renderHostVectorIcon(name?: string, size = 13): React.ReactNode {
  switch (name) {
    case "database":
      return <Database size={size} />;
    case "cloud":
      return <Cloud size={size} />;
    case "monitor":
      return <Monitor size={size} />;
    case "laptop":
      return <Laptop size={size} />;
    case "wifi":
      return <Wifi size={size} />;
    case "smartphone":
      return <Smartphone size={size} />;
    case "box":
      return <Box size={size} />;
    case "code":
      return <Code2 size={size} />;
    case "rocket":
      return <Rocket size={size} />;
    case "globe":
      return <Globe size={size} />;
    case "flask":
      return <FlaskConical size={size} />;
    case "git":
      return <GitBranch size={size} />;
    case "home":
      return <Home size={size} />;
    case "building":
      return <Building2 size={size} />;
    case "shield":
      return <Shield size={size} />;
    case "gamepad":
      return <Gamepad2 size={size} />;
    case "linux":
      return <Terminal size={size} />;
    case "grid":
      return <Grid2x2 size={size} />;
    case "apple":
      return <Apple size={size} />;
    case "windows":
      return <Cpu size={size} />;
    case "android":
      return <Bot size={size} />;
    default:
      return <Server size={size} />;
  }
}

export function pickAutoIcon(name: string, host: string): HostIconName {
  const s = `${name} ${host}`.toLowerCase();
  if (s.includes("aws") || s.includes("aliyun") || s.includes("cloud")) return "cloud";
  if (s.includes("db") || s.includes("sql") || s.includes("redis")) return "database";
  if (s.includes("gpu") || s.includes("ai") || s.includes("a100")) return "gamepad";
  if (s.includes("home") || s.includes("nas")) return "home";
  if (s.includes("mac")) return "apple";
  if (s.includes("win")) return "windows";
  if (s.includes("docker") || s.includes("k8s")) return "box";
  if (s.includes("git")) return "git";
  return "rocket";
}

export function pickAutoColor(name: string, host: string): string {
  const str = `${name}:${host}`;
  let hash = 0;
  for (let i = 0; i < str.length; i++) {
    hash = (hash * 31 + str.charCodeAt(i)) | 0;
  }
  return HOST_COLORS[Math.abs(hash) % HOST_COLORS.length];
}

export const HostBadge: React.FC<{
  icon?: string;
  badgeText?: string;
  color?: string;
  size?: "xs" | "sm" | "md" | "lg";
}> = ({ icon, badgeText, color = "#f59e0b", size = "sm" }) => {
  const dims =
    size === "lg"
      ? "w-11 h-11 rounded-2xl text-base"
      : size === "md"
      ? "w-9 h-9 rounded-xl text-xs"
      : size === "xs"
      ? "w-[18px] h-[18px] rounded-[5px] text-[9.5px]"
      : "w-6 h-6 rounded-lg text-[11px]";
  const iconPx =
    size === "lg" ? 21 : size === "md" ? 18 : size === "xs" ? 11 : 13;

  return (
    <span
      style={{ backgroundColor: color || "#f59e0b" }}
      className={`${dims} text-white font-bold flex items-center justify-center shrink-0 shadow-sm select-none overflow-hidden`}
    >
      {badgeText && badgeText.trim() !== "" ? (
        <span className="leading-none truncate px-0.5">
          {badgeText.trim().slice(0, 4)}
        </span>
      ) : (
        renderHostVectorIcon(icon || "rocket", iconPx)
      )}
    </span>
  );
};
