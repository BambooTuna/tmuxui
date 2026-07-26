package herdr

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BambooTuna/tmuxui/internal/backend"
)

// fakeHerdrServer is a minimal fake herdr socket server for protocol-level tests.
// It listens on a Unix socket under t.TempDir() and dispatches each request line to a
// handler keyed by method name.
type fakeHerdrServer struct {
	mu       sync.Mutex
	handlers map[string]func(params json.RawMessage) (result interface{}, errCode, errMsg string)
	calls    []string // methods invoked, in order, for assertions

	ln net.Listener
}

func newFakeHerdrServer(t *testing.T) *fakeHerdrServer {
	t.Helper()
	// macOS caps AF_UNIX paths (sun_path) at ~104 bytes; t.TempDir() embeds the full test
	// name and fails for our longer test names, so use a short, independent temp dir instead.
	dir, err := os.MkdirTemp("", "hs")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sockPath := filepath.Join(dir, "h.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeHerdrServer{
		handlers: map[string]func(json.RawMessage) (interface{}, string, string){},
		ln:       ln,
	}
	go s.serve()
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *fakeHerdrServer) socketPath() string {
	return s.ln.Addr().String()
}

func (s *fakeHerdrServer) on(method string, fn func(params json.RawMessage) (result interface{}, errCode, errMsg string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = fn
}

func (s *fakeHerdrServer) calledMethods() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.calls))
	copy(out, s.calls)
	return out
}

func (s *fakeHerdrServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *fakeHerdrServer) handleConn(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return
	}
	var req herdrRequest
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		return
	}

	s.mu.Lock()
	s.calls = append(s.calls, req.Method)
	fn := s.handlers[req.Method]
	s.mu.Unlock()

	var paramsRaw json.RawMessage
	if req.Params != nil {
		paramsRaw, _ = json.Marshal(req.Params)
	}

	resp := map[string]interface{}{"id": req.ID}
	if fn == nil {
		resp["error"] = map[string]string{"code": "unknown_method", "message": "no handler: " + req.Method}
	} else {
		result, errCode, errMsg := fn(paramsRaw)
		if errCode != "" {
			resp["error"] = map[string]string{"code": errCode, "message": errMsg}
		} else {
			resp["result"] = result
		}
	}
	data, _ := json.Marshal(resp)
	conn.Write(append(data, '\n'))
}

func TestHerdrClientCallSuccess(t *testing.T) {
	s := newFakeHerdrServer(t)
	s.on("ping", func(json.RawMessage) (interface{}, string, string) {
		return map[string]string{"type": "pong"}, "", ""
	})

	c := newHerdrClient(s.socketPath())
	var out struct {
		Type string `json:"type"`
	}
	if err := c.call("ping", struct{}{}, &out); err != nil {
		t.Fatalf("call: unexpected error: %v", err)
	}
	if out.Type != "pong" {
		t.Errorf("out.Type = %q, want pong", out.Type)
	}
}

func TestHerdrClientCallError(t *testing.T) {
	s := newFakeHerdrServer(t)
	s.on("pane.split", func(json.RawMessage) (interface{}, string, string) {
		return nil, "invalid_request", "missing field `direction`"
	})

	c := newHerdrClient(s.socketPath())
	err := c.call("pane.split", struct{}{}, nil)
	if err == nil {
		t.Fatal("call: expected error, got nil")
	}
	herr, ok := err.(*herdrError)
	if !ok {
		t.Fatalf("call: error type = %T, want *herdrError", err)
	}
	if herr.Code != "invalid_request" {
		t.Errorf("herr.Code = %q, want invalid_request", herr.Code)
	}
}

func TestHerdrClientCallUnreachableSocket(t *testing.T) {
	c := newHerdrClient(filepath.Join(t.TempDir(), "does-not-exist.sock"))
	if err := c.call("ping", struct{}{}, nil); err == nil {
		t.Fatal("call: expected error for unreachable socket, got nil")
	}
}

// TestHerdrClientCallReusesConnectionPerCall verifies each call opens (and the server
// sees) an independent connection/request, matching the "no persistent multiplexed
// connection" design.
func TestHerdrClientCallSequentialRequests(t *testing.T) {
	s := newFakeHerdrServer(t)
	s.on("ping", func(json.RawMessage) (interface{}, string, string) {
		return map[string]string{"type": "pong"}, "", ""
	})

	c := newHerdrClient(s.socketPath())
	for i := 0; i < 3; i++ {
		if err := c.call("ping", struct{}{}, nil); err != nil {
			t.Fatalf("call #%d: %v", i, err)
		}
	}
	if got := len(s.calledMethods()); got != 3 {
		t.Errorf("calledMethods count = %d, want 3", got)
	}
}

