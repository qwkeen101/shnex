package localpty

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shnex/shnex/core/transport"
)

func TestLocalPTYSessionEcho(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tp := NewTransport()
	sess, err := tp.OpenSession(ctx, transport.SessionConfig{
		Target:      "cmd",
		InitialSize: transport.TerminalSize{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("OpenSession(local cmd): %v", err)
	}
	defer sess.Close()

	_, err = sess.Write([]byte("echo SHNEX_LOCAL_OK\r\n"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	buf := make([]byte, 4096)
	var out strings.Builder
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n, rErr := sess.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			if strings.Contains(out.String(), "SHNEX_LOCAL_OK") {
				return
			}
		}
		if rErr != nil {
			break
		}
	}
	t.Fatalf("did not observe SHNEX_LOCAL_OK in local PTY output; got: %q", out.String())
}

func TestLocalPTYPowerShellBuiltinPowerline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	tp := NewTransport()
	sess, err := tp.OpenSession(ctx, transport.SessionConfig{
		Target:           "powershell",
		BuiltinPowerline: true,
		InitialSize:      transport.TerminalSize{Cols: 100, Rows: 28},
	})
	if err != nil {
		t.Fatalf("OpenSession(powershell powerline): %v", err)
	}
	defer sess.Close()

	buf := make([]byte, 4096)
	var out strings.Builder
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		n, rErr := sess.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			s := out.String()
			if strings.Contains(s, "]7;file://") && strings.Contains(s, "]133;B") {
				return
			}
		}
		if rErr != nil {
			break
		}
	}
	t.Fatalf("did not observe OSC 7 + OSC 133;B from built-in Powerline prompt; got: %q", out.String())
}
