package settings

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shnex/shnex/core/platform"
)

type RightRailVisibility struct {
	Monitor      bool `json:"monitor"`
	Processes    bool `json:"processes"`
	HostInfo     bool `json:"host_info"`
	Files        bool `json:"files"`
	Docker       bool `json:"docker"`
	Tmux         bool `json:"tmux"`
	AI           bool `json:"ai"`
	PortForward  bool `json:"port_forward"`
	Snippets     bool `json:"snippets"`
	CmdHistory   bool `json:"cmd_history"`
	Broadcast    bool `json:"broadcast"`
	SessionLogs  bool `json:"session_logs"`
	QuickConnect bool `json:"quick_connect"`
	ImportSSH    bool `json:"import_ssh"`
}

type GeneralSettings struct {
	Language               string              `json:"language"`                 // "system" | "zh-CN" | "en-US"
	RememberLayout         bool                `json:"remember_layout"`          // restore tabs & splits on startup
	RightBarMode           string              `json:"right_bar_mode"`           // "replace" | "side_by_side"
	BillingReminderEnabled bool                `json:"billing_reminder_enabled"` // notify before host subscription renewal
	CloseAction            string              `json:"close_action"`             // "tray" | "quit"
	TrayIconStyle          string              `json:"tray_icon_style"`          // "color" | "mono"
	TrayClickAction        string              `json:"tray_click_action"`        // "popup" | "main_window"
	TrayMaterial           string              `json:"tray_material"`            // "translucent" | "opaque"
	AutoCheckUpdate        bool                `json:"auto_check_update"`
	UpdateIntervalHours    int                 `json:"update_interval_hours"` // 1 | 2 | 6 | 12 | 24
	UpdateProxy            string              `json:"update_proxy"`          // "system" | "direct" | proxyId
	RightRailItems         RightRailVisibility `json:"right_rail_items"`
	DefaultPrivacyMode     bool                `json:"default_privacy_mode"`
	ConfirmBeforeClose     bool                `json:"confirm_before_close"`
	StartupBehavior        string              `json:"startup_behavior"`
}

type SecuritySettings struct {
	MasterLockEnabled bool `json:"master_lock_enabled"`
	AutoLockMinutes   int  `json:"auto_lock_minutes"`
	VaultLocked       bool `json:"vault_locked"` // runtime status exposed to UI
}

type AppearanceSettings struct {
	Theme         string `json:"theme"`                    // "dark", "oled", "slate"
	UIFontSize    int    `json:"ui_font_size"`             // 11..24
	CompactMode   bool   `json:"compact_mode"`
	HasWallpaper  bool   `json:"has_wallpaper"`            // true when custom wallpaper file exists
	WallpaperMime string `json:"wallpaper_mime,omitempty"` // preserved image MIME type (e.g. image/png, image/webp, image/svg+xml)
	WallpaperDim  int    `json:"wallpaper_dim"`            // 15..90 (default 52%)
	GlassBlurPx   int    `json:"glass_blur_px"`            // 0..32 (default 16px)
}

type TerminalSettings struct {
	FontFamily            string  `json:"font_family"`
	FontSize              float64 `json:"font_size"`
	LineHeight            float64 `json:"line_height"`
	CursorStyle           string  `json:"cursor_style"` // "block", "bar", "underline"
	CursorBlink           bool    `json:"cursor_blink"`
	Scrollback            int     `json:"scrollback"`
	DefaultRelayMode      string  `json:"default_relay_mode"`
	DefaultShell          string  `json:"default_shell"`            // "powershell" | "pwsh" | "cmd" | "wsl" | "gitbash"
	KeepaliveSec          int     `json:"keepalive_sec"`            // default 30
	AutoReconnect         bool    `json:"auto_reconnect"`           // default true
	RestoreDirOnReconnect bool    `json:"restore_dir_on_reconnect"` // default true
	TermType              string  `json:"term_type"`                // default "xterm-256color"
	RightClickAction      string  `json:"right_click_action"`       // "menu" | "paste" | "copy_or_paste"
	CopyOnSelect          bool    `json:"copy_on_select"`           // default false
	OSC52Mode             string  `json:"osc52_mode"`               // "allow" | "ask" | "deny"
	OSC52MaxKiB           int     `json:"osc52_max_kib"`            // 64..8192, default 1024
	OSC52Notify           bool    `json:"osc52_notify"`             // default true
	CompletionEnabled     bool    `json:"completion_enabled"`       // default true
	CompletionMode        string  `json:"completion_mode"`          // "inline" | "list" | "hybrid" (default "hybrid")
	BuiltinPowerline      bool    `json:"builtin_powerline"`        // default true
}

