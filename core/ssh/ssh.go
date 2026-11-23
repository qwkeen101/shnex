package ssh

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"github.com/shnex/shnex/core/dialer"
	"github.com/shnex/shnex/core/platform"
	"github.com/shnex/shnex/core/relay"
	"github.com/shnex/shnex/core/transport"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	ProtocolName = "ssh"

	defaultKeepaliveInterval  = 15 * time.Second
	defaultKeepaliveTimeout   = 5 * time.Second
	defaultKeepaliveMaxMissed = 3
	defaultHandshakeTimeout   = 15 * time.Second
)

// ErrHostKeyVerificationRequired is returned when no host key policy was configured.
var ErrHostKeyVerificationRequired = errors.New("ssh: host key policy required (set pinned_sha256, known_hosts_path, authorized_host_keys, or explicit insecure_ignore)")

// ErrPasswordRequired indicates that local/saved keys could not authenticate and an interactive password is needed.
var ErrPasswordRequired = errors.New("ssh: authentication requires password (AUTH_PASSWORD_NEEDED)")

// Transport implements transport.Transport over SSHv2 PTY and shnex-agent mux channels.
type Transport struct {
	bridge     platform.PlatformBridge
	netChanges *platform.NetworkBroadcaster
}

// NewTransport constructs an SSH Transport wired to optional keychain/notification bridge
// and host network-change broadcaster.
func NewTransport(bridge platform.PlatformBridge, netChanges *platform.NetworkBroadcaster) *Transport {
	return &Transport{
		bridge:     bridge,
		netChanges: netChanges,
	}
}

func (t *Transport) Protocol() string {
	return ProtocolName
}

