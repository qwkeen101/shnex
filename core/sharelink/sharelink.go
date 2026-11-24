// Package sharelink parses the proxy share links airports hand out (vless://, vmess://,
// trojan://, ss://, hysteria2:// / hy2://) and subscription bodies that carry them.
// It has no xray dependency so it stays cheap to test; core/dialer turns a Node into
// an xray outbound.
package sharelink

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Node is one parsed share link, flattened to what the dialer needs.
type Node struct {
	Name     string
	Protocol string // "vless" | "vmess" | "trojan" | "ss" | "hysteria2"
	Host     string
	Port     int

	// credentials
	ID         string // vless / vmess uuid
	Flow       string // vless
	Encryption string // vless ("none")
	Cipher     string // vmess security or ss method
	Password   string // trojan / ss / hysteria2 auth

	// transport: "tcp" | "ws" | "grpc" | "httpupgrade" | "xhttp" | "hysteria"
	Network     string
	Path        string
	HostHeader  string
	ServiceName string
	Mode        string // grpc "multi" / xhttp mode

	// security: "" | "tls" | "reality"
	Security    string
	SNI         string
	Fingerprint string
	ALPN        []string
	PinSHA256   string // comma separated hex sha256 of the peer certificate
	VerifyName  string
	Insecure    bool // insecure=1 / allowInsecure=1: skip proxy certificate checks

	RealityPublicKey string
	RealityShortID   string
	RealitySpiderX   string
	RealityMldsa65   string

	ObfsPassword string // hysteria2 salamander
}

var protocols = map[string]string{
	"vless":     "vless",
	"vmess":     "vmess",
	"trojan":    "trojan",
	"ss":        "ss",
	"hysteria2": "hysteria2",
	"hy2":       "hysteria2",
}

// IsShareLink reports whether s starts with a share-link scheme this package parses.
func IsShareLink(s string) bool {
	i := strings.Index(s, "://")
	if i <= 0 {
		return false
	}
	_, ok := protocols[strings.ToLower(strings.TrimSpace(s[:i]))]
	return ok
}

// IsProtocol reports whether a pool entry scheme is a share-link protocol.
func IsProtocol(p string) bool {
	switch p {
	case "vless", "vmess", "trojan", "ss", "hysteria2":
		return true
	}
	return false
}

// Parse parses one share link. Error messages are user facing and never echo the link.
func Parse(link string) (Node, error) {
	link = strings.TrimSpace(link)
	i := strings.Index(link, "://")
	if i <= 0 {
		return Node{}, errors.New("不是有效的链接")
	}
	proto, ok := protocols[strings.ToLower(link[:i])]
	if !ok {
		return Node{}, fmt.Errorf("暂不支持 %s:// 链接", link[:i])
	}
	var (
		n   Node
		err error
	)
	switch proto {
	case "vless":
		n, err = parseVLESS(link)
	case "trojan":
		n, err = parseTrojan(link)
	case "vmess":
		n, err = parseVMess(link)
	case "ss":
		n, err = parseSS(link)
	case "hysteria2":
		n, err = parseHysteria2(link)
	}
	if err != nil {
		return Node{}, err
	}
	n.Protocol = proto
	if n.Host == "" || n.Port <= 0 || n.Port > 65535 {
		return Node{}, errors.New("缺少地址或端口")
	}
	if n.Name == "" {
		n.Name = net.JoinHostPort(n.Host, strconv.Itoa(n.Port))
	}
	return n, nil
}

// parseURL cuts the name off first: names such as "#100%流量" are not valid URL escapes.
func parseURL(link string) (*url.URL, int, string, error) {
	link, name := cutName(link)
	u, err := url.Parse(link)
	if err != nil {
		return nil, 0, "", errors.New("不是有效的链接")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return nil, 0, "", errors.New("缺少地址或端口")
	}
	return u, port, name, nil
}

func cutName(link string) (string, string) {
	body, frag, ok := strings.Cut(link, "#")
	if !ok {
		return link, ""
	}
	if name, err := url.PathUnescape(frag); err == nil {
		return body, strings.TrimSpace(name)
	}
	return body, strings.TrimSpace(frag)
}

