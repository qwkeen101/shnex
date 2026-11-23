package mobilebridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shnex/shnex/core/api"
	"github.com/shnex/shnex/core/platform"
)

// NativePlatformBridge is implemented in Kotlin on Android (backed by Android Keystore
// and NotificationManager) and passed into StartEngine via gomobile bind.
type NativePlatformBridge interface {
	GetSecret(key string) (string, error)
	SetSecret(key string, value string) error
	Notify(title string, body string, category string, sessionID string) error
}

type bridgeAdapter struct {
	native NativePlatformBridge

	mu      sync.RWMutex
	memSecs map[string]string
	history []platform.Notification
}

func newBridgeAdapter(native NativePlatformBridge) *bridgeAdapter {
	return &bridgeAdapter{
		native:  native,
		memSecs: make(map[string]string),
	}
}

func (b *bridgeAdapter) GetSecret(_ context.Context, key string) (string, error) {
	if b.native != nil {
		val, err := b.native.GetSecret(key)
		if err != nil {
			return "", err
		}
		if val == "" {
			return "", platform.ErrSecretNotFound
		}
		return val, nil
	}
	b.mu.RLock()
	v, ok := b.memSecs[key]
	b.mu.RUnlock()
	if !ok || v == "" {
		return "", platform.ErrSecretNotFound
	}
	return v, nil
}

func (b *bridgeAdapter) SetSecret(_ context.Context, key string, value string) error {
	if b.native != nil {
		return b.native.SetSecret(key, value)
	}
	b.mu.Lock()
	if value == "" {
		delete(b.memSecs, key)
	} else {
		b.memSecs[key] = value
	}
	b.mu.Unlock()
	return nil
}

func (b *bridgeAdapter) Notify(_ context.Context, n platform.Notification) error {
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	b.mu.Lock()
	b.history = append(b.history, n)
	if len(b.history) > 100 {
		b.history = b.history[len(b.history)-100:]
	}
	b.mu.Unlock()

	if b.native != nil {
		return b.native.Notify(n.Title, n.Body, string(n.Category), n.SessionID)
	}
	return nil
}

func (b *bridgeAdapter) Notifications() []platform.Notification {
	b.mu.RLock()
	defer b.mu.RUnlock()
	cp := make([]platform.Notification, len(b.history))
	copy(cp, b.history)
	return cp
}

// Engine wraps the in-process 127.0.0.1 Shnex core Server for Android lifecycle management.
type Engine struct {
	srv     *api.Server
	dataDir string
}

// StartEngine launches the Shnex core engine bound strictly to 127.0.0.1:0 inside the Android process.
func StartEngine(dataDir string, nativeBridge NativePlatformBridge) (*Engine, error) {
	cleanDir := strings.TrimSpace(dataDir)
	if cleanDir == "" {
		cleanDir = os.TempDir()
	}
	if err := os.MkdirAll(cleanDir, 0o700); err != nil {
		return nil, err
	}
	// Ensure ~/.ssh and embedded tsnet state resolve inside Android's private app storage
	if strings.TrimSpace(os.Getenv("HOME")) == "" || os.Getenv("HOME") == "/" {
		_ = os.Setenv("HOME", cleanDir)
	}
	_ = os.Setenv("XDG_CONFIG_HOME", filepath.Join(cleanDir, "config"))

	adapter := newBridgeAdapter(nativeBridge)
	srv, err := api.NewServer(api.Config{
		ListenAddr:    "127.0.0.1:0",
		Bridge:        adapter,
		WorkspacePath: filepath.Join(cleanDir, "hosts.json"),
	})
	if err != nil {
		return nil, err
	}
	if err := srv.Start(); err != nil {
		return nil, err
	}
	return &Engine{
		srv:     srv,
		dataDir: cleanDir,
	}, nil
}

// Addr returns the bound "127.0.0.1:<port>" address.
func (e *Engine) Addr() string {
	if e == nil || e.srv == nil {
		return ""
	}
	return e.srv.Addr()
}

// ApiBaseUrl returns "http://127.0.0.1:<port>".
func (e *Engine) ApiBaseUrl() string {
	addr := e.Addr()
	if addr == "" {
		return ""
	}
	return "http://" + addr
}

// WsBaseUrl returns "ws://127.0.0.1:<port>".
func (e *Engine) WsBaseUrl() string {
	addr := e.Addr()
	if addr == "" {
		return ""
	}
	return "ws://" + addr
}

// AuthToken returns the per-process 32-byte (64 hex chars) authentication token.
func (e *Engine) AuthToken() string {
	if e == nil || e.srv == nil {
		return ""
	}
	return e.srv.AuthToken()
}

// NotifyNetworkChanged informs all SSH and Tailscale transports that Android switched networks (Wi-Fi <-> 5G).
func (e *Engine) NotifyNetworkChanged(reason string) {
	if e == nil || e.srv == nil {
		return
	}
	e.srv.NotifyNetworkChangedWithReason(reason)
}

// Stop gracefully shuts down the loopback server, active sessions, and embedded tsnet.
func (e *Engine) Stop() error {
	if e == nil || e.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	return e.srv.Shutdown(ctx)
}
