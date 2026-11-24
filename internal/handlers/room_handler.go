package handlers

import (
	"database/sql"
	"fmt"
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

type createDMRequest struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
}

// ListRooms returns public rooms and direct-message rooms belonging to the current user.
func (h *RoomHandler) ListRooms(c *gin.Context) {
	userID := c.GetInt64(auth.ContextUserIDKey)

	rows, err := h.db.Query(`
		SELECT r.id,
		       CASE
		         WHEN r.is_direct THEN COALESCE((
		           SELECT u.username
		           FROM room_members rm2
		           JOIN users u ON u.id = rm2.user_id
		           WHERE rm2.room_id = r.id AND rm2.user_id <> $1
		           LIMIT 1
		         ), r.name)
		         ELSE r.name
		       END AS display_name,
		       r.is_direct,
		       COALESCE(r.created_by, 0),
		       r.created_at
		FROM rooms r
		LEFT JOIN room_members rm ON rm.room_id = r.id AND rm.user_id = $1
		WHERE r.is_direct = FALSE OR rm.user_id = $1
		ORDER BY r.is_direct ASC, r.id ASC
	`, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load rooms"})
		return
	}
	defer rows.Close()

	rooms := make([]models.Room, 0)
	for rows.Next() {
		var r models.Room
		if err := rows.Scan(&r.ID, &r.Name, &r.IsDirect, &r.CreatedBy, &r.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan room"})
			return
		}
		rooms = append(rooms, r)
	}
	c.JSON(http.StatusOK, gin.H{"rooms": rooms})
}

// CreateRoom creates a new public chat room.
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
		`INSERT INTO rooms (name, is_direct, created_by) VALUES ($1, FALSE, $2)
		 RETURNING id, name, is_direct, COALESCE(created_by, 0), created_at`,
		name, userID,
	).Scan(&room.ID, &room.Name, &room.IsDirect, &room.CreatedBy, &room.CreatedAt)
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

// CreateDirectRoom finds or creates a 1-on-1 direct message room with the target username.
func (h *RoomHandler) CreateDirectRoom(c *gin.Context) {
	var req createDMRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target username is required"})
		return
	}

	currentID := c.GetInt64(auth.ContextUserIDKey)
	targetUsername := strings.TrimSpace(req.Username)

	var targetID int64
	err := h.db.QueryRow(`SELECT id FROM users WHERE username = $1`, targetUsername).Scan(&targetID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if targetID == currentID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot start a DM with yourself"})
		return
	}

	low, high := currentID, targetID
	if low > high {
		low, high = high, low
	}
	dmName := fmt.Sprintf("dm:%d:%d", low, high)

	var room models.Room
	err = h.db.QueryRow(
		`SELECT id, is_direct, COALESCE(created_by, 0), created_at FROM rooms WHERE name = $1`,
		dmName,
	).Scan(&room.ID, &room.IsDirect, &room.CreatedBy, &room.CreatedAt)
	if err == sql.ErrNoRows {
		err = h.db.QueryRow(
			`INSERT INTO rooms (name, is_direct, created_by) VALUES ($1, TRUE, $2)
			 RETURNING id, is_direct, COALESCE(created_by, 0), created_at`,
			dmName, currentID,
		).Scan(&room.ID, &room.IsDirect, &room.CreatedBy, &room.CreatedAt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create DM room"})
			return
		}
		_, _ = h.db.Exec(
			`INSERT INTO room_members (room_id, user_id) VALUES ($1, $2), ($1, $3) ON CONFLICT DO NOTHING`,
			room.ID, currentID, targetID,
		)
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	room.Name = targetUsername
	c.JSON(http.StatusOK, gin.H{"room": room})
}

