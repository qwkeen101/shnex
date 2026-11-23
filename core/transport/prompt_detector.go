package transport

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	defaultTailMaxLines = 16
	defaultMaxLineRunes = 240
)

type termStripState uint8

const (
	stripGround termStripState = iota
	stripEsc
	stripCSI
	stripOSC
	stripOSCEsc
)

// TailLineTracker incrementally strips ANSI CSI/OSC escape sequences, handles
// carriage-return line overwrites (\r), backspaces, and Ink-style cursor-forward
// spacing (ESC[<n>C), maintaining a clean rolling window of recent visible lines.
type TailLineTracker struct {
	maxLines int
	lines    []string
	curLine  []rune
	col      int

	state     termStripState
	csiParams strings.Builder
	oscLen    int
	utf8Buf   [4]byte
	utf8Len   int
}

// NewTailLineTracker creates a rolling terminal tail-line extractor.
func NewTailLineTracker(maxLines int) *TailLineTracker {
	if maxLines <= 0 {
		maxLines = defaultTailMaxLines
	}
	return &TailLineTracker{
		maxLines: maxLines,
		lines:    make([]string, 0, maxLines),
	}
}

// Feed ingests raw PTY output bytes into the virtual tail-line buffer.
func (t *TailLineTracker) Feed(chunk []byte) {
	for _, b := range chunk {
		switch t.state {
		case stripGround:
			if t.utf8Len > 0 {
				t.utf8Buf[t.utf8Len] = b
				t.utf8Len++
				if utf8.FullRune(t.utf8Buf[:t.utf8Len]) {
					r, _ := utf8.DecodeRune(t.utf8Buf[:t.utf8Len])
					t.utf8Len = 0
					t.putRune(r)
				} else if t.utf8Len >= 4 {
					t.utf8Len = 0
				}
				continue
			}

			switch b {
			case 0x1b:
				t.state = stripEsc
			case '\n':
				t.commitLine()
			case '\r':
				t.col = 0
			case '\b', 0x7f:
				if t.col > 0 {
					t.col--
				}
			case '\t':
				t.putRune(' ')
			default:
				if b >= 0x20 && b < 0x7f {
					t.putRune(rune(b))
				} else if b >= 0x80 {
					t.utf8Buf[0] = b
					t.utf8Len = 1
				}
			}

		case stripEsc:
			switch b {
			case '[':
				t.state = stripCSI
				t.csiParams.Reset()
			case ']':
				t.state = stripOSC
				t.oscLen = 0
			default:
				t.state = stripGround
			}

		case stripCSI:
			if b >= 0x40 && b <= 0x7e {
				t.handleCSIFinal(b, t.csiParams.String())
				t.state = stripGround
			} else if t.csiParams.Len() < 64 {
				t.csiParams.WriteByte(b)
			} else {
				t.state = stripGround
			}

		case stripOSC:
			t.oscLen++
			if b == 0x07 || b == 0x18 || b == 0x1a || t.oscLen > maxOSCLength {
				t.state = stripGround
			} else if b == 0x1b {
				t.state = stripOSCEsc
			}

		case stripOSCEsc:
			t.state = stripGround
		}
	}
}

func (t *TailLineTracker) handleCSIFinal(final byte, params string) {
	switch final {
	case 'C': // CUF: Cursor Forward <n> — Ink/Claude Code uses ESC[1C to emit spaces!
		n := 1
		if params != "" {
			if parsed, err := strconv.Atoi(strings.TrimLeft(params, "?><=")); err == nil && parsed > 0 {
				n = parsed
			}
		}
		if n > 8 {
			n = 8
		}
		for i := 0; i < n; i++ {
			t.putRune(' ')
		}
	case 'K': // EL: Erase in Line
		switch params {
		case "", "0":
			if t.col < len(t.curLine) {
				t.curLine = t.curLine[:t.col]
			}
		case "2":
			t.curLine = t.curLine[:0]
			t.col = 0
		}
	case 'A', 'B', 'E', 'F', 'H', 'f':
		// Vertical cursor motions in TUI boxes separate logical rows cleanly.
		if len(t.curLine) > 0 {
			t.commitLine()
		}
	}
}

func (t *TailLineTracker) putRune(r rune) {
	if isBoxBorderRune(r) {
		r = ' '
	}
	if !unicode.IsPrint(r) {
		return
	}
	if t.col >= defaultMaxLineRunes {
		return
	}
	if t.col < len(t.curLine) {
		t.curLine[t.col] = r
	} else {
		t.curLine = append(t.curLine, r)
	}
	t.col++
}

