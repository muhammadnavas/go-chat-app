package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"go-chat/internal/auth"
	"go-chat/internal/models"
	"go-chat/internal/presence"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

type RoomHandler struct {
	db       *sql.DB
	presence presence.Tracker
}

func NewRoomHandler(db *sql.DB, tracker presence.Tracker) *RoomHandler {
	return &RoomHandler{db: db, presence: tracker}
}

type createRoomRequest struct {
	Name string `json:"name" binding:"required,min=2,max=40"`
}

// ListRooms returns all available chat rooms.
func (h *RoomHandler) ListRooms(c *gin.Context) {
	rows, err := h.db.Query(`SELECT id, name, COALESCE(created_by, 0), created_at FROM rooms ORDER BY id ASC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load rooms"})
		return
	}
	defer rows.Close()

	rooms := make([]models.Room, 0)
	for rows.Next() {
		var r models.Room
		if err := rows.Scan(&r.ID, &r.Name, &r.CreatedBy, &r.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan room"})
			return
		}
		rooms = append(rooms, r)
	}
	c.JSON(http.StatusOK, gin.H{"rooms": rooms})
}

// CreateRoom creates a new chat room.
func (h *RoomHandler) CreateRoom(c *gin.Context) {
	var req createRoomRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "room name (2-40 chars) is required"})
		return
	}

	name := strings.ToLower(strings.TrimSpace(req.Name))
	userID := c.GetInt64(auth.ContextUserIDKey)

	var room models.Room
	err := h.db.QueryRow(
		`INSERT INTO rooms (name, created_by) VALUES ($1, $2) RETURNING id, name, COALESCE(created_by, 0), created_at`,
		name, userID,
	).Scan(&room.ID, &room.Name, &room.CreatedBy, &room.CreatedAt)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			c.JSON(http.StatusConflict, gin.H{"error": "room already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create room"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"room": room})
}

// GetMessages returns recent chat history for a room, ordered oldest to newest.
func (h *RoomHandler) GetMessages(c *gin.Context) {
	roomID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || roomID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid room id"})
		return
	}

	limit := 50
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	rows, err := h.db.Query(`
		SELECT id, room_id, user_id, username, content, created_at
		FROM (
			SELECT m.id, m.room_id, m.user_id, u.username, m.content, m.created_at
			FROM messages m
			JOIN users u ON u.id = m.user_id
			WHERE m.room_id = $1
			ORDER BY m.created_at DESC
			LIMIT $2
		) sub
		ORDER BY created_at ASC
	`, roomID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load messages"})
		return
	}
	defer rows.Close()

	messages := make([]models.Message, 0)
	for rows.Next() {
		var m models.Message
		if err := rows.Scan(&m.ID, &m.RoomID, &m.UserID, &m.Username, &m.Content, &m.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan message"})
			return
		}
		messages = append(messages, m)
	}

	online, _ := h.presence.OnlineInRoom(roomID)
	c.JSON(http.StatusOK, gin.H{
		"messages":     messages,
		"online_users": online,
	})
}

