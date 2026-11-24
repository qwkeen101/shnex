package dialer

// Share-link nodes (VLESS / VMess / Trojan / Shadowsocks / Hysteria2) are dialed through an
// embedded xray-core instance holding one outbound and no inbound. The config is built as
// protobuf directly instead of through xray's JSON layer (infra/conf): that layer links
// every protocol xray has, including the gVisor-based TUN / WireGuard code that would
// collide with tailscale's gVisor and bloat the mobile library.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shnex/shnex/core/sharelink"

	"github.com/xtls/xray-core/app/dispatcher"
	applog "github.com/xtls/xray-core/app/log"
	"github.com/xtls/xray-core/app/proxyman"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/common/uuid"
	xcore "github.com/xtls/xray-core/core"
	hyproxy "github.com/xtls/xray-core/proxy/hysteria"
	"github.com/xtls/xray-core/proxy/shadowsocks"
	"github.com/xtls/xray-core/proxy/shadowsocks_2022"
	"github.com/xtls/xray-core/proxy/trojan"
	"github.com/xtls/xray-core/proxy/vless"
	vlessout "github.com/xtls/xray-core/proxy/vless/outbound"
	"github.com/xtls/xray-core/proxy/vmess"
	vmessout "github.com/xtls/xray-core/proxy/vmess/outbound"
	"github.com/xtls/xray-core/transport/internet"
	"github.com/xtls/xray-core/transport/internet/finalmask/salamander"
	xgrpc "github.com/xtls/xray-core/transport/internet/grpc"
	"github.com/xtls/xray-core/transport/internet/httpupgrade"
	hytransport "github.com/xtls/xray-core/transport/internet/hysteria"
	"github.com/xtls/xray-core/transport/internet/reality"
	"github.com/xtls/xray-core/transport/internet/splithttp"
	_ "github.com/xtls/xray-core/transport/internet/tagged/taggedimpl"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
	xtls "github.com/xtls/xray-core/transport/internet/tls"
	_ "github.com/xtls/xray-core/transport/internet/udp"
	"github.com/xtls/xray-core/transport/internet/websocket"
	"google.golang.org/protobuf/proto"
)

// XrayDialer reaches targets through one share-link node.
type XrayDialer struct {
	inst *xcore.Instance
}

// One running instance per distinct link, shared by every session that uses the node.
// Instances are small (no listeners) and the pool is user sized, so they live for the
// process lifetime.
var xrayInstances = struct {
	sync.Mutex
	m map[string]*xcore.Instance
}{m: map[string]*xcore.Instance{}}

// NewXrayDialer starts (or reuses) the xray instance for a share link.
func NewXrayDialer(link string) (*XrayDialer, error) {
	sum := sha256.Sum256([]byte(strings.TrimSpace(link)))
	key := hex.EncodeToString(sum[:])

	xrayInstances.Lock()
	defer xrayInstances.Unlock()
	if inst, ok := xrayInstances.m[key]; ok {
		return &XrayDialer{inst: inst}, nil
	}
	node, err := sharelink.Parse(link)
	if err != nil {
		return nil, err
	}
	cfg, err := xrayConfig(node)
	if err != nil {
		return nil, fmt.Errorf("参数无效：%w", err)
	}
	inst, err := xcore.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("dialer: xray: %w", err)
	}
	if err := inst.Start(); err != nil {
		_ = inst.Close()
		return nil, fmt.Errorf("dialer: xray: %w", err)
	}
	xrayInstances.m[key] = inst
	return &XrayDialer{inst: inst}, nil
}

// Dial returns a stream to addr through the node. xray answers before the remote end
// is reached, so a dead node only shows when nothing comes back; RaceDialer's SSH
// banner check is what catches it.
func (d *XrayDialer) Dial(ctx context.Context, addr string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(ensureHostPort(addr, "22"))
	if err != nil {
		return nil, err
	}
	port, err := xnet.PortFromString(portStr)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The link must outlive the dial context (RaceDialer cancels it once a route wins);
	// closing the returned conn is what tears it down.
	conn, err := xcore.Dial(context.WithoutCancel(ctx), d.inst, xnet.TCPDestination(xnet.ParseAddress(host), port))
	if err != nil {
		return nil, err
	}
	return &xrayConn{Conn: conn}, nil
}

// xrayConn gives xray's pipe connection working deadlines: its own SetDeadline is a
// no-op, so a node that never answers would block the SSH banner wait and handshake
// until xray's idle timeout instead of the 15 s route timeout. A passed deadline closes
// the connection; callers here only use deadlines to bound handshakes and clear them
// afterwards, so that is the semantics they need.
type xrayConn struct {
	net.Conn
	mu    sync.Mutex
	timer *time.Timer
}

