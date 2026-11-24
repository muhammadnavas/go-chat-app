package presence

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Tracker manages online user presence globally and per chat room.
type Tracker interface {
	SetOnline(roomID int64, username string) error
	SetOffline(roomID int64, username string) error
	OnlineInRoom(roomID int64) ([]string, error)
	Close() error
}

// NewTracker connects to Redis if available, otherwise falls back to an in-memory tracker.
func NewTracker(redisAddr string) Tracker {
	if redisAddr == "" {
		log.Println("presence: REDIS_ADDR not set, using in-memory presence tracker")
		return newMemoryTracker()
	}

	client := redis.NewClient(&redis.Options{
		Addr:        redisAddr,
		DialTimeout: 2 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("presence: Redis unavailable (%v), falling back to in-memory tracker", err)
		_ = client.Close()
		return newMemoryTracker()
	}

	log.Printf("presence: connected to Redis at %s", redisAddr)
	return &redisTracker{client: client}
}

type redisTracker struct {
	client *redis.Client
}

func roomKey(roomID int64) string {
	return fmt.Sprintf("chat:room:%d:online", roomID)
}

func (r *redisTracker) SetOnline(roomID int64, username string) error {
	if roomID <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return r.client.SAdd(ctx, roomKey(roomID), username).Err()
}

func (r *redisTracker) SetOffline(roomID int64, username string) error {
	if roomID <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return r.client.SRem(ctx, roomKey(roomID), username).Err()
}

func (r *redisTracker) OnlineInRoom(roomID int64) ([]string, error) {
	if roomID <= 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	members, err := r.client.SMembers(ctx, roomKey(roomID)).Result()
	if err != nil {
		return nil, err
	}
	sort.Strings(members)
	return members, nil
}

func (r *redisTracker) Close() error {
	return r.client.Close()
}

type memoryTracker struct {
	mu    sync.RWMutex
	rooms map[int64]map[string]int
}

func newMemoryTracker() *memoryTracker {
	return &memoryTracker{
		rooms: make(map[int64]map[string]int),
	}
}

func (m *memoryTracker) SetOnline(roomID int64, username string) error {
	if roomID <= 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rooms[roomID]; !ok {
		m.rooms[roomID] = make(map[string]int)
	}
	m.rooms[roomID][username]++
	return nil
}

func (m *memoryTracker) SetOffline(roomID int64, username string) error {
	if roomID <= 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	users, ok := m.rooms[roomID]
	if !ok {
		return nil
	}
	if users[username] <= 1 {
		delete(users, username)
	} else {
		users[username]--
	}
	if len(users) == 0 {
		delete(m.rooms, roomID)
	}
	return nil
}

func (m *memoryTracker) OnlineInRoom(roomID int64) ([]string, error) {
	if roomID <= 0 {
		return nil, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	users := m.rooms[roomID]
	out := make([]string, 0, len(users))
	for u := range users {
		out = append(out, u)
	}
	sort.Strings(out)
	return out, nil
}

func (m *memoryTracker) Close() error {
	return nil
}

