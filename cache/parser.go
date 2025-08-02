package cache

import (
	"net/http"
	"strings"
)

type ParsedHeaders struct {
	Directives map[string]map[string]string
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

func NewParsedHeaders(h http.Header) *ParsedHeaders {
	parsed := make(map[string]map[string]string)

	for name, values := range h {
		if len(values) == 0 {
			continue
		}

		fullValue := strings.Join(values, ", ")
		parsed[strings.ToLower(name)] = parseDirectives(fullValue)
	}

	return &ParsedHeaders{Directives: parsed}
}

func (p *ParsedHeaders) GetDirective(headerName, directive string) (string, bool) {
	if h, ok := p.Directives[strings.ToLower(headerName)]; ok {
		val, ok := h[strings.ToLower(directive)]
		return val, ok
	}
	return "", false
}
