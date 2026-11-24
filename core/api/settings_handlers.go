package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shnex/shnex/core/hosts"
	"github.com/shnex/shnex/core/settings"
	"github.com/shnex/shnex/core/sharelink"
	gossh "golang.org/x/crypto/ssh"
)

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.settingsStore.Get())
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var incoming settings.AppSettings
	if err := decodeJSON(r, &incoming); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.settingsStore.Update(func(cur *settings.AppSettings) {
		cur.General = incoming.General
		cur.Security.AutoLockMinutes = incoming.Security.AutoLockMinutes
		prevMime := cur.Appearance.WallpaperMime
		cur.Appearance = incoming.Appearance
		if cur.Appearance.WallpaperMime == "" {
			cur.Appearance.WallpaperMime = prevMime
		}
		cur.Terminal = incoming.Terminal
		cur.Monitor = incoming.Monitor
		cur.Files = incoming.Files
		if incoming.MCPServers != nil {
			cur.MCPServers = incoming.MCPServers
		}
		// Preserve Providers slice (managed via dedicated endpoints so SecretRef/HasAPIKey stay intact)
		existingProviders := cur.AI.Providers
		cur.AI = incoming.AI
		cur.AI.Providers = existingProviders
		if cur.AI.ConfirmedHosts == nil {
			cur.AI.ConfirmedHosts = []string{}
		}
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleSetMasterPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.settingsStore.SetMasterPassword(r.Context(), body.Password)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleLockVault(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.settingsStore.LockVault())
}

func (s *Server) handleUnlockVault(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.settingsStore.UnlockVault(r.Context(), body.Password)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

type knownHostEntryDTO struct {
	LineNumber  int    `json:"line_number"`
	Hosts       string `json:"hosts"`
	KeyType     string `json:"key_type"`
	Fingerprint string `json:"fingerprint"`
}

func defaultKnownHostsFile() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".ssh", "known_hosts")
}

func (s *Server) handleListKnownHosts(w http.ResponseWriter, _ *http.Request) {
	khPath := defaultKnownHostsFile()
	if khPath == "" {
		writeJSON(w, http.StatusOK, map[string]any{"path": "", "entries": []knownHostEntryDTO{}})
		return
	}
	raw, err := os.ReadFile(khPath)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"path": khPath, "entries": []knownHostEntryDTO{}})
		return
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	var entries []knownHostEntryDTO
	for idx, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		marker, hostsList, pubKey, _, _, pErr := gossh.ParseKnownHosts([]byte(trimmed))
		if pErr != nil || marker == "revoked" || pubKey == nil {
			continue
		}
		entries = append(entries, knownHostEntryDTO{
			LineNumber:  idx + 1,
			Hosts:       strings.Join(hostsList, ", "),
			KeyType:     pubKey.Type(),
			Fingerprint: gossh.FingerprintSHA256(pubKey),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":    khPath,
		"entries": entries,
	})
}

func (s *Server) handleDeleteKnownHost(w http.ResponseWriter, r *http.Request) {
	lineNum, err := strconv.Atoi(r.PathValue("line"))
	if err != nil || lineNum <= 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid line number")
		return
	}
	khPath := defaultKnownHostsFile()
	if khPath == "" {
		writeJSONError(w, http.StatusNotFound, "known_hosts not found")
		return
	}
	raw, err := os.ReadFile(khPath)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	if lineNum > len(lines) {
		writeJSONError(w, http.StatusNotFound, "line out of range")
		return
	}
	nextLines := append(lines[:lineNum-1], lines[lineNum:]...)
	if err := os.WriteFile(khPath, []byte(strings.Join(nextLines, "\n")), 0o600); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.handleListKnownHosts(w, r)
}

