package kyache

import (
	"bytes"
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/kota-yata/kyache/cache"
	"github.com/quic-go/quic-go/http3"
)

type CacheServer struct {
	cacheStore   *cache.CacheStore
	transport    http.RoundTripper
	pathHandlers map[string]http.HandlerFunc
}

type Config struct {
	Transport   http.RoundTripper
	EnableHTTP3 bool
	TLSConfig   *tls.Config
}

func New(config *Config) *CacheServer {
	transport := config.Transport
	if transport == nil {
		if config.EnableHTTP3 {
			tlsConfig := config.TLSConfig
			if tlsConfig == nil {
				tlsConfig = &tls.Config{
					InsecureSkipVerify: true,
				}
			}
			transport = &http3.Transport{
				TLSClientConfig: tlsConfig,
			}
		} else {
			transport = http.DefaultTransport
		}
	}

	cs := &CacheServer{
		cacheStore:   cache.NewCacheStore(),
		transport:    transport,
		pathHandlers: make(map[string]http.HandlerFunc),
	}

	cs.RegisterPath("/statusz", cs.handleStatus)
	return cs
}

func (cs *CacheServer) RoundTrip(req *http.Request) (*http.Response, error) {
	// Unsafe methods: proxy to origin and invalidate related cache entries (RFC9111 Section 4.4)
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return cs.proxyAndInvalidate(req)
	}

	reqHeaders := cache.NewParsedHeaders(req.Header)

	_, reqHasOnlyIfCached := reqHeaders.GetDirective("Cache-Control", "only-if-cached")
	_, reqHasNoStore := reqHeaders.GetDirective("Cache-Control", "no-store")

	key := cache.GenerateCacheKey(req.URL.String(), reqHeaders)

	if cachedResp, exists := cs.cacheStore.Get(key); exists {
		origReqHeaders := cache.NewParsedHeaders(cachedResp.RequestHeader)
		respHeaders := cache.NewParsedHeaders(cachedResp.ResponseHeader)

		if cache.IsReqAllowedToUseCache(reqHeaders, origReqHeaders, respHeaders) {
			isFresh := cache.IsFresh(cachedResp)
			reqNeedsReval := cache.ReqNeedsRevalidation(reqHeaders)
			respNeedsReval := cache.RespNeedsRevalidation(respHeaders)

			if !reqNeedsReval && !respNeedsReval {
				// Fresh response that satisfies request freshness constraints
				if isFresh && cache.IsFreshEnoughForRequest(cachedResp, reqHeaders) {
					return cs.createResponseFromCache(cachedResp, req), nil
				}
				// Stale response acceptable via max-stale (unless must-revalidate forbids it)
				if !isFresh && !cache.MustRevalidateStale(respHeaders) && cache.CanServeStaleForRequest(cachedResp, reqHeaders) {
					return cs.createResponseFromCache(cachedResp, req), nil
				}
			}

			// Revalidation required but only-if-cached prevents going to origin
			if reqHasOnlyIfCached {
				return gatewayTimeoutResponse(req), nil
			}

			// Attempt conditional revalidation with origin (RFC9111 Section 4.3)
			conditionalReq := cs.buildConditionalRequest(req, cachedResp)
			resp, err := cs.transport.RoundTrip(conditionalReq)
			if err != nil {
				return nil, err
			}

			if resp.StatusCode == http.StatusNotModified {
				// 304: update stored headers and serve cached body (RFC9111 Section 4.3.4)
				cs.updateCachedResponse(key, cachedResp, resp)
				resp.Body.Close()
				return cs.createResponseFromCache(cachedResp, req), nil
			}

			// Got a new response; cache it if appropriate
			if !reqHasNoStore {
				respHeaderStruct := cache.NewParsedHeaders(resp.Header)
				if cache.IsCacheable(req.Method, resp.StatusCode, respHeaderStruct) {
					cs.cacheResponseFromReader(key, req, resp, respHeaderStruct)
				}
			}
			return resp, nil
		}
	}

	// No usable cached response
	if reqHasOnlyIfCached {
		return gatewayTimeoutResponse(req), nil
	}

	resp, err := cs.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if !reqHasNoStore {
		respHeaderStruct := cache.NewParsedHeaders(resp.Header)
		if cache.IsCacheable(resp.Request.Method, resp.StatusCode, respHeaderStruct) {
			cs.cacheResponseFromReader(key, req, resp, respHeaderStruct)
		}
	}

	return resp, nil
}

