package relay

import (
	"fmt"
	"strings"

	"github.com/shnex/shnex/core/transport"
)

const DefaultWorkspaceName = "shnex-main"

// SanitizeWorkspaceName enforces a strict [a-zA-Z0-9_-] allowlist (max 48 chars)
// so workspace names can never inject shell metacharacters on remote hosts.
func SanitizeWorkspaceName(raw string) string {
	raw = strings.TrimSpace(raw)
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
			if b.Len() >= 48 {
				break
			}
		}
	}
	if b.Len() == 0 {
		return DefaultWorkspaceName
	}
	return b.String()
}

// PosixQuote wraps s in single quotes safe for POSIX /bin/sh evaluation.
func PosixQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ResolveAgentBinary returns the sanitized remote command expression to invoke shnex-agent.
func ResolveAgentBinary(customPath string) string {
	p := strings.TrimSpace(customPath)
	if p == "" {
		return ""
	}
	return PosixQuote(p)
}

// BuildPTYBootstrapCommand returns the remote shell command to execute inside an SSH PTY
// for RelayTmux, RelayZellij, or RelayAuto modes. Returns "" for RelayNone.
func BuildPTYBootstrapCommand(cfg transport.RelayConfig) string {
	mode := cfg.Mode
	if mode == "" || mode == transport.RelayNone {
		if cmd := strings.TrimSpace(cfg.Command); cmd != "" {
			return cmd
		}
		return ""
	}

	ws := SanitizeWorkspaceName(cfg.SessionName)
	qWS := PosixQuote(ws)
	extraCmd := strings.TrimSpace(cfg.Command)

	tmuxSnippet := buildTmuxAttachSnippet(qWS, extraCmd)
	zellijSnippet := buildZellijAttachSnippet(qWS)

	switch mode {
	case transport.RelayTmux:
		return fmt.Sprintf("sh -c %s", PosixQuote(tmuxSnippet))
	case transport.RelayZellij:
		return fmt.Sprintf("sh -c %s", PosixQuote(zellijSnippet))
	case transport.RelayAuto:
		// Probe tmux -> zellij -> fallback interactive login shell seamlessly without failing.
		script := fmt.Sprintf(
			"if command -v tmux >/dev/null 2>&1; then %s; "+
				"elif command -v zellij >/dev/null 2>&1; then %s; "+
				"else exec \"${SHELL:-/bin/sh}\" -l; fi",
			tmuxSnippet,
			zellijSnippet,
		)
		return fmt.Sprintf("sh -c %s", PosixQuote(script))
	default:
		return ""
	}
}

func buildTmuxAttachSnippet(quotedSession string, initialCmd string) string {
	// Use `tmux new-session -A -s <name>` which is atomic inside the tmux server:
	// if <name> already exists (even under concurrent attaches), it attaches cleanly
	// instead of failing with `duplicate session: <name>`.
	attachOrCreate := fmt.Sprintf("tmux new-session -A -s %s", quotedSession)
	if initialCmd != "" {
		attachOrCreate = fmt.Sprintf("tmux new-session -A -s %s %s", quotedSession, PosixQuote(initialCmd))
	}
	// Configure window-size=latest (tmux >= 2.9) best-effort before attaching,
	// and fall back to an interactive login shell only if tmux exits with an error code.
	return fmt.Sprintf(
		"(tmux start-server \\; set-option -g window-size latest >/dev/null 2>&1 || true); "+
			"%s || exec \"${SHELL:-/bin/sh}\" -l",
		attachOrCreate,
	)
}

func buildZellijAttachSnippet(quotedSession string) string {
	return fmt.Sprintf("zellij attach --create %s || exec \"${SHELL:-/bin/sh}\" -l", quotedSession)
}

// BuildAgentMuxExecCommand constructs the remote exec command for RelayShnexAgent mode,
// searching explicit AgentPath, $PATH, and ~/.local/bin/shnex-agent.
func BuildAgentMuxExecCommand(cfg transport.RelayConfig, size transport.TerminalSize, term string) string {
	ws := SanitizeWorkspaceName(cfg.SessionName)
	norm := size.Normalized()
	if strings.TrimSpace(term) == "" {
		term = "xterm-256color"
	}

	args := fmt.Sprintf(
		"mux-attach --session %s --cols %d --rows %d --term %s",
		PosixQuote(ws),
		norm.Cols,
		norm.Rows,
		PosixQuote(term),
	)
	if cmd := strings.TrimSpace(cfg.Command); cmd != "" {
		args += " --cmd " + PosixQuote(cmd)
	}

	if explicit := ResolveAgentBinary(cfg.AgentPath); explicit != "" {
		return fmt.Sprintf("sh -c %s", PosixQuote(fmt.Sprintf("exec %s %s", explicit, args)))
	}

	probeScript := fmt.Sprintf(
		"if command -v shnex-agent >/dev/null 2>&1; then exec shnex-agent %s; "+
			"elif [ -x \"$HOME/.local/bin/shnex-agent\" ]; then exec \"$HOME/.local/bin/shnex-agent\" %s; "+
			"elif [ -x \"/usr/local/bin/shnex-agent\" ]; then exec \"/usr/local/bin/shnex-agent\" %s; "+
			"else echo 'SHNEX_AGENT_MISSING' >&2; exit 127; fi",
		args, args, args,
	)
	return fmt.Sprintf("sh -c %s", PosixQuote(probeScript))
}