func detectImageMimeType(raw []byte, hintName string) string {
	if len(raw) >= 8 && bytes.Equal(raw[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return "image/png"
	}
	if len(raw) >= 3 && raw[0] == 0xff && raw[1] == 0xd8 && raw[2] == 0xff {
		return "image/jpeg"
	}
	if len(raw) >= 12 && bytes.Equal(raw[:4], []byte("RIFF")) && bytes.Equal(raw[8:12], []byte("WEBP")) {
		return "image/webp"
	}
	if len(raw) >= 6 && (bytes.Equal(raw[:6], []byte("GIF87a")) || bytes.Equal(raw[:6], []byte("GIF89a"))) {
		return "image/gif"
	}
	if len(raw) >= 2 && raw[0] == 'B' && raw[1] == 'M' {
		return "image/bmp"
	}
	if len(raw) >= 12 && bytes.Equal(raw[4:8], []byte("ftyp")) {
		brand := string(raw[8:12])
		switch brand {
		case "avif", "avis":
			return "image/avif"
		case "heic", "heix", "mif1", "msf1":
			return "image/heic"
		}
	}
	head := raw
	if len(head) > 1024 {
		head = head[:1024]
	}
	if bytes.Contains(bytes.ToLower(head), []byte("<svg")) {
		return "image/svg+xml"
	}
	switch strings.ToLower(filepath.Ext(hintName)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg", ".jfif":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".avif":
		return "image/avif"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	}
	ct := http.DetectContentType(raw)
	if strings.HasPrefix(ct, "image/") {
		return ct
	}
	return "image/png"
}

func decodeBase64Flexible(s string) ([]byte, error) {
	if comma := strings.IndexByte(s, ','); comma >= 0 && strings.Contains(s[:comma], "base64") {
		s = s[comma+1:]
	}
	clean := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, s)
	if raw, err := base64.StdEncoding.DecodeString(clean); err == nil {
		return raw, nil
	}
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(clean, "="))
}

