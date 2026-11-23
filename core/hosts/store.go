package hosts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shnex/shnex/core/platform"
	"github.com/shnex/shnex/core/transport"
)

// RouteProfile stores non-sensitive connection routing rules for a saved host.
type RouteProfile struct {
	UseTSNet bool   `json:"use_tsnet,omitempty"`
	ProxyURL string `json:"proxy_url,omitempty"`
}

// HostProfile represents a saved server entry in the Left Host Tree.
// Sensitive credentials (passwords / private keys) are NEVER stored in this struct or JSON;
// they are delegated to PlatformBridge (Windows Credential Manager / Android Keystore) via SecretRef.
type HostProfile struct {
	ID               string                  `json:"id"`
	Name             string                  `json:"name"`
	Protocol         string                  `json:"protocol,omitempty"` // "ssh" | "telnet" | "serial"
	Group            string                  `json:"group,omitempty"`
	Host             string                  `json:"host"`
	Port             int                     `json:"port"`
	User             string                  `json:"user"`
	Icon             string                  `json:"icon,omitempty"`          // vector icon key (e.g. "rocket", "ubuntu", "cloud")
	BadgeText        string                  `json:"badge_text,omitempty"`    // optional emoji or <=4 chars
	Color            string                  `json:"color,omitempty"`         // hex color e.g. "#f59e0b"
	AuthMode         transport.AuthMode      `json:"auth_mode"`
	CredentialID     string                  `json:"credential_id,omitempty"` // optional vault credential ref
	SecretRef        string                  `json:"secret_ref,omitempty"`
	HasSecret        bool                    `json:"has_secret"`
	DefaultDir       string                  `json:"default_dir,omitempty"`
	FileProtocol     string                  `json:"file_protocol,omitempty"`     // "auto" | "sftp" | "scp"
	SFTPSudo         bool                    `json:"sftp_sudo,omitempty"`         // elevate SFTP/file ops via sudo
	TmuxSession      string                  `json:"tmux_session,omitempty"`      // empty = disabled; non-empty = attach/create tmux session
	PostLoginScript  string                  `json:"post_login_script,omitempty"` // executed line-by-line after cd DefaultDir and before tmux
	MetricsBar       string                  `json:"metrics_bar,omitempty"`       // "follow" | "on" | "off"
	TerminalEncoding string                  `json:"terminal_encoding,omitempty"` // "utf-8", "gb18030", "big5", "shift_jis", "euc-kr", "iso-8859-1"
	TermType         string                  `json:"term_type,omitempty"`         // "" (follow global) or explicit TERM e.g. "xterm-256color"
	AutoSessionLog   bool                    `json:"auto_session_log,omitempty"`  // auto-record terminal output to Session Logs pane
	Favorite         bool                    `json:"favorite,omitempty"`          // pinned in Connection Hub favorites
	LastConnectedAt  *time.Time              `json:"last_connected_at,omitempty"` // updated automatically on session connect
	HostKey          transport.HostKeyPolicy `json:"host_key"`
	Route            RouteProfile            `json:"route"`
	Relay            transport.RelayConfig   `json:"relay"`
	UpdatedAt        time.Time               `json:"updated_at"`
}

// UpsertInput carries metadata and optional plaintext secret (written only to PlatformBridge).
type UpsertInput struct {
	ID               string                  `json:"id,omitempty"`
	Name             string                  `json:"name"`
	Protocol         string                  `json:"protocol,omitempty"`
	Group            string                  `json:"group,omitempty"`
	Host             string                  `json:"host"`
	Port             int                     `json:"port"`
	User             string                  `json:"user"`
	Icon             string                  `json:"icon,omitempty"`
	BadgeText        string                  `json:"badge_text,omitempty"`
	Color            string                  `json:"color,omitempty"`
	AuthMode         transport.AuthMode      `json:"auth_mode"`
	CredentialID     string                  `json:"credential_id,omitempty"`
	Password         string                  `json:"password,omitempty"`
	PrivateKeyPEM    string                  `json:"private_key_pem,omitempty"`
	DefaultDir       string                  `json:"default_dir,omitempty"`
	FileProtocol     string                  `json:"file_protocol,omitempty"`
	SFTPSudo         bool                    `json:"sftp_sudo,omitempty"`
	TmuxSession      string                  `json:"tmux_session,omitempty"`
	PostLoginScript  string                  `json:"post_login_script,omitempty"`
	MetricsBar       string                  `json:"metrics_bar,omitempty"`
	TerminalEncoding string                  `json:"terminal_encoding,omitempty"`
	TermType         string                  `json:"term_type,omitempty"`
	AutoSessionLog   bool                    `json:"auto_session_log,omitempty"`
	Favorite         *bool                   `json:"favorite,omitempty"`
	HostKey          transport.HostKeyPolicy `json:"host_key"`
	Route            RouteProfile            `json:"route"`
	Relay            transport.RelayConfig   `json:"relay"`
}

