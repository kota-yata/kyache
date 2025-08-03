package cache

import (
	"net/http"
	"sync"
	"time"
)

type CachedResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	StoredAt   time.Time
	InitialAge int
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

// Comparing stored header and request header. see Section 4.1 for the detail
// Assuming whitespace removal, capital normalization are done beforehand
func (cs *CacheStore) HeadersIdentical(storedHeader, incomingHeader *http.Header) bool {
	if len(*storedHeader) != len(*incomingHeader) {
		return false
	}
	storedHeaderStruct := NewParsedHeaders(*storedHeader)
	incomingHeaderStruct := NewParsedHeaders(*incomingHeader)

	if len(storedHeaderStruct.Directives) != len(incomingHeaderStruct.Directives) {
		return false
	}
	for headerName, storedDirectives := range storedHeaderStruct.Directives {
		incomingDirectives, exists := incomingHeaderStruct.Directives[headerName]
		if !exists {
			return false
		}
		if len(storedDirectives) != len(incomingDirectives) {
			return false
		}
		for directive, storedValue := range storedDirectives {
			incomingValue, exists := incomingDirectives[directive]
			if !exists || storedValue != incomingValue {
				return false
			}
		}
	}

	if len(storedHeaderStruct.Values) != len(incomingHeaderStruct.Values) {
		return false
	}
	for headerName, storedValue := range storedHeaderStruct.Values {
		incomingValue, exists := incomingHeaderStruct.Values[headerName]
		if !exists || len(storedValue) != len(incomingValue) {
			return false
		}
		// TODO: Compare multiple values
		if storedValue[0] != incomingValue[0] {
			return false
		}
	}

	return true
}

func IsCacheable(resp *http.Response) bool {
	if resp.Request.Method != http.MethodGet {
		return false
	}
	headerStruct := NewParsedHeaders(resp.Header)
	_, hasNoCache := headerStruct.GetDirective("Cache-Control", "no-cache")
	if hasNoCache {
		// TODO: Handle "no-cache" directive according to RFC 9111
		// Need interpretation of the section 5.2.1.4.
		return false
	}
	_, hasNoStore := headerStruct.GetDirective("Cache-Control", "no-store")
	if hasNoStore {
		return false
	}
	_, hasPrivate := headerStruct.GetDirective("Cache-Control", "private")
	if hasPrivate {
		return false
	}

	_, hasCdnNoCache := headerStruct.GetDirective("CDN-Cache-Control", "no-cache")
	if hasCdnNoCache {
		// TODO: Handle "no-cache" directive according to RFC 9111
		return false
	}
	_, hasCdnNoStore := headerStruct.GetDirective("CDN-Cache-Control", "no-store")
	if hasCdnNoStore {
		return false
	}
	_, hasCdnPrivate := headerStruct.GetDirective("CDN-Cache-Control", "private")
	if hasCdnPrivate {
		return false
	}
	return true
}