// ListUsers returns all registered usernames except the caller.
func (h *RoomHandler) ListUsers(c *gin.Context) {
	currentID := c.GetInt64(auth.ContextUserIDKey)
	rows, err := h.db.Query(`SELECT username FROM users WHERE id <> $1 ORDER BY username ASC`, currentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load users"})
		return
	}
	defer rows.Close()

	users := make([]string, 0)
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err == nil {
			users = append(users, u)
		}
	}
	globalOnline, _ := h.presence.GlobalOnline()
	c.JSON(http.StatusOK, gin.H{
		"users":         users,
		"global_online": globalOnline,
	})
}

// GetMessages returns paginated chat history (and optional search) for a room, ordered oldest to newest.
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

	var beforeID int64
	if raw := c.Query("before_id"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			beforeID = parsed
		}
	}

	searchQuery := strings.TrimSpace(c.Query("q"))

	query := `
		SELECT id, room_id, user_id, username, content, edited_at, deleted_at, created_at
		FROM (
			SELECT m.id, m.room_id, m.user_id, u.username, m.content, m.edited_at, m.deleted_at, m.created_at
			FROM messages m
			JOIN users u ON u.id = m.user_id
			WHERE m.room_id = $1
			  AND ($2 = 0 OR m.id < $2)
			  AND ($3 = '' OR (m.deleted_at IS NULL AND m.content ILIKE '%' || $3 || '%'))
			ORDER BY m.id DESC
			LIMIT $4
		) sub
		ORDER BY id ASC
	`
	rows, err := h.db.Query(query, roomID, beforeID, searchQuery, limit+1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load messages"})
		return
	}
	defer rows.Close()

	messages := make([]models.Message, 0)
	msgIDs := make([]int64, 0)
	for rows.Next() {
		var m models.Message
		if err := rows.Scan(&m.ID, &m.RoomID, &m.UserID, &m.Username, &m.Content, &m.EditedAt, &m.DeletedAt, &m.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan message"})
			return
		}
		messages = append(messages, m)
	}

	hasMore := false
	if len(messages) > limit {
		hasMore = true
		messages = messages[1:]
	}

	for _, m := range messages {
		msgIDs = append(msgIDs, m.ID)
	}

	if len(msgIDs) > 0 {
		reactMap, err := h.fetchReactionsBatch(msgIDs)
		if err == nil {
			for i := range messages {
				if r, ok := reactMap[messages[i].ID]; ok {
					messages[i].Reactions = r
				}
			}
		}
	}

	online, _ := h.presence.OnlineInRoom(roomID)
	globalOnline, _ := h.presence.GlobalOnline()
	c.JSON(http.StatusOK, gin.H{
		"messages":      messages,
		"has_more":      hasMore,
		"online_users":  online,
		"global_users":  globalOnline,
	})
}

func (h *RoomHandler) fetchReactionsBatch(messageIDs []int64) (map[int64][]models.ReactionSummary, error) {
	rows, err := h.db.Query(`
		SELECT r.message_id, r.emoji, u.username
		FROM message_reactions r
		JOIN users u ON u.id = r.user_id
		WHERE r.message_id = ANY($1)
		ORDER BY r.created_at ASC
	`, pq.Array(messageIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type key struct {
		msgID int64
		emoji string
	}
	grouped := make(map[key]*models.ReactionSummary)
	order := make(map[int64][]string)

	for rows.Next() {
		var msgID int64
		var emoji, username string
		if err := rows.Scan(&msgID, &emoji, &username); err != nil {
			return nil, err
		}
		k := key{msgID: msgID, emoji: emoji}
		if _, ok := grouped[k]; !ok {
			grouped[k] = &models.ReactionSummary{Emoji: emoji}
			order[msgID] = append(order[msgID], emoji)
		}
		grouped[k].Count++
		grouped[k].Users = append(grouped[k].Users, username)
	}

	result := make(map[int64][]models.ReactionSummary)
	for msgID, emojis := range order {
		list := make([]models.ReactionSummary, 0, len(emojis))
		for _, e := range emojis {
			list = append(list, *grouped[key{msgID: msgID, emoji: e}])
		}
		result[msgID] = list
	}
	return result, nil
}
