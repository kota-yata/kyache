package cache

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/kota-yata/kyache/cache"
)

func TestGetDirective(t *testing.T) {
	tests := []struct {
		name           string
		headers        http.Header
		headerName     string
		directiveName  string
		expectedValue  string
		expectedExists bool
	}{
		{
			name: "Cache-Control max-age directive",
			headers: http.Header{
				"Cache-Control": []string{"max-age=3600, no-cache"},
			},
			headerName:     "Cache-Control",
			directiveName:  "max-age",
			expectedValue:  "3600",
			expectedExists: true,
		},
		{
			name: "Cache-Control no-cache directive (no value)",
			headers: http.Header{
				"Cache-Control": []string{"max-age=3600, no-cache"},
			},
			headerName:     "Cache-Control",
			directiveName:  "no-cache",
			expectedValue:  "",
			expectedExists: true,
		},
		{
			name: "Case insensitive header name",
			headers: http.Header{
				"cache-control": []string{"public, max-age=86400"},
			},
			headerName:     "CACHE-CONTROL",
			directiveName:  "max-age",
			expectedValue:  "86400",
			expectedExists: true,
		},
		{
			name: "Case insensitive directive name",
			headers: http.Header{
				"Cache-Control": []string{"MAX-AGE=7200, Public"},
			},
			headerName:     "cache-control",
			directiveName:  "max-age",
			expectedValue:  "7200",
			expectedExists: true,
		},
		{
			name: "Non-existent directive",
			headers: http.Header{
				"Cache-Control": []string{"max-age=3600"},
			},
			headerName:     "Cache-Control",
			directiveName:  "no-store",
			expectedValue:  "",
			expectedExists: false,
		},
		{
			name: "Non-existent header",
			headers: http.Header{
				"Content-Type": []string{"text/html"},
			},
			headerName:     "Cache-Control",
			directiveName:  "max-age",
			expectedValue:  "",
			expectedExists: false,
		},
		{
			name: "Pragma directive",
			headers: http.Header{
				"Pragma": []string{"no-cache"},
			},
			headerName:     "Pragma",
			directiveName:  "no-cache",
			expectedValue:  "",
			expectedExists: true,
		},
		{
			name: "Directive with quoted value",
			headers: http.Header{
				"Cache-Control": []string{`private="user-123", max-age=300`},
			},
			headerName:     "Cache-Control",
			directiveName:  "private",
			expectedValue:  "user-123",
			expectedExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := cache.NewParsedHeaders(tt.headers)
			value, exists := parsed.GetDirective(tt.headerName, tt.directiveName)

			if exists != tt.expectedExists {
				t.Errorf("GetDirective() exists = %v, want %v", exists, tt.expectedExists)
			}

			if value != tt.expectedValue {
				t.Errorf("GetDirective() value = %v, want %v", value, tt.expectedValue)
			}
		})
	}
}

func TestGetValue(t *testing.T) {
	tests := []struct {
		name           string
		headers        http.Header
		headerName     string
		expectedValue  []string
		expectedExists bool
	}{
		{
			name: "Date header",
			headers: http.Header{
				"Date": []string{"Wed, 21 Oct 2015 07:28:00 GMT"},
			},
			headerName:     "Date",
			expectedValue:  []string{"Wed, 21 Oct 2015 07:28:00 GMT"},
			expectedExists: true,
		},
		{
			name: "Expires header",
			headers: http.Header{
				"Expires": []string{"Thu, 01 Dec 2024 16:00:00 GMT"},
			},
			headerName:     "Expires",
			expectedValue:  []string{"Thu, 01 Dec 2024 16:00:00 GMT"},
			expectedExists: true,
		},
		{
			name: "Age header",
			headers: http.Header{
				"Age": []string{"300"},
			},
			headerName:     "Age",
			expectedValue:  []string{"300"},
			expectedExists: true,
		},
		{
			name: "Case insensitive header name",
			headers: http.Header{
				"content-type": []string{"application/json"},
			},
			headerName:     "CONTENT-TYPE",
			expectedValue:  []string{"application/json"},
			expectedExists: true,
		},
		{
			name: "Multiple values",
			headers: http.Header{
				"Accept": []string{"text/html", "application/json"},
			},
			headerName:     "Accept",
			expectedValue:  []string{"text/html", "application/json"},
			expectedExists: true,
		},
		{
			name: "Non-existent header",
			headers: http.Header{
				"Content-Type": []string{"text/html"},
			},
			headerName:     "Authorization",
			expectedValue:  nil,
			expectedExists: false,
		},
		{
			name: "Empty header value",
			headers: http.Header{
				"X-Custom": []string{""},
			},
			headerName:     "X-Custom",
			expectedValue:  []string{""},
			expectedExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := cache.NewParsedHeaders(tt.headers)
			value, exists := parsed.GetValue(tt.headerName)

			if exists != tt.expectedExists {
				t.Errorf("GetValue() exists = %v, want %v", exists, tt.expectedExists)
			}

			if !reflect.DeepEqual(value, tt.expectedValue) {
				t.Errorf("GetValue() value = %v, want %v", value, tt.expectedValue)
			}
		})
	}
}