func parseVLESS(link string) (Node, error) {
	u, port, name, err := parseURL(link)
	if err != nil {
		return Node{}, err
	}
	q := u.Query()
	n := Node{
		Name:       name,
		Host:       u.Hostname(),
		Port:       port,
		ID:         u.User.Username(),
		Flow:       q.Get("flow"),
		Encryption: q.Get("encryption"),
	}
	if n.ID == "" {
		return Node{}, errors.New("缺少用户 ID")
	}
	if n.Encryption == "" {
		n.Encryption = "none"
	}
	if n.Encryption != "none" {
		return Node{}, errors.New("暂不支持开启 VLESS Encryption 的节点")
	}
	switch n.Flow {
	case "", "xtls-rprx-vision", "xtls-rprx-vision-udp443":
	default:
		return Node{}, fmt.Errorf("暂不支持 flow=%s", n.Flow)
	}
	err = applyStream(&n, q, "none")
	return n, err
}

func parseTrojan(link string) (Node, error) {
	u, port, name, err := parseURL(link)
	if err != nil {
		return Node{}, err
	}
	q := u.Query()
	n := Node{Name: name, Host: u.Hostname(), Port: port, Password: u.User.Username()}
	if n.Password == "" {
		return Node{}, errors.New("缺少密码")
	}
	err = applyStream(&n, q, "tls")
	return n, err
}

// applyStream reads the transport / TLS parameters shared by vless:// and trojan://.
func applyStream(n *Node, q url.Values, defaultSecurity string) error {
	n.Network = strings.ToLower(q.Get("type"))
	switch n.Network {
	case "", "tcp", "raw":
		n.Network = "tcp"
	case "ws", "websocket":
		n.Network = "ws"
	case "splithttp", "xhttp":
		n.Network = "xhttp"
	case "grpc", "httpupgrade":
	case "h2", "http":
		return errors.New("xray 已不再支持 h2 传输")
	default:
		return fmt.Errorf("暂不支持 %s 传输", n.Network)
	}
	if n.Network == "tcp" && strings.EqualFold(q.Get("headerType"), "http") {
		return errors.New("暂不支持 HTTP 伪装的 TCP 节点")
	}
	n.Path = q.Get("path")
	n.HostHeader = firstNonEmpty(q.Get("host"), q.Get("authority"))
	n.ServiceName = q.Get("serviceName")
	n.Mode = q.Get("mode")
	if n.Network == "xhttp" && strings.TrimSpace(q.Get("extra")) != "" {
		return errors.New("暂不支持带 extra 参数的 xhttp 节点")
	}

	sec := strings.ToLower(q.Get("security"))
	if sec == "" {
		sec = defaultSecurity
	}
	switch sec {
	case "none":
		sec = ""
	case "tls", "reality":
	case "xtls":
		return errors.New("xray 已不再支持旧版 XTLS")
	default:
		return fmt.Errorf("暂不支持 security=%s", sec)
	}
	n.Security = sec
	n.SNI = firstNonEmpty(q.Get("sni"), q.Get("peer"))
	n.Fingerprint = strings.ToLower(q.Get("fp"))
	n.ALPN = splitList(q.Get("alpn"))
	n.PinSHA256 = q.Get("pcs")
	n.VerifyName = q.Get("vcn")
	n.Insecure = isTrue(q.Get("allowInsecure")) || isTrue(q.Get("insecure"))
	if sec == "reality" {
		n.RealityPublicKey = firstNonEmpty(q.Get("pbk"), q.Get("password"))
		n.RealityShortID = q.Get("sid")
		n.RealitySpiderX = q.Get("spx")
		n.RealityMldsa65 = q.Get("pqv")
		if n.RealityPublicKey == "" {
			return errors.New("REALITY 节点缺少公钥 (pbk)")
		}
		switch n.Network {
		case "tcp", "xhttp", "grpc":
		default:
			return errors.New("REALITY 只支持 tcp / xhttp / grpc 传输")
		}
	}
	return nil
}

