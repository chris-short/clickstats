package main

import (
	"sync"
	"time"
)

type cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	ttl     time.Duration
}

type cacheEntry struct {
	value     any
	expiresAt time.Time
}

func newCache(ttl time.Duration) *cache {
	return &cache{entries: make(map[string]cacheEntry), ttl: ttl}
}

func (c *cache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.value, true
}

func (c *cache) set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{value: value, expiresAt: time.Now().Add(c.ttl)}
}
