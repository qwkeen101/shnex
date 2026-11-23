package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/shnex/shnex/core/platform"
	"github.com/shnex/shnex/core/relay"
	"github.com/shnex/shnex/core/transport"
	gossh "golang.org/x/crypto/ssh"
)

func TestHostKeyPolicyRequiredByDefault(t *testing.T) {
	tp := NewTransport(platform.NewMemoryBridge(nil), platform.NewNetworkBroadcaster())
	_, err := tp.OpenSession(context.Background(), transport.SessionConfig{
		Target: "127.0.0.1:22",
		User:   "root",
		Credentials: transport.CredentialsSpec{
			Password: "pw",
		},
	})
	if !errors.Is(err, ErrHostKeyVerificationRequired) {
		t.Fatalf("expected ErrHostKeyVerificationRequired, got %v", err)
	}
}

func TestAuthorizedHostKeysAndNetworkChangeProbe(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	authorizedLine := strings.TrimSpace(string(gossh.MarshalAuthorizedKey(signer.PublicKey())))

	srvCfg := &gossh.ServerConfig{
		PasswordCallback: func(c gossh.ConnMetadata, pass []byte) (*gossh.Permissions, error) {
			return nil, nil
		},
	}
	srvCfg.AddHostKey(signer)

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, chans, reqs, err := gossh.NewServerConn(c, srvCfg)
				if err != nil {
					return
				}
				go func() {
					for r := range reqs {
						if r.WantReply {
							_ = r.Reply(true, nil)
						}
					}
				}()
				for nch := range chans {
					ch, chReqs, err := nch.Accept()
					if err != nil {
						continue
					}
					go func() {
						for r := range chReqs {
							if r.WantReply {
								_ = r.Reply(true, nil)
							}
						}
					}()
					_ = ch
				}
			}(nc)
		}
	}()

	netBroadcaster := platform.NewNetworkBroadcaster()
	tp := NewTransport(platform.NewMemoryBridge(nil), netBroadcaster)

	probedCh := make(chan transport.SessionEvent, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := tp.OpenSession(ctx, transport.SessionConfig{
		Target:            ln.Addr().String(),
		User:              "ubuntu",
		KeepaliveInterval: 30 * time.Second,
		Credentials: transport.CredentialsSpec{
			Password: "any",
			HostKey: transport.HostKeyPolicy{
				AuthorizedHostKeys: []string{authorizedLine},
			},
		},
		InitialHooks: []transport.EventHook{
			func(ev transport.SessionEvent) {
				if ev.Type == transport.EventNetworkProbed {
					probedCh <- ev
				}
			},
		},
	})
	if err != nil {
		t.Fatalf("OpenSession with AuthorizedHostKeys failed: %v", err)
	}
	defer sess.Close()

	netBroadcaster.NotifyNetworkChanged("laptop_lid_opened")

	select {
	case ev := <-probedCh:
		if !strings.Contains(ev.Message, "laptop_lid_opened") {
			t.Fatalf("unexpected probe message: %q", ev.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for immediate keepalive probe on NotifyNetworkChanged")
	}
}

func TestOpenSessionOverShnexAgentRelay(t *testing.T) {
	// Start an in-process relay.Daemon to simulate remote shnex-agent daemon.
	agentLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen agent: %v", err)
	}
	daemon := relay.NewDaemon(0)
	defer daemon.Close()
	go func() { _ = daemon.Serve(agentLn) }()

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := gossh.NewSignerFromKey(priv)
	srvCfg := &gossh.ServerConfig{
		PasswordCallback: func(c gossh.ConnMetadata, pass []byte) (*gossh.Permissions, error) {
			return nil, nil
		},
	}
	srvCfg.AddHostKey(signer)

	sshLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen ssh: %v", err)
	}
	defer sshLn.Close()

	go func() {
		for {
			nc, err := sshLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, chans, reqs, err := gossh.NewServerConn(c, srvCfg)
				if err != nil {
					return
				}
				go gossh.DiscardRequests(reqs)
				for nch := range chans {
					ch, chReqs, err := nch.Accept()
					if err != nil {
						continue
					}
					go func() {
						defer ch.Close()
						for r := range chReqs {
							if r.Type == "exec" {
								if r.WantReply {
									_ = r.Reply(true, nil)
								}
								dConn, dErr := net.Dial("tcp4", agentLn.Addr().String())
								if dErr != nil {
									return
								}
								defer dConn.Close()
								go func() {
									_, _ = io.Copy(dConn, ch)
									_ = dConn.Close()
								}()
								_, _ = io.Copy(ch, dConn)
								return
							}
							if r.WantReply {
								_ = r.Reply(true, nil)
							}
						}
					}()
				}
			}(nc)
		}
	}()

	tp := NewTransport(platform.NewMemoryBridge(nil), platform.NewNetworkBroadcaster())
	relayAttachedCh := make(chan transport.SessionEvent, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := tp.OpenSession(ctx, transport.SessionConfig{
		Target: sshLn.Addr().String(),
		User:   "dev",
		Relay: transport.RelayConfig{
			Mode:        transport.RelayShnexAgent,
			SessionName: "ssh-agent-mux",
		},
		Credentials: transport.CredentialsSpec{
			Password: "pw",
			HostKey: transport.HostKeyPolicy{
				PinnedSHA256: gossh.FingerprintSHA256(signer.PublicKey()),
			},
		},
		InitialHooks: []transport.EventHook{
			func(ev transport.SessionEvent) {
				if ev.Type == transport.EventRelayAttached {
					relayAttachedCh <- ev
				}
			},
		},
	})
	if err != nil {
		t.Fatalf("OpenSession(RelayShnexAgent) failed: %v", err)
	}
	defer sess.Close()

	select {
	case ev := <-relayAttachedCh:
		if ev.RelayBackend != string(transport.RelayShnexAgent) {
			t.Fatalf("unexpected relay backend: %q", ev.RelayBackend)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected EventRelayAttached")
	}

	if err := sess.Resize(ctx, transport.TerminalSize{Cols: 100, Rows: 30}); err != nil {
		t.Fatalf("Resize over shnex-agent failed: %v", err)
	}
	if _, err := sess.Write([]byte("echo MUX_SSH_OK\r\n")); err != nil {
		t.Fatalf("Write over shnex-agent failed: %v", err)
	}

	buf := make([]byte, 4096)
	var out strings.Builder
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		n, rErr := sess.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			if strings.Contains(out.String(), "MUX_SSH_OK") {
				break
			}
		}
		if rErr != nil {
			break
		}
	}
	if !strings.Contains(out.String(), "MUX_SSH_OK") {
		t.Fatalf("did not receive MUX_SSH_OK over shnex-agent SSH channel, got: %q", out.String())
	}
}
