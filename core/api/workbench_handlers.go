package api

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shnex/shnex/core/ai"
	"github.com/shnex/shnex/core/completion"
	"github.com/shnex/shnex/core/hosts"
	"github.com/shnex/shnex/core/localpty"
	"github.com/shnex/shnex/core/markdown"
	"github.com/shnex/shnex/core/platform"
	"github.com/shnex/shnex/core/relay"
	"github.com/shnex/shnex/core/transport"
)

func (s *Server) handleListNotifications(w http.ResponseWriter, _ *http.Request) {
	if hp, ok := s.bridge.(platform.NotificationHistoryProvider); ok {
		writeJSON(w, http.StatusOK, map[string]any{"notifications": hp.Notifications()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": []platform.Notification{}})
}

func (s *Server) handleSendTestNotification(w http.ResponseWriter, r *http.Request) {
	n := platform.Notification{
		Title:     "Shnex 测试通知",
		Body:      "通知服务运行正常，Claude 审批与续费提醒将通过此通道送达。",
		Category:  platform.CategoryTaskIdle,
		CreatedAt: time.Now(),
	}
	if s.bridge != nil {
		if err := s.bridge.Notify(r.Context(), n); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": true, "notification": n})
}

func (s *Server) handleListForwards(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	fp, ok := ms.sess.(transport.PortForwardProvider)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"forwards": []transport.PortForwardRule{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"forwards": fp.ListForwards()})
}

func (s *Server) handleStartForward(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	fp, ok := ms.sess.(transport.PortForwardProvider)
	if !ok {
		writeJSONError(w, http.StatusNotImplemented, "transport does not support port forwarding")
		return
	}
	var body struct {
		LocalPort  int    `json:"local_port"`
		RemoteAddr string `json:"remote_addr"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.RemoteAddr) == "" {
		writeJSONError(w, http.StatusBadRequest, "local_port and remote_addr required")
		return
	}
	rule, err := fp.StartLocalForward(r.Context(), body.LocalPort, body.RemoteAddr)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) handleStopForward(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	fp, ok := ms.sess.(transport.PortForwardProvider)
	if !ok {
		writeJSONError(w, http.StatusNotImplemented, "transport does not support port forwarding")
		return
	}
	fwdID := r.PathValue("fwdId")
	if err := fp.StopForward(fwdID); err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stopped": fwdID})
}

func (s *Server) handleSessionComplete(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	var req completion.SuggestRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp := s.compEngine.Suggest(r.Context(), ms.compInspector, req)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSessionCompleteAccept(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind  completion.CandidateKind `json:"kind"`
		Label string                   `json:"label"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Label != "" {
		s.compEngine.RecordSelection(body.Kind, body.Label)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- Native Markdown & TeX Formula Typesetting Handlers ---

func (s *Server) handleMarkdownSample(w http.ResponseWriter, _ *http.Request) {
	doc := markdown.Compile(markdown.SampleMathDocumentMarkdown)
	writeJSON(w, http.StatusOK, map[string]any{
		"title":    markdown.SampleMathDocumentName,
		"content":  markdown.SampleMathDocumentMarkdown,
		"document": doc,
	})
}

func (s *Server) handleMarkdownRender(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	doc := markdown.Compile(body.Content)
	writeJSON(w, http.StatusOK, doc)
}

func expandUserLocalPath(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~/"), `~\`))
		}
	}
	return filepath.Clean(p)
}

func (s *Server) handleMarkdownReadLocal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Path) == "" {
		writeJSONError(w, http.StatusBadRequest, "path required")
		return
	}
	cleanPath := expandUserLocalPath(body.Path)
	st, err := os.Stat(cleanPath)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if st.IsDir() {
		writeJSONError(w, http.StatusBadRequest, "target path is a directory")
		return
	}
	if st.Size() > 4*1024*1024 {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "markdown file exceeds 4 MiB limit")
		return
	}
	raw, err := os.ReadFile(cleanPath)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	content := string(raw)
	writeJSON(w, http.StatusOK, map[string]any{
		"title":    filepath.Base(cleanPath),
		"path":     cleanPath,
		"content":  content,
		"document": markdown.Compile(content),
	})
}

func (s *Server) handleMarkdownWriteLocal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Path) == "" {
		writeJSONError(w, http.StatusBadRequest, "path required")
		return
	}
	cleanPath := expandUserLocalPath(body.Path)
	if err := os.WriteFile(cleanPath, []byte(body.Content), 0o644); err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"saved":    true,
		"path":     cleanPath,
		"document": markdown.Compile(body.Content),
	})
}

