package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shnex/shnex/core/platform"
	"github.com/shnex/shnex/core/transport"
	gossh "golang.org/x/crypto/ssh"
)

type testPTYServer struct {
	addr        string
	fingerprint string
	lastCols    atomic.Uint32
	lastRows    atomic.Uint32
}

func startLocalSSHPTYServer(t *testing.T, wantUser, wantPass string) *testPTYServer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}

	srv := &testPTYServer{
		fingerprint: gossh.FingerprintSHA256(signer.PublicKey()),
	}

	cfg := &gossh.ServerConfig{
		PasswordCallback: func(c gossh.ConnMetadata, pass []byte) (*gossh.Permissions, error) {
			if c.User() == wantUser && string(pass) == wantPass {
				return nil, nil
			}
			return nil, fmt.Errorf("bad credentials")
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen ssh: %v", err)
	}
	srv.addr = ln.Addr().String()
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func(raw net.Conn) {
				defer raw.Close()
				_, chans, reqs, err := gossh.NewServerConn(raw, cfg)
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
				for newCh := range chans {
					if newCh.ChannelType() != "session" {
						_ = newCh.Reject(gossh.UnknownChannelType, "unsupported")
						continue
					}
					ch, chReqs, err := newCh.Accept()
					if err != nil {
						continue
					}
					go func() {
						defer ch.Close()
						for req := range chReqs {
							switch req.Type {
							case "pty-req":
								// Parse termLen + uint32 cols + uint32 rows
								if len(req.Payload) >= 4 {
									termLen := binary.BigEndian.Uint32(req.Payload[:4])
									off := 4 + int(termLen)
									if len(req.Payload) >= off+8 {
										srv.lastCols.Store(binary.BigEndian.Uint32(req.Payload[off : off+4]))
										srv.lastRows.Store(binary.BigEndian.Uint32(req.Payload[off+4 : off+8]))
									}
								}
								if req.WantReply {
									_ = req.Reply(true, nil)
								}
							case "window-change":
								if len(req.Payload) >= 8 {
									srv.lastCols.Store(binary.BigEndian.Uint32(req.Payload[0:4]))
									srv.lastRows.Store(binary.BigEndian.Uint32(req.Payload[4:8]))
								}
								if req.WantReply {
									_ = req.Reply(true, nil)
								}
							case "shell":
								if req.WantReply {
									_ = req.Reply(true, nil)
								}
								go func() {
									_, _ = ch.Write([]byte("WELCOME-SHNEX\r\n"))
									buf := make([]byte, 1024)
									for {
										n, rErr := ch.Read(buf)
										if n > 0 {
											in := string(buf[:n])
											if strings.Contains(in, "ring-bell") {
												_, _ = ch.Write([]byte("BELL-NOW\x07"))
											} else if strings.Contains(in, "exit") {
												_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
												_ = ch.Close()
												return
											} else {
												_, _ = ch.Write([]byte("ECHO:" + in))
											}
										}
										if rErr != nil {
											return
										}
									}
								}()
							default:
								if req.WantReply {
									_ = req.Reply(true, nil)
								}
							}
						}
					}()
				}
			}(nc)
		}
	}()
	return srv
}

func TestLoopbackSecurityRedlines(t *testing.T) {
	badAddrs := []string{"0.0.0.0:0", "0.0.0.0:8080", ":8080", "[::]:0", "192.168.1.10:0"}
	for _, bad := range badAddrs {
		if _, err := NewServer(Config{ListenAddr: bad}); !errors.Is(err, ErrNonLoopbackAddress) {
			t.Fatalf("expected ErrNonLoopbackAddress for %q, got %v", bad, err)
		}
	}
}