// OpenSession dials, authenticates, and opens either an interactive SSH PTY (supporting
// plain shell, tmux, zellij, or auto relay) or a framed shnex-agent mux session.
func (t *Transport) OpenSession(ctx context.Context, cfg transport.SessionConfig) (transport.Session, error) {
	target := normalizeSSHAddr(strings.TrimSpace(cfg.Target))
	if target == "" {
		if cfg.DialerCloser != nil {
			_ = cfg.DialerCloser.Close()
		}
		return nil, errors.New("ssh: target host must not be empty")
	}
	user := strings.TrimSpace(cfg.User)
	if user == "" {
		if cfg.DialerCloser != nil {
			_ = cfg.DialerCloser.Close()
		}
		return nil, errors.New("ssh: username must not be empty")
	}

	sessionID := strings.TrimSpace(cfg.ID)
	if sessionID == "" {
		sessionID = randomSessionID()
	}

	hostKeyCb, err := buildHostKeyCallback(cfg.Credentials.HostKey)
	if err != nil {
		if cfg.DialerCloser != nil {
			_ = cfg.DialerCloser.Close()
		}
		return nil, err
	}

	dispatcher := transport.NewEventDispatcher(cfg.InitialHooks...)
	var bannerBuf bytes.Buffer

	authMethods, resolvedPassword, err := t.buildAuthMethods(ctx, cfg.Credentials)
	if err != nil {
		if cfg.DialerCloser != nil {
			_ = cfg.DialerCloser.Close()
		}
		return nil, err
	}
	if len(authMethods) == 0 && cfg.Credentials.Mode == transport.AuthAuto {
		if cfg.DialerCloser != nil {
			_ = cfg.DialerCloser.Close()
		}
		return nil, ErrPasswordRequired
	}
	if len(authMethods) == 0 && cfg.Credentials.Mode != transport.AuthTailscaleSSH {
		if cfg.DialerCloser != nil {
			_ = cfg.DialerCloser.Close()
		}
		return nil, errors.New("ssh: no authentication credentials provided")
	}

	d := cfg.Dialer
	if d == nil {
		d = dialer.NewDirectDialer(defaultHandshakeTimeout, 30*time.Second)
	}

	rawConn, err := d.Dial(ctx, target)
	if err != nil {
		if cfg.DialerCloser != nil {
			_ = cfg.DialerCloser.Close()
		}
		return nil, fmt.Errorf("ssh: dial %s: %w", target, err)
	}

	clientCfg := &gossh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCb,
		Timeout:         defaultHandshakeTimeout,
		BannerCallback: func(message string) error {
			if strings.TrimSpace(message) != "" {
				bannerBuf.WriteString(strings.ReplaceAll(message, "\n", "\r\n"))
				if !strings.HasSuffix(message, "\n") {
					bannerBuf.WriteString("\r\n")
				}
			}
			return nil
		},
	}

	sshConn, chans, reqs, err := handshakeWithContext(ctx, rawConn, target, clientCfg)
	if err != nil {
		if cfg.DialerCloser != nil {
			_ = cfg.DialerCloser.Close()
		}
		if cfg.Credentials.Mode == transport.AuthAuto && strings.Contains(err.Error(), "unable to authenticate") {
			return nil, fmt.Errorf("%w: %v", ErrPasswordRequired, err)
		}
		return nil, fmt.Errorf("ssh: handshake %s: %w", target, err)
	}
	client := gossh.NewClient(sshConn, chans, reqs)

	sshSess, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		if cfg.DialerCloser != nil {
			_ = cfg.DialerCloser.Close()
		}
		return nil, fmt.Errorf("ssh: open session channel: %w", err)
	}

	cleanupFail := func() {
		_ = sshSess.Close()
		_ = client.Close()
		if cfg.DialerCloser != nil {
			_ = cfg.DialerCloser.Close()
		}
	}

	size := cfg.InitialSize.Normalized()
	termType := strings.TrimSpace(cfg.Term)
	if termType == "" {
		termType = "xterm-256color"
	}

	for k, v := range cfg.Env {
		_ = sshSess.Setenv(k, v)
	}

	var (
		stdinWriter io.WriteCloser
		pr          *io.PipeReader
		resizeFn    func(cols, rows uint16) error
		relayEvent  *transport.SessionEvent
	)

	if cfg.Relay.Mode == transport.RelayShnexAgent {
		stdinPipe, sErr := sshSess.StdinPipe()
		if sErr != nil {
			cleanupFail()
			return nil, fmt.Errorf("ssh: attach agent stdin: %w", sErr)
		}
		stdoutPipe, oErr := sshSess.StdoutPipe()
		if oErr != nil {
			cleanupFail()
			return nil, fmt.Errorf("ssh: attach agent stdout: %w", oErr)
		}
		stderrPipe, eErr := sshSess.StderrPipe()
		if eErr != nil {
			cleanupFail()
			return nil, fmt.Errorf("ssh: attach agent stderr: %w", eErr)
		}

		execCmd := relay.BuildAgentMuxExecCommand(cfg.Relay, size, termType)
		if err := sshSess.Start(execCmd); err != nil {
			cleanupFail()
			return nil, fmt.Errorf("ssh: exec shnex-agent: %w", err)
		}

		sw := relay.NewStreamWriter(stdinPipe)
		welcome, hsErr := handshakeShnexAgent(ctx, sw, stdoutPipe, stderrPipe, cfg.Relay, size, termType)
		if hsErr != nil {
			cleanupFail()
			return nil, hsErr
		}

		var pw *io.PipeWriter
		pr, pw = io.Pipe()
		go demuxAgentStream(stdoutPipe, pw)

		stdinWriter = &agentFramedStdin{sw: sw, raw: stdinPipe}
		resizeFn = func(cols, rows uint16) error {
			return sw.WriteResize(cols, rows)
		}
		relayEvent = &transport.SessionEvent{
			SessionID:    sessionID,
			Type:         transport.EventRelayAttached,
			RelayBackend: string(transport.RelayShnexAgent),
			Message: fmt.Sprintf(
				"attached to shnex-agent workspace %q (pid=%d, peers=%d, replayed=%dB)",
				welcome.SessionName, welcome.PID, welcome.AttachedPeers, welcome.ReplayedBytes,
			),
		}
	} else {
		modes := gossh.TerminalModes{
			gossh.ECHO:          1,
			gossh.TTY_OP_ISPEED: 14400,
			gossh.TTY_OP_OSPEED: 14400,
		}
		if err := sshSess.RequestPty(termType, int(size.Rows), int(size.Cols), modes); err != nil {
			cleanupFail()
			return nil, fmt.Errorf("ssh: request pty (%s %dx%d): %w", termType, size.Cols, size.Rows, err)
		}

		stdinPipe, sErr := sshSess.StdinPipe()
		if sErr != nil {
			cleanupFail()
			return nil, fmt.Errorf("ssh: attach stdin: %w", sErr)
		}
		stdoutPipe, oErr := sshSess.StdoutPipe()
		if oErr != nil {
			cleanupFail()
			return nil, fmt.Errorf("ssh: attach stdout: %w", oErr)
		}
		stderrPipe, eErr := sshSess.StderrPipe()
		if eErr != nil {
			cleanupFail()
			return nil, fmt.Errorf("ssh: attach stderr: %w", eErr)
		}

		bootCmd := relay.BuildPTYBootstrapCommand(cfg.Relay)
		defaultDir := strings.TrimSpace(cfg.DefaultDir)
		postScript := strings.TrimSpace(strings.ReplaceAll(cfg.PostLoginScript, "\r\n", "\n"))
		firstOutputCh := make(chan struct{}, 1)

		if postScript != "" {
			// When post-login commands are configured, launch an interactive login shell
			// (entering DefaultDir first if set), then feed each line sequentially before attaching tmux.
			if defaultDir != "" {
				cdLogin := fmt.Sprintf("cd %s 2>/dev/null || true; exec \"${SHELL:-/bin/sh}\" -l", relay.PosixQuote(defaultDir))
				if err := sshSess.Start("sh -c " + relay.PosixQuote(cdLogin)); err != nil {
					cleanupFail()
					return nil, fmt.Errorf("ssh: start login shell in %q: %w", defaultDir, err)
				}
			} else {
				if err := sshSess.Shell(); err != nil {
					cleanupFail()
					return nil, fmt.Errorf("ssh: start interactive shell: %w", err)
				}
			}
		} else if bootCmd != "" {
			if defaultDir != "" {
				bootCmd = fmt.Sprintf("sh -c %s", relay.PosixQuote(
					fmt.Sprintf("cd %s 2>/dev/null || true; exec %s", relay.PosixQuote(defaultDir), bootCmd),
				))
			}
			if err := sshSess.Start(bootCmd); err != nil {
				cleanupFail()
				return nil, fmt.Errorf("ssh: start relay command (%s): %w", cfg.Relay.Mode, err)
			}
			if cfg.Relay.Mode != "" && cfg.Relay.Mode != transport.RelayNone {
				wsName := relay.SanitizeWorkspaceName(cfg.Relay.SessionName)
				relayEvent = &transport.SessionEvent{
					SessionID:    sessionID,
					Type:         transport.EventRelayAttached,
					RelayBackend: string(cfg.Relay.Mode),
					Message:      fmt.Sprintf("bootstrapped %s workspace %q", cfg.Relay.Mode, wsName),
				}
			}
		} else if defaultDir != "" {
			cdLogin := fmt.Sprintf("cd %s 2>/dev/null || true; exec \"${SHELL:-/bin/sh}\" -l", relay.PosixQuote(defaultDir))
			if err := sshSess.Start("sh -c " + relay.PosixQuote(cdLogin)); err != nil {
				cleanupFail()
				return nil, fmt.Errorf("ssh: start login shell in %q: %w", defaultDir, err)
			}
		} else {
			if err := sshSess.Shell(); err != nil {
				cleanupFail()
				return nil, fmt.Errorf("ssh: start interactive shell: %w", err)
			}
		}

		var pw *io.PipeWriter
		pr, pw = io.Pipe()
		var pipeWG sync.WaitGroup
		if bannerBuf.Len() > 0 {
			bannerBytes := append([]byte(nil), bannerBuf.Bytes()...)
			pipeWG.Add(1)
			go func() {
				defer pipeWG.Done()
				_, _ = pw.Write(bannerBytes)
			}()
		}
		pipeWG.Add(2)
		go func() {
			defer pipeWG.Done()
			buf := make([]byte, 16*1024)
			notifiedFirst := false
			for {
				n, rErr := stdoutPipe.Read(buf)
				if n > 0 {
					if !notifiedFirst {
						notifiedFirst = true
						select {
						case firstOutputCh <- struct{}{}:
						default:
						}
					}
					if _, wErr := pw.Write(buf[:n]); wErr != nil {
						return
					}
				}
				if rErr != nil {
					return
				}
			}
		}()
		go func() {
			defer pipeWG.Done()
			_, _ = io.Copy(pw, stderrPipe)
		}()
		go func() {
			pipeWG.Wait()
			_ = pw.Close()
		}()

		stdinWriter = stdinPipe
		resizeFn = func(cols, rows uint16) error {
			return sshSess.WindowChange(int(rows), int(cols))
		}

		if postScript != "" {
			relayCfgCopy := cfg.Relay
			go func() {
				select {
				case <-firstOutputCh:
					time.Sleep(60 * time.Millisecond)
				case <-time.After(280 * time.Millisecond):
				}
				lines := strings.Split(postScript, "\n")
				for _, line := range lines {
					if _, wErr := stdinPipe.Write([]byte(line + "\n")); wErr != nil {
						return
					}
					time.Sleep(25 * time.Millisecond)
				}
				if relayCfgCopy.Mode == transport.RelayTmux || (relayCfgCopy.Mode == transport.RelayAuto && strings.TrimSpace(relayCfgCopy.SessionName) != "") {
					ws := relay.SanitizeWorkspaceName(relayCfgCopy.SessionName)
					tmuxLine := fmt.Sprintf("(tmux start-server \\; set-option -g window-size latest >/dev/null 2>&1 || true); tmux new-session -A -s %s\n", relay.PosixQuote(ws))
					_, _ = stdinPipe.Write([]byte(tmuxLine))
				}
			}()
		}
	}

	observer := transport.NewStreamObserver(pr, transport.ObserverConfig{
		SessionID:         sessionID,
		SilenceThreshold:  cfg.SilenceThreshold,
		SilenceMinBytes:   cfg.SilenceMinBytes,
		ApprovalDebounce:  cfg.ApprovalDebounce,
		DisableAIDetector: cfg.DisableAIDetector,
		BellCooldown:      cfg.BellCooldown,
		Dispatcher:        dispatcher,
	})

	kaInterval := cfg.KeepaliveInterval
	if kaInterval == 0 {
		kaInterval = defaultKeepaliveInterval
	}
	kaTimeout := cfg.KeepaliveTimeout
	if kaTimeout <= 0 {
		kaTimeout = defaultKeepaliveTimeout
	}
	kaMaxMissed := cfg.KeepaliveMaxMissed
	if kaMaxMissed <= 0 {
		kaMaxMissed = defaultKeepaliveMaxMissed
	}

	s := &Session{
		id:           sessionID,
		target:       target,
		createdAt:    time.Now(),
		client:       client,
		sshSess:      sshSess,
		stdin:        stdinWriter,
		resizeFn:     resizeFn,
		output:       observer,
		pipeReader:   pr,
		dialerCloser: cfg.DialerCloser,
		dispatcher:   dispatcher,
		kaInterval:   kaInterval,
		kaTimeout:    kaTimeout,
		kaMaxMissed:  kaMaxMissed,
		fileProtocol: strings.ToLower(strings.TrimSpace(cfg.FileProtocol)),
		sftpSudo:     cfg.SFTPSudo,
		sudoPassword: resolvedPassword,
		probeNowCh:   make(chan string, 1),
		stopCh:       make(chan struct{}),
		waitDoneCh:   make(chan struct{}),
	}

	if t.netChanges != nil {
		s.unsubNet = t.netChanges.Subscribe(func(ev platform.NetworkChangeEvent) {
			s.triggerImmediateProbe(ev.Reason)
		})
	}

	dispatcher.Emit(transport.SessionEvent{
		SessionID: sessionID,
		Type:      transport.EventConnected,
		Message:   fmt.Sprintf("connected to %s as %s", target, user),
	})
	if relayEvent != nil {
		dispatcher.Emit(*relayEvent)
	}

	go s.monitorWait()
	if kaInterval > 0 {
		go s.keepaliveLoop()
	}

	return s, nil
}

