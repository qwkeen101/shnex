package tailnet

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shnex/shnex/core/dialer"
	"github.com/shnex/shnex/core/platform"
	"github.com/shnex/shnex/core/transport"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tsnet"
	"tailscale.com/types/logger"
)

const OfficialControlURL = "https://controlplane.tailscale.com"

// ErrNotStarted indicates that the embedded tsnet engine has not been started yet.
var ErrNotStarted = errors.New("tailnet: tsnet manager is not started")

// Config configures the embedded userspace Tailscale / Headscale (tsnet) node.
type Config struct {
	Hostname      string   `json:"hostname"`
	StateDir      string   `json:"state_dir,omitempty"`
	ControlURL    string   `json:"control_url,omitempty"` // empty = Official Tailscale; custom URL = Headscale
	AuthKey       string   `json:"auth_key,omitempty"`
	AuthSecretRef string   `json:"auth_secret_ref,omitempty"`
	Ephemeral     bool     `json:"ephemeral,omitempty"`
	AdvertiseTags []string `json:"advertise_tags,omitempty"`
	VerboseLog    bool     `json:"verbose_log,omitempty"`
}

// NodeInfo describes a node discovered on the user's Tailnet (including MagicDNS metadata).
type NodeInfo struct {
	ID            string    `json:"id"`
	HostName      string    `json:"hostname"`
	MagicDNSName  string    `json:"magic_dns_name"`
	ShortDNSName  string    `json:"short_dns_name"`
	TailscaleIPs  []string  `json:"tailscale_ips"`
	OS            string    `json:"os"`
	Online        bool      `json:"online"`
	Active        bool      `json:"active"`
	Relay         string    `json:"relay,omitempty"`
	Tags          []string  `json:"tags,omitempty"`
	SSHHostKeys   []string  `json:"ssh_host_keys,omitempty"`
	SupportsTSSSH bool      `json:"supports_tailscale_ssh"`
	LastSeen      time.Time `json:"last_seen,omitempty"`
}

// StatusSnapshot reports the current state of the embedded tsnet engine.
type StatusSnapshot struct {
	Running        bool      `json:"running"`
	BackendState   string    `json:"backend_state"`
	ControlURL     string    `json:"control_url"`
	AuthURL        string    `json:"auth_url,omitempty"`
	MagicDNSSuffix string    `json:"magic_dns_suffix,omitempty"`
	Self           *NodeInfo `json:"self,omitempty"`
	Health         []string  `json:"health,omitempty"`
}

// TailscaleSSHMode represents the reserved passwordless / identity-based Tailscale SSH mode.
type TailscaleSSHMode string

const (
	TailscaleSSHAuto             TailscaleSSHMode = "auto"
	TailscaleSSHCheckInteractive TailscaleSSHMode = "check_interactive"
)

// TailscaleSSHPlan bundles the tsnet Dialer and host-key / auth configuration needed to
// connect to a peer using Tailscale SSH (WireGuard node identity rather than static SSH keys).
type TailscaleSSHPlan struct {
	TargetAddr         string                    `json:"target_addr"`
	User               string                    `json:"user"`
	Mode               TailscaleSSHMode          `json:"mode"`
	Peer               *NodeInfo                 `json:"peer,omitempty"`
	AuthorizedHostKeys []string                  `json:"authorized_host_keys,omitempty"`
	Dialer             dialer.Dialer             `json:"-"`
	Credentials        transport.CredentialsSpec `json:"-"`
}

// backend abstracts tsnet.Server + local.Client so Manager can be deterministically unit-tested.
type backend interface {
	Start(ctx context.Context) error
	StartInteractiveLogin(ctx context.Context) error
	Status(ctx context.Context) (*ipnstate.Status, string, error)
	Dial(ctx context.Context, network, addr string) (net.Conn, error)
	Close() error
}

type backendFactory func(cfg Config) (backend, error)

