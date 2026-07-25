package tmuxctl

import (
	"bytes"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/BambooTuna/tmuxui/internal/backend"
)

// TmuxControlBackend はtmux control mode(ControlSession)を用いたbackend.PaneBackend実装。
// セッション名ごとに1つのControlSessionを保持し、pane_id⇔targetの対応表を
// listSessions()の結果からSyncSessionsで更新する。
type TmuxControlBackend struct {
	mu                sync.Mutex
	sessions          map[string]*ControlSession // session name -> ControlSession (起動中はnilで予約)
	paneToTarget      map[string]string
	targetToPane      map[string]string
	subs              map[string]*backend.Broadcaster // target -> subscribers
	clientfulSessions map[string]struct{}             // 実クライアント(スマホ以外)がアタッチ中のセッション名

	onTopologyChange func()
}

var _ backend.PaneBackend = (*TmuxControlBackend)(nil)

func New() *TmuxControlBackend {
	return &TmuxControlBackend{
		sessions:          map[string]*ControlSession{},
		paneToTarget:      map[string]string{},
		targetToPane:      map[string]string{},
		subs:              map[string]*backend.Broadcaster{},
		clientfulSessions: map[string]struct{}{},
	}
}

func (b *TmuxControlBackend) ListSessions() ([]backend.Session, error) {
	return listSessions()
}

func (b *TmuxControlBackend) CapturePane(target string) (*backend.PaneContent, error) {
	return capturePane(target)
}

func (b *TmuxControlBackend) CapturePanePlain(target string) (string, error) {
	return capturePanePlain(target)
}

func (b *TmuxControlBackend) SendKeys(target, keys string) error {
	return sendKeys(target, keys)
}

func (b *TmuxControlBackend) NewSession(name, dir string) error {
	return newSession(name, dir)
}

func (b *TmuxControlBackend) KillSession(name string) error {
	return killSession(name)
}

func (b *TmuxControlBackend) RenameSession(oldName, newName string) error {
	return renameSession(oldName, newName)
}

func (b *TmuxControlBackend) NewWindow(sessionName, windowName string) error {
	return newWindow(sessionName, windowName)
}

func (b *TmuxControlBackend) KillWindow(target string) error {
	return killWindow(target)
}

func (b *TmuxControlBackend) RenameWindow(target, newName string) error {
	return renameWindow(target, newName)
}

func (b *TmuxControlBackend) KillPane(target string) error {
	return killPane(target)
}

func (b *TmuxControlBackend) SplitPane(target string, horizontal bool) error {
	return splitPane(target, horizontal)
}

func (b *TmuxControlBackend) ValidTarget(s string) bool {
	return isValidTarget(s)
}

func (b *TmuxControlBackend) SupportsTextPermissionDetection() bool {
	return true
}

func (b *TmuxControlBackend) OnTopologyChange(fn func()) {
	b.onTopologyChange = fn
}

// Snapshot は現在のpane状態を1回分のバイト列として合成する。cols/rowsは呼び出し側が
// termSetSize相当でクライアント側ターミナルをペインの実サイズに追従させるために使う。
func (b *TmuxControlBackend) Snapshot(target string) ([]byte, int, int, error) {
	infoOut, err := exec.Command("tmux", "display-message", "-p", "-t", target,
		"-F", "#{alternate_on} #{cursor_x} #{cursor_y} #{cursor_flag} #{pane_width} #{pane_height}").Output()
	if err != nil {
		return nil, 0, 0, err
	}
	fields := strings.Fields(strings.TrimSpace(string(infoOut)))
	if len(fields) < 6 {
		return nil, 0, 0, fmt.Errorf("unexpected display-message output: %q", infoOut)
	}
	altOn := fields[0] == "1"
	cursorX, _ := strconv.Atoi(fields[1])
	cursorY, _ := strconv.Atoi(fields[2])
	cursorFlag := fields[3] == "1"
	paneWidth, _ := strconv.Atoi(fields[4])
	paneHeight, _ := strconv.Atoi(fields[5])

	args := []string{"capture-pane", "-t", target, "-p", "-e", "-J"}
	if !altOn {
		args = append(args, "-S", "-"+strconv.Itoa(backend.SnapshotHistoryLines))
	}
	body, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return nil, 0, 0, err
	}

	var buf bytes.Buffer
	if altOn {
		buf.WriteString("\x1b[?1049h")
		buf.WriteString("\x1b[0m")
	}
	// capture-paneの行区切りは\nのみ。端末に書き込む際は\r\nでないと桁位置がずれる
	buf.Write(bytes.ReplaceAll(body, []byte("\n"), []byte("\r\n")))
	fmt.Fprintf(&buf, "\x1b[%d;%dH", cursorY+1, cursorX+1)
	if !cursorFlag {
		buf.WriteString("\x1b[?25l")
	}
	return buf.Bytes(), paneWidth, paneHeight, nil
}

// Subscribe は購読登録のみを行う。Snapshotの取得は呼び出し側(Hub)の責務。
func (b *TmuxControlBackend) Subscribe(target string) (<-chan []byte, func(), error) {
	ch := make(chan []byte, 256)
	once := &sync.Once{}

	b.mu.Lock()
	bc, ok := b.subs[target]
	if !ok {
		bc = backend.NewBroadcaster()
		b.subs[target] = bc
	}
	bc.Add(ch, once)
	b.mu.Unlock()

	// handleOutput側(chan満杯時)とcancel側の両方からcloseされうるため、
	// 実際のcloseはonceで一本化して二重closeによるpanicを防ぐ。
	cancel := func() {
		b.mu.Lock()
		if cur, ok := b.subs[target]; ok && cur == bc {
			cur.Remove(ch)
			if cur.Len() == 0 {
				delete(b.subs, target)
			}
		}
		once.Do(func() { close(ch) })
		b.mu.Unlock()
	}
	return ch, cancel, nil
}

