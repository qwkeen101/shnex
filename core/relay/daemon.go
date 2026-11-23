package relay

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

const agentScrollbackBytes = 128 * 1024

type localProcess interface {
	io.ReadWriteCloser
	Resize(cols, rows uint16) error
	PID() int
	Wait() error
}

// Daemon hosts persistent background PTY workspaces so SSH disconnects and
// desktop <-> mobile device handoffs never interrupt running Claude CLI sessions.
type Daemon struct {
	mu         sync.RWMutex
	workspaces map[string]*daemonWorkspace
	idleExit   time.Duration
	idleTimer  *time.Timer
	ln         net.Listener
}

// NewDaemon constructs a persistent workspace Daemon.
func NewDaemon(idleExit time.Duration) *Daemon {
	return &Daemon{
		workspaces: make(map[string]*daemonWorkspace),
		idleExit:   idleExit,
	}
}

// Serve accepts client connections on ln until ln is closed or idleExit expires with 0 workspaces.
func (d *Daemon) Serve(ln net.Listener) error {
	d.mu.Lock()
	d.ln = ln
	d.resetIdleTimerLocked()
	d.mu.Unlock()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go d.handleConn(conn)
	}
}

// Close terminates all hosted workspaces and closes the listener.
func (d *Daemon) Close() error {
	d.mu.Lock()
	if d.idleTimer != nil {
		d.idleTimer.Stop()
	}
	ln := d.ln
	list := make([]*daemonWorkspace, 0, len(d.workspaces))
	for _, ws := range d.workspaces {
		list = append(list, ws)
	}
	d.workspaces = make(map[string]*daemonWorkspace)
	d.mu.Unlock()

	for _, ws := range list {
		ws.terminate()
	}
	if ln != nil {
		return ln.Close()
	}
	return nil
}

func (d *Daemon) handleConn(conn net.Conn) {
	defer conn.Close()

	if err := ReadMagic(conn); err != nil {
		return
	}
	sw := NewStreamWriter(conn)
	if err := sw.WriteMagic(); err != nil {
		return
	}

	pktType, payload, err := ReadPacket(conn)
	if err != nil {
		return
	}

	switch pktType {
	case PktListReq:
		d.mu.RLock()
		infos := make([]WorkspaceInfo, 0, len(d.workspaces))
		for _, ws := range d.workspaces {
			infos = append(infos, ws.info())
		}
		d.mu.RUnlock()
		_ = sw.WriteJSON(PktListRes, infos)

	case PktKillReq:
		name := SanitizeWorkspaceName(string(payload))
		d.mu.Lock()
		ws, ok := d.workspaces[name]
		if ok {
			delete(d.workspaces, name)
		}
		d.resetIdleTimerLocked()
		d.mu.Unlock()
		if ok {
			ws.terminate()
		}
		_ = sw.WriteJSON(PktExit, ExitPayload{ExitCode: 0, Message: "killed"})

	case PktHello:
		var hello HelloPayload
		if err := json.Unmarshal(payload, &hello); err != nil {
			return
		}
		d.serveAttach(conn, sw, hello)
	}
}

func (d *Daemon) serveAttach(conn net.Conn, sw *StreamWriter, hello HelloPayload) {
	name := SanitizeWorkspaceName(hello.SessionName)
	cols := hello.Cols
	rows := hello.Rows
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}

	ws, created, err := d.getOrCreateWorkspace(name, hello.Command, hello.Term, cols, rows)
	if err != nil {
		_ = sw.WriteJSON(PktExit, ExitPayload{ExitCode: 1, Message: err.Error()})
		return
	}

	if !created {
		_ = ws.resize(cols, rows)
	}

	subID, outCh, replay, peerCount := ws.attach()
	defer ws.detach(subID)

	if err := sw.WriteJSON(PktWelcome, WelcomePayload{
		SessionName:   name,
		PID:           ws.proc.PID(),
		CreatedAt:     ws.createdAt,
		AttachedPeers: peerCount,
		ReplayedBytes: len(replay),
		NewlyCreated:  created,
	}); err != nil {
		return
	}

	if len(replay) > 0 {
		for len(replay) > 0 {
			chunk := replay
			if len(chunk) > 32*1024 {
				chunk = replay[:32*1024]
			}
			replay = replay[len(chunk):]
			if err := sw.WritePacket(PktData, chunk); err != nil {
				return
			}
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for frame := range outCh {
			if err := sw.WritePacket(frame.pktType, frame.payload); err != nil {
				_ = conn.Close()
				return
			}
		}
		_ = conn.Close()
	}()

	for {
		pt, pl, rErr := ReadPacket(conn)
		if rErr != nil {
			break
		}
		switch pt {
		case PktData:
			if len(pl) > 0 {
				if _, wErr := ws.proc.Write(pl); wErr != nil {
					return
				}
			}
		case PktResize:
			if c, r, dErr := DecodeResizePayload(pl); dErr == nil {
				_ = ws.resize(c, r)
			}
		case PktPing:
			_ = sw.WritePacket(PktPong, pl)
		}
	}
	<-done
}

