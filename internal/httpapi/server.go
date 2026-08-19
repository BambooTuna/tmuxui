// Package httpapi wires the HTTP mux (REST API, static assets, auth middleware) and
// implements the REST handlers on top of the backend registry, hub, preferences, and
// update manager passed in via Config.
package httpapi

import (
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/BambooTuna/tmuxui/internal/backend"
	"github.com/BambooTuna/tmuxui/internal/hub"
	"github.com/BambooTuna/tmuxui/internal/prefs"
	"github.com/BambooTuna/tmuxui/internal/selfupdate"
	"github.com/BambooTuna/tmuxui/internal/shellws"
)

// Config は httpapi.New に渡す依存一式。WebFS の //go:embed 宣言は埋め込み元ソースファイルの
// 相対パスに縛られるため、リポジトリルートの embed.go 側で宣言したものをここへ注入する。
type Config struct {
	Token       string
	Dev         bool
	WebFS       embed.FS
	Hub         *hub.Hub
	Registry    *backend.BackendRegistry
	Preferences *prefs.Preferences
	Updates     *selfupdate.UpdateManager
}

// Server はHTTPハンドラの実体。かつてのパッケージ変数(preferences/registry/update manager)を
// すべてこの構造体のフィールドとしてコンストラクタ注入する。
type Server struct {
	hub         *hub.Hub
	registry    *backend.BackendRegistry
	preferences *prefs.Preferences
	updates     *selfupdate.UpdateManager
}

// New はConfigからhttp.Handler(認証ミドルウェア込みのmux)を組み立てる。
func New(cfg Config) http.Handler {
	s := &Server{
		hub:         cfg.Hub,
		registry:    cfg.Registry,
		preferences: cfg.Preferences,
		updates:     cfg.Updates,
	}

	mux := http.NewServeMux()

	var webRoot fs.FS
	if cfg.Dev {
		webRoot = os.DirFS("web")
	} else {
		webRoot, _ = fs.Sub(cfg.WebFS, "web")
	}

	mux.HandleFunc("GET /api/sessions", s.handleSessions)
	mux.HandleFunc("POST /api/sessions", s.withPaneNotify(s.handleCreateSession))
	mux.HandleFunc("DELETE /api/sessions/{name}", s.withPaneNotify(s.handleKillSession))
	mux.HandleFunc("POST /api/sessions/{name}/rename", s.withPaneNotify(s.handleRenameSession))
	mux.HandleFunc("POST /api/sessions/{name}/windows", s.withPaneNotify(s.handleCreateWindow))
	mux.HandleFunc("POST /api/sessions/{name}/worktrees", s.withPaneNotify(s.handleCreateWorktree))
	mux.HandleFunc("DELETE /api/sessions/{name}/windows/{index}", s.withPaneNotify(s.handleKillWindow))
	mux.HandleFunc("POST /api/sessions/{name}/windows/{index}/rename", s.withPaneNotify(s.handleRenameWindow))
	mux.HandleFunc("GET /api/panes/{target}/content", s.handlePaneContent)
	mux.HandleFunc("POST /api/panes/{target}/keys", s.handlePaneKeys)
	mux.HandleFunc("DELETE /api/panes/{target}", s.withPaneNotify(s.handleKillPane))
	mux.HandleFunc("POST /api/panes/{target}/split", s.withPaneNotify(s.handleSplitPane))
	mux.HandleFunc("GET /api/preferences", s.handleGetPreferences)
	mux.HandleFunc("PUT /api/preferences", s.handlePutPreferences)
	mux.HandleFunc("GET /api/update/status", s.handleUpdateStatus)
	mux.HandleFunc("GET /api/update/releases", s.handleUpdateReleases)
	mux.HandleFunc("POST /api/update/check", s.handleUpdateCheck)
	mux.HandleFunc("POST /api/update/apply", s.handleUpdateApply)
	mux.HandleFunc("GET /api/claude/commands", handleClaudeCommands)
	mux.HandleFunc("GET /api/snippets", handleSnippetList)
	mux.HandleFunc("GET /api/snippets/{name}", handleSnippetContent)
	mux.HandleFunc("POST /api/snippets", handleCreateSnippet)
	mux.HandleFunc("PUT /api/snippets/{name}", handleUpdateSnippet)
	mux.HandleFunc("DELETE /api/snippets/{name}", handleDeleteSnippet)
	mux.HandleFunc("GET /api/filer/list", handleFilerList)
	mux.HandleFunc("GET /api/filer/read", handleFilerRead)
	mux.HandleFunc("GET /api/filer/raw", handleFilerRaw)
	mux.HandleFunc("GET /api/filer/download", handleFilerDownload)
	mux.HandleFunc("POST /api/filer/create", handleFilerCreate)
	mux.HandleFunc("POST /api/filer/upload", handleFilerUpload)
	mux.HandleFunc("/ws", s.hub.HandleWS)
	mux.HandleFunc("/ws/shell", shellws.Handler())
	// ブラウザSSHの単一画面(index.htmlとは別エントリ)。static file serverは
	// パスをそのままファイル名に落とすため、拡張子を補うハンドラで明示配線する。
	mux.HandleFunc("GET /terminal", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		f, err := webRoot.Open("terminal.html")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := io.Copy(w, f); err != nil {
			return
		}
	})
	// PWAの起動URLにtokenを埋め込むため動的生成(認証はauthMiddleware側で担保)
	mux.HandleFunc("GET /manifest.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]any{
			"name":             "tmuxui",
			"short_name":       "tmuxui",
			"start_url":        "/?token=" + cfg.Token,
			"display":          "standalone",
			"background_color": "#1a1a2e",
			"theme_color":      "#1a1a2e",
			"icons": []map[string]string{
				{"src": "icon-192.png", "sizes": "192x192", "type": "image/png"},
				{"src": "icon-512.png", "sizes": "512x512", "type": "image/png"},
			},
		})
	})
	fileServer := http.FileServer(http.FS(webRoot))
	if cfg.Dev {
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			fileServer.ServeHTTP(w, r)
		}))
	} else {
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// index.htmlが古いままだと?v=付きJSと食い違って画面が壊れるため、HTMLだけは必ず再検証させる
			if r.URL.Path == "/" || r.URL.Path == "/index.html" {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
		}))
	}

	return authMiddleware(cfg.Token, mux)
}

func (s *Server) withPaneNotify(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next(w, r)
		go s.hub.BroadcastPaneList()
	}
}

const tokenCookieName = "tmuxui_token"

func authMiddleware(validToken string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// manifest.jsonはstart_urlにtokenを埋め込むため認証必須
		// /terminal はブラウザSSHのエントリHTML、/ws/shell はそのWebSocket。両方認証対象
		if p != "/" && p != "/terminal" && !strings.HasPrefix(p, "/api/") && !strings.HasPrefix(p, "/ws") && p != "/manifest.json" {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Query().Get("token") == validToken {
			// クエリ認証成功時にCookieを発行し、以降token無しURL(PWA起動等)でも通す
			http.SetCookie(w, &http.Cookie{
				Name:     tokenCookieName,
				Value:    validToken,
				Path:     "/",
				MaxAge:   365 * 24 * 60 * 60,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
			})
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(tokenCookieName); err == nil && c.Value == validToken {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "Forbidden", http.StatusForbidden)
	})
}
