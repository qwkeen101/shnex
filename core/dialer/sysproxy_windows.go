//go:build windows

package dialer

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

func readOSSystemProxy() (proxyServer string, proxyOverride string) {
	k, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return "", ""
	}
	defer k.Close()

	enabled, _, err := k.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return "", ""
	}

	srv, _, _ := k.GetStringValue("ProxyServer")
	ovr, _, _ := k.GetStringValue("ProxyOverride")
	return strings.TrimSpace(srv), strings.TrimSpace(ovr)
}
