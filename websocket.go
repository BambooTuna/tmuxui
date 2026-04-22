package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

func recoverAndLog(where string) {
	if r := recover(); r != nil {
		log.Printf("panic recovered in %s: %v\n%s", where, r, debug.Stack())
	}
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
	Type         string           `json:"type"`
	Target       string           `json:"target,omitempty"`
	Content      string           `json:"content,omitempty"`
	Ts           int64            `json:"ts,omitempty"`
	Sessions     []Session        `json:"sessions,omitempty"`
	LastActivity map[string]int64 `json:"lastActivity,omitempty"`
	Prompt       string           `json:"prompt,omitempty"`
	Keys         string           `json:"keys,omitempty"`
	Cols         int              `json:"cols,omitempty"`
	Rows         int              `json:"rows,omitempty"`
}

type Client struct {
	hub       *Hub
	conn      *websocket.Conn
	send      chan []byte // 送信側が複数存在するため close しない。終了シグナルは done で行う
	mu        sync.Mutex
	target    string
	done      chan struct{}
	closeOnce sync.Once
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
	activity    *ActivityTracker
}

func newHub(activity *ActivityTracker) *Hub {
	return &Hub{
		clients:     map[*Client]struct{}{},
		prevContent: map[string]string{},
		activity:    activity,
	}
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
	c.close()
}

func (h *Hub) run() {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	tick := 0
	for range ticker.C {
		func() {
			defer recoverAndLog("hub.run tick")
			tick++
			h.pollPanes()
			if tick%17 == 0 {
				h.broadcastPaneList()
			}
		}()
	}
}

func (h *Hub) pollPanes() {
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
	h.mu.Unlock()

	for target, clients := range targets {
		pc, err := capturePane(target)
		if err != nil {
			continue
		}

		h.mu.Lock()
		changed := h.prevContent[target] != pc.Content
		if changed {
			h.prevContent[target] = pc.Content
		}
		h.mu.Unlock()

		if !changed {
			continue
		}

		if sessionName := targetToSession(target); sessionName != "" {
			h.activity.Touch(sessionName)
		}

		msg, _ := json.Marshal(WSMessage{
			Type:    "pane_content",
			Target:  target,
			Content: pc.Content,
			Ts:      pc.Ts,
		})
		h.sendToClients(clients, msg)

		if detected, prompt := detectPermission(pc.Content); detected {
			permMsg, _ := json.Marshal(WSMessage{
				Type:   "permission_detected",
				Target: target,
				Prompt: prompt,
			})
			h.sendToClients(clients, permMsg)
		}
	}
}

func (h *Hub) broadcastPaneList() {
	sessions, err := listSessions()
	if err != nil {
		return
	}
	la := h.activity.BuildMap(sessions)
	msg, _ := json.Marshal(WSMessage{Type: "pane_list", Sessions: sessions, LastActivity: la})

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

func handleWS(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &Client{
		hub:  hub,
		conn: conn,
		send: make(chan []byte, 64),
		done: make(chan struct{}),
	}
	hub.register(c)
	go c.writePump()

	if sessions, err := listSessions(); err == nil {
		la := hub.activity.BuildMap(sessions)
		if msg, err := json.Marshal(WSMessage{Type: "pane_list", Sessions: sessions, LastActivity: la}); err == nil {
			c.trySend(msg)
		}
	}

	c.readPump()
}

func (c *Client) readPump() {
	defer c.hub.unregister(c)
	defer recoverAndLog("readPump")
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
			if msg.Target != "" && !isValidTarget(msg.Target) {
				continue
			}
			c.mu.Lock()
			c.target = msg.Target
			c.mu.Unlock()
			if msg.Cols > 0 && msg.Rows > 0 {
				resizePane(msg.Target, msg.Cols, msg.Rows)
			}
			if pc, err := capturePane(msg.Target); err == nil {
				out, _ := json.Marshal(WSMessage{
					Type:    "pane_content",
					Target:  msg.Target,
					Content: pc.Content,
					Ts:      pc.Ts,
				})
				c.trySend(out)
			}
		case "unsubscribe":
			c.mu.Lock()
			c.target = ""
			c.mu.Unlock()
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 && isValidTarget(msg.Target) {
				resizePane(msg.Target, msg.Cols, msg.Rows)
			}
		case "send_keys":
			if !isValidTarget(msg.Target) {
				continue
			}
			sendKeys(msg.Target, msg.Keys)
		case "refresh":
			if !isValidTarget(msg.Target) {
				continue
			}
			if pc, err := capturePane(msg.Target); err == nil {
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
	defer recoverAndLog("writePump")
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

func targetToSession(target string) string {
	if i := strings.Index(target, ":"); i >= 0 {
		return target[:i]
	}
	return ""
}
