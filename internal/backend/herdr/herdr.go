// Package herdr implements backend.PaneBackend against herdr's local socket API
// (newline-delimited JSON-RPC-ish protocol; see https://herdr.dev/docs/socket-api/).
package herdr

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BambooTuna/tmuxui/internal/backend"
)

const (
	herdrDialTimeout           = 3 * time.Second
	herdrRequestTimeout        = 5 * time.Second
	herdrSubscribePollInterval = 400 * time.Millisecond
	herdrTopologyPollInterval  = 2 * time.Second
)

// DefaultSocketPath はherdrのソケットパス解決順(HERDR_SOCKET_PATH -> HERDR_SESSION -> デフォルト)
// に従い、herdrデーモンが待ち受けるUnixドメインソケットのパスを返す。
// (実機のhttps://herdr.dev/docs/socket-api/および`herdr --help`で確認済み)
func DefaultSocketPath() string {
	if p := os.Getenv("HERDR_SOCKET_PATH"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if name := os.Getenv("HERDR_SESSION"); name != "" {
		return filepath.Join(home, ".config", "herdr", "sessions", name, "herdr.sock")
	}
	return filepath.Join(home, ".config", "herdr", "herdr.sock")
}

// herdrClient はherdrソケットAPI(改行区切りJSON-RPC風)への最小クライアント。
// 呼び出しごとに新しいUnix接続を張って1往復で閉じる。herdrのソケットはローカルで
// レイテンシがほぼゼロなため、これで永続接続の多重化・再接続状態管理を丸ごと避けられる。
// herdrサーバーが再起動していても次回呼び出しが素朴に再接続するだけで自然に復旧する。
type herdrClient struct {
	socketPath string
	idCounter  uint64
}

func newHerdrClient(socketPath string) *herdrClient {
	return &herdrClient{socketPath: socketPath}
}

type herdrRequest struct {
	ID     string      `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params"`
}

type herdrError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *herdrError) Error() string {
	return fmt.Sprintf("herdr: %s: %s", e.Code, e.Message)
}

type herdrResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *herdrError     `json:"error"`
}

