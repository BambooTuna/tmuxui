package main

import (
	"embed"
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
	mux.HandleFunc("POST /api/sessions", handleCreateSession)
	mux.HandleFunc("DELETE /api/sessions/{name}", handleKillSession)
	mux.HandleFunc("POST /api/sessions/{name}/rename", handleRenameSession)
	mux.HandleFunc("GET /api/panes/{target}/content", handlePaneContent)
	mux.HandleFunc("POST /api/panes/{target}/keys", handlePaneKeys)
	mux.HandleFunc("GET /api/snippets", handleSnippetList)
	mux.HandleFunc("GET /api/snippets/{name}", handleSnippetContent)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWS(hub, w, r)
	})
	fileServer := http.FileServer(http.FS(webRoot))
	if dev {
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			fileServer.ServeHTTP(w, r)
		}))
	} else {
		mux.Handle("/", fileServer)
	}

	return authMiddleware(token, mux)
}

func authMiddleware(validToken string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p != "/" && !strings.HasPrefix(p, "/api/") && p != "/ws" {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Query().Get("token") != validToken {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