func (s *Server) handleMarkdownAsset(w http.ResponseWriter, r *http.Request) {
	tok := extractBearerHeader(r)
	if tok == "" {
		tok = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if !s.verifyToken(tok) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	src := strings.TrimSpace(r.URL.Query().Get("src"))
	if src == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	sessID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	sftpBase := strings.TrimSpace(r.URL.Query().Get("sftp_base"))
	localBase := strings.TrimSpace(r.URL.Query().Get("local_base"))

	if sessID != "" {
		ms := s.getManagedSession(sessID)
		if ms == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		sftpProv, ok := ms.sess.(transport.SFTPProvider)
		if !ok {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		target := src
		if !strings.HasPrefix(src, "/") && sftpBase != "" {
			target = path.Join(path.Dir(sftpBase), src)
		}
		raw, err := sftpProv.ReadFile(r.Context(), target, 32<<20)
		if err != nil || len(raw) == 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", detectImageMimeType(raw, target))
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
		return
	}

	cleanSrc := strings.TrimPrefix(src, "file:///")
	cleanSrc = strings.TrimPrefix(cleanSrc, "file://")
	resolved := cleanSrc
	if !filepath.IsAbs(resolved) && localBase != "" {
		resolved = filepath.Join(filepath.Dir(expandUserLocalPath(localBase)), filepath.FromSlash(resolved))
	} else {
		resolved = expandUserLocalPath(filepath.FromSlash(resolved))
	}
	raw, err := os.ReadFile(resolved)
	if err != nil || len(raw) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", detectImageMimeType(raw, resolved))
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// --- Hosts & Snippets Handlers ---

func (s *Server) handleListHosts(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"hosts": s.hostStore.List()})
}

func (s *Server) handleUpsertHost(w http.ResponseWriter, r *http.Request) {
	var in hosts.UpsertInput
	if err := decodeJSON(r, &in); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	profile, err := s.hostStore.Upsert(r.Context(), in)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) handleDeleteHost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.hostStore.Delete(id) {
		writeJSONError(w, http.StatusNotFound, "host not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (s *Server) handleToggleFavoriteHost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	hp, ok := s.hostStore.ToggleFavorite(id)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "host not found")
		return
	}
	writeJSON(w, http.StatusOK, hp)
}

func (s *Server) handleImportSSHConfig(w http.ResponseWriter, r *http.Request) {
	imported, err := s.hostStore.ImportLocalSSHConfig(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"imported_count": len(imported),
		"imported":       imported,
		"hosts":          s.hostStore.List(),
	})
}

func (s *Server) handlePingAllHosts(w http.ResponseWriter, r *http.Request) {
	list := s.hostStore.List()
	results := make(map[string]int64, len(list))
	for _, hp := range list {
		ms, err := hosts.PingHostTCP(r.Context(), hp.Host, hp.Port)
		if err != nil {
			results[hp.ID] = -1
		} else {
			results[hp.ID] = ms
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"latencies_ms": results})
}

func (s *Server) handleListSnippets(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"snippets": s.hostStore.ListSnippets()})
}

func (s *Server) handleUpsertSnippet(w http.ResponseWriter, r *http.Request) {
	var sn hosts.Snippet
	if err := decodeJSON(r, &sn); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := s.hostStore.UpsertSnippet(sn)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snippets": list})
}

func (s *Server) handleDeleteSnippet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	list := s.hostStore.DeleteSnippet(id)
	writeJSON(w, http.StatusOK, map[string]any{"snippets": list})
}

// --- Server System Monitor & Exec Handlers ---

