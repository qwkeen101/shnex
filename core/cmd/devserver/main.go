package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shnex/shnex/core/api"
	"github.com/shnex/shnex/core/platform"
)

//go:embed index.html
var devConsoleHTML []byte

func main() {
	port := flag.Int("port", 0, "loopback port on 127.0.0.1 (0 picks a random free port)")
	flag.Parse()

	if *port < 0 || *port > 65535 {
		log.Fatalf("invalid -port %d (must be 0..65535)", *port)
	}

	listenAddr := fmt.Sprintf("127.0.0.1:%d", *port)

	bridge := platform.NewMemoryBridge(func(n platform.Notification) {
		log.Printf("[PlatformBridge.Notify] [%s] %s — %s (session=%s)", n.Category, n.Title, n.Body, n.SessionID)
	})

	rootHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(devConsoleHTML)
	})

	srv, err := api.NewServer(api.Config{
		ListenAddr:  listenAddr,
		Bridge:      bridge,
		RootHandler: rootHandler,
	})
	if err != nil {
		log.Fatalf("failed to initialize core api server: %v", err)
	}

	if err := srv.Start(); err != nil {
		log.Fatalf("failed to start core api server: %v", err)
	}

	bound := srv.Addr()
	token := srv.AuthToken()
	testURL := fmt.Sprintf("http://%s/#token=%s", bound, token)

	fmt.Println("========================================================================")
	fmt.Println("  Shnex Core DevServer (Milestone 1 — Loopback Bridge Ready)")
	fmt.Println("========================================================================")
	fmt.Printf("  Listen Address : %s (loopback only)\n", bound)
	fmt.Printf("  Auth Token     : %s\n", token)
	fmt.Printf("  Test Terminal  : %s\n", testURL)
	fmt.Println("------------------------------------------------------------------------")
	fmt.Println("  Open the Test Terminal URL in your browser to connect to a real SSH")
	fmt.Println("  server, test Claude CLI, resize PTY, or inspect tsnet / notifications.")
	fmt.Println("  Press Ctrl+C to shut down cleanly.")
	fmt.Println("========================================================================")

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-sigCtx.Done()

	fmt.Println("\nShutting down Shnex Core DevServer...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown warning: %v", err)
	}
}
