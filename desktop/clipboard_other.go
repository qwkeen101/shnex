//go:build !windows

package main

func readSystemClipboard() ClipboardReadResult {
	return ClipboardReadResult{Kind: "empty"}
}
