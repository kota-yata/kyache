package cache

import (
	"net/http"
	"strconv"
	"time"
)

// see freshness.md for what this functions do

func IsFresh(resp *CachedResponse) bool {
	headerStruct := NewParsedHeaders(resp.ResponseHeader)
	freshFor := GetFreshnessLifetime(headerStruct)
	currentAge := time.Duration(GetCurrentAge(resp)) * time.Second
	return freshFor > 0 && currentAge < freshFor
}

func GetFreshnessLifetime(headerStruct *ParsedHeaders) time.Duration {
	// CDN-Cache-Control
	cdnMaxAge, hasCDNMaxAge := headerStruct.GetDirective("CDN-Cache-Control", "max-age")
	if hasCDNMaxAge {
		seconds, err := strconv.Atoi(cdnMaxAge)
		if err == nil && seconds >= 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	// Cache-Control s-maxage
	sMaxAge, hasSMaxAge := headerStruct.GetDirective("Cache-Control", "s-maxage")
	if hasSMaxAge {
		seconds, err := strconv.Atoi(sMaxAge)
		if err == nil && seconds >= 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	// Cache-Control max-age
	maxAge, hasMaxAge := headerStruct.GetDirective("Cache-Control", "max-age")
	if hasMaxAge {
		seconds, err := strconv.Atoi(maxAge)
		if err == nil && seconds >= 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	// Fallback: Expires - Date
	dateStr, hasDate := headerStruct.GetValue("Date")
	expStr, hasExp := headerStruct.GetValue("Expires")
	if hasDate && hasExp && len(dateStr) > 0 && len(expStr) > 0 {
		dateTime, err1 := http.ParseTime(dateStr[0])
		expTime, err2 := http.ParseTime(expStr[0])
		if err1 == nil && err2 == nil {
			d := expTime.Sub(dateTime)
			if d >= 0 {
				return d
			}
		}
	}
	// Heuristic freshness based on Last-Modified (RFC9111 Section 4.2.2)
	return GetHeuristicFreshness(headerStruct)
}

// GetHeuristicFreshness computes a heuristic freshness lifetime using the Last-Modified header.
// Per RFC9111 Section 4.2.2, uses 10% of (Date - Last-Modified) when no explicit freshness is set.
func GetHeuristicFreshness(headerStruct *ParsedHeaders) time.Duration {
	dateStr, hasDate := headerStruct.GetValue("Date")
	lastModStr, hasLastMod := headerStruct.GetValue("Last-Modified")
	if !hasDate || !hasLastMod || len(dateStr) == 0 || len(lastModStr) == 0 {
		return 0
	}
	dateTime, err1 := http.ParseTime(dateStr[0])
	lastModTime, err2 := http.ParseTime(lastModStr[0])
	if err1 != nil || err2 != nil || !lastModTime.Before(dateTime) {
		return 0
	}
	return dateTime.Sub(lastModTime) / 10
}

func ValidateAge(age string) bool {
	ageVal, err := strconv.Atoi(age)
	if err != nil || ageVal < 0 {
		return false // Ignore invalid Age values like non-numeric or negative
	}
	return true
}

func GetCurrentAge(resp *CachedResponse) int {
	age := time.Since(resp.StoredAt).Seconds() + float64(resp.InitialAge)
	return int(age)
}

// IsFreshEnoughForRequest checks whether a fresh cached response satisfies the freshness
// constraints expressed in the request's Cache-Control directives (RFC9111 Section 5.2.1).
// This checks max-age and min-fresh request directives.
func IsFreshEnoughForRequest(cachedResp *CachedResponse, reqHeader *ParsedHeaders) bool {
	headerStruct := NewParsedHeaders(cachedResp.ResponseHeader)
	currentAge := GetCurrentAge(cachedResp)
	freshnessLifetime := GetFreshnessLifetime(headerStruct)

	// Request max-age: the response must not be older than the specified number of seconds
	if maxAgeStr, hasMaxAge := reqHeader.GetDirective("Cache-Control", "max-age"); hasMaxAge {
		maxAge, err := strconv.Atoi(maxAgeStr)
		if err == nil && maxAge >= 0 && currentAge >= maxAge {
			return false
		}
	}

	// Request min-fresh: the response must remain fresh for at least N more seconds
	if minFreshStr, hasMinFresh := reqHeader.GetDirective("Cache-Control", "min-fresh"); hasMinFresh {
		minFresh, err := strconv.Atoi(minFreshStr)
		if err == nil && minFresh >= 0 {
			remaining := int(freshnessLifetime.Seconds()) - currentAge
			if remaining < minFresh {
				return false
			}
		}
	}

	return true
}

// CanServeStaleForRequest checks whether a stale cached response can still be served
// based on the request's max-stale directive (RFC9111 Section 5.2.1.2).
func CanServeStaleForRequest(cachedResp *CachedResponse, reqHeader *ParsedHeaders) bool {
	maxStaleStr, hasMaxStale := reqHeader.GetDirective("Cache-Control", "max-stale")
	if !hasMaxStale {
		return false
	}
	if maxStaleStr == "" {
		// max-stale without a value means any stale response is acceptable
		return true
	}
	maxStale, err := strconv.Atoi(maxStaleStr)
	if err != nil || maxStale < 0 {
		return true
	}
	headerStruct := NewParsedHeaders(cachedResp.ResponseHeader)
	freshnessLifetime := GetFreshnessLifetime(headerStruct)
	staleness := GetCurrentAge(cachedResp) - int(freshnessLifetime.Seconds())
	return staleness <= maxStale
}
