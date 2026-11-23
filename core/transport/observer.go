package transport

import (
	"io"
	"sync"
	"time"
)

type ansiScanState uint8

const (
	ansiGround ansiScanState = iota
	ansiEsc
	ansiOSC
	ansiOSCEsc
)

const (
	maxOSCLength            = 4096
	defaultApprovalDebounce = 450 * time.Millisecond
)

// ObserverConfig configures BEL, output-silence, and AI approval prompt detection on a PTY stream.
type ObserverConfig struct {
	SessionID         string
	SilenceThreshold  time.Duration
	SilenceMinBytes   uint64
	ApprovalDebounce  time.Duration
	DisableAIDetector bool
	BellCooldown      time.Duration
	Dispatcher        *EventDispatcher
}

// StreamObserver wraps an output io.Reader and detects:
//  1. Standalone terminal BEL (0x07) while ignoring 0x07 used as ANSI OSC String Terminator.
//  2. AI CLI approval gates (e.g. Claude Code "Do you want to proceed?" / "Allow once / Deny")
//     using ANSI-stripped rolling tail lines, firing EventAIApprovalRequired quickly.
//  3. Output silence transitions after an active output burst stops for SilenceThreshold
//     (suppressed if EventAIApprovalRequired already fired for the current burst).
type StreamObserver struct {
	r   io.Reader
	cfg ObserverConfig

	mu                  sync.Mutex
	state               ansiScanState
	oscBytes            int
	lastBellAt          time.Time
	burstBytes          uint64
	seq                 uint64
	silenceTmr          *time.Timer
	approvalTmr         *time.Timer
	tailTracker         *TailLineTracker
	lastApprovalSnippet string
	approvalFiredBurst  bool
	closed              bool
}

// NewStreamObserver wraps r with ANSI-aware BEL, AI approval prompt, and silence detection.
func NewStreamObserver(r io.Reader, cfg ObserverConfig) *StreamObserver {
	if cfg.SilenceMinBytes == 0 {
		cfg.SilenceMinBytes = 1
	}
	if !cfg.DisableAIDetector && cfg.ApprovalDebounce <= 0 {
		cfg.ApprovalDebounce = defaultApprovalDebounce
	}
	var tracker *TailLineTracker
	if !cfg.DisableAIDetector {
		tracker = NewTailLineTracker(defaultTailMaxLines)
	}
	return &StreamObserver{
		r:           r,
		cfg:         cfg,
		tailTracker: tracker,
	}
}

func (o *StreamObserver) Read(p []byte) (int, error) {
	n, err := o.r.Read(p)
	if n > 0 {
		o.Observe(p[:n])
	}
	if err != nil {
		o.Close()
	}
	return n, err
}

// NotifyInput informs the observer that user input was written to stdin, resetting
// duplicate-approval suppression so the next prompt can alert cleanly.
func (o *StreamObserver) NotifyInput() {
	o.mu.Lock()
	o.approvalFiredBurst = false
	o.lastApprovalSnippet = ""
	if o.tailTracker != nil {
		o.tailTracker.Clear()
	}
	o.mu.Unlock()
}

