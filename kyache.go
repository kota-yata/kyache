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
	cacheStore *cache.CacheStore
	transport  http.RoundTripper
}

type Config struct {
	Transport http.RoundTripper
}

func New(config *Config) *CacheServer {
	transport := config.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	return &CacheServer{
		cacheStore: cache.NewCacheStore(),
		transport:  transport,
	}
}

func (cs *CacheServer) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return cs.transport.RoundTrip(req)
	}

	key := req.URL.String()

	if cachedResp, exists := cs.cacheStore.Get(key); exists {
		if cs.isCachedResponseFresh(cachedResp) {
			return cs.createResponseFromCache(cachedResp, req), nil
		}
	}

	resp, err := cs.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if IsCacheable(resp) {
		cs.storeCachedResponse(key, resp)
	}

	return resp, nil
}

func (cs *CacheServer) isCachedResponseFresh(cachedResp *cache.CachedResponse) bool {
	headerStruct := cache.NewParsedHeaders(cachedResp.Header)
	maxAge, hasMaxAge := headerStruct.GetDirective("Cache-Control", "max-age")
	if !hasMaxAge {
		return false
	}

	maxAgeInt, err := strconv.Atoi(maxAge)
	if err != nil {
		return false
	}

	return cache.IsFresh(cachedResp.StoredAt, maxAgeInt)
}

func (cs *CacheServer) createResponseFromCache(cachedResp *cache.CachedResponse, req *http.Request) *http.Response {
	return &http.Response{
		StatusCode:    cachedResp.StatusCode,
		Header:        cachedResp.Header.Clone(),
		Body:          io.NopCloser(bytes.NewReader(cachedResp.Body)),
		ContentLength: int64(len(cachedResp.Body)),
		Request:       req,
		ProtoMajor:    1,
		ProtoMinor:    1,
		Proto:         "HTTP/1.1",
	}
}

func (cs *CacheServer) storeCachedResponse(key string, resp *http.Response) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read response body for caching: %v", err)
		return
	}
	resp.Body.Close()

	resp.Body = io.NopCloser(bytes.NewReader(body))

	cached := &cache.CachedResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       body,
		StoredAt:   time.Now(),
	}
	cs.cacheStore.Set(key, cached)
}

func (cs *CacheServer) Handler(originURL *url.URL) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	if !cs.isCachedResponseFresh(cachedResp) {
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

	if IsCacheable(resp) {
		cs.cacheResponse(r.URL.String(), resp, body)
	}
}

func IsCacheable(resp *http.Response) bool {
	if resp.Request.Method != http.MethodGet {
		return false
	}
	headerStruct := cache.NewParsedHeaders(resp.Header)
	_, hasNoCache := headerStruct.GetDirective("Cache-Control", "no-cache")
	if hasNoCache {
		// TODO: Handle "no-cache" directive according to RFC 9111
		// For now, we treat it as not cacheable
		return false
	}
	_, hasNoStore := headerStruct.GetDirective("Cache-Control", "no-store")
	if hasNoStore {
		return false // If "no-store" is present, the response is not cacheable
	}
	_, hasPrivate := headerStruct.GetDirective("Cache-Control", "private")
	return !hasPrivate // If "private" is present, the response is cacheable only for the user
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

func (cs *CacheServer) cacheResponse(key string, resp *http.Response, body []byte) {
	cached := &cache.CachedResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       body,
		StoredAt:   time.Now(),
	}
	cs.cacheStore.Set(key, cached)
}

func (cs *CacheServer) writeCachedResponse(w http.ResponseWriter, cachedResp *cache.CachedResponse) {
	cs.copyHeadersFromCache(w, cachedResp)
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
	for k, vals := range cachedResp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
}
