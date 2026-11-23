package ssh

import (
	"testing"
	"time"
)

func TestParseTelemetryOutput(t *testing.T) {
	sample := `aws-prod
Linux 6.12.57-amd64
4
===STAT===
cpu  1000 0 500 8500 0 0 0 0 0 0
===LOAD===
0.40 0.20 0.10 1/245 9912
===MEM===
MemTotal:        1677720 kB
MemFree:          219136 kB
MemAvailable:     595968 kB
Buffers:           40000 kB
Cached:           595000 kB
SwapTotal:       1048576 kB
SwapFree:        1048576 kB
===NET===
Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:   50000     100    0    0    0     0          0         0    50000     100    0    0    0     0       0          0
  eth0: 1048576     800    0    0    0     0          0         0   524288     600    0    0    0     0       0          0
===DF===
/dev/vda1       41943040 13841203 28101837      33% /
===UPTIME===
1897214.52 3700000.11
`
	st := parseTelemetryOutput(sample, time.Now())
	if st.Hostname != "aws-prod" || st.CPUCores != 4 {
		t.Fatalf("unexpected header: %+v", st)
	}
	if st.UptimeSec != 1897214 {
		t.Fatalf("expected UptimeSec=1897214, got %d", st.UptimeSec)
	}
	if st.MemPercent < 64.0 || st.MemPercent > 65.0 {
		t.Fatalf("expected ~64.5%% mem usage, got %.1f%%", st.MemPercent)
	}
	if len(st.Interfaces) != 1 || st.Interfaces[0].Name != "eth0" {
		t.Fatalf("expected eth0 only (lo skipped), got %+v", st.Interfaces)
	}
	if len(st.Disks) != 1 || st.Disks[0].MountPoint != "/" {
		t.Fatalf("unexpected disks: %+v", st.Disks)
	}
}
