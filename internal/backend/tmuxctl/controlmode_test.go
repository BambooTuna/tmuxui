package tmuxctl

import (
	"crypto/rand"
	"encoding/hex"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func randomTestSessionName(t *testing.T, prefix string) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return prefix + hex.EncodeToString(b)
}

func TestControlSessionReceivesOutput(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	session := randomTestSessionName(t, "tmuxui_ctrltest_")
	if err := exec.Command("tmux", "new-session", "-d", "-s", session, "-x", "80", "-y", "24").Run(); err != nil {
		t.Fatalf("failed to create tmux session: %v", err)
	}
	defer exec.Command("tmux", "kill-session", "-t", session).Run()

	cs, err := newControlSession(session)
	if err != nil {
		t.Fatalf("newControlSession: %v", err)
	}

	received := make(chan []byte, 256)
	cs.onOutput = func(paneID string, data []byte) {
		if paneID == "" {
			t.Errorf("onOutput called with empty paneID")
		}
		received <- append([]byte(nil), data...)
	}
	cs.Start()

	// attachが完了しシェルプロンプトが出るまでの猶予
	time.Sleep(300 * time.Millisecond)

	if err := exec.Command("tmux", "send-keys", "-t", session, "echo hello-ctrltest", "Enter").Run(); err != nil {
		t.Fatalf("send-keys: %v", err)
	}

	var all []byte
	found := false
	deadline := time.After(5 * time.Second)
loop:
	for {
		select {
		case d := <-received:
			all = append(all, d...)
			if strings.Contains(string(all), "hello-ctrltest") {
				found = true
				break loop
			}
		case <-deadline:
			break loop
		}
	}
	if !found {
		t.Fatalf("did not receive %%output containing hello-ctrltest, got: %q", all)
	}

	pid := cs.cmd.Process.Pid
	cs.Close()

	if cs.cmd.ProcessState == nil {
		t.Fatalf("process %d state not set after Close (may still be running)", pid)
	}
	if !cs.cmd.ProcessState.Exited() {
		t.Fatalf("process %d did not exit after Close", pid)
	}
}
