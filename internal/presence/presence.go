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
	SetGlobalOnline(username string) error
	SetGlobalOffline(username string) error
	GlobalOnline() ([]string, error)
	SetOnline(roomID int64, username string) error
	SetOffline(roomID int64, username string) error
	OnlineInRoom(roomID int64) ([]string, error)
	Close() error
}

// NewTracker connects to Redis if available, otherwise falls back to an in-memory tracker.
func NewTracker(redisAddr string) Tracker {
	if redisAddr == "" {
		log.Println("presence: REDIS_ADDR not set, using in-memory presence tracker")
		return NewMemoryTracker()
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
		return NewMemoryTracker()
	}

	log.Printf("presence: connected to Redis at %s", redisAddr)
	return &redisTracker{client: client}
}

type redisTracker struct {
	client *redis.Client
}

const globalOnlineKey = "chat:global:online"

func roomKey(roomID int64) string {
	return fmt.Sprintf("chat:room:%d:online", roomID)
}

func (r *redisTracker) SetGlobalOnline(username string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return r.client.SAdd(ctx, globalOnlineKey, username).Err()
}

func (r *redisTracker) SetGlobalOffline(username string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return r.client.SRem(ctx, globalOnlineKey, username).Err()
}

func (r *redisTracker) GlobalOnline() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	members, err := r.client.SMembers(ctx, globalOnlineKey).Result()
	if err != nil {
		return nil, err
	}
	sort.Strings(members)
	return members, nil
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

// MemoryTracker is an in-memory implementation of Tracker used as a fallback or in tests.
type MemoryTracker struct {
	mu     sync.RWMutex
	global map[string]int
	rooms  map[int64]map[string]int
}

func NewMemoryTracker() *MemoryTracker {
	return &MemoryTracker{
		global: make(map[string]int),
		rooms:  make(map[int64]map[string]int),
	}
}

func (m *MemoryTracker) SetGlobalOnline(username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.global[username]++
	return nil
}

func (m *MemoryTracker) SetGlobalOffline(username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.global[username] <= 1 {
		delete(m.global, username)
	} else {
		m.global[username]--
	}
	return nil
}

func (m *MemoryTracker) GlobalOnline() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.global))
	for u := range m.global {
		out = append(out, u)
	}
	sort.Strings(out)
	return out, nil
}

func (m *MemoryTracker) SetOnline(roomID int64, username string) error {
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

func (m *MemoryTracker) SetOffline(roomID int64, username string) error {
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

func (m *MemoryTracker) OnlineInRoom(roomID int64) ([]string, error) {
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

func (m *MemoryTracker) Close() error {
	return nil
}