func (s *Server) handleSessionStats(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	sp, ok := ms.sess.(transport.StatsProvider)
	if !ok {
		writeJSONError(w, http.StatusNotImplemented, "transport does not support system stats")
		return
	}
	stats, err := sp.CollectStats(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleSessionExec(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	execProv, ok := ms.sess.(transport.CommandExecutor)
	if !ok {
		writeJSONError(w, http.StatusNotImplemented, "transport does not support command execution")
		return
	}
	var body struct {
		Command string `json:"command"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Command) == "" {
		writeJSONError(w, http.StatusBadRequest, "command is required")
		return
	}
	res, err := execProv.ExecCommand(r.Context(), body.Command)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// --- SFTP Handlers ---

func writeSFTPError(w http.ResponseWriter, err error) {
	if errors.Is(err, transport.ErrSudoPasswordRequired) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "sudo 提权需要输入密码（仅在本次运行中保存在内存）",
			"code":  "SUDO_PASSWORD_NEEDED",
		})
		return
	}
	writeJSONError(w, http.StatusBadGateway, err.Error())
}

func (s *Server) handleSFTPSudoPassword(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	sudoCfg, ok := ms.sess.(transport.SFTPSudoConfigurable)
	if !ok {
		writeJSONError(w, http.StatusNotImplemented, "transport does not support sftp sudo")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	sudoCfg.SetSFTPSudoPassword(body.Password)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSFTPList(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	sftpProv, ok := ms.sess.(transport.SFTPProvider)
	if !ok {
		writeJSONError(w, http.StatusNotImplemented, "transport does not support sftp")
		return
	}
	dirPath := r.URL.Query().Get("path")
	resolved, entries, err := sftpProv.ListDir(r.Context(), dirPath)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":    resolved,
		"entries": entries,
	})
}

func (s *Server) handleSFTPRead(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	sftpProv, ok := ms.sess.(transport.SFTPProvider)
	if !ok {
		writeJSONError(w, http.StatusNotImplemented, "transport does not support sftp")
		return
	}
	remotePath := strings.TrimSpace(r.URL.Query().Get("path"))
	if remotePath == "" {
		writeJSONError(w, http.StatusBadRequest, "path query parameter required")
		return
	}
	data, err := sftpProv.ReadFile(r.Context(), remotePath, 20<<20)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":           remotePath,
		"size":           len(data),
		"content_base64": base64.StdEncoding.EncodeToString(data),
	})
}

func (s *Server) handleSFTPWrite(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	sftpProv, ok := ms.sess.(transport.SFTPProvider)
	if !ok {
		writeJSONError(w, http.StatusNotImplemented, "transport does not support sftp")
		return
	}
	var body struct {
		Path          string `json:"path"`
		ContentBase64 string `json:"content_base64"`
	}
	if err := decodeJSONWithLimit(r, &body, 64<<20); err != nil || strings.TrimSpace(body.Path) == "" {
		writeJSONError(w, http.StatusBadRequest, "path and content_base64 required")
		return
	}
	raw, err := decodeBase64Flexible(body.ContentBase64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid base64 content")
		return
	}
	if err := sftpProv.WriteFile(r.Context(), body.Path, raw); err != nil {
		writeSFTPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"written": len(raw), "path": body.Path})
}

func (s *Server) handleSFTPUploadRaw(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	sftpProv, ok := ms.sess.(transport.SFTPProvider)
	if !ok {
		writeJSONError(w, http.StatusNotImplemented, "transport does not support sftp")
		return
	}
	destPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if destPath == "" {
		writeJSONError(w, http.StatusBadRequest, "path query parameter required")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 256<<20))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "failed reading upload stream: "+err.Error())
		return
	}
	if err := sftpProv.WriteFile(r.Context(), destPath, raw); err != nil {
		writeSFTPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"written": len(raw), "path": destPath})
}

func (s *Server) handleSFTPUploadLocal(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	sftpProv, ok := ms.sess.(transport.SFTPProvider)
	if !ok {
		writeJSONError(w, http.StatusNotImplemented, "transport does not support sftp")
		return
	}
	var body struct {
		LocalPaths []string `json:"local_paths"`
		DestDir    string   `json:"dest_dir"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.LocalPaths) == 0 {
		writeJSONError(w, http.StatusBadRequest, "local_paths required")
		return
	}
	destDir := strings.TrimSpace(body.DestDir)
	if destDir == "" {
		destDir = "."
	}
	var uploaded []string
	for _, lp := range body.LocalPaths {
		clean := expandUserLocalPath(lp)
		raw, err := os.ReadFile(clean)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "failed to read local file: "+err.Error())
			return
		}
		remoteTarget := path.Join(destDir, filepath.Base(clean))
		if err := sftpProv.WriteFile(r.Context(), remoteTarget, raw); err != nil {
			writeSFTPError(w, err)
			return
		}
		uploaded = append(uploaded, remoteTarget)
	}
	writeJSON(w, http.StatusOK, map[string]any{"uploaded": uploaded})
}

