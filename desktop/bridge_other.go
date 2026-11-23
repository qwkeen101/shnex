//go:build !windows

package main

import "github.com/shnex/shnex/core/platform"

func newPlatformBridge(onNotify func(platform.Notification)) platform.PlatformBridge {
	return platform.NewMemoryBridge(onNotify)
}
