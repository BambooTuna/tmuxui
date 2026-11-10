package hub

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

// terminalタブが使うtmuxセッション名。/ws/ptyはこのセッションへのattach専用
// (任意セッション名を受けると既存セッションのオプションを書き換えられてしまう)。
const ptySessionName = "webterm"

// HandlePtyWS はterminalタブ専用のWSエンドポイント。ptyの中で
// `tmux attach` を走らせ、ptyのバイト列をWSと素通しする。
// 画面の再描画・全端末モード復元・リサイズはattachしたtmux自身が行うため、
// control-mode購読+Snapshot合成(自前の端末状態再現)が抱えるズレの問題が起きない。
//
// プロトコル(ttyd等と同型の最小構成):
//   client→server: TextMessage  JSON {"type":"resize","cols":N,"rows":N}
//   client→server: BinaryMessage 生バイト(キー入力/マウスレポート) → ptyへ書く
//   server→client: BinaryMessage ptyからの出力
func (h *Hub) HandlePtyWS(w http.ResponseWriter, r *http.Request) {
	if s := r.URL.Query().Get("session"); s != "" && s != ptySessionName {
		http.Error(w, "unsupported session", http.StatusBadRequest)
		return
	}
	session := ptySessionName

	// 先にdetachedで作成(既存ならエラーを無視)し、このセッションに限り
	// tmuxを「透明なパイプ」にする設定を当ててからattachする。
	// ユーザーの.tmux.confのprefix(C-a)やbind -n(C-h/j/k/l等)が
	// 中で動くherdrへのキーを横取りしないようにするため。
	// いずれもsession option。mouse offはtmuxがマウスを消費しないだけで、
	// pane内アプリへのマウスシーケンス転送は生きる(SGRのタップ転送に必要)。
	exec.Command("tmux", "new-session", "-d", "-s", session).Run()
	for _, kv := range [][]string{
		{"prefix", "None"},       // C-aを素通し
		{"prefix2", "None"},      // 第2prefixも素通し
		{"key-table", "off"},     // bind -n(C-h/j/k/l等)を全て素通し
		{"status", "off"},        // status barを消しherdrに全行渡す
		{"mouse", "off"},         // tmuxがマウスを食わずpaneのアプリへ転送
		{"set-clipboard", "on"},  // herdr/paneアプリが出すOSC52をクライアント(xterm)へ転送する。
		//                           ユーザーの.tmux.confはmosh対策でoffだが、ここはWS直結なのでon。
	} {
		if err := exec.Command("tmux", "set-option", "-t", session, kv[0], kv[1]).Run(); err != nil {
			log.Printf("ptyws[%s]: set-option %s failed: %v", session, kv[0], err)
			http.Error(w, "tmux setup failed", http.StatusInternalServerError)
			return
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	cmd := exec.Command("tmux", "attach-session", "-t", session)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Printf("ptyws[%s]: pty start failed: %v", session, err)
		return
	}
	// WS読取が終わったら先にptyを閉じてreader goroutineのptmx.Readを解除する。
	// (reader終了を待ってから閉じる順だと、tmuxが自発終了しない限り
	//  reader=Read待ち/handler=done待ち/close=handler待ち のデッドロックになる)
	defer func() {
		ptmx.Close()
		cmd.Wait() // attachクライアント(このpty)を失ったtmuxは終了する
	}()

	var writeMu sync.Mutex // gorilla/websocketは並行Writeを許さない

	// pty → WS
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				writeMu.Lock()
				werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n])
				writeMu.Unlock()
				if werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// 接続維持ping(iPadOSの中間機器によるアイドル切断対策)
	stopPing := make(chan struct{})
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				writeMu.Lock()
				conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
				writeMu.Unlock()
			case <-stopPing:
				return
			}
		}
	}()
	defer close(stopPing)

	// WS → pty
readLoop:
	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		switch mt {
		case websocket.BinaryMessage:
			if _, err := ptmx.Write(data); err != nil {
				break readLoop
			}
		case websocket.TextMessage:
			var msg struct {
				Type string `json:"type"`
				Cols uint16 `json:"cols"`
				Rows uint16 `json:"rows"`
			}
			if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 {
				pty.Setsize(ptmx, &pty.Winsize{Cols: msg.Cols, Rows: msg.Rows})
			}
		}
	}
	// ptyを閉じてreaderを解除してから終了を待つ(deferの順序で保証)
	ptmx.Close()
	<-done
}