// Snippet represents a reusable quick shell/Claude command snippet.
type Snippet struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Command string `json:"command"`
}

type persistedData struct {
	Hosts    []HostProfile `json:"hosts"`
	Snippets []Snippet     `json:"snippets"`
}

// Store persists non-secret host profiles & command snippets to UserConfigDir/shnex/workspace.json
// while storing secrets strictly via PlatformBridge.
type Store struct {
	filePath string
	bridge   platform.PlatformBridge

	mu       sync.RWMutex
	hosts    map[string]HostProfile
	snippets []Snippet
}

// NewStore initializes a Store at filePath (defaulting to UserConfigDir/shnex/workspace.json when empty).
func NewStore(filePath string, bridge platform.PlatformBridge) (*Store, error) {
	if strings.TrimSpace(filePath) == "" {
		cfgDir, err := os.UserConfigDir()
		if err != nil {
			cfgDir = os.TempDir()
		}
		filePath = filepath.Join(cfgDir, "shnex", "workspace.json")
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		return nil, fmt.Errorf("hosts: create config dir: %w", err)
	}

	s := &Store{
		filePath: filePath,
		bridge:   bridge,
		hosts:    make(map[string]HostProfile),
	}
	_ = s.load()
	if len(s.snippets) == 0 {
		s.snippets = defaultSnippets()
	}
	return s, nil
}

func defaultSnippets() []Snippet {
	return []Snippet{
		{ID: "snip-claude", Title: "启动 Claude CLI", Command: "claude\n"},
		{ID: "snip-tmux-ls", Title: "查看 tmux 会话", Command: "tmux ls\n"},
		{ID: "snip-docker-ps", Title: "查看运行容器", Command: "docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'\n"},
		{ID: "snip-top-mem", Title: "内存占用前 10 进程", Command: "ps aux --sort=-%mem | head -n 11\n"},
	}
}

func (s *Store) List() []HostProfile {
	s.mu.RLock()
	out := make([]HostProfile, 0, len(s.hosts))
	for _, h := range s.hosts {
		out = append(out, h)
	}
	s.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func (s *Store) Get(id string) (HostProfile, bool) {
	s.mu.RLock()
	h, ok := s.hosts[id]
	s.mu.RUnlock()
	return h, ok
}

func (s *Store) Upsert(ctx context.Context, in UpsertInput) (HostProfile, error) {
	host := strings.TrimSpace(in.Host)
	if host == "" {
		return HostProfile{}, errors.New("hosts: host address is required")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = host
	}
	user := strings.TrimSpace(in.User)
	if user == "" {
		user = "root"
	}
	port := in.Port
	if port <= 0 || port > 65535 {
		port = 22
	}
	authMode := in.AuthMode
	if authMode == "" {
		authMode = transport.AuthAuto
	}

	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = randomID("host")
	}

	s.mu.Lock()
	existing, exists := s.hosts[id]
	s.mu.Unlock()

	secretRef := existing.SecretRef
	hasSecret := existing.HasSecret

	secretVal := ""
	if strings.TrimSpace(in.PrivateKeyPEM) != "" {
		secretVal = in.PrivateKeyPEM
		if in.AuthMode == "" {
			authMode = transport.AuthPrivateKey
		}
	} else if in.Password != "" {
		secretVal = in.Password
	}

	if secretVal != "" {
		if s.bridge == nil {
			return HostProfile{}, errors.New("hosts: PlatformBridge required to store secret")
		}
		if secretRef == "" {
			secretRef = fmt.Sprintf("shnex:host:%s:cred", id)
		}
		if err := s.bridge.SetSecret(ctx, secretRef, secretVal); err != nil {
			return HostProfile{}, fmt.Errorf("hosts: save secret to keychain: %w", err)
		}
		hasSecret = true
	} else if !exists {
		secretRef = fmt.Sprintf("shnex:host:%s:cred", id)
	}

	proto := strings.TrimSpace(in.Protocol)
	if proto == "" {
		proto = "ssh"
	}
	fileProto := strings.TrimSpace(in.FileProtocol)
	if fileProto == "" {
		fileProto = "auto"
	}

	metricsBar := strings.TrimSpace(in.MetricsBar)
	if metricsBar == "" {
		metricsBar = "follow"
	}
	termEnc := strings.ToLower(strings.TrimSpace(in.TerminalEncoding))
	if termEnc == "" {
		termEnc = "utf-8"
	}
	favVal := existing.Favorite
	if in.Favorite != nil {
		favVal = *in.Favorite
	}

	profile := HostProfile{
		ID:               id,
		Name:             name,
		Protocol:         proto,
		Group:            strings.TrimSpace(in.Group),
		Host:             host,
		Port:             port,
		User:             user,
		Icon:             strings.TrimSpace(in.Icon),
		BadgeText:        strings.TrimSpace(in.BadgeText),
		Color:            strings.TrimSpace(in.Color),
		AuthMode:         authMode,
		CredentialID:     strings.TrimSpace(in.CredentialID),
		SecretRef:        secretRef,
		HasSecret:        hasSecret,
		DefaultDir:       strings.TrimSpace(in.DefaultDir),
		FileProtocol:     fileProto,
		SFTPSudo:         in.SFTPSudo,
		TmuxSession:      strings.TrimSpace(in.TmuxSession),
		PostLoginScript:  strings.ReplaceAll(in.PostLoginScript, "\r\n", "\n"),
		MetricsBar:       metricsBar,
		TerminalEncoding: termEnc,
		TermType:         strings.TrimSpace(in.TermType),
		AutoSessionLog:   in.AutoSessionLog,
		Favorite:         favVal,
		LastConnectedAt:  existing.LastConnectedAt,
		HostKey:          in.HostKey,
		Route:            in.Route,
		Relay:            in.Relay,
		UpdatedAt:        time.Now(),
	}

	s.mu.Lock()
	s.hosts[id] = profile
	err := s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		return HostProfile{}, err
	}
	return profile, nil
}