type MonitorSettings struct {
	Enabled       bool `json:"enabled"`
	IntervalSec   int  `json:"interval_sec"`
	ShowBottomBar bool `json:"show_bottom_bar"`
}

type FileSettings struct {
	DefaultPath   string `json:"default_path"`
	ShowHidden    bool   `json:"show_hidden"`
	MaxDownloadMB int    `json:"max_download_mb"`
}

type AIProvider struct {
	ID        string `json:"id"`
	Name      string `json:"name"`      // e.g. "openai"
	Protocol  string `json:"protocol"`  // "openai" | "anthropic"
	BaseURL   string `json:"base_url"`  // e.g. "https://api.openai.com/v1"
	Model     string `json:"model"`     // e.g. "gpt-4o"
	IsDefault bool   `json:"is_default"`
	HasAPIKey bool   `json:"has_api_key"`
	SecretRef string `json:"secret_ref"`
}

type UpsertProviderInput struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	APIKey    string `json:"api_key,omitempty"`
	IsDefault bool   `json:"is_default"`
}

type AISettings struct {
	DefaultAgent       string       `json:"default_agent"`        // "builtin" | "acp" | "claude_cli"
	ACPCommand         string       `json:"acp_command"`          // external local ACP executable
	ContextLines       int          `json:"context_lines"`        // default 200
	AuditRetentionDays int          `json:"audit_retention_days"` // default 90
	RedactSecrets      bool         `json:"redact_secrets"`       // default true
	ConfirmedHosts     []string     `json:"confirmed_hosts"`      // host names/IDs approved to send context
	DefaultPermission  string       `json:"default_permission"`   // "ask" | "auto_review" | "allow_all"
	ReviewProviderID   string       `json:"review_provider_id"`   // provider ID used as safety guardrail
	ReviewStrictness   string       `json:"review_strictness"`    // "loose" (80%) | "standard" (90%) | "strict" (97%)
	Providers          []AIProvider `json:"providers"`
}

type MCPServerEntry struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Enabled bool     `json:"enabled"`
}

type CredentialEntry struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"` // "password" | "private_key"
	Username  string    `json:"username"`
	SecretRef string    `json:"secret_ref"`
	CreatedAt time.Time `json:"created_at"`
}

type ProxyEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Scheme    string `json:"scheme"` // "socks5" | "http" | "https"
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username,omitempty"`
	HasSecret bool   `json:"has_secret"`
	SecretRef string `json:"secret_ref,omitempty"`
}

type AppSettings struct {
	General     GeneralSettings    `json:"general"`
	Security    SecuritySettings   `json:"security"`
	Appearance  AppearanceSettings `json:"appearance"`
	Terminal    TerminalSettings   `json:"terminal"`
	Monitor     MonitorSettings    `json:"monitor"`
	Files       FileSettings       `json:"files"`
	AI          AISettings         `json:"ai"`
	MCPServers  []MCPServerEntry   `json:"mcp_servers"`
	Credentials []CredentialEntry  `json:"credentials"`
	Proxies     []ProxyEntry       `json:"proxies"`
}

