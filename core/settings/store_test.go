package settings

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/shnex/shnex/core/platform"
)

func TestSettingsVaultLockAndTerminalDefaults(t *testing.T) {
	ctx := context.Background()
	bridge := platform.NewMemoryBridge(nil)
	cfgPath := filepath.Join(t.TempDir(), "settings.json")

	st, err := NewStore(cfgPath, bridge)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	got := st.Get()
	if got.Terminal.KeepaliveSec != 30 || got.Terminal.OSC52Mode != "ask" || got.Terminal.OSC52MaxKiB != 1024 {
		t.Fatalf("unexpected terminal defaults: %+v", got.Terminal)
	}
	if got.Security.VaultLocked {
		t.Fatal("expected vault unlocked by default when master password is not enabled")
	}

	afterSet, err := st.SetMasterPassword(ctx, "MyMasterKey#2026")
	if err != nil {
		t.Fatalf("SetMasterPassword: %v", err)
	}
	if !afterSet.Security.MasterLockEnabled {
		t.Fatal("expected MasterLockEnabled=true")
	}

	locked := st.LockVault()
	if !locked.Security.VaultLocked {
		t.Fatal("expected VaultLocked=true after LockVault")
	}

	if _, err := st.UnlockVault(ctx, "wrong-password"); err == nil {
		t.Fatal("expected UnlockVault to reject wrong password")
	}

	unlocked, err := st.UnlockVault(ctx, "MyMasterKey#2026")
	if err != nil || unlocked.Security.VaultLocked {
		t.Fatalf("expected UnlockVault success, got (%+v, %v)", unlocked.Security, err)
	}
}
