package platform

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestMemoryBridgeSecretsAndNotifications(t *testing.T) {
	ctx := context.Background()
	var notified atomic.Int32

	mb := NewMemoryBridge(func(n Notification) {
		if n.Title == "Claude Done" {
			notified.Add(1)
		}
	})

	if _, err := mb.GetSecret(ctx, "missing"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("expected ErrSecretNotFound, got %v", err)
	}

	if err := mb.SetSecret(ctx, "prod-ssh-pw", "s3cr3t!"); err != nil {
		t.Fatalf("SetSecret failed: %v", err)
	}
	got, err := mb.GetSecret(ctx, "prod-ssh-pw")
	if err != nil || got != "s3cr3t!" {
		t.Fatalf("GetSecret = (%q, %v), want s3cr3t!", got, err)
	}

	if err := mb.Notify(ctx, Notification{
		Title:    "Claude Done",
		Body:     "Output idle for 5s",
		Category: CategoryTaskIdle,
	}); err != nil {
		t.Fatalf("Notify failed: %v", err)
	}

	if notified.Load() != 1 || len(mb.Notifications()) != 1 {
		t.Fatalf("unexpected notification count: hook=%d slice=%d", notified.Load(), len(mb.Notifications()))
	}
}

func TestNetworkBroadcasterLifecycle(t *testing.T) {
	nb := NewNetworkBroadcaster()
	var count atomic.Int32

	unsub := nb.Subscribe(func(ev NetworkChangeEvent) {
		count.Add(1)
	})

	ev1 := nb.NotifyNetworkChanged("wifi_to_5g")
	if ev1.Generation != 1 || count.Load() != 1 {
		t.Fatalf("after 1st notify: gen=%d count=%d", ev1.Generation, count.Load())
	}

	unsub()
	unsub() // idempotent

	ev2 := nb.NotifyNetworkChanged("resume_sleep")
	if ev2.Generation != 2 || count.Load() != 1 {
		t.Fatalf("after unsub notify: gen=%d count=%d", ev2.Generation, count.Load())
	}
}