func (s *Server) handleGetWallpaper(w http.ResponseWriter, r *http.Request) {
	tok := extractBearerHeader(r)
	if tok == "" {
		tok = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if !s.verifyToken(tok) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	wpPath := s.settingsStore.WallpaperPath()
	raw, err := os.ReadFile(wpPath)
	if err != nil || len(raw) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	ct := s.settingsStore.Get().Appearance.WallpaperMime
	if !strings.HasPrefix(ct, "image/") {
		ct = detectImageMimeType(raw, "")
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Server) handleUploadWallpaper(w http.ResponseWriter, r *http.Request) {
	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	hintName := strings.TrimSpace(r.URL.Query().Get("name"))
	if hintName == "" {
		hintName = strings.TrimSpace(r.Header.Get("X-File-Name"))
	}

	var raw []byte
	if strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "application/octet-stream") {
		data, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
		if err != nil || len(data) == 0 {
			writeJSONError(w, http.StatusBadRequest, "empty image upload stream")
			return
		}
		raw = data
	} else {
		var body struct {
			LocalPath  string `json:"local_path"`
			Name       string `json:"name"`
			DataBase64 string `json:"data_base64"`
		}
		if err := decodeJSONWithLimit(r, &body, 64<<20); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid wallpaper payload: "+err.Error())
			return
		}
		if body.Name != "" {
			hintName = body.Name
		}
		if strings.TrimSpace(body.LocalPath) != "" {
			clean := expandUserLocalPath(body.LocalPath)
			if hintName == "" {
				hintName = filepath.Base(clean)
			}
			data, err := os.ReadFile(clean)
			if err != nil || len(data) == 0 {
				writeJSONError(w, http.StatusBadRequest, "failed to read local image: "+err.Error())
				return
			}
			raw = data
		} else if strings.TrimSpace(body.DataBase64) != "" {
			data, err := decodeBase64Flexible(body.DataBase64)
			if err != nil || len(data) == 0 {
				writeJSONError(w, http.StatusBadRequest, "invalid base64 image")
				return
			}
			raw = data
		} else {
			writeJSONError(w, http.StatusBadRequest, "local_path or data_base64 required")
			return
		}
	}

	mimeType := detectImageMimeType(raw, hintName)
	if err := os.WriteFile(s.settingsStore.WallpaperPath(), raw, 0o600); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _ := s.settingsStore.Update(func(cur *settings.AppSettings) {
		cur.Appearance.HasWallpaper = true
		cur.Appearance.WallpaperMime = mimeType
	})
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteWallpaper(w http.ResponseWriter, _ *http.Request) {
	_ = os.Remove(s.settingsStore.WallpaperPath())
	updated, _ := s.settingsStore.Update(func(cur *settings.AppSettings) {
		cur.Appearance.HasWallpaper = false
		cur.Appearance.WallpaperMime = ""
	})
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleUpsertAIProvider(w http.ResponseWriter, r *http.Request) {
	var in settings.UpsertProviderInput
	if err := decodeJSON(r, &in); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.settingsStore.UpsertAIProvider(r.Context(), in)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleSetDefaultAIProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	updated, err := s.settingsStore.SetDefaultAIProvider(id)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteAIProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	updated, err := s.settingsStore.DeleteAIProvider(id)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleAddCredential(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		Username string `json:"username"`
		Secret   string `json:"secret"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.settingsStore.AddCredential(r.Context(), body.Name, body.Kind, body.Username, body.Secret)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteCredential(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	updated, err := s.settingsStore.DeleteCredential(id)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleUpsertProxy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		settings.ProxyEntry
		Password string `json:"password,omitempty"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.settingsStore.UpsertProxy(r.Context(), body.ProxyEntry, body.Password)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// POST /api/v1/settings/proxies/import  {"text": "one link per line"}
// Pasting is the import: whichever links the user copied are the ones kept.
// handleImportProxies is the whole import UI: one link per line. Proxy and share links
// are added at once. A subscription URL (http(s) with a path or query) is fetched and its
// nodes returned as candidates first; the client sends the same text back with "pick"
// set to the keys to add. Nodes already imported from that subscription are refreshed
// in place by name (hosts keep routing through them); nothing is ever removed.
func (s *Server) handleImportProxies(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string    `json:"text"`
		Pick *[]string `json:"pick"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	type lineError struct {
		Line  int    `json:"line"`
		Error string `json:"error"`
	}
	type candidate struct {
		Key      string `json:"key"`
		Name     string `json:"name"`
		Scheme   string `json:"scheme"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Existing bool   `json:"existing"`

		entry      settings.ProxyEntry
		link       string
		existingID string
	}
	type plainLine struct {
		line int
		text string
	}
	errs := []lineError{}
	var plain []plainLine
	var candidates []candidate
	current := s.settingsStore.Get().Proxies
	for i, raw := range strings.Split(body.Text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !isSubscriptionURL(line) {
			plain = append(plain, plainLine{i + 1, line})
			continue
		}
		links, err := fetchSubscription(r.Context(), line)
		if err != nil {
			errs = append(errs, lineError{Line: i + 1, Error: err.Error()})
			continue
		}
		sum := sha256.Sum256([]byte(line))
		source := "sub:" + hex.EncodeToString(sum[:8])
		seen := map[string]int{}
		unsupported := 0
		for _, link := range links {
			entry, _, err := settings.ParseProxyLink(link)
			if err != nil {
				unsupported++
				continue
			}
			// airports reuse names rarely, but a refresh must map one name to one node
			if seen[entry.Name]++; seen[entry.Name] > 1 {
				entry.Name = fmt.Sprintf("%s (%d)", entry.Name, seen[entry.Name])
			}
			entry.Source = source
			c := candidate{
				Key: source + "/" + entry.Name, Name: entry.Name, Scheme: entry.Scheme,
				Host: entry.Host, Port: entry.Port, entry: entry, link: link,
			}
			for _, p := range current {
				if p.Source == source && p.Name == entry.Name {
					c.Existing, c.existingID = true, p.ID
					break
				}
			}
			candidates = append(candidates, c)
		}
		if unsupported > 0 {
			errs = append(errs, lineError{Line: i + 1, Error: fmt.Sprintf("订阅里有 %d 个节点暂不支持，已跳过", unsupported)})
		}
		if len(candidates) > 1000 {
			candidates = candidates[:1000]
		}
	}

	// A subscription was pasted: show its nodes before writing anything.
	if body.Pick == nil && len(candidates) > 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"settings": s.settingsStore.Get(), "imported": 0, "errors": errs, "candidates": candidates,
		})
		return
	}

	updated := s.settingsStore.Get()
	imported, refreshed := 0, 0
	for _, pl := range plain {
		entry, secret, err := settings.ParseProxyLink(pl.text)
		if err == nil {
			var next settings.AppSettings
			if next, err = s.settingsStore.UpsertProxy(r.Context(), entry, secret); err == nil {
				updated = next
			}
		}
		if err != nil {
			// report the line number only: the line may contain a password
			errs = append(errs, lineError{Line: pl.line, Error: err.Error()})
			continue
		}
		imported++
	}
	picked := map[string]bool{}
	if body.Pick != nil {
		for _, k := range *body.Pick {
			picked[k] = true
		}
	}
	for _, c := range candidates {
		delete(picked, c.Key)
		if !c.Existing && body.Pick != nil && !containsString(*body.Pick, c.Key) {
			continue
		}
		entry := c.entry
		entry.ID = c.existingID
		next, err := s.settingsStore.UpsertProxy(r.Context(), entry, c.link)
		if err != nil {
			errs = append(errs, lineError{Error: err.Error()})
			continue
		}
		updated = next
		if c.Existing {
			refreshed++
		} else {
			imported++
		}
	}
	if len(picked) > 0 {
		// the subscription is fetched again on pick; it may have changed in between
		errs = append(errs, lineError{Error: fmt.Sprintf("有 %d 个勾选的节点在订阅里找不到了，请重新粘贴订阅", len(picked))})
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": updated, "imported": imported, "refreshed": refreshed, "errors": errs})
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// isSubscriptionURL tells a subscription (https://airport/api/sub?token=...) from an
// HTTP proxy (http://user:pass@1.2.3.4:8080), which has no path.
func isSubscriptionURL(line string) bool {
	u, err := url.Parse(line)
	if err != nil || u.Host == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return false
	}
	return (u.Path != "" && u.Path != "/") || u.RawQuery != ""
}

// fetchSubscription downloads a subscription and returns its share links. Errors never
// include the URL: its token is a credential.
func fetchSubscription(ctx context.Context, rawURL string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.New("订阅地址无效")
	}
	// airports pick the response format by client; this one gets base64 share links
	req.Header.Set("User-Agent", "v2rayN/7.0")
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("订阅拉取失败：连不上订阅服务器")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("订阅拉取失败：HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, errors.New("订阅拉取失败：读取中断")
	}
	return sharelink.DecodeSubscription(data)
}

