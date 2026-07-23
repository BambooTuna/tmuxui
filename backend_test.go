package main

import (
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startSimulatedRealClient はscript(1)でptyを介してtmux attachし、control-mode以外の
// 実クライアントを模擬する。stdinがすぐEOFになるとscriptが即終了しclientがdetachして
// しまうため、決してEOFにならないstdin(sleepのprocess substitution)を与える。
// 呼び出し側はdeferでcleanup()を呼ぶこと。
func startSimulatedRealClient(t *testing.T, session string) (cleanup func()) {
	t.Helper()
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script(1) not installed")
	}
	cmd := exec.Command("bash", "-c", "script -q /dev/null tmux attach -t "+session+" < <(sleep 999999)")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start simulated real client: %v", err)
	}
	return func() {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Wait()
	}
}

// waitForRealClient はsessionsWithRealClients()がsessionを検知するまで待つ。
func waitForRealClient(t *testing.T, session string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := sessionsWithRealClients()[session]; ok {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("real client on session %q was not detected within timeout", session)
}

func TestTmuxControlBackendSubscribeAndSnapshot(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	session := randomTestSessionName(t, "tmuxui_backendtest_")
	if err := exec.Command("tmux", "new-session", "-d", "-s", session, "-x", "80", "-y", "24").Run(); err != nil {
		t.Fatalf("failed to create tmux session: %v", err)
	}
	defer exec.Command("tmux", "kill-session", "-t", session).Run()

	backend := newTmuxControlBackend()
	defer backend.Close()

	sessions, err := listSessions()
	if err != nil {
		t.Fatalf("listSessions: %v", err)
	}
	backend.SyncSessions(sessions)

	var target string
	for _, s := range sessions {
		if s.Name != session {
			continue
		}
		for _, w := range s.Windows {
			for _, p := range w.Panes {
				target = p.Target
			}
		}
	}
	if target == "" {
		t.Fatalf("could not find target for session %q in %+v", session, sessions)
	}

	// SyncSessionsで起動したControlSessionがattachし終わるまでの猶予
	time.Sleep(300 * time.Millisecond)

	stream, cancel, err := backend.Subscribe(target)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, cols, rows, err := backend.Snapshot(target); err != nil {
		t.Fatalf("Snapshot: %v", err)
	} else if cols <= 0 || rows <= 0 {
		t.Fatalf("Snapshot returned invalid size: cols=%d rows=%d", cols, rows)
	}

	if err := exec.Command("tmux", "send-keys", "-t", target, "echo hello-backendtest", "Enter").Run(); err != nil {
		t.Fatalf("send-keys: %v", err)
	}

	var all []byte
	found := false
	deadline := time.After(5 * time.Second)
loop:
	for {
		select {
		case data, ok := <-stream:
			if !ok {
				break loop
			}
			all = append(all, data...)
			if strings.Contains(string(all), "hello-backendtest") {
				found = true
				break loop
			}
		case <-deadline:
			break loop
		}
	}
	if !found {
		t.Fatalf("did not receive expected output via backend Subscribe channel, got: %q", all)
	}
}

func TestTmuxControlBackendSupportsTextPermissionDetectionIsTrue(t *testing.T) {
	backend := newTmuxControlBackend()
	defer backend.Close()
	if !backend.SupportsTextPermissionDetection() {
		t.Error("TmuxControlBackend.SupportsTextPermissionDetection() = false, want true")
	}
}

func TestSessionsWithRealClients(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	session := randomTestSessionName(t, "tmuxui_realclienttest_")
	if err := exec.Command("tmux", "new-session", "-d", "-s", session, "-x", "80", "-y", "24").Run(); err != nil {
		t.Fatalf("failed to create tmux session: %v", err)
	}
	defer exec.Command("tmux", "kill-session", "-t", session).Run()

	if _, ok := sessionsWithRealClients()[session]; ok {
		t.Fatalf("session %q unexpectedly already has a real client", session)
	}

	cleanup := startSimulatedRealClient(t, session)
	defer cleanup()
	waitForRealClient(t, session)
}

// TestTmuxControlBackendResizeSkippedWithRealClient は「ペインの実サイズが正」方針の中核:
// 実クライアントがいるセッションはResize要求を無視し、ウィンドウサイズが変わらないことを検証する。
func TestTmuxControlBackendResizeSkippedWithRealClient(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	session := randomTestSessionName(t, "tmuxui_resizetest_")
	if err := exec.Command("tmux", "new-session", "-d", "-s", session, "-x", "80", "-y", "24").Run(); err != nil {
		t.Fatalf("failed to create tmux session: %v", err)
	}
	defer exec.Command("tmux", "kill-session", "-t", session).Run()

	if err := exec.Command("tmux", "resize-window", "-t", session, "-x", "150", "-y", "30").Run(); err != nil {
		t.Fatalf("resize-window: %v", err)
	}

	cleanup := startSimulatedRealClient(t, session)
	defer cleanup()
	waitForRealClient(t, session)

	backend := newTmuxControlBackend()
	defer backend.Close()

	sessions, err := listSessions()
	if err != nil {
		t.Fatalf("listSessions: %v", err)
	}
	backend.SyncSessions(sessions)

	var target string
	for _, s := range sessions {
		if s.Name != session {
			continue
		}
		for _, w := range s.Windows {
			for _, p := range w.Panes {
				target = p.Target
			}
		}
	}
	if target == "" {
		t.Fatalf("could not find target for session %q", session)
	}

	if err := backend.Resize(target, 40, 20); err != nil {
		t.Fatalf("Resize returned error, want nil (no-op when real client present): %v", err)
	}

	out, err := exec.Command("tmux", "list-windows", "-t", session, "-F", "#{window_width}x#{window_height}").Output()
	if err != nil {
		t.Fatalf("list-windows: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "150x30" {
		t.Fatalf("window was resized despite real client present: got %q, want 150x30", got)
	}
}