func DefaultRightRailVisibility() RightRailVisibility {
	return RightRailVisibility{
		Monitor:      true,
		Processes:    true,
		HostInfo:     true,
		Files:        true,
		Docker:       true,
		Tmux:         true,
		AI:           true,
		PortForward:  true,
		Snippets:     true,
		CmdHistory:   true,
		Broadcast:    true,
		SessionLogs:  true,
		QuickConnect: true,
		ImportSSH:    true,
	}
}

func DefaultSettings() AppSettings {
	return AppSettings{
		General: GeneralSettings{
			Language:               "system",
			RememberLayout:         false,
			RightBarMode:           "replace",
			BillingReminderEnabled: true,
			CloseAction:            "tray",
			TrayIconStyle:          "color",
			TrayClickAction:        "popup",
			TrayMaterial:           "opaque",
			AutoCheckUpdate:        true,
			UpdateIntervalHours:    1,
			UpdateProxy:            "system",
			RightRailItems:         DefaultRightRailVisibility(),
			DefaultPrivacyMode:     true,
			ConfirmBeforeClose:     true,
			StartupBehavior:        "welcome",
		},
		Security: SecuritySettings{
			MasterLockEnabled: false,
			AutoLockMinutes:   30,
			VaultLocked:       false,
		},
		Appearance: AppearanceSettings{
			Theme:        "dark",
			UIFontSize:   13,
			CompactMode:  false,
			HasWallpaper: false,
			WallpaperDim: 52,
			GlassBlurPx:  16,
		},
		Terminal: TerminalSettings{
			FontFamily:            `"Cascadia Mono", "JetBrains Mono", Consolas, monospace`,
			FontSize:              13.5,
			LineHeight:            1.24,
			CursorStyle:           "block",
			CursorBlink:           true,
			Scrollback:            5000,
			DefaultRelayMode:      "none",
			DefaultShell:          "powershell",
			KeepaliveSec:          30,
			AutoReconnect:         true,
			RestoreDirOnReconnect: true,
			TermType:              "xterm-256color",
			RightClickAction:      "menu",
			CopyOnSelect:          false,
			OSC52Mode:             "ask",
			OSC52MaxKiB:           1024,
			OSC52Notify:           true,
			CompletionEnabled:     true,
			CompletionMode:        "hybrid",
			BuiltinPowerline:      true,
		},
		Monitor: MonitorSettings{
			Enabled:       true,
			IntervalSec:   3,
			ShowBottomBar: true,
		},
		Files: FileSettings{
			DefaultPath:   "",
			ShowHidden:    true,
			MaxDownloadMB: 20,
		},
		AI: AISettings{
			DefaultAgent:       "builtin",
			ACPCommand:         "claude",
			ContextLines:       200,
			AuditRetentionDays: 90,
			RedactSecrets:      true,
			ConfirmedHosts:     []string{},
			DefaultPermission:  "auto_review",
			ReviewProviderID:   "",
			ReviewStrictness:   "standard",
			Providers:          []AIProvider{},
		},
		MCPServers:  []MCPServerEntry{},
		Credentials: []CredentialEntry{},
		Proxies:     []ProxyEntry{},
	}
}

type Store struct {
	filePath string
	bridge   platform.PlatformBridge

	mu          sync.RWMutex
	data        AppSettings
	vaultLocked bool
}

func NewStore(filePath string, bridge platform.PlatformBridge) (*Store, error) {
	if strings.TrimSpace(filePath) == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			dir = os.TempDir()
		}
		filePath = filepath.Join(dir, "shnex", "settings.json")
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		return nil, fmt.Errorf("settings: create dir: %w", err)
	}
	s := &Store{
		filePath: filePath,
		bridge:   bridge,
		data:     DefaultSettings(),
	}
	_ = s.load()
	s.normalizeLocked()
	if s.data.Security.MasterLockEnabled {
		s.vaultLocked = true
	}
	return s, nil
}

func (s *Store) Dir() string {
	return filepath.Dir(s.filePath)
}

func (s *Store) WallpaperPath() string {
	return filepath.Join(filepath.Dir(s.filePath), "custom_wallpaper.bin")
}

