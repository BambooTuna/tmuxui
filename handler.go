package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

var globalActivity *ActivityTracker
var globalPreferences *Preferences

func handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := listSessions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	lastActivity := globalActivity.BuildMap(sessions)
	names := make([]string, 0, len(sessions))
	for _, s := range sessions {
		names = append(names, s.Name)
	}
	globalActivity.Cleanup(names)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"sessions":     sessions,
		"lastActivity": lastActivity,
	})
}

func handlePaneContent(w http.ResponseWriter, r *http.Request) {
	target, _ := url.PathUnescape(r.PathValue("target"))
	if !isValidTarget(target) {
		http.Error(w, "invalid target", http.StatusBadRequest)
		return
	}
	pc, err := capturePane(target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pc)
}

func handlePaneKeys(w http.ResponseWriter, r *http.Request) {
	target, _ := url.PathUnescape(r.PathValue("target"))
	if !isValidTarget(target) {
		http.Error(w, "invalid target", http.StatusBadRequest)
		return
	}
	var body struct {
		Keys string `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := sendKeys(target, body.Keys); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		Dir  string `json:"dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	if err := newSession(body.Name, body.Dir); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleKillSession(w http.ResponseWriter, r *http.Request) {
	name, _ := url.PathUnescape(r.PathValue("name"))
	if !isValidTarget(name) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	if err := killSession(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	globalActivity.Remove(name)
	w.WriteHeader(http.StatusNoContent)
}

func handleRenameSession(w http.ResponseWriter, r *http.Request) {
	oldName, _ := url.PathUnescape(r.PathValue("name"))
	if !isValidTarget(oldName) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	if err := renameSession(oldName, body.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	globalActivity.Rename(oldName, body.Name)
	w.WriteHeader(http.StatusNoContent)
}

func handleCreateWindow(w http.ResponseWriter, r *http.Request) {
	name, _ := url.PathUnescape(r.PathValue("name"))
	if !isValidTarget(name) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := newWindow(name, body.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleKillWindow(w http.ResponseWriter, r *http.Request) {
	name, _ := url.PathUnescape(r.PathValue("name"))
	index, _ := url.PathUnescape(r.PathValue("index"))
	if !isValidTarget(name) || !isValidTarget(index) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	if err := killWindow(name + ":" + index); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleRenameWindow(w http.ResponseWriter, r *http.Request) {
	name, _ := url.PathUnescape(r.PathValue("name"))
	index, _ := url.PathUnescape(r.PathValue("index"))
	if !isValidTarget(name) || !isValidTarget(index) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	if err := renameWindow(name+":"+index, body.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleKillPane(w http.ResponseWriter, r *http.Request) {
	target, _ := url.PathUnescape(r.PathValue("target"))
	if !isValidTarget(target) {
		http.Error(w, "invalid target", http.StatusBadRequest)
		return
	}
	if err := killPane(target); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleSplitPane(w http.ResponseWriter, r *http.Request) {
	target, _ := url.PathUnescape(r.PathValue("target"))
	if !isValidTarget(target) {
		http.Error(w, "invalid target", http.StatusBadRequest)
		return
	}
	var body struct {
		Horizontal bool `json:"horizontal"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if err := splitPane(target, body.Horizontal); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func snippetsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "tmuxui", "snippets")
}

func handleSnippetList(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(snippetsDir())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"snippets": []any{}})
		return
	}
	type snippet struct {
		Name  string `json:"name"`
		Label string `json:"label"`
	}
	var list []snippet
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		label := strings.TrimSuffix(name, filepath.Ext(name))
		list = append(list, snippet{Name: name, Label: label})
	}
	if list == nil {
		list = []snippet{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"snippets": list})
}

func handleSnippetContent(w http.ResponseWriter, r *http.Request) {
	name, _ := url.PathUnescape(r.PathValue("name"))
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(filepath.Join(snippetsDir(), name))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"content": string(data)})
}

func handleCreateSnippet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	if strings.Contains(body.Name, "/") || strings.Contains(body.Name, "..") {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	path := filepath.Join(snippetsDir(), body.Name)
	if _, err := os.Stat(path); err == nil {
		http.Error(w, "already exists", http.StatusConflict)
		return
	}
	if err := os.MkdirAll(snippetsDir(), 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(path, []byte(body.Content), 0644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleUpdateSnippet(w http.ResponseWriter, r *http.Request) {
	name, _ := url.PathUnescape(r.PathValue("name"))
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	oldPath := filepath.Join(snippetsDir(), name)
	if _, err := os.Stat(oldPath); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var body struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.Name != "" && body.Name != name {
		if strings.Contains(body.Name, "/") || strings.Contains(body.Name, "..") {
			http.Error(w, "invalid name", http.StatusBadRequest)
			return
		}
		newPath := filepath.Join(snippetsDir(), body.Name)
		if _, err := os.Stat(newPath); err == nil {
			http.Error(w, "already exists", http.StatusConflict)
			return
		}
	}
	if err := os.WriteFile(oldPath, []byte(body.Content), 0644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if body.Name != "" && body.Name != name {
		if err := os.Rename(oldPath, filepath.Join(snippetsDir(), body.Name)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(globalPreferences.GetAll())
}

func handlePutPreferences(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	globalPreferences.Merge(body)
	w.WriteHeader(http.StatusNoContent)
}

func handleClaudeCommands(w http.ResponseWriter, r *http.Request) {
	cmds := loadClaudeCommands()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"commands": cmds})
}

func handleDeleteSnippet(w http.ResponseWriter, r *http.Request) {
	name, _ := url.PathUnescape(r.PathValue("name"))
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	if err := os.Remove(filepath.Join(snippetsDir(), name)); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
