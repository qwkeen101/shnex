package tailnet

import (
	"context"
	"net"
	"net/netip"
	"testing"

	"github.com/shnex/shnex/core/platform"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
	"tailscale.com/types/views"
)

type fakeTSBackend struct {
	cfg            Config
	status         *ipnstate.Status
	busAuthURL     string
	interactiveHit bool
	dialedAddr     string
}

func (f *fakeTSBackend) Start(context.Context) error { return nil }
func (f *fakeTSBackend) StartInteractiveLogin(context.Context) error {
	f.interactiveHit = true
	f.busAuthURL = "https://headscale.example.com/register/nodekey:abcdef"
	return nil
}
func (f *fakeTSBackend) Status(context.Context) (*ipnstate.Status, string, error) {
	return f.status, f.busAuthURL, nil
}
func (f *fakeTSBackend) Dial(_ context.Context, _, addr string) (net.Conn, error) {
	f.dialedAddr = addr
	c1, c2 := net.Pipe()
	_ = c2.Close()
	return c1, nil
}
func (f *fakeTSBackend) Close() error { return nil }

func TestManagerHeadscaleNodesAndTailscaleSSHPlan(t *testing.T) {
	ctx := context.Background()
	bridge := platform.NewMemoryBridge(nil)
	_ = bridge.SetSecret(ctx, "hs-auth", "hskey-secret-123")

	var capturedFake *fakeTSBackend
	mgr := NewManager(bridge, platform.NewNetworkBroadcaster())
	mgr.factory = func(cfg Config) (backend, error) {
		tags := views.SliceOf([]string{"tag:ai-server"})
		kOnline := key.NewNode().Public()
		kOffline := key.NewNode().Public()

		capturedFake = &fakeTSBackend{
			cfg: cfg,
			status: &ipnstate.Status{
				BackendState: "Running",
				CurrentTailnet: &ipnstate.TailnetStatus{
					MagicDNSSuffix: "ai.headscale.net.",
				},
				Peer: map[key.NodePublic]*ipnstate.PeerStatus{
					kOnline: {
						ID:           "node-1",
						HostName:     "claude-box",
						DNSName:      "claude-box.ai.headscale.net.",
						OS:           "linux",
						Online:       true,
						TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.10")},
						Tags:         &tags,
						SSH_HostKeys: []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILM+testkey"},
					},
					kOffline: {
						ID:       "node-2",
						HostName: "old-laptop",
						DNSName:  "old-laptop.ai.headscale.net.",
						OS:       "windows",
						Online:   false,
					},
				},
			},
		}
		return capturedFake, nil
	}

	tmpDir := t.TempDir()
	snap, err := mgr.Start(ctx, Config{
		Hostname:      "shnex-win",
		StateDir:      tmpDir,
		ControlURL:    "https://headscale.example.com",
		AuthSecretRef: "hs-auth",
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if capturedFake.cfg.AuthKey != "hskey-secret-123" {
		t.Fatalf("expected AuthKey resolved from PlatformBridge, got %q", capturedFake.cfg.AuthKey)
	}
	if snap.ControlURL != "https://headscale.example.com" || snap.MagicDNSSuffix != "ai.headscale.net" {
		t.Fatalf("unexpected status snapshot: %+v", snap)
	}

	onlineNodes, err := mgr.ListNodes(ctx, true)
	if err != nil {
		t.Fatalf("ListNodes(onlineOnly=true) failed: %v", err)
	}
	if len(onlineNodes) != 1 {
		t.Fatalf("expected 1 online node, got %d", len(onlineNodes))
	}
	if onlineNodes[0].MagicDNSName != "claude-box.ai.headscale.net" || !onlineNodes[0].SupportsTSSSH {
		t.Fatalf("unexpected online node metadata: %+v", onlineNodes[0])
	}

	plan, err := mgr.PrepareTailscaleSSH(ctx, "claude-box", "ubuntu", TailscaleSSHAuto, false)
	if err != nil {
		t.Fatalf("PrepareTailscaleSSH failed: %v", err)
	}
	if plan.TargetAddr != "claude-box.ai.headscale.net:22" || len(plan.AuthorizedHostKeys) != 1 {
		t.Fatalf("unexpected TailscaleSSH plan: %+v", plan)
	}

	conn, err := plan.Dialer.Dial(ctx, plan.TargetAddr)
	if err != nil {
		t.Fatalf("plan.Dialer.Dial failed: %v", err)
	}
	_ = conn.Close()
	if capturedFake.dialedAddr != "claude-box.ai.headscale.net:22" {
		t.Fatalf("expected tsnet dial to MagicDNS target, got %q", capturedFake.dialedAddr)
	}
}