func (c *xrayConn) SetDeadline(t time.Time) error     { return c.arm(t) }
func (c *xrayConn) SetReadDeadline(t time.Time) error { return c.arm(t) }
func (c *xrayConn) SetWriteDeadline(time.Time) error  { return nil }

func (c *xrayConn) arm(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if t.IsZero() {
		return nil
	}
	c.timer = time.AfterFunc(max(time.Until(t), 0), func() { _ = c.Conn.Close() })
	return nil
}

func (c *xrayConn) Close() error {
	c.mu.Lock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.mu.Unlock()
	return c.Conn.Close()
}

func xrayConfig(n sharelink.Node) (*xcore.Config, error) {
	stream, err := xrayStream(n)
	if err != nil {
		return nil, err
	}
	proxy, err := xrayProxy(n)
	if err != nil {
		return nil, err
	}
	return &xcore.Config{
		App: []*serial.TypedMessage{
			serial.ToTypedMessage(&applog.Config{ErrorLogType: applog.LogType_None, AccessLogType: applog.LogType_None}),
			serial.ToTypedMessage(&dispatcher.Config{}),
			serial.ToTypedMessage(&proxyman.OutboundConfig{}),
		},
		Outbound: []*xcore.OutboundHandlerConfig{{
			Tag:            "node",
			SenderSettings: serial.ToTypedMessage(&proxyman.SenderConfig{StreamSettings: stream}),
			ProxySettings:  serial.ToTypedMessage(proxy),
		}},
	}, nil
}

func endpoint(n sharelink.Node, account *serial.TypedMessage) *protocol.ServerEndpoint {
	return &protocol.ServerEndpoint{
		Address: xnet.NewIPOrDomain(xnet.ParseAddress(n.Host)),
		Port:    uint32(n.Port),
		User:    &protocol.User{Account: account},
	}
}

func xrayProxy(n sharelink.Node) (proto.Message, error) {
	switch n.Protocol {
	case "vless":
		id, err := uuid.ParseString(n.ID)
		if err != nil {
			return nil, errors.New("用户 ID 无效")
		}
		return &vlessout.Config{Vnext: endpoint(n, serial.ToTypedMessage(&vless.Account{
			Id:         id.String(),
			Flow:       n.Flow,
			Encryption: "none",
		}))}, nil
	case "vmess":
		id, err := uuid.ParseString(n.ID)
		if err != nil {
			return nil, errors.New("用户 ID 无效")
		}
		return &vmessout.Config{Receiver: endpoint(n, serial.ToTypedMessage(&vmess.Account{
			Id:               id.String(),
			SecuritySettings: &protocol.SecurityConfig{Type: vmessSecurity(n.Cipher)},
		}))}, nil
	case "trojan":
		return &trojan.ClientConfig{Server: endpoint(n, serial.ToTypedMessage(&trojan.Account{Password: n.Password}))}, nil
	case "ss":
		if strings.HasPrefix(n.Cipher, "2022-") {
			return &shadowsocks_2022.ClientConfig{
				Address: xnet.NewIPOrDomain(xnet.ParseAddress(n.Host)),
				Port:    uint32(n.Port),
				Method:  n.Cipher,
				Key:     n.Password,
			}, nil
		}
		cipher := ssCipher(n.Cipher)
		if cipher == shadowsocks.CipherType_UNKNOWN {
			return nil, fmt.Errorf("不支持的加密方式 %s", n.Cipher)
		}
		return &shadowsocks.ClientConfig{Server: endpoint(n, serial.ToTypedMessage(&shadowsocks.Account{
			Password:   n.Password,
			CipherType: cipher,
		}))}, nil
	case "hysteria2":
		return &hyproxy.ClientConfig{
			Version: 2,
			Server: &protocol.ServerEndpoint{
				Address: xnet.NewIPOrDomain(xnet.ParseAddress(n.Host)),
				Port:    uint32(n.Port),
			},
		}, nil
	}
	return nil, fmt.Errorf("不支持的协议 %s", n.Protocol)
}

func vmessSecurity(s string) protocol.SecurityType {
	switch strings.ToLower(s) {
	case "aes-128-gcm":
		return protocol.SecurityType_AES128_GCM
	case "chacha20-poly1305":
		return protocol.SecurityType_CHACHA20_POLY1305
	case "none":
		return protocol.SecurityType_NONE
	case "zero":
		return protocol.SecurityType_ZERO
	}
	return protocol.SecurityType_AUTO
}