func handshakeShnexAgent(
	ctx context.Context,
	sw *relay.StreamWriter,
	stdout io.Reader,
	stderr io.Reader,
	rcfg transport.RelayConfig,
	size transport.TerminalSize,
	term string,
) (*relay.WelcomePayload, error) {
	type hsResult struct {
		welcome *relay.WelcomePayload
		err     error
	}
	ch := make(chan hsResult, 1)

	go func() {
		if err := sw.WriteMagic(); err != nil {
			ch <- hsResult{err: err}
			return
		}
		hello := relay.HelloPayload{
			SessionName: relay.SanitizeWorkspaceName(rcfg.SessionName),
			Cols:        size.Cols,
			Rows:        size.Rows,
			Term:        term,
			Command:     strings.TrimSpace(rcfg.Command),
		}
		if err := sw.WriteJSON(relay.PktHello, hello); err != nil {
			ch <- hsResult{err: err}
			return
		}
		if err := relay.ReadMagic(stdout); err != nil {
			errBytes, _ := io.ReadAll(io.LimitReader(stderr, 512))
			errText := strings.TrimSpace(string(errBytes))
			if strings.Contains(errText, "SHNEX_AGENT_MISSING") {
				ch <- hsResult{err: errors.New("ssh: shnex-agent binary not found on remote server (deploy shnex-agent to ~/.local/bin or switch relay mode to 'auto'/'tmux')")}
				return
			}
			if errText != "" {
				ch <- hsResult{err: fmt.Errorf("%w: %s", err, errText)}
				return
			}
			ch <- hsResult{err: err}
			return
		}
		pt, pl, err := relay.ReadPacket(stdout)
		if err != nil {
			ch <- hsResult{err: fmt.Errorf("ssh: read shnex-agent welcome: %w", err)}
			return
		}
		if pt == relay.PktExit {
			var ex relay.ExitPayload
			_ = json.Unmarshal(pl, &ex)
			ch <- hsResult{err: fmt.Errorf("ssh: remote shnex-agent exited: %s", ex.Message)}
			return
		}
		if pt != relay.PktWelcome {
			ch <- hsResult{err: fmt.Errorf("ssh: unexpected shnex-agent handshake opcode 0x%02x", pt)}
			return
		}
		var w relay.WelcomePayload
		if err := json.Unmarshal(pl, &w); err != nil {
			ch <- hsResult{err: err}
			return
		}
		ch <- hsResult{welcome: &w}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.welcome, res.err
	}
}

