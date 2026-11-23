package transport

import (
	"context"
	"errors"
	"time"
)

// ErrSudoPasswordRequired indicates that elevated SFTP requires an interactive sudo password in memory.
var ErrSudoPasswordRequired = errors.New("sftp: sudo password required (SUDO_PASSWORD_NEEDED)")

// NetInterfaceStat reports real-time throughput and cumulative counters for a remote NIC.
type NetInterfaceStat struct {
	Name      string  `json:"name"`
	RxBytesPS float64 `json:"rx_bytes_per_sec"`
	TxBytesPS float64 `json:"tx_bytes_per_sec"`
	TotalRx   uint64  `json:"total_rx"`
	TotalTx   uint64  `json:"total_tx"`
}

// DiskStat reports filesystem capacity for a remote mount point.
type DiskStat struct {
	MountPoint  string  `json:"mount_point"`
	UsedBytes   uint64  `json:"used_bytes"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

// SystemStats contains live remote server metrics powering both the inline terminal
// telemetry strip and the System Monitor cards drawer.
type SystemStats struct {
	Hostname       string             `json:"hostname"`
	OS             string             `json:"os"`
	CPUCores       int                `json:"cpu_cores"`
	CPUPercent     float64            `json:"cpu_percent"`
	Load1          float64            `json:"load_1"`
	Load5          float64            `json:"load_5"`
	Load15         float64            `json:"load_15"`
	LoadPercent    float64            `json:"load_percent"`
	MemUsedBytes   uint64             `json:"mem_used_bytes"`
	MemTotalBytes  uint64             `json:"mem_total_bytes"`
	MemPercent     float64            `json:"mem_percent"`
	SwapUsedBytes  uint64             `json:"swap_used_bytes"`
	SwapTotalBytes uint64             `json:"swap_total_bytes"`
	SwapPercent    float64            `json:"swap_percent"`
	TotalRxBytesPS float64            `json:"total_rx_bytes_per_sec"`
	TotalTxBytesPS float64            `json:"total_tx_bytes_per_sec"`
	Interfaces     []NetInterfaceStat `json:"interfaces"`
	Disks          []DiskStat         `json:"disks"`
	RTTMillis      float64            `json:"rtt_ms"`
	UptimeSec      int64              `json:"uptime_sec,omitempty"`
	ConnectedSec   int64              `json:"connected_sec"`
	CollectedAt    time.Time          `json:"collected_at"`
}

// StatsProvider is an optional capability implemented by Session types that support
// non-intrusive server telemetry collection over the existing multiplexed connection.
type StatsProvider interface {
	CollectStats(ctx context.Context) (*SystemStats, error)
}

// CommandResult captures bounded output from a one-shot non-interactive command execution.
type CommandResult struct {
	Command    string `json:"command"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
}

// CommandExecutor is an optional capability implemented by Session types that can run
// bounded non-interactive commands on the same connection (e.g. for AI Ops assistant).
type CommandExecutor interface {
	ExecCommand(ctx context.Context, command string) (*CommandResult, error)
}

// FileEntry represents a remote file or directory returned by SFTP listing.
type FileEntry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"mod_time"`
}

// SFTPProvider is an optional capability implemented by SSH sessions for remote file browsing.
type SFTPProvider interface {
	ListDir(ctx context.Context, remotePath string) (resolvedPath string, entries []FileEntry, err error)
	ReadFile(ctx context.Context, remotePath string, maxBytes int64) ([]byte, error)
	WriteFile(ctx context.Context, remotePath string, data []byte) error
	MkdirAll(ctx context.Context, remotePath string) error
	Remove(ctx context.Context, remotePath string) error
}

// SFTPSudoConfigurable allows supplying an ephemeral in-memory sudo password for elevated SFTP.
type SFTPSudoConfigurable interface {
	SetSFTPSudoPassword(password string)
}

// PortForwardRule describes an active local-to-remote TCP port forward (127.0.0.1:local -> remote).
type PortForwardRule struct {
	ID         string    `json:"id"`
	LocalAddr  string    `json:"local_addr"`
	RemoteAddr string    `json:"remote_addr"`
	CreatedAt  time.Time `json:"created_at"`
}

// PortForwardProvider is implemented by SSH sessions supporting live TCP port forwarding.
type PortForwardProvider interface {
	StartLocalForward(ctx context.Context, localPort int, remoteAddr string) (PortForwardRule, error)
	ListForwards() []PortForwardRule
	StopForward(id string) error
}
