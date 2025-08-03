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
		if cache.IsFresh(cachedResp) {
			return cs.createResponseFromCache(cachedResp, req), nil
		}
	}

	resp, err := cs.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	respHeaderStruct := cache.NewParsedHeaders(resp.Header)

	if cache.IsCacheable(resp.Request.Method, respHeaderStruct) {
		cs.cacheResponseFromReader(key, resp, respHeaderStruct)
	}

	return resp, nil
}

func (cs *CacheServer) createResponseFromCache(cachedResp *cache.CachedResponse, req *http.Request) *http.Response {
	return &http.Response{
		StatusCode:    cachedResp.StatusCode,
		Header:        cachedResp.Header.Clone(),
		Body:          io.NopCloser(bytes.NewReader(cachedResp.Body)),
		ContentLength: int64(len(cachedResp.Body)),
		Request:       req,
		ProtoMajor:    2,
		ProtoMinor:    0,
		Proto:         "HTTP/2.0",
	}
}

func (cs *CacheServer) cacheResponseFromReader(key string, resp *http.Response) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read response body for caching: %v", err)
		return
	}
	resp.Body.Close()

	resp.Body = io.NopCloser(bytes.NewReader(body))

	cs.cacheResponse(key, resp, body)
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

	if !cache.IsFresh(cachedResp) {
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

	if cache.IsCacheable(resp) {
		cs.cacheResponse(r.URL.String(), resp, body)
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

func (cs *CacheServer) cacheResponse(key string, resp *http.Response, body []byte) {
	age, err := strconv.Atoi(resp.Header.Get("Age"))
	if err != nil || age < 0 {
		age = 0
	}
	cached := &cache.CachedResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       body,
		StoredAt:   time.Now(),
		InitialAge: age,
	}
	cs.cacheStore.Set(key, cached)
}

func (cs *CacheServer) writeCachedResponse(w http.ResponseWriter, cachedResp *cache.CachedResponse) {
	age := cache.GetAge(cachedResp)
	cs.copyHeadersFromCache(w, cachedResp)
	w.WriteHeader(cachedResp.StatusCode)
	w.Header().Set("Age", strconv.Itoa(age))
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
