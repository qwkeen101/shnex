package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/shnex/shnex/core/dialer"
)

// TerminalSize represents PTY character dimensions and optional pixel geometry.
type TerminalSize struct {
	Cols     uint16 `json:"cols"`
	Rows     uint16 `json:"rows"`
	WidthPx  uint16 `json:"width_px,omitempty"`
	HeightPx uint16 `json:"height_px,omitempty"`
}

// Normalized returns a TerminalSize with sane fallback dimensions (80x24) when unset.
func (s TerminalSize) Normalized() TerminalSize {
	if s.Cols == 0 {
		s.Cols = 80
	}
	if s.Rows == 0 {
		s.Rows = 24
	}
	return s
}

// EventType enumerates lifecycle and stream events emitted by a Session.
type EventType string

const (
	EventConnected          EventType = "connected"
	EventRelayAttached      EventType = "relay_attached"
	EventBell               EventType = "bell"
	EventAIApprovalRequired EventType = "ai_approval_required"
	EventOutputSilence      EventType = "output_silence"
	EventKeepaliveRTT       EventType = "keepalive_rtt"
	EventKeepaliveMissed    EventType = "keepalive_missed"
	EventNetworkProbed      EventType = "network_probed"
	EventDisconnected       EventType = "disconnected"
)

// SessionEvent carries structured telemetry or notification triggers from an active Session.
type SessionEvent struct {
	SessionID     string        `json:"session_id"`
	Type          EventType     `json:"type"`
	Timestamp     time.Time     `json:"timestamp"`
	RTT           time.Duration `json:"rtt_ns,omitempty"`
	QuietFor      time.Duration `json:"quiet_for_ns,omitempty"`
	BurstBytes    uint64        `json:"burst_bytes,omitempty"`
	MissedCount   int           `json:"missed_count,omitempty"`
	PromptSnippet string        `json:"prompt_snippet,omitempty"`
	RelayBackend  string        `json:"relay_backend,omitempty"`
	Message       string        `json:"message,omitempty"`
}

// EventHook receives session lifecycle, keepalive, BEL, and output-silence events.
type EventHook func(ev SessionEvent)

// AuthMode selects how the transport authenticates with the remote host.
type AuthMode string

const (
	AuthAuto         AuthMode = "auto" // Tries local ~/.ssh keys + saved keychain secret; prompts password on failure
	AuthPassword     AuthMode = "password"
	AuthPrivateKey   AuthMode = "private_key"
	AuthTailscaleSSH AuthMode = "tailscale_ssh"
	AuthSecretRef    AuthMode = "secret_ref"
)

// HostKeyPolicy defines how remote host keys are verified during handshake.
type HostKeyPolicy struct {
	// InsecureIgnore disables host key verification (for dev/testing only; must be explicitly opted in).
	InsecureIgnore bool `json:"insecure_ignore,omitempty"`
	// PinnedSHA256 verifies the server key against an exact "SHA256:..." fingerprint.
	PinnedSHA256 string `json:"pinned_sha256,omitempty"`
	// AuthorizedHostKeys verifies the server key against OpenSSH public key lines
	// (such as SSH_HostKeys distributed by Tailscale/Headscale netmap).
	AuthorizedHostKeys []string `json:"authorized_host_keys,omitempty"`
	// KnownHostsPath verifies the server key against an OpenSSH known_hosts file.
	KnownHostsPath string `json:"known_hosts_path,omitempty"`
}

// CredentialsSpec encapsulates authentication material or keychain secret references.
type CredentialsSpec struct {
	Mode                 AuthMode      `json:"mode,omitempty"`
	Password             string        `json:"password,omitempty"`
	PrivateKeyPEM        []byte        `json:"private_key_pem,omitempty"`
	PrivateKeyPassphrase []byte        `json:"private_key_passphrase,omitempty"`
	SecretKeyRef         string        `json:"secret_key_ref,omitempty"`
	HostKey              HostKeyPolicy `json:"host_key"`
}

// RelayMode configures remote session persistence for seamless desktop + mobile handoff.
type RelayMode string

const (
	RelayNone       RelayMode = "none"
	RelayAuto       RelayMode = "auto"        // Probes shnex-agent -> tmux -> zellij -> login shell
	RelayTmux       RelayMode = "tmux"        // Attaches/creates persistent tmux workspace
	RelayZellij     RelayMode = "zellij"      // Attaches/creates persistent zellij workspace
	RelayShnexAgent RelayMode = "shnex_agent" // Connects to remote single-file shnex-agent daemon
)

// RelayConfig specifies how a session persists on the remote host across disconnects/devices.
type RelayConfig struct {
	Mode        RelayMode `json:"mode,omitempty"`
	SessionName string    `json:"session_name,omitempty"`
	AgentPath   string    `json:"agent_path,omitempty"`
	Command     string    `json:"command,omitempty"`
	// MustExist (tmux only): attach to SessionName only if it already exists instead of
	// creating it, so reconnecting to a conversation ended elsewhere cannot resurrect it.
	MustExist bool `json:"must_exist,omitempty"`
}

