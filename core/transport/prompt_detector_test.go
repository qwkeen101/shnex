package transport

import (
	"strings"
	"testing"
	"time"
)

func TestTailLineTrackerClaudeInkBoxAndApprovalMatch(t *testing.T) {
	tracker := NewTailLineTracker(16)

	// Simulate Ink/Claude Code TUI output using ANSI truecolor, box-drawing runes,
	// and ESC[1C cursor-forward space optimization.
	rawTUI := "\x1b[38;2;215;119;87m╭────────────────────────────────────────────╮\x1b[0m\r\n" +
		"│\x1b[1CBash\x1b[1Ccommand\x1b[31C│\r\n" +
		"│\x1b[3Cgit\x1b[1Cpush\x1b[1Corigin\x1b[1Cmain\x1b[21C│\r\n" +
		"│\x1b[1CDo\x1b[1Cyou\x1b[1Cwant\x1b[1Cto\x1b[1Cproceed?\x1b[20C│\r\n" +
		"│\x1b[1C❯\x1b[1C1.\x1b[1CYes\x1b[35C│\r\n" +
		"│\x1b[3C2.\x1b[1CYes,\x1b[1Cand\x1b[1Cdon't\x1b[1Cask\x1b[1Cagain\x1b[14C│\r\n" +
		"│\x1b[3C3.\x1b[1CNo\x1b[36C│\r\n" +
		"╰────────────────────────────────────────────╯\r\n"

	tracker.Feed([]byte(rawTUI))
	match := DetectAIApprovalPrompt(tracker.Snapshot())
	if !match.Matched {
		t.Fatalf("expected Claude TUI box to match approval prompt, lines=%v", tracker.Snapshot())
	}
	if !strings.Contains(match.Snippet, "git push origin main") || !strings.Contains(match.Snippet, "Do you want to proceed?") {
		t.Fatalf("unexpected snippet: %q", match.Snippet)
	}
}

func TestStreamObserverApprovalSuppressesDuplicateSilence(t *testing.T) {
	evCh := make(chan SessionEvent, 8)
	disp := NewEventDispatcher(func(ev SessionEvent) {
		evCh <- ev
	})

	obs := NewStreamObserver(nil, ObserverConfig{
		SessionID:        "sess-claude",
		ApprovalDebounce: 25 * time.Millisecond,
		SilenceThreshold: 90 * time.Millisecond,
		Dispatcher:       disp,
	})
	defer obs.Close()

	obs.Observe([]byte("Edit file main.go\r\nDo you want to proceed?\r\n1. Yes\r\n2. No\r\n"))

	select {
	case ev := <-evCh:
		if ev.Type != EventAIApprovalRequired {
			t.Fatalf("expected EventAIApprovalRequired first, got %s", ev.Type)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for EventAIApprovalRequired")
	}

	// Wait past SilenceThreshold (90ms): redundant EventOutputSilence must be suppressed!
	select {
	case unexpected := <-evCh:
		t.Fatalf("expected redundant silence event to be suppressed, got %+v", unexpected)
	case <-time.After(140 * time.Millisecond):
	}

	// Simulate user pressing Enter ("1\n") -> subsequent ordinary output should trigger EventOutputSilence.
	obs.NotifyInput()
	obs.Observe([]byte("Applied edit to main.go successfully. All tests pass!\r\n"))

	select {
	case ev := <-evCh:
		if ev.Type != EventOutputSilence {
			t.Fatalf("expected EventOutputSilence for normal completion, got %+v", ev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for EventOutputSilence after normal output")
	}
}
