package kyache

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/kota-yata/kyache/cache"
)

type CacheServer struct {
	cacheStore   *cache.CacheStore
	transport    http.RoundTripper
	pathHandlers map[string]http.HandlerFunc
}

type Config struct {
	Transport http.RoundTripper
}

func New(config *Config) *CacheServer {
	transport := config.Transport
	if transport == nil {
		transport = http.DefaultTransport
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
	if req.Method != http.MethodGet {
		return cs.transport.RoundTrip(req)
	}

	reqHeaderStruct := cache.NewParsedHeaders(req.Header)
	key := cache.GenerateCacheKey(req.URL.String(), reqHeaderStruct)

	if cachedResp, exists := cs.cacheStore.Get(key); exists {
		respHeader := cache.NewParsedHeaders(cachedResp.ResponseHeader)
		originalReqHeaderStruct := cache.NewParsedHeaders(cachedResp.RequestHeader)
		if cache.IsFresh(cachedResp) && cache.IsReqAllowedToUseCache(reqHeaderStruct, originalReqHeaderStruct, respHeader) {
			return cs.createResponseFromCache(cachedResp, req), nil
		}
	}

	resp, err := cs.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	respHeaderStruct := cache.NewParsedHeaders(resp.Header)

	if cache.IsCacheable(resp.Request.Method, respHeaderStruct) {
		cs.cacheResponseFromReader(key, req, resp, respHeaderStruct)
	}

	return resp, nil
}

func (cs *CacheServer) createResponseFromCache(cachedResp *cache.CachedResponse, req *http.Request) *http.Response {
	header := cachedResp.RequestHeader.Clone()
	header.Set("Age", strconv.Itoa(cache.GetCurrentAge(cachedResp)))
	return &http.Response{
		StatusCode:    cachedResp.StatusCode,
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(cachedResp.Body)),
		ContentLength: int64(len(cachedResp.Body)),
		Request:       req,
		ProtoMajor:    1,
		ProtoMinor:    1,
		Proto:         "HTTP/1.1",
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

		if r.Method != http.MethodGet {
			cs.proxyToOrigin(w, r, originURL)
			return
		}

		if cs.serveCachedResponse(w, r) {
			return
		}

		cs.fetchAndCache(w, r, originURL)
	})
}

func (cs *CacheServer) proxyToOrigin(w http.ResponseWriter, r *http.Request, originURL *url.URL) {
	req := cs.buildOriginRequest(r, originURL)
	resp, err := cs.transport.RoundTrip(req)
	if err != nil {
		log.Printf("Origin fetch failed for %s: %v", req.URL.String(), err)
		http.Error(w, "Origin fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	cs.copyResponse(w, resp)
}

func (cs *CacheServer) serveCachedResponse(w http.ResponseWriter, r *http.Request) bool {
	key := r.URL.String()

	cachedResp, exists := cs.cacheStore.Get(key)
	if !exists {
		return false
	}

	reqHeaderStruct := cache.NewParsedHeaders(r.Header)
	originalReqHeaderStruct := cache.NewParsedHeaders(cachedResp.RequestHeader)
	respHeader := cache.NewParsedHeaders(cachedResp.ResponseHeader)

	if !cache.IsFresh(cachedResp) || !cache.IsReqAllowedToUseCache(reqHeaderStruct, originalReqHeaderStruct, respHeader) {
		return false
	}

	cs.writeCachedResponse(w, cachedResp)
	return true
}

func (cs *CacheServer) fetchAndCache(w http.ResponseWriter, r *http.Request, originURL *url.URL) {
	req := cs.buildOriginRequest(r, originURL)

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

	headerStruct := cache.NewParsedHeaders(resp.Header)

	if cache.IsCacheable(resp.Request.Method, headerStruct) {
		key := cache.GenerateCacheKey(r.URL.String(), headerStruct)
		cs.cacheResponse(key, r, resp, headerStruct, body)
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
	}
	cs.cacheStore.Set(key, cached)
}

func (cs *CacheServer) writeCachedResponse(w http.ResponseWriter, cachedResp *cache.CachedResponse) {
	age := cache.GetCurrentAge(cachedResp)
	cs.copyHeadersFromCache(w, cachedResp)
	w.Header().Set("Age", strconv.Itoa(age))
	w.WriteHeader(cachedResp.StatusCode)
	w.Write(cachedResp.Body)
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