func (s *Store) Get() AppSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, _ := json.Marshal(s.data)
	var cp AppSettings
	_ = json.Unmarshal(raw, &cp)
	cp.Security.VaultLocked = s.vaultLocked
	if _, err := os.Stat(s.WallpaperPath()); err == nil {
		cp.Appearance.HasWallpaper = true
	} else {
		cp.Appearance.HasWallpaper = false
	}
	return cp
}

func (s *Store) IsVaultLocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.vaultLocked
}

const masterHashSecretKey = "shnex:security:master_hash"

func hashMasterPassword(pw string) string {
	sum := sha256.Sum256([]byte("shnex-master-v1:" + pw))
	return hex.EncodeToString(sum[:])
}

func (s *Store) SetMasterPassword(ctx context.Context, password string) (AppSettings, error) {
	if s.bridge == nil {
		return AppSettings{}, errors.New("settings: PlatformBridge unavailable")
	}
	pw := strings.TrimSpace(password)
	if pw == "" {
		// Disable master lock
		_ = s.bridge.SetSecret(ctx, masterHashSecretKey, "")
		return s.Update(func(cur *AppSettings) {
			cur.Security.MasterLockEnabled = false
			s.vaultLocked = false
		})
	}
	if err := s.bridge.SetSecret(ctx, masterHashSecretKey, hashMasterPassword(pw)); err != nil {
		return AppSettings{}, err
	}
	return s.Update(func(cur *AppSettings) {
		cur.Security.MasterLockEnabled = true
		s.vaultLocked = false
	})
}

func (s *Store) LockVault() AppSettings {
	s.mu.Lock()
	s.vaultLocked = true
	s.mu.Unlock()
	return s.Get()
}

func (s *Store) UnlockVault(ctx context.Context, password string) (AppSettings, error) {
	s.mu.RLock()
	enabled := s.data.Security.MasterLockEnabled
	s.mu.RUnlock()

	if !enabled || s.bridge == nil {
		s.mu.Lock()
		s.vaultLocked = false
		s.mu.Unlock()
		return s.Get(), nil
	}

	storedHash, err := s.bridge.GetSecret(ctx, masterHashSecretKey)
	if err != nil || storedHash == "" {
		s.mu.Lock()
		s.vaultLocked = false
		s.mu.Unlock()
		return s.Get(), nil
	}

	candidate := hashMasterPassword(strings.TrimSpace(password))
	if subtle.ConstantTimeCompare([]byte(storedHash), []byte(candidate)) != 1 {
		return AppSettings{}, errors.New("主密码不正确，无法解锁已保存的机密内容")
	}

	s.mu.Lock()
	s.vaultLocked = false
	s.mu.Unlock()
	return s.Get(), nil
}

func (s *Store) Update(patch func(cur *AppSettings)) (AppSettings, error) {
	s.mu.Lock()
	patch(&s.data)
	s.normalizeLocked()
	err := s.saveLocked()
	s.mu.Unlock()
	return s.Get(), err
}

