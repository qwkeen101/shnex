package relay

import (
	"bytes"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/shnex/shnex/core/transport"
)

func TestSanitizeWorkspaceNameAndBootstrapSafety(t *testing.T) {
	got := SanitizeWorkspaceName("claude; rm -rf / $(whoami)")
	if got != "clauderm-rfwhoami" {
		t.Fatalf("unexpected sanitized name: %q", got)
	}
	if SanitizeWorkspaceName("   ") != DefaultWorkspaceName {
		t.Fatal("expected fallback to DefaultWorkspaceName")
	}

	cmdAuto := BuildPTYBootstrapCommand(transport.RelayConfig{
		Mode:        transport.RelayAuto,
		SessionName: "my-work",
	})
	if !strings.Contains(cmdAuto, "tmux") || !strings.Contains(cmdAuto, "zellij") || !strings.Contains(cmdAuto, "window-size latest") {
		t.Fatalf("unexpected auto bootstrap script: %s", cmdAuto)
	}
}

func TestDaemonDesktopToMobileHandoffAndReplay(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	d := NewDaemon(0)
	defer d.Close()
	go func() { _ = d.Serve(ln) }()

	// 1. Desktop attaches and creates persistent workspace "ai-bench", writes output, then disconnects.
	desktopConn, err := net.DialTimeout("tcp4", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial desktop: %v", err)
	}
	dSW := NewStreamWriter(desktopConn)
	if err := dSW.WriteMagic(); err != nil {
		t.Fatal(err)
	}
	if err := ReadMagic(desktopConn); err != nil {
		t.Fatal(err)
	}
	if err := dSW.WriteJSON(PktHello, HelloPayload{
		SessionName: "ai-bench",
		Cols:        120,
		Rows:        40,
	}); err != nil {
		t.Fatal(err)
	}

	pt, pl, err := ReadPacket(desktopConn)
	if err != nil || pt != PktWelcome {
		t.Fatalf("desktop welcome err=%v pt=%d", err, pt)
	}
	var dWelcome WelcomePayload
	_ = json.Unmarshal(pl, &dWelcome)
	if !dWelcome.NewlyCreated {
		t.Fatal("expected NewlyCreated=true on first desktop attach")
	}

	// Run an echo command inside the persistent shell
	_ = dSW.WritePacket(PktData, []byte("echo HANDOFF_PERSISTED_TOKEN\r\n"))

	var desktopSeen bytes.Buffer
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = desktopConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		pType, pData, rErr := ReadPacket(desktopConn)
		if rErr != nil {
			break
		}
		if pType == PktData {
			desktopSeen.Write(pData)
			if strings.Contains(desktopSeen.String(), "HANDOFF_PERSISTED_TOKEN") {
				break
			}
		}
	}
	if !strings.Contains(desktopSeen.String(), "HANDOFF_PERSISTED_TOKEN") {
		t.Fatalf("desktop did not see echo output, got: %q", desktopSeen.String())
	}

	// Disconnect desktop completely!
	_ = desktopConn.Close()
	time.Sleep(40 * time.Millisecond)

	// 2. Mobile connects to the SAME workspace "ai-bench": must attach to same PID and replay scrollback!
	mobileConn, err := net.DialTimeout("tcp4", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial mobile: %v", err)
	}
	defer mobileConn.Close()

	mSW := NewStreamWriter(mobileConn)
	_ = mSW.WriteMagic()
	if err := ReadMagic(mobileConn); err != nil {
		t.Fatal(err)
	}
	_ = mSW.WriteJSON(PktHello, HelloPayload{
		SessionName: "ai-bench",
		Cols:        48,
		Rows:        28,
	})

	pt2, pl2, err := ReadPacket(mobileConn)
	if err != nil || pt2 != PktWelcome {
		t.Fatalf("mobile welcome err=%v pt=%d", err, pt2)
	}
	var mWelcome WelcomePayload
	_ = json.Unmarshal(pl2, &mWelcome)
	if mWelcome.NewlyCreated || mWelcome.PID != dWelcome.PID || mWelcome.ReplayedBytes == 0 {
		t.Fatalf("expected mobile to reuse existing workspace & replay bytes: desktop=%+v mobile=%+v", dWelcome, mWelcome)
	}

	var mobileReplay bytes.Buffer
	replayDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(replayDeadline) {
		_ = mobileConn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
		pType, pData, rErr := ReadPacket(mobileConn)
		if rErr != nil {
			break
		}
		if pType == PktData {
			mobileReplay.Write(pData)
			if strings.Contains(mobileReplay.String(), "HANDOFF_PERSISTED_TOKEN") {
				break
			}
		}
	}
	if !strings.Contains(mobileReplay.String(), "HANDOFF_PERSISTED_TOKEN") {
		t.Fatalf("mobile did not receive scrollback replay token, got %q", mobileReplay.String())
	}
}