func ssCipher(m string) shadowsocks.CipherType {
	switch m {
	case "aes-128-gcm":
		return shadowsocks.CipherType_AES_128_GCM
	case "aes-256-gcm":
		return shadowsocks.CipherType_AES_256_GCM
	case "chacha20-poly1305", "chacha20-ietf-poly1305":
		return shadowsocks.CipherType_CHACHA20_POLY1305
	case "xchacha20-poly1305", "xchacha20-ietf-poly1305":
		return shadowsocks.CipherType_XCHACHA20_POLY1305
	case "none", "plain":
		return shadowsocks.CipherType_NONE
	}
	return shadowsocks.CipherType_UNKNOWN
}

func xrayStream(n sharelink.Node) (*internet.StreamConfig, error) {
	sc := &internet.StreamConfig{ProtocolName: "tcp"}
	addTransport := func(name string, settings proto.Message) {
		sc.ProtocolName = name
		sc.TransportSettings = append(sc.TransportSettings, &internet.TransportConfig{
			ProtocolName: name,
			Settings:     serial.ToTypedMessage(settings),
		})
	}
	switch n.Network {
	case "", "tcp":
	case "ws":
		path, ed := splitEarlyData(n.Path)
		addTransport("websocket", &websocket.Config{Path: path, Host: n.HostHeader, Ed: ed})
	case "httpupgrade":
		path, ed := splitEarlyData(n.Path)
		addTransport("httpupgrade", &httpupgrade.Config{Path: path, Host: n.HostHeader, Ed: ed})
	case "grpc":
		addTransport("grpc", &xgrpc.Config{ServiceName: n.ServiceName, MultiMode: n.Mode == "multi", Authority: n.HostHeader})
	case "xhttp":
		cfg, err := xhttpConfig(n)
		if err != nil {
			return nil, err
		}
		addTransport("splithttp", cfg)
	case "hysteria":
		// xray shares one hysteria client per server address across instances, so two
		// nodes on the same host:port with different auth would reuse the first one.
		addTransport("hysteria", &hytransport.Config{Version: 2, Auth: n.Password, UdpIdleTimeout: 60})
		if n.ObfsPassword != "" {
			sc.Udpmasks = append(sc.Udpmasks, serial.ToTypedMessage(&salamander.Config{Password: n.ObfsPassword}))
		}
	default:
		return nil, fmt.Errorf("不支持的传输 %s", n.Network)
	}

	switch n.Security {
	case "":
	case "tls":
		cfg := &xtls.Config{
			ServerName:   tlsServerName(n),
			NextProtocol: n.ALPN,
			Fingerprint:  n.Fingerprint,
			// xray's JSON layer dropped allowInsecure, but airports still ship self-signed
			// nodes. Here the proxy only carries SSH, which verifies the host key end to
			// end, so an unverified proxy certificate cannot expose the session.
			AllowInsecure: n.Insecure,
		}
		if xtls.GetFingerprint(cfg.Fingerprint) == nil {
			return nil, fmt.Errorf("未知的 TLS 指纹 %s", n.Fingerprint)
		}
		for _, v := range strings.Split(n.PinSHA256, ",") {
			v = strings.ReplaceAll(strings.TrimSpace(v), ":", "")
			if v == "" {
				continue
			}
			sum, err := hex.DecodeString(v)
			if err != nil || len(sum) != 32 {
				return nil, errors.New("证书指纹 (pcs / pinSHA256) 无效")
			}
			cfg.PinnedPeerCertSha256 = append(cfg.PinnedPeerCertSha256, sum)
		}
		for _, v := range strings.Split(n.VerifyName, ",") {
			if v = strings.TrimSpace(v); v != "" {
				cfg.VerifyPeerCertByName = append(cfg.VerifyPeerCertByName, v)
			}
		}
		tm := serial.ToTypedMessage(cfg)
		sc.SecuritySettings = append(sc.SecuritySettings, tm)
		sc.SecurityType = tm.Type
	case "reality":
		cfg, err := realityConfig(n)
		if err != nil {
			return nil, err
		}
		tm := serial.ToTypedMessage(cfg)
		sc.SecuritySettings = append(sc.SecuritySettings, tm)
		sc.SecurityType = tm.Type
	default:
		return nil, fmt.Errorf("不支持的安全层 %s", n.Security)
	}
	if n.Network == "hysteria" && n.Security != "tls" {
		return nil, errors.New("Hysteria2 需要 TLS")
	}
	return sc, nil
}

// tlsServerName follows xray's client priority: explicit sni, else the HTTP host of a
// ws / httpupgrade / xhttp node, else xray falls back to the server address.
func tlsServerName(n sharelink.Node) string {
	if n.SNI != "" {
		return n.SNI
	}
	if n.HostHeader != "" && net.ParseIP(n.HostHeader) == nil {
		return n.HostHeader
	}
	if n.Network == "hysteria" {
		// xray's hysteria dialer does not pass the destination to TLS, and quic-go would
		// then send the literal transport name as SNI
		return n.Host
	}
	return ""
}

