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
