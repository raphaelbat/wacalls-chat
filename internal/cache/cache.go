// Package cache provides a thin pluggable interface used by the server for
// ephemeral state (avatar TTL cache), cross-instance event fan-out (SSE)
// and campaign/broadcast job queues. Two implementations are bundled:
//
//   - Memory (default): single-process in-memory map + channels. Identical
//     semantics to the historical behaviour.
//   - Redis (opt-in): activated when REDIS_URL is set. Allows running more
//     than one wacalls server instance behind a load balancer.
package cache

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache is the minimum surface used by the server.
type Cache interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Del(ctx context.Context, key string) error
	Publish(ctx context.Context, channel, payload string) error
	Subscribe(ctx context.Context, channel string) (<-chan string, func(), error)
	Close() error
}

// FromEnv returns a Redis-backed Cache when REDIS_URL is set, otherwise the
// in-memory implementation. Connection failures fall back to memory and
// surface a non-nil error so the caller can log it.
func FromEnv() (Cache, error) {
	url := strings.TrimSpace(os.Getenv("REDIS_URL"))
	if url == "" {
		return NewMemory(), nil
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		return NewMemory(), err
	}
	c := redis.NewClient(opt)
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Ping(pingCtx).Err(); err != nil {
		_ = c.Close()
		return NewMemory(), err
	}
	return &redisCache{c: c}, nil
}

// ---------------- in-memory implementation ----------------

type memEntry struct {
	value   string
	expires time.Time // zero = no expiry
}

type memCache struct {
	mu     sync.Mutex
	data   map[string]memEntry
	subsMu sync.Mutex
	subs   map[string][]chan string
}

// NewMemory returns a process-local Cache. Useful for tests and for the
// default single-node deployment.
func NewMemory() Cache {
	return &memCache{data: map[string]memEntry{}, subs: map[string][]chan string{}}
}

func (m *memCache) Get(_ context.Context, key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.data[key]
	if !ok {
		return "", false, nil
	}
	if !e.expires.IsZero() && time.Now().After(e.expires) {
		delete(m.data, key)
		return "", false, nil
	}
	return e.value, true, nil
}

func (m *memCache) Set(_ context.Context, key, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := memEntry{value: value}
	if ttl > 0 {
		e.expires = time.Now().Add(ttl)
	}
	m.data[key] = e
	return nil
}

func (m *memCache) Del(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *memCache) Publish(_ context.Context, channel, payload string) error {
	m.subsMu.Lock()
	subs := append([]chan string(nil), m.subs[channel]...)
	m.subsMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- payload:
		default:
		}
	}
	return nil
}

func (m *memCache) Subscribe(_ context.Context, channel string) (<-chan string, func(), error) {
	ch := make(chan string, 64)
	m.subsMu.Lock()
	m.subs[channel] = append(m.subs[channel], ch)
	m.subsMu.Unlock()
	cancel := func() {
		m.subsMu.Lock()
		defer m.subsMu.Unlock()
		list := m.subs[channel]
		for i, c := range list {
			if c == ch {
				m.subs[channel] = append(list[:i], list[i+1:]...)
				break
			}
		}
		close(ch)
	}
	return ch, cancel, nil
}

func (m *memCache) Close() error { return nil }

// ---------------- Redis implementation ----------------

type redisCache struct{ c *redis.Client }

func (r *redisCache) Get(ctx context.Context, key string) (string, bool, error) {
	v, err := r.c.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (r *redisCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.c.Set(ctx, key, value, ttl).Err()
}

func (r *redisCache) Del(ctx context.Context, key string) error {
	return r.c.Del(ctx, key).Err()
}

func (r *redisCache) Publish(ctx context.Context, channel, payload string) error {
	return r.c.Publish(ctx, channel, payload).Err()
}

func (r *redisCache) Subscribe(ctx context.Context, channel string) (<-chan string, func(), error) {
	ps := r.c.Subscribe(ctx, channel)
	if _, err := ps.Receive(ctx); err != nil {
		_ = ps.Close()
		return nil, nil, err
	}
	out := make(chan string, 64)
	go func() {
		defer close(out)
		ch := ps.Channel()
		for msg := range ch {
			select {
			case out <- msg.Payload:
			default:
			}
		}
	}()
	cancel := func() { _ = ps.Close() }
	return out, cancel, nil
}

func (r *redisCache) Close() error { return r.c.Close() }