func TestEndToEndServerControlAndBinaryWebSocket(t *testing.T) {
	sshSrv := startLocalSSHPTYServer(t, "dev", "claude-pass")

	var bellNotified atomic.Int32
	bridge := platform.NewMemoryBridge(func(n platform.Notification) {
		if n.Category == platform.CategoryBell {
			bellNotified.Add(1)
		}
	})
	_ = bridge.SetSecret(context.Background(), "my-ssh-secret", "claude-pass")

	srv, err := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		Bridge:     bridge,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if len(srv.AuthToken()) != 64 {
		t.Fatalf("expected 64-hex-char (32-byte) authToken, got len=%d", len(srv.AuthToken()))
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	if !strings.HasPrefix(srv.Addr(), "127.0.0.1:") {
		t.Fatalf("server bound non-127.0.0.1 addr: %q", srv.Addr())
	}

	baseURL := "http://" + srv.Addr()

	// 1. Unauthenticated HTTP request must return 401.
	res401, err := http.Get(baseURL + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET health unauth: %v", err)
	}
	_ = res401.Body.Close()
	if res401.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", res401.StatusCode)
	}

	// 2. Create SSH session referencing PlatformBridge secret + PinnedSHA256 host key.
	createPayload, _ := json.Marshal(CreateSessionRequest{
		Protocol:           "ssh",
		Target:             sshSrv.addr,
		User:               "dev",
		Cols:               100,
		Rows:               32,
		KeepaliveSec:       10,
		SilenceThresholdMs: 80,
		NotifyOnBell:       true,
		NotifyOnIdle:       true,
		Auth: AuthDTO{
			Mode:         transport.AuthSecretRef,
			SecretKeyRef: "my-ssh-secret",
			HostKey: transport.HostKeyPolicy{
				PinnedSHA256: sshSrv.fingerprint,
			},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/sessions", bytes.NewReader(createPayload))
	req.Header.Set("Authorization", "Bearer "+srv.AuthToken())
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/v1/sessions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", resp.StatusCode)
	}
	var summary SessionSummary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}

	// 3. Cross-Origin WebSocket hijacking attempt MUST fail with 403.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	badHdr := http.Header{}
	badHdr.Set("Origin", "https://evil.example.com")
	wsURL := fmt.Sprintf("ws://%s/api/v1/sessions/%s/ws?token=%s", srv.Addr(), summary.ID, srv.AuthToken())
	_, badResp, badErr := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: badHdr})
	if badErr == nil {
		t.Fatal("expected cross-origin websocket dial to be rejected")
	}
	if badResp != nil && badResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden on bad Origin, got %d", badResp.StatusCode)
	}

	// 4. Legitimate WebSocket attach -> receive welcome, send binary input, resize, ping, and trigger BEL + network change.
	wsConn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer wsConn.CloseNow()

	// Send input triggering echo + standalone BEL
	if err := wsConn.Write(ctx, websocket.MessageBinary, EncodeDataFrame([]byte("ring-bell\n"))); err != nil {
		t.Fatalf("ws write data: %v", err)
	}
	// Send binary resize frame (132x44)
	if err := wsConn.Write(ctx, websocket.MessageBinary, EncodeResizeFrame(transport.TerminalSize{Cols: 132, Rows: 44})); err != nil {
		t.Fatalf("ws write resize: %v", err)
	}
	// Send binary ping frame
	pingBuf := make([]byte, 9)
	pingBuf[0] = FramePing
	binary.BigEndian.PutUint64(pingBuf[1:9], 42)
	if err := wsConn.Write(ctx, websocket.MessageBinary, pingBuf); err != nil {
		t.Fatalf("ws write ping: %v", err)
	}

	// Also trigger NotifyNetworkChanged to exercise immediate SSH keepalive probe.
	srv.NotifyNetworkChanged()

	var (
		gotBellData bool
		gotPong     bool
	)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && (!gotBellData || !gotPong) {
		rCtx, rCancel := context.WithTimeout(ctx, time.Second)
		typ, frame, rErr := wsConn.Read(rCtx)
		rCancel()
		if rErr != nil {
			t.Fatalf("ws read: %v", rErr)
		}
		if typ != websocket.MessageBinary || len(frame) == 0 {
			continue
		}
		switch frame[0] {
		case FrameData:
			if strings.Contains(string(frame[1:]), "BELL-NOW") {
				gotBellData = true
			}
		case FramePong:
			gotPong = true
		}
	}

	if !gotBellData || !gotPong {
		t.Fatalf("missing expected frames: gotBellData=%v gotPong=%v", gotBellData, gotPong)
	}

	// Verify server PTY observed binary resize 132x44 and PlatformBridge received BEL notification.
	deadlineResize := time.Now().Add(time.Second)
	for time.Now().Before(deadlineResize) {
		if sshSrv.lastCols.Load() == 132 && sshSrv.lastRows.Load() == 44 && bellNotified.Load() >= 1 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if sshSrv.lastCols.Load() != 132 || sshSrv.lastRows.Load() != 44 {
		t.Fatalf("server pty dims = %dx%d, want 132x44", sshSrv.lastCols.Load(), sshSrv.lastRows.Load())
	}
	if bellNotified.Load() < 1 {
		t.Fatalf("expected PlatformBridge bell notification, got %d", bellNotified.Load())
	}
}