func (s *Store) Delete(id string) bool {
	s.mu.Lock()
	_, ok := s.hosts[id]
	if ok {
		delete(s.hosts, id)
		_ = s.saveLocked()
	}
	s.mu.Unlock()
	return ok
}

func (s *Store) ToggleFavorite(id string) (HostProfile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.hosts[id]
	if !ok {
		return HostProfile{}, false
	}
	h.Favorite = !h.Favorite
	s.hosts[id] = h
	_ = s.saveLocked()
	return h, true
}

func (s *Store) TouchLastConnected(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.hosts[id]
	if !ok {
		return
	}
	now := time.Now()
	h.LastConnectedAt = &now
	s.hosts[id] = h
	_ = s.saveLocked()
}

func (s *Store) ListSnippets() []Snippet {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Snippet, len(s.snippets))
	copy(out, s.snippets)
	return out
}

func (s *Store) UpsertSnippet(sn Snippet) ([]Snippet, error) {
	if strings.TrimSpace(sn.Title) == "" || strings.TrimSpace(sn.Command) == "" {
		return nil, errors.New("hosts: snippet title and command are required")
	}
	if strings.TrimSpace(sn.ID) == "" {
		sn.ID = randomID("snip")
	}
	s.mu.Lock()
	replaced := false
	for i := range s.snippets {
		if s.snippets[i].ID == sn.ID {
			s.snippets[i] = sn
			replaced = true
			break
		}
	}
	if !replaced {
		s.snippets = append(s.snippets, sn)
	}
	err := s.saveLocked()
	out := append([]Snippet(nil), s.snippets...)
	s.mu.Unlock()
	return out, err
}

func (s *Store) DeleteSnippet(id string) []Snippet {
	s.mu.Lock()
	filtered := s.snippets[:0]
	for _, sn := range s.snippets {
		if sn.ID != id {
			filtered = append(filtered, sn)
		}
	}
	s.snippets = filtered
	_ = s.saveLocked()
	out := append([]Snippet(nil), s.snippets...)
	s.mu.Unlock()
	return out
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}
	var data persistedData
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	for _, h := range data.Hosts {
		if h.ID != "" {
			s.hosts[h.ID] = h
		}
	}
	if len(data.Snippets) > 0 {
		s.snippets = data.Snippets
	}
	return nil
}

func (s *Store) saveLocked() error {
	data := persistedData{
		Hosts:    make([]HostProfile, 0, len(s.hosts)),
		Snippets: s.snippets,
	}
	for _, h := range s.hosts {
		data.Hosts = append(data.Hosts, h)
	}
	sort.Slice(data.Hosts, func(i, j int) bool {
		return data.Hosts[i].ID < data.Hosts[j].ID
	})

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.filePath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.filePath)
}

func randomID(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return prefix + "-" + hex.EncodeToString(b[:])
}
