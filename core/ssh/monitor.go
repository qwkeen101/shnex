package ssh

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shnex/shnex/core/transport"
	gossh "golang.org/x/crypto/ssh"
)

const maxExecOutputBytes = 128 * 1024

type cpuSample struct {
	total uint64
	idle  uint64
}

type netSample struct {
	rx uint64
	tx uint64
}

type telemetrySnapshot struct {
	at  time.Time
	cpu cpuSample
	net map[string]netSample
}

const linuxTelemetryScript = `uname -n 2>/dev/null || echo unknown
uname -sr 2>/dev/null || echo linux
nproc 2>/dev/null || grep -c '^processor' /proc/cpuinfo 2>/dev/null || echo 1
echo '===STAT==='
head -n 1 /proc/stat 2>/dev/null
echo '===LOAD==='
cat /proc/loadavg 2>/dev/null
echo '===MEM==='
grep -E '^(MemTotal|MemAvailable|MemFree|Buffers|Cached|SwapTotal|SwapFree):' /proc/meminfo 2>/dev/null
echo '===NET==='
cat /proc/net/dev 2>/dev/null
echo '===DF==='
df -kP / 2>/dev/null | tail -n +2
echo '===UPTIME==='
cat /proc/uptime 2>/dev/null`

// ExecCommand executes a non-interactive command over a multiplexed channel on the existing SSH connection.
func (s *Session) ExecCommand(ctx context.Context, command string) (*transport.CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-s.stopCh:
		return nil, errors.New("ssh: session closed")
	default:
	}

	execSess, err := s.client.NewSession()
	if err != nil {
		return nil, err
	}
	defer execSess.Close()

	var stdoutBuf, stderrBuf limitedBuffer
	stdoutBuf.limit = maxExecOutputBytes
	stderrBuf.limit = maxExecOutputBytes
	execSess.Stdout = &stdoutBuf
	execSess.Stderr = &stderrBuf

	stop := context.AfterFunc(ctx, func() {
		_ = execSess.Close()
	})
	defer stop()

	start := time.Now()
	err = execSess.Run(command)
	dur := time.Since(start).Milliseconds()

	exitCode := 0
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		var exitErr *gossh.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitStatus()
		} else {
			return nil, err
		}
	}

	return &transport.CommandResult{
		Command:    command,
		Stdout:     stdoutBuf.String(),
		Stderr:     stderrBuf.String(),
		ExitCode:   exitCode,
		DurationMs: dur,
	}, nil
}

// CollectStats gathers CPU, Load, RAM, Swap, per-NIC throughput, Disk, and SSH RTT
// over a lightweight multiplexed exec channel with zero remote agent installation required.
func (s *Session) CollectStats(ctx context.Context) (*transport.SystemStats, error) {
	probeStart := time.Now()
	res, err := s.ExecCommand(ctx, linuxTelemetryScript)
	if err != nil {
		return nil, err
	}
	rttMs := float64(time.Since(probeStart).Microseconds()) / 1000.0
	now := time.Now()

	stats := parseTelemetryOutput(res.Stdout, now)
	stats.RTTMillis = math.Round(rttMs*10) / 10
	if !s.createdAt.IsZero() {
		stats.ConnectedSec = int64(now.Sub(s.createdAt).Seconds())
	}

	// Compute exact CPU% and per-interface RX/TX bytes/sec deltas against previous sample.
	curCPU := parseRawCPUSample(res.Stdout)
	curNet := parseRawNetSamples(res.Stdout)

	s.statsMu.Lock()
	prev := s.prevStats
	s.prevStats = &telemetrySnapshot{
		at:  now,
		cpu: curCPU,
		net: curNet,
	}
	s.statsMu.Unlock()

	if prev != nil {
		dt := now.Sub(prev.at).Seconds()
		if curCPU.total > prev.cpu.total {
			dTotal := float64(curCPU.total - prev.cpu.total)
			dIdle := float64(curCPU.idle - prev.cpu.idle)
			usedPct := ((dTotal - dIdle) / dTotal) * 100.0
			stats.CPUPercent = clampPercent(usedPct)
		}
		if dt > 0.05 {
			var totalRxPS, totalTxPS float64
			for i := range stats.Interfaces {
				nic := &stats.Interfaces[i]
				if pNic, ok := prev.net[nic.Name]; ok {
					if nic.TotalRx >= pNic.rx {
						nic.RxBytesPS = math.Round(float64(nic.TotalRx-pNic.rx) / dt)
					}
					if nic.TotalTx >= pNic.tx {
						nic.TxBytesPS = math.Round(float64(nic.TotalTx-pNic.tx) / dt)
					}
					totalRxPS += nic.RxBytesPS
					totalTxPS += nic.TxBytesPS
				}
			}
			stats.TotalRxBytesPS = totalRxPS
			stats.TotalTxBytesPS = totalTxPS
		}
	} else if curCPU.total > 0 {
		usedPct := (float64(curCPU.total-curCPU.idle) / float64(curCPU.total)) * 100.0
		stats.CPUPercent = clampPercent(usedPct)
	}

	return stats, nil
}

