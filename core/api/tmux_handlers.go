package api

import (
	"context"
	"crypto/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/shnex/shnex/core/relay"
	"github.com/shnex/shnex/core/transport"
)

// RemoteTmuxSession is one tmux session on the remote host, shown as a top tab on desktop and Android.
type RemoteTmuxSession struct {
	ID              string `json:"id"`               // exact tmux session_name ([A-Za-z0-9_-]{1,48})
	Title           string `json:"title"`            // @shnex_title, falls back to ID
	Command         string `json:"command"`          // command of the active pane (or of a busy pane)
	Busy            bool   `json:"busy"`             // any pane runs something other than an idle shell
	Path            string `json:"path"`             // current directory of the active pane
	AttachedClients int    `json:"attached_clients"` // #{session_attached}
	CreatedUnix     int64  `json:"created_unix"`     // #{session_created}; tabs are ordered by it
}

type remoteTmuxList struct {
	Available bool                `json:"available"`
	Sessions  []RemoteTmuxSession `json:"sessions"`
}

const (
	// tmux history for scrolling back on clients whose local screen is tmux's alternate screen.
	tmuxHistoryDefaultLines = 3000
	tmuxHistoryMaxLines     = 10000
	// stays below the exec output cap (ssh maxExecOutputBytes = 128 KiB); tail keeps the newest bytes
	tmuxHistoryMaxBytes = 120 * 1024

	tmuxNoTmuxMarker  = "__NO_TMUX__"
	maxTmuxTitleRunes = 40
	tmuxExecTimeout   = 15 * time.Second
	tmuxTitleOption   = "@shnex_title"
)

var idleShellCommands = map[string]bool{
	"bash": true, "zsh": true, "sh": true, "fish": true, "dash": true, "ash": true, "ksh": true,
}

// tmuxListPanesFields is joined with a real tab byte; the title goes last so it may contain anything.
var tmuxListPanesFields = []string{
	"#{session_name}",
	"#{session_created}",
	"#{session_attached}",
	"#{window_active}",
	"#{pane_active}",
	"#{pane_current_command}",
	"#{pane_current_path}",
	"#{" + tmuxTitleOption + "}",
}

// tmuxListCommand lists every pane of every session. "-u" makes tmux emit UTF-8 even though
// the non-interactive exec channel carries no locale.
func tmuxListCommand() string {
	format := strings.Join(tmuxListPanesFields, "\t")
	return "command -v tmux >/dev/null 2>&1 || { echo " + tmuxNoTmuxMarker + "; exit 0; }; " +
		"tmux -u list-panes -a -F " + relay.PosixQuote(format) + " 2>/dev/null || true"
}

// validTmuxID reports whether name is already a safe, exact session name. Names that
// SanitizeWorkspaceName would rewrite are rejected rather than silently mapped to another
// session (an empty name would otherwise become "shnex-main").
func validTmuxID(name string) bool {
	return name != "" && relay.SanitizeWorkspaceName(name) == name
}

// tmuxTarget returns an exact-match target; plain "-t name" is a prefix/pattern match in tmux.
func tmuxTarget(id string) string { return relay.PosixQuote("=" + id) }

// tmuxOptionTarget targets the session (its current window) for set-option.
func tmuxOptionTarget(id string) string { return relay.PosixQuote("=" + id + ":") }

func sanitizeTmuxTitle(raw string) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.TrimSpace(raw) {
		if unicode.IsControl(r) {
			continue
		}
		if n >= maxTmuxTitleRunes {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

func isIdleShell(cmd string) bool {
	c := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(cmd)), "-")
	return c == "" || idleShellCommands[c]
}

