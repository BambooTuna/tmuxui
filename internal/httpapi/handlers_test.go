package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/BambooTuna/tmuxui/internal/backend"
	"github.com/BambooTuna/tmuxui/internal/hub"
	"github.com/BambooTuna/tmuxui/internal/prefs"
)

// --- validSnippetName ---

func TestValidSnippetName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"foo.md", true},
		{"my snippet.txt", true},
		{"", false},
		{".", false},
		{"..", false},
		{"../escape", false},
		{"a/../../b", false},
		{"sub/dir", false},
		{"back\\slash", false},
		{"..hidden", false}, // contains ".." even though not a traversal component
	}
	for _, c := range cases {
		if got := validSnippetName(c.name); got != c.want {
			t.Errorf("validSnippetName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// --- fakeBackend: minimal backend.PaneBackend stand-in for handler tests ---

type fakeBackend struct {
	sessions   []backend.Session
	killErr    error
	splitErr   error
	sendKeys   func(target, keys string) error
	splitCalls []struct {
		target     string
		horizontal bool
	}
	newWorktreeErr   error
	newWorktreeCalls []struct {
		sessionName string
		branch      string
	}
}

var _ backend.PaneBackend = (*fakeBackend)(nil)

func (f *fakeBackend) ListSessions() ([]backend.Session, error) { return f.sessions, nil }
func (f *fakeBackend) SyncSessions([]backend.Session)           {}
func (f *fakeBackend) Snapshot(string) ([]byte, int, int, error) {
	return nil, 0, 0, nil
}
func (f *fakeBackend) Subscribe(string) (<-chan []byte, func(), error) {
	return nil, func() {}, nil
}
func (f *fakeBackend) CapturePane(target string) (*backend.PaneContent, error) {
	return &backend.PaneContent{Target: target, Content: "hello"}, nil
}
func (f *fakeBackend) CapturePanePlain(string) (string, error) { return "", nil }
func (f *fakeBackend) SendKeys(target, keys string) error {
	if f.sendKeys != nil {
		return f.sendKeys(target, keys)
	}
	return nil
}
func (f *fakeBackend) Resize(string, int, int) error      { return nil }
func (f *fakeBackend) NewSession(string, string) error    { return nil }
func (f *fakeBackend) KillSession(string) error           { return f.killErr }
func (f *fakeBackend) RenameSession(string, string) error { return nil }
func (f *fakeBackend) NewWindow(string, string) error     { return nil }
func (f *fakeBackend) KillWindow(string) error            { return nil }
func (f *fakeBackend) RenameWindow(string, string) error  { return nil }
func (f *fakeBackend) KillPane(target string) error {
	if target == "missing" {
		return errors.New("no such pane")
	}
	return nil
}
func (f *fakeBackend) SplitPane(target string, horizontal bool) error {
	f.splitCalls = append(f.splitCalls, struct {
		target     string
		horizontal bool
	}{target, horizontal})
	return f.splitErr
}
func (f *fakeBackend) NewWorktree(sessionName, branch string) error {
	f.newWorktreeCalls = append(f.newWorktreeCalls, struct {
		sessionName string
		branch      string
	}{sessionName, branch})
	return f.newWorktreeErr
}
func (f *fakeBackend) OnTopologyChange(func())               {}
func (f *fakeBackend) ValidTarget(s string) bool             { return s != "" }
func (f *fakeBackend) SupportsTextPermissionDetection() bool { return true }

// newTestServer builds a Server wired to a fakeBackend under the "tmux" prefix, and a
// Preferences instance rooted at a temp HOME so tests never touch the real user's
// ~/.config/tmuxui/preferences.json.
func newTestServer(t *testing.T, be *fakeBackend) *Server {
	t.Helper()
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", oldHome) })
	os.Setenv("HOME", t.TempDir())

	reg := backend.NewBackendRegistry("tmux")
	reg.Register("tmux", be)

	h := hub.New()
	h.SetRegistry(reg)

	return &Server{
		hub:         h,
		registry:    reg,
		preferences: prefs.New(),
	}
}

// newReq builds a test request. path is only used as the (generic) request URL; the actual
// route parameters handlers read via r.PathValue are injected directly through pathValues,
// so path never needs to contain the (possibly %-unsafe) raw target/name itself.
func newReq(t *testing.T, method, path string, body string, pathValues map[string]string) *http.Request {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	for k, v := range pathValues {
		r.SetPathValue(k, v)
	}
	return r
}

// --- handleSessions ---

func TestHandleSessions(t *testing.T) {
	be := &fakeBackend{sessions: []backend.Session{{Name: "s1"}}}
	s := newTestServer(t, be)

	w := httptest.NewRecorder()
	s.handleSessions(w, newReq(t, "GET", "/api/sessions", "", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var out struct {
		Sessions []backend.Session `json:"sessions"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Sessions) != 1 || out.Sessions[0].Name != "s1" {
		t.Errorf("sessions = %+v, want one session named s1", out.Sessions)
	}
}

// --- handleKillPane ---

func TestHandleKillPaneSuccess(t *testing.T) {
	be := &fakeBackend{}
	s := newTestServer(t, be)

	w := httptest.NewRecorder()
	s.handleKillPane(w, newReq(t, "DELETE", "/api/panes/x", "", map[string]string{"target": "tmux:main:0.1"}))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleKillPaneBackendError(t *testing.T) {
	be := &fakeBackend{}
	s := newTestServer(t, be)

	w := httptest.NewRecorder()
	s.handleKillPane(w, newReq(t, "DELETE", "/api/panes/x", "", map[string]string{"target": "tmux:missing"}))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleKillPaneInvalidTarget(t *testing.T) {
	be := &fakeBackend{}
	s := newTestServer(t, be)

	w := httptest.NewRecorder()
	// Empty target string fails registry.Resolve (empty id).
	s.handleKillPane(w, newReq(t, "DELETE", "/api/panes/", "", map[string]string{"target": ""}))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if got := strings.TrimSpace(w.Body.String()); got != "invalid target" {
		t.Errorf("body = %q, want %q", got, "invalid target")
	}
}

// --- handleSplitPane: covers the json-decode-error fix (item 8) ---

func TestHandleSplitPaneDefaultsToVerticalWithEmptyBody(t *testing.T) {
	be := &fakeBackend{}
	s := newTestServer(t, be)

	w := httptest.NewRecorder()
	s.handleSplitPane(w, newReq(t, "POST", "/api/panes/x/split", "", map[string]string{"target": "tmux:main:0.1"}))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body=%s", w.Code, w.Body.String())
	}
	if len(be.splitCalls) != 1 || be.splitCalls[0].horizontal {
		t.Errorf("splitCalls = %+v, want one call with horizontal=false", be.splitCalls)
	}
}

func TestHandleSplitPaneHorizontalFromBody(t *testing.T) {
	be := &fakeBackend{}
	s := newTestServer(t, be)

	w := httptest.NewRecorder()
	s.handleSplitPane(w, newReq(t, "POST", "/api/panes/x/split", `{"horizontal":true}`, map[string]string{"target": "tmux:main:0.1"}))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body=%s", w.Code, w.Body.String())
	}
	if len(be.splitCalls) != 1 || !be.splitCalls[0].horizontal {
		t.Errorf("splitCalls = %+v, want one call with horizontal=true", be.splitCalls)
	}
}

// TestHandleSplitPaneMalformedBodyIsBadRequest guards the fix for item 8: previously the
// json decode error from a malformed (non-empty, non-EOF) body was silently ignored and the
// handler proceeded with the zero-value body anyway. It must now return 400.
func TestHandleSplitPaneMalformedBodyIsBadRequest(t *testing.T) {
	be := &fakeBackend{}
	s := newTestServer(t, be)

	w := httptest.NewRecorder()
	s.handleSplitPane(w, newReq(t, "POST", "/api/panes/x/split", `{not valid json`, map[string]string{"target": "tmux:main:0.1"}))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
	if len(be.splitCalls) != 0 {
		t.Errorf("SplitPane should not be called when the body is malformed, got calls: %+v", be.splitCalls)
	}
}

// --- handleCreateWorktree ---

func TestHandleCreateWorktreeSuccess(t *testing.T) {
	be := &fakeBackend{}
	s := newTestServer(t, be)

	w := httptest.NewRecorder()
	s.handleCreateWorktree(w, newReq(t, "POST", "/api/sessions/x/worktrees", `{"branch":"feature/foo"}`, map[string]string{"name": "tmux:main"}))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body=%s", w.Code, w.Body.String())
	}
	if len(be.newWorktreeCalls) != 1 || be.newWorktreeCalls[0].sessionName != "main" || be.newWorktreeCalls[0].branch != "feature/foo" {
		t.Errorf("newWorktreeCalls = %+v, want one call with sessionName=main branch=feature/foo", be.newWorktreeCalls)
	}
}

func TestHandleCreateWorktreeEmptyBranchIsBadRequest(t *testing.T) {
	be := &fakeBackend{}
	s := newTestServer(t, be)

	w := httptest.NewRecorder()
	s.handleCreateWorktree(w, newReq(t, "POST", "/api/sessions/x/worktrees", `{"branch":""}`, map[string]string{"name": "tmux:main"}))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
	if len(be.newWorktreeCalls) != 0 {
		t.Errorf("NewWorktree should not be called with empty branch, got calls: %+v", be.newWorktreeCalls)
	}
}

func TestHandleCreateWorktreeBackendError(t *testing.T) {
	be := &fakeBackend{newWorktreeErr: errors.New("worktree.create failed")}
	s := newTestServer(t, be)

	w := httptest.NewRecorder()
	s.handleCreateWorktree(w, newReq(t, "POST", "/api/sessions/x/worktrees", `{"branch":"foo"}`, map[string]string{"name": "tmux:main"}))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body=%s", w.Code, w.Body.String())
	}
}

// --- handleKillSession: covers writeResultThen's pinned-session side effect ---

func TestHandleKillSessionRemovesPinnedSession(t *testing.T) {
	be := &fakeBackend{}
	s := newTestServer(t, be)
	s.preferences.Merge(map[string]any{"pinnedSessions": []any{"main", "other"}})

	w := httptest.NewRecorder()
	s.handleKillSession(w, newReq(t, "DELETE", "/api/sessions/main", "", map[string]string{"name": "tmux:main"}))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body=%s", w.Code, w.Body.String())
	}
	pinned, _ := s.preferences.GetAll()["pinnedSessions"].([]any)
	if len(pinned) != 1 || pinned[0] != "other" {
		t.Errorf("pinnedSessions after kill = %+v, want [other]", pinned)
	}
}

// --- handlePaneKeys ---

func TestHandlePaneKeysSendsKeys(t *testing.T) {
	var gotTarget, gotKeys string
	be := &fakeBackend{sendKeys: func(target, keys string) error {
		gotTarget, gotKeys = target, keys
		return nil
	}}
	s := newTestServer(t, be)

	w := httptest.NewRecorder()
	s.handlePaneKeys(w, newReq(t, "POST", "/api/panes/x/keys", `{"keys":"echo hi\n"}`, map[string]string{"target": "tmux:main:0.1"}))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body=%s", w.Code, w.Body.String())
	}
	if gotTarget != "main:0.1" || gotKeys != "echo hi\n" {
		t.Errorf("SendKeys called with (%q, %q), want (%q, %q)", gotTarget, gotKeys, "main:0.1", "echo hi\n")
	}
}

func TestHandlePaneKeysMalformedBodyIsBadRequest(t *testing.T) {
	be := &fakeBackend{}
	s := newTestServer(t, be)

	w := httptest.NewRecorder()
	s.handlePaneKeys(w, newReq(t, "POST", "/api/panes/x/keys", `{not json`, map[string]string{"target": "tmux:main:0.1"}))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
