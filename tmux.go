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
	Title  string `json:"title"`
	Cmd    string `json:"cmd"`
	Size   string `json:"size"`
	Path   string `json:"path"`
}

type Window struct {
	Index  int    `json:"index"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
	Panes  []Pane `json:"panes"`
}

type Session struct {
	Name     string   `json:"name"`
	Attached bool     `json:"attached"`
	Windows  []Window `json:"windows"`
}

type PaneContent struct {
	Target  string `json:"target"`
	Content string `json:"content"`
	Lines   int    `json:"lines"`
	Ts      int64  `json:"ts"`
}

func listSessions() ([]Session, error) {
	sessOut, err := exec.Command("tmux", "list-sessions",
		"-F", "#{session_name}\t#{session_attached}").Output()
	if err != nil {
		return nil, err
	}

	type sessEntry struct {
		attached bool
	}
	sessMap := map[string]*sessEntry{}
	sessOrder := []string{}

	for _, line := range strings.Split(strings.TrimSpace(string(sessOut)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		sessMap[parts[0]] = &sessEntry{attached: parts[1] == "1"}
		sessOrder = append(sessOrder, parts[0])
	}

	paneOut, err := exec.Command("tmux", "list-panes", "-a",
		"-F", "#{session_name}\t#{window_index}\t#{window_id}\t#{window_name}\t#{window_active}\t#{pane_index}\t#{pane_current_command}\t#{pane_width}\t#{pane_height}\t#{pane_current_path}\t#{pane_title}").Output()
	if err != nil {
		return nil, err
	}

	type winKey struct {
		session string
		index   int
	}
	type winEntry struct {
		id     string
		name   string
		active bool
		panes  []Pane
	}
	winMap := map[winKey]*winEntry{}
	winOrder := map[string][]int{}

	for _, line := range strings.Split(strings.TrimSpace(string(paneOut)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 11)
		if len(parts) < 10 {
			continue
		}
		sessName := parts[0]
		winIdx, _ := strconv.Atoi(parts[1])
		paneTitle := ""
		if len(parts) >= 11 {
			paneTitle = parts[10]
		}
		target := fmt.Sprintf("%s:%d.%s", sessName, winIdx, parts[5])
		size := fmt.Sprintf("%sx%s", parts[7], parts[8])
		pane := Pane{Target: target, Title: paneTitle, Cmd: parts[6], Size: size, Path: parts[9]}

		key := winKey{session: sessName, index: winIdx}
		if _, ok := winMap[key]; !ok {
			winMap[key] = &winEntry{id: parts[2], name: parts[3], active: parts[4] == "1"}
			winOrder[sessName] = append(winOrder[sessName], winIdx)
		}
		winMap[key].panes = append(winMap[key].panes, pane)
	}

	sessions := make([]Session, 0, len(sessOrder))
	for _, sessName := range sessOrder {
		e := sessMap[sessName]
		windows := make([]Window, 0)
		for _, winIdx := range winOrder[sessName] {
			key := winKey{session: sessName, index: winIdx}
			we := winMap[key]
			windows = append(windows, Window{
				Index:  winIdx,
				ID:     we.id,
				Name:   we.name,
				Active: we.active,
				Panes:  we.panes,
			})
		}
		sessions = append(sessions, Session{
			Name:     sessName,
			Attached: e.attached,
			Windows:  windows,
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

func newWindow(sessionName, windowName string) error {
	args := []string{"new-window", "-t", sessionName}
	if windowName != "" {
		args = append(args, "-n", windowName)
	}
	return exec.Command("tmux", args...).Run()
}

func killWindow(target string) error {
	return exec.Command("tmux", "kill-window", "-t", target).Run()
}

func renameWindow(target, newName string) error {
	return exec.Command("tmux", "rename-window", "-t", target, newName).Run()
}

func killPane(target string) error {
	return exec.Command("tmux", "kill-pane", "-t", target).Run()
}

func splitPane(target string, horizontal bool) error {
	args := []string{"split-window", "-t", target}
	if horizontal {
		args = append(args, "-h")
	}
	return exec.Command("tmux", args...).Run()
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
