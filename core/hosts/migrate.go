package hosts

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shnex/shnex/core/dialer"
	"github.com/shnex/shnex/core/transport"
)

// ImportLocalSSHConfig scans ~/.ssh/config (if present) and imports concrete Host entries
// (skipping wildcard '*' blocks), loading IdentityFile PEM keys into PlatformBridge safely.
func (s *Store) ImportLocalSSHConfig(ctx context.Context) ([]HostProfile, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cfgFile := filepath.Join(home, ".ssh", "config")
	raw, err := os.ReadFile(cfgFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []HostProfile{}, nil
		}
		return nil, err
	}
	return s.ImportSSHConfigText(ctx, string(raw), home)
}

// ImportSSHConfigText parses OpenSSH client config text and upserts discovered hosts.
func (s *Store) ImportSSHConfigText(ctx context.Context, content string, homeDir string) ([]HostProfile, error) {
	type block struct {
		alias        string
		hostName     string
		user         string
		port         int
		identityFile string
	}

	var blocks []block
	var cur *block

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip inline comment
		if idx := strings.IndexByte(line, '#'); idx > 0 {
			line = strings.TrimSpace(line[:idx])
		}
		key, val := splitDirective(line)
		if key == "" || val == "" {
			continue
		}

		switch strings.ToLower(key) {
		case "host":
			if cur != nil && isConcreteHostBlock(cur.alias, cur.hostName) {
				blocks = append(blocks, *cur)
			}
			// Take first alias token
			fields := strings.Fields(val)
			alias := fields[0]
			cur = &block{alias: alias, port: 22, user: "root"}
		case "hostname":
			if cur != nil {
				cur.hostName = strings.Trim(val, `"`)
			}
		case "user":
			if cur != nil {
				cur.user = strings.Trim(val, `"`)
			}
		case "port":
			if cur != nil {
				if p, err := strconv.Atoi(val); err == nil && p > 0 && p <= 65535 {
					cur.port = p
				}
			}
		case "identityfile":
			if cur != nil && cur.identityFile == "" {
				cur.identityFile = strings.Trim(val, `"`)
			}
		}
	}
	if cur != nil && isConcreteHostBlock(cur.alias, cur.hostName) {
		blocks = append(blocks, *cur)
	}

	var imported []HostProfile
	existing := s.List()

	for _, b := range blocks {
		addr := b.hostName
		if addr == "" {
			addr = b.alias
		}
		// Skip if already imported with same name & host
		alreadyExists := false
		for _, ex := range existing {
			if strings.EqualFold(ex.Name, b.alias) && strings.EqualFold(ex.Host, addr) {
				alreadyExists = true
				break
			}
		}
		if alreadyExists {
			continue
		}

		var pemContent string
		authMode := transport.AuthPassword
		if b.identityFile != "" {
			keyPath := b.identityFile
			if strings.HasPrefix(keyPath, "~/") || strings.HasPrefix(keyPath, `~\`) {
				if homeDir != "" {
					keyPath = filepath.Join(homeDir, keyPath[2:])
				}
			}
			if keyBytes, kErr := os.ReadFile(keyPath); kErr == nil && strings.Contains(string(keyBytes), "PRIVATE KEY") {
				pemContent = string(keyBytes)
				authMode = transport.AuthPrivateKey
			}
		}

		hp, err := s.Upsert(ctx, UpsertInput{
			Name:          b.alias,
			Host:          addr,
			Port:          b.port,
			User:          b.user,
			AuthMode:      authMode,
			PrivateKeyPEM: pemContent,
			HostKey:       transport.HostKeyPolicy{InsecureIgnore: true},
			Relay:         transport.RelayConfig{Mode: transport.RelayAuto, SessionName: "shnex-main"},
		})
		if err == nil {
			imported = append(imported, hp)
		}
	}
	return imported, nil
}

// PingHostTCP measures TCP connect latency in milliseconds to a saved host
// (falling back to OS/environment proxy if direct TCP is unreachable).
func PingHostTCP(ctx context.Context, host string, port int) (int64, error) {
	if port <= 0 {
		port = 22
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	sd := dialer.NewSmartDirectDialer(4*time.Second, 0)
	sd.StaggerDelay = 500 * time.Millisecond
	start := time.Now()
	conn, err := sd.Dial(ctx, addr)
	if err != nil {
		return -1, err
	}
	_ = conn.Close()
	return time.Since(start).Milliseconds(), nil
}

func isConcreteHostBlock(alias, hostName string) bool {
	if alias == "" || strings.ContainsAny(alias, "*?!") {
		return false
	}
	return alias != "" || hostName != ""
}

func splitDirective(line string) (string, string) {
	if eq := strings.IndexByte(line, '='); eq > 0 {
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		if !strings.ContainsAny(k, " \t") {
			return k, v
		}
	}
	for i, r := range line {
		if r == ' ' || r == '\t' {
			return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i:])
		}
	}
	return "", ""
}