// --- ListSessions mapping ---

func TestHerdrBackendListSessionsMapsWorkspaceTabPane(t *testing.T) {
	s := newFakeHerdrServer(t)
	s.on("workspace.list", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{
			"workspaces": []map[string]interface{}{
				{"workspace_id": "w1", "number": 1, "label": "proj", "focused": true, "agent_status": "working"},
			},
		}, "", ""
	})
	s.on("tab.list", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{
			"tabs": []map[string]interface{}{
				{"tab_id": "w1:t1", "workspace_id": "w1", "number": 1, "label": "1", "focused": true, "agent_status": "working"},
			},
		}, "", ""
	})
	s.on("pane.list", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{
			"panes": []map[string]interface{}{
				{
					"pane_id": "w1:p1", "terminal_id": "term_abc", "workspace_id": "w1", "tab_id": "w1:t1",
					"focused": true, "cwd": "/home/user/proj", "foreground_cwd": "/home/user/proj", "agent": "claude",
					"agent_status": "working",
				},
			},
		}, "", ""
	})
	s.on("pane.layout", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{
			"layout": map[string]interface{}{
				"area": map[string]int{"width": 80, "height": 24},
				"panes": []map[string]interface{}{
					{"pane_id": "w1:p1", "rect": map[string]int{"width": 80, "height": 24}},
				},
			},
		}, "", ""
	})

	b := New(s.socketPath())
	defer b.Close()

	sessions, err := b.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}
	sess := sessions[0]
	if sess.Name != "w1" {
		t.Errorf("sess.Name = %q, want w1 (native workspace_id)", sess.Name)
	}
	if !sess.Attached {
		t.Errorf("sess.Attached = false, want true (workspace focused)")
	}
	if sess.DisplayName != "proj" {
		t.Errorf("sess.DisplayName = %q, want proj (from workspace.label)", sess.DisplayName)
	}
	if sess.AgentStatus != "working" {
		t.Errorf("sess.AgentStatus = %q, want working (from workspace.list agent_status)", sess.AgentStatus)
	}
	if sess.WorktreeLabel != "" {
		t.Errorf("sess.WorktreeLabel = %q, want empty (no worktree field in fixture)", sess.WorktreeLabel)
	}
	if len(sess.Windows) != 1 {
		t.Fatalf("len(sess.Windows) = %d, want 1", len(sess.Windows))
	}
	win := sess.Windows[0]
	if win.Index != 1 || win.ID != "w1:t1" || !win.Active {
		t.Errorf("win = %+v, unexpected", win)
	}
	if win.AgentStatus != "working" {
		t.Errorf("win.AgentStatus = %q, want working (from tab.list agent_status)", win.AgentStatus)
	}
	if len(win.Panes) != 1 {
		t.Fatalf("len(win.Panes) = %d, want 1", len(win.Panes))
	}
	pane := win.Panes[0]
	if pane.Target != "w1:p1" {
		t.Errorf("pane.Target = %q, want w1:p1 (native pane_id, no prefix at backend level)", pane.Target)
	}
	if pane.ID != "term_abc" {
		t.Errorf("pane.ID = %q, want term_abc", pane.ID)
	}
	if pane.Cmd != "claude" {
		t.Errorf("pane.Cmd = %q, want claude", pane.Cmd)
	}
	if pane.Agent != "claude" {
		t.Errorf("pane.Agent = %q, want claude", pane.Agent)
	}
	if pane.AgentStatus != "working" {
		t.Errorf("pane.AgentStatus = %q, want working (pane.list itself carries agent_status)", pane.AgentStatus)
	}
	if pane.Size != "80x24" {
		t.Errorf("pane.Size = %q, want 80x24", pane.Size)
	}
	if pane.Path != "/home/user/proj" {
		t.Errorf("pane.Path = %q, want /home/user/proj", pane.Path)
	}
}