// Manager manages the lifecycle of an embedded userspace Tailscale / Headscale node.
type Manager struct {
	bridge     platform.PlatformBridge
	netChanges *platform.NetworkBroadcaster
	factory    backendFactory

	mu      sync.RWMutex
	cfg     Config
	be      backend
	unsubNW func()
}

// NewManager creates a Tailnet Manager decoupled from host keychain via PlatformBridge.
func NewManager(bridge platform.PlatformBridge, netChanges *platform.NetworkBroadcaster) *Manager {
	return &Manager{
		bridge:     bridge,
		netChanges: netChanges,
		factory:    newTSNetBackend,
	}
}

// Start initializes (or restarts with updated config) the userspace tsnet instance.
// If AuthKey is omitted and no prior node state exists, interactive login can be initiated
// immediately or via StartInteractiveLogin, exposing AuthURL in Status().
func (m *Manager) Start(ctx context.Context, cfg Config) (*StatusSnapshot, error) {
	if strings.TrimSpace(cfg.Hostname) == "" {
		cfg.Hostname = "shnex-client"
	}
	cfg.ControlURL = strings.TrimSpace(cfg.ControlURL)

	if cfg.AuthKey == "" && cfg.AuthSecretRef != "" {
		if m.bridge == nil {
			return nil, errors.New("tailnet: auth_secret_ref specified, but PlatformBridge is nil")
		}
		secret, err := m.bridge.GetSecret(ctx, cfg.AuthSecretRef)
		if err != nil {
			return nil, fmt.Errorf("tailnet: read auth key from bridge (%s): %w", cfg.AuthSecretRef, err)
		}
		cfg.AuthKey = strings.TrimSpace(secret)
	}

	if strings.TrimSpace(cfg.StateDir) == "" {
		baseDir, err := os.UserConfigDir()
		if err != nil {
			baseDir = os.TempDir()
		}
		safeHost := sanitizeFilename(cfg.Hostname)
		cfg.StateDir = filepath.Join(baseDir, "shnex", "tsnet-"+safeHost)
	}
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		return nil, fmt.Errorf("tailnet: create state dir: %w", err)
	}

	be, err := m.factory(cfg)
	if err != nil {
		return nil, err
	}
	if err := be.Start(ctx); err != nil {
		_ = be.Close()
		return nil, fmt.Errorf("tailnet: start tsnet: %w", err)
	}

	m.mu.Lock()
	old := m.be
	oldUnsub := m.unsubNW
	m.cfg = cfg
	m.be = be
	if m.netChanges != nil {
		m.unsubNW = m.netChanges.Subscribe(func(_ platform.NetworkChangeEvent) {
			// Poke status check asynchronously on host network transitions.
			go func() {
				probeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_, _ = m.Status(probeCtx)
			}()
		})
	}
	m.mu.Unlock()

	if oldUnsub != nil {
		oldUnsub()
	}
	if old != nil {
		_ = old.Close()
	}

	// If no AuthKey was supplied and backend requires login, kick off interactive auth URL request.
	st, err := m.Status(ctx)
	if err == nil && cfg.AuthKey == "" && st.BackendState == "NeedsLogin" && st.AuthURL == "" {
		_ = be.StartInteractiveLogin(ctx)
		// Brief non-blocking check in case AuthURL arrived quickly.
		if updated, sErr := m.Status(ctx); sErr == nil {
			st = updated
		}
	}
	return st, err
}

// StartInteractiveLogin explicitly requests an OAuth / Headscale interactive login URL.
func (m *Manager) StartInteractiveLogin(ctx context.Context) (*StatusSnapshot, error) {
	m.mu.RLock()
	be := m.be
	m.mu.RUnlock()
	if be == nil {
		return nil, ErrNotStarted
	}
	if err := be.StartInteractiveLogin(ctx); err != nil {
		return nil, fmt.Errorf("tailnet: interactive login: %w", err)
	}
	return m.Status(ctx)
}

