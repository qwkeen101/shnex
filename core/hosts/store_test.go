package hosts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shnex/shnex/core/platform"
)

func TestHostStoreNeverWritesPlaintextSecretToDisk(t *testing.T) {
	ctx := context.Background()
	bridge := platform.NewMemoryBridge(nil)
	jsonPath := filepath.Join(t.TempDir(), "workspace.json")

	st, err := NewStore(jsonPath, bridge)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	saved, err := st.Upsert(ctx, UpsertInput{
		Name:     "新加坡",
		Host:     "43.134.10.20",
		Port:     22,
		User:     "ubuntu",
		Password: "TOP_SECRET_PASSWORD_999",
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if !saved.HasSecret || saved.SecretRef == "" {
		t.Fatalf("expected HasSecret & SecretRef, got %+v", saved)
	}

	rawOnDisk, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(rawOnDisk), "TOP_SECRET_PASSWORD_999") {
		t.Fatal("SECURITY VIOLATION: plaintext password leaked to workspace.json!")
	}

	gotSecret, err := bridge.GetSecret(ctx, saved.SecretRef)
	if err != nil || gotSecret != "TOP_SECRET_PASSWORD_999" {
		t.Fatalf("expected secret in PlatformBridge keychain, got (%q, %v)", gotSecret, err)
	}
}

func TestImportSSHConfigText(t *testing.T) {
	ctx := context.Background()
	bridge := platform.NewMemoryBridge(nil)
	st, err := NewStore(filepath.Join(t.TempDir(), "ws.json"), bridge)
	if err != nil {
		t.Fatal(err)
	}

	sshCfg := `
Host *
  ServerAliveInterval 60

Host aws-dev
  HostName 54.179.10.20
  User ubuntu
  Port 2222

Host sg-box
  HostName 43.134.88.99
  User root
`
	imported, err := st.ImportSSHConfigText(ctx, sshCfg, "")
	if err != nil {
		t.Fatalf("ImportSSHConfigText: %v", err)
	}
	if len(imported) != 2 || imported[0].Port != 2222 {
		t.Fatalf("unexpected imported hosts: %+v", imported)
	}
}

func TestHostStoreAdvancedOptionsRoundTrip(t *testing.T) {
	ctx := context.Background()
	bridge := platform.NewMemoryBridge(nil)
	jsonPath := filepath.Join(t.TempDir(), "workspace.json")

	st, err := NewStore(jsonPath, bridge)
	if err != nil {
		t.Fatal(err)
	}

	in := UpsertInput{
		Name:             "web-01",
		Host:             "example.com",
		Port:             22,
		User:             "ubuntu",
		FileProtocol:     "auto",
		SFTPSudo:         true,
		TmuxSession:      "claude-dev",
		PostLoginScript:  "source ~/.venv/bin/activate\r\nexport LANG=en_US.UTF-8",
		MetricsBar:       "on",
		TerminalEncoding: "gb18030",
		TermType:         "xterm-256color",
		AutoSessionLog:   true,
	}
	saved, err := st.Upsert(ctx, in)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	reloaded, err := NewStore(jsonPath, bridge)
	if err != nil {
		t.Fatalf("reload NewStore: %v", err)
	}
	got, ok := reloaded.Get(saved.ID)
	if !ok {
		t.Fatal("expected saved host after reload")
	}
	if !got.SFTPSudo || got.TmuxSession != "claude-dev" || got.MetricsBar != "on" ||
		got.TerminalEncoding != "gb18030" || got.TermType != "xterm-256color" || !got.AutoSessionLog {
		t.Fatalf("advanced options mismatch after reload: %+v", got)
	}
	if got.PostLoginScript != "source ~/.venv/bin/activate\nexport LANG=en_US.UTF-8" {
		t.Fatalf("unexpected PostLoginScript normalization: %q", got.PostLoginScript)
	}
}
