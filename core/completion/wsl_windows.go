//go:build windows

package completion

import (
	"sort"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// readRegisteredWSLDistros enumerates registered WSL distributions directly from
// HKCU\Software\Microsoft\Windows\CurrentVersion\Lxss in < 0.2ms without spawning wsl.exe.
func readRegisteredWSLDistros() []string {
	lxssKey, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Lxss`,
		registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE,
	)
	if err != nil {
		return nil
	}
	defer lxssKey.Close()

	subNames, err := lxssKey.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var distros []string
	for _, sub := range subNames {
		sk, sErr := registry.OpenKey(lxssKey, sub, registry.QUERY_VALUE)
		if sErr != nil {
			continue
		}
		name, _, vErr := sk.GetStringValue("DistributionName")
		_ = sk.Close()
		if vErr != nil {
			continue
		}
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			seen[name] = true
			distros = append(distros, name)
		}
	}
	sort.Strings(distros)
	return distros
}
