package api

import "sync"

// ringBuffer retains the most recent capacity bytes of terminal output in memory
// so reconnecting or newly-attached WebSocket views immediately replay recent context.
type ringBuffer struct {
	mu   sync.RWMutex
	buf  []byte
	cap  int
	head int
	full bool
}

func newRingBuffer(capacity int) *ringBuffer {
	if capacity <= 0 {
		capacity = 64 * 1024
	}
	return &ringBuffer{
		buf: make([]byte, capacity),
		cap: capacity,
	}
}

func (r *ringBuffer) Write(p []byte) {
	if len(p) == 0 {
		return
	}
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

func (r *ringBuffer) Snapshot() []byte {
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
