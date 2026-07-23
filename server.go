package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"strings"
)

//go:embed web/*
var webFS embed.FS

func newServer(token string, hub *Hub, dev bool) http.Handler {
	mux := http.NewServeMux()

	var webRoot fs.FS
	if dev {
		webRoot = os.DirFS("web")
	} else {
		webRoot, _ = fs.Sub(webFS, "web")
	}

	mux.HandleFunc("GET /api/sessions", handleSessions)
	mux.HandleFunc("POST /api/sessions", withPaneNotify(hub, handleCreateSession))
	mux.HandleFunc("DELETE /api/sessions/{name}", withPaneNotify(hub, handleKillSession))
	mux.HandleFunc("POST /api/sessions/{name}/rename", withPaneNotify(hub, handleRenameSession))
	mux.HandleFunc("POST /api/sessions/{name}/windows", withPaneNotify(hub, handleCreateWindow))
	mux.HandleFunc("DELETE /api/sessions/{name}/windows/{index}", withPaneNotify(hub, handleKillWindow))
	mux.HandleFunc("POST /api/sessions/{name}/windows/{index}/rename", withPaneNotify(hub, handleRenameWindow))
	mux.HandleFunc("GET /api/panes/{target}/content", handlePaneContent)
	mux.HandleFunc("POST /api/panes/{target}/keys", handlePaneKeys)
	mux.HandleFunc("DELETE /api/panes/{target}", withPaneNotify(hub, handleKillPane))
	mux.HandleFunc("POST /api/panes/{target}/split", withPaneNotify(hub, handleSplitPane))
	mux.HandleFunc("GET /api/preferences", handleGetPreferences)
	mux.HandleFunc("PUT /api/preferences", handlePutPreferences)
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
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWS(hub, w, r)
	})
	// PWAの起動URLにtokenを埋め込むため動的生成(認証はauthMiddleware側で担保)
	mux.HandleFunc("GET /manifest.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]any{
			"name":             "tmuxui",
			"short_name":       "tmuxui",
			"start_url":        "/?token=" + token,
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
	if dev {
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

	return authMiddleware(token, mux)
}

func withPaneNotify(hub *Hub, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next(w, r)
		go hub.broadcastPaneList()
	}
}

const tokenCookieName = "tmuxui_token"

func authMiddleware(validToken string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// manifest.jsonはstart_urlにtokenを埋め込むため認証必須
		if p != "/" && !strings.HasPrefix(p, "/api/") && p != "/ws" && p != "/manifest.json" {
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