// SessionConfig contains protocol-neutral parameters for opening an interactive terminal session.
type SessionConfig struct {
	ID                 string
	Target             string // host:port
	User               string
	Dialer             dialer.Dialer
	DialerCloser       io.Closer // optional resource cleanup owned by the session (e.g. jump chain)
	Credentials        CredentialsSpec
	Relay              RelayConfig
	InitialSize        TerminalSize
	Term               string
	Env                map[string]string
	DefaultDir         string // optional initial working directory on login
	PostLoginScript    string // optional line-by-line commands fed after DefaultDir and before tmux attach
	FileProtocol       string // "auto" | "sftp" | "scp"
	SFTPSudo           bool   // run remote sftp-server or shell file ops under sudo
	BuiltinPowerline   bool   // inject built-in Powerline prompt + OSC 7 / OSC 133 markers
	KeepaliveInterval  time.Duration // 0 uses default (15s); < 0 disables periodic keepalive
	KeepaliveTimeout   time.Duration // 0 uses default (5s)
	KeepaliveMaxMissed int           // 0 uses default (3)
	SilenceThreshold   time.Duration // > 0 emits EventOutputSilence once output stops for this duration
	SilenceMinBytes    uint64        // minimum bytes in burst before silence triggers (default 1)
	ApprovalDebounce   time.Duration // settle delay before firing EventAIApprovalRequired (0 -> 450ms when AI detector enabled)
	DisableAIDetector  bool          // when false (default), inspects tail lines for Claude/AI approval prompts
	BellCooldown       time.Duration // optional minimum interval between consecutive EventBell emissions
	InitialHooks       []EventHook
}

// Session is the unified interactive terminal session abstraction.
// Today backed by SSH PTY; future transports (QUIC relay, Mosh-style roam, local PTY)
// implement this exact contract without changing core/api or frontend shells.
type Session interface {
	ID() string
	Protocol() string
	Target() string
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(ctx context.Context, size TerminalSize) error
	Keepalive(ctx context.Context) (time.Duration, error)
	SubscribeEvents(hook EventHook) (unsubscribe func())
	Wait() error
	Close() error
}

// Transport creates interactive terminal sessions over a specific protocol (e.g., "ssh").
type Transport interface {
	Protocol() string
	OpenSession(ctx context.Context, cfg SessionConfig) (Session, error)
}

// Registry manages pluggable Transport implementations by protocol name.
type Registry struct {
	mu         sync.RWMutex
	transports map[string]Transport
}

// NewRegistry creates an empty Transport Registry.
func NewRegistry() *Registry {
	return &Registry{
		transports: make(map[string]Transport),
	}
}

// Register adds or replaces a Transport implementation.
func (r *Registry) Register(t Transport) error {
	if t == nil || t.Protocol() == "" {
		return errors.New("transport: invalid transport registration")
	}
	r.mu.Lock()
	r.transports[t.Protocol()] = t
	r.mu.Unlock()
	return nil
}

// Get retrieves a registered Transport by protocol name.
func (r *Registry) Get(protocol string) (Transport, error) {
	r.mu.RLock()
	t, ok := r.transports[protocol]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("transport: unsupported protocol %q", protocol)
	}
	return t, nil
}

// Protocols returns all registered transport protocol names.
func (r *Registry) Protocols() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.transports))
	for name := range r.transports {
		out = append(out, name)
	}
	return out
}

// EventDispatcher provides concurrency-safe event pub/sub for Session implementations.
type EventDispatcher struct {
	mu     sync.RWMutex
	nextID uint64
	subs   map[uint64]EventHook
}

// NewEventDispatcher initializes an EventDispatcher with optional initial subscribers.
func NewEventDispatcher(initial ...EventHook) *EventDispatcher {
	d := &EventDispatcher{
		subs: make(map[uint64]EventHook),
	}
	for _, fn := range initial {
		if fn != nil {
			d.Subscribe(fn)
		}
	}
	return d
}

// Subscribe registers an EventHook and returns an idempotent unsubscribe function.
func (d *EventDispatcher) Subscribe(hook EventHook) func() {
	if hook == nil {
		return func() {}
	}
	d.mu.Lock()
	d.nextID++
	id := d.nextID
	d.subs[id] = hook
	d.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			d.mu.Lock()
			delete(d.subs, id)
			d.mu.Unlock()
		})
	}
}

// Emit broadcasts ev to all active subscribers outside the internal lock.
func (d *EventDispatcher) Emit(ev SessionEvent) {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}
	d.mu.RLock()
	hooks := make([]EventHook, 0, len(d.subs))
	for _, fn := range d.subs {
		hooks = append(hooks, fn)
	}
	d.mu.RUnlock()

	for _, fn := range hooks {
		fn(ev)
	}
}
