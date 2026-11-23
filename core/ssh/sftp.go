package ssh

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"github.com/shnex/shnex/core/relay"
	"github.com/shnex/shnex/core/transport"
)

const sftpServerProbeSnippet = `for b in /usr/lib/openssh/sftp-server /usr/libexec/openssh/sftp-server /usr/lib/ssh/sftp-server /usr/libexec/sftp-server /usr/sbin/sftp-server; do [ -x "$b" ] && exec "$b"; done; exit 127`

// SetSFTPSudoPassword stores an ephemeral sudo password strictly in session memory.
func (s *Session) SetSFTPSudoPassword(password string) {
	s.sftpMu.Lock()
	defer s.sftpMu.Unlock()
	s.sudoPassword = password
	s.shellFallback = false
	s.sftpHomeDir = ""
	if s.sftpClient != nil {
		_ = s.sftpClient.Close()
		s.sftpClient = nil
	}
	if s.sftpSubSess != nil {
		_ = s.sftpSubSess.Close()
		s.sftpSubSess = nil
	}
}

func (s *Session) useShellModeLocked() bool {
	return s.fileProtocol == "scp" || s.shellFallback
}

func (s *Session) checkSudoModeLocked(ctx context.Context) (noPasswd bool, pw string, err error) {
	if s.sudoNoPasswd != nil && *s.sudoNoPasswd {
		return true, "", nil
	}
	if s.sudoNoPasswd == nil {
		ok := s.runQuickCheck(ctx, "sudo -n true >/dev/null 2>&1", "")
		s.sudoNoPasswd = &ok
		if ok {
			return true, "", nil
		}
	}
	if s.sudoPassword == "" {
		return false, "", transport.ErrSudoPasswordRequired
	}
	if !s.runQuickCheck(ctx, "sudo -S -p '' true >/dev/null 2>&1", s.sudoPassword+"\n") {
		s.sudoPassword = ""
		return false, "", transport.ErrSudoPasswordRequired
	}
	return false, s.sudoPassword, nil
}

func (s *Session) runQuickCheck(ctx context.Context, cmd string, stdinStr string) bool {
	sess, err := s.client.NewSession()
	if err != nil {
		return false
	}
	defer sess.Close()

	if stdinStr != "" {
		sess.Stdin = strings.NewReader(stdinStr)
	}
	done := make(chan error, 1)
	go func() {
		done <- sess.Run(cmd)
	}()
	select {
	case <-ctx.Done():
		_ = sess.Close()
		return false
	case err := <-done:
		return err == nil
	}
}

func (s *Session) getOrCreateSFTP(ctx context.Context) (*sftp.Client, bool, error) {
	s.sftpMu.Lock()
	defer s.sftpMu.Unlock()

	select {
	case <-s.stopCh:
		return nil, false, errors.New("ssh: session closed")
	default:
	}

	if s.useShellModeLocked() {
		if s.sftpSudo {
			if _, _, err := s.checkSudoModeLocked(ctx); err != nil {
				return nil, false, err
			}
		}
		return nil, true, nil
	}

	if s.sftpClient != nil {
		return s.sftpClient, false, nil
	}

	c, subSess, err := s.openSFTPClientLocked(ctx)
	if err != nil {
		if errors.Is(err, transport.ErrSudoPasswordRequired) {
			return nil, false, err
		}
		if s.fileProtocol == "" || s.fileProtocol == "auto" {
			s.shellFallback = true
			return nil, true, nil
		}
		return nil, false, err
	}
	s.sftpClient = c
	s.sftpSubSess = subSess
	return c, false, nil
}

func (s *Session) openSFTPClientLocked(ctx context.Context) (*sftp.Client, interface{ Close() error }, error) {
	if !s.sftpSudo {
		subSess, err := s.client.NewSession()
		if err != nil {
			return nil, nil, err
		}
		stdinPipe, err := subSess.StdinPipe()
		if err != nil {
			_ = subSess.Close()
			return nil, nil, err
		}
		stdoutPipe, err := subSess.StdoutPipe()
		if err != nil {
			_ = subSess.Close()
			return nil, nil, err
		}
		if err := subSess.RequestSubsystem("sftp"); err != nil {
			_ = subSess.Close()
			return nil, nil, err
		}
		stopCancel := context.AfterFunc(ctx, func() {
			_ = subSess.Close()
		})
		c, err := sftp.NewClientPipe(stdoutPipe, stdinPipe)
		stopCancel()
		if err != nil {
			_ = subSess.Close()
			return nil, nil, err
		}
		return c, subSess, nil
	}

	noPasswd, pw, err := s.checkSudoModeLocked(ctx)
	if err != nil {
		return nil, nil, err
	}

	subSess, err := s.client.NewSession()
	if err != nil {
		return nil, nil, err
	}
	stdinPipe, err := subSess.StdinPipe()
	if err != nil {
		_ = subSess.Close()
		return nil, nil, err
	}
	stdoutPipe, err := subSess.StdoutPipe()
	if err != nil {
		_ = subSess.Close()
		return nil, nil, err
	}

	var startCmd string
	if noPasswd {
		startCmd = "sudo -n sh -c " + relay.PosixQuote(sftpServerProbeSnippet)
	} else {
		startCmd = "sudo -S -p '' sh -c " + relay.PosixQuote(sftpServerProbeSnippet)
	}

	if err := subSess.Start(startCmd); err != nil {
		_ = subSess.Close()
		return nil, nil, err
	}

	if !noPasswd {
		if _, err := io.WriteString(stdinPipe, pw+"\n"); err != nil {
			_ = subSess.Close()
			return nil, nil, err
		}
	}

	c, err := sftp.NewClientPipe(stdoutPipe, stdinPipe)
	if err != nil {
		_ = subSess.Close()
		return nil, nil, err
	}
	return c, subSess, nil
}