// Resize はresize-windowでウィンドウサイズを明示指定する(そのウィンドウはmanualサイズになる)。
// ignore-size付きcontrol clientのrefresh-client -Cはウィンドウサイズに反映されないため使えない。
// ただし他に実クライアント(スマホ以外)がアタッチ中のセッションは、そちらの表示が乱れるため
// 何もせずnilを返す(resizePaneフォールバックも発動させない)。
// resize-windowが失敗した場合(対象ペインがウィンドウ内で分割されている等)はresizePaneにフォールバックする。
func (b *TmuxControlBackend) Resize(target string, cols, rows int) error {
	b.mu.Lock()
	_, hasRealClient := b.clientfulSessions[sessionNameFromTarget(target)]
	b.mu.Unlock()
	if hasRealClient {
		return nil
	}
	if err := exec.Command("tmux", "resize-window", "-t", target,
		"-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows)).Run(); err != nil {
		return resizePane(target, cols, rows)
	}
	return nil
}

func sessionNameFromTarget(target string) string {
	if i := strings.IndexByte(target, ':'); i >= 0 {
		return target[:i]
	}
	return target
}

// SyncSessions は一覧にあるがControlSession未起動のセッションを起動し、消えたセッションを閉じる。
// pane_id⇔targetの対応表もここで丸ごと更新する。
func (b *TmuxControlBackend) SyncSessions(sessions []backend.Session) {
	paneToTarget := map[string]string{}
	targetToPane := map[string]string{}
	seen := map[string]struct{}{}
	for _, s := range sessions {
		seen[s.Name] = struct{}{}
		for _, w := range s.Windows {
			for _, p := range w.Panes {
				if p.ID == "" {
					continue
				}
				paneToTarget[p.ID] = p.Target
				targetToPane[p.Target] = p.ID
			}
		}
	}

	clientful := sessionsWithRealClients()

	b.mu.Lock()
	b.paneToTarget = paneToTarget
	b.targetToPane = targetToPane
	b.clientfulSessions = clientful
	var toClose []*ControlSession
	for name, cs := range b.sessions {
		if _, ok := seen[name]; !ok {
			if cs != nil {
				toClose = append(toClose, cs)
			}
			delete(b.sessions, name)
		}
	}
	b.mu.Unlock()

	for name := range seen {
		b.startSession(name)
	}
	for _, cs := range toClose {
		// SyncSessionsはControlSessionのreadLoop起点のコールバックからも呼ばれるため、
		// 同期Closeだと自分自身の終了を待って永久ブロックしうる。非同期で閉じる
		go cs.Close()
	}
}

// startSession はnameのControlSessionが無ければ起動する。起動中の重複開始を防ぐため、
// プロセス生成前にnilで枠を予約してからmuを外す。
func (b *TmuxControlBackend) startSession(name string) {
	b.mu.Lock()
	if _, exists := b.sessions[name]; exists {
		b.mu.Unlock()
		return
	}
	b.sessions[name] = nil
	b.mu.Unlock()

	cs, err := newControlSession(name)
	if err != nil {
		log.Printf("controlmode: failed to attach session %q: %v", name, err)
		b.mu.Lock()
		delete(b.sessions, name)
		b.mu.Unlock()
		return
	}
	cs.onOutput = b.handleOutput
	cs.onTopologyChange = b.notifyTopologyChange
	cs.onExit = func() { b.handleExit(name, cs) }

	b.mu.Lock()
	b.sessions[name] = cs
	b.mu.Unlock()

	cs.Start()
}

// onExit時、Hub側の次回SyncSessionsが再起動するので自前のバックオフは持たない。
func (b *TmuxControlBackend) handleExit(name string, cs *ControlSession) {
	b.mu.Lock()
	if b.sessions[name] == cs {
		delete(b.sessions, name)
	}
	b.mu.Unlock()
}

func (b *TmuxControlBackend) notifyTopologyChange() {
	if b.onTopologyChange != nil {
		b.onTopologyChange()
	}
}

func (b *TmuxControlBackend) handleOutput(paneID string, data []byte) {
	b.mu.Lock()
	target, ok := b.paneToTarget[paneID]
	var bc *backend.Broadcaster
	if ok {
		bc = b.subs[target]
	}
	b.mu.Unlock()
	if !ok || bc == nil {
		return
	}

	bc.Publish(data)

	// overflowで購読者が0になった場合は、Broadcaster自体を外側のmapからも取り除く
	// (Publish/Removeはこのbc内部のロックのみで完結するため、外側map操作はここで別途行う)。
	if bc.Len() == 0 {
		b.mu.Lock()
		if cur, ok := b.subs[target]; ok && cur == bc {
			delete(b.subs, target)
		}
		b.mu.Unlock()
	}
}

// Close は管理下の全ControlSessionを終了する(主にテスト/シャットダウン用)。
func (b *TmuxControlBackend) Close() {
	b.mu.Lock()
	sessions := make([]*ControlSession, 0, len(b.sessions))
	for _, cs := range b.sessions {
		if cs != nil {
			sessions = append(sessions, cs)
		}
	}
	b.sessions = map[string]*ControlSession{}
	b.mu.Unlock()

	for _, cs := range sessions {
		cs.Close()
	}
}
