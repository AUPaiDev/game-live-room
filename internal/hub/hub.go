package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ClientType distinguishes between admin and overlay clients.
type ClientType string

const (
	ClientAdmin   ClientType = "admin"
	ClientOverlay ClientType = "overlay"
)

// Message is the JSON structure for hub communication.
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Hub manages WebSocket connections and broadcasts messages.
type Hub struct {
	clients    map[*client]bool
	clientsMu  sync.RWMutex
	register  chan *client
	unregister chan *client
	broadcast chan Message
	upgrader  websocket.Upgrader
}

// client represents a connected WebSocket client.
type client struct {
	hub       *Hub
	conn      *websocket.Conn
	clientType ClientType
	send      chan []byte
}

// New creates a new Hub.
func New() *Hub {
	return &Hub{
		clients:    make(map[*client]bool),
		register:  make(chan *client),
		unregister: make(chan *client),
		broadcast: make(chan Message, 256),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
			ReadBufferSize:  1024,
			WriteBufferSize:  1024,
		},
	}
}

// Run starts the hub's event loop.
// It must be called in a goroutine.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			h.closeAll()
			return
		case c := <-h.register:
			h.clientsMu.Lock()
			h.clients[c] = true
			h.clientsMu.Unlock()
		case c := <-h.unregister:
			h.clientsMu.Lock()
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
			}
			h.clientsMu.Unlock()
		case msg := <-h.broadcast:
			data := h.marshal(msg)
			var dead []*client
			h.clientsMu.RLock()
			for c := range h.clients {
				select {
				case c.send <- data:
				default:
					dead = append(dead, c)
				}
			}
			h.clientsMu.RUnlock()
			if len(dead) > 0 {
				h.clientsMu.Lock()
				for _, c := range dead {
					if _, ok := h.clients[c]; ok {
						delete(h.clients, c)
						close(c.send)
					}
				}
				h.clientsMu.Unlock()
			}
		}
	}
}

// ServeHTTP upgrades an HTTP request to WebSocket.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t := r.URL.Query().Get("type")
	var ct ClientType
	switch t {
	case "overlay":
		ct = ClientOverlay
	default:
		ct = ClientAdmin
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	c := &client{
		hub:       h,
		conn:      conn,
		clientType: ct,
		send:      make(chan []byte, 256),
	}
	h.register <- c
	go c.writePump()
	go c.readPump()
}

func (h *Hub) marshal(msg Message) []byte {
	data, _ := json.Marshal(msg)
	return data
}

func (h *Hub) closeAll() {
	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()
	for c := range h.clients {
		c.conn.Close()
		delete(h.clients, c)
	}
}

// Broadcast sends a message to all connected clients.
func (h *Hub) Broadcast(msg Message) {
	select {
	case h.broadcast <- msg:
	default:
		// channel full, skip
	}
}

// BroadcastTo sends a message to clients of a specific type.
func (h *Hub) BroadcastTo(clientType ClientType, msg Message) {
	data := h.marshal(msg)
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	for c := range h.clients {
		if c.clientType == clientType {
			select {
			case c.send <- data:
			default:
				// channel full, skip
			}
		}
	}
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount(clientType ClientType) int {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	var n int
	for c := range h.clients {
		if clientType == "" || c.clientType == clientType {
			n++
		}
	}
	return n
}

func (c *client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (c *client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}