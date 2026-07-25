package backend

import (
	"errors"
	"reflect"
	"testing"
)

// fakeBackend is a minimal PaneBackend stand-in for registry tests that don't need real tmux.
type fakeBackend struct {
	sessions      []Session
	sessionsErr   error
	validTargetFn func(string) bool
	syncedWith    []Session // last argument passed to SyncSessions
}

var _ PaneBackend = (*fakeBackend)(nil)

func (f *fakeBackend) ListSessions() ([]Session, error)          { return f.sessions, f.sessionsErr }
func (f *fakeBackend) SyncSessions(sessions []Session)           { f.syncedWith = sessions }
func (f *fakeBackend) Snapshot(string) ([]byte, int, int, error) { return nil, 0, 0, nil }
func (f *fakeBackend) Subscribe(string) (<-chan []byte, func(), error) {
	return nil, func() {}, nil
}
func (f *fakeBackend) CapturePane(string) (*PaneContent, error) { return nil, nil }
func (f *fakeBackend) CapturePanePlain(string) (string, error)  { return "", nil }
func (f *fakeBackend) SendKeys(string, string) error            { return nil }
func (f *fakeBackend) Resize(string, int, int) error            { return nil }
func (f *fakeBackend) NewSession(string, string) error          { return nil }
func (f *fakeBackend) KillSession(string) error                 { return nil }
func (f *fakeBackend) RenameSession(string, string) error       { return nil }
func (f *fakeBackend) NewWindow(string, string) error           { return nil }
func (f *fakeBackend) KillWindow(string) error                  { return nil }
func (f *fakeBackend) RenameWindow(string, string) error        { return nil }
func (f *fakeBackend) KillPane(string) error                    { return nil }
func (f *fakeBackend) SplitPane(string, bool) error             { return nil }
func (f *fakeBackend) OnTopologyChange(func())                  {}
func (f *fakeBackend) ValidTarget(s string) bool {
	if f.validTargetFn != nil {
		return f.validTargetFn(s)
	}
	return s != ""
}
func (f *fakeBackend) SupportsTextPermissionDetection() bool { return true }

func TestBackendRegistryResolveWithPrefix(t *testing.T) {
	reg := NewBackendRegistry("tmux")
	tmuxBackend := &fakeBackend{}
	reg.Register("tmux", tmuxBackend)

	b, native, err := reg.Resolve("tmux:main:0.%1")
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if b != tmuxBackend {
		t.Fatalf("Resolve: got backend %v, want tmuxBackend", b)
	}
	if native != "main:0.%1" {
		t.Fatalf("Resolve: native = %q, want %q", native, "main:0.%1")
	}
}

func TestBackendRegistryResolveWithoutPrefixUsesFallback(t *testing.T) {
	reg := NewBackendRegistry("tmux")
	tmuxBackend := &fakeBackend{}
	reg.Register("tmux", tmuxBackend)

	b, native, err := reg.Resolve("main:0.%1")
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if b != tmuxBackend {
		t.Fatalf("Resolve: got backend %v, want tmuxBackend (fallback)", b)
	}
	if native != "main:0.%1" {
		t.Fatalf("Resolve: native = %q, want id unchanged %q", native, "main:0.%1")
	}
}

func TestBackendRegistryResolveUnknownPrefixUsesFallback(t *testing.T) {
	reg := NewBackendRegistry("tmux")
	tmuxBackend := &fakeBackend{}
	reg.Register("tmux", tmuxBackend)

	// "herdr" is not a registered prefix, so the whole id is treated as a
	// fallback-native identifier (back-compat with pre-prefix clients/URLs).
	b, native, err := reg.Resolve("herdr:main:0.%1")
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if b != tmuxBackend {
		t.Fatalf("Resolve: got backend %v, want tmuxBackend (fallback)", b)
	}
	if native != "herdr:main:0.%1" {
		t.Fatalf("Resolve: native = %q, want id unchanged %q", native, "herdr:main:0.%1")
	}
}

func TestBackendRegistryResolveEmptyID(t *testing.T) {
	reg := NewBackendRegistry("tmux")
	reg.Register("tmux", &fakeBackend{})

	if _, _, err := reg.Resolve(""); err == nil {
		t.Fatalf("Resolve(\"\"): expected error, got nil")
	}
}

func TestBackendRegistryResolveInvalidTarget(t *testing.T) {
	reg := NewBackendRegistry("tmux")
	tmuxBackend := &fakeBackend{validTargetFn: func(s string) bool { return false }}
	reg.Register("tmux", tmuxBackend)

	if _, _, err := reg.Resolve("tmux:bad target"); err == nil {
		t.Fatalf("Resolve: expected error for invalid target, got nil")
	}
	if _, _, err := reg.Resolve("bad target"); err == nil {
		t.Fatalf("Resolve: expected error for invalid fallback target, got nil")
	}
}