func demuxAgentStream(stdout io.Reader, pw *io.PipeWriter) {
	defer pw.Close()
	for {
		pt, pl, err := relay.ReadPacket(stdout)
		if err != nil {
			return
		}
		switch pt {
		case relay.PktData:
			if len(pl) > 0 {
				if _, wErr := pw.Write(pl); wErr != nil {
					return
				}
			}
		case relay.PktExit:
			return
		}
	}
}

type agentFramedStdin struct {
	sw  *relay.StreamWriter
	raw io.Closer
}

func (a *agentFramedStdin) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		chunk := p
		if len(chunk) > relay.MaxPacketPayload {
			chunk = p[:relay.MaxPacketPayload]
		}
		if err := a.sw.WritePacket(relay.PktData, chunk); err != nil {
			return 0, err
		}
		p = p[len(chunk):]
	}
	return total, nil
}

func (a *agentFramedStdin) Close() error {
	return a.raw.Close()
}

func (t *Transport) buildAuthMethods(ctx context.Context, cred transport.CredentialsSpec) ([]gossh.AuthMethod, string, error) {
	pw := cred.Password
	pemBytes := cred.PrivateKeyPEM

	if cred.SecretKeyRef != "" {
		if t.bridge == nil {
			if cred.Mode != transport.AuthAuto {
				return nil, "", errors.New("ssh: secret_key_ref requested, but no PlatformBridge is configured")
			}
		} else {
			secret, err := t.bridge.GetSecret(ctx, cred.SecretKeyRef)
			if err != nil {
				if cred.Mode != transport.AuthAuto {
					return nil, "", fmt.Errorf("ssh: resolve secret %q: %w", cred.SecretKeyRef, err)
				}
			} else if strings.Contains(secret, "-----BEGIN ") {
				pemBytes = []byte(secret)
			} else if secret != "" {
				pw = secret
			}
		}
	}

	var methods []gossh.AuthMethod
	if cred.Mode == transport.AuthAuto {
		if signers := loadLocalDefaultSSHKeys(); len(signers) > 0 {
			methods = append(methods, gossh.PublicKeys(signers...))
		}
	}
	if len(pemBytes) > 0 {
		var (
			signer gossh.Signer
			err    error
		)
		if len(cred.PrivateKeyPassphrase) > 0 {
			signer, err = gossh.ParsePrivateKeyWithPassphrase(pemBytes, cred.PrivateKeyPassphrase)
		} else {
			signer, err = gossh.ParsePrivateKey(pemBytes)
		}
		if err != nil {
			return nil, "", fmt.Errorf("ssh: parse private key: %w", err)
		}
		methods = append(methods, gossh.PublicKeys(signer))
	}

	if pw != "" {
		secretPw := pw
		methods = append(methods,
			gossh.Password(secretPw),
			gossh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					answers[i] = secretPw
				}
				return answers, nil
			}),
		)
	}

	if cred.Mode == transport.AuthTailscaleSSH {
		methods = append(methods, gossh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			return make([]string, len(questions)), nil
		}))
	}

	return methods, pw, nil
}

