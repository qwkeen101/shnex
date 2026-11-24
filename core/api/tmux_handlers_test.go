package api

import (
	"strings"
	"testing"

	"github.com/shnex/shnex/core/transport"
)

func paneLine(fields ...string) string { return strings.Join(fields, "\t") }

func TestParseTmuxPanesNoTmux(t *testing.T) {
	avail, sessions := parseTmuxPanes("__NO_TMUX__\n")
	if avail || len(sessions) != 0 {
		t.Fatalf("want unavailable/empty, got %v %v", avail, sessions)
	}
}

func TestParseTmuxPanesAggregatesAndSorts(t *testing.T) {
	out := strings.Join([]string{
		// shnex-b: created later, two panes, a busy claude in a non-active window
		paneLine("shnex-b", "200", "1", "1", "1", "-bash", "/srv/b", "api修复"),
		paneLine("shnex-b", "200", "1", "0", "1", "claude", "/srv/b2", "api修复"),
		// shnex-main: created first, idle, no title
		paneLine("shnex-main", "100", "2", "1", "1", "zsh", "/home/u", ""),
		// external session whose name cannot be targeted exactly: skipped
		paneLine("my proj", "50", "0", "1", "1", "bash", "/tmp", ""),
		// title containing a tab must survive (title is the last column)
		paneLine("shnex-c", "300", "0", "1", "1", "vim", "/srv/c", "a\tb"),
		"",
	}, "\n")
	avail, s := parseTmuxPanes(out)
	if !avail {
		t.Fatal("want available")
	}
	if len(s) != 3 {
		t.Fatalf("want 3 sessions, got %d: %+v", len(s), s)
	}
	if s[0].ID != "shnex-main" || s[1].ID != "shnex-b" || s[2].ID != "shnex-c" {
		t.Fatalf("order by created_unix broken: %s %s %s", s[0].ID, s[1].ID, s[2].ID)
	}
	if s[0].Busy || s[0].Title != "shnex-main" || s[0].AttachedClients != 2 || s[0].Command != "zsh" {
		t.Fatalf("shnex-main parsed wrong: %+v", s[0])
	}
	if !s[1].Busy || s[1].Command != "claude" || s[1].Title != "api修复" || s[1].Path != "/srv/b" {
		t.Fatalf("shnex-b parsed wrong: %+v", s[1])
	}
	if !s[2].Busy || s[2].Title != "ab" {
		t.Fatalf("shnex-c parsed wrong: %+v", s[2])
	}
}

func TestValidTmuxID(t *testing.T) {
	for _, ok := range []string{"shnex-main", "a", "A_b-9"} {
		if !validTmuxID(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", " ", "a.b", "my proj", "x;rm", "中文", strings.Repeat("a", 49)} {
		if validTmuxID(bad) {
			t.Errorf("%q should be rejected (never silently mapped to another session)", bad)
		}
	}
}

func TestIsIdleShell(t *testing.T) {
	for _, idle := range []string{"bash", "-bash", "ZSH", "fish", ""} {
		if !isIdleShell(idle) {
			t.Errorf("%q should be idle", idle)
		}
	}
	for _, busy := range []string{"claude", "node", "python3", "vim", "ssh"} {
		if isIdleShell(busy) {
			t.Errorf("%q should be busy", busy)
		}
	}
}

func TestSanitizeTmuxTitle(t *testing.T) {
	if got := sanitizeTmuxTitle("  前端\x1b[31m修复\n "); got != "前端[31m修复" {
		t.Fatalf("control chars not stripped: %q", got)
	}
	if got := sanitizeTmuxTitle(strings.Repeat("长", 50)); len([]rune(got)) != maxTmuxTitleRunes {
		t.Fatalf("title not capped: %d runes", len([]rune(got)))
	}
}

func TestNewTmuxIDIsValidAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id := newTmuxID()
		if !validTmuxID(id) || !strings.HasPrefix(id, "shnex-") {
			t.Fatalf("generated id %q is not a valid tmux name", id)
		}
		seen[id] = true
	}
	if len(seen) < 2 {
		t.Fatal("ids should not all collide")
	}
}

func TestTmuxListCommandUsesRealTabsAndUTF8(t *testing.T) {
	cmd := tmuxListCommand()
	if !strings.Contains(cmd, "#{session_name}\t#{session_created}") {
		t.Fatal("format must be separated by real tab bytes, not a literal backslash-t")
	}
	if strings.Contains(cmd, "\\t") {
		t.Fatal("literal backslash-t found in format")
	}
	if !strings.Contains(cmd, "tmux -u list-panes -a") {
		t.Fatal("list must use -u and cover all panes")
	}
}

func TestTmuxTargetsAreExact(t *testing.T) {
	if tmuxTarget("shnex-1") != "'=shnex-1'" || tmuxOptionTarget("shnex-1") != "'=shnex-1:'" {
		t.Fatalf("targets must use exact '=' match: %s %s", tmuxTarget("shnex-1"), tmuxOptionTarget("shnex-1"))
	}
}

func TestTmuxHistoryCommand(t *testing.T) {
	cmd := tmuxHistoryCommand("shnex-1", 3000)
	for _, want := range []string{"tmux -u capture-pane -p -e -S -3000", "'=shnex-1:'", "tail -c"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("history command %q lacks %q", cmd, want)
		}
	}
	if got := trimHistory("a\nb\n"); got != "a\nb\n" {
		t.Fatalf("short output must be kept whole, got %q", got)
	}
	long := "partial" + "\n" + strings.Repeat("x", tmuxHistoryMaxBytes)
	if got := trimHistory(long); strings.HasPrefix(got, "partial") {
		t.Fatal("cut first line must be dropped")
	}
}

func TestWorkspaceNameFor(t *testing.T) {
	if got := workspaceNameFor(transport.RelayConfig{Mode: transport.RelayAuto}); got != "shnex-main" {
		t.Fatalf("auto with empty name should report the real workspace, got %q", got)
	}
	if got := workspaceNameFor(transport.RelayConfig{Mode: transport.RelayTmux, SessionName: " work "}); got != "work" {
		t.Fatalf("got %q", got)
	}
	if got := workspaceNameFor(transport.RelayConfig{}); got != "" {
		t.Fatalf("no relay should have no workspace, got %q", got)
	}
}
