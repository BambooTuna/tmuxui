// Package shellws は「ブラウザSSH」用のPTY↔WebSocketブリッジ。
// tmuxuiが動いているホスト上でユーザーのログインシェルをPTYで起動し、
// WebSocketの生バイト双方向で受け渡す。セッションの永続化はしない
// (herdr/tmux側で担う前提)。
package shellws

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

// POSIX ユーザー名 + サロゲート的な範囲。sudo -u に渡す前の sanity check。
var userNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		h := u.Hostname()
		if h == "127.0.0.1" || h == "localhost" {
			return true
		}
		reqHost := r.Host
		if i := strings.LastIndex(reqHost, ":"); i >= 0 {
			reqHost = reqHost[:i]
		}
		return h == reqHost
	},
}

// Handler は "/ws/shell" を受ける http.HandlerFunc を返す。
// クエリ: ?user=<name>&shell=<path>
// user 指定時は sudo -n -u <user> -H で切り替える(sudo設定必須、パスワード対話は不許可)。
// shell 未指定時は $SHELL → bash → /bin/sh の順で解決する。
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := r.URL.Query().Get("user")
		shell := r.URL.Query().Get("shell")
		if user != "" && !userNameRe.MatchString(user) {
			http.Error(w, "invalid user", http.StatusBadRequest)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		serve(conn, user, shell)
	}
}

func resolveShell(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	return "/bin/sh"
}

func serve(conn *websocket.Conn, targetUser, explicitShell string) {
	shell := resolveShell(explicitShell)

	var cmd *exec.Cmd
	if targetUser != "" {
		// -n: 非対話。パスワード要求で即失敗させる(WSからパスワード入力を扱わないため)
		// -H: HOMEを切替先ユーザーのものに設定
		cmd = exec.Command("sudo", "-n", "-u", targetUser, "-H", shell, "-l")
	} else {
		cmd = exec.Command(shell, "-l")
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}

	ptyFile, err := pty.Start(cmd)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"err","d":"pty start failed"}`))
		return
	}

	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			ptyFile.Close()
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			cmd.Wait()
		})
	}
	defer cleanup()

	// PTY → WS: 生バイトをそのままBinaryで送る
	go func() {
		defer cleanup()
		buf := make([]byte, 32*1024)
		for {
			n, err := ptyFile.Read(buf)
			if n > 0 {
				if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WS → PTY: Binaryは生入力、TextはJSON制御 (resize等)
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		switch msgType {
		case websocket.BinaryMessage:
			if _, err := ptyFile.Write(data); err != nil {
				return
			}
		case websocket.TextMessage:
			var ctrl struct {
				T    string `json:"t"`
				Cols int    `json:"c"`
				Rows int    `json:"r"`
				Data string `json:"d"`
			}
			if err := json.Unmarshal(data, &ctrl); err != nil {
				continue
			}
			switch ctrl.T {
			case "resize":
				if ctrl.Cols > 0 && ctrl.Rows > 0 {
					if err := pty.Setsize(ptyFile, &pty.Winsize{
						Cols: uint16(ctrl.Cols),
						Rows: uint16(ctrl.Rows),
					}); err != nil {
						log.Printf("shellws: resize failed: %v", err)
					}
				}
			case "in":
				if _, err := ptyFile.Write([]byte(ctrl.Data)); err != nil {
					return
				}
			}
		}
	}
}