// splitEarlyData pulls "?ed=2048" out of a ws / httpupgrade path, as xray's JSON layer does.
func splitEarlyData(path string) (string, uint32) {
	u, err := url.Parse(path)
	if err != nil {
		return path, 0
	}
	q := u.Query()
	if q.Get("ed") == "" {
		return path, 0
	}
	ed, _ := strconv.Atoi(q.Get("ed"))
	q.Del("ed")
	u.RawQuery = q.Encode()
	return u.String(), uint32(ed)
}

func realityConfig(n sharelink.Node) (*reality.Config, error) {
	fp := n.Fingerprint
	if fp == "" {
		fp = "chrome"
	}
	if fp == "unsafe" || fp == "hellogolang" || xtls.GetFingerprint(fp) == nil {
		return nil, fmt.Errorf("REALITY 不支持指纹 %s", fp)
	}
	pub, err := base64.RawURLEncoding.DecodeString(n.RealityPublicKey)
	if err != nil || len(pub) != 32 {
		return nil, errors.New("REALITY 公钥 (pbk) 无效")
	}
	if len(n.RealityShortID) > 16 {
		return nil, errors.New("REALITY shortId 过长")
	}
	shortID := make([]byte, 8)
	if _, err := hex.Decode(shortID, []byte(n.RealityShortID)); err != nil {
		return nil, errors.New("REALITY shortId 无效")
	}
	cfg := &reality.Config{
		Fingerprint: fp,
		ServerName:  n.SNI,
		PublicKey:   pub,
		ShortId:     shortID,
	}
	if n.RealityMldsa65 != "" {
		v, err := base64.RawURLEncoding.DecodeString(n.RealityMldsa65)
		if err != nil || len(v) != 1952 {
			return nil, errors.New("REALITY mldsa65 公钥 (pqv) 无效")
		}
		cfg.Mldsa65Verify = v
	}
	// spiderX "/path?p=1-2&c=..." -> SpiderX path + SpiderY ranges, as xray's JSON layer does.
	spx := n.RealitySpiderX
	if spx == "" {
		spx = "/"
	}
	if spx[0] != '/' {
		return nil, errors.New("REALITY spiderX 无效")
	}
	cfg.SpiderY = make([]int64, 10)
	u, _ := url.Parse(spx)
	if u == nil {
		u = &url.URL{Path: "/"}
	}
	q := u.Query()
	for i, key := range []string{"p", "c", "t", "i", "r"} {
		if v := q.Get(key); v != "" {
			lo, hi, ok := strings.Cut(v, "-")
			if !ok {
				hi = lo
			}
			cfg.SpiderY[i*2], _ = strconv.ParseInt(lo, 10, 64)
			cfg.SpiderY[i*2+1], _ = strconv.ParseInt(hi, 10, 64)
		}
		q.Del(key)
	}
	u.RawQuery = q.Encode()
	cfg.SpiderX = u.String()
	return cfg, nil
}

// xhttpConfig mirrors the defaults xray's JSON layer fills in for an XHTTP client.
func xhttpConfig(n sharelink.Node) (*splithttp.Config, error) {
	mode := n.Mode
	switch mode {
	case "":
		mode = "auto"
	case "auto", "packet-up", "stream-up", "stream-one":
	default:
		return nil, fmt.Errorf("不支持的 xhttp 模式 %s", mode)
	}
	rng := func(from, to int32) *splithttp.RangeConfig { return &splithttp.RangeConfig{From: from, To: to} }
	return &splithttp.Config{
		Host:                 n.HostHeader,
		Path:                 n.Path,
		Mode:                 mode,
		XPaddingBytes:        rng(0, 0),
		XPaddingKey:          "x_padding",
		XPaddingHeader:       "X-Padding",
		XPaddingPlacement:    "queryInHeader",
		XPaddingMethod:       "repeat-x",
		UplinkHTTPMethod:     "POST",
		SessionPlacement:     "path",
		SeqPlacement:         "path",
		UplinkDataPlacement:  splithttp.PlacementAuto,
		UplinkDataKey:        "X-Data",
		UplinkChunkSize:      rng(0, 0),
		ScMaxEachPostBytes:   rng(0, 0),
		ScMinPostsIntervalMs: rng(0, 0),
		ScStreamUpServerSecs: rng(0, 0),
		Xmux: &splithttp.XmuxConfig{
			MaxConcurrency:   rng(1, 1),
			MaxConnections:   rng(0, 0),
			CMaxReuseTimes:   rng(0, 0),
			HMaxRequestTimes: rng(600, 900),
			HMaxReusableSecs: rng(1800, 3000),
		},
	}, nil
}
