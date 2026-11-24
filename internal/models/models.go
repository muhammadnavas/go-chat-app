package models

import "time"

// User represents a registered chat user.
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

// Room represents a public chat room or a 1-on-1 direct message room.
type Room struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	IsDirect  bool      `json:"is_direct"`
	CreatedBy int64     `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// ReactionSummary aggregates reactions on a message by emoji.
type ReactionSummary struct {
	Emoji string   `json:"emoji"`
	Count int      `json:"count"`
	Users []string `json:"users"`
}

// Message represents a persisted chat message.
type Message struct {
	ID        int64             `json:"id"`
	RoomID    int64             `json:"room_id"`
	UserID    int64             `json:"user_id"`
	Username  string            `json:"username"`
	Content   string            `json:"content"`
	EditedAt  *time.Time        `json:"edited_at,omitempty"`
	DeletedAt *time.Time        `json:"deleted_at,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	Reactions []ReactionSummary `json:"reactions,omitempty"`
}

// WSMessage represents a JSON payload sent over WebSocket.
// Type can be: "join", "message", "edit", "delete", "react", "typing", "stop_typing", "presence", "system", "error".
type WSMessage struct {
	Type        string            `json:"type"`
	MessageID   int64             `json:"message_id,omitempty"`
	RoomID      int64             `json:"room_id,omitempty"`
	UserID      int64             `json:"user_id,omitempty"`
	Username    string            `json:"username,omitempty"`
	Content     string            `json:"content,omitempty"`
	Emoji       string            `json:"emoji,omitempty"`
	OnlineUsers []string          `json:"online_users,omitempty"`
	GlobalUsers []string          `json:"global_users,omitempty"`
	Reactions   []ReactionSummary `json:"reactions,omitempty"`
	EditedAt    *time.Time        `json:"edited_at,omitempty"`
	CreatedAt   time.Time         `json:"created_at,omitempty"`
}
