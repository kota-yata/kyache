package kyache

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestCustomPathHandler(t *testing.T) {
	originURL, _ := url.Parse("http://example.com")
	cs := New(&Config{})
	handler := cs.Handler(originURL)

	req := httptest.NewRequest("GET", "/statusz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	expectedBody := `{"status":"ok","cache":"running"}`
	if w.Body.String() != expectedBody {
		t.Errorf("Expected body %q, got %q", expectedBody, w.Body.String())
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %q", contentType)
	}
}

func TestRegisterPath(t *testing.T) {
	originURL, _ := url.Parse("http://example.com")
	cs := New(&Config{})

	cs.RegisterPath("/custom", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("custom handler"))
	})

	handler := cs.Handler(originURL)
	req := httptest.NewRequest("GET", "/custom", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	expectedBody := "custom handler"
	if w.Body.String() != expectedBody {
		t.Errorf("Expected body %q, got %q", expectedBody, w.Body.String())
	}
}

// mockTransport is a simple RoundTripper that calls a user-provided function.
type mockTransport struct {
	fn func(*http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.fn(req)
}

func makeResp(statusCode int, headers http.Header, body string) *http.Response {
	// Canonicalize header keys so that Get("ETag") etc. work correctly,
	// matching the behaviour of Go's real HTTP stack.
	h := http.Header{}
	for k, vs := range headers {
		for _, v := range vs {
			h.Add(k, v)
		}
	}
	return &http.Response{
		StatusCode: statusCode,
		Header:     h,
		Body:       io.NopCloser(stringReader(body)),
		Request:    &http.Request{Method: http.MethodGet},
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}
}

func stringReader(s string) io.Reader {
	return &stringReadCloser{s: s, pos: 0}
}

type stringReadCloser struct {
	s   string
	pos int
}

func (sr *stringReadCloser) Read(p []byte) (n int, err error) {
	if sr.pos >= len(sr.s) {
		return 0, io.EOF
	}
	n = copy(p, sr.s[sr.pos:])
	sr.pos += n
	return n, nil
}

// TestRoundTrip_NoCacheResponseDirective verifies that a response with
// Cache-Control: no-cache IS stored but forces revalidation on next request (RFC9111 §5.2.2.4).
func TestRoundTrip_NoCacheResponseDirective(t *testing.T) {
	var calls int32

	transport := &mockTransport{fn: func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		// First call: return 200 with no-cache
		if atomic.LoadInt32(&calls) == 1 {
			return makeResp(http.StatusOK, http.Header{
				"Cache-Control": []string{"no-cache, max-age=3600"},
				"ETag":          []string{`"v1"`},
			}, "hello"), nil
		}
		// Second call (revalidation): return 304
		if req.Header.Get("If-None-Match") != `"v1"` {
			t.Errorf("expected If-None-Match header on revalidation, got %q", req.Header.Get("If-None-Match"))
		}
		return &http.Response{
			StatusCode: http.StatusNotModified,
			Header:     http.Header{"ETag": []string{`"v1"`}},
			Body:       io.NopCloser(http.NoBody),
			Request:    req,
			Proto:      "HTTP/1.1",
			ProtoMajor: 1, ProtoMinor: 1,
		}, nil
	}}

	cs := New(&Config{Transport: transport})
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/data", nil)

	// First request: miss, fetch from origin
	resp, err := cs.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("first request failed: %v %v", err, resp)
	}
	resp.Body.Close()

	// Second request: no-cache means must revalidate even though max-age not expired
	resp, err = cs.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("second request failed: %v %v", err, resp)
	}
	resp.Body.Close()

	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Errorf("expected 2 origin calls (fetch + revalidate), got %d", n)
	}
}