func buildHostKeyCallback(policy transport.HostKeyPolicy) (gossh.HostKeyCallback, error) {
	if pinned := strings.TrimSpace(policy.PinnedSHA256); pinned != "" {
		return func(hostname string, _ net.Addr, key gossh.PublicKey) error {
			got := gossh.FingerprintSHA256(key)
			if got != pinned {
				return fmt.Errorf("ssh: host key mismatch for %s: got %s, expected %s", hostname, got, pinned)
			}
			return nil
		}, nil
	}

	if len(policy.AuthorizedHostKeys) > 0 {
		var trusted [][]byte
		for _, line := range policy.AuthorizedHostKeys {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			pub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(line))
			if err != nil {
				return nil, fmt.Errorf("ssh: invalid authorized host key %q: %w", line, err)
			}
			trusted = append(trusted, pub.Marshal())
		}
		if len(trusted) == 0 {
			return nil, errors.New("ssh: authorized_host_keys contained no valid keys")
		}
		return func(hostname string, _ net.Addr, key gossh.PublicKey) error {
			raw := key.Marshal()
			for _, okKey := range trusted {
				if bytes.Equal(raw, okKey) {
					return nil
				}
			}
			return fmt.Errorf("ssh: server host key %s not in trusted host key set for %s", gossh.FingerprintSHA256(key), hostname)
		}, nil
	}

	if khPath := strings.TrimSpace(policy.KnownHostsPath); khPath != "" {
		cb, err := knownhosts.New(khPath)
		if err != nil {
			return nil, fmt.Errorf("ssh: load known_hosts %q: %w", khPath, err)
		}
		return cb, nil
	}

	if policy.InsecureIgnore {
		return gossh.InsecureIgnoreHostKey(), nil
	}

	return nil, ErrHostKeyVerificationRequired
}

