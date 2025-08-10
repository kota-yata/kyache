package kyache

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestCustomPathHandler(t *testing.T) {
	originURL, _ := url.Parse("http://example.com")
	cs := New(&Config{})
	handler := cs.Handler(originURL)

	req := httptest.NewRequest("GET", "/statusz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	expectedBody := `{"status":"ok","cache":"running"}`
	if w.Body.String() != expectedBody {
		t.Errorf("Expected body %q, got %q", expectedBody, w.Body.String())
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %q", contentType)
	}
}

func TestRegisterPath(t *testing.T) {
	originURL, _ := url.Parse("http://example.com")
	cs := New(&Config{})

	cs.RegisterPath("/custom", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("custom handler"))
	})

	handler := cs.Handler(originURL)
	req := httptest.NewRequest("GET", "/custom", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	expectedBody := "custom handler"
	if w.Body.String() != expectedBody {
		t.Errorf("Expected body %q, got %q", expectedBody, w.Body.String())
	}
}