// TestRoundTrip_ConditionalRequest_304 verifies that a stale entry is revalidated
// via a conditional request and 304 is handled correctly (RFC9111 §4.3).
func TestRoundTrip_ConditionalRequest_304(t *testing.T) {
	var calls int32

	transport := &mockTransport{fn: func(req *http.Request) (*http.Response, error) {
		c := atomic.AddInt32(&calls, 1)
		if c == 1 {
			return makeResp(http.StatusOK, http.Header{
				"Cache-Control": []string{"max-age=1"},
				"ETag":          []string{`"abc"`},
			}, "original body"), nil
		}
		// Revalidation: expect conditional request
		if req.Header.Get("If-None-Match") != `"abc"` {
			t.Errorf("missing If-None-Match on revalidation")
		}
		return &http.Response{
			StatusCode: http.StatusNotModified,
			Header:     http.Header{"ETag": []string{`"abc"`}},
			Body:       io.NopCloser(http.NoBody),
			Request:    req,
			Proto:      "HTTP/1.1",
			ProtoMajor: 1, ProtoMinor: 1,
		}, nil
	}}

	cs := New(&Config{Transport: transport})
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/item", nil)

	// First request
	resp, _ := cs.RoundTrip(req)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "original body" {
		t.Fatalf("unexpected body: %q", body)
	}

	// Wait for entry to go stale
	time.Sleep(1100 * time.Millisecond)

	// Second request: stale → revalidation → 304 → serve cached body
	resp, err := cs.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("second request failed: %v %v", err, resp)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "original body" {
		t.Errorf("expected cached body after 304, got %q", body)
	}

	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Errorf("expected 2 origin calls, got %d", n)
	}
}

// TestRoundTrip_OnlyIfCached returns 504 when the cache has no usable entry (RFC9111 §5.2.1.7).
func TestRoundTrip_OnlyIfCached_Miss(t *testing.T) {
	transport := &mockTransport{fn: func(req *http.Request) (*http.Response, error) {
		t.Error("transport should not be called when only-if-cached")
		return nil, nil
	}}

	cs := New(&Config{Transport: transport})
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/nothing", nil)
	req.Header.Set("Cache-Control", "only-if-cached")

	resp, err := cs.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Errorf("expected 504, got %d", resp.StatusCode)
	}
}

// TestRoundTrip_OnlyIfCached_Hit serves from cache without going to origin.
func TestRoundTrip_OnlyIfCached_Hit(t *testing.T) {
	var calls int32
	transport := &mockTransport{fn: func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return makeResp(http.StatusOK, http.Header{
			"Cache-Control": []string{"max-age=3600"},
		}, "cached"), nil
	}}

	cs := New(&Config{Transport: transport})

	// Warm up cache
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/warm", nil)
	resp, _ := cs.RoundTrip(req)
	resp.Body.Close()

	// Now use only-if-cached
	req2, _ := http.NewRequest(http.MethodGet, "http://example.com/warm", nil)
	req2.Header.Set("Cache-Control", "only-if-cached")
	resp, err := cs.RoundTrip(req2)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from cache, got %v %v", err, resp)
	}
	resp.Body.Close()

	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("expected only 1 origin call (warm-up), got %d", n)
	}
}

// TestRoundTrip_NoStoreRequest: request no-store skips caching the response (RFC9111 §5.2.1.5).
func TestRoundTrip_NoStoreRequest(t *testing.T) {
	var calls int32
	transport := &mockTransport{fn: func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return makeResp(http.StatusOK, http.Header{
			"Cache-Control": []string{"max-age=3600"},
		}, "body"), nil
	}}

	cs := New(&Config{Transport: transport})

	// First request: no-store → response must NOT be stored
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/ns", nil)
	req.Header.Set("Cache-Control", "no-store")
	resp, _ := cs.RoundTrip(req)
	resp.Body.Close()

	// Second request without no-store → should miss cache (not stored)
	req2, _ := http.NewRequest(http.MethodGet, "http://example.com/ns", nil)
	resp, _ = cs.RoundTrip(req2)
	resp.Body.Close()

	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Errorf("expected 2 origin calls (no caching due to no-store), got %d", n)
	}
}

