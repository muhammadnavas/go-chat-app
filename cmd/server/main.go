package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-chat/internal/auth"
	"go-chat/internal/config"
	"go-chat/internal/db"
	"go-chat/internal/handlers"
	"go-chat/internal/hub"
	"go-chat/internal/presence"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.Load()

	conn, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database initialization failed: %v", err)
	}
	defer conn.Close()

	tracker := presence.NewTracker(cfg.RedisAddr)
	defer tracker.Close()

	chatHub := hub.NewHub(conn, tracker)
	go chatHub.Run()

	authHandler := handlers.NewAuthHandler(conn, cfg.JWTSecret)
	roomHandler := handlers.NewRoomHandler(conn, tracker)
	wsHandler := handlers.NewWSHandler(chatHub, cfg.JWTSecret)

	router := gin.Default()

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Public auth routes (rate-limited: 20 requests/min per IP)
	api := router.Group("/api")
	authLimiter := auth.RateLimitMiddleware(20, time.Minute)
	{
		api.POST("/auth/register", authLimiter, authHandler.Register)
		api.POST("/auth/login", authLimiter, authHandler.Login)
	}

	// Protected REST routes
	protected := api.Group("")
	protected.Use(auth.Middleware(cfg.JWTSecret))
	{
		protected.GET("/auth/me", authHandler.Me)
		protected.GET("/users", roomHandler.ListUsers)
		protected.GET("/rooms", roomHandler.ListRooms)
		protected.POST("/rooms", roomHandler.CreateRoom)
		protected.POST("/rooms/dm", roomHandler.CreateDirectRoom)
		protected.GET("/rooms/:id/messages", roomHandler.GetMessages)
	}

	// WebSocket endpoint
	router.GET("/ws", wsHandler.ServeWS)

	// Frontend static assets
	router.StaticFile("/", "./web/index.html")
	router.Static("/static", "./web")

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	go func() {
		log.Printf("GoChat server listening on http://localhost:%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("forced shutdown: %v", err)
	}
}