func sanitizeImageExt(hintName string, raw []byte) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(hintName)))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".svg", ".avif":
		return ext
	}
	mime := detectImageMimeType(raw, hintName)
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/bmp":
		return ".bmp"
	case "image/svg+xml":
		return ".svg"
	case "image/avif":
		return ".avif"
	default:
		return ".png"
	}
}

func quoteShellPathIfNeeded(p string) string {
	if strings.ContainsAny(p, " \t()[]{}&;'\"") {
		return "'" + strings.ReplaceAll(p, "'", "''") + "'"
	}
	return p
}

// handleSessionUploadImage uploads a pasted/dropped/selected image to the session's target machine
// (local temp folder for local ConPTY, or /tmp/shnex_img_<ts>.<ext> via SFTP or shell fallback for remote SSH)
// and returns the ready-to-insert file path for Claude CLI / terminal commands.
func (s *Server) handleSessionUploadImage(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}

	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	hintName := strings.TrimSpace(r.URL.Query().Get("name"))
	if hintName == "" {
		hintName = strings.TrimSpace(r.Header.Get("X-File-Name"))
	}

	var raw []byte
	if strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "application/octet-stream") {
		data, err := io.ReadAll(io.LimitReader(r.Body, 48<<20))
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
			writeJSONError(w, http.StatusBadRequest, "invalid image upload payload: "+err.Error())
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
			// If session is already local on the same machine, we can reference the user's real local file directly!
			if ms.sess.Protocol() == localpty.ProtocolName {
				if st, err := os.Stat(clean); err == nil && !st.IsDir() {
					writeJSON(w, http.StatusOK, map[string]any{
						"path":      clean,
						"shell_arg": quoteShellPathIfNeeded(clean),
						"size":      st.Size(),
					})
					return
				}
			}
			data, err := os.ReadFile(clean)
			if err != nil || len(data) == 0 {
				writeJSONError(w, http.StatusBadRequest, "failed reading local image: "+err.Error())
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
			writeJSONError(w, http.StatusBadRequest, "image payload required")
			return
		}
	}

	ext := sanitizeImageExt(hintName, raw)
	fileName := fmt.Sprintf("shnex_img_%d%s", time.Now().UnixMilli(), ext)

	if ms.sess.Protocol() == localpty.ProtocolName {
		dir := filepath.Join(os.TempDir(), "shnex-images")
		_ = os.MkdirAll(dir, 0o700)
		fullPath := filepath.Join(dir, fileName)
		if err := os.WriteFile(fullPath, raw, 0o600); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"path":      fullPath,
			"shell_arg": quoteShellPathIfNeeded(fullPath),
			"size":      len(raw),
		})
		return
	}

	remotePath := "/tmp/" + fileName
	var writeErr error
	if sftpProv, ok := ms.sess.(transport.SFTPProvider); ok {
		writeErr = sftpProv.WriteFile(r.Context(), remotePath, raw)
	} else {
		writeErr = errors.New("no sftp provider")
	}

	// Automatic shell fallback if SFTP subsystem is unavailable or requires sudo
	if writeErr != nil {
		if execProv, ok := ms.sess.(transport.CommandExecutor); ok && len(raw) <= 8<<20 {
			qPath := relay.PosixQuote(remotePath)
			_, _ = execProv.ExecCommand(r.Context(), "rm -f -- "+qPath)
			b64 := base64.StdEncoding.EncodeToString(raw)
			const chunkSz = 32768
			fallbackErr := error(nil)
			for off := 0; off < len(b64); off += chunkSz {
				end := off + chunkSz
				if end > len(b64) {
					end = len(b64)
				}
				chunk := b64[off:end]
				cmd := fmt.Sprintf("printf '%%s' '%s' | (base64 -d 2>/dev/null || base64 -D) >> %s", chunk, qPath)
				if res, eErr := execProv.ExecCommand(r.Context(), cmd); eErr != nil || res.ExitCode != 0 {
					fallbackErr = fmt.Errorf("remote shell upload failed: %v", eErr)
					break
				}
			}
			if fallbackErr == nil {
				_, _ = execProv.ExecCommand(r.Context(), "chmod 644 "+qPath)
				writeErr = nil
			}
		}
	}

	if writeErr != nil {
		writeSFTPError(w, writeErr)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"path":      remotePath,
		"shell_arg": quoteShellPathIfNeeded(remotePath),
		"size":      len(raw),
	})
}