func TestHerdrBackendListSessionsMapsWorktreeLabel(t *testing.T) {
	s := newFakeHerdrServer(t)
	s.on("workspace.list", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{
			"workspaces": []map[string]interface{}{
				{
					"workspace_id": "w1", "number": 1, "label": "proj", "focused": false, "agent_status": "idle",
					"worktree": map[string]interface{}{
						"repo_key": "/home/user/proj/.git", "repo_name": "proj",
						"repo_root": "/home/user/proj", "checkout_path": "/home/user/proj",
						"is_linked_worktree": false,
					},
				},
				{
					"workspace_id": "w2", "number": 2, "label": "feat", "focused": false, "agent_status": "unknown",
					"worktree": map[string]interface{}{
						"repo_key": "/home/user/proj/.git", "repo_name": "proj",
						"repo_root": "/home/user/proj", "checkout_path": "/home/user/.herdr/worktrees/proj/my-feature",
						"is_linked_worktree": true,
					},
				},
				{"workspace_id": "w3", "number": 3, "label": "noworktree", "focused": false, "agent_status": "unknown"},
			},
		}, "", ""
	})
	s.on("tab.list", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{"tabs": []map[string]interface{}{}}, "", ""
	})
	s.on("pane.list", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{"panes": []map[string]interface{}{}}, "", ""
	})

	b := New(s.socketPath())
	defer b.Close()

	sessions, err := b.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	byName := map[string]backend.Session{}
	for _, s := range sessions {
		byName[s.Name] = s
	}

	if got := byName["w1"].WorktreeLabel; got != "proj" {
		t.Errorf("w1.WorktreeLabel = %q, want proj (main checkout: repo name only)", got)
	}
	if got := byName["w2"].WorktreeLabel; got != "proj · my-feature" {
		t.Errorf("w2.WorktreeLabel = %q, want %q (linked worktree: repo + dirname)", got, "proj · my-feature")
	}
	if got := byName["w3"].WorktreeLabel; got != "" {
		t.Errorf("w3.WorktreeLabel = %q, want empty (no worktree field)", got)
	}
}

func TestHerdrBackendListSessionsPropagatesError(t *testing.T) {
	s := newFakeHerdrServer(t)
	s.on("workspace.list", func(json.RawMessage) (interface{}, string, string) {
		return nil, "internal_error", "boom"
	})
	b := New(s.socketPath())
	defer b.Close()

	if _, err := b.ListSessions(); err == nil {
		t.Fatal("ListSessions: expected error, got nil")
	}
}

// --- SendKeys translation ---