func handshakeWithContext(ctx context.Context, conn net.Conn, addr string, cfg *gossh.ClientConfig) (gossh.Conn, <-chan gossh.NewChannel, <-chan *gossh.Request, error) {
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, nil, nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
		defer func() { _ = conn.SetDeadline(time.Time{}) }()
	}
	stop := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})
	defer stop()

	c, chans, reqs, err := gossh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, nil, ctxErr
		}
		return nil, nil, nil, err
	}
	return c, chans, reqs, nil
}

// Session implements transport.Session for an interactive SSH PTY or shnex-agent mux session.
type Session struct {
	id           string
	target       string
	createdAt    time.Time
	client       *gossh.Client
	sshSess      *gossh.Session
	stdin        io.WriteCloser
	resizeFn     func(cols, rows uint16) error
	output       *transport.StreamObserver
	pipeReader   *io.PipeReader
	dialerCloser io.Closer
	dispatcher   *transport.EventDispatcher
	unsubNet     func()

	kaInterval  time.Duration
	kaTimeout   time.Duration
	kaMaxMissed int
	probeNowCh  chan string

	resizeMu sync.Mutex

	statsMu   sync.Mutex
	prevStats *telemetrySnapshot

	sftpMu        sync.Mutex
	sftpClient    *sftp.Client
	sftpSubSess   io.Closer
	fileProtocol  string
	sftpSudo      bool
	sudoPassword  string // strictly in-memory only; wiped on Close
	sudoNoPasswd  *bool
	shellFallback bool

	fwdMu    sync.Mutex
	forwards map[string]*activeForward

	closeOnce  sync.Once
	stopCh     chan struct{}
	waitDoneCh chan struct{}
	waitErr    error
}

