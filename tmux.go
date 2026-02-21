package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Pane struct {
	Target string `json:"target"`
	Cmd    string `json:"cmd"`
	Size   string `json:"size"`
	Path   string `json:"path"`
}

type Session struct {
	Name     string `json:"name"`
	Windows  int    `json:"windows"`
	Attached bool   `json:"attached"`
	Panes    []Pane `json:"panes"`
}

type PaneContent struct {
	Target  string `json:"target"`
	Content string `json:"content"`
	Lines   int    `json:"lines"`
	Ts      int64  `json:"ts"`
}

func listSessions() ([]Session, error) {
	sessOut, err := exec.Command("tmux", "list-sessions",
		"-F", "#{session_name}\t#{session_windows}\t#{session_attached}").Output()
	if err != nil {
		return nil, err
	}

	type sessEntry struct {
		windows  int
		attached bool
	}
	sessMap := map[string]*sessEntry{}
	sessOrder := []string{}

	for _, line := range strings.Split(strings.TrimSpace(string(sessOut)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		windows, _ := strconv.Atoi(parts[1])
		sessMap[parts[0]] = &sessEntry{windows: windows, attached: parts[2] == "1"}
		sessOrder = append(sessOrder, parts[0])
	}

	paneOut, err := exec.Command("tmux", "list-panes", "-a",
		"-F", "#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_current_command}\t#{pane_width}\t#{pane_height}\t#{pane_current_path}").Output()
	if err != nil {
		return nil, err
	}

	panesMap := map[string][]Pane{}
	for _, line := range strings.Split(strings.TrimSpace(string(paneOut)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 7)
		if len(parts) < 7 {
			continue
		}
		target := fmt.Sprintf("%s:%s.%s", parts[0], parts[1], parts[2])
		size := fmt.Sprintf("%sx%s", parts[4], parts[5])
		panesMap[parts[0]] = append(panesMap[parts[0]], Pane{Target: target, Cmd: parts[3], Size: size, Path: parts[6]})
	}

	sessions := make([]Session, 0, len(sessOrder))
	for _, name := range sessOrder {
		e := sessMap[name]
		panes := panesMap[name]
		if panes == nil {
			panes = []Pane{}
		}
		sessions = append(sessions, Session{
			Name:     name,
			Windows:  e.windows,
			Attached: e.attached,
			Panes:    panes,
		})
	}
	return sessions, nil
}

func capturePane(target string) (*PaneContent, error) {
	out, err := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-e", "-S", "-200").Output()
	if err != nil {
		return nil, err
	}
	content := string(out)
	return &PaneContent{
		Target:  target,
		Content: content,
		Lines:   strings.Count(content, "\n"),
		Ts:      time.Now().Unix(),
	}, nil
}

func resizePane(target string, cols, rows int) error {
	return exec.Command("tmux", "resize-pane", "-t", target, "-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows)).Run()
}

func newSession(name, dir string) error {
	args := []string{"new-session", "-d", "-s", name}
	if dir != "" {
		args = append(args, "-c", dir)
	}
	return exec.Command("tmux", args...).Run()
}

func killSession(name string) error {
	return exec.Command("tmux", "kill-session", "-t", name).Run()
}

func renameSession(oldName, newName string) error {
	return exec.Command("tmux", "rename-session", "-t", oldName, newName).Run()
}

func sendKeys(target, keys string) error {
	// 長文や改行含むテキストはload-buffer + paste-bufferで送る
	if strings.Contains(keys, "\n") && len(keys) > 1 {
		// 末尾の\nを分離（paste後にEnterで送信）
		endsWithNewline := strings.HasSuffix(keys, "\n")
		text := strings.TrimSuffix(keys, "\n")

		cmd := exec.Command("tmux", "load-buffer", "-")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err != nil {
			return err
		}
		if err := exec.Command("tmux", "paste-buffer", "-t", target).Run(); err != nil {
			return err
		}
		if endsWithNewline {
			return exec.Command("tmux", "send-keys", "-t", target, "Enter").Run()
		}
		return nil
	}

	args := []string{"send-keys", "-t", target}
	if strings.HasSuffix(keys, "\n") {
		trimmed := strings.TrimSuffix(keys, "\n")
		if trimmed != "" {
			args = append(args, trimmed)
		}
		args = append(args, "Enter")
	} else {
		args = append(args, keys)
	}
	return exec.Command("tmux", args...).Run()
}
