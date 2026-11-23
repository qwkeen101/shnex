//go:build windows

package relay

import (
	"io"
	"os"
	"os/exec"
)

type windowsPipeProcess struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	cmd    *exec.Cmd
}

func startLocalProcess(command, term string, _, _ uint16) (localProcess, error) {
	var cmd *exec.Cmd
	if command != "" {
		cmd = exec.Command("cmd.exe", "/Q", "/C", command)
	} else {
		cmd = exec.Command("cmd.exe", "/Q")
	}
	if term == "" {
		term = "xterm-256color"
	}
	cmd.Env = append(os.Environ(), "TERM="+term)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	return &windowsPipeProcess{
		stdin:  stdin,
		stdout: stdout,
		cmd:    cmd,
	}, nil
}

func (w *windowsPipeProcess) Read(p []byte) (int, error)   { return w.stdout.Read(p) }
func (w *windowsPipeProcess) Write(p []byte) (int, error)  { return w.stdin.Write(p) }
func (w *windowsPipeProcess) Resize(_, _ uint16) error     { return nil }
func (w *windowsPipeProcess) PID() int {
	if w.cmd.Process != nil {
		return w.cmd.Process.Pid
	}
	return 0
}
func (w *windowsPipeProcess) Wait() error { return w.cmd.Wait() }
func (w *windowsPipeProcess) Close() error {
	_ = w.stdin.Close()
	_ = w.stdout.Close()
	if w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
	}
	return nil
}