func (s *Store) UpsertAIProvider(ctx context.Context, in UpsertProviderInput) (AppSettings, error) {
	if strings.TrimSpace(in.BaseURL) == "" || strings.TrimSpace(in.Model) == "" {
		return AppSettings{}, errors.New("settings: provider base_url and model are required")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "openai"
	}
	proto := strings.TrimSpace(in.Protocol)
	if proto == "" {
		proto = "openai"
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = randomID("prov")
	}
	secretRef := "shnex:aiprovider:" + id

	hasKey := false
	s.mu.RLock()
	for _, p := range s.data.AI.Providers {
		if p.ID == id {
			hasKey = p.HasAPIKey
			if p.SecretRef != "" {
				secretRef = p.SecretRef
			}
			break
		}
	}
	s.mu.RUnlock()

	if strings.TrimSpace(in.APIKey) != "" {
		if s.bridge == nil {
			return AppSettings{}, errors.New("settings: PlatformBridge unavailable")
		}
		if err := s.bridge.SetSecret(ctx, secretRef, strings.TrimSpace(in.APIKey)); err != nil {
			return AppSettings{}, err
		}
		hasKey = true
	}

	entry := AIProvider{
		ID:        id,
		Name:      name,
		Protocol:  proto,
		BaseURL:   strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"),
		Model:     strings.TrimSpace(in.Model),
		IsDefault: in.IsDefault,
		HasAPIKey: hasKey,
		SecretRef: secretRef,
	}

	return s.Update(func(cur *AppSettings) {
		if len(cur.AI.Providers) == 0 {
			entry.IsDefault = true
		}
		if entry.IsDefault {
			for i := range cur.AI.Providers {
				cur.AI.Providers[i].IsDefault = false
			}
		}
		replaced := false
		for i := range cur.AI.Providers {
			if cur.AI.Providers[i].ID == id {
				cur.AI.Providers[i] = entry
				replaced = true
				break
			}
		}
		if !replaced {
			cur.AI.Providers = append(cur.AI.Providers, entry)
		}
		if cur.AI.ReviewProviderID == "" {
			cur.AI.ReviewProviderID = entry.ID
		}
	})
}

func (s *Store) DeleteAIProvider(id string) (AppSettings, error) {
	return s.Update(func(cur *AppSettings) {
		out := cur.AI.Providers[:0]
		for _, p := range cur.AI.Providers {
			if p.ID != id {
				out = append(out, p)
			}
		}
		cur.AI.Providers = out
		if len(cur.AI.Providers) > 0 {
			hasDef := false
			for _, p := range cur.AI.Providers {
				if p.IsDefault {
					hasDef = true
				}
			}
			if !hasDef {
				cur.AI.Providers[0].IsDefault = true
			}
		}
		if cur.AI.ReviewProviderID == id {
			if len(cur.AI.Providers) > 0 {
				cur.AI.ReviewProviderID = cur.AI.Providers[0].ID
			} else {
				cur.AI.ReviewProviderID = ""
			}
		}
	})
}

func (s *Store) SetDefaultAIProvider(id string) (AppSettings, error) {
	return s.Update(func(cur *AppSettings) {
		for i := range cur.AI.Providers {
			cur.AI.Providers[i].IsDefault = (cur.AI.Providers[i].ID == id)
		}
	})
}

func (s *Store) AddCredential(ctx context.Context, name, kind, username, secret string) (AppSettings, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(secret) == "" {
		return AppSettings{}, errors.New("settings: credential name and secret are required")
	}
	id := randomID("cred")
	ref := "shnex:vault:" + id
	if s.bridge == nil {
		return AppSettings{}, errors.New("settings: PlatformBridge unavailable")
	}
	if err := s.bridge.SetSecret(ctx, ref, secret); err != nil {
		return AppSettings{}, err
	}
	if kind != "private_key" {
		kind = "password"
	}
	return s.Update(func(cur *AppSettings) {
		cur.Credentials = append(cur.Credentials, CredentialEntry{
			ID:        id,
			Name:      strings.TrimSpace(name),
			Kind:      kind,
			Username:  strings.TrimSpace(username),
			SecretRef: ref,
			CreatedAt: time.Now(),
		})
	})
}

func (s *Store) DeleteCredential(id string) (AppSettings, error) {
	return s.Update(func(cur *AppSettings) {
		out := cur.Credentials[:0]
		for _, c := range cur.Credentials {
			if c.ID != id {
				out = append(out, c)
			}
		}
		cur.Credentials = out
	})
}

