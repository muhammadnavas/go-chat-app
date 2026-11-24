package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// Connect opens a PostgreSQL connection with retries and runs schema migrations.
func Connect(databaseURL string) (*sql.DB, error) {
	var conn *sql.DB
	var err error

	for i := 0; i < 10; i++ {
		conn, err = sql.Open("postgres", databaseURL)
		if err == nil {
			err = conn.Ping()
			if err == nil {
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	conn.SetMaxOpenConns(25)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(5 * time.Minute)

	if err := migrate(conn); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return conn, nil
}

func migrate(conn *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		username VARCHAR(50) UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS rooms (
		id SERIAL PRIMARY KEY,
		name VARCHAR(64) UNIQUE NOT NULL,
		created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS messages (
		id SERIAL PRIMARY KEY,
		room_id BIGINT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
		user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		content TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_messages_room_created ON messages(room_id, created_at DESC);
	`
	if _, err := conn.Exec(schema); err != nil {
		return err
	}

	// Seed default chat rooms if they don't exist.
	defaultRooms := []string{"general", "tech", "random"}
	for _, name := range defaultRooms {
		if _, err := conn.Exec(
			`INSERT INTO rooms (name) VALUES ($1) ON CONFLICT (name) DO NOTHING`,
			name,
		); err != nil {
			return err
		}
	}

	return nil
}

