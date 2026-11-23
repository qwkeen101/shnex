package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/shnex/shnex/core/relay"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	sub := os.Args[1]
	switch sub {
	case "daemon":
		runDaemon(os.Args[2:])
	case "mux-attach":
		runMuxAttach(os.Args[2:])
	case "ls":
		runList(os.Args[2:])
	case "kill":
		runKill(os.Args[2:])
	default:
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: shnex-agent <daemon|mux-attach|ls|kill> [flags]")
}

func defaultSocketPath() (string, error) {
	runDir := os.Getenv("XDG_RUNTIME_DIR")
	if runDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			runDir = os.TempDir()
		} else {
			runDir = filepath.Join(home, ".shnex", "run")
		}
	} else {
		runDir = filepath.Join(runDir, "shnex")
	}
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return "", err
	}
	_ = os.Chmod(runDir, 0o700)
	return filepath.Join(runDir, "agent.sock"), nil
}

func runDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	sock := fs.String("socket", "", "unix domain socket path")
	idle := fs.Duration("idle-exit", 24*time.Hour, "exit daemon when 0 workspaces remain for this duration")
	_ = fs.Parse(args)

	sockPath := *sock
	if sockPath == "" {
		var err error
		sockPath, err = defaultSocketPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "resolve socket path: %v\n", err)
			os.Exit(1)
		}
	}

	if err := os.MkdirAll(filepath.Dir(sockPath), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir socket dir: %v\n", err)
		os.Exit(1)
	}
	// If an active daemon already listens on sockPath, exit quietly.
	if c, err := net.DialTimeout("unix", sockPath, 250*time.Millisecond); err == nil {
		_ = c.Close()
		return
	}
	_ = os.Remove(sockPath)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen unix %s: %v\n", sockPath, err)
		os.Exit(1)
	}
	_ = os.Chmod(sockPath, 0o600)
	defer func() {
		_ = ln.Close()
		_ = os.Remove(sockPath)
	}()

	d := relay.NewDaemon(*idle)
	if err := d.Serve(ln); err != nil {
		fmt.Fprintf(os.Stderr, "daemon serve: %v\n", err)
		os.Exit(1)
	}
}

func connectOrSpawnDaemon(sockPath string) (net.Conn, error) {
	if sockPath == "" {
		var err error
		sockPath, err = defaultSocketPath()
		if err != nil {
			return nil, err
		}
	}
	if conn, err := net.DialTimeout("unix", sockPath, 300*time.Millisecond); err == nil {
		return conn, nil
	}

	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "daemon", "--socket", sockPath)
	// Redirect stdio to nil (/dev/null) so OpenSSH exec channels never block waiting
	// on inherited file descriptors held by the background daemon.
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	configureDetachedDaemonCmd(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("auto-start daemon: %w", err)
	}
	_ = cmd.Process.Release()

	deadline := time.Now().Add(3 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, dErr := net.DialTimeout("unix", sockPath, 200*time.Millisecond)
		if dErr == nil {
			return conn, nil
		}
		lastErr = dErr
		time.Sleep(40 * time.Millisecond)
	}
	return nil, fmt.Errorf("daemon did not become ready at %s: %v", sockPath, lastErr)
}

func runMuxAttach(args []string) {
	fs := flag.NewFlagSet("mux-attach", flag.ExitOnError)
	sock := fs.String("socket", "", "unix domain socket path")
	session := fs.String("session", relay.DefaultWorkspaceName, "workspace session name")
	cols := fs.Uint("cols", 80, "initial terminal columns")
	rows := fs.Uint("rows", 24, "initial terminal rows")
	term := fs.String("term", "xterm-256color", "TERM value")
	command := fs.String("cmd", "", "optional initial command")
	_ = fs.Parse(args)

	conn, err := connectOrSpawnDaemon(*sock)
	if err != nil {
		fmt.Fprintf(os.Stderr, "shnex-agent connect failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Perform handshake with local daemon if caller speaks raw mux stream on stdio,
	// OR let core/ssh speak the mux protocol directly end-to-end through this bridge!
	// Wait: Look! Because Daemon itself speaks the exact SHNEX01\n mux protocol on conn,
	// if core/ssh sends SHNEX01\n + PktHello directly over SSH stdio, mux-attach can either:
	//   (a) send PktHello derived from CLI flags when stdin starts with SHNEX01\n or
	//   (b) transparently splice os.Stdin <-> conn <-> os.Stdout!
	// Let's check: splicing os.Stdin <-> conn <-> os.Stdout directly means zero double-framing!
	// AND if invoked with flags by core/ssh, core/ssh sends SHNEX01\n + PktHello directly!
	_ = session
	_ = cols
	_ = rows
	_ = term
	_ = command

	errCh := make(chan error, 2)
	go func() {
		_, cErr := io.Copy(conn, os.Stdin)
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		errCh <- cErr
	}()
	go func() {
		_, cErr := io.Copy(os.Stdout, conn)
		errCh <- cErr
	}()

	// Exit as soon as daemon->stdout finishes (or stdin closes cleanly).
	<-errCh
}

func runList(args []string) {
	fs := flag.NewFlagSet("ls", flag.ExitOnError)
	sock := fs.String("socket", "", "unix domain socket path")
	_ = fs.Parse(args)

	sockPath := *sock
	if sockPath == "" {
		var err error
		sockPath, err = defaultSocketPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
	conn, err := net.DialTimeout("unix", sockPath, 500*time.Millisecond)
	if err != nil {
		fmt.Println("No running shnex-agent daemon (0 workspaces).")
		return
	}
	defer conn.Close()

	sw := relay.NewStreamWriter(conn)
	if err := sw.WriteMagic(); err != nil {
		os.Exit(1)
	}
	if err := relay.ReadMagic(conn); err != nil {
		os.Exit(1)
	}
	if err := sw.WritePacket(relay.PktListReq, nil); err != nil {
		os.Exit(1)
	}
	pt, pl, err := relay.ReadPacket(conn)
	if err != nil || pt != relay.PktListRes {
		os.Exit(1)
	}
	var list []relay.WorkspaceInfo
	_ = json.Unmarshal(pl, &list)
	if len(list) == 0 {
		fmt.Println("0 active workspaces.")
		return
	}
	for _, w := range list {
		fmt.Printf("%-20s pid=%-7d size=%dx%-4d peers=%d created=%s\n",
			w.SessionName, w.PID, w.Cols, w.Rows, w.AttachedPeers, w.CreatedAt.Format(time.RFC3339))
	}
}

func runKill(args []string) {
	fs := flag.NewFlagSet("kill", flag.ExitOnError)
	sock := fs.String("socket", "", "unix domain socket path")
	session := fs.String("session", "", "workspace session name to terminate")
	_ = fs.Parse(args)

	if *session == "" {
		fmt.Fprintln(os.Stderr, "--session is required")
		os.Exit(2)
	}
	sockPath := *sock
	if sockPath == "" {
		var err error
		sockPath, err = defaultSocketPath()
		if err != nil {
			os.Exit(1)
		}
	}
	conn, err := net.DialTimeout("unix", sockPath, 500*time.Millisecond)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "daemon not running: %v\n", err)
		}
		return
	}
	defer conn.Close()

	sw := relay.NewStreamWriter(conn)
	_ = sw.WriteMagic()
	_ = relay.ReadMagic(conn)
	_ = sw.WritePacket(relay.PktKillReq, []byte(*session))
	_, _, _ = relay.ReadPacket(conn)
	fmt.Printf("Terminated workspace %q\n", *session)
}