func (t *TailLineTracker) commitLine() {
	norm := normalizeVisibleLine(string(t.curLine))
	t.curLine = t.curLine[:0]
	t.col = 0
	if norm == "" {
		return
	}
	if len(t.lines) >= t.maxLines {
		copy(t.lines, t.lines[1:])
		t.lines[len(t.lines)-1] = norm
	} else {
		t.lines = append(t.lines, norm)
	}
}

// Snapshot returns up to maxLines recent normalized non-empty lines, including any uncommitted active line.
func (t *TailLineTracker) Snapshot() []string {
	out := make([]string, 0, len(t.lines)+1)
	out = append(out, t.lines...)
	if cur := normalizeVisibleLine(string(t.curLine)); cur != "" {
		out = append(out, cur)
	}
	if len(out) > t.maxLines {
		out = out[len(out)-t.maxLines:]
	}
	return out
}

// Clear resets the tracked tail lines (e.g. after user submits an answer).
func (t *TailLineTracker) Clear() {
	t.lines = t.lines[:0]
	t.curLine = t.curLine[:0]
	t.col = 0
}

func isBoxBorderRune(r rune) bool {
	// Unicode Box Drawing block U+2500..U+257F plus common TUI bullets/cursors
	if r >= 0x2500 && r <= 0x257f {
		return true
	}
	switch r {
	case '❯', '›', '●', '◯', '▪':
		return true
	}
	return false
}

func normalizeVisibleLine(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
}

var ynSuffixPattern = regexp.MustCompile(`(?i)(\[y/n\]|\(y/n\)|\[yes/no\]|\(yes/no\))\s*[:?]?\s*$`)

// ApprovalMatch holds details when the terminal tail lines match an AI CLI approval gate.
type ApprovalMatch struct {
	Matched bool
	Snippet string
}

// DetectAIApprovalPrompt inspects recent normalized visible terminal lines for
// Claude Code, Codex, Gemini CLI, or standard interactive tool confirmation prompts.
func DetectAIApprovalPrompt(lines []string) ApprovalMatch {
	if len(lines) == 0 {
		return ApprovalMatch{}
	}
	// Inspect at most the last 10 visible non-empty lines.
	if len(lines) > 10 {
		lines = lines[len(lines)-10:]
	}

	joinedLower := strings.ToLower(strings.Join(lines, "\n"))

	// 1. Claude Code TUI confirmation dialogs:
	//    "Do you want to proceed?" / "Do you want to make this edit to ..."
	//    paired with numbered choices ("1. Yes", "2. Yes, and don't ask again", "Allow once", etc.)
	claudeQuestions := []string{
		"do you want to proceed?",
		"do you want to make this edit",
		"do you want to create ",
		"do you want to run ",
		"would you like to proceed",
		"allow claude to ",
	}
	for _, q := range claudeQuestions {
		if strings.Contains(joinedLower, q) {
			return ApprovalMatch{
				Matched: true,
				Snippet: buildSnippet(lines, q),
			}
		}
	}

	// 2. Explicit Allow/Deny or Yes/Allow menu options in AI coding CLIs.
	hasAllowChoice := strings.Contains(joinedLower, "allow once") ||
		strings.Contains(joinedLower, "allow always") ||
		strings.Contains(joinedLower, "yes, allow") ||
		strings.Contains(joinedLower, "don't ask again")
	hasRejectChoice := strings.Contains(joinedLower, "deny") ||
		strings.Contains(joinedLower, "no, and ") ||
		strings.Contains(joinedLower, "2. no") ||
		strings.Contains(joinedLower, "3. no")
	if hasAllowChoice && hasRejectChoice {
		return ApprovalMatch{
			Matched: true,
			Snippet: summarizeTail(lines, 3),
		}
	}

	// 3. Classic CLI [y/N] or (y/n) confirmation on the very last 2 visible lines.
	tailCheck := lines
	if len(tailCheck) > 2 {
		tailCheck = tailCheck[len(tailCheck)-2:]
	}
	for _, l := range tailCheck {
		if ynSuffixPattern.MatchString(l) {
			return ApprovalMatch{
				Matched: true,
				Snippet: truncateRunes(l, 160),
			}
		}
	}

	return ApprovalMatch{}
}

func buildSnippet(lines []string, needleLower string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(strings.ToLower(lines[i]), needleLower) {
			if i > 0 {
				return truncateRunes(lines[i-1]+" — "+lines[i], 160)
			}
			return truncateRunes(lines[i], 160)
		}
	}
	return summarizeTail(lines, 2)
}

func summarizeTail(lines []string, count int) string {
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return truncateRunes(strings.Join(lines, " | "), 160)
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