// parseTmuxPanes folds list-panes output into one entry per session, ordered by creation time.
func parseTmuxPanes(out string) (available bool, sessions []RemoteTmuxSession) {
	if strings.Contains(out, tmuxNoTmuxMarker) {
		return false, []RemoteTmuxSession{}
	}
	type acc struct {
		s             RemoteTmuxSession
		activeCommand string
		activePath    string
		busyCommand   string
		busyPath      string
	}
	byID := map[string]*acc{}
	order := []string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", len(tmuxListPanesFields))
		if len(parts) < len(tmuxListPanesFields) {
			continue
		}
		id := parts[0]
		if !validTmuxID(id) {
			continue // external session with a name we could not attach to exactly
		}
		a, ok := byID[id]
		if !ok {
			created, _ := strconv.ParseInt(parts[1], 10, 64)
			attached, _ := strconv.Atoi(parts[2])
			a = &acc{s: RemoteTmuxSession{ID: id, CreatedUnix: created, AttachedClients: attached}}
			byID[id] = a
			order = append(order, id)
		}
		if a.s.Title == "" {
			a.s.Title = sanitizeTmuxTitle(parts[7])
		}
		cmd, path := parts[5], parts[6]
		if parts[3] == "1" && parts[4] == "1" {
			a.activeCommand, a.activePath = cmd, path
		}
		if !isIdleShell(cmd) {
			a.s.Busy = true
			if a.busyCommand == "" {
				a.busyCommand, a.busyPath = cmd, path
			}
		}
	}

	sessions = make([]RemoteTmuxSession, 0, len(order))
	for _, id := range order {
		a := byID[id]
		s := a.s
		s.Command, s.Path = a.activeCommand, a.activePath
		if s.Busy && isIdleShell(s.Command) {
			s.Command = a.busyCommand // tell the user what would actually be killed
		}
		if s.Path == "" {
			s.Path = a.busyPath
		}
		if s.Title == "" {
			s.Title = s.ID
		}
		sessions = append(sessions, s)
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].CreatedUnix != sessions[j].CreatedUnix {
			return sessions[i].CreatedUnix < sessions[j].CreatedUnix
		}
		return sessions[i].ID < sessions[j].ID
	})
	return true, sessions
}

const base36Digits = "0123456789abcdefghijklmnopqrstuvwxyz"

// newTmuxID returns a collision-resistant session name, so two devices creating a
// conversation at the same moment never race for the same "shnex-N".
func newTmuxID() string {
	var rnd [2]byte
	_, _ = rand.Read(rnd[:])
	return "shnex-" + strconv.FormatInt(time.Now().UnixMilli(), 36) +
		string(base36Digits[int(rnd[0])%36]) + string(base36Digits[int(rnd[1])%36])
}

func defaultTmuxTitle(existing int) string {
	return "对话" + strconv.Itoa(existing+1)
}

// workspaceNameFor is the tmux/zellij/agent workspace a relay config actually attaches to.
func workspaceNameFor(cfg transport.RelayConfig) string {
	if cfg.Mode == "" || cfg.Mode == transport.RelayNone {
		return strings.TrimSpace(cfg.SessionName)
	}
	return relay.SanitizeWorkspaceName(cfg.SessionName)
}

func (s *Server) tmuxExecutor(w http.ResponseWriter, r *http.Request) (transport.CommandExecutor, bool) {
	ms := s.getManagedSession(r.PathValue("id"))
	if ms == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return nil, false
	}
	execProv, ok := ms.sess.(transport.CommandExecutor)
	if !ok {
		writeJSONError(w, http.StatusNotImplemented, "transport does not support command execution")
		return nil, false
	}
	return execProv, true
}

func runTmux(ctx context.Context, execProv transport.CommandExecutor, command string) (*transport.CommandResult, error) {
	ctx, cancel := context.WithTimeout(ctx, tmuxExecTimeout)
	defer cancel()
	return execProv.ExecCommand(ctx, command)
}

func tmuxFailure(res *transport.CommandResult) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	if msg == "" {
		msg = "tmux exited with code " + strconv.Itoa(res.ExitCode)
	}
	return msg
}

func (s *Server) listRemoteTmux(ctx context.Context, execProv transport.CommandExecutor) (remoteTmuxList, error) {
	res, err := runTmux(ctx, execProv, tmuxListCommand())
	if err != nil {
		return remoteTmuxList{}, err
	}
	avail, sessions := parseTmuxPanes(res.Stdout)
	return remoteTmuxList{Available: avail, Sessions: sessions}, nil
}

