package transport

import (
	"sync"
	"testing"
	"time"
)

func TestStreamObserverBellAndOSCFiltering(t *testing.T) {
	var (
		mu     sync.Mutex
		events []SessionEvent
	)
	disp := NewEventDispatcher(func(ev SessionEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})

	obs := NewStreamObserver(nil, ObserverConfig{
		SessionID:  "s1",
		Dispatcher: disp,
	})
	defer obs.Close()

	// 1. Split ANSI OSC window title ending in 0x07 MUST NOT emit EventBell.
	obs.Observe([]byte("\x1b]0;user@gpu-box:~/claude"))
	obs.Observe([]byte("-workspace\x07normal output 思考完成\r\n"))

	mu.Lock()
	if len(events) != 0 {
		t.Fatalf("expected 0 false bells from OSC title, got %d", len(events))
	}
	mu.Unlock()

	// 2. Standalone 0x07 MUST emit EventBell.
	obs.Observe([]byte("Task awaiting confirmation\x07"))

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 || events[0].Type != EventBell {
		t.Fatalf("expected 1 EventBell, got %+v", events)
	}
}

func TestStreamObserverOutputSilenceBurst(t *testing.T) {
	ch := make(chan SessionEvent, 4)
	disp := NewEventDispatcher(func(ev SessionEvent) {
		if ev.Type == EventOutputSilence {
			ch <- ev
		}
	})

	obs := NewStreamObserver(nil, ObserverConfig{
		SessionID:        "s-idle",
		SilenceThreshold: 45 * time.Millisecond,
		SilenceMinBytes:  5,
		Dispatcher:       disp,
	})
	defer obs.Close()

	obs.Observe([]byte("streaming token 1... "))
	time.Sleep(15 * time.Millisecond)
	obs.Observe([]byte("streaming token 2... done."))

	select {
	case ev := <-ch:
		if ev.BurstBytes == 0 {
			t.Fatalf("expected non-zero BurstBytes, got %+v", ev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for EventOutputSilence")
	}

	// Ensure it does NOT fire repeatedly while remaining idle.
	select {
	case unexpected := <-ch:
		t.Fatalf("unexpected duplicate silence event while idle: %+v", unexpected)
	case <-time.After(90 * time.Millisecond):
	}
}