func (s *Session) resolveSFTPTarget(sc *sftp.Client, remotePath string) string {
	s.sftpMu.Lock()
	home := s.sftpHomeDir
	s.sftpMu.Unlock()

	ensureHome := func() string {
		if home != "" {
			return home
		}
		if wd, err := sc.Getwd(); err == nil && wd != "" {
			s.sftpMu.Lock()
			s.sftpHomeDir = wd
			s.sftpMu.Unlock()
			home = wd
			return wd
		}
		return "/"
	}

	target := strings.TrimSpace(remotePath)
	if target == "" || target == "." || target == "~" {
		return ensureHome()
	}
	if strings.HasPrefix(target, "~/") {
		return path.Join(ensureHome(), target[2:])
	}
	if !strings.HasPrefix(target, "/") {
		return path.Join(ensureHome(), target)
	}
	return path.Clean(target)
}

// ListDir lists files and folders in remotePath via SFTP (with optional sudo) or shell/SCP fallback.
func (s *Session) ListDir(ctx context.Context, remotePath string) (string, []transport.FileEntry, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	sc, useShell, err := s.getOrCreateSFTP(ctx)
	if err != nil {
		return "", nil, err
	}
	if useShell {
		return s.shellListDir(ctx, remotePath)
	}

	target := s.resolveSFTPTarget(sc, remotePath)
	infos, err := sc.ReadDir(target)
	if err != nil {
		return "", nil, err
	}

	entries := make([]transport.FileEntry, 0, len(infos))
	for _, info := range infos {
		fullEntryPath := path.Join(target, info.Name())
		isDir := info.IsDir()
		if !isDir && info.Mode()&os.ModeSymlink != 0 {
			if st, sErr := sc.Stat(fullEntryPath); sErr == nil && st.IsDir() {
				isDir = true
			}
		}
		entries = append(entries, transport.FileEntry{
			Name:    info.Name(),
			Path:    fullEntryPath,
			IsDir:   isDir,
			Size:    info.Size(),
			Mode:    info.Mode().String(),
			ModTime: info.ModTime(),
		})
	}

	sortFileEntries(entries)
	return target, entries, nil
}

// ReadFile downloads up to maxBytes from remotePath over SFTP or shell stream fallback.
func (s *Session) ReadFile(ctx context.Context, remotePath string, maxBytes int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxBytes <= 0 {
		maxBytes = 10 << 20 // 10 MiB default safety cap
	}
	sc, useShell, err := s.getOrCreateSFTP(ctx)
	if err != nil {
		return nil, err
	}
	if useShell {
		return s.shellReadFile(ctx, remotePath, maxBytes)
	}
	f, err := sc.Open(s.resolveSFTPTarget(sc, remotePath))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxBytes))
}

// WriteFile uploads data to remotePath over SFTP or shell/base64 stream fallback.
func (s *Session) WriteFile(ctx context.Context, remotePath string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sc, useShell, err := s.getOrCreateSFTP(ctx)
	if err != nil {
		return err
	}
	if useShell {
		return s.shellWriteFile(ctx, remotePath, data)
	}
	f, err := sc.Create(remotePath)
	if err != nil {
		return err
	}
	_, wErr := f.Write(data)
	cErr := f.Close()
	if wErr != nil {
		return wErr
	}
	if strings.HasPrefix(remotePath, "/tmp/shnex_img_") {
		_ = sc.Chmod(remotePath, 0o644)
	}
	return cErr
}

// MkdirAll creates a remote directory path recursively.
func (s *Session) MkdirAll(ctx context.Context, remotePath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sc, useShell, err := s.getOrCreateSFTP(ctx)
	if err != nil {
		return err
	}
	if useShell {
		_, err := s.runFileShellScript(ctx, "mkdir -p -- "+relay.PosixQuote(remotePath), nil)
		return err
	}
	return sc.MkdirAll(remotePath)
}

// Remove deletes a remote file or empty directory.
func (s *Session) Remove(ctx context.Context, remotePath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sc, useShell, err := s.getOrCreateSFTP(ctx)
	if err != nil {
		return err
	}
	if useShell {
		_, err := s.runFileShellScript(ctx, "rm -rf -- "+relay.PosixQuote(remotePath), nil)
		return err
	}
	return sc.Remove(remotePath)
}

