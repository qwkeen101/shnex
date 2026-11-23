//go:build !windows

package dialer

func readOSSystemProxy() (proxyServer string, proxyOverride string) {
	return "", ""
}
