// Package tmuxctl implements backend.PaneBackend using tmux control mode
// (`tmux -C attach-session`) plus plain `tmux` CLI invocations for CRUD operations.
package tmuxctl

import (
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/BambooTuna/tmuxui/internal/backend"
)

var validTarget = regexp.MustCompile(`^[a-zA-Z0-9_:.\-]+$`)

func isValidTarget(s string) bool {
	return s != "" && validTarget.MatchString(s)
}

func listSessions() ([]backend.Session, error) {
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
		"-F", "#{session_name}\t#{window_index}\t#{window_id}\t#{window_name}\t#{window_active}\t#{pane_index}\t#{pane_current_command}\t#{pane_width}\t#{pane_height}\t#{pane_current_path}\t#{pane_id}\t#{pane_title}").Output()
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
		panes  []backend.Pane
	}
	winMap := map[winKey]*winEntry{}
	winOrder := map[string][]int{}

	for _, line := range strings.Split(strings.TrimSpace(string(paneOut)), "\n") {
		if line == "" {
			continue
		}
		// pane_idを#{pane_title}の手前に挿入しているため、末尾のtitleフィールド(タブを含みうる残り全部)より前のインデックスは変わらない
		parts := strings.SplitN(line, "\t", 12)
		if len(parts) < 11 {
			continue
		}
		sessName := parts[0]
		winIdx, _ := strconv.Atoi(parts[1])
		paneID := parts[10]
		paneTitle := ""
		if len(parts) >= 12 {
			paneTitle = parts[11]
		}
		target := fmt.Sprintf("%s:%d.%s", sessName, winIdx, parts[5])
		size := fmt.Sprintf("%sx%s", parts[7], parts[8])
		pane := backend.Pane{Target: target, ID: paneID, Title: paneTitle, Cmd: parts[6], Size: size, Path: parts[9]}

		key := winKey{session: sessName, index: winIdx}
		if _, ok := winMap[key]; !ok {
			winMap[key] = &winEntry{id: parts[2], name: parts[3], active: parts[4] == "1"}
			winOrder[sessName] = append(winOrder[sessName], winIdx)
		}
		winMap[key].panes = append(winMap[key].panes, pane)
	}

	sessions := make([]backend.Session, 0, len(sessOrder))
	for _, sessName := range sessOrder {
		e := sessMap[sessName]
		windows := make([]backend.Window, 0)
		for _, winIdx := range winOrder[sessName] {
			key := winKey{session: sessName, index: winIdx}
			we := winMap[key]
			windows = append(windows, backend.Window{
				Index:  winIdx,
				ID:     we.id,
				Name:   we.name,
				Active: we.active,
				Panes:  we.panes,
			})
		}
		sessions = append(sessions, backend.Session{
			Name:     sessName,
			Attached: e.attached,
			Windows:  windows,
		})
	}
	return sessions, nil
}

func capturePane(target string) (*backend.PaneContent, error) {
	out, err := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-e", "-S", "-"+strconv.Itoa(backend.SnapshotHistoryLines)).Output()
	if err != nil {
		return nil, err
	}
	content := string(out)
	return &backend.PaneContent{
		Target:  target,
		Content: content,
		Lines:   strings.Count(content, "\n"),
		Ts:      time.Now().Unix(),
	}, nil
}

// detectPermission用の軽量キャプチャ。可視画面のみでANSIエスケープも含めない。
func capturePanePlain(target string) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-p", "-t", target).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// sessionsWithRealClients は control-mode 以外の実クライアント(スマホ専用ではないセッション)が
// アタッチしているセッション名の集合を返す。クライアントが1つもいない場合、tmuxは非ゼロ終了する
// ことがあるためエラーは空集合として扱う。
func sessionsWithRealClients() map[string]struct{} {
	result := map[string]struct{}{}
	out, err := exec.Command("tmux", "list-clients", "-F", "#{client_session}\t#{client_flags}").Output()
	if err != nil {
		return result
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		if !strings.Contains(parts[1], "control-mode") {
			result[parts[0]] = struct{}{}
		}
	}
	return result
}

func resizePane(target string, cols, rows int) error {
	return exec.Command("tmux", "resize-pane", "-t", target, "-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows)).Run()
}

func newSession(name, dir string) error {
	// Claude Codeのfullscreen(alternate screen)モードではtmuxのhistoryに履歴が残らず
	// capture-paneで遡れないため、tmuxui発のセッションはclassicレンダラーに固定する
	args := []string{"new-session", "-d", "-s", name, "-e", "CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN=1"}
	if dir != "" {
		args = append(args, "-c", dir)
	}
	if err := exec.Command("tmux", args...).Run(); err != nil {
		return err
	}
	// デフォルトのhistory-limit(2000)のままだとSnapshotHistoryLines分を遡れないため、
	// このセッションのみ引き上げる(-gはユーザーの他セッションにも影響するため使わない)。
	// セッション自体は既に作成済みのため、ここが失敗してもエラーにはせずログのみに留める。
	if err := exec.Command("tmux", "set-option", "-t", name, "history-limit", strconv.Itoa(backend.SnapshotHistoryLines)).Run(); err != nil {
		log.Printf("newSession: failed to raise history-limit for %q: %v", name, err)
	}
	return nil
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