func parseTelemetryOutput(out string, now time.Time) *transport.SystemStats {
	st := &transport.SystemStats{
		CPUCores:    1,
		CollectedAt: now,
	}

	sections := splitSections(out)
	headerLines := strings.Split(strings.TrimSpace(sections["HEADER"]), "\n")
	if len(headerLines) >= 1 {
		st.Hostname = strings.TrimSpace(headerLines[0])
	}
	if len(headerLines) >= 2 {
		st.OS = strings.TrimSpace(headerLines[1])
	}
	if len(headerLines) >= 3 {
		if cores, err := strconv.Atoi(strings.TrimSpace(headerLines[2])); err == nil && cores > 0 {
			st.CPUCores = cores
		}
	}

	// Loadavg
	loadFields := strings.Fields(sections["LOAD"])
	if len(loadFields) >= 3 {
		st.Load1, _ = strconv.ParseFloat(loadFields[0], 64)
		st.Load5, _ = strconv.ParseFloat(loadFields[1], 64)
		st.Load15, _ = strconv.ParseFloat(loadFields[2], 64)
		if st.CPUCores > 0 {
			st.LoadPercent = clampPercent((st.Load1 / float64(st.CPUCores)) * 100.0)
		}
	}

	// Meminfo (values in KiB)
	mem := make(map[string]uint64)
	for _, line := range strings.Split(sections["MEM"], "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			key := strings.TrimSuffix(parts[0], ":")
			val, _ := strconv.ParseUint(parts[1], 10, 64)
			mem[key] = val * 1024
		}
	}
	st.MemTotalBytes = mem["MemTotal"]
	if st.MemTotalBytes > 0 {
		avail := mem["MemAvailable"]
		if avail == 0 {
			avail = mem["MemFree"] + mem["Buffers"] + mem["Cached"]
		}
		if st.MemTotalBytes >= avail {
			st.MemUsedBytes = st.MemTotalBytes - avail
		}
		st.MemPercent = clampPercent((float64(st.MemUsedBytes) / float64(st.MemTotalBytes)) * 100.0)
	}
	st.SwapTotalBytes = mem["SwapTotal"]
	if st.SwapTotalBytes > 0 && st.SwapTotalBytes >= mem["SwapFree"] {
		st.SwapUsedBytes = st.SwapTotalBytes - mem["SwapFree"]
		st.SwapPercent = clampPercent((float64(st.SwapUsedBytes) / float64(st.SwapTotalBytes)) * 100.0)
	}

	// Network interfaces
	rawNets := parseRawNetSamples(out)
	nics := make([]transport.NetInterfaceStat, 0, len(rawNets))
	for name, ns := range rawNets {
		nics = append(nics, transport.NetInterfaceStat{
			Name:    name,
			TotalRx: ns.rx,
			TotalTx: ns.tx,
		})
	}
	sort.Slice(nics, func(i, j int) bool { return nics[i].Name < nics[j].Name })
	st.Interfaces = nics

	// Disk df -kP
	for _, line := range strings.Split(strings.TrimSpace(sections["DF"]), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 6 {
			totalKB, _ := strconv.ParseUint(fields[1], 10, 64)
			usedKB, _ := strconv.ParseUint(fields[2], 10, 64)
			totalB := totalKB * 1024
			usedB := usedKB * 1024
			var pct float64
			if totalB > 0 {
				pct = clampPercent((float64(usedB) / float64(totalB)) * 100.0)
			}
			st.Disks = append(st.Disks, transport.DiskStat{
				MountPoint:  fields[5],
				UsedBytes:   usedB,
				TotalBytes:  totalB,
				UsedPercent: pct,
			})
		}
	}

	// System Uptime (/proc/uptime)
	uptimeFields := strings.Fields(sections["UPTIME"])
	if len(uptimeFields) >= 1 {
		if upFloat, err := strconv.ParseFloat(uptimeFields[0], 64); err == nil && upFloat > 0 {
			st.UptimeSec = int64(upFloat)
		}
	}

	return st
}

func parseRawCPUSample(out string) cpuSample {
	sections := splitSections(out)
	fields := strings.Fields(sections["STAT"])
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuSample{}
	}
	var total, idle uint64
	for i := 1; i < len(fields); i++ {
		v, _ := strconv.ParseUint(fields[i], 10, 64)
		total += v
		if i == 4 || i == 5 { // idle + iowait
			idle += v
		}
	}
	return cpuSample{total: total, idle: idle}
}

func parseRawNetSamples(out string) map[string]netSample {
	sections := splitSections(out)
	res := make(map[string]netSample)
	for _, line := range strings.Split(sections["NET"], "\n") {
		idx := strings.IndexByte(line, ':')
		if idx <= 0 {
			continue
		}
		name := strings.TrimSpace(line[:idx])
		if name == "" || name == "lo" {
			continue
		}
		fields := strings.Fields(line[idx+1:])
		if len(fields) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		res[name] = netSample{rx: rx, tx: tx}
	}
	return res
}

func splitSections(out string) map[string]string {
	res := make(map[string]string)
	cur := "HEADER"
	var buf strings.Builder
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "===") && strings.HasSuffix(trimmed, "===") && len(trimmed) > 6 {
			res[cur] = buf.String()
			buf.Reset()
			cur = strings.Trim(trimmed, "=")
			continue
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	res[cur] = buf.String()
	return res
}

func clampPercent(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return math.Round(v*10) / 10
}

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	origLen := len(p)
	rem := l.limit - l.buf.Len()
	if rem > 0 {
		if len(p) > rem {
			p = p[:rem]
		}
		_, _ = l.buf.Write(p)
	}
	return origLen, nil
}

func (l *limitedBuffer) String() string {
	return l.buf.String()
}

var _ io.Writer = (*limitedBuffer)(nil)
