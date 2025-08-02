package cache

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

type CachedResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	StoredAt   time.Time
}

type CacheStore struct {
	mu    sync.RWMutex
	store map[string]*CachedResponse
}

func NewCacheStore() *CacheStore {
	return &CacheStore{store: make(map[string]*CachedResponse)}
}

func (cs *CacheStore) Get(key string) (*CachedResponse, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	resp, ok := cs.store[key]
	return resp, ok
}

func (cs *CacheStore) Set(key string, resp *CachedResponse) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.store[key] = resp
}

func IsFresh(storedAt time.Time, maxAge int) bool {
	if maxAge <= 0 {
		return false
	}
	elapsed := time.Since(storedAt)
	return elapsed < time.Duration(maxAge)*time.Second
}

// Simple check based on Section 3, 5.2.2
func IsCacheable(resp *http.Response) bool {
	if resp.Request.Method != http.MethodGet {
		return false
	}
	cc := resp.Header.Get("Cache-Control")
	return !strings.Contains(cc, "no-store")
}