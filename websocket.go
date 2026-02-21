package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type WSMessage struct {
	Type    string `json:"type"`
	Target  string `json:"target,omitempty"`
	Content string `json:"content,omitempty"`
	Ts      int64  `json:"ts,omitempty"`
	Panes   []Pane `json:"panes,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
	Keys    string `json:"keys,omitempty"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
}

type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	send   chan []byte
	mu     sync.Mutex
	target string
}

type Hub struct {
	mu          sync.RWMutex
	clients     map[*Client]struct{}
	prevContent map[string]string
}

func newHub() *Hub {
	return &Hub{
		clients:     map[*Client]struct{}{},
		prevContent: map[string]string{},
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
	close(c.send)
}

func (h *Hub) run() {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	tick := 0
	for range ticker.C {
		tick++
		h.pollPanes()
		if tick%17 == 0 {
			h.broadcastPaneList()
		}
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
	var panes []Pane
	for _, s := range sessions {
		panes = append(panes, s.Panes...)
	}
	msg, _ := json.Marshal(WSMessage{Type: "pane_list", Panes: panes})

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
		select {
		case c.send <- msg:
		default:
		}
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
	}
	hub.register(c)
	go c.writePump()

	if sessions, err := listSessions(); err == nil {
		var panes []Pane
		for _, s := range sessions {
			panes = append(panes, s.Panes...)
		}
		if msg, err := json.Marshal(WSMessage{Type: "pane_list", Panes: panes}); err == nil {
			c.send <- msg
		}
	}

	c.readPump()
}

func (c *Client) readPump() {
	defer c.hub.unregister(c)
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
				select {
				case c.send <- out:
				default:
				}
			}
		case "unsubscribe":
			c.mu.Lock()
			c.target = ""
			c.mu.Unlock()
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 && msg.Target != "" {
				resizePane(msg.Target, msg.Cols, msg.Rows)
			}
		case "send_keys":
			sendKeys(msg.Target, msg.Keys)
		case "refresh":
			if pc, err := capturePane(msg.Target); err == nil {
				out, _ := json.Marshal(WSMessage{
					Type:    "pane_content",
					Target:  msg.Target,
					Content: pc.Content,
					Ts:      pc.Ts,
				})
				select {
				case c.send <- out:
				default:
				}
			}
		}
	}
}

func (c *Client) writePump() {
	defer c.conn.Close()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
		}
	}
}
