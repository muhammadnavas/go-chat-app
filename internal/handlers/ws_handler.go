package handlers

import (
	"net/http"
	"strconv"

	"go-chat/internal/auth"
	"go-chat/internal/hub"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type WSHandler struct {
	hub       *hub.Hub
	jwtSecret string
}

func NewWSHandler(h *hub.Hub, jwtSecret string) *WSHandler {
	return &WSHandler{hub: h, jwtSecret: jwtSecret}
}

// ServeWS authenticates the WebSocket connection via ?token= and spawns ReadPump/WritePump goroutines.
func (h *WSHandler) ServeWS(c *gin.Context) {
	tokenStr := c.Query("token")
	if tokenStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
		return
	}

	claims, err := auth.ParseToken(tokenStr, h.jwtSecret)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return
	}

	var initialRoomID int64
	if raw := c.Query("room_id"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			initialRoomID = parsed
		}
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	client := hub.NewClient(h.hub, conn, claims.UserID, claims.Username, initialRoomID)
	h.hub.Register(client)

	go client.WritePump()
	go client.ReadPump()
}

