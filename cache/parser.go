package cache

import (
	"net/http"
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

var directiveHeaders = map[string]bool{
	"cache-control": true,
	"pragma":        true,
	"warning":       true,
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

func (p *ParsedHeaders) GetValue(headerName string) ([]string, bool) {
	val, ok := p.Values[strings.ToLower(headerName)]
	return val, ok
}