func TestBackendRegistryResolveFallbackNotRegistered(t *testing.T) {
	reg := NewBackendRegistry("tmux")
	// No backend registered at all, including the fallback.
	if _, _, err := reg.Resolve("main:0.%1"); err == nil {
		t.Fatalf("Resolve: expected error when fallback backend is not registered, got nil")
	}
}

func TestBackendRegistryListSessionsMerges(t *testing.T) {
	reg := NewBackendRegistry("tmux")

	tmuxSessions := []Session{{
		Name: "s1",
		Windows: []Window{{
			Index: 0,
			Panes: []Pane{{Target: "s1:0.0", ID: "%1"}},
		}},
	}}
	herdrSessions := []Session{{
		Name: "s2",
		Windows: []Window{{
			Index: 0,
			Panes: []Pane{{Target: "s2:0.0", ID: "%2"}},
		}},
	}}

	tmuxBackend := &fakeBackend{sessions: tmuxSessions}
	herdrBackend := &fakeBackend{sessions: herdrSessions}
	reg.Register("tmux", tmuxBackend)
	reg.Register("herdr", herdrBackend)

	merged, err := reg.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: unexpected error: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("ListSessions: got %d sessions, want 2", len(merged))
	}

	byName := map[string]Session{}
	for _, s := range merged {
		byName[s.Name] = s
	}

	s1, ok := byName["s1"]
	if !ok {
		t.Fatalf("ListSessions: missing session s1 in %+v", merged)
	}
	if s1.Backend != "tmux" {
		t.Errorf("s1.Backend = %q, want %q", s1.Backend, "tmux")
	}
	if got := s1.Windows[0].Panes[0].Target; got != "tmux:s1:0.0" {
		t.Errorf("s1 pane target = %q, want %q", got, "tmux:s1:0.0")
	}

	s2, ok := byName["s2"]
	if !ok {
		t.Fatalf("ListSessions: missing session s2 in %+v", merged)
	}
	if s2.Backend != "herdr" {
		t.Errorf("s2.Backend = %q, want %q", s2.Backend, "herdr")
	}
	if got := s2.Windows[0].Panes[0].Target; got != "herdr:s2:0.0" {
		t.Errorf("s2 pane target = %q, want %q", got, "herdr:s2:0.0")
	}

	// SyncSessions must be called with the native (unprefixed) list, not the merged/prefixed one.
	if !reflect.DeepEqual(tmuxBackend.syncedWith, tmuxSessions) {
		t.Errorf("tmuxBackend.SyncSessions called with %+v, want native %+v", tmuxBackend.syncedWith, tmuxSessions)
	}
	if !reflect.DeepEqual(herdrBackend.syncedWith, herdrSessions) {
		t.Errorf("herdrBackend.SyncSessions called with %+v, want native %+v", herdrBackend.syncedWith, herdrSessions)
	}
}

func TestBackendRegistryListSessionsPartialErrorStillReturnsOthers(t *testing.T) {
	reg := NewBackendRegistry("tmux")

	tmuxBackend := &fakeBackend{sessions: []Session{{Name: "s1"}}}
	herdrBackend := &fakeBackend{sessionsErr: errors.New("herdr socket unavailable")}
	reg.Register("tmux", tmuxBackend)
	reg.Register("herdr", herdrBackend)

	merged, err := reg.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: unexpected error when one backend fails but another succeeds: %v", err)
	}
	if len(merged) != 1 || merged[0].Name != "s1" {
		t.Fatalf("ListSessions: got %+v, want only s1", merged)
	}
}

func TestBackendRegistryListSessionsAllErrorsReturnsError(t *testing.T) {
	reg := NewBackendRegistry("tmux")

	wantErr := errors.New("tmux unavailable")
	tmuxBackend := &fakeBackend{sessionsErr: wantErr}
	reg.Register("tmux", tmuxBackend)

	if _, err := reg.ListSessions(); err == nil {
		t.Fatalf("ListSessions: expected error when the only backend fails, got nil")
	}
}

func TestBackendRegistryValidTarget(t *testing.T) {
	reg := NewBackendRegistry("tmux")
	reg.Register("tmux", &fakeBackend{})

	if !reg.ValidTarget("tmux:main:0.%1") {
		t.Errorf("ValidTarget(prefixed): got false, want true")
	}
	if !reg.ValidTarget("main:0.%1") {
		t.Errorf("ValidTarget(fallback): got false, want true")
	}
	if reg.ValidTarget("") {
		t.Errorf("ValidTarget(empty): got true, want false")
	}
}