// proxyAndInvalidate forwards an unsafe-method request to the origin and invalidates
// any cached entries for affected URLs (RFC9111 Section 4.4).
func (cs *CacheServer) proxyAndInvalidate(req *http.Request) (*http.Response, error) {
	resp, err := cs.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	// Invalidate on successful (2xx/3xx) responses
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		cs.invalidateRelatedURLs(req.URL.String(), resp)
	}
	return resp, nil
}

// invalidateRelatedURLs removes cached entries for the target URL and any URLs
// referenced in Location / Content-Location response headers (RFC9111 Section 4.4).
func (cs *CacheServer) invalidateRelatedURLs(targetURL string, resp *http.Response) {
	cs.cacheStore.Delete(targetURL)
	if loc := resp.Header.Get("Location"); loc != "" {
		cs.cacheStore.Delete(loc)
	}
	if cl := resp.Header.Get("Content-Location"); cl != "" {
		cs.cacheStore.Delete(cl)
	}
}

// buildConditionalRequest constructs a conditional request using ETag and/or Last-Modified
// from the cached response (RFC9111 Section 4.3.1).
func (cs *CacheServer) buildConditionalRequest(req *http.Request, cachedResp *cache.CachedResponse) *http.Request {
	conditionalReq := req.Clone(req.Context())

	etag := cachedResp.ResponseHeader.Get("ETag")
	if etag != "" {
		conditionalReq.Header.Set("If-None-Match", etag)
	}

	lastModified := cachedResp.ResponseHeader.Get("Last-Modified")
	// Use If-Modified-Since only when there is no ETag (ETag takes precedence)
	if lastModified != "" && etag == "" {
		conditionalReq.Header.Set("If-Modified-Since", lastModified)
	}

	return conditionalReq
}

// updateCachedResponse refreshes the stored response's headers and timestamps
// after receiving a 304 Not Modified response (RFC9111 Section 4.3.4).
func (cs *CacheServer) updateCachedResponse(key string, cached *cache.CachedResponse, resp *http.Response) {
	for k, vals := range resp.Header {
		if len(vals) > 0 {
			cached.ResponseHeader[k] = vals
		}
	}
	cached.StoredAt = time.Now()
	newHeaders := cache.NewParsedHeaders(resp.Header)
	cached.InitialAge = newHeaders.GetValidatedAge()
}

// gatewayTimeoutResponse returns a 504 response for use with only-if-cached (RFC9111 Section 5.2.1.7).
func gatewayTimeoutResponse(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusGatewayTimeout,
		Status:     "504 Gateway Timeout",
		Body:       io.NopCloser(bytes.NewReader(nil)),
		Header:     make(http.Header),
		Request:    req,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}
}

func (cs *CacheServer) createResponseFromCache(cachedResp *cache.CachedResponse, req *http.Request) *http.Response {
	header := cachedResp.ResponseHeader.Clone()
	header.Set("Age", strconv.Itoa(cache.GetCurrentAge(cachedResp)))

	// Use stored protocol information, fallback to HTTP/1.1 if not available
	protoMajor, protoMinor, proto := cachedResp.ProtoMajor, cachedResp.ProtoMinor, cachedResp.Proto
	if proto == "" {
		protoMajor, protoMinor, proto = 1, 1, "HTTP/1.1"
	}

	// HEAD requests share cached GET entries but must not include a body
	var body io.ReadCloser
	var contentLength int64
	if req.Method == http.MethodHead {
		body = io.NopCloser(bytes.NewReader(nil))
		contentLength = 0
	} else {
		body = io.NopCloser(bytes.NewReader(cachedResp.Body))
		contentLength = int64(len(cachedResp.Body))
	}

	return &http.Response{
		StatusCode:    cachedResp.StatusCode,
		Header:        header,
		Body:          body,
		ContentLength: contentLength,
		Request:       req,
		ProtoMajor:    protoMajor,
		ProtoMinor:    protoMinor,
		Proto:         proto,
	}
}

