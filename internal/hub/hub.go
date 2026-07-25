// Package hub implements the WebSocket fan-out hub: it multiplexes pane content polling,
// backend-native output streaming (via Subscribe), pane-list/update-status broadcasts, and
// permission-detection notifications across all connected browser clients.
package hub

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/BambooTuna/tmuxui/internal/backend"
	"github.com/BambooTuna/tmuxui/internal/selfupdate"
)

// UpdateStatusProvider はWebSocket接続直後に送る初期update_statusメッセージの取得元。
// selfupdate.Managerが実装する(このパッケージがinternal/selfupdateへ依存する方向のみで、
// selfupdateからhubへの依存は発生しない)。
type UpdateStatusProvider interface {
	Status() selfupdate.UpdateStatus
}

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

type WSMessage struct {
	Type         string                   `json:"type"`
	Target       string                   `json:"target,omitempty"`
	Content      string                   `json:"content,omitempty"`
	Data         string                   `json:"data,omitempty"` // pane_snapshot/pane_outputのbase64本文
	Ts           int64                    `json:"ts,omitempty"`
	Sessions     []backend.Session        `json:"sessions,omitempty"`
	Prompt       string                   `json:"prompt,omitempty"`
	Keys         string                   `json:"keys,omitempty"`
	Cols         int                      `json:"cols,omitempty"`
	Rows         int                      `json:"rows,omitempty"`
	Mode         string                   `json:"mode,omitempty"` // subscribeの表示モード。"classic"のとき差分ストリーム(pane_snapshot/pane_output)を送らず、300ms tickのpane_contentだけに任せる。
	UpdateStatus *selfupdate.UpdateStatus `json:"updateStatus,omitempty"`
}

type Client struct {
	hub       *Hub
	conn      *websocket.Conn
	send      chan []byte // 送信側が複数存在するため close しない。終了シグナルは done で行う
	done      chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	target    string

	// backend.Subscribe経由のpane_output配信を管理する。1subscriptionにつき1goroutineで直列送信する。
	subMu   sync.Mutex
	subGen  uint64
	subStop func()
}

func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.done)
	})
}

func (c *Client) trySend(msg []byte) {
	select {
	case <-c.done:
		return
	default:
	}
	select {
	case c.send <- msg:
	case <-c.done:
	default:
	}
}

type Hub struct {
	mu          sync.RWMutex
	clients     map[*Client]struct{}
	prevContent map[string]string
	// failedCapture はCapturePaneが直近失敗したtargetの集合。ログをtarget単位の状態変化時
	// (失敗開始/復帰)のみに絞り、連続失敗でログが溢れるのを防ぐために使う。
	failedCapture map[string]bool
	registry      *backend.BackendRegistry
	updates       UpdateStatusProvider
}

func New() *Hub {
	return &Hub{
		clients:       map[*Client]struct{}{},
		prevContent:   map[string]string{},
		failedCapture: map[string]bool{},
	}
}

// SetRegistry はpane操作を委譲するBackendRegistryを設定する。main.goの組み立て時に1回呼ぶ。
func (h *Hub) SetRegistry(r *backend.BackendRegistry) {
	h.registry = r
}

// SetUpdates はWebSocket接続直後に送る初期update_statusメッセージの取得元を設定する。
func (h *Hub) SetUpdates(u UpdateStatusProvider) {
	h.updates = u
}

func (h *Hub) register(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	c.stopSubscription()
	c.close()
}

func (h *Hub) Run() {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	tick := 0
	for range ticker.C {
		func() {
			defer backend.RecoverAndLog("hub.Run tick")
			tick++
			// 権限検知は現行tickを3回に1回に間引く(約1秒間隔)。表示用ポーリング自体は300msのまま維持する。
			h.pollPanes(tick%3 == 0)
			if tick%20 == 0 {
				h.BroadcastPaneList()
			}
		}()
	}
}