func (s *Store) UpsertProxy(ctx context.Context, p ProxyEntry, password string) (AppSettings, error) {
	if strings.TrimSpace(p.Host) == "" || p.Port <= 0 {
		return AppSettings{}, errors.New("settings: proxy host and port required")
	}
	if p.ID == "" {
		p.ID = randomID("prx")
	}
	if p.Name == "" {
		p.Name = fmt.Sprintf("%s:%d", p.Host, p.Port)
	}
	if p.Scheme == "" {
		p.Scheme = "socks5"
	}
	if password != "" && s.bridge != nil {
		p.SecretRef = "shnex:proxy:" + p.ID
		if err := s.bridge.SetSecret(ctx, p.SecretRef, password); err != nil {
			return AppSettings{}, err
		}
		p.HasSecret = true
	}
	return s.Update(func(cur *AppSettings) {
		replaced := false
		for i := range cur.Proxies {
			if cur.Proxies[i].ID == p.ID {
				if password == "" {
					// editing without retyping the password keeps the stored one
					p.SecretRef, p.HasSecret = cur.Proxies[i].SecretRef, cur.Proxies[i].HasSecret
				}
				cur.Proxies[i] = p
				replaced = true
				break
			}
		}
		if !replaced {
			cur.Proxies = append(cur.Proxies, p)
		}
	})
}

// ProxyByID returns a pool proxy and its URL including credentials, for dialing only;
// the URL must never be logged or returned to clients.
func (s *Store) ProxyByID(ctx context.Context, id string) (ProxyEntry, string, error) {
	var entry *ProxyEntry
	for _, p := range s.Get().Proxies {
		if p.ID == id {
			p := p
			entry = &p
			break
		}
	}
	if entry == nil {
		return ProxyEntry{}, "", fmt.Errorf("settings: proxy %q has been deleted", id)
	}
	u := url.URL{Scheme: entry.Scheme, Host: net.JoinHostPort(entry.Host, strconv.Itoa(entry.Port))}
	password := ""
	if entry.HasSecret && entry.SecretRef != "" && s.bridge != nil {
		password, _ = s.bridge.GetSecret(ctx, entry.SecretRef)
	}
	switch {
	case entry.Username != "" && password != "":
		u.User = url.UserPassword(entry.Username, password)
	case entry.Username != "":
		u.User = url.User(entry.Username)
	}
	return *entry, u.String(), nil
}

// ParseProxyLink turns one pasted line such as "socks5://user:pass@host:1080#香港" into a
// pool entry and its password. Only SOCKS5 / HTTP(S) proxies for now.
func ParseProxyLink(line string) (ProxyEntry, string, error) {
	u, err := url.Parse(strings.TrimSpace(line))
	if err != nil {
		return ProxyEntry{}, "", errors.New("不是有效的链接")
	}
	if u.Scheme == "" {
		return ProxyEntry{}, "", errors.New("不是有效的链接")
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "socks5", "socks5h", "socks":
		scheme = "socks5"
	case "http", "https":
	default:
		return ProxyEntry{}, "", fmt.Errorf("暂不支持 %s:// 链接（目前只支持 socks5 / http）", u.Scheme)
	}
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if host == "" || err != nil || port <= 0 || port > 65535 {
		return ProxyEntry{}, "", errors.New("缺少地址或端口")
	}
	name := strings.TrimSpace(u.Fragment)
	if name == "" {
		name = net.JoinHostPort(host, strconv.Itoa(port))
	}
	p := ProxyEntry{Name: name, Scheme: scheme, Host: host, Port: port}
	password := ""
	if u.User != nil {
		p.Username = u.User.Username()
		password, _ = u.User.Password()
	}
	return p, password, nil
}

func (s *Store) DeleteProxy(id string) (AppSettings, error) {
	return s.Update(func(cur *AppSettings) {
		out := cur.Proxies[:0]
		for _, x := range cur.Proxies {
			if x.ID != id {
				out = append(out, x)
			}
		}
		cur.Proxies = out
	})
}

