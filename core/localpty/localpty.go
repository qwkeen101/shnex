package localpty

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/shnex/shnex/core/transport"
)

const ProtocolName = "local"

type platformPTY interface {
	io.ReadWriteCloser
	Resize(cols, rows uint16) error
	Wait() error
}

// Transport implements transport.Transport for local interactive shells (Windows ConPTY / Unix PTY).
type Transport struct{}

func NewTransport() *Transport {
	return &Transport{}
}

func (t *Transport) Protocol() string {
	return ProtocolName
}

func (t *Transport) OpenSession(ctx context.Context, cfg transport.SessionConfig) (transport.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	shellKey := strings.ToLower(strings.TrimSpace(cfg.Target))
	if shellKey == "" {
		shellKey = "powershell"
	}
	workDir := strings.TrimSpace(cfg.DefaultDir)
	if workDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			workDir = home
		}
	}
	size := cfg.InitialSize.Normalized()
	ppty, resolvedLabel, err := startPlatformPTY(shellKey, workDir, size, cfg.BuiltinPowerline)
	if err != nil {
		return nil, err
	}

	sessID := strings.TrimSpace(cfg.ID)
	if sessID == "" {
		var b [8]byte
		_, _ = rand.Read(b[:])
		sessID = "local-" + hex.EncodeToString(b[:])
	}

	dispatcher := transport.NewEventDispatcher(cfg.InitialHooks...)
	observer := transport.NewStreamObserver(ppty, transport.ObserverConfig{
		SessionID:         sessID,
		SilenceThreshold:  cfg.SilenceThreshold,
		SilenceMinBytes:   cfg.SilenceMinBytes,
		ApprovalDebounce:  cfg.ApprovalDebounce,
		DisableAIDetector: cfg.DisableAIDetector,
		BellCooldown:      cfg.BellCooldown,
		Dispatcher:        dispatcher,
	})

	s := &Session{
		id:         sessID,
		target:     resolvedLabel,
		pty:        ppty,
		output:     observer,
		dispatcher: dispatcher,
		stopCh:     make(chan struct{}),
		waitDoneCh: make(chan struct{}),
	}

	dispatcher.Emit(transport.SessionEvent{
		SessionID: sessID,
		Type:      transport.EventConnected,
		Message:   "local shell started: " + resolvedLabel,
	})

	go func() {
		wErr := ppty.Wait()
		s.waitErr = wErr
		close(s.waitDoneCh)
		dispatcher.Emit(transport.SessionEvent{
			SessionID: sessID,
			Type:      transport.EventDisconnected,
			Message:   "local shell exited",
		})
		_ = s.Close()
	}()

	return s, nil
}

type Session struct {
	id         string
	target     string
	pty        platformPTY
	output     *transport.StreamObserver
	dispatcher *transport.EventDispatcher

	resizeMu   sync.Mutex
	closeOnce  sync.Once
	stopCh     chan struct{}
	waitDoneCh chan struct{}
	waitErr    error
}

func (s *Session) ID() string       { return s.id }
func (s *Session) Protocol() string { return ProtocolName }
func (s *Session) Target() string   { return s.target }

func (s *Session) Read(p []byte) (int, error) {
	return s.output.Read(p)
}

func (s *Session) Write(p []byte) (int, error) {
	if bytes.IndexAny(p, "\r\n") >= 0 {
		s.output.NotifyInput()
	}
	return s.pty.Write(p)
}

func (s *Session) Resize(ctx context.Context, size transport.TerminalSize) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	norm := size.Normalized()
	s.resizeMu.Lock()
	defer s.resizeMu.Unlock()
	select {
	case <-s.stopCh:
		return net.ErrClosed
	default:
	}
	return s.pty.Resize(norm.Cols, norm.Rows)
}

func (s *Session) Keepalive(_ context.Context) (time.Duration, error) {
	return 100 * time.Microsecond, nil
}

func (s *Session) SubscribeEvents(hook transport.EventHook) func() {
	return s.dispatcher.Subscribe(hook)
}

func (s *Session) Wait() error {
	<-s.waitDoneCh
	return s.waitErr
}

func (s *Session) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.stopCh)
		s.output.Close()
		err = s.pty.Close()
	})
	return err
}
