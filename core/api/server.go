package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/shnex/shnex/core/ai"
	"github.com/shnex/shnex/core/completion"
	"github.com/shnex/shnex/core/dialer"
	"github.com/shnex/shnex/core/hosts"
	"github.com/shnex/shnex/core/localpty"
	"github.com/shnex/shnex/core/platform"
	"github.com/shnex/shnex/core/settings"
	shnexssh "github.com/shnex/shnex/core/ssh"
	"github.com/shnex/shnex/core/tailnet"
	"github.com/shnex/shnex/core/transport"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const DefaultListenAddr = "127.0.0.1:0"

// ErrNonLoopbackAddress enforces the architectural security redline:
// core MUST ONLY listen on 127.0.0.1 loopback, never 0.0.0.0 or external interfaces.
var ErrNonLoopbackAddress = errors.New("api: listen address must be an IPv4 loopback address (127.0.0.1)")

// Config configures the in-process local bridge Server.
type Config struct {
	// ListenAddr defaults to "127.0.0.1:0". Non-loopback addresses are strictly rejected.
	ListenAddr string
	// Bridge connects core to OS keychain and desktop/mobile notifications.
	// Defaults to platform.NewMemoryBridge(nil) when nil.
	Bridge platform.PlatformBridge
	// Transports allows registering additional transport protocols alongside "ssh".
	Transports *transport.Registry
	// Tailnet allows passing an existing *tailnet.Manager; created automatically when nil.
	Tailnet *tailnet.Manager
	// AllowedOrigins permits trusted local WebView origins (e.g. "http://wails.localhost")
	// to perform authenticated CORS / WebSocket requests against 127.0.0.1.
	AllowedOrigins []string
	// WorkspacePath overrides where host profiles & snippets JSON is stored (used in tests).
	WorkspacePath string
	// RootHandler optionally serves "/" (used ONLY by cmd/devserver for browser testing).
	RootHandler http.Handler
}

// Server hosts the 127.0.0.1 HTTP control plane and binary WebSocket terminal data plane.
type Server struct {
	listenCfgAddr  string
	authToken      string
	allowedOrigins []string
	startedAt      time.Time

	bridge     platform.PlatformBridge
	netChanges *platform.NetworkBroadcaster
	transports    *transport.Registry
	tailnetMgr    *tailnet.Manager
	hostStore     *hosts.Store
	settingsStore *settings.Store
	aiService     *ai.Service
	compEngine    *completion.Engine
	logsDir       string
	rootHdlr      http.Handler

	listener net.Listener
	httpSrv  *http.Server

	mu       sync.RWMutex
	sessions map[string]*managedSession
}

// NewServer validates configuration, generates a 32-byte cryptographic authToken,
// and wires default SSH and Tailnet subsystems.
func NewServer(cfg Config) (*Server, error) {
	listenAddr := strings.TrimSpace(cfg.ListenAddr)
	if listenAddr == "" {
		listenAddr = DefaultListenAddr
	}
	if err := validateLoopbackAddr(listenAddr); err != nil {
		return nil, err
	}

	token, err := generateAuthToken32()
	if err != nil {
		return nil, err
	}

	bridge := cfg.Bridge
	if bridge == nil {
		bridge = platform.NewMemoryBridge(nil)
	}
	netChanges := platform.NewNetworkBroadcaster()

	reg := cfg.Transports
	if reg == nil {
		reg = transport.NewRegistry()
	}
	if _, err := reg.Get(shnexssh.ProtocolName); err != nil {
		_ = reg.Register(shnexssh.NewTransport(bridge, netChanges))
	}
	if _, err := reg.Get(localpty.ProtocolName); err != nil {
		_ = reg.Register(localpty.NewTransport())
	}

	tn := cfg.Tailnet
	if tn == nil {
		tn = tailnet.NewManager(bridge, netChanges)
	}

	hStore, err := hosts.NewStore(cfg.WorkspacePath, bridge)
	if err != nil {
		return nil, err
	}
	settingsPath := ""
	logsDir := ""
	if cfg.WorkspacePath != "" {
		settingsPath = cfg.WorkspacePath + ".settings.json"
		logsDir = filepath.Join(filepath.Dir(cfg.WorkspacePath), "session_logs")
	} else {
		cfgDir, dErr := os.UserConfigDir()
		if dErr != nil {
			cfgDir = os.TempDir()
		}
		logsDir = filepath.Join(cfgDir, "shnex", "session_logs")
	}
	setStore, err := settings.NewStore(settingsPath, bridge)
	if err != nil {
		return nil, err
	}
	aiSvc := ai.NewService("", bridge, setStore)
	compEng := completion.NewEngine(func() []completion.SSHHostHint {
		list := hStore.List()
		hints := make([]completion.SSHHostHint, 0, len(list)*2)
		for _, hp := range list {
			addr := hp.User + "@" + hp.Host
			if hp.Name != "" && !strings.ContainsAny(hp.Name, " \t") {
				hints = append(hints, completion.SSHHostHint{
					Alias:  hp.Name,
					Detail: addr,
				})
			}
			hints = append(hints, completion.SSHHostHint{
				Alias:  addr,
				Detail: hp.Name,
			})
		}
		return hints
	})

	s := &Server{
		listenCfgAddr:  listenAddr,
		authToken:      token,
		allowedOrigins: append([]string(nil), cfg.AllowedOrigins...),
		bridge:         bridge,
		netChanges:     netChanges,
		transports:     reg,
		tailnetMgr:     tn,
		hostStore:      hStore,
		settingsStore:  setStore,
		aiService:      aiSvc,
		compEngine:     compEng,
		logsDir:        logsDir,
		rootHdlr:       cfg.RootHandler,
		sessions:       make(map[string]*managedSession),
	}
	return s, nil
}

