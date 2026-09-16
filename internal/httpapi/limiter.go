package httpapi

import (
	"sync"
	"time"
)

const (
	createRequestsPerWindow = 60
	returnAttemptsPerWindow = 6
	rateLimitWindow         = time.Minute
	maxRateLimitEntries     = 4096
)

type fixedWindowLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string]rateLimitEntry
}

type rateLimitEntry struct {
	started time.Time
	count   int
}

func newFixedWindowLimiter(limit int, window time.Duration) *fixedWindowLimiter {
	return &fixedWindowLimiter{
		limit:   limit,
		window:  window,
		entries: make(map[string]rateLimitEntry),
	}
}

func (l *fixedWindowLimiter) allow(key string) bool {
	if key == "" {
		return false
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.entries[key]
	if !ok || now.Sub(entry.started) >= l.window {
		if !ok && len(l.entries) >= maxRateLimitEntries {
			if !l.evictOne(now) {
				// Do not evict an active window. Expired entries are reclaimed below.
				return false
			}
		}
		l.entries[key] = rateLimitEntry{started: now, count: 1}
		return true
	}
	if entry.count >= l.limit {
		return false
	}
	entry.count++
	l.entries[key] = entry
	return true
}

func (l *fixedWindowLimiter) evictOne(now time.Time) bool {
	// Entries are deliberately bounded; reclaim an expired window without a
	// second unbounded eviction index.
	for key, entry := range l.entries {
		if now.Sub(entry.started) < l.window {
			continue
		}
		delete(l.entries, key)
		return true
	}
	return false
}
