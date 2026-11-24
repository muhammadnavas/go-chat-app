# GoChat — Real-Time Chat Application

A real-time multi-room chat application built with **Go**, **Gin**, **WebSockets**, **Goroutines & Channels**, **PostgreSQL**, **Redis**, and **HTML/CSS/JavaScript**.

## Features
- **User Authentication**: Register and log in with bcrypt-hashed passwords and JWT tokens.
- **Chat Rooms**: Default seeded rooms (`#general`, `#tech`, `#random`) plus creating and switching rooms.
- **Real-Time Messaging**: Instant message delivery via WebSockets (`gorilla/websocket`), coordinated by a `Hub` using goroutines and channels.
- **Chat History**: Messages are persisted in PostgreSQL and loaded when joining or switching rooms.
- **Online Presence**: Tracks online users per room in Redis (with automatic in-memory fallback if Redis is not configured/available) and broadcasts live presence updates.

## Architecture
- `cmd/server/main.go`: Application entry point, Gin router, and graceful shutdown.
- `internal/config/config.go`: Environment configuration (`PORT`, `DATABASE_URL`, `REDIS_ADDR`, `JWT_SECRET`).
- `internal/db/db.go`: PostgreSQL connection, schema migrations, and default room seeding.
- `internal/auth/auth.go`: Password hashing, JWT generation/validation, and Gin auth middleware.
- `internal/presence/presence.go`: `Tracker` interface with Redis (`SADD`/`SREM`/`SMEMBERS`) and in-memory fallback.
- `internal/hub/hub.go`: WebSocket `Hub` and `Client` (`ReadPump`, `WritePump`, `Run` loop with `register`, `unregister`, and `inbound` channels).
- `internal/handlers/`: REST handlers (`auth_handler.go`, `room_handler.go`) and WebSocket upgrade handler (`ws_handler.go`).
- `web/`: Frontend (`index.html`, `style.css`, `app.js`).

## Quick Start (Docker Compose)
```bash
docker compose up --build
```
Then open [http://localhost:8080](http://localhost:8080) in two browser tabs, register two users, and start chatting in real time.

## Running Locally (without Docker)
Ensure Go 1.22+ and PostgreSQL (plus optional Redis) are running, then:
```bash
go mod tidy
go run ./cmd/server
```
Environment variables:
- `PORT` (default `8080`)
- `DATABASE_URL` (default `postgres://postgres:postgres@localhost:5432/gochat?sslmode=disable`)
- `REDIS_ADDR` (default `localhost:6379`; falls back to in-memory tracking if unreachable)
- `JWT_SECRET` (default `dev-secret-change-in-production`)

## API Endpoints
- `POST /api/auth/register` — Register a new user
- `POST /api/auth/login` — Log in and receive a JWT
- `GET /api/auth/me` — Get current user (requires `Authorization: Bearer <token>`)
- `GET /api/rooms` — List chat rooms (requires auth)
- `POST /api/rooms` — Create a chat room (requires auth)
- `GET /api/rooms/:id/messages` — Fetch recent room messages and online users (requires auth)
- `GET /ws?token=<jwt>&room_id=<id>` — WebSocket connection

"# go-chat-app" 
