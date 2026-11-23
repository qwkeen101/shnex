package mobilebridge

import (
	"net/http"
	"strings"
	"testing"
)

type fakeNativeBridge struct {
	secrets map[string]string
	alerts  []string
}

func (f *fakeNativeBridge) GetSecret(key string) (string, error) {
	return f.secrets[key], nil
}

func (f *fakeNativeBridge) SetSecret(key, value string) error {
	if f.secrets == nil {
		f.secrets = make(map[string]string)
	}
	f.secrets[key] = value
	return nil
}

func (f *fakeNativeBridge) Notify(title, body, category, sessionID string) error {
	f.alerts = append(f.alerts, title+":"+category+":"+sessionID)
	return nil
}

func TestMobileEngineLifecycleAndLoopbackGuard(t *testing.T) {
	fb := &fakeNativeBridge{}
	eng, err := StartEngine(t.TempDir(), fb)
	if err != nil {
		t.Fatalf("StartEngine failed: %v", err)
	}
	defer eng.Stop()

	if !strings.HasPrefix(eng.Addr(), "127.0.0.1:") {
		t.Fatalf("expected 127.0.0.1 loopback bind, got %q", eng.Addr())
	}
	if len(eng.AuthToken()) != 64 {
		t.Fatalf("expected 64-hex auth token, got %d chars", len(eng.AuthToken()))
	}

	req, _ := http.NewRequest(http.MethodGet, eng.ApiBaseUrl()+"/api/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+eng.AuthToken())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from mobile core health endpoint, got %d", resp.StatusCode)
	}

	eng.NotifyNetworkChanged("wifi_to_cellular_5g")
}