func TestHerdrBackendSendKeysSpecialKey(t *testing.T) {
	s := newFakeHerdrServer(t)
	var gotParams json.RawMessage
	s.on("pane.send_keys", func(params json.RawMessage) (interface{}, string, string) {
		gotParams = params
		return map[string]string{"type": "ok"}, "", ""
	})
	b := New(s.socketPath())
	defer b.Close()

	if err := b.SendKeys("w1:p1", "Enter"); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	var params struct {
		PaneID string   `json:"pane_id"`
		Keys   []string `json:"keys"`
	}
	if err := json.Unmarshal(gotParams, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.PaneID != "w1:p1" || len(params.Keys) != 1 || params.Keys[0] != "enter" {
		t.Errorf("params = %+v, want pane_id=w1:p1 keys=[enter]", params)
	}
}

func TestHerdrBackendSendKeysCtrlChord(t *testing.T) {
	s := newFakeHerdrServer(t)
	var gotKeys []string
	s.on("pane.send_keys", func(params json.RawMessage) (interface{}, string, string) {
		var p struct {
			Keys []string `json:"keys"`
		}
		json.Unmarshal(params, &p)
		gotKeys = p.Keys
		return map[string]string{"type": "ok"}, "", ""
	})
	b := New(s.socketPath())
	defer b.Close()

	if err := b.SendKeys("w1:p1", "C-c"); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	if len(gotKeys) != 1 || gotKeys[0] != "ctrl+c" {
		t.Errorf("gotKeys = %v, want [ctrl+c]", gotKeys)
	}
}

func TestHerdrBackendSendKeysUnsupportedNamedKeyFallsBackToRawEscape(t *testing.T) {
	s := newFakeHerdrServer(t)
	var gotText string
	var sendKeysCalled bool
	s.on("pane.send_keys", func(json.RawMessage) (interface{}, string, string) {
		sendKeysCalled = true
		return map[string]string{"type": "ok"}, "", ""
	})
	s.on("pane.send_text", func(params json.RawMessage) (interface{}, string, string) {
		var p struct {
			Text string `json:"text"`
		}
		json.Unmarshal(params, &p)
		gotText = p.Text
		return map[string]string{"type": "ok"}, "", ""
	})
	b := New(s.socketPath())
	defer b.Close()

	// PPage (Page Up) is not in herdr's pane.send_keys vocabulary (confirmed against the
	// real server: it returns "unsupported key"), so it must go through send_text as a raw
	// ANSI escape sequence instead of send_keys.
	if err := b.SendKeys("w1:p1", "PPage"); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	if sendKeysCalled {
		t.Errorf("pane.send_keys should not be called for PPage")
	}
	if gotText != "\x1b[5~" {
		t.Errorf("gotText = %q, want ESC[5~", gotText)
	}
}

func TestHerdrBackendSendKeysLiteralText(t *testing.T) {
	s := newFakeHerdrServer(t)
	var gotText string
	s.on("pane.send_text", func(params json.RawMessage) (interface{}, string, string) {
		var p struct {
			Text string `json:"text"`
		}
		json.Unmarshal(params, &p)
		gotText = p.Text
		return map[string]string{"type": "ok"}, "", ""
	})
	b := New(s.socketPath())
	defer b.Close()

	if err := b.SendKeys("w1:p1", "echo hi\n"); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	if gotText != "echo hi\n" {
		t.Errorf("gotText = %q, want %q (herdr's send_text interprets embedded \\n as Enter)", gotText, "echo hi\n")
	}
}

// --- Permission detection delegation ---

// TestHerdrBackendSupportsTextPermissionDetectionIsFalse guards against duplicate "permission
// detected" notifications: herdr panes carry their own structured agent_status, so the Hub's
// text-heuristic detectPermission (websocket.go pollPanes) must skip herdr-backed targets.
func TestHerdrBackendSupportsTextPermissionDetectionIsFalse(t *testing.T) {
	b := New("/nonexistent.sock")
	defer b.Close()
	if b.SupportsTextPermissionDetection() {
		t.Error("HerdrBackend.SupportsTextPermissionDetection() = true, want false")
	}
}

// --- ValidTarget ---

func TestHerdrBackendValidTarget(t *testing.T) {
	b := New("/nonexistent.sock")
	defer b.Close()

	cases := []struct {
		target string
		want   bool
	}{
		{"w1:p1", true},
		{"wY:p2", true},
		{"w3:pJ", true},
		{"w1:t2", true},
		// workspace_id単体・素の数値インデックス(NewWindow/RenameSession/KillSession等の
		// workspace単位操作やhandleKillWindow/handleRenameWindowが渡す"2"のようなtab.number)
		// も許可する(旧実装ではpane_id形式しか許可されず、これらが軒並みHTTP 400になっていたバグの修正)。
		{"w1", true},
		{"2", true},
		{"", false},
		{"w1:0.0", false},
		{"w1:x2", false},
		{"w1:", false},
	}
	for _, c := range cases {
		if got := b.ValidTarget(c.target); got != c.want {
			t.Errorf("ValidTarget(%q) = %v, want %v", c.target, got, c.want)
		}
	}
}

// --- Subscribe polling ---

func TestHerdrBackendSubscribeDeliversChangedContent(t *testing.T) {
	s := newFakeHerdrServer(t)
	var mu sync.Mutex
	text := "first"
	s.on("pane.read", func(json.RawMessage) (interface{}, string, string) {
		mu.Lock()
		defer mu.Unlock()
		return map[string]interface{}{
			"read": map[string]string{"text": text},
		}, "", ""
	})

	b := New(s.socketPath())
	defer b.Close()

	stream, cancel, err := b.Subscribe("w1:p1")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	select {
	case chunk := <-stream:
		if string(chunk) != "\x1b[H\x1b[2Jfirst" {
			t.Errorf("first chunk = %q, want clear+first", chunk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first chunk")
	}

	mu.Lock()
	text = "second"
	mu.Unlock()

	select {
	case chunk := <-stream:
		if string(chunk) != "\x1b[H\x1b[2Jsecond" {
			t.Errorf("second chunk = %q, want clear+second", chunk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second (changed) chunk")
	}
}

func TestHerdrBackendSubscribeCancelClosesChannel(t *testing.T) {
	s := newFakeHerdrServer(t)
	s.on("pane.read", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{"read": map[string]string{"text": "x"}}, "", ""
	})
	b := New(s.socketPath())
	defer b.Close()

	stream, cancel, err := b.Subscribe("w1:p1")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	cancel()

	select {
	case _, ok := <-stream:
		if ok {
			// A final buffered chunk might race in before cancel(); drain until closed.
			for ok {
				_, ok = <-stream
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for channel to close after cancel")
	}
}

// --- Mutating operations: verify request shapes ---

func TestHerdrBackendNewSession(t *testing.T) {
	s := newFakeHerdrServer(t)
	var got map[string]interface{}
	s.on("workspace.create", func(params json.RawMessage) (interface{}, string, string) {
		json.Unmarshal(params, &got)
		return map[string]interface{}{"workspace": map[string]interface{}{"workspace_id": "wNew"}}, "", ""
	})
	b := New(s.socketPath())
	defer b.Close()

	if err := b.NewSession("myproj", "/tmp/dir"); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if got["label"] != "myproj" || got["cwd"] != "/tmp/dir" || got["focus"] != false {
		t.Errorf("params = %+v, unexpected", got)
	}
}

func TestHerdrBackendKillSession(t *testing.T) {
	s := newFakeHerdrServer(t)
	var got map[string]interface{}
	s.on("workspace.close", func(params json.RawMessage) (interface{}, string, string) {
		json.Unmarshal(params, &got)
		return map[string]interface{}{"type": "ok"}, "", ""
	})
	b := New(s.socketPath())
	defer b.Close()

	if err := b.KillSession("w1"); err != nil {
		t.Fatalf("KillSession: %v", err)
	}
	if got["workspace_id"] != "w1" {
		t.Errorf("params = %+v, want workspace_id=w1", got)
	}
}

func TestHerdrBackendRenameSession(t *testing.T) {
	s := newFakeHerdrServer(t)
	var got map[string]interface{}
	s.on("workspace.rename", func(params json.RawMessage) (interface{}, string, string) {
		json.Unmarshal(params, &got)
		return map[string]interface{}{"type": "ok"}, "", ""
	})
	b := New(s.socketPath())
	defer b.Close()

	if err := b.RenameSession("w1", "newlabel"); err != nil {
		t.Fatalf("RenameSession: %v", err)
	}
	if got["workspace_id"] != "w1" || got["label"] != "newlabel" {
		t.Errorf("params = %+v, unexpected", got)
	}
}

func TestHerdrBackendNewWindow(t *testing.T) {
	s := newFakeHerdrServer(t)
	var got map[string]interface{}
	s.on("tab.create", func(params json.RawMessage) (interface{}, string, string) {
		json.Unmarshal(params, &got)
		return map[string]interface{}{"tab": map[string]interface{}{"tab_id": "w1:t2"}}, "", ""
	})
	b := New(s.socketPath())
	defer b.Close()

	if err := b.NewWindow("w1", "mytab"); err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	if got["workspace_id"] != "w1" || got["label"] != "mytab" || got["focus"] != false {
		t.Errorf("params = %+v, unexpected", got)
	}
}

func TestHerdrBackendNewWorktree(t *testing.T) {
	s := newFakeHerdrServer(t)
	var got map[string]interface{}
	s.on("worktree.create", func(params json.RawMessage) (interface{}, string, string) {
		json.Unmarshal(params, &got)
		return map[string]interface{}{"workspace": map[string]interface{}{"workspace_id": "wNew"}}, "", ""
	})
	b := New(s.socketPath())
	defer b.Close()

	if err := b.NewWorktree("w1", "feature/foo"); err != nil {
		t.Fatalf("NewWorktree: %v", err)
	}
	if got["workspace_id"] != "w1" || got["branch"] != "feature/foo" || got["focus"] != false {
		t.Errorf("params = %+v, unexpected", got)
	}
}

func TestHerdrBackendKillWindowResolvesTabID(t *testing.T) {
	s := newFakeHerdrServer(t)
	s.on("tab.list", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{
			"tabs": []map[string]interface{}{
				{"tab_id": "w1:t1", "workspace_id": "w1", "number": 1},
				{"tab_id": "w1:t2", "workspace_id": "w1", "number": 2},
			},
		}, "", ""
	})
	var got map[string]interface{}
	s.on("tab.close", func(params json.RawMessage) (interface{}, string, string) {
		json.Unmarshal(params, &got)
		return map[string]interface{}{"type": "ok"}, "", ""
	})
	b := New(s.socketPath())
	defer b.Close()

	// handler builds this target as native(workspace_id) + ":" + Window.Index (tab.number).
	if err := b.KillWindow("w1:2"); err != nil {
		t.Fatalf("KillWindow: %v", err)
	}
	if got["tab_id"] != "w1:t2" {
		t.Errorf("params = %+v, want tab_id=w1:t2", got)
	}
}

func TestHerdrBackendKillWindowUnknownIndexErrors(t *testing.T) {
	s := newFakeHerdrServer(t)
	s.on("tab.list", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{"tabs": []map[string]interface{}{}}, "", ""
	})
	b := New(s.socketPath())
	defer b.Close()

	if err := b.KillWindow("w1:99"); err == nil {
		t.Fatal("KillWindow: expected error for unknown window index, got nil")
	}
}

func TestHerdrBackendKillPane(t *testing.T) {
	s := newFakeHerdrServer(t)
	var got map[string]interface{}
	s.on("pane.close", func(params json.RawMessage) (interface{}, string, string) {
		json.Unmarshal(params, &got)
		return map[string]interface{}{"type": "ok"}, "", ""
	})
	b := New(s.socketPath())
	defer b.Close()

	if err := b.KillPane("w1:p1"); err != nil {
		t.Fatalf("KillPane: %v", err)
	}
	if got["pane_id"] != "w1:p1" {
		t.Errorf("params = %+v, want pane_id=w1:p1", got)
	}
}

func TestHerdrBackendSplitPaneUsesTargetPaneIDDirectly(t *testing.T) {
	s := newFakeHerdrServer(t)
	var splitParams map[string]interface{}
	var focusCalled bool
	s.on("pane.focus", func(json.RawMessage) (interface{}, string, string) {
		focusCalled = true
		return map[string]interface{}{"type": "ok"}, "", ""
	})
	s.on("pane.split", func(params json.RawMessage) (interface{}, string, string) {
		json.Unmarshal(params, &splitParams)
		return map[string]interface{}{"type": "ok"}, "", ""
	})
	b := New(s.socketPath())
	defer b.Close()

	if err := b.SplitPane("w1:p1", true); err != nil {
		t.Fatalf("SplitPane: %v", err)
	}
	// Confirmed against herdr's source (src/api/schema/panes.rs, PaneSplitParams) that
	// pane.split accepts an explicit target_pane_id, so no pane.focus round-trip (and no
	// global UI focus change) should be needed at all.
	if focusCalled {
		t.Error("SplitPane should not call pane.focus; target_pane_id targets the pane directly")
	}
	if splitParams["target_pane_id"] != "w1:p1" {
		t.Errorf("splitParams = %+v, want target_pane_id=w1:p1", splitParams)
	}
	if splitParams["direction"] != "right" {
		t.Errorf("splitParams = %+v, want direction=right for horizontal=true", splitParams)
	}
	if splitParams["focus"] != false {
		t.Errorf("splitParams = %+v, want focus=false", splitParams)
	}
}

func TestHerdrBackendSplitPaneVerticalDirection(t *testing.T) {
	s := newFakeHerdrServer(t)
	var splitParams map[string]interface{}
	s.on("pane.split", func(params json.RawMessage) (interface{}, string, string) {
		json.Unmarshal(params, &splitParams)
		return map[string]interface{}{"type": "ok"}, "", ""
	})
	b := New(s.socketPath())
	defer b.Close()

	if err := b.SplitPane("w1:p1", false); err != nil {
		t.Fatalf("SplitPane: %v", err)
	}
	if splitParams["direction"] != "down" {
		t.Errorf("splitParams = %+v, want direction=down for horizontal=false", splitParams)
	}
}

// --- Resize / SyncSessions no-ops ---

func TestHerdrBackendResizeIsNoop(t *testing.T) {
	s := newFakeHerdrServer(t)
	called := false
	s.on("pane.resize", func(json.RawMessage) (interface{}, string, string) {
		called = true
		return map[string]interface{}{"type": "ok"}, "", ""
	})
	b := New(s.socketPath())
	defer b.Close()

	if err := b.Resize("w1:p1", 80, 24); err != nil {
		t.Fatalf("Resize: unexpected error: %v", err)
	}
	if called {
		t.Error("Resize should not call pane.resize (no absolute-size API exists)")
	}
}

func TestHerdrBackendSyncSessionsIsNoop(t *testing.T) {
	b := New("/nonexistent.sock")
	defer b.Close()
	// Must not panic and must not require any socket connectivity.
	b.SyncSessions([]backend.Session{{Name: "whatever"}})
}

// --- pane size detection (実pty実測: stripAnsi/measureMaxWidth/observePaneSize/paneSize) ---

func TestStripAnsiRemovesSGRAndCR(t *testing.T) {
	in := "\x1b[31mhello\x1b[0m\r\nworld\r\n"
	got := stripAnsi(in)
	want := "hello\nworld\n"
	if got != want {
		t.Errorf("stripAnsi(%q) = %q, want %q", in, got, want)
	}
}

func TestMeasureMaxWidthCountsFullWidthCharsAsTwoCells(t *testing.T) {
	// "全角文字"は4文字、East Asian Width Wideなので表示幅は8になる(半角なら4のはず)。
	if got := measureMaxWidth("全角文字"); got != 8 {
		t.Errorf("measureMaxWidth(zenkaku) = %d, want 8", got)
	}
}

func TestMeasureMaxWidthIgnoresANSIAndTakesLongestLine(t *testing.T) {
	text := "\x1b[31mshort\x1b[0m\r\n" + strings.Repeat("x", 60) + "\r\nshort2"
	if got := measureMaxWidth(text); got != 60 {
		t.Errorf("measureMaxWidth = %d, want 60 (longest line, ANSI stripped)", got)
	}
}

// TestHerdrBackendSnapshotReturnsRealPtySize は本バグの中心的な回帰テスト。
// pane.layoutが返すrect(グリッド座標)ではなく、pane.get.scroll.viewport_rowsと
// pane.readテキストの実測幅が優先されることを確認する(実機確認済みの172x51 vs 54x23と同じ構図)。
func TestHerdrBackendSnapshotReturnsRealPtySize(t *testing.T) {
	s := newFakeHerdrServer(t)
	wideText := "\x1b[31m" + strings.Repeat("x", 172) + "\x1b[0m\r\nshort line"
	s.on("pane.read", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{"read": map[string]string{"text": wideText}}, "", ""
	})
	s.on("pane.get", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{
			"pane": map[string]interface{}{
				"scroll": map[string]interface{}{"viewport_rows": 51, "max_offset_from_bottom": 0},
			},
		}, "", ""
	})
	// pane.layoutは古いグリッド座標(54x23)を返す。実測値が優先され使われないことを確認する。
	s.on("pane.layout", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{
			"layout": map[string]interface{}{
				"area": map[string]int{"width": 54, "height": 23},
				"panes": []map[string]interface{}{
					{"pane_id": "w1:p1", "rect": map[string]int{"width": 54, "height": 23}},
				},
			},
		}, "", ""
	})

	b := New(s.socketPath())
	defer b.Close()

	data, cols, rows, err := b.Snapshot("w1:p1")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if cols != 172 {
		t.Errorf("cols = %d, want 172 (measured width, not layout rect 54)", cols)
	}
	if rows != 51 {
		t.Errorf("rows = %d, want 51 (viewport_rows, not layout rect 23)", rows)
	}
	if len(data) == 0 {
		t.Error("Snapshot data is empty")
	}
}

func TestHerdrBackendPaneSizeFallsBackToLayoutRectWhenViewportRowsUnavailable(t *testing.T) {
	s := newFakeHerdrServer(t)
	// pane.getハンドラを登録しない -> viewport_rows取得失敗 -> layout rectへフォールバック。
	// colsCacheも未観測(pane.read未呼び出し)なのでcolsもrectへフォールバックする。
	s.on("pane.layout", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{
			"layout": map[string]interface{}{
				"area": map[string]int{"width": 54, "height": 23},
				"panes": []map[string]interface{}{
					{"pane_id": "w1:p1", "rect": map[string]int{"width": 54, "height": 23}},
				},
			},
		}, "", ""
	})

	b := New(s.socketPath())
	defer b.Close()

	cols, rows := b.paneSize("w1:p1")
	if cols != 54 || rows != 23 {
		t.Errorf("paneSize = %dx%d, want 54x23 (layout rect fallback)", cols, rows)
	}
}