// --- Shell / SCP Fallback Operations ---

func (s *Session) runFileShellScript(ctx context.Context, script string, extraStdin []byte) ([]byte, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	var cmd string
	var stdinBuf bytes.Buffer

	if s.sftpSudo {
		s.sftpMu.Lock()
		noPasswd, pw, sErr := s.checkSudoModeLocked(ctx)
		s.sftpMu.Unlock()
		if sErr != nil {
			return nil, sErr
		}
		if noPasswd {
			cmd = "sudo -n sh -c " + relay.PosixQuote(script)
		} else {
			cmd = "sudo -S -p '' sh -c " + relay.PosixQuote(script)
			stdinBuf.WriteString(pw + "\n")
		}
	} else {
		cmd = "sh -c " + relay.PosixQuote(script)
	}

	if len(extraStdin) > 0 {
		stdinBuf.Write(extraStdin)
	}
	if stdinBuf.Len() > 0 {
		sess.Stdin = &stdinBuf
	}

	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr

	done := make(chan error, 1)
	go func() {
		done <- sess.Run(cmd)
	}()

	select {
	case <-ctx.Done():
		_ = sess.Close()
		return nil, ctx.Err()
	case rErr := <-done:
		if rErr != nil {
			errMsg := strings.TrimSpace(stderr.String())
			if errMsg != "" {
				return nil, fmt.Errorf("%w: %s", rErr, errMsg)
			}
			return nil, rErr
		}
		return stdout.Bytes(), nil
	}
}

func (s *Session) shellListDir(ctx context.Context, remotePath string) (string, []transport.FileEntry, error) {
	target := strings.TrimSpace(remotePath)
	cdExpr := "cd ."
	if strings.HasPrefix(target, "~/") {
		cdExpr = "cd && cd -- " + relay.PosixQuote(path.Clean(target[2:]))
	} else if target != "" && target != "." && target != "~" {
		cdExpr = "cd -- " + relay.PosixQuote(path.Clean(target))
	}
	script := cdExpr + ` && pwd && (LC_ALL=C ls -la --time-style=+%s 2>/dev/null || LC_ALL=C ls -la)`
	out, err := s.runFileShellScript(ctx, script, nil)
	if err != nil {
		return "", nil, err
	}

	lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return "/", nil, nil
	}
	resolved := strings.TrimSpace(lines[0])
	if resolved == "" {
		resolved = "/"
	}

	var entries []transport.FileEntry
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "total ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		mode := fields[0]
		size, _ := strconv.ParseInt(fields[4], 10, 64)

		// With --time-style=+%s: perms links owner group size epoch name...
		var modTime time.Time
		var name string
		if epoch, pErr := strconv.ParseInt(fields[5], 10, 64); pErr == nil && len(fields) >= 7 {
			modTime = time.Unix(epoch, 0)
			idx := nthFieldStart(line, 6)
			name = line[idx:]
		} else if len(fields) >= 9 {
			idx := nthFieldStart(line, 8)
			name = line[idx:]
		} else {
			continue
		}
		if symIdx := strings.Index(name, " -> "); symIdx > 0 && strings.HasPrefix(mode, "l") {
			name = name[:symIdx]
		}
		if name == "." || name == ".." || name == "" {
			continue
		}
		entries = append(entries, transport.FileEntry{
			Name:    name,
			Path:    path.Join(resolved, name),
			IsDir:   strings.HasPrefix(mode, "d"),
			Size:    size,
			Mode:    mode,
			ModTime: modTime,
		})
	}
	sortFileEntries(entries)
	return resolved, entries, nil
}

func (s *Session) shellReadFile(ctx context.Context, remotePath string, maxBytes int64) ([]byte, error) {
	script := fmt.Sprintf("head -c %d -- %s", maxBytes, relay.PosixQuote(remotePath))
	return s.runFileShellScript(ctx, script, nil)
}

func (s *Session) shellWriteFile(ctx context.Context, remotePath string, data []byte) error {
	encoded := base64.StdEncoding.EncodeToString(data) + "\n"
	q := relay.PosixQuote(remotePath)
	script := "base64 -d > " + q
	if strings.HasPrefix(remotePath, "/tmp/shnex_img_") {
		script += " && chmod 644 " + q
	}
	_, err := s.runFileShellScript(ctx, script, []byte(encoded))
	return err
}

func nthFieldStart(s string, fieldIdx int) int {
	inSpace := true
	cur := -1
	for i := 0; i < len(s); i++ {
		isSpace := s[i] == ' ' || s[i] == '\t'
		if !isSpace && inSpace {
			cur++
			if cur == fieldIdx {
				return i
			}
			inSpace = false
		} else if isSpace {
			inSpace = true
		}
	}
	return len(s)
}

func sortFileEntries(entries []transport.FileEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}
