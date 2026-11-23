package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageUploadWallpaperTerminalAndMarkdownAsset(t *testing.T) {
	wsDir := t.TempDir()
	srv, err := NewServer(Config{
		ListenAddr:    "127.0.0.1:0",
		WorkspacePath: filepath.Join(wsDir, "hosts.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown(t.Context())

	baseURL := "http://" + srv.Addr()
	tok := srv.AuthToken()

	// 1. Test raw binary PNG wallpaper upload + GET MIME verification
	pngBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00, 0x00, 0x00, 0x0d}
	req1, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/settings/wallpaper?name=bg.png", bytes.NewReader(pngBytes))
	req1.Header.Set("Authorization", "Bearer "+tok)
	req1.Header.Set("Content-Type", "image/png")
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on binary wallpaper upload, got %d", resp1.StatusCode)
	}

	getWp, err := http.Get(baseURL + "/api/v1/settings/wallpaper?token=" + url.QueryEscape(tok))
	if err != nil {
		t.Fatal(err)
	}
	gotBody, _ := io.ReadAll(getWp.Body)
	getWp.Body.Close()
	if getWp.StatusCode != http.StatusOK || getWp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(gotBody, pngBytes) {
		t.Fatalf("unexpected wallpaper GET: status=%d ct=%q len=%d", getWp.StatusCode, getWp.Header.Get("Content-Type"), len(gotBody))
	}

	// 2. Test local_path SVG wallpaper upload & SVG Content-Type preservation
	svgFile := filepath.Join(wsDir, "hero.svg")
	svgRaw := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"></svg>`)
	if err := os.WriteFile(svgFile, svgRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	payload2, _ := json.Marshal(map[string]string{"local_path": svgFile})
	req2, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/settings/wallpaper", bytes.NewReader(payload2))
	req2.Header.Set("Authorization", "Bearer "+tok)
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on local_path wallpaper upload, got %d", resp2.StatusCode)
	}
	getSvg, err := http.Get(baseURL + "/api/v1/settings/wallpaper?token=" + url.QueryEscape(tok))
	if err != nil {
		t.Fatal(err)
	}
	getSvg.Body.Close()
	if getSvg.Header.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("expected image/svg+xml for SVG wallpaper, got %q", getSvg.Header.Get("Content-Type"))
	}

	// 3. Test Markdown relative local image asset endpoint
	docDir := filepath.Join(wsDir, "docs")
	_ = os.MkdirAll(docDir, 0o755)
	mdPath := filepath.Join(docDir, "readme.md")
	_ = os.WriteFile(mdPath, []byte("![pic](./diagram.png)"), 0o644)
	_ = os.WriteFile(filepath.Join(docDir, "diagram.png"), pngBytes, 0o644)

	assetURL := baseURL + "/api/v1/markdown/asset?token=" + url.QueryEscape(tok) + "&local_base=" + url.QueryEscape(mdPath) + "&src=" + url.QueryEscape("./diagram.png")
	assetResp, err := http.Get(assetURL)
	if err != nil {
		t.Fatal(err)
	}
	assetBody, _ := io.ReadAll(assetResp.Body)
	assetResp.Body.Close()
	if assetResp.StatusCode != http.StatusOK || !bytes.Equal(assetBody, pngBytes) {
		t.Fatalf("markdown relative asset failed: status=%d len=%d", assetResp.StatusCode, len(assetBody))
	}

	// 4. Test Terminal clipboard screenshot upload on a local PTY session
	sessReq, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/sessions", strings.NewReader(`{"protocol":"local","target":"cmd"}`))
	sessReq.Header.Set("Authorization", "Bearer "+tok)
	sessReq.Header.Set("Content-Type", "application/json")
	sessResp, err := http.DefaultClient.Do(sessReq)
	if err != nil {
		t.Fatal(err)
	}
	var created SessionSummary
	_ = json.NewDecoder(sessResp.Body).Decode(&created)
	sessResp.Body.Close()
	if created.ID == "" {
		t.Fatalf("expected created local session")
	}

	upReq, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/sessions/"+created.ID+"/upload-image?name=paste.png", bytes.NewReader(pngBytes))
	upReq.Header.Set("Authorization", "Bearer "+tok)
	upReq.Header.Set("Content-Type", "image/png")
	upResp, err := http.DefaultClient.Do(upReq)
	if err != nil {
		t.Fatal(err)
	}
	var upOut struct {
		Path     string `json:"path"`
		ShellArg string `json:"shell_arg"`
	}
	_ = json.NewDecoder(upResp.Body).Decode(&upOut)
	upResp.Body.Close()
	if upResp.StatusCode != http.StatusOK || upOut.Path == "" || upOut.ShellArg == "" {
		t.Fatalf("session upload-image failed: status=%d out=%+v", upResp.StatusCode, upOut)
	}
	if _, statErr := os.Stat(upOut.Path); statErr != nil {
		t.Fatalf("uploaded image missing on disk at %s: %v", upOut.Path, statErr)
	}
}
