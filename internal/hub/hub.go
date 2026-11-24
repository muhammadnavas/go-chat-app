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
	maxMessageSize = 8192
	rateWindow     = 5 * time.Second
	maxMsgsPerWin  = 20
)

// Client represents a single connected WebSocket user.
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	UserID   int64
	Username string
	RoomID   int64
	msgTimes []time.Time
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
	allClients map[*Client]bool
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
		allClients: make(map[*Client]bool),
		rooms:      make(map[int64]map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		inbound:    make(chan InboundEvent, 512),
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
			h.allClients[client] = true
			if err := h.presence.SetGlobalOnline(client.Username); err != nil {
				log.Printf("presence: set global online error: %v", err)
			}
			if client.RoomID > 0 {
				h.joinRoom(client, client.RoomID)
			}
			h.broadcastGlobalPresence()

		case client := <-h.unregister:
			delete(h.allClients, client)
			h.leaveCurrentRoom(client)

			stillOnlineGlobally := false
			for other := range h.allClients {
				if other.Username == client.Username {
					stillOnlineGlobally = true
					break
				}
			}
			if !stillOnlineGlobally {
				if err := h.presence.SetGlobalOffline(client.Username); err != nil {
					log.Printf("presence: set global offline error: %v", err)
				}
			}
			close(client.send)
			h.broadcastGlobalPresence()

		case event := <-h.inbound:
			h.handleInbound(event)
		}
	}
}

func (c *Client) allowMessage() bool {
	now := time.Now()
	cutoff := now.Add(-rateWindow)
	valid := c.msgTimes[:0]
	for _, t := range c.msgTimes {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	c.msgTimes = valid
	if len(c.msgTimes) >= maxMsgsPerWin {
		return false
	}
	c.msgTimes = append(c.msgTimes, now)
	return true
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

	case "typing", "stop_typing":
		if c.RoomID <= 0 {
			return
		}
		h.broadcastToRoomExcept(c.RoomID, c, models.WSMessage{
			Type:     msg.Type,
			RoomID:   c.RoomID,
			UserID:   c.UserID,
			Username: c.Username,
		})

	case "message":
		if !c.allowMessage() {
			h.sendError(c, "Rate limit exceeded. Please slow down.")
			return
		}
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
			MessageID: msgID,
			RoomID:    c.RoomID,
			UserID:    c.UserID,
			Username:  c.Username,
			Content:   content,
			CreatedAt: createdAt,
		}
		h.broadcastToRoom(c.RoomID, out)

	case "edit":
		content := strings.TrimSpace(msg.Content)
		if msg.MessageID <= 0 || content == "" || c.RoomID <= 0 {
			return
		}
		var editedAt time.Time
		err := h.db.QueryRow(
			`UPDATE messages SET content = $1, edited_at = NOW()
			 WHERE id = $2 AND user_id = $3 AND room_id = $4 AND deleted_at IS NULL
			 RETURNING edited_at`,
			content, msg.MessageID, c.UserID, c.RoomID,
		).Scan(&editedAt)
		if err != nil {
			return
		}
		h.broadcastToRoom(c.RoomID, models.WSMessage{
			Type:      "edit",
			MessageID: msg.MessageID,
			RoomID:    c.RoomID,
			UserID:    c.UserID,
			Username:  c.Username,
			Content:   content,
			EditedAt:  &editedAt,
		})

	case "delete":
		if msg.MessageID <= 0 || c.RoomID <= 0 {
			return
		}
		res, err := h.db.Exec(
			`UPDATE messages SET content = '', deleted_at = NOW()
			 WHERE id = $1 AND user_id = $2 AND room_id = $3 AND deleted_at IS NULL`,
			msg.MessageID, c.UserID, c.RoomID,
		)
		if err != nil {
			return
		}
		if rows, _ := res.RowsAffected(); rows == 0 {
			return
		}
		h.broadcastToRoom(c.RoomID, models.WSMessage{
			Type:      "delete",
			MessageID: msg.MessageID,
			RoomID:    c.RoomID,
		})

	case "react":
		emoji := strings.TrimSpace(msg.Emoji)
		if msg.MessageID <= 0 || emoji == "" || len(emoji) > 16 || c.RoomID <= 0 {
			return
		}
		// Toggle reaction: delete if exists, otherwise insert.
		res, err := h.db.Exec(
			`DELETE FROM message_reactions WHERE message_id = $1 AND user_id = $2 AND emoji = $3`,
			msg.MessageID, c.UserID, emoji,
		)
		if err != nil {
			return
		}
		if rows, _ := res.RowsAffected(); rows == 0 {
			_, err = h.db.Exec(
				`INSERT INTO message_reactions (message_id, user_id, emoji) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
				msg.MessageID, c.UserID, emoji,
			)
			if err != nil {
				return
			}
		}

		reactions, err := h.fetchReactions(msg.MessageID)
		if err != nil {
			return
		}
		h.broadcastToRoom(c.RoomID, models.WSMessage{
			Type:      "react",
			MessageID: msg.MessageID,
			RoomID:    c.RoomID,
			Reactions: reactions,
		})
	}
}

func (h *Hub) fetchReactions(messageID int64) ([]models.ReactionSummary, error) {
	rows, err := h.db.Query(`
		SELECT r.emoji, u.username
		FROM message_reactions r
		JOIN users u ON u.id = r.user_id
		WHERE r.message_id = $1
		ORDER BY r.created_at ASC
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byEmoji := make(map[string]*models.ReactionSummary)
	order := make([]string, 0)
	for rows.Next() {
		var emoji, username string
		if err := rows.Scan(&emoji, &username); err != nil {
			return nil, err
		}
		if _, ok := byEmoji[emoji]; !ok {
			byEmoji[emoji] = &models.ReactionSummary{Emoji: emoji}
			order = append(order, emoji)
		}
		byEmoji[emoji].Count++
		byEmoji[emoji].Users = append(byEmoji[emoji].Users, username)
	}

	out := make([]models.ReactionSummary, 0, len(order))
	for _, e := range order {
		out = append(out, *byEmoji[e])
	}
	return out, nil
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
	globalUsers, _ := h.presence.GlobalOnline()
	h.broadcastToRoom(roomID, models.WSMessage{
		Type:        "presence",
		RoomID:      roomID,
		OnlineUsers: users,
		GlobalUsers: globalUsers,
		CreatedAt:   time.Now(),
	})
}

func (h *Hub) broadcastGlobalPresence() {
	globalUsers, err := h.presence.GlobalOnline()
	if err != nil {
		return
	}
	data, err := json.Marshal(models.WSMessage{
		Type:        "global_presence",
		GlobalUsers: globalUsers,
		CreatedAt:   time.Now(),
	})
	if err != nil {
		return
	}
	for client := range h.allClients {
		select {
		case client.send <- data:
		default:
		}
	}
}

func (h *Hub) sendError(c *Client, errMsg string) {
	data, err := json.Marshal(models.WSMessage{
		Type:    "error",
		Content: errMsg,
	})
	if err != nil {
		return
	}
	select {
	case c.send <- data:
	default:
	}
}

func (h *Hub) broadcastToRoom(roomID int64, msg models.WSMessage) {
	h.broadcastToRoomExcept(roomID, nil, msg)
}

func (h *Hub) broadcastToRoomExcept(roomID int64, except *Client, msg models.WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	for client := range h.rooms[roomID] {
		if except != nil && client == except {
			continue
		}
		select {
		case client.send <- data:
		default:
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
