package cache

import (
	"net/http"
	"testing"
	"time"

	"github.com/kota-yata/kyache/cache"
)

// ---- Heuristic freshness (RFC9111 Section 4.2.2) ----

func TestGetHeuristicFreshness(t *testing.T) {
	tests := []struct {
		name     string
		headers  http.Header
		wantZero bool
		wantMin  time.Duration
		wantMax  time.Duration
	}{
		{
			name: "10% of Date-LastModified when no explicit freshness",
			headers: http.Header{
				// Date is 1000 seconds after Last-Modified → heuristic = 100 s
				"Date":          []string{"Thu, 01 Jan 2015 00:16:40 GMT"},
				"Last-Modified": []string{"Thu, 01 Jan 2015 00:00:00 GMT"},
			},
			wantMin: 99 * time.Second,
			wantMax: 101 * time.Second,
		},
		{
			name: "returns 0 when Last-Modified missing",
			headers: http.Header{
				"Date": []string{"Thu, 01 Jan 2015 00:16:40 GMT"},
			},
			wantZero: true,
		},
		{
			name: "returns 0 when Date missing",
			headers: http.Header{
				"Last-Modified": []string{"Thu, 01 Jan 2015 00:00:00 GMT"},
			},
			wantZero: true,
		},
		{
			name: "returns 0 when Last-Modified is after Date",
			headers: http.Header{
				"Date":          []string{"Thu, 01 Jan 2015 00:00:00 GMT"},
				"Last-Modified": []string{"Thu, 01 Jan 2015 00:16:40 GMT"},
			},
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := cache.NewParsedHeaders(tt.headers)
			got := cache.GetHeuristicFreshness(parsed)
			if tt.wantZero {
				if got != 0 {
					t.Errorf("expected 0, got %v", got)
				}
				return
			}
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("expected %v–%v, got %v", tt.wantMin, tt.wantMax, got)
			}
		})
	}
}

func TestGetFreshnessLifetimeFallsBackToHeuristic(t *testing.T) {
	headers := http.Header{
		// 1000 s gap → heuristic = 100 s
		"Date":          []string{"Thu, 01 Jan 2015 00:16:40 GMT"},
		"Last-Modified": []string{"Thu, 01 Jan 2015 00:00:00 GMT"},
	}
	parsed := cache.NewParsedHeaders(headers)
	got := cache.GetFreshnessLifetime(parsed)
	if got < 99*time.Second || got > 101*time.Second {
		t.Errorf("expected ~100s heuristic, got %v", got)
	}
}

// ---- IsFreshEnoughForRequest (RFC9111 Section 5.2.1) ----

func makeCachedResp(maxAge int, storedSecondsAgo int) *cache.CachedResponse {
	h := http.Header{}
	if maxAge >= 0 {
		h.Set("Cache-Control", "max-age="+itoa(maxAge))
	}
	return &cache.CachedResponse{
		ResponseHeader: h,
		StoredAt:       time.Now().Add(-time.Duration(storedSecondsAgo) * time.Second),
	}
}

func itoa(n int) string {
	return http.Header{}.Get("X-Fake") // won't be called, just satisfies compiler
}

func TestIsFreshEnoughForRequest(t *testing.T) {
	// Response stored 10 s ago with max-age=60 → age=10, remaining=50
	cached := &cache.CachedResponse{
		ResponseHeader: http.Header{"Cache-Control": []string{"max-age=60"}},
		StoredAt:       time.Now().Add(-10 * time.Second),
	}

	t.Run("max-age request: age < limit → fresh enough", func(t *testing.T) {
		req := cache.NewParsedHeaders(http.Header{
			"Cache-Control": []string{"max-age=20"},
		})
		if !cache.IsFreshEnoughForRequest(cached, req) {
			t.Error("expected fresh enough")
		}
	})

	t.Run("max-age request: age >= limit → not fresh enough", func(t *testing.T) {
		req := cache.NewParsedHeaders(http.Header{
			"Cache-Control": []string{"max-age=5"},
		})
		if cache.IsFreshEnoughForRequest(cached, req) {
			t.Error("expected NOT fresh enough")
		}
	})

	t.Run("min-fresh: remaining > min-fresh → fresh enough", func(t *testing.T) {
		req := cache.NewParsedHeaders(http.Header{
			"Cache-Control": []string{"min-fresh=30"},
		})
		if !cache.IsFreshEnoughForRequest(cached, req) {
			t.Error("expected fresh enough")
		}
	})

	t.Run("min-fresh: remaining < min-fresh → not fresh enough", func(t *testing.T) {
		req := cache.NewParsedHeaders(http.Header{
			"Cache-Control": []string{"min-fresh=60"},
		})
		if cache.IsFreshEnoughForRequest(cached, req) {
			t.Error("expected NOT fresh enough")
		}
	})

	t.Run("no request constraints → always fresh enough", func(t *testing.T) {
		req := cache.NewParsedHeaders(http.Header{})
		if !cache.IsFreshEnoughForRequest(cached, req) {
			t.Error("expected fresh enough")
		}
	})
}

