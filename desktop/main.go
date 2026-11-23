package main

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/shnex/shnex/core/api"
	"github.com/shnex/shnex/core/platform"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var embeddedFrontend embed.FS

func main() {
	var appPtr atomic.Pointer[application.App]
	var winPtr atomic.Pointer[application.WebviewWindow]
	var trayPtr atomic.Pointer[application.SystemTray]

	bridge := newPlatformBridge(func(n platform.Notification) {
		if app := appPtr.Load(); app != nil {
			app.Event.Emit("shnex:notification", n)
		}
	})

	coreSrv, err := api.NewServer(api.Config{
		ListenAddr: "127.0.0.1:0",
		Bridge:     bridge,
		AllowedOrigins: []string{
			"http://wails.localhost",
			"https://wails.localhost",
			"wails://wails.localhost",
			"http://localhost:5173",
			"http://127.0.0.1:5173",
		},
	})
	if err != nil {
		log.Fatalf("initialize shnex core failed: %v", err)
	}
	if err := coreSrv.Start(); err != nil {
		log.Fatalf("start shnex core failed: %v", err)
	}

	distFS, err := fs.Sub(embeddedFrontend, "frontend/dist")
	if err != nil {
		log.Fatalf("load embedded frontend/dist failed: %v", err)
	}

	desktopMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/__shnex_desktop/") {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")

			switch r.URL.Path {
			case "/__shnex_desktop/bootstrap":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"api_base": "http://" + coreSrv.Addr(),
					"ws_base":  "ws://" + coreSrv.Addr(),
					"token":    coreSrv.AuthToken(),
					"platform": runtime.GOOS,
				})
			case "/__shnex_desktop/window/minimize":
				if win := winPtr.Load(); win != nil {
					win.Minimise()
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			case "/__shnex_desktop/window/maximize":
				maximised := false
				if win := winPtr.Load(); win != nil {
					win.ToggleMaximise()
					maximised = win.IsMaximised()
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "maximised": maximised})
			case "/__shnex_desktop/window/close":
				if win := winPtr.Load(); win != nil {
					win.Close()
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			case "/__shnex_desktop/window/hide":
				if win := winPtr.Load(); win != nil {
					win.Hide()
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "hidden_to_tray": true})
			case "/__shnex_desktop/window/quit":
				if app := appPtr.Load(); app != nil {
					go app.Quit()
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			case "/__shnex_desktop/tray/style":
				mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
				if tr := trayPtr.Load(); tr != nil {
					tr.SetIcon(renderTrayIconPNG(mode == "mono"))
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "mode": mode})
			case "/__shnex_desktop/network/changed":
				ev := coreSrv.NotifyNetworkChanged()
				_ = json.NewEncoder(w).Encode(ev)
			case "/__shnex_desktop/clipboard/read":
				clip := readSystemClipboard()
				_ = json.NewEncoder(w).Encode(clip)
			case "/__shnex_desktop/dialog/open-file":
				app := appPtr.Load()
				win := winPtr.Load()
				if app == nil {
					w.WriteHeader(http.StatusServiceUnavailable)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "desktop shell unavailable"})
					return
				}
				kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
				multi := r.URL.Query().Get("multiple") == "1"
				opts := application.OpenFileDialogOptions{
					CanChooseFiles:          true,
					CanChooseDirectories:    false,
					AllowsMultipleSelection: multi,
					Window:                  win,
				}
				switch kind {
				case "image":
					opts.Title = "选择图片文件"
					opts.Filters = []application.FileFilter{
						{DisplayName: "图片文件 (*.png;*.jpg;*.jpeg;*.webp;*.gif;*.bmp;*.avif;*.svg)", Pattern: "*.png;*.jpg;*.jpeg;*.webp;*.gif;*.bmp;*.avif;*.svg"},
						{DisplayName: "所有文件 (*.*)", Pattern: "*.*"},
					}
				case "markdown":
					opts.Title = "选择 Markdown 文档"
					opts.Filters = []application.FileFilter{
						{DisplayName: "Markdown 文档 (*.md;*.markdown;*.mdown;*.mdx;*.txt)", Pattern: "*.md;*.markdown;*.mdown;*.mdx;*.txt"},
						{DisplayName: "所有文件 (*.*)", Pattern: "*.*"},
					}
				default:
					opts.Title = "选择要上传的文件"
					opts.Filters = []application.FileFilter{
						{DisplayName: "所有文件 (*.*)", Pattern: "*.*"},
					}
				}
				dlg := app.Dialog.OpenFileWithOptions(&opts)
				if multi {
					files, dErr := dlg.PromptForMultipleSelection()
					if dErr != nil || len(files) == 0 {
						_ = json.NewEncoder(w).Encode(map[string]any{"canceled": true, "paths": []string{}})
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"canceled": false, "paths": files})
				} else {
					file, dErr := dlg.PromptForSingleSelection()
					if dErr != nil || strings.TrimSpace(file) == "" {
						_ = json.NewEncoder(w).Encode(map[string]any{"canceled": true, "paths": []string{}})
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"canceled": false, "paths": []string{file}})
				}
			default:
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
			}
		})
	}

	app := application.New(application.Options{
		Name:        "Shnex",
		Description: "Cross-Platform SSH & Claude CLI Relay Workbench",
		Assets: application.AssetOptions{
			Handler:        application.AssetFileServerFS(distFS),
			Middleware:     desktopMiddleware,
			DisableLogging: true,
		},
		OnShutdown: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			_ = coreSrv.Shutdown(ctx)
		},
	})
	appPtr.Store(app)

	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Shnex",
		Width:            1380,
		Height:           860,
		MinWidth:         960,
		MinHeight:        620,
		Frameless:        true,
		BackgroundColour: application.NewRGB(19, 20, 23),
		URL:              "/",
	})
	winPtr.Store(mainWindow)

	// Restore Win32 keyboard & IME focus back to the WebView2 child surface
	// after frameless window move/resize/restore (Wails v3 temporarily calls
	// SetFocus(w.hwnd) on WM_ENTERSIZEMOVE, which otherwise leaves focus on
	// DefWindowProc and causes Windows IME to pop up at screen (0, 0)).
	restoreWebViewFocus := func(_ *application.WindowEvent) {
		mainWindow.Focus()
	}
	mainWindow.OnWindowEvent(events.Windows.WindowEndMove, restoreWebViewFocus)
	mainWindow.OnWindowEvent(events.Windows.WindowEndResize, restoreWebViewFocus)
	mainWindow.OnWindowEvent(events.Windows.WindowRestore, restoreWebViewFocus)

	// System Tray Integration ("隐藏到系统托盘后，会话和后台服务继续运行")
	sysTray := app.SystemTray.New()
	sysTray.SetTooltip("Shnex - SSH 与 Claude 接力工作台")
	sysTray.SetIcon(renderTrayIconPNG(false))
	trayMenu := app.NewMenu()
	trayMenu.Add("打开主窗口").OnClick(func(_ *application.Context) {
		mainWindow.Show()
		mainWindow.Focus()
	})
	trayMenu.AddSeparator()
	trayMenu.Add("彻底退出 Shnex").OnClick(func(_ *application.Context) {
		app.Quit()
	})
	sysTray.SetMenu(trayMenu)
	sysTray.OnClick(func() {
		mainWindow.Show()
		mainWindow.Focus()
	})
	trayPtr.Store(sysTray)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