// TestHerdrBackendColsCacheGrowsOnly はcolsCacheが縮まないことを確認する。内容依存で幅が
// 揺れて再subscribeループになるのを防ぐための設計(observePaneSizeのコメント参照)。
func TestHerdrBackendColsCacheGrowsOnly(t *testing.T) {
	s := newFakeHerdrServer(t)
	var mu sync.Mutex
	text := strings.Repeat("x", 100)
	s.on("pane.read", func(json.RawMessage) (interface{}, string, string) {
		mu.Lock()
		defer mu.Unlock()
		return map[string]interface{}{"read": map[string]string{"text": text}}, "", ""
	})
	s.on("pane.get", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{
			"pane": map[string]interface{}{"scroll": map[string]interface{}{"viewport_rows": 24}},
		}, "", ""
	})
	s.on("pane.layout", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{
			"layout": map[string]interface{}{"area": map[string]int{"width": 10, "height": 10}, "panes": []map[string]interface{}{}},
		}, "", ""
	})

	b := New(s.socketPath())
	defer b.Close()

	_, cols1, _, err := b.Snapshot("w1:p1")
	if err != nil {
		t.Fatalf("Snapshot #1: %v", err)
	}
	if cols1 != 100 {
		t.Fatalf("cols1 = %d, want 100", cols1)
	}

	mu.Lock()
	text = strings.Repeat("x", 20) // 狭いテキストに変わる
	mu.Unlock()

	_, cols2, _, err := b.Snapshot("w1:p1")
	if err != nil {
		t.Fatalf("Snapshot #2: %v", err)
	}
	if cols2 != 100 {
		t.Errorf("cols2 = %d, want 100 (colsCache should not shrink)", cols2)
	}
}

