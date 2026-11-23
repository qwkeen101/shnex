package dialer

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

func startEchoServer(t *testing.T, greeting string) string {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				if greeting != "" {
					_, _ = conn.Write([]byte(greeting))
				}
				_, _ = io.Copy(conn, conn)
			}(c)
		}
	}()
	return ln.Addr().String()
}

func TestHTTPProxyDialerPreservesEarlyBanner(t *testing.T) {
	targetAddr := startEchoServer(t, "SSH-2.0-EarlyBanner\r\n")

	proxyLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen http proxy: %v", err)
	}
	defer proxyLn.Close()

	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:wonderland"))

	go func() {
		for {
			c, err := proxyLn.Accept()
			if err != nil {
				return
			}
			go func(clientConn net.Conn) {
				defer clientConn.Close()
				br := bufio.NewReader(clientConn)
				req, err := http.ReadRequest(br)
				if err != nil || req.Method != http.MethodConnect {
					return
				}
				if req.Header.Get("Proxy-Authorization") != expectedAuth {
					_, _ = clientConn.Write([]byte("HTTP/1.1 407 Proxy Auth Required\r\n\r\n"))
					return
				}
				upConn, err := net.Dial("tcp4", req.Host)
				if err != nil {
					_, _ = clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
					return
				}
				defer upConn.Close()

				// Read early banner from target and coalesce into same write right after 200 OK
				// to verify HTTPProxyDialer's bufferedConn never drops early SSH bytes.
				early := make([]byte, 21)
				_, _ = io.ReadFull(upConn, early)
				_, _ = clientConn.Write(append([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"), early...))

				go func() { _, _ = io.Copy(upConn, clientConn) }()
				_, _ = io.Copy(clientConn, upConn)
			}(c)
		}
	}()

	d, err := NewProxyDialerFromURL(fmt.Sprintf("http://alice:wonderland@%s", proxyLn.Addr().String()), nil)
	if err != nil {
		t.Fatalf("NewProxyDialerFromURL: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := d.Dial(ctx, targetAddr)
	if err != nil {
		t.Fatalf("HTTPProxyDialer.Dial: %v", err)
	}
	defer conn.Close()

	banner := make([]byte, 21)
	if _, err := io.ReadFull(conn, banner); err != nil {
		t.Fatalf("read early banner: %v", err)
	}
	if string(banner) != "SSH-2.0-EarlyBanner\r\n" {
		t.Fatalf("got banner %q", string(banner))
	}
}

func TestSOCKS5DialerWithAuth(t *testing.T) {
	targetAddr := startEchoServer(t, "")

	socksLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen socks5: %v", err)
	}
	defer socksLn.Close()

	go func() {
		for {
			c, err := socksLn.Accept()
			if err != nil {
				return
			}
			go handleMinimalSOCKS5(c, "proxyuser", "proxypass")
		}
	}()

	d, err := NewProxyDialerFromURL(fmt.Sprintf("socks5://proxyuser:proxypass@%s", socksLn.Addr().String()), nil)
	if err != nil {
		t.Fatalf("NewProxyDialerFromURL socks5: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := d.Dial(ctx, targetAddr)
	if err != nil {
		t.Fatalf("SOCKS5 Dial failed: %v", err)
	}
	defer conn.Close()

	msg := "hello-over-socks5"
	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}
	reply := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(reply) != msg {
		t.Fatalf("echo mismatch: got %q, want %q", string(reply), msg)
	}
}

func handleMinimalSOCKS5(c net.Conn, wantUser, wantPass string) {
	defer c.Close()
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil || hdr[0] != 0x05 {
		return
	}
	methods := make([]byte, int(hdr[1]))
	if _, err := io.ReadFull(c, methods); err != nil {
		return
	}
	// Require RFC 1929 Username/Password auth (0x02)
	_, _ = c.Write([]byte{0x05, 0x02})

	authVer := make([]byte, 2)
	if _, err := io.ReadFull(c, authVer); err != nil || authVer[0] != 0x01 {
		return
	}
	uBytes := make([]byte, int(authVer[1]))
	_, _ = io.ReadFull(c, uBytes)
	pLen := make([]byte, 1)
	_, _ = io.ReadFull(c, pLen)
	pBytes := make([]byte, int(pLen[0]))
	_, _ = io.ReadFull(c, pBytes)

	if string(uBytes) != wantUser || string(pBytes) != wantPass {
		_, _ = c.Write([]byte{0x01, 0x01})
		return
	}
	_, _ = c.Write([]byte{0x01, 0x00})

	// Connect request: VER CMD RSV ATYP ...
	reqHdr := make([]byte, 4)
	if _, err := io.ReadFull(c, reqHdr); err != nil || reqHdr[1] != 0x01 {
		return
	}
	var host string
	switch reqHdr[3] {
	case 0x01: // IPv4
		ip := make([]byte, 4)
		_, _ = io.ReadFull(c, ip)
		host = net.IP(ip).String()
	case 0x03: // Domain
		dl := make([]byte, 1)
		_, _ = io.ReadFull(c, dl)
		dom := make([]byte, int(dl[0]))
		_, _ = io.ReadFull(c, dom)
		host = string(dom)
	default:
		return
	}
	portBuf := make([]byte, 2)
	_, _ = io.ReadFull(c, portBuf)
	port := binary.BigEndian.Uint16(portBuf)

	up, err := net.Dial("tcp4", net.JoinHostPort(host, strconv.Itoa(int(port))))
	if err != nil {
		_, _ = c.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer up.Close()

	_, _ = c.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0})
	go func() { _, _ = io.Copy(up, c) }()
	_, _ = io.Copy(c, up)
}

func startBastionSSHServer(t *testing.T, wantUser, wantPass string) (string, gossh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519 keygen: %v", err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}

	cfg := &gossh.ServerConfig{
		PasswordCallback: func(c gossh.ConnMetadata, pass []byte) (*gossh.Permissions, error) {
			if c.User() == wantUser && string(pass) == wantPass {
				return nil, nil
			}
			return nil, fmt.Errorf("unauthorized")
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen bastion: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			go func(nc net.Conn) {
				defer nc.Close()
				_, chans, reqs, err := gossh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go func() {
					for req := range reqs {
						if req.WantReply {
							_ = req.Reply(true, nil)
						}
					}
				}()
				for newCh := range chans {
					if newCh.ChannelType() != "direct-tcpip" {
						_ = newCh.Reject(gossh.UnknownChannelType, "only direct-tcpip supported")
						continue
					}
					var payload struct {
						Host       string
						Port       uint32
						OriginHost string
						OriginPort uint32
					}
					if err := gossh.Unmarshal(newCh.ExtraData(), &payload); err != nil {
						_ = newCh.Reject(gossh.ConnectionFailed, err.Error())
						continue
					}
					targetConn, err := net.Dial("tcp4", net.JoinHostPort(payload.Host, strconv.Itoa(int(payload.Port))))
					if err != nil {
						_ = newCh.Reject(gossh.ConnectionFailed, err.Error())
						continue
					}
					ch, chReqs, err := newCh.Accept()
					if err != nil {
						_ = targetConn.Close()
						continue
					}
					go gossh.DiscardRequests(chReqs)
					go func() {
						defer ch.Close()
						defer targetConn.Close()
						_, _ = io.Copy(ch, targetConn)
					}()
					go func() {
						defer ch.Close()
						defer targetConn.Close()
						_, _ = io.Copy(targetConn, ch)
					}()
				}
			}(raw)
		}
	}()

	return ln.Addr().String(), signer.PublicKey()
}

func TestMultiHopSSHJumpChain(t *testing.T) {
	targetAddr := startEchoServer(t, "")
	jump1Addr, jump1Pub := startBastionSSHServer(t, "bastion1", "pw1")
	jump2Addr, jump2Pub := startBastionSSHServer(t, "bastion2", "pw2")

	chain, closer, err := NewSSHJumpChain(
		nil,
		JumpHostConfig{
			Addr:            jump1Addr,
			User:            "bastion1",
			Password:        "pw1",
			HostKeyCallback: gossh.FixedHostKey(jump1Pub),
		},
		JumpHostConfig{
			Addr:            jump2Addr,
			User:            "bastion2",
			Password:        "pw2",
			HostKeyCallback: gossh.FixedHostKey(jump2Pub),
		},
	)
	if err != nil {
		t.Fatalf("NewSSHJumpChain: %v", err)
	}
	defer closer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := chain.Dial(ctx, targetAddr)
	if err != nil {
		t.Fatalf("2-hop SSH jump Dial failed: %v", err)
	}
	defer conn.Close()

	payload := "claude-cli-over-two-bastions"
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("got %q, want %q", string(got), payload)
	}
}

type stubTSNode struct {
	lastAddr string
}

func (s *stubTSNode) Dial(_ context.Context, network, addr string) (net.Conn, error) {
	if !strings.HasPrefix(network, "tcp") {
		return nil, fmt.Errorf("unexpected network %s", network)
	}
	s.lastAddr = addr
	c1, c2 := net.Pipe()
	_ = c2.Close()
	return c1, nil
}

func TestTSNetDialer(t *testing.T) {
	node := &stubTSNode{}
	d, err := NewTSNetDialer(node)
	if err != nil {
		t.Fatalf("NewTSNetDialer: %v", err)
	}
	conn, err := d.Dial(context.Background(), "gpu.tailnet.ts.net:22")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = conn.Close()
	if node.lastAddr != "gpu.tailnet.ts.net:22" {
		t.Fatalf("got %q", node.lastAddr)
	}
}

func TestParseProxyServerSpecAndBypass(t *testing.T) {
	cands := parseProxyServerSpec("127.0.0.1:10808")
	if len(cands) != 2 || cands[0] != "http://127.0.0.1:10808" || cands[1] != "socks5://127.0.0.1:10808" {
		t.Fatalf("unexpected bare spec candidates: %v", cands)
	}

	multi := parseProxyServerSpec("http=127.0.0.1:10809;https=127.0.0.1:10809;socks=127.0.0.1:10808")
	if len(multi) != 2 || multi[0] != "http://127.0.0.1:10809" || multi[1] != "socks5://127.0.0.1:10808" {
		t.Fatalf("unexpected multi spec candidates: %v", multi)
	}

	for _, priv := range []string{"127.0.0.1", "192.168.1.50", "10.0.0.8", "100.101.102.103", "localhost", "box.local", "dev.ts.net"} {
		if !isBypassedPrivateOrLocalHost(priv) {
			t.Fatalf("expected %q to bypass proxy", priv)
		}
	}
	if isBypassedPrivateOrLocalHost("108.137.19.108") {
		t.Fatalf("public IP 108.137.19.108 must not bypass proxy")
	}
}

func TestSmartDirectDialerSystemProxyFallback(t *testing.T) {
	realBackend := startEchoServer(t, "SSH-2.0-ViaSystemProxy\r\n")

	proxyLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen http proxy: %v", err)
	}
	defer proxyLn.Close()

	go func() {
		for {
			c, err := proxyLn.Accept()
			if err != nil {
				return
			}
			go func(clientConn net.Conn) {
				defer clientConn.Close()
				br := bufio.NewReader(clientConn)
				req, err := http.ReadRequest(br)
				if err != nil || req.Method != http.MethodConnect {
					return
				}
				// Route synthetic unreachable public target 203.0.113.99:22 to local echo server
				upConn, err := net.Dial("tcp4", realBackend)
				if err != nil {
					_, _ = clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
					return
				}
				defer upConn.Close()
				_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
				go func() { _, _ = io.Copy(upConn, clientConn) }()
				_, _ = io.Copy(clientConn, upConn)
			}(c)
		}
	}()

	t.Setenv("ALL_PROXY", proxyLn.Addr().String())
	t.Setenv("NO_PROXY", "")

	sd := NewSmartDirectDialer(4*time.Second, 0)
	sd.StaggerDelay = 60 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	// 203.0.113.0/24 is TEST-NET-3 (public non-private documentation block that drops/fails direct TCP)
	conn, err := sd.Dial(ctx, "203.0.113.99:22")
	if err != nil {
		t.Fatalf("SmartDirectDialer.Dial failed to fallback to system proxy: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, len("SSH-2.0-ViaSystemProxy\r\n"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read banner via proxy fallback: %v", err)
	}
	if string(buf) != "SSH-2.0-ViaSystemProxy\r\n" {
		t.Fatalf("unexpected banner: %q", string(buf))
	}
}
