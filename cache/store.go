package cache

import (
	"log"
	"net/http"
	"sort"
	"sync"
	"time"
)

type CachedResponse struct {
	StatusCode     int
	RequestHeader  http.Header
	ResponseHeader http.Header
	Body           []byte
	StoredAt       time.Time
	InitialAge     int
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
func HeadersMeetVaryConstraints(reqHeader, originalReqHeader, respHeader *ParsedHeaders) bool {
	vary, hasVary := respHeader.GetValue("vary")
	if !hasVary {
		return true
	}
	log.Printf("Vary header found: %v", vary)
	// If Vary header is "*", it means the response is not cacheable in the first place
	// This should not happen in practice, but we handle it just in case
	if vary[0] == "*" {
		return false
	}
	for _, field := range vary {
		reqVal, reqOk := reqHeader.GetValue(field)
		respVal, respOk := originalReqHeader.GetValue(field)
		if !reqOk && !respOk {
			continue
		}
		if reqOk != respOk {
			return false
		}
		if len(reqVal) != len(respVal) {
			return false
		}
		sort.Strings(reqVal)
		sort.Strings(respVal)
		for i := range reqVal {
			if reqVal[i] != respVal[i] {
				return false
			}
		}
	}
	return true
}

func IsReqAllowedToUseCache(reqHeader, originalReqHeader, respHeader *ParsedHeaders) bool {
	// When Authorization header is present, the request cannot be responded with cache unless
	// any of public, must-revalidate, or s-maxage directive is present in the response header.
	_, reqHasAuthorization := reqHeader.GetDirectives("Authorization")
	if reqHasAuthorization {
		_, respHasPublic := respHeader.GetDirective("Cache-Control", "public")
		_, respHasMustRevalidate := respHeader.GetDirective("Cache-Control", "must-revalidate")
		_, respHasSMaxAge := respHeader.GetDirective("Cache-Control", "s-maxage")
		if !respHasPublic && !respHasMustRevalidate && !respHasSMaxAge {
			return false
		}
	}

	if !HeadersMeetVaryConstraints(reqHeader, originalReqHeader, respHeader) {
		return false
	}
	return true
}

func IsCacheable(method string, header *ParsedHeaders) bool {
	if method != http.MethodGet {
		return false
	}
	_, hasNoCache := header.GetDirective("Cache-Control", "no-cache")
	if hasNoCache {
		// TODO: Handle "no-cache" directive according to RFC 9111
		// Need interpretation of the section 5.2.1.4.
		return false
	}
	_, hasNoStore := header.GetDirective("Cache-Control", "no-store")
	if hasNoStore {
		return false
	}
	_, hasPrivate := header.GetDirective("Cache-Control", "private")
	if hasPrivate {
		return false
	}

	vary, hasVary := header.GetValue("Vary")
	if hasVary && vary[0] == "*" {
		// Vary: * means the response is not cacheable
		return false
	}

	_, hasCdnNoCache := header.GetDirective("CDN-Cache-Control", "no-cache")
	if hasCdnNoCache {
		// TODO: Handle "no-cache" directive according to RFC 9111
		return false
	}
	_, hasCdnNoStore := header.GetDirective("CDN-Cache-Control", "no-store")
	if hasCdnNoStore {
		return false
	}
	_, hasCdnPrivate := header.GetDirective("CDN-Cache-Control", "private")
	if hasCdnPrivate {
		return false
	}

	return true
}

func GenerateCacheKey(urlStr string, header *ParsedHeaders) string {
	key := urlStr
	// TODO: Append Vary header
	return key
}