// GET /api/v1/sessions/{id}/tmux
func (s *Server) handleListTmux(w http.ResponseWriter, r *http.Request) {
	execProv, ok := s.tmuxExecutor(w, r)
	if !ok {
		return
	}
	list, err := s.listRemoteTmux(r.Context(), execProv)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/v1/sessions/{id}/tmux  {"title"?: string}
func (s *Server) handleCreateTmux(w http.ResponseWriter, r *http.Request) {
	execProv, ok := s.tmuxExecutor(w, r)
	if !ok {
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &body); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	title := sanitizeTmuxTitle(body.Title)
	if title == "" {
		list, err := s.listRemoteTmux(r.Context(), execProv)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, err.Error())
			return
		}
		if !list.Available {
			writeJSONError(w, http.StatusConflict, "远端未安装 tmux")
			return
		}
		title = defaultTmuxTitle(len(list.Sessions))
	}

	id := newTmuxID()
	cmd := "tmux -u new-session -d -s " + relay.PosixQuote(id) +
		" && tmux -u set-option -t " + tmuxOptionTarget(id) + " " + tmuxTitleOption + " " + relay.PosixQuote(title)
	res, err := runTmux(r.Context(), execProv, cmd)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	if res.ExitCode != 0 {
		writeJSONError(w, http.StatusBadGateway, tmuxFailure(res))
		return
	}
	writeJSON(w, http.StatusCreated, RemoteTmuxSession{
		ID:          id,
		Title:       title,
		CreatedUnix: time.Now().Unix(),
	})
}

// PATCH /api/v1/sessions/{id}/tmux/{name}  {"title": string}
func (s *Server) handleRenameTmux(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validTmuxID(name) {
		writeJSONError(w, http.StatusBadRequest, "invalid tmux session name")
		return
	}
	execProv, ok := s.tmuxExecutor(w, r)
	if !ok {
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	title := sanitizeTmuxTitle(body.Title)
	if title == "" {
		writeJSONError(w, http.StatusBadRequest, "标题不能为空")
		return
	}
	cmd := "tmux -u set-option -t " + tmuxOptionTarget(name) + " " + tmuxTitleOption + " " + relay.PosixQuote(title)
	res, err := runTmux(r.Context(), execProv, cmd)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	if res.ExitCode != 0 {
		writeJSONError(w, http.StatusNotFound, tmuxFailure(res))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": name, "title": title})
}

// tmuxHistoryCommand prints the last lines of the session's active pane (history + visible
// screen) with SGR colors, keeping only the newest tmuxHistoryMaxBytes bytes.
func tmuxHistoryCommand(id string, lines int) string {
	return "tmux -u capture-pane -p -e -S -" + strconv.Itoa(lines) + " -t " + tmuxOptionTarget(id) +
		" | tail -c " + strconv.Itoa(tmuxHistoryMaxBytes)
}

// trimHistory drops the first line when tail cut the output, since it may start mid-sequence.
func trimHistory(out string) string {
	if len(out) < tmuxHistoryMaxBytes {
		return out
	}
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		return out[i+1:]
	}
	return ""
}

// GET /api/v1/sessions/{id}/tmux/{name}/history?lines=N
//
// Inside tmux the client only ever sees tmux's alternate screen, so its local scrollback is
// empty; the history lives on the remote host. Reading it never touches tmux state
// (no copy-mode), so typing and the other attached device are unaffected.
func (s *Server) handleTmuxHistory(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validTmuxID(name) {
		writeJSONError(w, http.StatusBadRequest, "invalid tmux session name")
		return
	}
	execProv, ok := s.tmuxExecutor(w, r)
	if !ok {
		return
	}
	lines := tmuxHistoryDefaultLines
	if v, err := strconv.Atoi(r.URL.Query().Get("lines")); err == nil && v > 0 {
		lines = min(v, tmuxHistoryMaxLines)
	}
	res, err := runTmux(r.Context(), execProv, tmuxHistoryCommand(name, lines))
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	if res.ExitCode != 0 {
		writeJSONError(w, http.StatusNotFound, tmuxFailure(res))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": trimHistory(res.Stdout)})
}

// DELETE /api/v1/sessions/{id}/tmux/{name}
//
// If the connection executing the kill is itself attached to that tmux session, the tmux
// client exits and the SSH connection closes, so the response may be lost; clients treat a
// failure here as "probably ended" and re-list to confirm.
func (s *Server) handleKillTmux(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validTmuxID(name) {
		writeJSONError(w, http.StatusBadRequest, "invalid tmux session name")
		return
	}
	execProv, ok := s.tmuxExecutor(w, r)
	if !ok {
		return
	}
	res, err := runTmux(r.Context(), execProv, "tmux kill-session -t "+tmuxTarget(name))
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	if res.ExitCode != 0 {
		writeJSONError(w, http.StatusNotFound, tmuxFailure(res))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": name})
}