func (s *Server) handleDeleteProxy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// A proxy a host still routes through cannot disappear under it.
	var users []string
	for _, hp := range s.hostStore.List() {
		for _, pid := range hp.Route.ProxyIDs {
			if pid == id {
				users = append(users, hp.Name)
				break
			}
		}
	}
	if len(users) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "以下主机正在使用这个代理，请先在主机里换掉：" + strings.Join(users, "、"),
			"code":  "PROXY_IN_USE",
			"hosts": users,
		})
		return
	}
	for _, p := range s.settingsStore.Get().Proxies {
		if p.ID == id && p.SecretRef != "" && s.bridge != nil {
			_ = s.bridge.SetSecret(r.Context(), p.SecretRef, "") // best effort: bridges have no delete
		}
	}
	updated, err := s.settingsStore.DeleteProxy(id)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

type backupBundle struct {
	Version    string                `json:"version"`
	ExportedAt time.Time             `json:"exported_at"`
	Settings   settings.AppSettings  `json:"settings"`
	Hosts      []hosts.HostProfile   `json:"hosts"`
	Snippets   []hosts.Snippet       `json:"snippets"`
}

func (s *Server) handleBackupExport(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, backupBundle{
		Version:    "0.2.2",
		ExportedAt: time.Now(),
		Settings:   s.settingsStore.Get(),
		Hosts:      s.hostStore.List(),
		Snippets:   s.hostStore.ListSnippets(),
	})
}

func (s *Server) handleBackupImport(w http.ResponseWriter, r *http.Request) {
	var b backupBundle
	if err := decodeJSON(r, &b); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	_, _ = s.settingsStore.Update(func(cur *settings.AppSettings) {
		cur.Appearance = b.Settings.Appearance
		cur.Terminal = b.Settings.Terminal
		cur.Monitor = b.Settings.Monitor
		cur.Files = b.Settings.Files
		if len(b.Settings.MCPServers) > 0 {
			cur.MCPServers = b.Settings.MCPServers
		}
	})
	for _, hp := range b.Hosts {
		_, _ = s.hostStore.Upsert(r.Context(), hosts.UpsertInput{
			ID:       hp.ID,
			Name:     hp.Name,
			Group:    hp.Group,
			Host:     hp.Host,
			Port:     hp.Port,
			User:     hp.User,
			AuthMode: hp.AuthMode,
			HostKey:  hp.HostKey,
			Route:    hp.Route,
			Relay:    hp.Relay,
		})
	}
	for _, sn := range b.Snippets {
		_, _ = s.hostStore.UpsertSnippet(sn)
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": true})
}

func (s *Server) handleSessionScrollback(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	maxLines := 200
	if q := r.URL.Query().Get("lines"); q != "" {
		if parsed, err := strconv.Atoi(q); err == nil && parsed >= 0 {
			maxLines = parsed
		}
	}
	if maxLines == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"text": ""})
		return
	}
	raw := string(ms.scrollback.Snapshot())
	lines := strings.Split(raw, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": strings.Join(lines, "\n")})
}