// call はmethod/paramsを1リクエストとして送り、resultをoutにデコードする(out==nilなら結果を捨てる)。
func (c *herdrClient) call(method string, params interface{}, out interface{}) error {
	id := fmt.Sprintf("tmuxui_%d", atomic.AddUint64(&c.idCounter, 1))
	data, err := json.Marshal(herdrRequest{ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}

	conn, err := net.DialTimeout("unix", c.socketPath, herdrDialTimeout)
	if err != nil {
		return fmt.Errorf("herdr: dial: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(herdrRequestTimeout))

	if _, err := conn.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("herdr: write: %w", err)
	}

	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil && line == "" {
		return fmt.Errorf("herdr: read: %w", err)
	}

	var resp herdrResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return fmt.Errorf("herdr: decode response: %w", err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out != nil && resp.Result != nil {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("herdr: decode result: %w", err)
		}
	}
	return nil
}

// herdrWorkspace/herdrTab/herdrPane はworkspace.list/tab.list/pane.listが返すフラットな
// 一覧の要素。3つとも独立したフラットリストなので、ListSessionsでworkspace_id/tab_idを
// キーに手元で組み立て直す。
// herdrWorktree はworkspace.listの"worktree"フィールド(worktree下で開かれたworkspaceのみ存在。
// 実機確認済み: repo_key/repo_root/checkout_path/is_linked_worktree、branch名相当のフィールドは無い)。
type herdrWorktree struct {
	RepoKey          string `json:"repo_key"`
	RepoName         string `json:"repo_name"`
	RepoRoot         string `json:"repo_root"`
	CheckoutPath     string `json:"checkout_path"`
	IsLinkedWorktree bool   `json:"is_linked_worktree"`
}

type herdrWorkspace struct {
	WorkspaceID string `json:"workspace_id"`
	Number      int    `json:"number"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
	// AgentStatus はworkspace内のagentの集約状態(idle/working/blocked/done/unknown、実機確認済み)。
	AgentStatus string         `json:"agent_status"`
	Worktree    *herdrWorktree `json:"worktree"`
}

type herdrTab struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Number      int    `json:"number"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
	AgentStatus string `json:"agent_status"`
}

type herdrPane struct {
	PaneID        string `json:"pane_id"`
	TerminalID    string `json:"terminal_id"`
	WorkspaceID   string `json:"workspace_id"`
	TabID         string `json:"tab_id"`
	Focused       bool   `json:"focused"`
	Cwd           string `json:"cwd"`
	ForegroundCwd string `json:"foreground_cwd"`
	Agent         string `json:"agent"`
	// AgentStatus はpane.list自体が返す(実機確認済み: agent.listを別途呼ばなくてもここに含まれる)。
	AgentStatus string `json:"agent_status"`
}

type herdrRect struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type herdrLayoutPane struct {
	PaneID string    `json:"pane_id"`
	Rect   herdrRect `json:"rect"`
}

type herdrLayout struct {
	Area  herdrRect         `json:"area"`
	Panes []herdrLayoutPane `json:"panes"`
}

type herdrReadResult struct {
	Read struct {
		Text string `json:"text"`
	} `json:"read"`
}

// herdrPaneGetResult はpane.getの結果のうちscrollの必要フィールドだけを見る(実機確認済み:
// pane_id指定でそのpane1件分のscroll{offset_from_bottom, max_offset_from_bottom, viewport_rows}
// を返す)。
type herdrPaneGetResult struct {
	Pane struct {
		Scroll struct {
			ViewportRows        int `json:"viewport_rows"`
			MaxOffsetFromBottom int `json:"max_offset_from_bottom"`
		} `json:"scroll"`
	} `json:"pane"`
}

// herdrTargetPattern はherdrのネイティブ識別子形式にマッチする: workspace_id単体("w8"、
// workspace単位操作のNewWindow/RenameSession/KillSession等が渡す)、tab_id("w8:t3")、
// pane_id("w8:p1")のいずれか。かつてpane_id形式しか許可していなかったため、workspace単位の
// 操作がBackendRegistry.Resolve経由で軒並み"invalid name"になっていたバグの修正(実機確認済み)。
// handleKillWindow/handleRenameWindow(handlers.go)がtab.number相当の素の数値インデックス
// (例:"2")を渡すケースもworkspace_id単体の形(コロン無し)として許可される。
var herdrTargetPattern = regexp.MustCompile(`^[A-Za-z0-9]+(:[tp][A-Za-z0-9]+)?$`)

// herdrPoller はSubscribe向けのターゲット単位ポーリング状態。herdrにはtmux control-modeの
// %outputに相当する「生の差分出力」を押し出すイベントが無い(events.subscribeのpane系イベントは
// pane_id指定必須かつagent_status_changed等の構造化イベントのみで、出力バイト列そのものは
// 運ばれない)。そのためSubscribeは内部ポーリング(pane.read visible/ansi)で代替し、変化があれば
// 「画面クリア+全量再描画」のANSIチャンクをpane_outputとして流す。xterm.js側は通常の端末出力として
// 解釈できるため、Hub/フロントエンドの変更は不要。
type herdrPoller struct {
	broadcaster *backend.Broadcaster
	stop        chan struct{}
	stopOnce    sync.Once
	lastText    string
}

// HerdrBackend はherdrソケットAPI経由のbackend.PaneBackend実装。
type HerdrBackend struct {
	client *herdrClient

	mu      sync.Mutex
	pollers map[string]*herdrPoller
	wg      sync.WaitGroup // runPoller/runTopologyPollerの終了待ち合わせ用(Close参照)

	onTopologyChange func()
	closed           chan struct{}
	closeOnce        sync.Once
}

var _ backend.PaneBackend = (*HerdrBackend)(nil)

func New(socketPath string) *HerdrBackend {
	b := &HerdrBackend{
		client:  newHerdrClient(socketPath),
		pollers: map[string]*herdrPoller{},
		closed:  make(chan struct{}),
	}
	b.wg.Add(1)
	go b.runTopologyPoller()
	return b
}

// SocketReachable はsocketPathにping疎通できるかを返す。main.goの-herdr=auto判定用。
func SocketReachable(socketPath string) bool {
	if socketPath == "" {
		return false
	}
	return newHerdrClient(socketPath).call("ping", struct{}{}, nil) == nil
}

func (b *HerdrBackend) ValidTarget(s string) bool {
	return herdrTargetPattern.MatchString(s)
}

// SupportsTextPermissionDetection はfalseを返す。herdrはpane/tab/workspaceそれぞれにagent_status
// (idle/working/blocked/done/unknown)という構造化された状態を持つため、Hub側の画面テキスト解析
// (tmux向けのdetectPermission)を重ねて実行すると二重の権限待ち通知になってしまう。
func (b *HerdrBackend) SupportsTextPermissionDetection() bool {
	return false
}

// SyncSessions: HerdrBackendはControlSessionのような常駐プロセス/接続を持たず、window/paneの
// target解決も都度ソケット呼び出しで行う(resolveTabID参照)ため、同期すべき内部状態が無い。
func (b *HerdrBackend) SyncSessions(sessions []backend.Session) {}

// ListSessions はworkspace.list/tab.list/pane.listをマージしてSession/Window/Paneへ変換する。
// pane一つひとつのcols/rowsはpane.layoutで取得するが、herdrのlayoutはtabにつき1回呼べば
// そのtab内の全paneのrectがまとめて返るため、tabごとに1回(paneごとではなく)呼び出す。
func (b *HerdrBackend) ListSessions() ([]backend.Session, error) {
	var wsRes struct {
		Workspaces []herdrWorkspace `json:"workspaces"`
	}
	if err := b.client.call("workspace.list", struct{}{}, &wsRes); err != nil {
		return nil, err
	}
	var tabRes struct {
		Tabs []herdrTab `json:"tabs"`
	}
	if err := b.client.call("tab.list", struct{}{}, &tabRes); err != nil {
		return nil, err
	}
	var paneRes struct {
		Panes []herdrPane `json:"panes"`
	}
	if err := b.client.call("pane.list", struct{}{}, &paneRes); err != nil {
		return nil, err
	}

	tabsByWorkspace := map[string][]herdrTab{}
	for _, t := range tabRes.Tabs {
		tabsByWorkspace[t.WorkspaceID] = append(tabsByWorkspace[t.WorkspaceID], t)
	}
	panesByTab := map[string][]herdrPane{}
	for _, p := range paneRes.Panes {
		panesByTab[p.TabID] = append(panesByTab[p.TabID], p)
	}

	sessions := make([]backend.Session, 0, len(wsRes.Workspaces))
	for _, w := range wsRes.Workspaces {
		tabs := tabsByWorkspace[w.WorkspaceID]
		windows := make([]backend.Window, 0, len(tabs))
		for _, t := range tabs {
			panes := panesByTab[t.TabID]
			rects := b.tabLayoutRects(panes)
			wpanes := make([]backend.Pane, 0, len(panes))
			for _, p := range panes {
				size := ""
				if r, ok := rects[p.PaneID]; ok {
					size = fmt.Sprintf("%dx%d", r.Width, r.Height)
				}
				path := p.ForegroundCwd
				if path == "" {
					path = p.Cwd
				}
				wpanes = append(wpanes, backend.Pane{
					Target: p.PaneID,
					ID:     p.TerminalID,
					// Cmd引き続きp.Agentのまま(既存フロントのpaneLabel()フォールバックとの後方互換用)。
					// AgentはCmdと同値だが、agent_statusと対を成す明示フィールドとして別途持たせる。
					Cmd:         p.Agent,
					Size:        size,
					Path:        path,
					Agent:       p.Agent,
					AgentStatus: p.AgentStatus,
				})
			}
			windows = append(windows, backend.Window{
				Index:       t.Number,
				ID:          t.TabID,
				Name:        t.Label,
				Active:      t.Focused,
				AgentStatus: t.AgentStatus,
				Panes:       wpanes,
			})
		}
		sessions = append(sessions, backend.Session{
			Name:          w.WorkspaceID,
			Attached:      w.Focused,
			DisplayName:   w.Label,
			AgentStatus:   w.AgentStatus,
			WorktreeLabel: worktreeLabel(w.Worktree),
			Windows:       windows,
		})
	}
	return sessions, nil
}

// worktreeLabel はherdrのworktree情報から"repo · dirname"形式の表示用ラベルを組み立てる。
// herdrのソケットAPIはgitブランチ名を直接返さないため、linked worktreeについてはcheckout_pathの
// ディレクトリ名をブランチ/ラベル相当として代用する(herdrはworktreeを"~/.herdr/worktrees/<repo>/<name>"
// に配置するため、<name>は実質的に作成時のブランチ/ラベルと一致する。実機確認済み)。
func worktreeLabel(wt *herdrWorktree) string {
	if wt == nil || wt.RepoName == "" {
		return ""
	}
	if wt.IsLinkedWorktree && wt.CheckoutPath != "" {
		if base := filepath.Base(wt.CheckoutPath); base != "" && base != "." && base != wt.RepoName {
			return wt.RepoName + " · " + base
		}
	}
	return wt.RepoName
}

// tabLayoutRects はpanes(同一tab内)のうち先頭1つのpane_idだけを使ってpane.layoutを1回呼び、
// そのtab内全paneのrectをまとめて取得する。呼び出し失敗時はnilを返し、Sizeは空欄のまま
// (表示上の劣化のみで致命的ではない)。
func (b *HerdrBackend) tabLayoutRects(panes []herdrPane) map[string]herdrRect {
	if len(panes) == 0 {
		return nil
	}
	var res struct {
		Layout herdrLayout `json:"layout"`
	}
	if err := b.client.call("pane.layout", map[string]string{"pane_id": panes[0].PaneID}, &res); err != nil {
		return nil
	}
	m := make(map[string]herdrRect, len(res.Layout.Panes))
	for _, p := range res.Layout.Panes {
		m[p.PaneID] = p.Rect
	}
	return m
}

func (b *HerdrBackend) paneSize(target string) (cols, rows int) {
	var res struct {
		Layout herdrLayout `json:"layout"`
	}
	if err := b.client.call("pane.layout", map[string]string{"pane_id": target}, &res); err != nil {
		return 0, 0
	}
	for _, p := range res.Layout.Panes {
		if p.PaneID == target {
			return p.Rect.Width, p.Rect.Height
		}
	}
	return res.Layout.Area.Width, res.Layout.Area.Height
}

// Snapshot はスクロールバック込みの画面内容をANSI付きで1回分取得する。tmux版のcapture-pane
// -S -SnapshotHistoryLines同様、source:"recent"+lines:backend.SnapshotHistoryLinesでxterm.js側の
// scrollbackバッファ(term.jsのscrollback:20000)を初期表示から遡れるようにする。tmux版と異なり
// カーソル位置はherdr側から取得できない(pane.read/pane.layoutともにカーソル座標を返さない)ため、
// カーソル位置決め打ちは行わない。xterm.js側は書き込んだ内容の末尾にカーソルを置くため、
// 通常のシェル/エージェント画面では実用上問題にならない。
func (b *HerdrBackend) Snapshot(target string) ([]byte, int, int, error) {
	var res herdrReadResult
	if err := b.client.call("pane.read", map[string]interface{}{
		"pane_id": target, "source": "recent", "lines": backend.SnapshotHistoryLines, "format": "ansi", "strip_ansi": false,
	}, &res); err != nil {
		return nil, 0, 0, err
	}
	cols, rows := b.paneSize(target)
	data := "\x1b[H\x1b[2J" + res.Read.Text
	return []byte(data), cols, rows, nil
}

// Subscribe はターゲット単位のポーリングgoroutineを(無ければ)起動して購読者を登録する。
// 実際の出力取得はrunPollerが行う。
func (b *HerdrBackend) Subscribe(target string) (<-chan []byte, func(), error) {
	ch := make(chan []byte, 256)
	once := &sync.Once{}

	b.mu.Lock()
	p, ok := b.pollers[target]
	if !ok {
		p = &herdrPoller{broadcaster: backend.NewBroadcaster(), stop: make(chan struct{})}
		b.pollers[target] = p
		b.wg.Add(1)
		go b.runPoller(target, p)
	}
	p.broadcaster.Add(ch, once)
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		if cur, ok := b.pollers[target]; ok && cur == p {
			p.broadcaster.Remove(ch)
			if p.broadcaster.Len() == 0 {
				delete(b.pollers, target)
				p.stopOnce.Do(func() { close(p.stop) })
			}
		}
		b.mu.Unlock()
		once.Do(func() { close(ch) })
	}
	return ch, cancel, nil
}

// paneViewportRows はpane.get.scroll.viewport_rowsを取得する。herdrの"visible"はPC本体の
// (スクロール位置込みの)デスクトップUIビューポートをそのまま返すため、実機確認済みの通り
// PC側でスクロールされるとvisibleの内容もスクロール位置に応じて変わってしまう。runPollerは
// それだと他クライアント(スマホ等)の画面がPCのスクロール位置をミラーしてしまうため、
// "recent"+viewport_rows行で常にライブ最下部を基準に読む(下記runPoller参照)。取得失敗/0の
// 場合は0を返し、呼び出し側でvisibleへフォールバックする。
func (b *HerdrBackend) paneViewportRows(target string) int {
	var res herdrPaneGetResult
	if err := b.client.call("pane.get", map[string]string{"pane_id": target}, &res); err != nil {
		return 0
	}
	return res.Pane.Scroll.ViewportRows
}

// lastNLines はsの末尾n行だけを返す安全装置。"recent"+linesは実機確認済みでは要求行数
// ちょうどを返すが、万一lines指定を超えて返ってきた場合にxterm.js側のscrollbackへ
// (visible相当を超える分が)重複して積み上がるのを防ぐ。n<=0または行数がn以下ならsをそのまま返す。
func lastNLines(s string, n int) string {
	if n <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// runPoller はtargetの画面内容を定期的に取得し、前回と異なれば「画面クリア+全量再描画」
// チャンクを購読者全員に配信する。viewport_rowsが取得できる限りsource:"recent"+
// lines:viewport_rowsで読む(=PC本体のスクロール位置に関係なく常にライブ最下部のviewport_rows行、
// 実機確認済みでoffset_from_bottom=0時はvisibleと完全一致)。取得できない場合のみ従来通り
// source:"visible"にフォールバックする。クリアシーケンスは\x1b[H\x1b[2J(カーソルホーム+
// 画面消去)であり、\x1bc(RIS)ではないためxterm.js側のスクロールバックは消えない
// (xterm.jsのEraseInDisplayはmode 2ではviewport内の行のみをリセットし、scrollbackを
// trimするのはmode 3の場合のみ)。よってSnapshotがrecentで書き込んだ履歴はポーリング更新後も
// 残り続け、かつ(lastNLinesでviewport_rows以内に切り詰めているため)重複して積み上がることもない。
// 購読者はTmuxControlBackend.handleOutputと同様、溜めずにcloseしてcancel扱いにする。
func (b *HerdrBackend) runPoller(target string, p *herdrPoller) {
	defer b.wg.Done()
	defer backend.RecoverAndLog("herdr.runPoller")
	ticker := time.NewTicker(herdrSubscribePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
		}

		rows := b.paneViewportRows(target)
		params := map[string]interface{}{
			"pane_id": target, "source": "visible", "format": "ansi", "strip_ansi": false,
		}
		if rows > 0 {
			params["source"] = "recent"
			params["lines"] = rows
		}
		var res herdrReadResult
		if err := b.client.call("pane.read", params, &res); err != nil {
			continue
		}
		text := res.Read.Text
		if rows > 0 {
			text = lastNLines(text, rows)
		}

		b.mu.Lock()
		if p.lastText == text {
			b.mu.Unlock()
			continue
		}
		p.lastText = text
		b.mu.Unlock()

		chunk := []byte("\x1b[H\x1b[2J" + text)
		p.broadcaster.Publish(chunk)
	}
}

// CapturePane はclassic描画・REST経由取得向けのキャプチャを、Snapshotと同様
// source:"recent"+lines:backend.SnapshotHistoryLinesでスクロールバック込みに取得する。
// classicモード(ansi.js)側で色/装飾を描画できるようANSI付きで返す(permission検知など
// 平文が必要な用途はCapturePanePlainを使う)。
func (b *HerdrBackend) CapturePane(target string) (*backend.PaneContent, error) {
	var res herdrReadResult
	if err := b.client.call("pane.read", map[string]interface{}{
		"pane_id": target, "source": "recent", "lines": backend.SnapshotHistoryLines, "format": "ansi", "strip_ansi": false,
	}, &res); err != nil {
		return nil, err
	}
	return &backend.PaneContent{
		Target:  target,
		Content: res.Read.Text,
		Lines:   strings.Count(res.Read.Text, "\n"),
		Ts:      time.Now().Unix(),
	}, nil
}

func (b *HerdrBackend) CapturePanePlain(target string) (string, error) {
	var res herdrReadResult
	if err := b.client.call("pane.read", map[string]interface{}{
		"pane_id": target, "source": "recent", "lines": backend.SnapshotHistoryLines, "format": "text", "strip_ansi": true,
	}, &res); err != nil {
		return "", err
	}
	return res.Read.Text, nil
}

// herdrKeyTranslation はtmux由来のキー名(フロントエンドのkeys-sheet/input.jsが送ってくる形式)を
// herdrのpane.send_keysが受理する名前、またはraw ANSIエスケープシーケンスへ変換する。
// pane.send_keysの語彙は実機確認済みでenter/esc/tab/shift+tab/up/down/left/right/backspace/
// ctrl+<letter>のみをサポートし、pageup/pagedown/home/end/backtab等は"unsupported key"エラーに
// なるため、それらはraw ANSIシーケンスをpane.send_text(リテラル入力)で送る。
func herdrKeyTranslation(key string) (special string, raw string, ok bool) {
	switch key {
	case "Enter":
		return "enter", "", true
	case "Escape":
		return "esc", "", true
	case "Tab":
		return "tab", "", true
	case "BTab":
		return "shift+tab", "", true
	case "Up":
		return "up", "", true
	case "Down":
		return "down", "", true
	case "Left":
		return "left", "", true
	case "Right":
		return "right", "", true
	case "BSpace":
		return "backspace", "", true
	case "PPage":
		return "", "\x1b[5~", true
	case "NPage":
		return "", "\x1b[6~", true
	case "Home":
		return "", "\x1b[H", true
	case "End":
		return "", "\x1b[F", true
	}
	if len(key) == 3 && key[1] == '-' {
		letter := strings.ToLower(string(key[2]))
		switch key[0] {
		case 'C':
			return "ctrl+" + letter, "", true
		case 'M':
			return "alt+" + letter, "", true
		}
	}
	return "", "", false
}

// SendKeys はtmux形式のキー名は変換して送り、それ以外はリテラルテキストとしてpane.send_textへ
// そのまま渡す。herdrのsend_textは埋め込み/末尾の"\n"をEnterとして扱う(実機確認済み)ため、
// tmux版sendKeys(tmux.go)のような改行分離処理は不要。
// paneHasScrollback はpaneがherdr側スクロールバックを持つかを返す(通常シェル=true)。
// alternate screenのフルスクリーンTUI(claude/vim等)はmax_offset_from_bottom=0になる
// (実機確認済み)。取得失敗時はfalse(=送信を通す)に倒し、TUI操作を阻害しない。
func (b *HerdrBackend) paneHasScrollback(target string) bool {
	var res herdrPaneGetResult
	if err := b.client.call("pane.get", map[string]string{"pane_id": target}, &res); err != nil {
		return false
	}
	return res.Pane.Scroll.MaxOffsetFromBottom > 0
}

func (b *HerdrBackend) SendKeys(target, keys string) error {
	if keys == "" {
		return nil
	}
	// PgUp/PgDnはリモートスクロールボタン専用で、alternate screenのTUIに画面を
	// 遡らせるためのもの。通常シェルpaneではキーを解釈するアプリがおらず
	// "^[[5~"の生シーケンスが入力バッファに打ち込まれてしまうだけなので送らない。
	// (通常paneの履歴はスクロールバック込みでCapturePane/pollerが取得済みで、
	// クライアント側スクロールで閲覧できるため機能的な損失は無い)
	if (keys == "PPage" || keys == "NPage") && b.paneHasScrollback(target) {
		return nil
	}
	if special, raw, ok := herdrKeyTranslation(keys); ok {
		if special != "" {
			return b.client.call("pane.send_keys", map[string]interface{}{
				"pane_id": target, "keys": []string{special},
			}, nil)
		}
		return b.client.call("pane.send_text", map[string]interface{}{
			"pane_id": target, "text": raw,
		}, nil)
	}
	return b.client.call("pane.send_text", map[string]interface{}{
		"pane_id": target, "text": keys,
	}, nil)
}

// Resize はno-op。herdrのpaneは常に実クライアント(herdrのデスクトップUI)が表示している
// 実サイズに従い、pane.resizeは方向指定の相対リサイズのみでcols x rowsの絶対指定手段が無い
// (実機確認済み)。TmuxControlBackend.Resizeが実クライアントアタッチ中のセッションに対して
// 何もせずnilを返す分岐と同じ考え方。
func (b *HerdrBackend) Resize(target string, cols, rows int) error {
	return nil
}

func (b *HerdrBackend) NewSession(name, dir string) error {
	params := map[string]interface{}{"focus": false}
	if dir != "" {
		params["cwd"] = dir
	}
	if name != "" {
		params["label"] = name
	}
	return b.client.call("workspace.create", params, nil)
}

func (b *HerdrBackend) KillSession(name string) error {
	return b.client.call("workspace.close", map[string]string{"workspace_id": name}, nil)
}

func (b *HerdrBackend) RenameSession(oldName, newName string) error {
	return b.client.call("workspace.rename", map[string]interface{}{
		"workspace_id": oldName, "label": newName,
	}, nil)
}

func (b *HerdrBackend) NewWindow(sessionName, windowName string) error {
	params := map[string]interface{}{"workspace_id": sessionName, "focus": false}
	if windowName != "" {
		params["label"] = windowName
	}
	return b.client.call("tab.create", params, nil)
}

// NewWorktree はworkspace_id配下のリポジトリにbranchという名前のworktreeを新規作成する。
// worktree.createは新しいworkspaceを生やすAPIで、base/path/labelはユーザー指定を持たないため
// herdr側のデフォルトに任せる(focusはfalse固定でUIフォーカスを奪わない)。
func (b *HerdrBackend) NewWorktree(sessionName, branch string) error {
	return b.client.call("worktree.create", map[string]interface{}{
		"workspace_id": sessionName, "branch": branch, "focus": false,
	}, nil)
}

// resolveTabID はhandler側が組み立てる"<workspace_id>:<windowIndex>"形式のtarget
// (KillWindow/RenameWindowの引数、tmuxの"session:windowIndex"に相当)から実際のtab_id
// ("<workspace_id>:t<n>"形式)を引く。tab_idの末尾番号がWindow.Index(tab.number)と常に
// 対応する保証がない(numberは表示用の連番、tab_idは永続的なID)ため、都度tab.listで引き直す。
func (b *HerdrBackend) resolveTabID(target string) (string, error) {
	idx := strings.LastIndexByte(target, ':')
	if idx < 0 {
		return "", fmt.Errorf("herdr: invalid window target %q", target)
	}
	workspaceID := target[:idx]
	number, err := strconv.Atoi(target[idx+1:])
	if err != nil {
		return "", fmt.Errorf("herdr: invalid window target %q", target)
	}
	var res struct {
		Tabs []herdrTab `json:"tabs"`
	}
	if err := b.client.call("tab.list", map[string]string{"workspace_id": workspaceID}, &res); err != nil {
		return "", err
	}
	for _, t := range res.Tabs {
		if t.Number == number {
			return t.TabID, nil
		}
	}
	return "", fmt.Errorf("herdr: window %q not found", target)
}

func (b *HerdrBackend) KillWindow(target string) error {
	tabID, err := b.resolveTabID(target)
	if err != nil {
		return err
	}
	return b.client.call("tab.close", map[string]string{"tab_id": tabID}, nil)
}

func (b *HerdrBackend) RenameWindow(target, newName string) error {
	tabID, err := b.resolveTabID(target)
	if err != nil {
		return err
	}
	return b.client.call("tab.rename", map[string]interface{}{
		"tab_id": tabID, "label": newName,
	}, nil)
}

func (b *HerdrBackend) KillPane(target string) error {
	return b.client.call("pane.close", map[string]string{"pane_id": target}, nil)
}

// SplitPane: pane.splitのtarget_pane_id(herdrのsrc/api/schema/panes.rsのPaneSplitParamsで
// 確認済み。公式ドキュメントのJSON例には載っていない)を明示的に渡すことで、グローバルUI
// フォーカスに一切触れずに対象paneを分割できる。省略すると現在フォーカス中のpaneが対象に
// なり、未知のフィールド名はエラーにならず黙って無視される点に注意。
func (b *HerdrBackend) SplitPane(target string, horizontal bool) error {
	direction := "down"
	if horizontal {
		direction = "right"
	}
	return b.client.call("pane.split", map[string]interface{}{
		"target_pane_id": target, "direction": direction, "focus": false,
	}, nil)
}

func (b *HerdrBackend) OnTopologyChange(fn func()) {
	b.mu.Lock()
	b.onTopologyChange = fn
	b.mu.Unlock()
}

// runTopologyPoller はtab/pane作成・削除イベント(pane_id指定必須のためsubscribe困難)の
// 代替として、ListSessionsの結果を定期的にJSON化して前回と比較し、変化があればコールバックする。
// workspace.created/closedはevents.subscribeでpane_idなしにグローバル購読できることを確認済み
// だが、tab/pane単位の変化までは拾えないため、ここでは一貫してポーリングに統一している。
func (b *HerdrBackend) runTopologyPoller() {
	defer b.wg.Done()
	defer backend.RecoverAndLog("herdr.runTopologyPoller")
	ticker := time.NewTicker(herdrTopologyPollInterval)
	defer ticker.Stop()
	last := ""
	first := true
	for {
		select {
		case <-b.closed:
			return
		case <-ticker.C:
		}
		sessions, err := b.ListSessions()
		if err != nil {
			continue
		}
		data, err := json.Marshal(sessions)
		if err != nil {
			continue
		}
		fp := string(data)
		if fp == last {
			continue
		}
		last = fp
		if first {
			first = false
			continue
		}
		b.mu.Lock()
		fn := b.onTopologyChange
		b.mu.Unlock()
		if fn != nil {
			fn()
		}
	}
}

// Close はバックグラウンドgoroutine(トポロジーポーラー・各ターゲットのSubscribeポーラー)を
// 停止し、その終了を待ってから返る。PaneBackendインターフェースの一部ではなく、
// テスト/シャットダウン専用。
func (b *HerdrBackend) Close() {
	b.closeOnce.Do(func() {
		close(b.closed)
		b.mu.Lock()
		for target, p := range b.pollers {
			p.stopOnce.Do(func() { close(p.stop) })
			delete(b.pollers, target)
		}
		b.mu.Unlock()
	})
	b.wg.Wait()
}
