package sharelink

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestParseVLESSReality(t *testing.T) {
	n, err := Parse("vless://0b4a4c0e-1111-2222-3333-444455556666@1.2.3.4:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.example.com&fp=chrome&pbk=AbCdEfGhIjKlMnOpQrStUvWxYz0123456789abcdefg&sid=6ba85179e30d4fc2&type=tcp#%E9%A6%99%E6%B8%AF")
	if err != nil {
		t.Fatal(err)
	}
	if n.Protocol != "vless" || n.Host != "1.2.3.4" || n.Port != 443 || n.Name != "香港" {
		t.Fatalf("basic fields: %+v", n)
	}
	if n.Security != "reality" || n.SNI != "www.example.com" || n.RealityShortID != "6ba85179e30d4fc2" || n.Flow != "xtls-rprx-vision" || n.Network != "tcp" {
		t.Fatalf("stream fields: %+v", n)
	}
}

func TestParseTrojanDefaultsToTLS(t *testing.T) {
	n, err := Parse("trojan://p%40ss@example.com:443?type=ws&path=%2Fws&host=cdn.example.com#JP")
	if err != nil {
		t.Fatal(err)
	}
	if n.Password != "p@ss" || n.Security != "tls" || n.Network != "ws" || n.Path != "/ws" || n.HostHeader != "cdn.example.com" {
		t.Fatalf("%+v", n)
	}
}

func TestParseVMessJSON(t *testing.T) {
	js := `{"v":"2","ps":"美国","add":"us.example.com","port":"8443","id":"0b4a4c0e-1111-2222-3333-444455556666","aid":"0","scy":"auto","net":"ws","type":"none","host":"us.example.com","path":"/v","tls":"tls","sni":"us.example.com"}`
	n, err := Parse("vmess://" + base64.StdEncoding.EncodeToString([]byte(js)))
	if err != nil {
		t.Fatal(err)
	}
	if n.Name != "美国" || n.Port != 8443 || n.Network != "ws" || n.Security != "tls" || n.Path != "/v" {
		t.Fatalf("%+v", n)
	}
}

func TestParseSSForms(t *testing.T) {
	userinfo := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:secret"))
	for _, link := range []string{
		"ss://" + userinfo + "@5.6.7.8:8388#A",
		"ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:secret@5.6.7.8:8388")) + "#A",
		"ss://2022-blake3-aes-256-gcm:a%2Bb%3D@5.6.7.8:8388#A",
	} {
		n, err := Parse(link)
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}
		if n.Host != "5.6.7.8" || n.Port != 8388 || n.Name != "A" || n.Password == "" {
			t.Fatalf("%s: %+v", link, n)
		}
	}
	if _, err := Parse("ss://" + userinfo + "@5.6.7.8:8388/?plugin=obfs-local#A"); err == nil {
		t.Fatal("plugin nodes must be refused")
	}
}

func TestParseHysteria2PortHopping(t *testing.T) {
	n, err := Parse("hy2://pa%3Ass@hy.example.com:443,20000-30000/?sni=hy.example.com&obfs=salamander&obfs-password=xyz#HY")
	if err != nil {
		t.Fatal(err)
	}
	if n.Protocol != "hysteria2" || n.Port != 443 || n.Password != "pa:ss" || n.ObfsPassword != "xyz" || n.ALPN[0] != "h3" {
		t.Fatalf("%+v", n)
	}
}

func TestParseErrorsDoNotEchoLink(t *testing.T) {
	for _, link := range []string{
		"vless://secret-id@host:1?type=kcp",
		"wireguard://secret@host:1",
		"trojan://@host:1",
	} {
		_, err := Parse(link)
		if err == nil {
			t.Fatalf("%s: want error", link)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatalf("error leaks the link: %v", err)
		}
	}
}

func TestDecodeSubscription(t *testing.T) {
	plain := "vless://a@h:1#x\r\ntrojan://b@h:2#y\n"
	for _, body := range []string{plain, base64.StdEncoding.EncodeToString([]byte(plain))} {
		links, err := DecodeSubscription([]byte(body))
		if err != nil || len(links) != 2 {
			t.Fatalf("%q: %v %v", body, links, err)
		}
	}
	if _, err := DecodeSubscription([]byte("proxies:\n  - name: a")); err == nil || !strings.Contains(err.Error(), "Clash") {
		t.Fatalf("clash yaml: %v", err)
	}
}

func TestParseRealWorldQuirks(t *testing.T) {
	n, err := Parse("trojan://pw@h.example.com:443?allowInsecure=1&type=grpc&serviceName=s&authority=a.example.com#100%流量")
	if err != nil {
		t.Fatal(err)
	}
	if n.Name != "100%流量" || !n.Insecure || n.HostHeader != "a.example.com" || n.ServiceName != "s" {
		t.Fatalf("%+v", n)
	}
	if _, err := Parse("vless://0b4a4c0e-1111-2222-3333-444455556666@h:443?type=xhttp&extra=%7B%7D&security=tls"); err == nil {
		t.Fatal("xhttp extra must be refused, not silently dropped")
	}
	if _, err := Parse("ss://aes-256-gcm:pw@h:8388?type=ws#x"); err == nil {
		t.Fatal("ss with a transport must be refused")
	}
	padded := strings.ReplaceAll(base64.StdEncoding.EncodeToString([]byte("aes-128-gcm:p")), "=", "%3D")
	if n, err := Parse("ss://" + padded + "@h:8388#x"); err != nil || n.Cipher != "aes-128-gcm" || n.Password != "p" {
		t.Fatalf("escaped base64 padding: %+v %v", n, err)
	}
}

func TestDecodeSubscriptionRejectsPages(t *testing.T) {
	if _, err := DecodeSubscription([]byte("<html><a href=\"https://x/y\">login</a></html>")); err == nil {
		t.Fatal("an HTML page is not a subscription")
	}
	links, err := DecodeSubscription([]byte("tuic://u@h:1#t\nvless://a@h:1#v"))
	if err != nil || len(links) != 2 {
		t.Fatalf("unsupported schemes must still be counted: %v %v", links, err)
	}
}

func TestIsShareLink(t *testing.T) {
	if !IsShareLink("HY2://x@h:1") || IsShareLink("socks5://h:1") || IsShareLink("https://sub.example.com/x") {
		t.Fatal("IsShareLink")
	}
}