func (cs *CacheServer) cacheResponseFromReader(key string, req *http.Request, resp *http.Response, header *cache.ParsedHeaders) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read response body for caching: %v", err)
		return
	}
	resp.Body.Close()

	resp.Body = io.NopCloser(bytes.NewReader(body))

	cs.cacheResponse(key, req, resp, header, body)
}

func (cs *CacheServer) Handler(originURL *url.URL) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handler, exists := cs.pathHandlers[r.URL.Path]; exists {
			handler(w, r)
			return
		}

		// Only GET and HEAD are cacheable; unsafe methods invalidate cache
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			cs.proxyToOriginAndInvalidate(w, r, originURL)
			return
		}

		reqHeaders := cache.NewParsedHeaders(r.Header)
		_, hasOnlyIfCached := reqHeaders.GetDirective("Cache-Control", "only-if-cached")

		if cs.serveCachedResponse(w, r, reqHeaders) {
			return
		}

		if hasOnlyIfCached {
			http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
			return
		}

		cs.fetchAndCache(w, r, originURL, reqHeaders)
	})
}

// proxyToOriginAndInvalidate forwards an unsafe request and invalidates affected cache entries.
func (cs *CacheServer) proxyToOriginAndInvalidate(w http.ResponseWriter, r *http.Request, originURL *url.URL) {
	req := cs.buildOriginRequest(r, originURL)
	resp, err := cs.transport.RoundTrip(req)
	if err != nil {
		log.Printf("Origin fetch failed for %s: %v", req.URL.String(), err)
		http.Error(w, "Origin fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Invalidate related cache entries on successful unsafe requests (RFC9111 Section 4.4)
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		cs.invalidateRelatedURLs(r.URL.String(), resp)
	}

	cs.copyResponse(w, resp)
}

func (cs *CacheServer) serveCachedResponse(w http.ResponseWriter, r *http.Request, reqHeaders *cache.ParsedHeaders) bool {
	key := r.URL.String()

	cachedResp, exists := cs.cacheStore.Get(key)
	if !exists {
		return false
	}

	originalReqHeaderStruct := cache.NewParsedHeaders(cachedResp.RequestHeader)
	respHeader := cache.NewParsedHeaders(cachedResp.ResponseHeader)

	if !cache.IsReqAllowedToUseCache(reqHeaders, originalReqHeaderStruct, respHeader) {
		return false
	}

	isFresh := cache.IsFresh(cachedResp)
	reqNeedsReval := cache.ReqNeedsRevalidation(reqHeaders)
	respNeedsReval := cache.RespNeedsRevalidation(respHeader)

	if !reqNeedsReval && !respNeedsReval {
		if isFresh && cache.IsFreshEnoughForRequest(cachedResp, reqHeaders) {
			cs.writeCachedResponse(w, cachedResp, r.Method)
			return true
		}
		// Stale but max-stale permits serving without revalidation
		if !isFresh && !cache.MustRevalidateStale(respHeader) && cache.CanServeStaleForRequest(cachedResp, reqHeaders) {
			cs.writeCachedResponse(w, cachedResp, r.Method)
			return true
		}
	}

	return false
}

func (cs *CacheServer) fetchAndCache(w http.ResponseWriter, r *http.Request, originURL *url.URL, reqHeaders *cache.ParsedHeaders) {
	req := cs.buildOriginRequest(r, originURL)

	// If there is a stale cached response, attempt a conditional request (RFC9111 Section 4.3)
	key := r.URL.String()
	if cachedResp, exists := cs.cacheStore.Get(key); exists {
		etag := cachedResp.ResponseHeader.Get("ETag")
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		lastModified := cachedResp.ResponseHeader.Get("Last-Modified")
		if lastModified != "" && etag == "" {
			req.Header.Set("If-Modified-Since", lastModified)
		}

		resp, err := cs.transport.RoundTrip(req)
		if err != nil {
			log.Printf("Origin fetch failed for %s: %v", req.URL.String(), err)
			http.Error(w, "Origin fetch failed", http.StatusBadGateway)
			return
		}

		if resp.StatusCode == http.StatusNotModified {
			resp.Body.Close()
			cs.updateCachedResponse(key, cachedResp, resp)
			cs.writeCachedResponse(w, cachedResp, r.Method)
			return
		}

		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Failed to read response body for %s: %v", req.URL.String(), err)
			http.Error(w, "Failed to read response body", http.StatusInternalServerError)
			return
		}

		cs.copyHeaders(w, resp)
		w.WriteHeader(resp.StatusCode)
		w.Write(body)

		_, reqHasNoStore := reqHeaders.GetDirective("Cache-Control", "no-store")
		headerStruct := cache.NewParsedHeaders(resp.Header)
		if !reqHasNoStore && cache.IsCacheable(resp.Request.Method, resp.StatusCode, headerStruct) {
			cacheKey := cache.GenerateCacheKey(r.URL.String(), headerStruct)
			cs.cacheResponse(cacheKey, r, resp, headerStruct, body)
		}
		return
	}

	// No cached response at all – plain fetch
	resp, err := cs.transport.RoundTrip(req)
	if err != nil {
		log.Printf("Origin fetch failed for %s: %v", req.URL.String(), err)
		http.Error(w, "Origin fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read response body for %s: %v", req.URL.String(), err)
		http.Error(w, "Failed to read response body", http.StatusInternalServerError)
		return
	}

	cs.copyHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	w.Write(body)

	_, reqHasNoStore := reqHeaders.GetDirective("Cache-Control", "no-store")
	headerStruct := cache.NewParsedHeaders(resp.Header)
	if !reqHasNoStore && cache.IsCacheable(resp.Request.Method, resp.StatusCode, headerStruct) {
		cacheKey := cache.GenerateCacheKey(r.URL.String(), headerStruct)
		cs.cacheResponse(cacheKey, r, resp, headerStruct, body)
	}
}

