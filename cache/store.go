package cache

import (
	"net/http"
	"sort"
	"sync"
	"time"
)

// defaultCacheableStatusCodes lists status codes that are cacheable by default per RFC9111 Section 3.2.
// Other status codes require explicit freshness headers (e.g., max-age, Expires) to be cached.
var defaultCacheableStatusCodes = map[int]bool{
	http.StatusOK:                   true, // 200
	http.StatusNonAuthoritativeInfo: true, // 203
	http.StatusNoContent:            true, // 204
	http.StatusPartialContent:       true, // 206
	http.StatusMultipleChoices:      true, // 300
	http.StatusMovedPermanently:     true, // 301
	http.StatusPermanentRedirect:    true, // 308
	http.StatusNotFound:             true, // 404
	http.StatusMethodNotAllowed:     true, // 405
	http.StatusGone:                 true, // 410
	http.StatusRequestURITooLong:    true, // 414
	http.StatusNotImplemented:       true, // 501
}

type CachedResponse struct {
	StatusCode     int
	RequestHeader  http.Header
	ResponseHeader http.Header
	Body           []byte
	StoredAt       time.Time
	InitialAge     int
	ProtoMajor     int
	ProtoMinor     int
	Proto          string
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

// Delete removes a cached entry by key (RFC9111 Section 4.4).
func (cs *CacheStore) Delete(key string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.store, key)
}

// Comparing stored header and request header. see Section 4.1 for the detail
// Assuming whitespace removal, capital normalization are done beforehand
func HeadersMeetVaryConstraints(reqHeader, originalReqHeader, respHeader *ParsedHeaders) bool {
	vary, hasVary := respHeader.GetValue("vary")
	if !hasVary {
		return true
	}
	// If Vary header is "*", it means the response is not cacheable in the first place
	// This should not happen in practice, but we handle it just in case
	if respHeader.IsVaryWildcard() {
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

// ReqNeedsRevalidation returns true if the request requires the cache to revalidate the
// stored response with the origin server before serving (RFC9111 Section 5.2.1.4).
// Pragma: no-cache is treated as Cache-Control: no-cache when Cache-Control is absent (RFC9111 Section 5.4).
func ReqNeedsRevalidation(reqHeader *ParsedHeaders) bool {
	_, hasNoCache := reqHeader.GetDirective("Cache-Control", "no-cache")
	if hasNoCache {
		return true
	}
	// Only honour Pragma when Cache-Control is absent (RFC9111 Section 5.4)
	_, hasCacheControl := reqHeader.GetDirectives("Cache-Control")
	if !hasCacheControl {
		_, hasPragmaNoCache := reqHeader.GetDirective("Pragma", "no-cache")
		if hasPragmaNoCache {
			return true
		}
	}
	return false
}

// RespNeedsRevalidation returns true if the cached response must be revalidated before
// it is served, regardless of freshness (RFC9111 Section 5.2.2.4).
func RespNeedsRevalidation(respHeader *ParsedHeaders) bool {
	_, hasNoCache := respHeader.GetDirective("Cache-Control", "no-cache")
	return hasNoCache
}

// MustRevalidateStale returns true if a stale response MUST be revalidated and cannot be
// served even when the request would allow stale via max-stale (RFC9111 Section 5.2.2.2
// and Section 5.2.2.8).
func MustRevalidateStale(respHeader *ParsedHeaders) bool {
	_, hasMustRevalidate := respHeader.GetDirective("Cache-Control", "must-revalidate")
	if hasMustRevalidate {
		return true
	}
	_, hasProxyRevalidate := respHeader.GetDirective("Cache-Control", "proxy-revalidate")
	return hasProxyRevalidate
}

// hasExplicitFreshness returns true if the response carries explicit freshness information.
func hasExplicitFreshness(header *ParsedHeaders) bool {
	_, hasSMaxAge := header.GetDirective("Cache-Control", "s-maxage")
	_, hasMaxAge := header.GetDirective("Cache-Control", "max-age")
	_, hasExpires := header.GetValue("Expires")
	return hasSMaxAge || hasMaxAge || hasExpires
}

func IsCacheable(method string, statusCode int, header *ParsedHeaders) bool {
	if method != http.MethodGet {
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

	if header.IsVaryWildcard() {
		// Vary: * means the response is not cacheable
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

	// For non-default-cacheable status codes, require explicit freshness headers (RFC9111 Section 3.2)
	if !defaultCacheableStatusCodes[statusCode] {
		return hasExplicitFreshness(header)
	}

	return true
}

func GenerateCacheKey(urlStr string, header *ParsedHeaders) string {
	key := urlStr
	// TODO: Append Vary header
	return key
}