// TestHerdrBackendListSessionsUsesCachedSizeAfterObserved はweb/transport/ws.jsの
// checkPaneSizeSync再subscribeループ対策の回帰テスト: colsCache/rowsCacheが確定した後は
// pane_listのSizeもSnapshotが返す実サイズと一致するようになることを確認する。
func TestHerdrBackendListSessionsUsesCachedSizeAfterObserved(t *testing.T) {
	s := newFakeHerdrServer(t)
	s.on("workspace.list", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{
			"workspaces": []map[string]interface{}{
				{"workspace_id": "w1", "number": 1, "label": "proj", "focused": true, "agent_status": "working"},
			},
		}, "", ""
	})
	s.on("tab.list", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{
			"tabs": []map[string]interface{}{
				{"tab_id": "w1:t1", "workspace_id": "w1", "number": 1, "label": "1", "focused": true, "agent_status": "working"},
			},
		}, "", ""
	})
	s.on("pane.list", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{
			"panes": []map[string]interface{}{
				{"pane_id": "w1:p1", "terminal_id": "term_abc", "workspace_id": "w1", "tab_id": "w1:t1", "focused": true, "agent_status": "working"},
			},
		}, "", ""
	})
	s.on("pane.layout", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{
			"layout": map[string]interface{}{
				"area": map[string]int{"width": 54, "height": 23},
				"panes": []map[string]interface{}{
					{"pane_id": "w1:p1", "rect": map[string]int{"width": 54, "height": 23}},
				},
			},
		}, "", ""
	})
	s.on("pane.read", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{"read": map[string]string{"text": strings.Repeat("x", 172)}}, "", ""
	})
	s.on("pane.get", func(json.RawMessage) (interface{}, string, string) {
		return map[string]interface{}{
			"pane": map[string]interface{}{"scroll": map[string]interface{}{"viewport_rows": 51}},
		}, "", ""
	})

	b := New(s.socketPath())
	defer b.Close()

	// 観測前はrect由来のSize。
	sessionsBefore, err := b.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions (before): %v", err)
	}
	if got := sessionsBefore[0].Windows[0].Panes[0].Size; got != "54x23" {
		t.Errorf("Size before observe = %q, want 54x23 (layout rect)", got)
	}

	if _, _, _, err := b.Snapshot("w1:p1"); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	sessionsAfter, err := b.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions (after): %v", err)
	}
	if got := sessionsAfter[0].Windows[0].Panes[0].Size; got != "172x51" {
		t.Errorf("Size after observe = %q, want 172x51 (cached real pty size)", got)
	}
}