// TestRoundTrip_MaxStale: stale but within max-stale is served without revalidation (RFC9111 §5.2.1.2).
func TestRoundTrip_MaxStale(t *testing.T) {
	var calls int32
	transport := &mockTransport{fn: func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return makeResp(http.StatusOK, http.Header{
			"Cache-Control": []string{"max-age=1"},
		}, "stale body"), nil
	}}

	cs := New(&Config{Transport: transport})

	// Warm up cache
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/stale", nil)
	resp, _ := cs.RoundTrip(req)
	resp.Body.Close()

	// Wait for staleness
	time.Sleep(1100 * time.Millisecond)

	// Second request with max-stale=60 → can serve stale
	req2, _ := http.NewRequest(http.MethodGet, "http://example.com/stale", nil)
	req2.Header.Set("Cache-Control", "max-stale=60")
	resp, err := cs.RoundTrip(req2)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from stale cache, got %v %v", err, resp)
	}
	resp.Body.Close()

	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("expected only 1 origin call (stale served via max-stale), got %d", n)
	}
}

// TestRoundTrip_MustRevalidate: stale response with must-revalidate must NOT be served stale (RFC9111 §5.2.2.2).
func TestRoundTrip_MustRevalidate(t *testing.T) {
	var calls int32
	transport := &mockTransport{fn: func(req *http.Request) (*http.Response, error) {
		c := atomic.AddInt32(&calls, 1)
		if c == 1 {
			return makeResp(http.StatusOK, http.Header{
				"Cache-Control": []string{"max-age=1, must-revalidate"},
				"ETag":          []string{`"e1"`},
			}, "body"), nil
		}
		// Revalidation: return 200 (new response)
		return makeResp(http.StatusOK, http.Header{
			"Cache-Control": []string{"max-age=1, must-revalidate"},
			"ETag":          []string{`"e2"`},
		}, "new body"), nil
	}}

	cs := New(&Config{Transport: transport})

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/mr", nil)
	resp, _ := cs.RoundTrip(req)
	resp.Body.Close()

	// Wait for staleness
	time.Sleep(1100 * time.Millisecond)

	// Request with max-stale (should be ignored due to must-revalidate)
	req2, _ := http.NewRequest(http.MethodGet, "http://example.com/mr", nil)
	req2.Header.Set("Cache-Control", "max-stale=100")
	resp, err := cs.RoundTrip(req2)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Errorf("expected 2 calls (must-revalidate forces origin), got %d", n)
	}
}

// TestRoundTrip_CacheInvalidationUnsafeMethod verifies that a POST/PUT/DELETE
// invalidates the cached entry for the same URL (RFC9111 §4.4).
func TestRoundTrip_CacheInvalidationUnsafeMethod(t *testing.T) {
	var calls int32
	transport := &mockTransport{fn: func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		if req.Method == http.MethodPost {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(http.NoBody),
				Request:    req,
				Proto:      "HTTP/1.1",
				ProtoMajor: 1, ProtoMinor: 1,
			}, nil
		}
		return makeResp(http.StatusOK, http.Header{
			"Cache-Control": []string{"max-age=3600"},
		}, "resource"), nil
	}}

	cs := New(&Config{Transport: transport})

	// Warm cache with GET
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/res", nil)
	resp, _ := cs.RoundTrip(req)
	resp.Body.Close()

	// POST invalidates the entry
	postReq, _ := http.NewRequest(http.MethodPost, "http://example.com/res", nil)
	resp, _ = cs.RoundTrip(postReq)
	resp.Body.Close()

	// Second GET must miss (entry invalidated)
	req2, _ := http.NewRequest(http.MethodGet, "http://example.com/res", nil)
	resp, _ = cs.RoundTrip(req2)
	resp.Body.Close()

	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Errorf("expected 3 calls (GET + POST + GET miss), got %d", n)
	}
}

