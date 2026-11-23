//go:build !windows

package localpty

import (
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/shnex/shnex/core/transport"
)

type unixPTY struct {
	file      *os.File
	cmd       *exec.Cmd
	closeOnce sync.Once
}

func startPlatformPTY(shellKey string, workDir string, size transport.TerminalSize, _ bool) (platformPTY, string, error) {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	switch shellKey {
	case "zsh":
		sh = "zsh"
	case "bash":
		sh = "bash"
	}

	cmd := exec.Command(sh, "-l")
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	f, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: size.Cols,
		Rows: size.Rows,
	})
	if err != nil {
		return nil, "", err
	}
	return &unixPTY{file: f, cmd: cmd}, sh, nil
}

func (u *unixPTY) Read(p []byte) (int, error)  { return u.file.Read(p) }
func (u *unixPTY) Write(p []byte) (int, error) { return u.file.Write(p) }
func (u *unixPTY) Resize(cols, rows uint16) error {
	return pty.Setsize(u.file, &pty.Winsize{Cols: cols, Rows: rows})
}
func (u *unixPTY) Wait() error { return u.cmd.Wait() }
func (u *unixPTY) Close() error {
	var err error
	u.closeOnce.Do(func() {
		if u.cmd.Process != nil {
			_ = u.cmd.Process.Kill()
		}
		err = u.file.Close()
	})
	return err
}