// Stop shuts down the embedded tsnet engine cleanly.
func (m *Manager) Stop() error {
	m.mu.Lock()
	be := m.be
	unsub := m.unsubNW
	m.be = nil
	m.unsubNW = nil
	m.mu.Unlock()

	if unsub != nil {
		unsub()
	}
	if be != nil {
		return be.Close()
	}
	return nil
}

// Dial dials over the userspace Tailnet mesh, satisfying dialer.TSNetNode.
func (m *Manager) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	m.mu.RLock()
	be := m.be
	m.mu.RUnlock()
	if be == nil {
		return nil, ErrNotStarted
	}
	return be.Dial(ctx, network, address)
}

// Dialer returns a composable dialer.Dialer bound to this Tailnet Manager.
func (m *Manager) Dialer() (*dialer.TSNetDialer, error) {
	return dialer.NewTSNetDialer(m)
}

// Status returns the current state, ControlURL, MagicDNS suffix, and interactive AuthURL (if pending).
func (m *Manager) Status(ctx context.Context) (*StatusSnapshot, error) {
	m.mu.RLock()
	be := m.be
	cfg := m.cfg
	m.mu.RUnlock()

	effectiveControl := cfg.ControlURL
	if effectiveControl == "" {
		effectiveControl = OfficialControlURL
	}

	if be == nil {
		return &StatusSnapshot{
			Running:      false,
			BackendState: "Stopped",
			ControlURL:   effectiveControl,
		}, nil
	}

	raw, busAuthURL, err := be.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("tailnet: query status: %w", err)
	}

	var magicSuffix string
	if raw.CurrentTailnet != nil {
		magicSuffix = strings.TrimSuffix(raw.CurrentTailnet.MagicDNSSuffix, ".")
	}

	authURL := raw.AuthURL
	if authURL == "" {
		authURL = busAuthURL
	}

	snap := &StatusSnapshot{
		Running:        raw.BackendState == "Running",
		BackendState:   raw.BackendState,
		ControlURL:     effectiveControl,
		AuthURL:        authURL,
		MagicDNSSuffix: magicSuffix,
		Health:         append([]string(nil), raw.Health...),
	}
	if raw.Self != nil {
		selfNode := peerToNodeInfo(raw.Self, magicSuffix)
		snap.Self = &selfNode
	}
	return snap, nil
}

// ListNodes returns Tailnet peers sorted by online state and hostname.
// When onlineOnly is true, offline or expired peers are excluded.
func (m *Manager) ListNodes(ctx context.Context, onlineOnly bool) ([]NodeInfo, error) {
	m.mu.RLock()
	be := m.be
	m.mu.RUnlock()
	if be == nil {
		return nil, ErrNotStarted
	}

	raw, _, err := be.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("tailnet: query peers: %w", err)
	}

	var magicSuffix string
	if raw.CurrentTailnet != nil {
		magicSuffix = strings.TrimSuffix(raw.CurrentTailnet.MagicDNSSuffix, ".")
	}

	nodes := make([]NodeInfo, 0, len(raw.Peer))
	for _, peer := range raw.Peer {
		if peer == nil || peer.ShareeNode || peer.Expired {
			continue
		}
		if onlineOnly && !peer.Online {
			continue
		}
		nodes = append(nodes, peerToNodeInfo(peer, magicSuffix))
	}

	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Online != nodes[j].Online {
			return nodes[i].Online
		}
		return strings.ToLower(nodes[i].HostName) < strings.ToLower(nodes[j].HostName)
	})
	return nodes, nil
}

