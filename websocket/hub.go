package websocket

import (
	"encoding/json"
	"log"
	"sync"

	gorillaws "github.com/gorilla/websocket"
)

type Client struct {
	Conn *gorillaws.Conn
	Send chan []byte
	Subs map[string]struct{}
	mu   sync.Mutex
}

type Hub struct {
	clients    map[*Client]struct{}
	register   chan *Client
	unregister chan *Client
	broadcast  chan Message
	mu         sync.RWMutex
}

type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

func NewHub() *Hub {
	return &Hub{
		clients:    map[*Client]struct{}{},
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan Message, 2048),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c] = struct{}{}
			h.mu.Unlock()
		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.Send)
			}
			h.mu.Unlock()
		case msg := <-h.broadcast:
			h.dispatch(msg)
		}
	}
}

// dispatch sends a message to only clients that should receive it.
// If the message contains a symbol in Data (object with "symbol" key), we send only
// to clients subscribed to that symbol. Otherwise broadcast to all clients.
func (h *Hub) dispatch(msg Message) {
	b, err := json.Marshal(msg)
	if err != nil {
		log.Printf("hub: marshal broadcast: %v", err)
		return
	}

	// Try to extract symbol from Data if possible
	var symbol string
	// First, try if Data is a map[string]interface{}
	if m, ok := msg.Data.(map[string]interface{}); ok {
		if s, found := m["symbol"]; found {
			if ss, ok2 := s.(string); ok2 {
				symbol = ss
			}
		}
	} else {
		// re-marshal Data to explore its fields
		db, _ := json.Marshal(msg.Data)
		var tmp map[string]interface{}
		if err := json.Unmarshal(db, &tmp); err == nil {
			if s, ok := tmp["symbol"].(string); ok {
				symbol = s
			}
		}
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		// If message has a symbol, only send to clients that subscribed.
		if symbol != "" {
			c.mu.Lock()
			_, subscribed := c.Subs[symbol]
			c.mu.Unlock()
			if !subscribed {
				continue
			}
		}
		select {
		case c.Send <- b:
		default:
			// slow client; unregister it to avoid blocking
			go func(cl *Client) { h.unregister <- cl }(c)
		}
	}
}

func (h *Hub) Broadcast(msg Message) {
	select {
	case h.broadcast <- msg:
	default:
		log.Println("hub: broadcast channel full, dropping message")
	}
}

func (h *Hub) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		_ = c.Conn.WriteMessage(gorillaws.CloseMessage, gorillaws.FormatCloseMessage(gorillaws.CloseNormalClosure, "server shutdown"))
		_ = c.Conn.Close()
	}
}

func (h *Hub) Register(c *Client)   { h.register <- c }
func (h *Hub) Unregister(c *Client) { h.unregister <- c }

func (c *Client) AddSub(sym string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Subs == nil {
		c.Subs = make(map[string]struct{})
	}
	c.Subs[sym] = struct{}{}
}
func (c *Client) RemoveSub(sym string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.Subs, sym)
}