func (s *Session) ID() string       { return s.id }
func (s *Session) Protocol() string { return ProtocolName }
func (s *Session) Target() string   { return s.target }

func (s *Session) Read(p []byte) (int, error) {
	return s.output.Read(p)
}

func (s *Session) Write(p []byte) (int, error) {
	if bytes.IndexAny(p, "\r\n") >= 0 {
		s.output.NotifyInput()
	}
	return s.stdin.Write(p)
}

func (s *Session) Resize(ctx context.Context, size transport.TerminalSize) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	norm := size.Normalized()
	s.resizeMu.Lock()
	defer s.resizeMu.Unlock()

	select {
	case <-s.stopCh:
		return net.ErrClosed
	default:
	}
	return s.resizeFn(norm.Cols, norm.Rows)
}

// Keepalive sends an out-of-band SSH keepalive request and measures round-trip latency.
func (s *Session) Keepalive(ctx context.Context) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	type result struct {
		rtt time.Duration
		err error
	}
	ch := make(chan result, 1)
	start := time.Now()
	go func() {
		_, _, err := s.client.SendRequest("keepalive@shnex.dev", true, nil)
		ch <- result{rtt: time.Since(start), err: err}
	}()

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-s.stopCh:
		return 0, net.ErrClosed
	case res := <-ch:
		if res.err != nil {
			return 0, res.err
		}
		s.dispatcher.Emit(transport.SessionEvent{
			SessionID: s.id,
			Type:      transport.EventKeepaliveRTT,
			RTT:       res.rtt,
		})
		return res.rtt, nil
	}
}

