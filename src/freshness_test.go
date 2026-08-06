package cache

import (
	"net/http"
	"testing"
	"time"
)

func TestGetFreshnessLifetimeForStatusUsesHeuristicFreshness(t *testing.T) {
	header := NewParsedHeaders(http.Header{
		"Date":          []string{"Wed, 21 Oct 2015 07:28:00 GMT"},
		"Last-Modified": []string{"Wed, 21 Oct 2015 06:28:00 GMT"},
	})

	got := GetFreshnessLifetimeForStatus(header, http.StatusOK)
	want := 6 * time.Minute
	if got != want {
		t.Fatalf("freshness lifetime = %v, want %v", got, want)
	}
}

func TestGetFreshnessLifetimeForStatusAllowsPublicHeuristicFreshness(t *testing.T) {
	header := NewParsedHeaders(http.Header{
		"Cache-Control": []string{"public"},
		"Date":          []string{"Wed, 21 Oct 2015 07:28:00 GMT"},
		"Last-Modified": []string{"Wed, 21 Oct 2015 06:28:00 GMT"},
	})

	got := GetFreshnessLifetimeForStatus(header, http.StatusFound)
	want := 6 * time.Minute
	if got != want {
		t.Fatalf("freshness lifetime = %v, want %v", got, want)
	}
}

func TestGetFreshnessLifetimeForStatusDoesNotUseHeuristicWithExplicitFreshness(t *testing.T) {
	header := NewParsedHeaders(http.Header{
		"Cache-Control": []string{"max-age=120"},
		"Date":          []string{"Wed, 21 Oct 2015 07:28:00 GMT"},
		"Last-Modified": []string{"Wed, 21 Oct 2015 06:28:00 GMT"},
	})

	got := GetFreshnessLifetimeForStatus(header, http.StatusOK)
	want := 2 * time.Minute
	if got != want {
		t.Fatalf("freshness lifetime = %v, want %v", got, want)
	}
}

func TestGetFreshnessLifetimeForStatusDoesNotFallbackToHeuristicForInvalidExplicitFreshness(t *testing.T) {
	header := NewParsedHeaders(http.Header{
		"Expires":       []string{"not a date"},
		"Date":          []string{"Wed, 21 Oct 2015 07:28:00 GMT"},
		"Last-Modified": []string{"Wed, 21 Oct 2015 06:28:00 GMT"},
	})

	got := GetFreshnessLifetimeForStatus(header, http.StatusOK)
	if got != 0 {
		t.Fatalf("freshness lifetime = %v, want 0", got)
	}
}

func TestGetFreshnessLifetimeForStatusRejectsNonHeuristicallyCacheableStatus(t *testing.T) {
	header := NewParsedHeaders(http.Header{
		"Date":          []string{"Wed, 21 Oct 2015 07:28:00 GMT"},
		"Last-Modified": []string{"Wed, 21 Oct 2015 06:28:00 GMT"},
	})

	got := GetFreshnessLifetimeForStatus(header, http.StatusFound)
	if got != 0 {
		t.Fatalf("freshness lifetime = %v, want 0", got)
	}
}
