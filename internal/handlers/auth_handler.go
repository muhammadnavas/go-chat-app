package handlers

import (
	"database/sql"
	"net/http"
	"strings"

	"go-chat/internal/auth"
	"go-chat/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

type AuthHandler struct {
	db        *sql.DB
	jwtSecret string
}

func NewAuthHandler(db *sql.DB, jwtSecret string) *AuthHandler {
	return &AuthHandler{db: db, jwtSecret: jwtSecret}
}

type authRequest struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Password string `json:"password" binding:"required,min=6,max=72"`
}

// Register creates a new user account and returns a JWT.
func (h *AuthHandler) Register(c *gin.Context) {
	var req authRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username (3-32 chars) and password (min 6 chars) are required"})
		return
	}

	username := strings.TrimSpace(req.Username)
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	var user models.User
	err = h.db.QueryRow(
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id, username, created_at`,
		username, hash,
	).Scan(&user.ID, &user.Username, &user.CreatedAt)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			c.JSON(http.StatusConflict, gin.H{"error": "username already taken"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}

	token, err := auth.GenerateToken(user.ID, user.Username, h.jwtSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"token": token,
		"user":  user,
	})
}

// Login verifies credentials and returns a JWT.
func (h *AuthHandler) Login(c *gin.Context) {
	var req authRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password are required"})
		return
	}

	var user models.User
	err := h.db.QueryRow(
		`SELECT id, username, password_hash, created_at FROM users WHERE username = $1`,
		strings.TrimSpace(req.Username),
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)
	if err == sql.ErrNoRows || !auth.CheckPassword(req.Password, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	token, err := auth.GenerateToken(user.ID, user.Username, h.jwtSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user":  user,
	})
}

// Me returns the currently authenticated user.
func (h *AuthHandler) Me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"user_id":  c.GetInt64(auth.ContextUserIDKey),
		"username": c.GetString(auth.ContextUsernameKey),
	})
}