func (h *Hub) pollPanes(doDetect bool) {
	h.mu.RLock()
	targets := map[string][]*Client{}
	for c := range h.clients {
		c.mu.Lock()
		t := c.target
		c.mu.Unlock()
		if t != "" {
			targets[t] = append(targets[t], c)
		}
	}
	h.mu.RUnlock()

	// subscribe中でないターゲットのキャッシュを削除（メモリリーク防止）
	h.mu.Lock()
	for t := range h.prevContent {
		if _, ok := targets[t]; !ok {
			delete(h.prevContent, t)
		}
	}
	for t := range h.failedCapture {
		if _, ok := targets[t]; !ok {
			delete(h.failedCapture, t)
		}
	}
	h.mu.Unlock()

	for target, clients := range targets {
		be, native, err := h.registry.Resolve(target)
		if err != nil {
			continue
		}

		pc, err := be.CapturePane(native)
		if err != nil {
			h.mu.Lock()
			wasFailing := h.failedCapture[target]
			h.failedCapture[target] = true
			h.mu.Unlock()
			if !wasFailing {
				log.Printf("hub: CapturePane(%s) failed: %v", target, err)
			}
		} else {
			h.mu.Lock()
			wasFailing := h.failedCapture[target]
			delete(h.failedCapture, target)
			changed := h.prevContent[target] != pc.Content
			if changed {
				h.prevContent[target] = pc.Content
			}
			h.mu.Unlock()
			if wasFailing {
				log.Printf("hub: CapturePane(%s) recovered", target)
			}

			if changed {
				msg, _ := json.Marshal(WSMessage{
					Type:    "pane_content",
					Target:  target,
					Content: pc.Content,
					Ts:      pc.Ts,
				})
				h.sendToClients(clients, msg)
			}
		}

		if !doDetect || !be.SupportsTextPermissionDetection() {
			continue
		}
		plain, err := be.CapturePanePlain(native)
		if err != nil {
			continue
		}
		if detected, prompt := detectPermission(plain); detected {
			permMsg, _ := json.Marshal(WSMessage{
				Type:   "permission_detected",
				Target: target,
				Prompt: prompt,
			})
			h.sendToClients(clients, permMsg)
		}
	}
}

func (h *Hub) BroadcastPaneList() {
	sessions, err := h.registry.ListSessions()
	if err != nil {
		return
	}
	msg, _ := json.Marshal(WSMessage{Type: "pane_list", Sessions: sessions})

	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	h.sendToClients(clients, msg)
}

// BroadcastUpdateStatus はアップデート状態を全クライアントへ通知する。BroadcastPaneList と同じ流儀。
// selfupdate.StatusBroadcaster インターフェースを満たす。
func (h *Hub) BroadcastUpdateStatus(status selfupdate.UpdateStatus) {
	msg, _ := json.Marshal(WSMessage{Type: "update_status", UpdateStatus: &status})

	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	h.sendToClients(clients, msg)
}

func (h *Hub) sendToClients(clients []*Client, msg []byte) {
	for _, c := range clients {
		c.trySend(msg)
	}
}

// HandleWS はWebSocketアップグレード + クライアント登録 + 初期状態送信 + 読み取りループを行う。
// httpapiパッケージから "/ws" のハンドラとしてそのまま登録される。
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &Client{
		hub:  h,
		conn: conn,
		send: make(chan []byte, 64),
		done: make(chan struct{}),
	}
	h.register(c)
	go c.writePump()

	if sessions, err := h.registry.ListSessions(); err == nil {
		if msg, err := json.Marshal(WSMessage{Type: "pane_list", Sessions: sessions}); err == nil {
			c.trySend(msg)
		}
	}
	if h.updates != nil {
		status := h.updates.Status()
		if msg, err := json.Marshal(WSMessage{Type: "update_status", UpdateStatus: &status}); err == nil {
			c.trySend(msg)
		}
	}

	c.readPump()
}

func (c *Client) readPump() {
	defer c.hub.unregister(c)
	defer backend.RecoverAndLog("hub.readPump")
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "subscribe":
			if msg.Target != "" && !c.hub.registry.ValidTarget(msg.Target) {
				continue
			}
			c.mu.Lock()
			c.target = msg.Target
			c.mu.Unlock()
			if msg.Cols > 0 && msg.Rows > 0 {
				c.hub.resize(msg.Target, msg.Cols, msg.Rows)
			}
			if be, native, err := c.hub.registry.Resolve(msg.Target); err == nil {
				if pc, err := be.CapturePane(native); err == nil {
					out, _ := json.Marshal(WSMessage{
						Type:    "pane_content",
						Target:  msg.Target,
						Content: pc.Content,
						Ts:      pc.Ts,
					})
					c.trySend(out)
				}
			}
			// classicモードは差分ストリーム(pane_snapshot/pane_output)を使わないため購読を張らない。
			// 表示更新はHubの300msポーリング(pane_content)側に任せる。
			if msg.Target != "" && msg.Mode != "classic" {
				c.startSubscription(msg.Target)
			} else {
				c.stopSubscription()
			}
		case "unsubscribe":
			c.mu.Lock()
			c.target = ""
			c.mu.Unlock()
			c.stopSubscription()
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 && c.hub.registry.ValidTarget(msg.Target) {
				c.hub.resize(msg.Target, msg.Cols, msg.Rows)
			}
		case "send_keys":
			be, native, err := c.hub.registry.Resolve(msg.Target)
			if err != nil {
				continue
			}
			be.SendKeys(native, msg.Keys)
		case "refresh":
			be, native, err := c.hub.registry.Resolve(msg.Target)
			if err != nil {
				continue
			}
			if pc, err := be.CapturePane(native); err == nil {
				out, _ := json.Marshal(WSMessage{
					Type:    "pane_content",
					Target:  msg.Target,
					Content: pc.Content,
					Ts:      pc.Ts,
				})
				c.trySend(out)
			}
		}
	}
}