func (cs *CacheServer) buildOriginRequest(r *http.Request, originURL *url.URL) *http.Request {
	req := r.Clone(r.Context())
	req.RequestURI = ""
	req.URL.Scheme = originURL.Scheme
	req.URL.Host = originURL.Host
	req.URL.Path = r.URL.Path
	req.URL.RawQuery = r.URL.RawQuery
	req.Host = originURL.Host
	return req
}

func (cs *CacheServer) cacheResponse(key string, req *http.Request, resp *http.Response, header *cache.ParsedHeaders, body []byte) {
	age := header.GetValidatedAge()
	cached := &cache.CachedResponse{
		StatusCode:     resp.StatusCode,
		RequestHeader:  req.Header.Clone(),
		ResponseHeader: resp.Header.Clone(),
		Body:           body,
		StoredAt:       time.Now(),
		InitialAge:     age,
		ProtoMajor:     resp.ProtoMajor,
		ProtoMinor:     resp.ProtoMinor,
		Proto:          resp.Proto,
	}
	cs.cacheStore.Set(key, cached)
}

func (cs *CacheServer) writeCachedResponse(w http.ResponseWriter, cachedResp *cache.CachedResponse, method string) {
	age := cache.GetCurrentAge(cachedResp)
	cs.copyHeadersFromCache(w, cachedResp)
	w.Header().Set("Age", strconv.Itoa(age))
	w.WriteHeader(cachedResp.StatusCode)
	// HEAD responses share the GET cache entry but must not include a body
	if method != http.MethodHead {
		w.Write(cachedResp.Body)
	}
}

func (cs *CacheServer) copyResponse(w http.ResponseWriter, resp *http.Response) {
	cs.copyHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (cs *CacheServer) copyHeaders(w http.ResponseWriter, resp *http.Response) {
	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
}

func (cs *CacheServer) copyHeadersFromCache(w http.ResponseWriter, cachedResp *cache.CachedResponse) {
	for k, vals := range cachedResp.ResponseHeader {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
}

func (cs *CacheServer) RegisterPath(path string, handler http.HandlerFunc) {
	cs.pathHandlers[path] = handler
}

// default status handler for security camp 2025
func (cs *CacheServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","cache":"running"}`))
}