// Start binds 127.0.0.1 and begins serving HTTP and WebSocket requests in the background.
func (s *Server) Start() error {
	if err := validateLoopbackAddr(s.listenCfgAddr); err != nil {
		return err
	}
	ln, err := net.Listen("tcp4", s.listenCfgAddr)
	if err != nil {
		return fmt.Errorf("api: listen %s: %w", s.listenCfgAddr, err)
	}

	s.mu.Lock()
	s.listener = ln
	s.startedAt = time.Now()
	s.httpSrv = &http.Server{
		Handler:           s.buildMux(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	srv := s.httpSrv
	s.mu.Unlock()

	go func() {
		_ = srv.Serve(ln)
	}()
	return nil
}

// Addr returns the bound "127.0.0.1:<port>" address once Started.
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// AuthToken returns the 32-byte (64 hex chars) per-instance bearer token.
func (s *Server) AuthToken() string {
	return s.authToken
}

// Bridge returns the configured PlatformBridge.
func (s *Server) Bridge() platform.PlatformBridge {
	return s.bridge
}

// NotifyNetworkChanged notifies all active sessions and transports that the host
// woke from sleep or switched networks (Wi-Fi / hotspot / cellular).
func (s *Server) NotifyNetworkChanged() platform.NetworkChangeEvent {
	return s.netChanges.NotifyNetworkChanged("host_network_changed")
}

// NotifyNetworkChangedWithReason is identical to NotifyNetworkChanged with a diagnostic reason tag.
func (s *Server) NotifyNetworkChangedWithReason(reason string) platform.NetworkChangeEvent {
	if strings.TrimSpace(reason) == "" {
		reason = "host_network_changed"
	}
	return s.netChanges.NotifyNetworkChanged(reason)
}

// Shutdown gracefully closes the HTTP listener, active terminal sessions, and embedded tsnet.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	httpSrv := s.httpSrv
	sessions := make([]*managedSession, 0, len(s.sessions))
	for _, ms := range s.sessions {
		sessions = append(sessions, ms)
	}
	s.sessions = make(map[string]*managedSession)
	s.mu.Unlock()

	for _, ms := range sessions {
		ms.close()
	}

	var errs []error
	if err := s.tailnetMgr.Stop(); err != nil {
		errs = append(errs, err)
	}
	if httpSrv != nil {
		if err := httpSrv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Server) buildMux() http.Handler {
	mux := http.NewServeMux()

	if s.rootHdlr != nil {
		mux.Handle("GET /{$}", s.rootHdlr)
	}

	// Authenticated Control Plane REST Endpoints
	mux.HandleFunc("GET /api/v1/health", s.requireBearer(s.handleHealth))
	mux.HandleFunc("POST /api/v1/network/changed", s.requireBearer(s.handleNetworkChanged))
	mux.HandleFunc("POST /api/v1/secrets", s.requireBearer(s.handleSetSecret))
	mux.HandleFunc("GET /api/v1/notifications", s.requireBearer(s.handleListNotifications))
	mux.HandleFunc("POST /api/v1/notifications/test", s.requireBearer(s.handleSendTestNotification))

	mux.HandleFunc("GET /api/v1/sessions", s.requireBearer(s.handleListSessions))
	mux.HandleFunc("POST /api/v1/sessions", s.requireBearer(s.handleCreateSession))
	mux.HandleFunc("POST /api/v1/sessions/{id}/resize", s.requireBearer(s.handleResizeSession))
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", s.requireBearer(s.handleDeleteSession))
	mux.HandleFunc("GET /api/v1/sessions/{id}/stats", s.requireBearer(s.handleSessionStats))
	mux.HandleFunc("GET /api/v1/sessions/{id}/scrollback", s.requireBearer(s.handleSessionScrollback))
	mux.HandleFunc("POST /api/v1/sessions/{id}/exec", s.requireBearer(s.handleSessionExec))
	mux.HandleFunc("POST /api/v1/sessions/{id}/complete", s.requireBearer(s.handleSessionComplete))
	mux.HandleFunc("POST /api/v1/sessions/{id}/complete/accept", s.requireBearer(s.handleSessionCompleteAccept))
	mux.HandleFunc("GET /api/v1/sessions/{id}/forwards", s.requireBearer(s.handleListForwards))
	mux.HandleFunc("POST /api/v1/sessions/{id}/forwards", s.requireBearer(s.handleStartForward))
	mux.HandleFunc("DELETE /api/v1/sessions/{id}/forwards/{fwdId}", s.requireBearer(s.handleStopForward))

	mux.HandleFunc("GET /api/v1/sessions/{id}/sftp/list", s.requireBearer(s.handleSFTPList))
	mux.HandleFunc("GET /api/v1/sessions/{id}/sftp/read", s.requireBearer(s.handleSFTPRead))
	mux.HandleFunc("POST /api/v1/sessions/{id}/sftp/write", s.requireBearer(s.handleSFTPWrite))
	mux.HandleFunc("POST /api/v1/sessions/{id}/sftp/upload", s.requireBearer(s.handleSFTPUploadRaw))
	mux.HandleFunc("POST /api/v1/sessions/{id}/sftp/upload-local", s.requireBearer(s.handleSFTPUploadLocal))
	mux.HandleFunc("POST /api/v1/sessions/{id}/sftp/mkdir", s.requireBearer(s.handleSFTPMkdir))
	mux.HandleFunc("DELETE /api/v1/sessions/{id}/sftp/remove", s.requireBearer(s.handleSFTPRemove))
	mux.HandleFunc("POST /api/v1/sessions/{id}/sftp/sudo-password", s.requireBearer(s.handleSFTPSudoPassword))
	mux.HandleFunc("POST /api/v1/sessions/{id}/upload-image", s.requireBearer(s.handleSessionUploadImage))

	mux.HandleFunc("GET /api/v1/session-logs", s.requireBearer(s.handleListSessionLogs))
	mux.HandleFunc("GET /api/v1/session-logs/{name}", s.requireBearer(s.handleGetSessionLog))
	mux.HandleFunc("DELETE /api/v1/session-logs/{name}", s.requireBearer(s.handleDeleteSessionLog))

	mux.HandleFunc("GET /api/v1/hosts", s.requireBearer(s.handleListHosts))
	mux.HandleFunc("POST /api/v1/hosts", s.requireBearer(s.handleUpsertHost))
	mux.HandleFunc("POST /api/v1/hosts/import-ssh-config", s.requireBearer(s.handleImportSSHConfig))
	mux.HandleFunc("POST /api/v1/hosts/ping", s.requireBearer(s.handlePingAllHosts))
	mux.HandleFunc("POST /api/v1/hosts/{id}/favorite", s.requireBearer(s.handleToggleFavoriteHost))
	mux.HandleFunc("DELETE /api/v1/hosts/{id}", s.requireBearer(s.handleDeleteHost))

	mux.HandleFunc("GET /api/v1/snippets", s.requireBearer(s.handleListSnippets))
	mux.HandleFunc("POST /api/v1/snippets", s.requireBearer(s.handleUpsertSnippet))
	mux.HandleFunc("DELETE /api/v1/snippets/{id}", s.requireBearer(s.handleDeleteSnippet))

	mux.HandleFunc("GET /api/v1/markdown/sample", s.requireBearer(s.handleMarkdownSample))
	mux.HandleFunc("POST /api/v1/markdown/render", s.requireBearer(s.handleMarkdownRender))
	mux.HandleFunc("POST /api/v1/markdown/local/read", s.requireBearer(s.handleMarkdownReadLocal))
	mux.HandleFunc("POST /api/v1/markdown/local/write", s.requireBearer(s.handleMarkdownWriteLocal))
	mux.HandleFunc("GET /api/v1/markdown/asset", s.handleMarkdownAsset)

	mux.HandleFunc("GET /api/v1/ai/config", s.requireBearer(s.handleGetAIConfig))
	mux.HandleFunc("POST /api/v1/ai/config", s.requireBearer(s.handleUpdateAIConfig))
	mux.HandleFunc("POST /api/v1/sessions/{id}/ai/chat", s.requireBearer(s.handleSessionAIChat))
	mux.HandleFunc("POST /api/v1/sessions/{id}/ai/approve", s.requireBearer(s.handleSessionAIApprove))

	mux.HandleFunc("GET /api/v1/settings", s.requireBearer(s.handleGetSettings))
	mux.HandleFunc("PUT /api/v1/settings", s.requireBearer(s.handleUpdateSettings))
	mux.HandleFunc("GET /api/v1/settings/wallpaper", s.handleGetWallpaper)
	mux.HandleFunc("POST /api/v1/settings/wallpaper", s.requireBearer(s.handleUploadWallpaper))
	mux.HandleFunc("DELETE /api/v1/settings/wallpaper", s.requireBearer(s.handleDeleteWallpaper))
	mux.HandleFunc("POST /api/v1/settings/security/set-master", s.requireBearer(s.handleSetMasterPassword))
	mux.HandleFunc("POST /api/v1/settings/security/lock", s.requireBearer(s.handleLockVault))
	mux.HandleFunc("POST /api/v1/settings/security/unlock", s.requireBearer(s.handleUnlockVault))
	mux.HandleFunc("GET /api/v1/known-hosts", s.requireBearer(s.handleListKnownHosts))
	mux.HandleFunc("DELETE /api/v1/known-hosts/{line}", s.requireBearer(s.handleDeleteKnownHost))
	mux.HandleFunc("POST /api/v1/settings/ai/providers", s.requireBearer(s.handleUpsertAIProvider))
	mux.HandleFunc("POST /api/v1/settings/ai/providers/{id}/default", s.requireBearer(s.handleSetDefaultAIProvider))
	mux.HandleFunc("DELETE /api/v1/settings/ai/providers/{id}", s.requireBearer(s.handleDeleteAIProvider))
	mux.HandleFunc("POST /api/v1/settings/credentials", s.requireBearer(s.handleAddCredential))
	mux.HandleFunc("DELETE /api/v1/settings/credentials/{id}", s.requireBearer(s.handleDeleteCredential))
	mux.HandleFunc("POST /api/v1/settings/proxies", s.requireBearer(s.handleUpsertProxy))
	mux.HandleFunc("DELETE /api/v1/settings/proxies/{id}", s.requireBearer(s.handleDeleteProxy))
	mux.HandleFunc("GET /api/v1/settings/backup/export", s.requireBearer(s.handleBackupExport))
	mux.HandleFunc("POST /api/v1/settings/backup/import", s.requireBearer(s.handleBackupImport))

	mux.HandleFunc("GET /api/v1/tailnet/status", s.requireBearer(s.handleTailnetStatus))
	mux.HandleFunc("POST /api/v1/tailnet/up", s.requireBearer(s.handleTailnetUp))
	mux.HandleFunc("POST /api/v1/tailnet/login", s.requireBearer(s.handleTailnetLogin))
	mux.HandleFunc("POST /api/v1/tailnet/down", s.requireBearer(s.handleTailnetDown))
	mux.HandleFunc("GET /api/v1/tailnet/nodes", s.requireBearer(s.handleTailnetNodes))

	// Binary WebSocket Data Plane Endpoint
	mux.HandleFunc("GET /api/v1/sessions/{id}/ws", s.handleSessionWS)

	return s.withCORS(mux)
}

func (s *Server) isAllowedOrigin(origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return true
	}
	lower := strings.ToLower(origin)
	if lower == "http://"+strings.ToLower(s.Addr()) {
		return true
	}
	if strings.HasPrefix(lower, "http://wails.localhost") ||
		strings.HasPrefix(lower, "https://wails.localhost") ||
		strings.HasPrefix(lower, "wails://") {
		return true
	}
	for _, allowed := range s.allowedOrigins {
		if strings.EqualFold(origin, strings.TrimSpace(allowed)) {
			return true
		}
	}
	return false
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" && s.isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-File-Name")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			if origin != "" && !s.isAllowedOrigin(origin) {
				writeJSONError(w, http.StatusForbidden, "forbidden cors origin")
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireBearer(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := extractBearerHeader(r)
		if !s.verifyToken(tok) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="shnex-core"`)
			writeJSONError(w, http.StatusUnauthorized, "unauthorized: invalid or missing bearer token")
			return
		}
		next(w, r)
	}
}

func (s *Server) verifyToken(candidate string) bool {
	if len(candidate) != len(s.authToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(s.authToken)) == 1
}

func extractBearerHeader(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
		return strings.TrimSpace(auth[len(prefix):])
	}
	return ""
}

// --- Control Plane Handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	activeCount := len(s.sessions)
	started := s.startedAt
	s.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":             "ok",
		"listen_addr":        s.Addr(),
		"uptime_ms":          time.Since(started).Milliseconds(),
		"active_sessions":    activeCount,
		"protocols":          s.transports.Protocols(),
		"network_generation": s.netChanges.Generation(),
		"last_network_event": s.netChanges.LastEvent(),
	})
}

func (s *Server) handleNetworkChanged(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
	ev := s.NotifyNetworkChangedWithReason(body.Reason)
	writeJSON(w, http.StatusOK, ev)
}

func (s *Server) handleSetSecret(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.bridge.SetSecret(r.Context(), body.Key, body.Value); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stored": true, "key": body.Key})
}

// JumpHostDTO configures an intermediate SSH jump host in CreateSessionRequest.
type JumpHostDTO struct {
	Addr                 string                  `json:"addr"`
	User                 string                  `json:"user"`
	Password             string                  `json:"password,omitempty"`
	PrivateKeyPEM        string                  `json:"private_key_pem,omitempty"`
	PrivateKeyPassphrase string                  `json:"private_key_passphrase,omitempty"`
	HostKey              transport.HostKeyPolicy `json:"host_key"`
}

// RouteDTO configures how core dials the target host (Direct, Proxy, TSNet, Multi-Hop Bastions).
type RouteDTO struct {
	UseTSNet  bool          `json:"use_tsnet,omitempty"`
	ProxyURL  string        `json:"proxy_url,omitempty"` // e.g. socks5://127.0.0.1:1080 or http://127.0.0.1:7890
	JumpHosts []JumpHostDTO `json:"jump_hosts,omitempty"`
}

// AuthDTO carries session credentials in CreateSessionRequest.
type AuthDTO struct {
	Mode                 transport.AuthMode      `json:"mode,omitempty"`
	Password             string                  `json:"password,omitempty"`
	PrivateKeyPEM        string                  `json:"private_key_pem,omitempty"`
	PrivateKeyPassphrase string                  `json:"private_key_passphrase,omitempty"`
	SecretKeyRef         string                  `json:"secret_key_ref,omitempty"`
	HostKey              transport.HostKeyPolicy `json:"host_key"`
}

// CreateSessionRequest describes a new terminal session request over the HTTP control plane.
type CreateSessionRequest struct {
	HostID             string                `json:"host_id,omitempty"`
	HostTitle          string                `json:"host_title,omitempty"`
	Protocol           string                `json:"protocol,omitempty"`
	Target             string                `json:"target"`
	User               string                `json:"user"`
	Cols               uint16                `json:"cols,omitempty"`
	Rows               uint16                `json:"rows,omitempty"`
	Term               string                `json:"term,omitempty"`
	Env                map[string]string     `json:"env,omitempty"`
	DefaultDir         string                `json:"default_dir,omitempty"`
	PostLoginScript    string                `json:"post_login_script,omitempty"`
	FileProtocol       string                `json:"file_protocol,omitempty"`
	SFTPSudo           bool                  `json:"sftp_sudo,omitempty"`
	RecordLog          bool                  `json:"record_log,omitempty"`
	Route              RouteDTO              `json:"route"`
	Relay              transport.RelayConfig `json:"relay"`
	Auth               AuthDTO               `json:"auth"`
	KeepaliveSec       int                   `json:"keepalive_sec,omitempty"`
	SilenceThresholdMs int                   `json:"silence_threshold_ms,omitempty"`
	NotifyOnBell       bool                  `json:"notify_on_bell,omitempty"`
	NotifyOnApproval   bool                  `json:"notify_on_approval,omitempty"`
	NotifyOnIdle       bool                  `json:"notify_on_idle,omitempty"`
}

// SessionSummary describes an active session returned by GET / POST /api/v1/sessions.
type SessionSummary struct {
	ID              string                 `json:"id"`
	Protocol        string                 `json:"protocol"`
	Target          string                 `json:"target"`
	User            string                 `json:"user"`
	RelayMode       transport.RelayMode    `json:"relay_mode,omitempty"`
	WorkspaceName   string                 `json:"workspace_name,omitempty"`
	Size            transport.TerminalSize `json:"size"`
	CreatedAt       time.Time              `json:"created_at"`
	AttachedClients int32                  `json:"attached_clients"`
	LastRTTMicros   int64                  `json:"last_rtt_micros,omitempty"`
	Closed          bool                   `json:"closed"`
}

func (s *Server) handleListSessions(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	out := make([]SessionSummary, 0, len(s.sessions))
	for _, ms := range s.sessions {
		out = append(out, ms.summary())
	}
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.applySavedHostProfile(&req); err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	proto := strings.ToLower(strings.TrimSpace(req.Protocol))
	if proto == "" {
		proto = shnexssh.ProtocolName
	}
	tp, err := s.transports.Get(proto)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	d, closer, targetOverride, credOverride, err := s.buildSessionRoute(r.Context(), req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	target := req.Target
	if targetOverride != "" {
		target = targetOverride
	}

	creds := transport.CredentialsSpec{
		Mode:                 req.Auth.Mode,
		Password:             req.Auth.Password,
		PrivateKeyPEM:        []byte(req.Auth.PrivateKeyPEM),
		PrivateKeyPassphrase: []byte(req.Auth.PrivateKeyPassphrase),
		SecretKeyRef:         req.Auth.SecretKeyRef,
		HostKey:              req.Auth.HostKey,
	}
	if credOverride != nil {
		creds = *credOverride
	}

	size := transport.TerminalSize{Cols: req.Cols, Rows: req.Rows}.Normalized()
	sessCfg := transport.SessionConfig{
		Target:            target,
		User:              req.User,
		Dialer:            d,
		DialerCloser:      closer,
		Credentials:       creds,
		Relay:             req.Relay,
		InitialSize:       size,
		Term:              req.Term,
		Env:               req.Env,
		DefaultDir:        req.DefaultDir,
		PostLoginScript:   req.PostLoginScript,
		FileProtocol:      req.FileProtocol,
		SFTPSudo:          req.SFTPSudo,
		BuiltinPowerline:  s.settingsStore.Get().Terminal.BuiltinPowerline,
		KeepaliveInterval: time.Duration(req.KeepaliveSec) * time.Second,
		SilenceThreshold:  time.Duration(req.SilenceThresholdMs) * time.Millisecond,
		BellCooldown:      1500 * time.Millisecond,
	}

	sess, err := tp.OpenSession(r.Context(), sessCfg)
	if err != nil {
		if errors.Is(err, shnexssh.ErrPasswordRequired) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "需要输入 SSH 密码（本机默认密钥未能完成认证）",
				"code":  "AUTH_PASSWORD_NEEDED",
			})
			return
		}
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}

	var logWriter *sessionLogWriter
	if req.RecordLog && s.logsDir != "" {
		lbl := strings.TrimSpace(req.HostTitle)
		if lbl == "" {
			lbl = target
		}
		logWriter, _ = openSessionLogWriter(s.logsDir, lbl, req.User, target, sess.ID())
	}

	var compInsp completion.WorkspaceInspector
	if sess.Protocol() == localpty.ProtocolName {
		compInsp = completion.NewLocalInspector(req.DefaultDir)
	} else {
		remoteInsp := completion.NewRemoteSSHInspector(req.DefaultDir, sess)
		go remoteInsp.Prewarm()
		compInsp = remoteInsp
	}

	notifyApproval := req.NotifyOnApproval || req.NotifyOnIdle
	ms := newManagedSession(sess, req.User, req.Relay, size, s.bridge, logWriter, compInsp, req.NotifyOnBell, notifyApproval, req.NotifyOnIdle, func(id string) {
		s.mu.Lock()
		delete(s.sessions, id)
		s.mu.Unlock()
	})

	s.mu.Lock()
	s.sessions[sess.ID()] = ms
	s.mu.Unlock()

	ms.start()
	writeJSON(w, http.StatusCreated, ms.summary())
}

func (s *Server) buildSessionRoute(
	ctx context.Context,
	req CreateSessionRequest,
) (dialer.Dialer, io.Closer, string, *transport.CredentialsSpec, error) {
	// Special path: Tailscale SSH mode auto-wires tsnet + peer MagicDNS/HostKey discovery.
	if req.Auth.Mode == transport.AuthTailscaleSSH {
		plan, err := s.tailnetMgr.PrepareTailscaleSSH(
			ctx,
			req.Target,
			req.User,
			tailnet.TailscaleSSHAuto,
			req.Auth.HostKey.InsecureIgnore,
		)
		if err != nil {
			return nil, nil, "", nil, err
		}
		// Preserve explicit pinned SHA256 if caller supplied one.
		if req.Auth.HostKey.PinnedSHA256 != "" {
			plan.Credentials.HostKey.PinnedSHA256 = req.Auth.HostKey.PinnedSHA256
		}
		return plan.Dialer, nil, plan.TargetAddr, &plan.Credentials, nil
	}

	proxySpec := strings.TrimSpace(req.Route.ProxyURL)
	var base dialer.Dialer
	if req.Route.UseTSNet {
		tsd, err := s.tailnetMgr.Dialer()
		if err != nil {
			return nil, nil, "", nil, err
		}
		base = tsd
	} else if strings.EqualFold(proxySpec, "direct") {
		base = dialer.NewDirectDialer(15*time.Second, 30*time.Second)
		proxySpec = ""
	} else if strings.EqualFold(proxySpec, "system") {
		sd := dialer.NewSmartDirectDialer(15*time.Second, 30*time.Second)
		sd.StaggerDelay = 1 * time.Millisecond
		base = sd
		proxySpec = ""
	} else if proxySpec == "" {
		base = dialer.NewSmartDirectDialer(15*time.Second, 30*time.Second)
	} else {
		base = dialer.NewDirectDialer(15*time.Second, 30*time.Second)
	}

	if proxySpec != "" {
		pd, err := dialer.NewProxyDialerFromURL(proxySpec, base)
		if err != nil {
			return nil, nil, "", nil, err
		}
		base = pd
	}

	if len(req.Route.JumpHosts) == 0 {
		return base, nil, "", nil, nil
	}

	hops := make([]dialer.JumpHostConfig, 0, len(req.Route.JumpHosts))
	for i, jh := range req.Route.JumpHosts {
		hkCb, err := buildJumpHostKeyCallback(jh.HostKey)
		if err != nil {
			return nil, nil, "", nil, fmt.Errorf("jump_hosts[%d]: %w", i, err)
		}
		hops = append(hops, dialer.JumpHostConfig{
			Addr:                 jh.Addr,
			User:                 jh.User,
			Password:             jh.Password,
			PrivateKeyPEM:        []byte(jh.PrivateKeyPEM),
			PrivateKeyPassphrase: []byte(jh.PrivateKeyPassphrase),
			HostKeyCallback:      hkCb,
		})
	}

	chainDialer, closer, err := dialer.NewSSHJumpChain(base, hops...)
	if err != nil {
		return nil, nil, "", nil, err
	}
	return chainDialer, closer, "", nil, nil
}

func buildJumpHostKeyCallback(policy transport.HostKeyPolicy) (gossh.HostKeyCallback, error) {
	if pinned := strings.TrimSpace(policy.PinnedSHA256); pinned != "" {
		return func(hostname string, _ net.Addr, key gossh.PublicKey) error {
			if got := gossh.FingerprintSHA256(key); got != pinned {
				return fmt.Errorf("jump host key mismatch for %s: got %s, expected %s", hostname, got, pinned)
			}
			return nil
		}, nil
	}
	if kh := strings.TrimSpace(policy.KnownHostsPath); kh != "" {
		return knownhosts.New(kh)
	}
	if policy.InsecureIgnore {
		return gossh.InsecureIgnoreHostKey(), nil
	}
	return nil, shnexssh.ErrHostKeyVerificationRequired
}

func (s *Server) handleResizeSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ms := s.getManagedSession(id)
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	var size transport.TerminalSize
	if err := decodeJSON(r, &size); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := ms.resize(r.Context(), size); err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ms.summary())
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	ms, ok := s.sessions[id]
	if ok {
		delete(s.sessions, id)
	}
	s.mu.Unlock()
	if !ok {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	ms.close()
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (s *Server) handleTailnetStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.tailnetMgr.Status(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleTailnetUp(w http.ResponseWriter, r *http.Request) {
	var cfg tailnet.Config
	if err := decodeJSON(r, &cfg); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	st, err := s.tailnetMgr.Start(r.Context(), cfg)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleTailnetLogin(w http.ResponseWriter, r *http.Request) {
	st, err := s.tailnetMgr.StartInteractiveLogin(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleTailnetDown(w http.ResponseWriter, _ *http.Request) {
	if err := s.tailnetMgr.Stop(); err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stopped": true})
}

func (s *Server) handleTailnetNodes(w http.ResponseWriter, r *http.Request) {
	onlineOnly := r.URL.Query().Get("online") != "false"
	nodes, err := s.tailnetMgr.ListNodes(r.Context(), onlineOnly)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
}

// --- WebSocket Data Plane Handler ---

func (s *Server) handleSessionWS(w http.ResponseWriter, r *http.Request) {
	// 1. Strict CSWSH Origin validation: allow empty Origin (native desktop/mobile clients)
	// or exact match against our bound 127.0.0.1:<port> / allowlisted Wails WebView origins.
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		if !s.isAllowedOrigin(origin) {
			writeJSONError(w, http.StatusForbidden, "forbidden websocket origin")
			return
		}
	}

	// 2. Constant-time token authentication (Authorization header or ?token= query param).
	tok := extractBearerHeader(r)
	if tok == "" {
		tok = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if !s.verifyToken(tok) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized websocket token")
		return
	}

	id := r.PathValue("id")
	ms := s.getManagedSession(id)
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}

	// Origin has been strictly verified above.
	wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	wsConn.SetReadLimit(1 << 20) // 1 MiB max frame

	ms.serveWSClient(r.Context(), wsConn)
}

func (s *Server) getManagedSession(id string) *managedSession {
	s.mu.RLock()
	ms := s.sessions[id]
	s.mu.RUnlock()
	return ms
}

// --- Managed Session Multiplexer & Scrollback Hub ---

type managedSession struct {
	sess           transport.Session
	user           string
	relayCfg       transport.RelayConfig
	createdAt      time.Time
	bridge         platform.PlatformBridge
	logWriter      *sessionLogWriter
	compInspector  completion.WorkspaceInspector
	notifyBell     bool
	notifyApproval bool
	notifyIdle     bool
	onTerminated   func(id string)

	scrollback *ringBuffer

	mu      sync.RWMutex
	size    transport.TerminalSize
	nextSub uint64
	clients map[uint64]chan []byte
	closed  bool

	attachedCount atomic.Int32
	lastRTTMicros atomic.Int64
	closeOnce     sync.Once
}

func newManagedSession(
	sess transport.Session,
	user string,
	relayCfg transport.RelayConfig,
	initialSize transport.TerminalSize,
	bridge platform.PlatformBridge,
	logWriter *sessionLogWriter,
	compInspector completion.WorkspaceInspector,
	notifyBell bool,
	notifyApproval bool,
	notifyIdle bool,
	onTerminated func(id string),
) *managedSession {
	return &managedSession{
		sess:           sess,
		user:           user,
		relayCfg:       relayCfg,
		createdAt:      time.Now(),
		bridge:         bridge,
		logWriter:      logWriter,
		compInspector:  compInspector,
		notifyBell:     notifyBell,
		notifyApproval: notifyApproval,
		notifyIdle:     notifyIdle,
		onTerminated:   onTerminated,
		scrollback:     newRingBuffer(64 * 1024),
		size:           initialSize,
		clients:        make(map[uint64]chan []byte),
	}
}

func (m *managedSession) start() {
	m.sess.SubscribeEvents(func(ev transport.SessionEvent) {
		if ev.Type == transport.EventKeepaliveRTT || ev.Type == transport.EventNetworkProbed {
			m.lastRTTMicros.Store(ev.RTT.Microseconds())
		}

		if m.bridge != nil {
			switch ev.Type {
			case transport.EventBell:
				if m.notifyBell {
					_ = m.bridge.Notify(context.Background(), platform.Notification{
						Title:     fmt.Sprintf("终端响铃 - %s", m.sess.Target()),
						Body:      "远端会话收到独立响铃信号",
						Category:  platform.CategoryBell,
						SessionID: m.sess.ID(),
					})
				}
			case transport.EventAIApprovalRequired:
				if m.notifyApproval {
					_ = m.bridge.Notify(context.Background(), platform.Notification{
						Title:     fmt.Sprintf("Claude 等待人工审批 - %s", m.sess.Target()),
						Body:      ev.PromptSnippet,
						Category:  platform.CategoryActionRequired,
						SessionID: m.sess.ID(),
					})
				}
			case transport.EventOutputSilence:
				if m.notifyIdle {
					_ = m.bridge.Notify(context.Background(), platform.Notification{
						Title:     fmt.Sprintf("任务输出完成 - %s", m.sess.Target()),
						Body:      fmt.Sprintf("活跃输出 %d 字节后已静默 %s，等待下一条指令", ev.BurstBytes, ev.QuietFor.Round(100*time.Millisecond)),
						Category:  platform.CategoryTaskIdle,
						SessionID: m.sess.ID(),
					})
				}
			}
		}

		if frame, err := EncodeEventFrame(ev); err == nil {
			m.broadcastFrame(frame)
		}
	})

	go m.readPump()
}

func (m *managedSession) readPump() {
	buf := make([]byte, 32*1024)
	for {
		n, err := m.sess.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			m.scrollback.Write(chunk)
			if m.logWriter != nil {
				m.logWriter.WriteChunk(chunk)
			}
			m.broadcastFrame(EncodeDataFrame(chunk))
		}
		if err != nil {
			m.close()
			return
		}
	}
}

func (m *managedSession) broadcastFrame(frame []byte) {
	m.mu.RLock()
	for _, ch := range m.clients {
		select {
		case ch <- frame:
		default:
			// Drop frame for stalled slow consumer rather than blocking the SSH PTY reader.
		}
	}
	m.mu.RUnlock()
}

func (m *managedSession) resize(ctx context.Context, size transport.TerminalSize) error {
	norm := size.Normalized()
	if err := m.sess.Resize(ctx, norm); err != nil {
		return err
	}
	m.mu.Lock()
	m.size = norm
	m.mu.Unlock()
	return nil
}

func (m *managedSession) summary() SessionSummary {
	m.mu.RLock()
	sz := m.size
	closed := m.closed
	m.mu.RUnlock()

	return SessionSummary{
		ID:              m.sess.ID(),
		Protocol:        m.sess.Protocol(),
		Target:          m.sess.Target(),
		User:            m.user,
		RelayMode:       m.relayCfg.Mode,
		WorkspaceName:   m.relayCfg.SessionName,
		Size:            sz,
		CreatedAt:       m.createdAt,
		AttachedClients: m.attachedCount.Load(),
		LastRTTMicros:   m.lastRTTMicros.Load(),
		Closed:          closed,
	}
}

func (m *managedSession) serveWSClient(ctx context.Context, conn *websocket.Conn) {
	sendCh := make(chan []byte, 256)

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "session already closed")
		return
	}
	m.nextSub++
	subID := m.nextSub
	m.clients[subID] = sendCh
	m.mu.Unlock()

	m.attachedCount.Add(1)
	defer func() {
		m.attachedCount.Add(-1)
		m.mu.Lock()
		if ch, ok := m.clients[subID]; ok {
			delete(m.clients, subID)
			close(ch)
		}
		m.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "detached")
	}()

	// Replay recent scrollback snapshot immediately upon attach/reconnect.
	if replay := m.scrollback.Snapshot(); len(replay) > 0 {
		_ = conn.Write(ctx, websocket.MessageBinary, EncodeDataFrame(replay))
	}

	writerCtx, cancelWriter := context.WithCancel(ctx)
	defer cancelWriter()

	go func() {
		for {
			select {
			case <-writerCtx.Done():
				return
			case frame, ok := <-sendCh:
				if !ok {
					_ = conn.Close(websocket.StatusNormalClosure, "session ended")
					return
				}
				wCtx, cancel := context.WithTimeout(writerCtx, 5*time.Second)
				err := conn.Write(wCtx, websocket.MessageBinary, frame)
				cancel()
				if err != nil {
					return
				}
			}
		}
	}()

	for {
		msgType, frame, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if msgType != websocket.MessageBinary || len(frame) == 0 {
			continue
		}

		switch frame[0] {
		case FrameData:
			if len(frame) > 1 {
				if _, wErr := m.sess.Write(frame[1:]); wErr != nil {
					return
				}
			}
		case FrameResize:
			sz, rErr := DecodeResizeFrame(frame)
			if rErr == nil {
				_ = m.resize(ctx, sz)
			}
		case FramePing:
			clientTs := DecodePingTimestamp(frame)
			kaCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			rtt, kaErr := m.sess.Keepalive(kaCtx)
			cancel()
			if kaErr == nil {
				m.lastRTTMicros.Store(rtt.Microseconds())
				select {
				case sendCh <- EncodePongFrame(clientTs, uint64(rtt.Microseconds())):
				default:
				}
			}
		}
	}
}

func (m *managedSession) close() {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		for id, ch := range m.clients {
			delete(m.clients, id)
			close(ch)
		}
		m.mu.Unlock()

		if m.logWriter != nil {
			m.logWriter.Close()
		}
		_ = m.sess.Close()
		if m.onTerminated != nil {
			m.onTerminated(m.sess.ID())
		}
	})
}

// --- Helpers ---

func validateLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNonLoopbackAddress, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || !ip.IsLoopback() {
		return fmt.Errorf("%w (got host %q)", ErrNonLoopbackAddress, host)
	}
	return nil
}

func generateAuthToken32() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("api: generate auth token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func decodeJSON(r *http.Request, dst any) error {
	return decodeJSONWithLimit(r, dst, 1<<20)
}

func decodeJSONWithLimit(r *http.Request, dst any, maxBytes int64) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBytes))
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid json payload: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