// parseVMess handles the common base64(JSON) form and the newer URL form.
func parseVMess(link string) (Node, error) {
	body := strings.SplitN(link[len("vmess://"):], "#", 2)[0]
	if strings.Contains(body, "@") { // base64 never contains '@'
		return parseVMessURL(link)
	}
	raw, err := decodeBase64(body)
	if err != nil {
		return Node{}, errors.New("vmess 链接内容无法解码")
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		return Node{}, errors.New("vmess 链接内容无法解析")
	}
	str := func(k string) string {
		switch x := v[k].(type) {
		case string:
			return strings.TrimSpace(x)
		case float64:
			return strconv.FormatFloat(x, 'f', -1, 64)
		case bool:
			if x {
				return "1"
			}
		}
		return ""
	}
	port, _ := strconv.Atoi(str("port"))
	n := Node{
		Name:   str("ps"),
		Host:   str("add"),
		Port:   port,
		ID:     str("id"),
		Cipher: firstNonEmpty(str("scy"), "auto"),
	}
	if n.ID == "" {
		return Node{}, errors.New("缺少用户 ID")
	}
	q := url.Values{}
	q.Set("type", str("net"))
	q.Set("headerType", str("type"))
	q.Set("host", str("host"))
	q.Set("path", str("path"))
	switch str("net") {
	case "grpc":
		q.Set("serviceName", str("path"))
		q.Set("mode", str("type"))
		q.Del("headerType")
	case "xhttp", "splithttp":
		q.Set("mode", str("type"))
		q.Del("headerType")
	}
	q.Set("security", firstNonEmpty(str("tls"), "none"))
	q.Set("sni", str("sni"))
	q.Set("alpn", str("alpn"))
	q.Set("fp", str("fp"))
	q.Set("allowInsecure", str("allowInsecure"))
	err = applyStream(&n, q, "none")
	return n, err
}

func parseVMessURL(link string) (Node, error) {
	u, port, name, err := parseURL(link)
	if err != nil {
		return Node{}, err
	}
	q := u.Query()
	n := Node{
		Name:   name,
		Host:   u.Hostname(),
		Port:   port,
		ID:     u.User.Username(),
		Cipher: firstNonEmpty(q.Get("encryption"), "auto"),
	}
	if n.ID == "" {
		return Node{}, errors.New("缺少用户 ID")
	}
	err = applyStream(&n, q, "none")
	return n, err
}

// parseSS handles SIP002 (ss://base64(method:pass)@host:port#name, userinfo may also be
// percent-encoded) and the legacy ss://base64(method:pass@host:port)#name.
func parseSS(link string) (Node, error) {
	body, name := cutName(link[len("ss://"):])
	if b, query, ok := strings.Cut(body, "?"); ok {
		body = b
		q, _ := url.ParseQuery(query)
		if q.Get("plugin") != "" {
			return Node{}, errors.New("暂不支持带 plugin 的 ss 节点")
		}
		if t := strings.ToLower(q.Get("type")); t != "" && t != "tcp" && t != "raw" {
			return Node{}, errors.New("暂不支持带传输层的 ss 节点")
		}
		if s := strings.ToLower(q.Get("security")); s != "" && s != "none" {
			return Node{}, errors.New("暂不支持带 TLS 的 ss 节点")
		}
	}
	body = strings.TrimSuffix(body, "/")
	var userinfo, hostport string
	if at := strings.LastIndex(body, "@"); at >= 0 {
		userinfo, hostport = body[:at], body[at+1:]
		if un, err := url.PathUnescape(userinfo); err == nil {
			userinfo = un // plain "method:pass", or base64 with escaped "=" padding
		}
		if dec, err := decodeBase64(userinfo); err == nil && strings.Contains(string(dec), ":") {
			userinfo = string(dec)
		}
	} else {
		dec, err := decodeBase64(body)
		if err != nil {
			return Node{}, errors.New("ss 链接内容无法解码")
		}
		at := strings.LastIndex(string(dec), "@")
		if at < 0 {
			return Node{}, errors.New("ss 链接内容无法解析")
		}
		userinfo, hostport = string(dec[:at]), string(dec[at+1:])
	}
	method, password, ok := strings.Cut(userinfo, ":")
	if !ok || method == "" || password == "" {
		return Node{}, errors.New("ss 链接缺少加密方式或密码")
	}
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return Node{}, errors.New("缺少地址或端口")
	}
	port, _ := strconv.Atoi(portStr)
	method = strings.ToLower(method)
	if !SupportedSSMethod(method) {
		return Node{}, fmt.Errorf("暂不支持 ss 加密方式 %s", method)
	}
	return Node{Name: name, Host: host, Port: port, Cipher: method, Password: password, Network: "tcp"}, nil
}

