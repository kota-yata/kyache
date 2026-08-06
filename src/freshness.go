package cache

import (
	"net/http"
	"strconv"
	"time"
)

// see freshness.md for what these functions do

func IsFresh(resp *CachedResponse) bool {
	headerStruct := NewParsedHeaders(resp.ResponseHeader)
	freshFor := GetFreshnessLifetimeForStatus(headerStruct, resp.StatusCode)
	currentAge := time.Duration(GetCurrentAge(resp)) * time.Second
	return freshFor > 0 && currentAge < freshFor
}

func GetFreshnessLifetime(headerStruct *ParsedHeaders) time.Duration {
	return getExplicitFreshnessLifetime(headerStruct)
}

func GetFreshnessLifetimeForStatus(headerStruct *ParsedHeaders, statusCode int) time.Duration {
	freshFor := getExplicitFreshnessLifetime(headerStruct)
	if freshFor > 0 || hasExplicitFreshness(headerStruct) {
		return freshFor
	}
	return getHeuristicFreshnessLifetime(headerStruct, statusCode)
}

func getExplicitFreshnessLifetime(headerStruct *ParsedHeaders) time.Duration {
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
	if !hasDate || !hasExp {
		return 0
	}
	if dateStr[0] == "" || expStr[0] == "" {
		return 0
	}
	dateTime, err1 := http.ParseTime(dateStr[0])
	expTime, err2 := http.ParseTime(expStr[0])
	if err1 != nil || err2 != nil {
		return 0
	}
	d := expTime.Sub(dateTime)
	if d < 0 {
		return 0
	}
	return d
}

func hasExplicitFreshness(headerStruct *ParsedHeaders) bool {
	if _, ok := headerStruct.GetDirective("CDN-Cache-Control", "max-age"); ok {
		return true
	}
	if _, ok := headerStruct.GetDirective("Cache-Control", "s-maxage"); ok {
		return true
	}
	if _, ok := headerStruct.GetDirective("Cache-Control", "max-age"); ok {
		return true
	}
	if _, ok := headerStruct.GetValue("Expires"); ok {
		return true
	}
	return false
}

func getHeuristicFreshnessLifetime(headerStruct *ParsedHeaders, statusCode int) time.Duration {
	if !isHeuristicFreshnessAllowed(headerStruct, statusCode) {
		return 0
	}

	dateStr, hasDate := headerStruct.GetValue("Date")
	lastModifiedStr, hasLastModified := headerStruct.GetValue("Last-Modified")
	if !hasDate || !hasLastModified || dateStr[0] == "" || lastModifiedStr[0] == "" {
		return 0
	}

	dateTime, err1 := http.ParseTime(dateStr[0])
	lastModifiedTime, err2 := http.ParseTime(lastModifiedStr[0])
	if err1 != nil || err2 != nil {
		return 0
	}

	ageSinceLastModified := dateTime.Sub(lastModifiedTime)
	if ageSinceLastModified <= 0 {
		return 0
	}
	return ageSinceLastModified / 10 // 10% * (Date - Last-Modified)
}

func isHeuristicFreshnessAllowed(headerStruct *ParsedHeaders, statusCode int) bool {
	if _, ok := headerStruct.GetDirective("Cache-Control", "public"); ok {
		return true
	}
	return isHeuristicallyCacheableStatus(statusCode)
}

func isHeuristicallyCacheableStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusOK,
		http.StatusNonAuthoritativeInfo,
		http.StatusNoContent,
		http.StatusPartialContent,
		http.StatusMultipleChoices,
		http.StatusMovedPermanently,
		http.StatusPermanentRedirect,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusGone,
		http.StatusRequestURITooLong,
		http.StatusNotImplemented:
		return true
	default:
		return false
	}
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
