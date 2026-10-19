package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestCorsMiddleware(t *testing.T) {
	// Setup
	os.Setenv("CORS_ORIGINS", "http://localhost:3000,http://example.com")
	defer os.Unsetenv("CORS_ORIGINS")

	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Test allowed origin
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Errorf("Expected CORS header for allowed origin, got %s", w.Header().Get("Access-Control-Allow-Origin"))
	}

	// Test OPTIONS request
	req = httptest.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w = httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 for OPTIONS, got %d", w.Code)
	}
}

func TestCorsMiddlewareDisallowedOrigin(t *testing.T) {
	// Setup - production mode
	os.Setenv("CORS_ORIGINS", "http://localhost:3000")
	os.Setenv("NODE_ENV", "production")
	defer func() {
		os.Unsetenv("CORS_ORIGINS")
		os.Unsetenv("NODE_ENV")
	}()

	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Test disallowed origin
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "http://malicious.com")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403 for disallowed origin, got %d", w.Code)
	}
}

func TestImprovedRateLimitMiddleware(t *testing.T) {
	handler := improvedRateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make multiple requests from same IP
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "10.9.8.7:12345"

	// First DEFAULT_RATE_LIMIT_MAX requests should succeed
	for i := 0; i < DEFAULT_RATE_LIMIT_MAX; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("Request %d should succeed, got %d", i+1, w.Code)
		}
	}

	// the next one should be rate limited
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429, got %d", w.Code)
	}
}

func TestSkipRateLimit(t *testing.T) {
	if !skipRateLimit("/api/health") {
		t.Fatal("health should skip")
	}
	if !skipRateLimit("/drive/Animes") {
		t.Fatal("static/SPA should skip")
	}
	if !skipRateLimit("/api/stream/foo.mkv") {
		t.Fatal("stream should skip")
	}
	if !skipRateLimit("/api/download/foo.mkv") {
		t.Fatal("download should skip")
	}
	if !skipRateLimit("/api/thumbnail/Animes/show/ep.mkv") {
		t.Fatal("thumbnail GET should skip")
	}
	if !skipRateLimit("/api/thumbnails") {
		t.Fatal("thumbnail batch should skip")
	}
	if skipRateLimit("/api/files") {
		t.Fatal("/api/files should still be rate limited")
	}
	if skipRateLimit("/api/auth/login") {
		t.Fatal("login should still be rate limited")
	}
}

func TestImprovedRateLimitMiddlewareSkipsThumbnails(t *testing.T) {
	handler := improvedRateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/thumbnail/Animes/show/ep.mkv", nil)
	req.RemoteAddr = "10.0.0.99:12345"

	for i := 0; i < 250; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("thumbnail request %d should not be rate limited, got %d", i+1, w.Code)
		}
	}

	batch := httptest.NewRequest("POST", "/api/thumbnails", nil)
	batch.RemoteAddr = "10.0.0.99:12345"
	for i := 0; i < 50; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, batch)
		if w.Code != http.StatusOK {
			t.Fatalf("thumbnail batch %d should not be rate limited, got %d", i+1, w.Code)
		}
	}
}

func TestImprovedRateLimitMiddlewareAuthEndpoint(t *testing.T) {
	handler := improvedRateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/auth/login", nil)
	req.RemoteAddr = "127.0.0.1:12346"

	// First 10 requests should succeed
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("Auth request %d should succeed, got %d", i+1, w.Code)
		}
	}

	// 11th request should be rate limited
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429 for auth endpoint, got %d", w.Code)
	}
}

func TestPriorityMiddleware(t *testing.T) {
	handler := priorityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Test streaming request
	req := httptest.NewRequest("GET", "/api/stream/test.mp4", nil)
	req.RemoteAddr = "127.0.0.1:12347"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Verify streaming request is tracked
	streamingMutex.RLock()
	_, exists := activeStreamingRequests["127.0.0.1"]
	streamingMutex.RUnlock()

	if exists {
		t.Error("Streaming request should be cleaned up after completion")
	}
}

