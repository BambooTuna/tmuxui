package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var version = "dev"

func main() {
	port := flag.Int("port", 6062, "listen port")
	host := flag.String("host", "127.0.0.1", "bind address")
	token := flag.String("token", "", "auth token (auto-generated if empty)")
	dev := flag.Bool("dev", false, "use filesystem instead of embedded assets")
	herdrFlag := flag.String("herdr", "auto", "herdr backend: off, auto, or an explicit socket path")
	flag.Parse()

	if args := flag.Args(); len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Printf("tmuxui %s\n", version)
			return
		case "update":
			if err := runUpdate(); err != nil {
				log.Fatal(err)
			}
			return
		}
	}

	if *token == "" {
		*token = os.Getenv("TMUXUI_TOKEN")
	}
	if *token == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			log.Fatal(err)
		}
		*token = hex.EncodeToString(b)
	}

	globalPreferences = newPreferences()
	hub := newHub()
	backend := newTmuxControlBackend()
	registry := newBackendRegistry("tmux")
	registry.register("tmux", backend)
	backend.OnTopologyChange(hub.broadcastPaneList)

	switch *herdrFlag {
	case "off":
		// herdr連携を無効化
	case "auto":
		if path := defaultHerdrSocketPath(); herdrSocketReachable(path) {
			herdrBackend := newHerdrBackend(path)
			herdrBackend.OnTopologyChange(hub.broadcastPaneList)
			registry.register("herdr", herdrBackend)
			log.Printf("herdr: connected via %s", path)
		}
	default:
		herdrBackend := newHerdrBackend(*herdrFlag)
		herdrBackend.OnTopologyChange(hub.broadcastPaneList)
		registry.register("herdr", herdrBackend)
		if !herdrSocketReachable(*herdrFlag) {
			log.Printf("herdr: socket %s not reachable yet, will retry on demand", *herdrFlag)
		}
	}

	hub.registry = registry
	globalRegistry = registry
	go hub.run()

	addr := fmt.Sprintf("%s:%d", *host, *port)

	srv := &http.Server{
		Addr:    addr,
		Handler: newServer(*token, hub, *dev),
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	fmt.Printf("tmuxui %s\n", version)
	fmt.Printf("Listening on http://%s\n", addr)
	fmt.Printf("Access URL: http://%s?token=%s\n", addr, *token)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