// PrepareTailscaleSSH builds a passwordless / identity-authenticated SSH connection plan
// over tsnet. When the target peer publishes SSH_HostKeys in the Tailscale/Headscale netmap,
// host key verification is automatically pinned to those netmap keys.
//
// Reserved extension: full check-mode browser callback flow (TailscaleSSHCheckInteractive).
func (m *Manager) PrepareTailscaleSSH(ctx context.Context, targetHostOrIP, user string, mode TailscaleSSHMode, allowInsecureFallback bool) (*TailscaleSSHPlan, error) {
	targetHostOrIP = strings.TrimSpace(targetHostOrIP)
	user = strings.TrimSpace(user)
	if targetHostOrIP == "" {
		return nil, errors.New("tailnet: target host must not be empty")
	}
	if user == "" {
		return nil, errors.New("tailnet: ssh user must not be empty")
	}
	if mode == "" {
		mode = TailscaleSSHAuto
	}

	nodes, err := m.ListNodes(ctx, false)
	if err != nil {
		return nil, err
	}

	hostOnly, port, splitErr := net.SplitHostPort(targetHostOrIP)
	if splitErr != nil {
		hostOnly = targetHostOrIP
		port = "22"
	}

	var matched *NodeInfo
	needle := strings.ToLower(strings.TrimSuffix(hostOnly, "."))
	for i := range nodes {
		n := &nodes[i]
		if strings.ToLower(n.HostName) == needle ||
			strings.ToLower(n.ShortDNSName) == needle ||
			strings.ToLower(n.MagicDNSName) == needle {
			matched = n
			break
		}
		for _, ip := range n.TailscaleIPs {
			if ip == needle {
				matched = n
				break
			}
		}
		if matched != nil {
			break
		}
	}

	dialTarget := net.JoinHostPort(hostOnly, port)
	var hostKeys []string
	if matched != nil {
		if matched.MagicDNSName != "" {
			dialTarget = net.JoinHostPort(matched.MagicDNSName, port)
		} else if len(matched.TailscaleIPs) > 0 {
			dialTarget = net.JoinHostPort(matched.TailscaleIPs[0], port)
		}
		hostKeys = append(hostKeys, matched.SSHHostKeys...)
	}

	tsd, err := m.Dialer()
	if err != nil {
		return nil, err
	}

	hkPolicy := transport.HostKeyPolicy{
		AuthorizedHostKeys: hostKeys,
	}
	if len(hostKeys) == 0 && allowInsecureFallback {
		hkPolicy.InsecureIgnore = true
	}

	return &TailscaleSSHPlan{
		TargetAddr:         dialTarget,
		User:               user,
		Mode:               mode,
		Peer:               matched,
		AuthorizedHostKeys: hostKeys,
		Dialer:             tsd,
		Credentials: transport.CredentialsSpec{
			Mode:    transport.AuthTailscaleSSH,
			HostKey: hkPolicy,
		},
	}, nil
}

func peerToNodeInfo(p *ipnstate.PeerStatus, magicSuffix string) NodeInfo {
	fqdn := strings.TrimSuffix(p.DNSName, ".")
	shortName := fqdn
	if magicSuffix != "" && strings.HasSuffix(strings.ToLower(fqdn), "."+strings.ToLower(magicSuffix)) {
		shortName = fqdn[:len(fqdn)-len(magicSuffix)-1]
	} else if idx := strings.IndexByte(fqdn, '.'); idx > 0 {
		shortName = fqdn[:idx]
	}
	if shortName == "" {
		shortName = p.HostName
	}

	ips := make([]string, 0, len(p.TailscaleIPs))
	for _, ip := range p.TailscaleIPs {
		ips = append(ips, ip.String())
	}

	var tags []string
	if p.Tags != nil {
		tags = append(tags, p.Tags.AsSlice()...)
	}

	hostKeys := append([]string(nil), p.SSH_HostKeys...)
	return NodeInfo{
		ID:            string(p.ID),
		HostName:      p.HostName,
		MagicDNSName:  fqdn,
		ShortDNSName:  shortName,
		TailscaleIPs:  ips,
		OS:            p.OS,
		Online:        p.Online,
		Active:        p.Active,
		Relay:         p.Relay,
		Tags:          tags,
		SSHHostKeys:   hostKeys,
		SupportsTSSSH: len(hostKeys) > 0,
		LastSeen:      p.LastSeen,
	}
}

