package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BambooTuna/tmuxui/internal/backend"
	"github.com/BambooTuna/tmuxui/internal/claudecmds"
	"github.com/BambooTuna/tmuxui/internal/selfupdate"
)

// resolve はidをs.registryで解決し、失敗時はbadMsg付きの400を書いてok=falseを返す。
// target(pane)向けは"invalid target"、name(session)向けは"invalid name"を渡すこと
// (レスポンス文言は移行前の挙動を維持する)。
func (s *Server) resolve(w http.ResponseWriter, id, badMsg string) (backend.PaneBackend, string, bool) {
	b, native, err := s.registry.Resolve(id)
	if err != nil {
		http.Error(w, badMsg, http.StatusBadRequest)
		return nil, "", false
	}
	return b, native, true
}

// writeResult はerrがあれば500、なければ204を書く定型処理をまとめたヘルパー。
func writeResult(w http.ResponseWriter, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeResultThen はwriteResultに加え、成功時のみonSuccessを呼ぶ(ピン留めセッション名の
// 追従更新など、副作用を伴うハンドラ向け)。
func writeResultThen(w http.ResponseWriter, err error, onSuccess func()) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if onSuccess != nil {
		onSuccess()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.registry.ListSessions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"sessions": sessions,
	})
}

func (s *Server) handlePaneContent(w http.ResponseWriter, r *http.Request) {
	target, _ := url.PathUnescape(r.PathValue("target"))
	b, native, ok := s.resolve(w, target, "invalid target")
	if !ok {
		return
	}
	pc, err := b.CapturePane(native)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pc.Target = target
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pc)
}

func (s *Server) handlePaneKeys(w http.ResponseWriter, r *http.Request) {
	target, _ := url.PathUnescape(r.PathValue("target"))
	b, native, ok := s.resolve(w, target, "invalid target")
	if !ok {
		return
	}
	var body struct {
		Keys string `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeResult(w, b.SendKeys(native, body.Keys))
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		Dir  string `json:"dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	b, native, ok := s.resolve(w, body.Name, "invalid name")
	if !ok {
		return
	}
	writeResult(w, b.NewSession(native, body.Dir))
}

func (s *Server) handleKillSession(w http.ResponseWriter, r *http.Request) {
	name, _ := url.PathUnescape(r.PathValue("name"))
	b, native, ok := s.resolve(w, name, "invalid name")
	if !ok {
		return
	}
	writeResultThen(w, b.KillSession(native), func() {
		s.preferences.RemovePinnedSession(native)
	})
}

func (s *Server) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	oldName, _ := url.PathUnescape(r.PathValue("name"))
	b, native, ok := s.resolve(w, oldName, "invalid name")
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	writeResultThen(w, b.RenameSession(native, body.Name), func() {
		s.preferences.RenamePinnedSession(native, body.Name)
	})
}

func (s *Server) handleCreateWindow(w http.ResponseWriter, r *http.Request) {
	name, _ := url.PathUnescape(r.PathValue("name"))
	b, native, ok := s.resolve(w, name, "invalid name")
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeResult(w, b.NewWindow(native, body.Name))
}

func (s *Server) handleKillWindow(w http.ResponseWriter, r *http.Request) {
	name, _ := url.PathUnescape(r.PathValue("name"))
	index, _ := url.PathUnescape(r.PathValue("index"))
	b, native, ok := s.resolve(w, name, "invalid name")
	if !ok {
		return
	}
	if !b.ValidTarget(index) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	writeResult(w, b.KillWindow(native+":"+index))
}

func (s *Server) handleRenameWindow(w http.ResponseWriter, r *http.Request) {
	name, _ := url.PathUnescape(r.PathValue("name"))
	index, _ := url.PathUnescape(r.PathValue("index"))
	b, native, ok := s.resolve(w, name, "invalid name")
	if !ok {
		return
	}
	if !b.ValidTarget(index) {
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
	writeResult(w, b.RenameWindow(native+":"+index, body.Name))
}

func (s *Server) handleKillPane(w http.ResponseWriter, r *http.Request) {
	target, _ := url.PathUnescape(r.PathValue("target"))
	b, native, ok := s.resolve(w, target, "invalid target")
	if !ok {
		return
	}
	writeResult(w, b.KillPane(native))
}

func (s *Server) handleSplitPane(w http.ResponseWriter, r *http.Request) {
	target, _ := url.PathUnescape(r.PathValue("target"))
	b, native, ok := s.resolve(w, target, "invalid target")
	if !ok {
		return
	}
	var body struct {
		Horizontal bool `json:"horizontal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeResult(w, b.SplitPane(native, body.Horizontal))
}

func snippetsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "tmuxui", "snippets")
}

// validSnippetName はnameがsnippetsDir()直下のファイル名としてそのまま使って安全かどうかを返す。
// パス区切り(/, \)や".."を含む名前、空文字、"."/".."そのものは拒否する。safeFilerPath
// (filer.go)がroot配下へのパス正規化+prefix検証で境界を守るのに対し、こちらはsnippetsDir()
// 直下のフラットな1セグメントのファイル名しか扱わないため、区切り文字と".."の禁止だけで
// 同等以上に安全(ディレクトリを跨げない)。
func validSnippetName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, "/\\") {
		return false
	}
	return !strings.Contains(name, "..")
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
	if !validSnippetName(name) {
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
	if !validSnippetName(body.Name) {
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
	if !validSnippetName(name) {
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
		if !validSnippetName(body.Name) {
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

func handleDeleteSnippet(w http.ResponseWriter, r *http.Request) {
	name, _ := url.PathUnescape(r.PathValue("name"))
	if !validSnippetName(name) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	if err := os.Remove(filepath.Join(snippetsDir(), name)); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.preferences.GetAll())
}

func (s *Server) handlePutPreferences(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.preferences.Merge(body)
	if s.updates != nil {
		s.updates.NotifyPreferenceChanged()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.updates.Status())
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	status, err := s.updates.CheckNow(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	// body 省略 or version 空 → 従来通り latest を apply
	// {"version":"v2.0.1"} 付き → 指定版に切替 (ApplyVersion)
	var body struct {
		Version string `json:"version"`
	}
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	var (
		status selfupdate.UpdateStatus
		err    error
	)
	if body.Version != "" {
		status, err = s.updates.ApplyVersion(r.Context(), body.Version)
	} else {
		status, err = s.updates.ApplyNow(r.Context())
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleUpdateReleases(w http.ResponseWriter, r *http.Request) {
	rels, err := selfupdate.ListSelectableReleases(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"releases": rels})
}

func handleClaudeCommands(w http.ResponseWriter, r *http.Request) {
	cmds := claudecmds.Load()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"commands": cmds})
}