func (s *Server) handleSFTPMkdir(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	sftpProv, ok := ms.sess.(transport.SFTPProvider)
	if !ok {
		writeJSONError(w, http.StatusNotImplemented, "transport does not support sftp")
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Path) == "" {
		writeJSONError(w, http.StatusBadRequest, "path required")
		return
	}
	if err := sftpProv.MkdirAll(r.Context(), body.Path); err != nil {
		writeSFTPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": body.Path})
}

func (s *Server) handleSFTPRemove(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	sftpProv, ok := ms.sess.(transport.SFTPProvider)
	if !ok {
		writeJSONError(w, http.StatusNotImplemented, "transport does not support sftp")
		return
	}
	remotePath := strings.TrimSpace(r.URL.Query().Get("path"))
	if remotePath == "" {
		writeJSONError(w, http.StatusBadRequest, "path query parameter required")
		return
	}
	if err := sftpProv.Remove(r.Context(), remotePath); err != nil {
		writeSFTPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": remotePath})
}

// --- Built-in AI Ops Assistant Handlers ---

func (s *Server) handleGetAIConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.aiService.GetConfig(r.Context()))
}

func (s *Server) handleUpdateAIConfig(w http.ResponseWriter, r *http.Request) {
	var in ai.UpdateConfigInput
	if err := decodeJSON(r, &in); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg, err := s.aiService.UpdateConfig(r.Context(), in)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleSessionAIChat(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	execProv, ok := ms.sess.(transport.CommandExecutor)
	if !ok {
		writeJSONError(w, http.StatusNotImplemented, "transport does not support command execution")
		return
	}
	var req ai.ChatRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.aiService.RunTurn(r.Context(), ms.sess.Target(), execProv, req)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSessionAIApprove(w http.ResponseWriter, r *http.Request) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	execProv, ok := ms.sess.(transport.CommandExecutor)
	if !ok {
		writeJSONError(w, http.StatusNotImplemented, "transport does not support command execution")
		return
	}
	var body struct {
		CallID   string          `json:"call_id"`
		Command  string          `json:"command"`
		Approved bool            `json:"approved"`
		History  []ai.Message    `json:"history"`
		Policy   ai.ApprovalPolicy `json:"policy"`
		Model    string          `json:"model,omitempty"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	var step ai.ExecutedStep
	step.CallID = body.CallID
	step.Command = body.Command

	var toolMsg string
	if !body.Approved {
		step.Status = "rejected"
		toolMsg = "User rejected command execution."
	} else {
		cmdRes, err := execProv.ExecCommand(r.Context(), body.Command)
		if err != nil {
			step.Status = "failed"
			step.Stderr = err.Error()
			toolMsg = "ERROR: " + err.Error()
		} else {
			step.Status = "executed"
			step.Stdout = cmdRes.Stdout
			step.Stderr = cmdRes.Stderr
			step.ExitCode = cmdRes.ExitCode
			step.DurationMs = cmdRes.DurationMs
			toolMsg = fmt.Sprintf("exit_code=%d\nstdout:\n%s\nstderr:\n%s", cmdRes.ExitCode, cmdRes.Stdout, cmdRes.Stderr)
		}
	}

	nextHistory := append(body.History, ai.Message{
		Role:       "tool",
		ToolCallID: body.CallID,
		Content:    toolMsg,
	})

	res, err := s.aiService.RunTurn(r.Context(), ms.sess.Target(), execProv, ai.ChatRequest{
		Messages: nextHistory,
		Policy:   body.Policy,
		Model:    body.Model,
	})
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	res.Steps = append([]ai.ExecutedStep{step}, res.Steps...)
	writeJSON(w, http.StatusOK, res)
}

// applySavedHostProfile enriches req with stored host parameters when req.HostID is provided.
func (s *Server) applySavedHostProfile(req *CreateSessionRequest) error {
	if strings.TrimSpace(req.HostID) == "" {
		return nil
	}
	hp, ok := s.hostStore.Get(strings.TrimSpace(req.HostID))
	if !ok {
		return fmt.Errorf("saved host %q not found", req.HostID)
	}
	s.hostStore.TouchLastConnected(hp.ID)
	if req.Target == "" {
		port := hp.Port
		if port <= 0 {
			port = 22
		}
		req.Target = hp.Host + ":" + strconv.Itoa(port)
	}
	if req.HostTitle == "" {
		req.HostTitle = hp.Name
	}
	if req.User == "" {
		req.User = hp.User
	}
	if req.DefaultDir == "" && hp.DefaultDir != "" {
		req.DefaultDir = hp.DefaultDir
	}
	if req.PostLoginScript == "" && hp.PostLoginScript != "" {
		req.PostLoginScript = hp.PostLoginScript
	}
	if req.FileProtocol == "" && hp.FileProtocol != "" {
		req.FileProtocol = hp.FileProtocol
	}
	if !req.SFTPSudo && hp.SFTPSudo {
		req.SFTPSudo = true
	}
	globalTerm := s.settingsStore.Get().Terminal
	if req.Term == "" {
		if hp.TermType != "" {
			req.Term = hp.TermType
		} else if globalTerm.TermType != "" {
			req.Term = globalTerm.TermType
		}
	}
	if !req.RecordLog && hp.AutoSessionLog {
		req.RecordLog = true
	}
	if req.Relay.Mode == "" {
		if strings.TrimSpace(hp.TmuxSession) != "" {
			req.Relay = transport.RelayConfig{
				Mode:        transport.RelayTmux,
				SessionName: strings.TrimSpace(hp.TmuxSession),
			}
		} else {
			req.Relay = hp.Relay
		}
	}
	if !req.Route.UseTSNet && hp.Route.UseTSNet {
		req.Route.UseTSNet = true
	}
	if len(req.Route.ProxyIDs) == 0 && req.Route.ProxyURL == "" && len(hp.Route.ProxyIDs) > 0 {
		req.Route.ProxyIDs = append([]string(nil), hp.Route.ProxyIDs...)
	}
	if req.Route.ProxyURL == "" && hp.Route.ProxyURL != "" {
		req.Route.ProxyURL = hp.Route.ProxyURL
	}
	if req.Auth.Mode == "" && req.Auth.Password == "" && req.Auth.PrivateKeyPEM == "" {
		switch hp.AuthMode {
		case transport.AuthTailscaleSSH:
			req.Auth.Mode = transport.AuthTailscaleSSH
		case transport.AuthAuto:
			req.Auth.Mode = transport.AuthAuto
			if hp.CredentialID != "" {
				req.Auth.SecretKeyRef = "shnex:vault:" + hp.CredentialID
			} else {
				req.Auth.SecretKeyRef = hp.SecretRef
			}
		default:
			if hp.CredentialID != "" {
				req.Auth.Mode = transport.AuthSecretRef
				req.Auth.SecretKeyRef = "shnex:vault:" + hp.CredentialID
			} else if hp.HasSecret && hp.SecretRef != "" {
				req.Auth.Mode = transport.AuthSecretRef
				req.Auth.SecretKeyRef = hp.SecretRef
			} else {
				req.Auth.Mode = transport.AuthAuto
			}
		}
	}
	if !req.Auth.HostKey.InsecureIgnore && req.Auth.HostKey.PinnedSHA256 == "" && req.Auth.HostKey.KnownHostsPath == "" {
		req.Auth.HostKey = hp.HostKey
	}
	if req.KeepaliveSec == 0 {
		if globalTerm.KeepaliveSec > 0 {
			req.KeepaliveSec = globalTerm.KeepaliveSec
		} else {
			req.KeepaliveSec = 30
		}
	}
	if req.SilenceThresholdMs == 0 {
		req.SilenceThresholdMs = int((5 * time.Second).Milliseconds())
	}
	return nil
}