// Observe inspects a raw PTY output chunk and updates BEL, AI approval, & silence state machines.
func (o *StreamObserver) Observe(chunk []byte) {
	if len(chunk) == 0 {
		return
	}

	var fireBell bool
	now := time.Now()

	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return
	}

	for _, b := range chunk {
		switch o.state {
		case ansiGround:
			switch b {
			case 0x1b: // ESC
				o.state = ansiEsc
			case 0x07: // Standalone BEL
				if o.shouldFireBellLocked(now) {
					fireBell = true
				}
			}
		case ansiEsc:
			switch b {
			case ']': // ESC ] starts OSC sequence
				o.state = ansiOSC
				o.oscBytes = 0
			case 0x07:
				o.state = ansiGround
				if o.shouldFireBellLocked(now) {
					fireBell = true
				}
			default:
				o.state = ansiGround
			}
		case ansiOSC:
			o.oscBytes++
			switch b {
			case 0x07: // OSC String Terminator (ST) — NOT an audible bell
				o.state = ansiGround
				o.oscBytes = 0
			case 0x1b:
				o.state = ansiOSCEsc
			case 0x18, 0x1a: // CAN / SUB abort sequence
				o.state = ansiGround
				o.oscBytes = 0
			default:
				if o.oscBytes > maxOSCLength {
					o.state = ansiGround
					o.oscBytes = 0
				}
			}
		case ansiOSCEsc:
			o.state = ansiGround
			o.oscBytes = 0
		}
	}

	if o.tailTracker != nil {
		o.tailTracker.Feed(chunk)
	}

	o.burstBytes += uint64(len(chunk))
	o.seq++
	expectedSeq := o.seq

	if o.cfg.Dispatcher != nil {
		if o.tailTracker != nil && o.cfg.ApprovalDebounce > 0 {
			if o.approvalTmr != nil {
				o.approvalTmr.Stop()
			}
			debounce := o.cfg.ApprovalDebounce
			o.approvalTmr = time.AfterFunc(debounce, func() {
				o.onApprovalDebounce(expectedSeq)
			})
		}

		if o.cfg.SilenceThreshold > 0 {
			if o.silenceTmr != nil {
				o.silenceTmr.Stop()
			}
			threshold := o.cfg.SilenceThreshold
			o.silenceTmr = time.AfterFunc(threshold, func() {
				o.onSilenceTimer(expectedSeq, threshold)
			})
		}
	}
	o.mu.Unlock()

	if fireBell && o.cfg.Dispatcher != nil {
		o.cfg.Dispatcher.Emit(SessionEvent{
			SessionID: o.cfg.SessionID,
			Type:      EventBell,
			Timestamp: now,
			Message:   "terminal bell (BEL)",
		})
	}
}

func (o *StreamObserver) shouldFireBellLocked(now time.Time) bool {
	if o.cfg.BellCooldown > 0 && !o.lastBellAt.IsZero() && now.Sub(o.lastBellAt) < o.cfg.BellCooldown {
		return false
	}
	o.lastBellAt = now
	return true
}

func (o *StreamObserver) onApprovalDebounce(expectedSeq uint64) {
	o.mu.Lock()
	if o.closed || o.seq != expectedSeq || o.tailTracker == nil {
		o.mu.Unlock()
		return
	}
	match := DetectAIApprovalPrompt(o.tailTracker.Snapshot())
	if !match.Matched || match.Snippet == o.lastApprovalSnippet {
		o.mu.Unlock()
		return
	}
	o.lastApprovalSnippet = match.Snippet
	o.approvalFiredBurst = true
	bytes := o.burstBytes
	o.mu.Unlock()

	if o.cfg.Dispatcher != nil {
		o.cfg.Dispatcher.Emit(SessionEvent{
			SessionID:     o.cfg.SessionID,
			Type:          EventAIApprovalRequired,
			Timestamp:     time.Now(),
			BurstBytes:    bytes,
			PromptSnippet: match.Snippet,
			Message:       "AI CLI is waiting for user approval",
		})
	}
}

func (o *StreamObserver) onSilenceTimer(expectedSeq uint64, quietFor time.Duration) {
	o.mu.Lock()
	if o.closed || o.seq != expectedSeq || o.burstBytes < o.cfg.SilenceMinBytes {
		o.mu.Unlock()
		return
	}
	alreadyAlertedApproval := o.approvalFiredBurst
	bytes := o.burstBytes
	o.burstBytes = 0
	o.approvalFiredBurst = false
	o.mu.Unlock()

	if alreadyAlertedApproval {
		return
	}

	if o.cfg.Dispatcher != nil {
		o.cfg.Dispatcher.Emit(SessionEvent{
			SessionID:  o.cfg.SessionID,
			Type:       EventOutputSilence,
			Timestamp:  time.Now(),
			QuietFor:   quietFor,
			BurstBytes: bytes,
			Message:    "terminal output became idle after active burst",
		})
	}
}

// Close stops any pending timers and prevents further event emissions.
func (o *StreamObserver) Close() {
	o.mu.Lock()
	o.closed = true
	o.seq++
	if o.approvalTmr != nil {
		o.approvalTmr.Stop()
		o.approvalTmr = nil
	}
	if o.silenceTmr != nil {
		o.silenceTmr.Stop()
		o.silenceTmr = nil
	}
	o.mu.Unlock()
}
