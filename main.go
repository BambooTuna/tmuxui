package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/BambooTuna/tmuxui/internal/backend"
	"github.com/BambooTuna/tmuxui/internal/backend/herdr"
	"github.com/BambooTuna/tmuxui/internal/backend/tmuxctl"
	"github.com/BambooTuna/tmuxui/internal/config"
	"github.com/BambooTuna/tmuxui/internal/httpapi"
	"github.com/BambooTuna/tmuxui/internal/hub"
	"github.com/BambooTuna/tmuxui/internal/prefs"
	"github.com/BambooTuna/tmuxui/internal/selfupdate"
)

// version はgoreleaserが`-X main.version={{.Version}}`でビルド時に注入する。
// goreleaser/`go install`の制約上、rootのpackage mainに残す必要がある。
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
			if err := selfupdate.RunUpdateCLI(version); err != nil {
				log.Fatal(err)
			}
			return
		}
	}

	cfg, err := config.New(*host, *port, *token, *dev, *herdrFlag)
	if err != nil {
		log.Fatal(err)
	}

	preferences := prefs.New()
	h := hub.New()

	tmuxBackend := tmuxctl.New()
	registry := backend.NewBackendRegistry("tmux")
	registry.Register("tmux", tmuxBackend)
	tmuxBackend.OnTopologyChange(h.BroadcastPaneList)

	switch cfg.HerdrMode {
	case "off":
		// herdr連携を無効化
	case "auto":
		if path := herdr.DefaultSocketPath(); herdr.SocketReachable(path) {
			herdrBackend := herdr.New(path)
			herdrBackend.OnTopologyChange(h.BroadcastPaneList)
			registry.Register("herdr", herdrBackend)
			log.Printf("herdr: connected via %s", path)
		}
	default:
		herdrBackend := herdr.New(cfg.HerdrMode)
		herdrBackend.OnTopologyChange(h.BroadcastPaneList)
		registry.Register("herdr", herdrBackend)
		if !herdr.SocketReachable(cfg.HerdrMode) {
			log.Printf("herdr: socket %s not reachable yet, will retry on demand", cfg.HerdrMode)
		}
	}

	h.SetRegistry(registry)
	go h.Run()

	updateManager := selfupdate.NewManager(version, preferences, h)
	h.SetUpdates(updateManager)
	updateCtx, updateCancel := context.WithCancel(context.Background())
	defer updateCancel()
	go updateManager.Run(updateCtx)

	srv := &http.Server{
		Addr: cfg.Addr(),
		Handler: httpapi.New(httpapi.Config{
			Token:       cfg.Token,
			Dev:         cfg.Dev,
			WebFS:       webFS,
			Hub:         h,
			Registry:    registry,
			Preferences: preferences,
			Updates:     updateManager,
		}),
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		updateCancel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	fmt.Printf("tmuxui %s\n", version)
	fmt.Printf("Listening on http://%s\n", cfg.Addr())
	fmt.Printf("Access URL: http://%s?token=%s\n", cfg.Addr(), cfg.Token)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