// ---- CanServeStaleForRequest (RFC9111 Section 5.2.1.2) ----

func TestCanServeStaleForRequest(t *testing.T) {
	// Response stored 100 s ago with max-age=60 → stale by 40 s
	cached := &cache.CachedResponse{
		ResponseHeader: http.Header{"Cache-Control": []string{"max-age=60"}},
		StoredAt:       time.Now().Add(-100 * time.Second),
	}

	t.Run("no max-stale → cannot serve stale", func(t *testing.T) {
		req := cache.NewParsedHeaders(http.Header{})
		if cache.CanServeStaleForRequest(cached, req) {
			t.Error("expected cannot serve stale")
		}
	})

	t.Run("max-stale (no value) → any stale OK", func(t *testing.T) {
		req := cache.NewParsedHeaders(http.Header{
			"Cache-Control": []string{"max-stale"},
		})
		if !cache.CanServeStaleForRequest(cached, req) {
			t.Error("expected can serve stale")
		}
	})

	t.Run("max-stale=50 → staleness(40) <= 50 → OK", func(t *testing.T) {
		req := cache.NewParsedHeaders(http.Header{
			"Cache-Control": []string{"max-stale=50"},
		})
		if !cache.CanServeStaleForRequest(cached, req) {
			t.Error("expected can serve stale")
		}
	})

	t.Run("max-stale=30 → staleness(40) > 30 → not OK", func(t *testing.T) {
		req := cache.NewParsedHeaders(http.Header{
			"Cache-Control": []string{"max-stale=30"},
		})
		if cache.CanServeStaleForRequest(cached, req) {
			t.Error("expected cannot serve stale")
		}
	})
}

// ---- ReqNeedsRevalidation (RFC9111 Section 5.2.1.4 + 5.4) ----

func TestReqNeedsRevalidation(t *testing.T) {
	tests := []struct {
		name    string
		headers http.Header
		want    bool
	}{
		{
			name:    "no-cache in Cache-Control",
			headers: http.Header{"Cache-Control": []string{"no-cache"}},
			want:    true,
		},
		{
			name:    "Pragma: no-cache without Cache-Control",
			headers: http.Header{"Pragma": []string{"no-cache"}},
			want:    true,
		},
		{
			name: "Pragma: no-cache ignored when Cache-Control present",
			headers: http.Header{
				"Cache-Control": []string{"max-age=0"},
				"Pragma":        []string{"no-cache"},
			},
			want: false,
		},
		{
			name:    "max-age only: no revalidation needed",
			headers: http.Header{"Cache-Control": []string{"max-age=60"}},
			want:    false,
		},
		{
			name:    "empty headers: no revalidation needed",
			headers: http.Header{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := cache.NewParsedHeaders(tt.headers)
			got := cache.ReqNeedsRevalidation(parsed)
			if got != tt.want {
				t.Errorf("want %v, got %v", tt.want, got)
			}
		})
	}
}

// ---- RespNeedsRevalidation (RFC9111 Section 5.2.2.4) ----

func TestRespNeedsRevalidation(t *testing.T) {
	tests := []struct {
		name    string
		headers http.Header
		want    bool
	}{
		{
			name:    "no-cache response directive",
			headers: http.Header{"Cache-Control": []string{"no-cache"}},
			want:    true,
		},
		{
			name:    "no-cache with max-age",
			headers: http.Header{"Cache-Control": []string{"no-cache, max-age=3600"}},
			want:    true,
		},
		{
			name:    "max-age only: no revalidation needed",
			headers: http.Header{"Cache-Control": []string{"max-age=3600"}},
			want:    false,
		},
		{
			name:    "public: no revalidation needed",
			headers: http.Header{"Cache-Control": []string{"public, max-age=3600"}},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := cache.NewParsedHeaders(tt.headers)
			got := cache.RespNeedsRevalidation(parsed)
			if got != tt.want {
				t.Errorf("want %v, got %v", tt.want, got)
			}
		})
	}
}

// ---- MustRevalidateStale (RFC9111 Section 5.2.2.2 + 5.2.2.8) ----

func TestMustRevalidateStale(t *testing.T) {
	tests := []struct {
		name    string
		headers http.Header
		want    bool
	}{
		{
			name:    "must-revalidate",
			headers: http.Header{"Cache-Control": []string{"must-revalidate"}},
			want:    true,
		},
		{
			name:    "proxy-revalidate",
			headers: http.Header{"Cache-Control": []string{"proxy-revalidate"}},
			want:    true,
		},
		{
			name:    "max-age only: no must-revalidate",
			headers: http.Header{"Cache-Control": []string{"max-age=3600"}},
			want:    false,
		},
		{
			name:    "public: no must-revalidate",
			headers: http.Header{"Cache-Control": []string{"public, max-age=3600"}},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := cache.NewParsedHeaders(tt.headers)
			got := cache.MustRevalidateStale(parsed)
			if got != tt.want {
				t.Errorf("want %v, got %v", tt.want, got)
			}
		})
	}
}

// ---- IsCacheable with status codes (RFC9111 Section 3.2) ----

func TestIsCacheableStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		statusCode int
		headers    http.Header
		want       bool
	}{
		{
			name:       "200 OK without explicit freshness: cacheable by default",
			method:     http.MethodGet,
			statusCode: http.StatusOK,
			headers:    http.Header{},
			want:       true,
		},
		{
			name:       "404 Not Found: cacheable by default",
			method:     http.MethodGet,
			statusCode: http.StatusNotFound,
			headers:    http.Header{},
			want:       true,
		},
		{
			name:       "301 Moved Permanently: cacheable by default",
			method:     http.MethodGet,
			statusCode: http.StatusMovedPermanently,
			headers:    http.Header{},
			want:       true,
		},
		{
			name:       "302 Found without explicit freshness: not cacheable",
			method:     http.MethodGet,
			statusCode: http.StatusFound,
			headers:    http.Header{},
			want:       false,
		},
		{
			name:       "302 Found with max-age: cacheable",
			method:     http.MethodGet,
			statusCode: http.StatusFound,
			headers:    http.Header{"Cache-Control": []string{"max-age=3600"}},
			want:       true,
		},
		{
			name:       "500 Internal Server Error without explicit freshness: not cacheable",
			method:     http.MethodGet,
			statusCode: http.StatusInternalServerError,
			headers:    http.Header{},
			want:       false,
		},
		{
			name:       "501 Not Implemented: cacheable by default",
			method:     http.MethodGet,
			statusCode: http.StatusNotImplemented,
			headers:    http.Header{},
			want:       true,
		},
		{
			name:       "200 OK with no-store: not cacheable",
			method:     http.MethodGet,
			statusCode: http.StatusOK,
			headers:    http.Header{"Cache-Control": []string{"no-store"}},
			want:       false,
		},
		{
			name:       "200 OK with no-cache: cacheable (store but revalidate)",
			method:     http.MethodGet,
			statusCode: http.StatusOK,
			headers:    http.Header{"Cache-Control": []string{"no-cache"}},
			want:       true,
		},
		{
			name:       "POST 200: not cacheable (non-GET method)",
			method:     http.MethodPost,
			statusCode: http.StatusOK,
			headers:    http.Header{},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := cache.NewParsedHeaders(tt.headers)
			got := cache.IsCacheable(tt.method, tt.statusCode, parsed)
			if got != tt.want {
				t.Errorf("IsCacheable() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---- CacheStore.Delete ----

func TestCacheStoreDelete(t *testing.T) {
	store := cache.NewCacheStore()
	resp := &cache.CachedResponse{
		StatusCode: 200,
		StoredAt:   time.Now(),
	}

	store.Set("http://example.com/foo", resp)

	_, exists := store.Get("http://example.com/foo")
	if !exists {
		t.Fatal("expected entry to exist after Set")
	}

	store.Delete("http://example.com/foo")

	_, exists = store.Get("http://example.com/foo")
	if exists {
		t.Error("expected entry to be gone after Delete")
	}
}

// ---- Date header parsing (no comma-split) ----

func TestDateHeaderNotSplitOnComma(t *testing.T) {
	headers := http.Header{
		"Date":          []string{"Wed, 21 Oct 2015 07:28:00 GMT"},
		"Last-Modified": []string{"Mon, 19 Oct 2015 07:28:00 GMT"},
		"Expires":       []string{"Thu, 22 Oct 2015 07:28:00 GMT"},
	}
	parsed := cache.NewParsedHeaders(headers)

	dateVal, exists := parsed.GetValue("Date")
	if !exists || len(dateVal) != 1 {
		t.Fatalf("expected 1 Date value, got %v", dateVal)
	}
	if dateVal[0] != "Wed, 21 Oct 2015 07:28:00 GMT" {
		t.Errorf("Date value corrupted: %q", dateVal[0])
	}

	lastModVal, exists := parsed.GetValue("Last-Modified")
	if !exists || len(lastModVal) != 1 {
		t.Fatalf("expected 1 Last-Modified value, got %v", lastModVal)
	}
	if lastModVal[0] != "Mon, 19 Oct 2015 07:28:00 GMT" {
		t.Errorf("Last-Modified value corrupted: %q", lastModVal[0])
	}

	expiresVal, exists := parsed.GetValue("Expires")
	if !exists || len(expiresVal) != 1 {
		t.Fatalf("expected 1 Expires value, got %v", expiresVal)
	}
	if expiresVal[0] != "Thu, 22 Oct 2015 07:28:00 GMT" {
		t.Errorf("Expires value corrupted: %q", expiresVal[0])
	}
}

// ---- Expires-Date freshness calculation (should now work correctly) ----

func TestGetFreshnessLifetimeExpiresDate(t *testing.T) {
	headers := http.Header{
		"Date":    []string{"Wed, 21 Oct 2015 07:28:00 GMT"},
		"Expires": []string{"Wed, 21 Oct 2015 08:28:00 GMT"}, // 1 hour later
	}
	parsed := cache.NewParsedHeaders(headers)
	got := cache.GetFreshnessLifetime(parsed)
	expected := time.Hour
	if got != expected {
		t.Errorf("expected %v, got %v", expected, got)
	}
}
