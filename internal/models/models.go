package models

import "time"

// User represents a registered chat user.
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

// Room represents a chat room.
type Room struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedBy int64     `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// Message represents a persisted chat message.
type Message struct {
	ID        int64     `json:"id"`
	RoomID    int64     `json:"room_id"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// WSMessage represents a JSON payload sent over WebSocket.
// Type can be: "join", "message", "presence", "system", "error".
type WSMessage struct {
	Type        string    `json:"type"`
	RoomID      int64     `json:"room_id,omitempty"`
	UserID      int64     `json:"user_id,omitempty"`
	Username    string    `json:"username,omitempty"`
	Content     string    `json:"content,omitempty"`
	OnlineUsers []string  `json:"online_users,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
}