func TestMixedHeaderTypes(t *testing.T) {
	headers := http.Header{
		"Cache-Control": []string{"max-age=3600, public"},
		"Date":          []string{"Wed, 21 Oct 2015 07:28:00 GMT"},
		"Expires":       []string{"Thu, 22 Oct 2015 07:28:00 GMT"},
		"Age":           []string{"300"},
		"Content-Type":  []string{"text/html"},
		"Pragma":        []string{"no-cache"},
	}

	parsed := cache.NewParsedHeaders(headers)

	maxAge, exists := parsed.GetDirective("Cache-Control", "max-age")
	if !exists || maxAge != "3600" {
		t.Errorf("Expected Cache-Control max-age=3600, got %v (exists: %v)", maxAge, exists)
	}

	public, exists := parsed.GetDirective("Cache-Control", "public")
	if !exists || public != "" {
		t.Errorf("Expected Cache-Control public directive, got %v (exists: %v)", public, exists)
	}

	date, exists := parsed.GetValue("Date")
	if !exists || len(date) != 1 || date[0] != "Wed, 21 Oct 2015 07:28:00 GMT" {
		t.Errorf("Expected Date header, got %v (exists: %v)", date, exists)
	}

	_, exists = parsed.GetDirective("Date", "some-directive")
	if exists {
		t.Errorf("Date header should not be parsed as directives")
	}

	_, exists = parsed.GetValue("Cache-Control")
	if exists {
		t.Errorf("Cache-Control header should not be available as simple value")
	}
}

func TestAuthorizationHeaderParsing(t *testing.T) {
	tests := []struct {
		name           string
		headers        http.Header
		expectedScheme string
		expectedParams map[string]string
	}{
		{
			name: "Basic Authorization",
			headers: http.Header{
				"Authorization": []string{"Basic YWxhZGRpbjpvcGVuc2VzYW1l"},
			},
			expectedScheme: "basic",
			expectedParams: map[string]string{
				"scheme":      "basic",
				"credentials": "YWxhZGRpbjpvcGVuc2VzYW1l",
			},
		},
		{
			name: "Digest Authorization",
			headers: http.Header{
				"Authorization": []string{`Digest username="test", realm="example.com", nonce="abc123", response="def456"`},
			},
			expectedScheme: "digest",
			expectedParams: map[string]string{
				"scheme":   "digest",
				"username": "test",
				"realm":    "example.com",
				"nonce":    "abc123",
				"response": "def456",
			},
		},
		{
			name: "Bearer Authorization",
			headers: http.Header{
				"Authorization": []string{"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
			},
			expectedScheme: "bearer",
			expectedParams: map[string]string{
				"scheme":     "bearer",
				"parameters": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			},
		},
		{
			name: "Complex Digest Authorization",
			headers: http.Header{
				"Authorization": []string{`Digest username="user@example.com", realm="Protected Area", uri="/secret", algorithm=MD5, nonce="7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v", nc=00000001, cnonce="f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ", qop=auth, response="6629fae49393a05397450978507c4ef1", opaque="5ccc069c403ebaf9f0171e9517f40e41"`},
			},
			expectedScheme: "digest",
			expectedParams: map[string]string{
				"scheme":    "digest",
				"username":  "user@example.com",
				"realm":     "Protected Area",
				"uri":       "/secret",
				"algorithm": "MD5",
				"nonce":     "7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v",
				"nc":        "00000001",
				"cnonce":    "f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ",
				"qop":       "auth",
				"response":  "6629fae49393a05397450978507c4ef1",
				"opaque":    "5ccc069c403ebaf9f0171e9517f40e41",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := cache.NewParsedHeaders(tt.headers)

			_, exists := parsed.GetDirectives("Authorization")
			if !exists {
				t.Errorf("Authorization header not found")
				return
			}

			scheme, exists := parsed.GetDirective("Authorization", "scheme")
			if !exists {
				t.Errorf("Authorization scheme not found")
				return
			}

			if scheme != tt.expectedScheme {
				t.Errorf("Expected scheme %q, got %q", tt.expectedScheme, scheme)
			}

			for key, expectedValue := range tt.expectedParams {
				value, exists := parsed.GetDirective("Authorization", key)
				if !exists {
					t.Errorf("Authorization parameter %q not found", key)
					continue
				}

				if value != expectedValue {
					t.Errorf("Expected %q=%q, got %q", key, expectedValue, value)
				}
			}
		})
	}
}

func TestAuthorizationHeaderCaseInsensitive(t *testing.T) {
	headers := http.Header{
		"authorization": []string{"Basic dGVzdDp0ZXN0"},
	}

	parsed := cache.NewParsedHeaders(headers)

	scheme, exists := parsed.GetDirective("AUTHORIZATION", "scheme")
	if !exists || scheme != "basic" {
		t.Errorf("Expected basic scheme, got %q (exists: %v)", scheme, exists)
	}

	credentials, exists := parsed.GetDirective("authorization", "credentials")
	if !exists || credentials != "dGVzdDp0ZXN0" {
		t.Errorf("Expected credentials, got %q (exists: %v)", credentials, exists)
	}
}
