//go:build unix

package relay

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type unixPTYHandle struct {
	file *os.File
	cmd  *exec.Cmd
}

func startLocalProcess(command, term string, cols, rows uint16) (localProcess, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	var cmd *exec.Cmd
	if command != "" {
		cmd = exec.Command(shell, "-l", "-c", command)
	} else {
		cmd = exec.Command(shell, "-l")
	}
	if term == "" {
		term = "xterm-256color"
	}
	cmd.Env = append(os.Environ(), "TERM="+term, "COLORTERM=truecolor")

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		return nil, err
	}
	return &unixPTYHandle{file: ptmx, cmd: cmd}, nil
}

func (u *unixPTYHandle) Read(p []byte) (int, error)  { return u.file.Read(p) }
func (u *unixPTYHandle) Write(p []byte) (int, error) { return u.file.Write(p) }
func (u *unixPTYHandle) Resize(cols, rows uint16) error {
	return pty.Setsize(u.file, &pty.Winsize{Cols: cols, Rows: rows})
}
func (u *unixPTYHandle) PID() int {
	if u.cmd.Process != nil {
		return u.cmd.Process.Pid
	}
	return 0
}
func (u *unixPTYHandle) Wait() error { return u.cmd.Wait() }
func (u *unixPTYHandle) Close() error {
	_ = u.file.Close()
	if u.cmd.Process != nil {
		_ = u.cmd.Process.Kill()
	}
	return nil
}
