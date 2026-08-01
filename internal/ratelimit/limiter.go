// Package ratelimit: simple in-memory IP + address rate limiting for Gateway HTTP.
package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type bucket struct {
	count   int
	resetAt time.Time
}

// Limiter is a sliding fixed-window counter keyed by string.
type Limiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	limit    int
	window   time.Duration
}

func New(limit int, window time.Duration) *Limiter {
	if limit <= 0 {
		limit = 30
	}
	if window <= 0 {
		window = time.Minute
	}
	return &Limiter{
		buckets: make(map[string]*bucket),
		limit:   limit,
		window:  window,
	}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[key]
	if !ok || now.After(b.resetAt) {
		l.buckets[key] = &bucket{count: 1, resetAt: now.Add(l.window)}
		return true
	}
	if b.count >= l.limit {
		return false
	}
	b.count++
	return true
}

// ClientIP extracts a best-effort client IP.
func ClientIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// DeviceID reads optional client device id header.
func DeviceID(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Platarium-Device-Id"))
}
