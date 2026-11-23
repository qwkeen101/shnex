package platform

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// ErrSecretNotFound indicates that no secret exists for the given key in the host keychain.
var ErrSecretNotFound = errors.New("platform: secret not found")

// NotificationCategory classifies notifications emitted by the core engine.
type NotificationCategory string

const (
	CategoryInfo           NotificationCategory = "info"
	CategoryBell           NotificationCategory = "bell"
	CategoryActionRequired NotificationCategory = "action_required"
	CategoryTaskIdle       NotificationCategory = "task_idle"
	CategorySessionAlert   NotificationCategory = "session_alert"
)

// Notification represents a platform-agnostic desktop or mobile notification payload.
type Notification struct {
	Title     string               `json:"title"`
	Body      string               `json:"body"`
	Category  NotificationCategory `json:"category,omitempty"`
	SessionID string               `json:"session_id,omitempty"`
	CreatedAt time.Time            `json:"created_at"`
}

// PlatformBridge decouples the pure Go core engine from OS-specific keychains
// (Windows Credential Manager, Android Keystore) and system notification centers.
type PlatformBridge interface {
	GetSecret(ctx context.Context, key string) (string, error)
	SetSecret(ctx context.Context, key string, value string) error
	Notify(ctx context.Context, notification Notification) error
}

// NotificationHistoryProvider is an optional interface implemented by PlatformBridge
// implementations that retain recent notifications for in-app notification drawers.
type NotificationHistoryProvider interface {
	Notifications() []Notification
}

// MemoryBridge is a concurrency-safe, volatile in-memory PlatformBridge
// intended for devserver and automated tests. Secrets are never persisted to disk.
type MemoryBridge struct {
	mu            sync.RWMutex
	secrets       map[string]string
	notifications []Notification
	onNotify      func(Notification)
}

// NewMemoryBridge constructs an empty volatile MemoryBridge.
func NewMemoryBridge(onNotify func(Notification)) *MemoryBridge {
	return &MemoryBridge{
		secrets:  make(map[string]string),
		onNotify: onNotify,
	}
}

func (m *MemoryBridge) GetSecret(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, ok := m.secrets[key]
	if !ok {
		return "", ErrSecretNotFound
	}
	return val, nil
}

func (m *MemoryBridge) SetSecret(ctx context.Context, key string, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if key == "" {
		return errors.New("platform: secret key must not be empty")
	}
	m.mu.Lock()
	m.secrets[key] = value
	m.mu.Unlock()
	return nil
}

func (m *MemoryBridge) Notify(ctx context.Context, n Notification) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	m.mu.Lock()
	m.notifications = append(m.notifications, n)
	hook := m.onNotify
	m.mu.Unlock()

	if hook != nil {
		hook(n)
	}
	return nil
}

// Notifications returns a snapshot of notifications recorded by MemoryBridge.
func (m *MemoryBridge) Notifications() []Notification {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Notification, len(m.notifications))
	copy(out, m.notifications)
	return out
}

// NetworkChangeEvent describes an OS sleep/wake or interface handoff event.
type NetworkChangeEvent struct {
	Generation uint64    `json:"generation"`
	Reason     string    `json:"reason,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// NetworkChangeListener reacts to host network transitions (e.g., probing SSH keepalives).
type NetworkChangeListener func(ev NetworkChangeEvent)

// NetworkBroadcaster coordinates NotifyNetworkChanged fan-out across active sessions and transports.
type NetworkBroadcaster struct {
	generation atomic.Uint64

	mu        sync.RWMutex
	nextSubID uint64
	listeners map[uint64]NetworkChangeListener
	lastEvent *NetworkChangeEvent
}

// NewNetworkBroadcaster returns a ready-to-use NetworkBroadcaster.
func NewNetworkBroadcaster() *NetworkBroadcaster {
	return &NetworkBroadcaster{
		listeners: make(map[uint64]NetworkChangeListener),
	}
}

// Subscribe registers a listener invoked whenever NotifyNetworkChanged is triggered.
// The returned function removes the subscription and is safe to call multiple times.
func (b *NetworkBroadcaster) Subscribe(fn NetworkChangeListener) func() {
	if fn == nil {
		return func() {}
	}
	b.mu.Lock()
	b.nextSubID++
	id := b.nextSubID
	b.listeners[id] = fn
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.listeners, id)
			b.mu.Unlock()
		})
	}
}

// NotifyNetworkChanged increments the network generation and notifies all registered listeners.
func (b *NetworkBroadcaster) NotifyNetworkChanged(reason string) NetworkChangeEvent {
	gen := b.generation.Add(1)
	ev := NetworkChangeEvent{
		Generation: gen,
		Reason:     reason,
		OccurredAt: time.Now(),
	}

	b.mu.Lock()
	evCopy := ev
	b.lastEvent = &evCopy
	snapshot := make([]NetworkChangeListener, 0, len(b.listeners))
	for _, fn := range b.listeners {
		snapshot = append(snapshot, fn)
	}
	b.mu.Unlock()

	for _, fn := range snapshot {
		fn(ev)
	}
	return ev
}

// Generation returns the current network generation counter.
func (b *NetworkBroadcaster) Generation() uint64 {
	return b.generation.Load()
}

// LastEvent returns a copy of the most recent NetworkChangeEvent, or nil if none occurred yet.
func (b *NetworkBroadcaster) LastEvent() *NetworkChangeEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.lastEvent == nil {
		return nil
	}
	cp := *b.lastEvent
	return &cp
}