// SupportedSSMethod lists the Shadowsocks ciphers xray-core still implements.
func SupportedSSMethod(m string) bool {
	switch m {
	case "aes-128-gcm", "aes-256-gcm", "chacha20-poly1305", "chacha20-ietf-poly1305",
		"xchacha20-poly1305", "xchacha20-ietf-poly1305", "none", "plain",
		"2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		return true
	}
	return false
}

// parseHysteria2 parses by hand: port-hopping links ("443,20000-30000") are not valid
// URLs for net/url. Only the first port is used.
func parseHysteria2(link string) (Node, error) {
	body, name := cutName(link[strings.Index(link, "://")+3:])
	q := url.Values{}
	if b, query, ok := strings.Cut(body, "?"); ok {
		body = b
		q, _ = url.ParseQuery(query)
	}
	body = strings.TrimSuffix(body, "/")
	auth := ""
	if at := strings.LastIndex(body, "@"); at >= 0 {
		auth, _ = url.PathUnescape(body[:at])
		body = body[at+1:]
	}
	colon := strings.LastIndex(body, ":")
	if colon < 0 {
		return Node{}, errors.New("缺少地址或端口")
	}
	host := strings.Trim(body[:colon], "[]")
	portSpec := body[colon+1:]
	if i := strings.IndexAny(portSpec, ",-"); i >= 0 {
		portSpec = portSpec[:i]
	}
	port, err := strconv.Atoi(portSpec)
	if err != nil {
		return Node{}, errors.New("缺少地址或端口")
	}
	n := Node{
		Name:        name,
		Host:        host,
		Port:        port,
		Password:    auth,
		Network:     "hysteria",
		Security:    "tls",
		SNI:         q.Get("sni"),
		Fingerprint: strings.ToLower(q.Get("fp")),
		ALPN:        splitList(q.Get("alpn")),
		PinSHA256:   q.Get("pinSHA256"),
		Insecure:    isTrue(q.Get("insecure")),
	}
	switch strings.ToLower(q.Get("obfs")) {
	case "":
	case "salamander":
		n.ObfsPassword = q.Get("obfs-password")
	default:
		return Node{}, fmt.Errorf("暂不支持 obfs=%s", q.Get("obfs"))
	}
	if len(n.ALPN) == 0 {
		n.ALPN = []string{"h3"}
	}
	return n, nil
}

// DecodeSubscription returns the share links inside a subscription body: plain lines
// or a base64 blob of lines (the v2rayN format airports serve).
func DecodeSubscription(body []byte) ([]string, error) {
	text := strings.TrimSpace(string(body))
	if !strings.Contains(text, "://") {
		compact := strings.Join(strings.Fields(text), "")
		dec, err := decodeBase64(compact)
		if err != nil || !strings.Contains(string(dec), "://") {
			if strings.Contains(text, "proxies:") {
				return nil, errors.New("订阅返回的是 Clash 配置，请在机场后台复制「V2Ray / 通用」订阅链接")
			}
			return nil, errors.New("订阅内容无法识别")
		}
		text = string(dec)
	}
	var links []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if looksLikeNodeLink(line) {
			links = append(links, line)
		}
	}
	if len(links) == 0 {
		return nil, errors.New("订阅内容无法识别")
	}
	return links, nil
}

// looksLikeNodeLink keeps "scheme://..." lines (supported or not, so unsupported ones
// can be counted) and drops page noise such as HTML or http(s) URLs.
func looksLikeNodeLink(line string) bool {
	i := strings.Index(line, "://")
	if i <= 0 || i > 12 {
		return false
	}
	scheme := strings.ToLower(line[:i])
	if scheme == "http" || scheme == "https" {
		return false
	}
	for _, r := range scheme {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func isTrue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("not base64")
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