func (c *Client) writePump() {
	defer c.conn.Close()
	defer backend.RecoverAndLog("hub.writePump")
	for {
		select {
		case <-c.done:
			return
		case msg := <-c.send:
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}
}

// resize はtargetをbackendに解決し、そのResizeへ委譲する。
func (h *Hub) resize(target string, cols, rows int) {
	if h.registry == nil {
		return
	}
	be, native, err := h.registry.Resolve(target)
	if err != nil {
		return
	}
	be.Resize(native, cols, rows)
}

// stopSubscription は現在のbackend購読(あれば)を破棄する。新規subscribe/unsubscribe/切断時に呼ぶ。
func (c *Client) stopSubscription() {
	c.subMu.Lock()
	c.subGen++
	stop := c.subStop
	c.subStop = nil
	c.subMu.Unlock()
	if stop != nil {
		stop()
	}
}

// startSubscription は既存の購読を破棄したうえで、targetに対する新しいbackend購読を開始する。
// 1つのsubscriptionのpane_output配信は単一goroutineで直列に行い、送出順序を保証する。
func (c *Client) startSubscription(target string) {
	c.subMu.Lock()
	c.subGen++
	gen := c.subGen
	stop := c.subStop
	c.subStop = nil
	c.subMu.Unlock()
	if stop != nil {
		stop()
	}
	if c.hub.registry == nil {
		return
	}
	go c.runSubscription(gen, target)
}

// runSubscription は Subscribe(登録) -> Snapshot -> pane_snapshot送信 -> 以降pane_outputとして
// 転送、の順序を守る。backend側がchanを閉じた場合(overflow)は、このsubscriptionがまだ
// 有効(gen一致)である限りSubscribeからやり直してフルリシンクする。
// rawTargetはクライアントに送り返す(プレフィックス付きの)target、nativeはbackend呼び出し用。
func (c *Client) runSubscription(gen uint64, rawTarget string) {
	defer backend.RecoverAndLog("hub.runSubscription")
	be, native, err := c.hub.registry.Resolve(rawTarget)
	if err != nil {
		return
	}
	for {
		stream, cancel, err := be.Subscribe(native)
		if err != nil {
			return
		}

		c.subMu.Lock()
		if c.subGen != gen {
			c.subMu.Unlock()
			cancel()
			return
		}
		c.subStop = cancel
		c.subMu.Unlock()

		// Subscribe〜Snapshotの間にバッファされたチャンクの効果はスナップショットに含まれるため、
		// 先に捨てないと二重適用で画面が化ける
		drainStream(stream)
		resyncing := false
		if snap, cols, rows, err := be.Snapshot(native); err == nil {
			if !c.sendDrop(snapshotMessage(rawTarget, snap, cols, rows)) {
				resyncing = true
			}
		} else {
			resyncing = true
		}

		for data := range stream {
			if resyncing {
				drainStream(stream)
				snap, cols, rows, err := be.Snapshot(native)
				if err != nil {
					continue
				}
				if !c.sendDrop(snapshotMessage(rawTarget, snap, cols, rows)) {
					continue
				}
				resyncing = false
				// 手元のdataとdrain分の効果はsnapshotに含まれるため再適用しない
				continue
			}
			if !c.sendDrop(outputMessage(rawTarget, data)) {
				// 送信チャネルが詰まった場合は溜めずに以降を捨て、次にoutboxが空いたときsnapshotから再開する
				resyncing = true
			}
		}

		c.subMu.Lock()
		stillCurrent := c.subGen == gen
		c.subMu.Unlock()
		if !stillCurrent {
			return
		}
		// streamがbackend側の都合(overflow)で閉じられた場合はここに来るので、Subscribeからやり直す
	}
}

// drainStream はチャネルに溜まっているチャンクを非ブロッキングで読み捨てる
func drainStream(ch <-chan []byte) {
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		default:
			return
		}
	}
}

func (c *Client) sendDrop(msg []byte) bool {
	select {
	case c.send <- msg:
		return true
	default:
		return false
	}
}

func snapshotMessage(target string, data []byte, cols, rows int) []byte {
	msg, _ := json.Marshal(WSMessage{
		Type:   "pane_snapshot",
		Target: target,
		Data:   base64.StdEncoding.EncodeToString(data),
		Cols:   cols,
		Rows:   rows,
		Ts:     time.Now().Unix(),
	})
	return msg
}

func outputMessage(target string, data []byte) []byte {
	msg, _ := json.Marshal(WSMessage{
		Type:   "pane_output",
		Target: target,
		Data:   base64.StdEncoding.EncodeToString(data),
		Ts:     time.Now().Unix(),
	})
	return msg
}
