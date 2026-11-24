package hub

import (
	"database/sql"
	"encoding/json"
	"log"
	"strings"
	"time"

	"go-chat/internal/models"
	"go-chat/internal/presence"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
)

// Client represents a single connected WebSocket user.
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	UserID   int64
	Username string
	RoomID   int64
}

// InboundEvent couples a decoded WebSocket message with the sending client.
type InboundEvent struct {
	Client  *Client
	Payload models.WSMessage
}

// Hub coordinates all active WebSocket clients, rooms, and message broadcasting via channels.
type Hub struct {
	db         *sql.DB
	presence   presence.Tracker
	rooms      map[int64]map[*Client]bool
	register   chan *Client
	unregister chan *Client
	inbound    chan InboundEvent
}

// NewHub initializes a Hub ready to run in its own goroutine.
func NewHub(db *sql.DB, tracker presence.Tracker) *Hub {
	return &Hub{
		db:         db,
		presence:   tracker,
		rooms:      make(map[int64]map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		inbound:    make(chan InboundEvent, 256),
	}
}

// Register queues a newly connected client for registration.
func (h *Hub) Register(c *Client) {
	h.register <- c
}

// Run processes client registrations, unregistrations, and inbound messages.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			if client.RoomID > 0 {
				h.joinRoom(client, client.RoomID)
			}

		case client := <-h.unregister:
			h.leaveCurrentRoom(client)
			close(client.send)

		case event := <-h.inbound:
			h.handleInbound(event)
		}
	}
}

func (h *Hub) handleInbound(event InboundEvent) {
	c := event.Client
	msg := event.Payload

	switch msg.Type {
	case "join":
		if msg.RoomID <= 0 {
			return
		}
		if c.RoomID == msg.RoomID {
			h.broadcastPresence(c.RoomID)
			return
		}
		h.leaveCurrentRoom(c)
		h.joinRoom(c, msg.RoomID)

	case "message":
		content := strings.TrimSpace(msg.Content)
		if content == "" || c.RoomID <= 0 {
			return
		}
		var msgID int64
		var createdAt time.Time
		err := h.db.QueryRow(
			`INSERT INTO messages (room_id, user_id, content) VALUES ($1, $2, $3) RETURNING id, created_at`,
			c.RoomID, c.UserID, content,
		).Scan(&msgID, &createdAt)
		if err != nil {
			log.Printf("hub: failed to persist message: %v", err)
			return
		}

		out := models.WSMessage{
			Type:      "message",
			RoomID:    c.RoomID,
			UserID:    c.UserID,
			Username:  c.Username,
			Content:   content,
			CreatedAt: createdAt,
		}
		h.broadcastToRoom(c.RoomID, out)
	}
}

func (h *Hub) joinRoom(c *Client, roomID int64) {
	if _, ok := h.rooms[roomID]; !ok {
		h.rooms[roomID] = make(map[*Client]bool)
	}
	h.rooms[roomID][c] = true
	c.RoomID = roomID

	if err := h.presence.SetOnline(roomID, c.Username); err != nil {
		log.Printf("presence: set online error: %v", err)
	}

	h.broadcastToRoom(roomID, models.WSMessage{
		Type:      "system",
		RoomID:    roomID,
		Username:  c.Username,
		Content:   c.Username + " joined the room",
		CreatedAt: time.Now(),
	})
	h.broadcastPresence(roomID)
}

func (h *Hub) leaveCurrentRoom(c *Client) {
	roomID := c.RoomID
	if roomID <= 0 {
		return
	}
	clients, ok := h.rooms[roomID]
	if ok {
		if _, exists := clients[c]; exists {
			delete(clients, c)
			if len(clients) == 0 {
				delete(h.rooms, roomID)
			}
		}
	}
	c.RoomID = 0

	// Only mark offline in presence tracker if this user has no other active tab in the same room.
	stillInRoom := false
	for other := range h.rooms[roomID] {
		if other.Username == c.Username {
			stillInRoom = true
			break
		}
	}
	if !stillInRoom {
		if err := h.presence.SetOffline(roomID, c.Username); err != nil {
			log.Printf("presence: set offline error: %v", err)
		}
	}

	h.broadcastToRoom(roomID, models.WSMessage{
		Type:      "system",
		RoomID:    roomID,
		Username:  c.Username,
		Content:   c.Username + " left the room",
		CreatedAt: time.Now(),
	})
	h.broadcastPresence(roomID)
}

func (h *Hub) broadcastPresence(roomID int64) {
	users, err := h.presence.OnlineInRoom(roomID)
	if err != nil {
		log.Printf("presence: fetch online users error: %v", err)
		return
	}
	h.broadcastToRoom(roomID, models.WSMessage{
		Type:        "presence",
		RoomID:      roomID,
		OnlineUsers: users,
		CreatedAt:   time.Now(),
	})
}

func (h *Hub) broadcastToRoom(roomID int64, msg models.WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	for client := range h.rooms[roomID] {
		select {
		case client.send <- data:
		default:
			// Drop slow/stalled client.
			delete(h.rooms[roomID], client)
			close(client.send)
		}
	}
}

// NewClient constructs a Client bound to a Hub and WebSocket connection.
func NewClient(hub *Hub, conn *websocket.Conn, userID int64, username string, initialRoomID int64) *Client {
	return &Client{
		hub:      hub,
		conn:     conn,
		send:     make(chan []byte, 256),
		UserID:   userID,
		Username: username,
		RoomID:   initialRoomID,
	}
}

// ReadPump reads messages from the WebSocket connection and forwards them to the Hub.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		var payload models.WSMessage
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		c.hub.inbound <- InboundEvent{
			Client:  c,
			Payload: payload,
		}
	}
}

// WritePump writes queued outbound messages and periodic pings to the WebSocket connection.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

