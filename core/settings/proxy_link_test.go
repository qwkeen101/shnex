package settings

import "testing"

func TestParseProxyLink(t *testing.T) {
	p, pw, err := ParseProxyLink("  socks5://alice:s3cret@10.0.0.2:1080#香港  ")
	if err != nil {
		t.Fatal(err)
	}
	if p.Scheme != "socks5" || p.Host != "10.0.0.2" || p.Port != 1080 || p.Username != "alice" || pw != "s3cret" || p.Name != "香港" {
		t.Fatalf("parsed wrong: %+v %q", p, pw)
	}

	p, pw, err = ParseProxyLink("http://[2001:db8::1]:7890")
	if err != nil || p.Scheme != "http" || p.Host != "2001:db8::1" || p.Port != 7890 || pw != "" || p.Name != "[2001:db8::1]:7890" {
		t.Fatalf("ipv6 http parsed wrong: %+v %v", p, err)
	}

	if p, _, err := ParseProxyLink("socks://h:1"); err != nil || p.Scheme != "socks5" {
		t.Fatalf("socks:// should map to socks5: %+v %v", p, err)
	}

	link := "trojan://pw@example.com:443?sni=example.com#日本"
	p, secret, err := ParseProxyLink(link)
	if err != nil || p.Scheme != "trojan" || p.Host != "example.com" || p.Port != 443 || p.Name != "日本" || secret != link {
		t.Fatalf("share link parsed wrong: %+v %v", p, err)
	}

	for _, bad := range []string{"vless://h:443", "wireguard://k@h:1", "socks5://h", "http://:80", "not a link", "socks5://h:99999"} {
		if _, _, err := ParseProxyLink(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