// --- Concrete tsnet Backend Implementation ---

type tsnetBackend struct {
	cfg Config
	srv *tsnet.Server

	mu         sync.RWMutex
	busAuthURL string
	cancelBus  context.CancelFunc
}

func newTSNetBackend(cfg Config) (backend, error) {
	b := &tsnetBackend{cfg: cfg}
	logFn := logger.Discard
	if cfg.VerboseLog {
		logFn = log.Printf
	}

	b.srv = &tsnet.Server{
		Dir:           cfg.StateDir,
		Hostname:      cfg.Hostname,
		ControlURL:    cfg.ControlURL,
		AuthKey:       cfg.AuthKey,
		Ephemeral:     cfg.Ephemeral,
		AdvertiseTags: append([]string(nil), cfg.AdvertiseTags...),
		Logf:          logFn,
		UserLogf: func(format string, args ...any) {
			msg := fmt.Sprintf(format, args...)
			if idx := strings.Index(msg, "https://"); idx >= 0 {
				urlPart := strings.Fields(msg[idx:])[0]
				b.setBusAuthURL(urlPart)
			} else if idx := strings.Index(msg, "http://"); idx >= 0 {
				urlPart := strings.Fields(msg[idx:])[0]
				b.setBusAuthURL(urlPart)
			}
			if cfg.VerboseLog {
				log.Printf("[tsnet] %s", msg)
			}
		},
	}
	return b, nil
}

func (b *tsnetBackend) Start(ctx context.Context) error {
	if err := b.srv.Start(); err != nil {
		return err
	}

	lc, err := b.srv.LocalClient()
	if err != nil {
		return err
	}

	busCtx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.cancelBus = cancel
	b.mu.Unlock()

	// Watch IPN bus asynchronously so interactive BrowseToURL notifications from
	// Tailscale or Headscale are captured immediately.
	go func() {
		watcher, wErr := lc.WatchIPNBus(busCtx, ipn.NotifyInitialState)
		if wErr != nil {
			return
		}
		defer watcher.Close()
		for {
			n, rErr := watcher.Next()
			if rErr != nil {
				return
			}
			if n.BrowseToURL != nil && *n.BrowseToURL != "" {
				b.setBusAuthURL(*n.BrowseToURL)
			}
		}
	}()

	// Ensure WantRunning=true when an AuthKey or existing session is configured.
	if b.cfg.AuthKey != "" {
		_, _ = lc.EditPrefs(ctx, &ipn.MaskedPrefs{
			Prefs: ipn.Prefs{
				WantRunning: true,
			},
			WantRunningSet: true,
		})
	}
	return nil
}

func (b *tsnetBackend) StartInteractiveLogin(ctx context.Context) error {
	lc, err := b.srv.LocalClient()
	if err != nil {
		return err
	}
	return lc.StartLoginInteractive(ctx)
}

func (b *tsnetBackend) Status(ctx context.Context) (*ipnstate.Status, string, error) {
	lc, err := b.srv.LocalClient()
	if err != nil {
		return nil, "", err
	}
	st, err := lc.Status(ctx)
	if err != nil {
		return nil, "", err
	}
	b.mu.RLock()
	url := b.busAuthURL
	b.mu.RUnlock()
	return st, url, nil
}

func (b *tsnetBackend) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	return b.srv.Dial(ctx, network, addr)
}

func (b *tsnetBackend) Close() error {
	b.mu.Lock()
	cancel := b.cancelBus
	b.cancelBus = nil
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return b.srv.Close()
}

func (b *tsnetBackend) setBusAuthURL(u string) {
	b.mu.Lock()
	b.busAuthURL = strings.TrimSpace(u)
	b.mu.Unlock()
}

func sanitizeFilename(s string) string {
	var out strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			out.WriteRune(r)
		} else {
			out.WriteByte('_')
		}
	}
	if out.Len() == 0 {
		return "default"
	}
	return out.String()
}
