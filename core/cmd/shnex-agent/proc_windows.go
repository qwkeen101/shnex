//go:build windows

package main

import "os/exec"

func configureDetachedDaemonCmd(_ *exec.Cmd) {}