// TestRoundTrip_RequestNoCacheRevalidates: request no-cache forces revalidation (RFC9111 §5.2.1.4).
func TestRoundTrip_RequestNoCacheRevalidates(t *testing.T) {
	var calls int32
	transport := &mockTransport{fn: func(req *http.Request) (*http.Response, error) {
		c := atomic.AddInt32(&calls, 1)
		if c == 1 {
			return makeResp(http.StatusOK, http.Header{
				"Cache-Control": []string{"max-age=3600"},
				"ETag":          []string{`"tag1"`},
			}, "body"), nil
		}
		// Revalidation
		return &http.Response{
			StatusCode: http.StatusNotModified,
			Header:     http.Header{"ETag": []string{`"tag1"`}},
			Body:       io.NopCloser(http.NoBody),
			Request:    req,
			Proto:      "HTTP/1.1",
			ProtoMajor: 1, ProtoMinor: 1,
		}, nil
	}}

	cs := New(&Config{Transport: transport})

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/nc", nil)
	resp, _ := cs.RoundTrip(req)
	resp.Body.Close()

	// Second request with no-cache: must revalidate despite fresh entry
	req2, _ := http.NewRequest(http.MethodGet, "http://example.com/nc", nil)
	req2.Header.Set("Cache-Control", "no-cache")
	resp, err := cs.RoundTrip(req2)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected: %v %v", err, resp)
	}
	resp.Body.Close()

	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Errorf("expected 2 calls (GET + revalidation), got %d", n)
	}
}

// TestHandler_OnlyIfCached_Miss: Handler returns 504 when only-if-cached and no cache entry.
func TestHandler_OnlyIfCached_Miss(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("origin should not be called with only-if-cached")
	}))
	defer origin.Close()

	originURL, _ := url.Parse(origin.URL)
	cs := New(&Config{})
	handler := cs.Handler(originURL)

	req := httptest.NewRequest(http.MethodGet, "/data", nil)
	req.Header.Set("Cache-Control", "only-if-cached")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("expected 504, got %d", w.Code)
	}
}

// TestHandler_UnsafeMethodInvalidatesCache verifies the Handler invalidates cache on unsafe methods.
func TestHandler_UnsafeMethodInvalidatesCache(t *testing.T) {
	var originCalls int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&originCalls, 1)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("resource"))
	}))
	defer origin.Close()

	originURL, _ := url.Parse(origin.URL)
	cs := New(&Config{})
	handler := cs.Handler(originURL)

	// Warm cache
	req1 := httptest.NewRequest(http.MethodGet, "/item", nil)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	// POST invalidates
	req2 := httptest.NewRequest(http.MethodPost, "/item", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	// GET should miss (origin called again)
	req3 := httptest.NewRequest(http.MethodGet, "/item", nil)
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)

	if n := atomic.LoadInt32(&originCalls); n != 3 {
		t.Errorf("expected 3 origin calls (GET + POST + GET miss), got %d", n)
	}
}

// TestRoundTrip_ResponseHeaderCorrect verifies the bug fix: response headers
// (not request headers) are returned in cached responses.
func TestRoundTrip_ResponseHeaderCorrect(t *testing.T) {
	transport := &mockTransport{fn: func(req *http.Request) (*http.Response, error) {
		return makeResp(http.StatusOK, http.Header{
			"Cache-Control": []string{"max-age=3600"},
			"X-Custom":      []string{"from-origin"},
		}, "body"), nil
	}}

	cs := New(&Config{Transport: transport})

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/hdr", nil)
	req.Header.Set("X-Custom", "from-request")
	resp, _ := cs.RoundTrip(req)
	resp.Body.Close()

	// Second request: served from cache
	resp, err := cs.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Must return response header value, not request header value
	if got := resp.Header.Get("X-Custom"); got != "from-origin" {
		t.Errorf("expected response header 'from-origin', got %q", got)
	}
}

