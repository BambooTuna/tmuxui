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
	mux.HandleFunc("GET /api/claude/commands", handleClaudeCommands)
	mux.HandleFunc("GET /api/snippets", handleSnippetList)
	mux.HandleFunc("GET /api/snippets/{name}", handleSnippetContent)
	mux.HandleFunc("POST /api/snippets", handleCreateSnippet)
	mux.HandleFunc("PUT /api/snippets/{name}", handleUpdateSnippet)
	mux.HandleFunc("DELETE /api/snippets/{name}", handleDeleteSnippet)
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

func withPaneNotify(hub *Hub, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next(w, r)
		go hub.broadcastPaneList()
	}
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
