package cache

import (
	"net/http"
	"strconv"
	"time"
)

// see freshness.md for what this functions do

func IsFresh(resp *CachedResponse) bool {
	headerStruct := NewParsedHeaders(resp.Header)
	freshFor := GetFreshnessLifetime(headerStruct)
	currentAge := time.Duration(GetCurrentAge(resp)) * time.Second
	return freshFor > 0 && currentAge < freshFor
}

func GetFreshnessLifetime(headerStruct *ParsedHeaders) time.Duration {
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
	if dateStr == "" || expStr == "" {
		return 0
	}
	dateTime, err1 := http.ParseTime(dateStr)
	expTime, err2 := http.ParseTime(expStr)
	if err1 != nil || err2 != nil {
		return 0
	}
	d := expTime.Sub(dateTime)
	if d < 0 {
		return 0
	}
	return d
}

func GetAge(resp *CachedResponse) int {
	return int(time.Since(resp.StoredAt).Seconds())
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