// --- socket path resolution ---

func TestDefaultHerdrSocketPathRespectsEnvOverride(t *testing.T) {
	old := os.Getenv("HERDR_SOCKET_PATH")
	defer os.Setenv("HERDR_SOCKET_PATH", old)

	os.Setenv("HERDR_SOCKET_PATH", "/custom/herdr.sock")
	if got := DefaultSocketPath(); got != "/custom/herdr.sock" {
		t.Errorf("DefaultSocketPath() = %q, want /custom/herdr.sock", got)
	}
}

func TestDefaultHerdrSocketPathUsesSessionSubdir(t *testing.T) {
	oldSock := os.Getenv("HERDR_SOCKET_PATH")
	oldSession := os.Getenv("HERDR_SESSION")
	defer func() {
		os.Setenv("HERDR_SOCKET_PATH", oldSock)
		os.Setenv("HERDR_SESSION", oldSession)
	}()

	os.Unsetenv("HERDR_SOCKET_PATH")
	os.Setenv("HERDR_SESSION", "mysession")
	got := DefaultSocketPath()
	want := filepath.Join(".config", "herdr", "sessions", "mysession", "herdr.sock")
	if len(got) < len(want) || got[len(got)-len(want):] != want {
		t.Errorf("DefaultSocketPath() = %q, want suffix %q", got, want)
	}
}