func (d *Daemon) getOrCreateWorkspace(name, command, term string, cols, rows uint16) (*daemonWorkspace, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if existing, ok := d.workspaces[name]; ok && !existing.isClosed() {
		if d.idleTimer != nil {
			d.idleTimer.Stop()
		}
		return existing, false, nil
	}

	proc, err := startLocalProcess(command, term, cols, rows)
	if err != nil {
		return nil, false, err
	}

	ws := &daemonWorkspace{
		name:       name,
		proc:       proc,
		cols:       cols,
		rows:       rows,
		createdAt:  time.Now(),
		scrollback: newSimpleRing(agentScrollbackBytes),
		peers:      make(map[uint64]chan outboundFrame),
	}
	d.workspaces[name] = ws
	if d.idleTimer != nil {
		d.idleTimer.Stop()
	}

	ws.start(func() {
		d.mu.Lock()
		if cur, ok := d.workspaces[name]; ok && cur == ws {
			delete(d.workspaces, name)
		}
		d.resetIdleTimerLocked()
		d.mu.Unlock()
	})

	return ws, true, nil
}

func (d *Daemon) resetIdleTimerLocked() {
	if d.idleExit <= 0 || len(d.workspaces) > 0 {
		if d.idleTimer != nil {
			d.idleTimer.Stop()
		}
		return
	}
	if d.idleTimer != nil {
		d.idleTimer.Stop()
	}
	d.idleTimer = time.AfterFunc(d.idleExit, func() {
		d.mu.RLock()
		empty := len(d.workspaces) == 0
		ln := d.ln
		d.mu.RUnlock()
		if empty && ln != nil {
			_ = ln.Close()
		}
	})
}

type outboundFrame struct {
	pktType byte
	payload []byte
}

type daemonWorkspace struct {
	name       string
	proc       localProcess
	createdAt  time.Time
	scrollback *simpleRing

	mu      sync.RWMutex
	cols    uint16
	rows    uint16
	nextID  uint64
	peers   map[uint64]chan outboundFrame
	closed  bool
	endOnce sync.Once
}

func (w *daemonWorkspace) start(onExit func()) {
	go func() {
		buf := make([]byte, 16*1024)
		for {
			n, err := w.proc.Read(buf)
			if n > 0 {
				chunk := append([]byte(nil), buf[:n]...)
				w.scrollback.Write(chunk)
				w.broadcast(outboundFrame{pktType: PktData, payload: chunk})
			}
			if err != nil {
				break
			}
		}
		waitErr := w.proc.Wait()
		exitMsg := "workspace process exited"
		if waitErr != nil {
			exitMsg = waitErr.Error()
		}
		rawExit, _ := json.Marshal(ExitPayload{ExitCode: 0, Message: exitMsg})
		w.broadcast(outboundFrame{pktType: PktExit, payload: rawExit})
		w.terminate()
		if onExit != nil {
			onExit()
		}
	}()
}

func (w *daemonWorkspace) attach() (uint64, <-chan outboundFrame, []byte, int) {
	ch := make(chan outboundFrame, 256)
	w.mu.Lock()
	w.nextID++
	id := w.nextID
	w.peers[id] = ch
	count := len(w.peers)
	replay := w.scrollback.Snapshot()
	w.mu.Unlock()
	return id, ch, replay, count
}

func (w *daemonWorkspace) detach(id uint64) {
	w.mu.Lock()
	if ch, ok := w.peers[id]; ok {
		delete(w.peers, id)
		close(ch)
	}
	w.mu.Unlock()
}

func (w *daemonWorkspace) broadcast(f outboundFrame) {
	w.mu.RLock()
	for _, ch := range w.peers {
		select {
		case ch <- f:
		default:
		}
	}
	w.mu.RUnlock()
}

func (w *daemonWorkspace) resize(cols, rows uint16) error {
	w.mu.Lock()
	w.cols = cols
	w.rows = rows
	w.mu.Unlock()
	return w.proc.Resize(cols, rows)
}

func (w *daemonWorkspace) info() WorkspaceInfo {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return WorkspaceInfo{
		SessionName:   w.name,
		PID:           w.proc.PID(),
		Cols:          w.cols,
		Rows:          w.rows,
		AttachedPeers: len(w.peers),
		CreatedAt:     w.createdAt,
	}
}

func (w *daemonWorkspace) isClosed() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.closed
}

func (w *daemonWorkspace) terminate() {
	w.endOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		for id, ch := range w.peers {
			delete(w.peers, id)
			close(ch)
		}
		w.mu.Unlock()
		_ = w.proc.Close()
	})
}

type simpleRing struct {
	mu   sync.RWMutex
	buf  []byte
	cap  int
	head int
	full bool
}

func newSimpleRing(capacity int) *simpleRing {
	return &simpleRing{buf: make([]byte, capacity), cap: capacity}
}

func (r *simpleRing) Write(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(p) >= r.cap {
		copy(r.buf, p[len(p)-r.cap:])
		r.head = 0
		r.full = true
		return
	}
	first := r.cap - r.head
	if len(p) <= first {
		copy(r.buf[r.head:], p)
		r.head += len(p)
		if r.head == r.cap {
			r.head = 0
			r.full = true
		}
		return
	}
	copy(r.buf[r.head:], p[:first])
	rest := len(p) - first
	copy(r.buf[0:], p[first:])
	r.head = rest
	r.full = true
}

func (r *simpleRing) Snapshot() []byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.full {
		if r.head == 0 {
			return nil
		}
		out := make([]byte, r.head)
		copy(out, r.buf[:r.head])
		return out
	}
	out := make([]byte, r.cap)
	first := r.cap - r.head
	copy(out, r.buf[r.head:])
	copy(out[first:], r.buf[:r.head])
	return out
}
