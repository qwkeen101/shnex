package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SessionLogMeta describes a recorded terminal session log file for the "会话日志" pane.
type SessionLogMeta struct {
	Name      string    `json:"name"`
	HostLabel string    `json:"host_label"`
	SizeBytes int64     `json:"size_bytes"`
	UpdatedAt time.Time `json:"updated_at"`
	Active    bool      `json:"active"`
}

type sessionLogWriter struct {
	mu   sync.Mutex
	file *os.File
	name string
}

func openSessionLogWriter(dir, hostLabel, user, target, sessionID string) (*sessionLogWriter, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	safeHost := sanitizeLogToken(hostLabel)
	if safeHost == "" {
		safeHost = sanitizeLogToken(target)
	}
	safeSess := sanitizeLogToken(sessionID)
	fileName := fmt.Sprintf("%s_%s_%s.log", time.Now().Format("20060102-150405"), safeHost, safeSess)
	fullPath := filepath.Join(dir, fileName)

	f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	header := fmt.Sprintf(
		"# Shnex 终端会话日志\n# 主机: %s (%s@%s)\n# 会话: %s\n# 开始时间: %s\n# ------------------------------------------------------------\n",
		hostLabel, user, target, sessionID, time.Now().Format(time.RFC3339),
	)
	_, _ = f.WriteString(header)
	return &sessionLogWriter{file: f, name: fileName}, nil
}

func (lw *sessionLogWriter) WriteChunk(raw []byte) {
	if lw == nil || len(raw) == 0 {
		return
	}
	cleaned := stripANSILogBytes(raw)
	if len(cleaned) == 0 {
		return
	}
	lw.mu.Lock()
	if lw.file != nil {
		_, _ = lw.file.Write(cleaned)
	}
	lw.mu.Unlock()
}

func (lw *sessionLogWriter) Close() {
	if lw == nil {
		return
	}
	lw.mu.Lock()
	if lw.file != nil {
		_, _ = fmt.Fprintf(lw.file, "\n# [会话结束于 %s]\n", time.Now().Format(time.RFC3339))
		_ = lw.file.Close()
		lw.file = nil
	}
	lw.mu.Unlock()
}

func sanitizeLogToken(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_':
			b.WriteRune(r)
		case r > 127:
			b.WriteRune(r)
		case r == '.' || r == ':':
			b.WriteByte('-')
		}
		if b.Len() >= 40 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// stripANSILogBytes removes CSI/OSC escape sequences and CRs so saved session logs are human-readable.
func stripANSILogBytes(in []byte) []byte {
	out := make([]byte, 0, len(in))
	i := 0
	for i < len(in) {
		b := in[i]
		if b == 0x1b {
			i++
			if i >= len(in) {
				break
			}
			next := in[i]
			if next == '[' { // CSI
				i++
				for i < len(in) {
					c := in[i]
					i++
					if c >= 0x40 && c <= 0x7e {
						break
					}
				}
				continue
			}
			if next == ']' { // OSC
				i++
				for i < len(in) {
					if in[i] == 0x07 {
						i++
						break
					}
					if in[i] == 0x1b && i+1 < len(in) && in[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
				continue
			}
			i++
			continue
		}
		if b == '\r' || b == 0x07 || b == 0x08 {
			i++
			continue
		}
		out = append(out, b)
		i++
	}
	return out
}

func isValidLogFileName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || !strings.HasSuffix(name, ".log") {
		return false
	}
	if filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return false
	}
	return true
}

func parseHostLabelFromLogName(name string) string {
	base := strings.TrimSuffix(name, ".log")
	parts := strings.Split(base, "_")
	if len(parts) >= 3 {
		return strings.Join(parts[1:len(parts)-1], "_")
	}
	return base
}

func (s *Server) handleListSessionLogs(w http.ResponseWriter, _ *http.Request) {
	entries, err := os.ReadDir(s.logsDir)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"logs": []SessionLogMeta{}})
		return
	}

	activeNames := make(map[string]bool)
	s.mu.RLock()
	for _, ms := range s.sessions {
		if ms.logWriter != nil && ms.logWriter.name != "" {
			activeNames[ms.logWriter.name] = true
		}
	}
	s.mu.RUnlock()

	logs := make([]SessionLogMeta, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".log") {
			continue
		}
		info, iErr := ent.Info()
		if iErr != nil {
			continue
		}
		logs = append(logs, SessionLogMeta{
			Name:      ent.Name(),
			HostLabel: parseHostLabelFromLogName(ent.Name()),
			SizeBytes: info.Size(),
			UpdatedAt: info.ModTime(),
			Active:    activeNames[ent.Name()],
		})
	}

	sort.Slice(logs, func(i, j int) bool {
		return logs[i].UpdatedAt.After(logs[j].UpdatedAt)
	})
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

func (s *Server) handleGetSessionLog(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !isValidLogFileName(name) {
		writeJSONError(w, http.StatusBadRequest, "invalid log file name")
		return
	}
	fullPath := filepath.Join(s.logsDir, name)
	f, err := os.Open(fullPath)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "session log not found")
		return
	}
	defer f.Close()

	const maxTailBytes = 256 * 1024
	if st, sErr := f.Stat(); sErr == nil && st.Size() > maxTailBytes {
		_, _ = f.Seek(st.Size()-maxTailBytes, io.SeekStart)
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxTailBytes))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    name,
		"content": string(raw),
	})
}

func (s *Server) handleDeleteSessionLog(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !isValidLogFileName(name) {
		writeJSONError(w, http.StatusBadRequest, "invalid log file name")
		return
	}
	fullPath := filepath.Join(s.logsDir, name)
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": name})
}