func (s *Store) ResolveProviderSecret(ctx context.Context, providerID string) (AIProvider, string, error) {
	snap := s.Get()
	var chosen *AIProvider
	for i := range snap.AI.Providers {
		p := &snap.AI.Providers[i]
		if providerID != "" && p.ID == providerID {
			chosen = p
			break
		}
		if providerID == "" && p.IsDefault {
			chosen = p
			break
		}
	}
	if chosen == nil && len(snap.AI.Providers) > 0 {
		chosen = &snap.AI.Providers[0]
	}
	if chosen == nil {
		return AIProvider{}, "", errors.New("尚未添加模型服务商（请在「设置 -> AI 助理 -> 模型服务商」中点击添加提供商）")
	}
	if s.bridge == nil {
		return *chosen, "", errors.New("PlatformBridge 未初始化")
	}
	key, err := s.bridge.GetSecret(ctx, chosen.SecretRef)
	if err != nil || strings.TrimSpace(key) == "" {
		return *chosen, "", fmt.Errorf("模型服务商 %q 尚未配置有效的 API Key", chosen.Name)
	}
	return *chosen, strings.TrimSpace(key), nil
}

func (s *Store) normalizeLocked() {
	if s.data.Appearance.WallpaperDim < 15 || s.data.Appearance.WallpaperDim > 92 {
		s.data.Appearance.WallpaperDim = 52
	}
	if s.data.Appearance.GlassBlurPx < 0 || s.data.Appearance.GlassBlurPx > 40 {
		s.data.Appearance.GlassBlurPx = 16
	}
	if s.data.AI.ContextLines < 0 {
		s.data.AI.ContextLines = 0
	}
	if s.data.AI.ContextLines > 2000 {
		s.data.AI.ContextLines = 2000
	}
	if s.data.AI.AuditRetentionDays < 0 {
		s.data.AI.AuditRetentionDays = 0
	}
	if s.data.Monitor.IntervalSec < 1 {
		s.data.Monitor.IntervalSec = 3
	}
	if s.data.Terminal.FontSize < 9 {
		s.data.Terminal.FontSize = 13.5
	}
	if s.data.Terminal.DefaultShell == "" {
		s.data.Terminal.DefaultShell = "powershell"
	}
	if s.data.Terminal.KeepaliveSec < 0 {
		s.data.Terminal.KeepaliveSec = 30
	}
	if s.data.Terminal.TermType == "" {
		s.data.Terminal.TermType = "xterm-256color"
	}
	if s.data.Terminal.RightClickAction == "" {
		s.data.Terminal.RightClickAction = "menu"
	}
	if s.data.Terminal.OSC52Mode == "" {
		s.data.Terminal.OSC52Mode = "ask"
	}
	if s.data.Terminal.OSC52MaxKiB < 64 {
		s.data.Terminal.OSC52MaxKiB = 1024
	}
	if s.data.Terminal.OSC52MaxKiB > 8192 {
		s.data.Terminal.OSC52MaxKiB = 8192
	}
	switch s.data.Terminal.CompletionMode {
	case "inline", "list", "hybrid":
	default:
		s.data.Terminal.CompletionMode = "hybrid"
	}
	if s.data.General.Language == "" {
		s.data.General.Language = "system"
	}
	if s.data.General.RightBarMode == "" {
		s.data.General.RightBarMode = "replace"
	}
	if s.data.General.CloseAction == "" {
		s.data.General.CloseAction = "tray"
	}
	if s.data.General.TrayIconStyle == "" {
		s.data.General.TrayIconStyle = "color"
	}
	if s.data.General.TrayClickAction == "" {
		s.data.General.TrayClickAction = "popup"
	}
	if s.data.General.TrayMaterial == "" {
		s.data.General.TrayMaterial = "opaque"
	}
	if s.data.General.UpdateIntervalHours <= 0 {
		s.data.General.UpdateIntervalHours = 1
	}
	if s.data.General.UpdateProxy == "" {
		s.data.General.UpdateProxy = "system"
	}
	if s.data.General.StartupBehavior == "" {
		s.data.General.StartupBehavior = "welcome"
	}
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, &s.data)
}

func (s *Store) saveLocked() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
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
	var b [5]byte
	_, _ = rand.Read(b[:])
	return prefix + "-" + hex.EncodeToString(b[:])
}