func (s *Session) SubscribeEvents(hook transport.EventHook) func() {
	return s.dispatcher.Subscribe(hook)
}

func (s *Session) Wait() error {
	<-s.waitDoneCh
	return s.waitErr
}

func (s *Session) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		close(s.stopCh)
		if s.unsubNet != nil {
			s.unsubNet()
		}
		s.closeAllForwards()
		s.sftpMu.Lock()
		if s.sftpClient != nil {
			_ = s.sftpClient.Close()
			s.sftpClient = nil
		}
		if s.sftpSubSess != nil {
			_ = s.sftpSubSess.Close()
			s.sftpSubSess = nil
		}
		s.sudoPassword = ""
		s.sftpMu.Unlock()
		s.output.Close()
		_ = s.pipeReader.Close()
		_ = s.stdin.Close()
		_ = s.sshSess.Close()
		if err := s.client.Close(); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
			closeErr = err
		}
		if s.dialerCloser != nil {
			_ = s.dialerCloser.Close()
		}
	})
	return closeErr
}

func (s *Session) monitorWait() {
	err := s.sshSess.Wait()
	s.waitErr = err
	close(s.waitDoneCh)

	msg := "remote shell exited cleanly"
	if err != nil && !errors.Is(err, io.EOF) {
		msg = err.Error()
	}
	s.dispatcher.Emit(transport.SessionEvent{
		SessionID: s.id,
		Type:      transport.EventDisconnected,
		Message:   msg,
	})
	_ = s.Close()
}

func (s *Session) triggerImmediateProbe(reason string) {
	select {
	case s.probeNowCh <- reason:
	default:
	}
}

func (s *Session) keepaliveLoop() {
	ticker := time.NewTicker(s.kaInterval)
	defer ticker.Stop()

	missed := 0
	for {
		var networkReason string
		isUrgentNetProbe := false

		select {
		case <-s.stopCh:
			return
		case <-s.waitDoneCh:
			return
		case networkReason = <-s.probeNowCh:
			isUrgentNetProbe = true
		case <-ticker.C:
		}

		ctx, cancel := context.WithTimeout(context.Background(), s.kaTimeout)
		rtt, err := s.Keepalive(ctx)
		cancel()

		if err == nil {
			missed = 0
			if isUrgentNetProbe {
				s.dispatcher.Emit(transport.SessionEvent{
					SessionID: s.id,
					Type:      transport.EventNetworkProbed,
					RTT:       rtt,
					Message:   fmt.Sprintf("session survived network change (%s)", networkReason),
				})
			}
			continue
		}

		missed++
		s.dispatcher.Emit(transport.SessionEvent{
			SessionID:   s.id,
			Type:        transport.EventKeepaliveMissed,
			MissedCount: missed,
			Message:     err.Error(),
		})

		if isUrgentNetProbe || missed >= s.kaMaxMissed {
			s.dispatcher.Emit(transport.SessionEvent{
				SessionID: s.id,
				Type:      transport.EventDisconnected,
				Message:   fmt.Sprintf("keepalive failed (%d missed): %v", missed, err),
			})
			_ = s.Close()
			return
		}
	}
}

func normalizeSSHAddr(addr string) string {
	if addr == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(addr, "22")
}

func loadLocalDefaultSSHKeys() []gossh.Signer {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	candidates := []string{
		"id_ed25519",
		"id_ecdsa",
		"id_rsa",
	}
	var signers []gossh.Signer
	for _, name := range candidates {
		raw, rErr := os.ReadFile(filepath.Join(home, ".ssh", name))
		if rErr != nil {
			continue
		}
		if sg, pErr := gossh.ParsePrivateKey(raw); pErr == nil {
			signers = append(signers, sg)
		}
	}
	return signers
}

func randomSessionID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "sess-" + hex.EncodeToString(b[:])
}