func TestIsLocalNetworkOrigin(t *testing.T) {
	tests := []struct {
		origin string
		want   bool
	}{
		{"http://192.168.0.2", true},
		{"http://192.168.0.2:80", true},
		{"http://10.0.0.5", true},
		{"http://127.0.0.1", true},
		{"http://localhost", true},
		{"http://localhost:80", true},
		{"http://100.64.1.2", true},
		{"https://evil.example", false},
		{"http://8.8.8.8", false},
	}

	for _, tt := range tests {
		t.Run(tt.origin, func(t *testing.T) {
			got := isLocalNetworkOrigin(tt.origin)
			if got != tt.want {
				t.Errorf("isLocalNetworkOrigin(%q)=%v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}

func TestCorsMiddlewareAllowsLanHostname(t *testing.T) {
	os.Setenv("NODE_ENV", "production")
	os.Unsetenv("CORS_ORIGINS")
	defer os.Unsetenv("NODE_ENV")

	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/files", nil)
	req.Host = "homeserver"
	req.RemoteAddr = "192.168.0.15:54321"
	req.Header.Set("Origin", "http://homeserver")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("LAN hostname origin should be allowed, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCookieSecureOnlyWithTLS(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	if cookieSecure(req) {
		t.Fatal("HTTP request must not mark cookies Secure")
	}

	httpsReq := httptest.NewRequest("GET", "https://example.local/", nil)
	httpsReq.TLS = &tls.ConnectionState{}
	if !cookieSecure(httpsReq) {
		t.Fatal("HTTPS request should mark cookies Secure")
	}
}

func TestCorsMiddlewareAllowsPrivateLAN(t *testing.T) {
	os.Setenv("NODE_ENV", "production")
	os.Unsetenv("CORS_ORIGINS")
	defer os.Unsetenv("NODE_ENV")

	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Header.Set("Origin", "http://192.168.0.2")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("LAN origin should be allowed, got %d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://192.168.0.2" {
		t.Errorf("expected reflected LAN origin, got %s", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestShouldSkipAccessLog(t *testing.T) {
	thumb := httptest.NewRequest("GET", "/api/thumbnail/clip.mkv", nil)
	if !shouldSkipAccessLog(thumb, http.StatusOK) {
		t.Fatal("successful thumbnail GET should not be logged")
	}
	if !shouldSkipAccessLog(thumb, http.StatusAccepted) {
		t.Fatal("pending thumbnail GET should not be logged")
	}
	if shouldSkipAccessLog(thumb, http.StatusNotFound) {
		t.Fatal("thumbnail miss should be logged")
	}
	if shouldSkipAccessLog(thumb, http.StatusInternalServerError) {
		t.Fatal("thumbnail error should be logged")
	}

	files := httptest.NewRequest("GET", "/api/files", nil)
	if shouldSkipAccessLog(files, http.StatusOK) {
		t.Fatal("non-thumbnail requests should still be logged")
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name     string
		request  *http.Request
		expected string
	}{
		{
			name: "X-Forwarded-For header",
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/", nil)
				req.Header.Set("X-Forwarded-For", "192.168.1.1, 10.0.0.1")
				return req
			}(),
			expected: "192.168.1.1",
		},
		{
			name: "X-Real-IP header",
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/", nil)
				req.Header.Set("X-Real-IP", "192.168.1.2")
				return req
			}(),
			expected: "192.168.1.2",
		},
		{
			name: "RemoteAddr",
			request: func() *http.Request {
				req := httptest.NewRequest("GET", "/", nil)
				req.RemoteAddr = "192.0.2.1:12345"
				return req
			}(),
			expected: "192.0.2.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := getClientIP(tt.request)
			if ip != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, ip)
			}
		})
	}
}
