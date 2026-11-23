//go:build windows

package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/danieljoos/wincred"
	"github.com/shnex/shnex/core/platform"
)

const wincredTargetPrefix = "Shnex/"

// WindowsBridge implements platform.PlatformBridge backed by Windows Credential Manager (DPAPI).
type WindowsBridge struct {
	mu            sync.RWMutex
	notifications []platform.Notification
	onNotify      func(platform.Notification)
}

func newPlatformBridge(onNotify func(platform.Notification)) platform.PlatformBridge {
	return &WindowsBridge{
		onNotify: onNotify,
	}
}

func (b *WindowsBridge) GetSecret(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", platform.ErrSecretNotFound
	}
	cred, err := wincred.GetGenericCredential(wincredTargetPrefix + key)
	if err != nil {
		if errors.Is(err, wincred.ErrElementNotFound) {
			return "", platform.ErrSecretNotFound
		}
		return "", err
	}
	return string(cred.CredentialBlob), nil
}

func (b *WindowsBridge) SetSecret(ctx context.Context, key string, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("windows bridge: empty secret key")
	}
	cred := wincred.NewGenericCredential(wincredTargetPrefix + key)
	cred.UserName = key
	cred.CredentialBlob = []byte(value)
	cred.Persist = wincred.PersistLocalMachine
	return cred.Write()
}

func (b *WindowsBridge) Notify(ctx context.Context, n platform.Notification) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	b.mu.Lock()
	if len(b.notifications) >= 100 {
		copy(b.notifications, b.notifications[1:])
		b.notifications[len(b.notifications)-1] = n
	} else {
		b.notifications = append(b.notifications, n)
	}
	hook := b.onNotify
	b.mu.Unlock()

	if hook != nil {
		hook(n)
	}
	return nil
}

func (b *WindowsBridge) Notifications() []platform.Notification {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]platform.Notification, len(b.notifications))
	for i := range b.notifications {
		// Return newest first
		out[i] = b.notifications[len(b.notifications)-1-i]
	}
	return out
}
