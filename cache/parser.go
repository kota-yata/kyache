package cache

import (
	"net/http"
	"strconv"
	"strings"
)

type ParsedHeaders struct {
	Directives map[string]map[string]string
	Values     map[string][]string
}

func parseDirectives(headerValue string) map[string]string {
	result := make(map[string]string)
	directives := strings.Split(headerValue, ",")

	for _, directive := range directives {
		directive = strings.TrimSpace(directive)
		if directive == "" {
			continue
		}

		if parts := strings.SplitN(directive, "=", 2); len(parts) == 2 {
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			value := strings.Trim(strings.TrimSpace(parts[1]), `"`)
			result[key] = value
		} else {
			key := strings.ToLower(directive)
			result[key] = "" // No value provided, just the directive name
		}
	}

	return result
}

func parseAuthorizationHeader(headerValue string) map[string]string {
	result := make(map[string]string)

	// Split on first space to separate auth-scheme from parameters
	parts := strings.SplitN(headerValue, " ", 2)
	if len(parts) < 2 {
		return result
	}

	scheme := strings.ToLower(strings.TrimSpace(parts[0]))
	parameters := strings.TrimSpace(parts[1])

	result["scheme"] = scheme

	// For Basic auth, the parameter is just the credentials
	if scheme == "basic" {
		result["credentials"] = parameters
		return result
	}

	// For Digest and other auth schemes, parse as key-value pairs
	if scheme == "digest" || strings.Contains(parameters, "=") {
		// Parse comma-separated key-value pairs
		pairs := strings.Split(parameters, ",")
		for _, pair := range pairs {
			pair = strings.TrimSpace(pair)
			if parts := strings.SplitN(pair, "=", 2); len(parts) == 2 {
				key := strings.ToLower(strings.TrimSpace(parts[0]))
				value := strings.Trim(strings.TrimSpace(parts[1]), `"`)
				result[key] = value
			}
		}
	} else {
		// For other schemes, store the raw parameters
		result["parameters"] = parameters
	}

	return result
}

var directiveHeaders = map[string]bool{
	"cache-control": true,
	"pragma":        true,
	"warning":       true,
}

var authorizationHeaders = map[string]bool{
	"authorization": true,
}

func NewParsedHeaders(h http.Header) *ParsedHeaders {
	parsed := make(map[string]map[string]string)
	values := make(map[string][]string)

	for name, headerValues := range h {
		if len(headerValues) == 0 {
			continue
		}

		lowerName := strings.ToLower(name)
		fullValue := headerValues

		if directiveHeaders[lowerName] {
			parsed[lowerName] = parseDirectives(strings.Join(fullValue, ","))
		} else if authorizationHeaders[lowerName] {
			parsed[lowerName] = parseAuthorizationHeader(strings.Join(fullValue, " "))
		} else {
			values[lowerName] = fullValue
		}
	}

	return &ParsedHeaders{
		Directives: parsed,
		Values:     values,
	}
}

func (p *ParsedHeaders) GetDirective(headerName, directive string) (string, bool) {
	if h, ok := p.Directives[strings.ToLower(headerName)]; ok {
		val, ok := h[strings.ToLower(directive)]
		return val, ok
	}
	return "", false
}

func (p *ParsedHeaders) GetDirectives(headerName string) (map[string]string, bool) {
	if h, ok := p.Directives[strings.ToLower(headerName)]; ok {
		return h, true
	}
	return nil, false
}

func (p *ParsedHeaders) GetValue(headerName string) ([]string, bool) {
	val, ok := p.Values[strings.ToLower(headerName)]
	return val, ok
}

func (p *ParsedHeaders) GetValidatedAge() int {
	ageStr, hasAge := p.GetValue("Age")
	age := 0
	if hasAge {
		ageInt, err := strconv.Atoi(ageStr[0])
		if err == nil && ageInt > 0 {
			age = ageInt
		}
	}
	return age
}

func (p *ParsedHeaders) IsVaryWildcard() bool {
	vary, ok := p.GetValue("Vary")
	if !ok || len(vary) == 0 {
		return false
	}
	return slices.Contains(vary, "*")
}
