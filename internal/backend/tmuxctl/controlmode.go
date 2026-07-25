package tmuxctl

import (
	"bufio"
	"io"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/BambooTuna/tmuxui/internal/backend"
)

// ControlSession は tmux control mode (`tmux -C attach-session`) を1プロセス起動し、
// そのイベントストリームをコールバックへ配送する。onOutput/onTopologyChange/onExit は
// Start() を呼ぶ前にフィールドへ設定すること(読み取りgoroutine開始前なのでデータ競合しない)。
type ControlSession struct {
	session string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.Reader
	done    chan struct{}

	onOutput         func(paneID string, data []byte)
	onTopologyChange func()
	onExit           func()
}

// newControlSession はtmuxプロセスを起動するのみで、読み取りgoroutineはStart()まで開始しない。
func newControlSession(session string) (*ControlSession, error) {
	// -CC(2つ)はパイプstdinだとtcgetattrで即死するため、必ず-C 1つを使う。
	// read-onlyは付けない: このクライアントが唯一のアタッチクライアントの場合、tmuxは
	// 外部からのsend-keys/resize-window等も"client is read-only"で拒否してしまう
	// (実機確認済み)。本セッションはstdinへコマンドを書き込まないため安全性上の意味もない。
	// pause-afterも付けない: フロー制御でペインがpaused状態になると、read-onlyと同様に
	// 外部からのsend-keys/resize-windowが拒否される(実機確認済み、continue復帰も未実装のため
	// 一度pauseすると回復しない)。%extended-outputではなく%outputで届く形式に戻るが、
	// handleLineは両方に対応済み。
	cmd := exec.Command("tmux", "-C", "attach-session", "-t", session, "-f", "ignore-size")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &ControlSession{
		session: session,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		done:    make(chan struct{}),
	}, nil
}

func (cs *ControlSession) Start() {
	go cs.readLoop()
}

func (cs *ControlSession) readLoop() {
	defer close(cs.done)
	defer backend.RecoverAndLog("tmuxctl.ControlSession.readLoop")
	// 巨大な%output行(1行が数十KBになりうる)にbufio.Scannerの64KB上限では耐えられないため、
	// ReadStringで無制限に読む。
	reader := bufio.NewReader(cs.stdout)
	inBlock := false
	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if line != "" {
			cs.handleLine(line, &inBlock)
		}
		if err != nil {
			break
		}
	}
	if cs.onExit != nil {
		cs.onExit()
	}
}

func (cs *ControlSession) handleLine(line string, inBlock *bool) {
	if *inBlock {
		// %begin～%end/%errorの本文はコマンド応答(fire-and-forgetのため不要)なので読み飛ばす
		switch {
		case strings.HasPrefix(line, "%end"):
			*inBlock = false
		case strings.HasPrefix(line, "%error"):
			*inBlock = false
			log.Printf("controlmode[%s]: command error: %s", cs.session, line)
		}
		return
	}
	switch {
	case strings.HasPrefix(line, "%output "):
		cs.handleOutput(line[len("%output "):])
	case strings.HasPrefix(line, "%extended-output "):
		cs.handleExtendedOutput(line[len("%extended-output "):])
	case strings.HasPrefix(line, "%exit"):
		// プロセス終了はreadLoopがEOFを検知した時点でonExitを呼ぶので、ここでは何もしない
	case strings.HasPrefix(line, "%begin"):
		*inBlock = true
	case strings.HasPrefix(line, "%"):
		// %window-add/%window-close/%layout-change/%window-renamed/%unlinked-window-add/
		// %session-changed等、トポロジー変化系はまとめて「topology changed」として通知する
		if cs.onTopologyChange != nil {
			cs.onTopologyChange()
		}
	}
}

func (cs *ControlSession) handleOutput(rest string) {
	sp := strings.IndexByte(rest, ' ')
	if sp < 0 {
		return
	}
	paneID := rest[:sp]
	data := decodeControlModeData(rest[sp+1:])
	if cs.onOutput != nil {
		cs.onOutput(paneID, data)
	}
}

// pause-after flagを付けるとtmuxは%outputの代わりに%extended-outputで送ってくる(実機確認済み、
// man tmuxにも"New form of %output sent when the pause-after flag is set"と明記)。
// 形式: "%extended-output <pane-id> <age> ... : <value>"。ageや将来拡張分は無視する。
func (cs *ControlSession) handleExtendedOutput(rest string) {
	idx := strings.Index(rest, " : ")
	if idx < 0 {
		return
	}
	header := rest[:idx]
	value := rest[idx+len(" : "):]
	paneID := header
	if sp := strings.IndexByte(header, ' '); sp >= 0 {
		paneID = header[:sp]
	}
	data := decodeControlModeData(value)
	if cs.onOutput != nil {
		cs.onOutput(paneID, data)
	}
}

// decodeControlModeData: ASCII 32未満のバイトと`\`は\ooo(8進3桁)にエスケープされている。
// それ以外は生バイトのまま。
func decodeControlModeData(s string) []byte {
	b := []byte(s)
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == '\\' && i+3 < len(b) && isOctalDigit(b[i+1]) && isOctalDigit(b[i+2]) && isOctalDigit(b[i+3]) {
			v := int(b[i+1]-'0')*64 + int(b[i+2]-'0')*8 + int(b[i+3]-'0')
			out = append(out, byte(v))
			i += 3
			continue
		}
		out = append(out, b[i])
	}
	return out
}

func isOctalDigit(c byte) bool {
	return c >= '0' && c <= '7'
}

// Close はstdinを閉じて正常終了を待ち、猶予時間内に終了しなければkillする。goroutineリークを防ぐため
// readLoopの終了(cs.done close)を必ず待ってから返る。
func (cs *ControlSession) Close() {
	cs.stdin.Close()
	select {
	case <-cs.done:
	case <-time.After(1 * time.Second):
		cs.cmd.Process.Kill()
		<-cs.done
	}
	cs.cmd.Wait()
}
