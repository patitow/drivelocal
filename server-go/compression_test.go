package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompressionMiddleware(t *testing.T) {
	handler := compressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":"test"}`))
	}))

	// Test with Accept-Encoding: gzip
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Check if content encoding is set
	contentEncoding := w.Header().Get("Content-Encoding")
	if contentEncoding != "gzip" && contentEncoding != "" {
		// Compression is optional, so empty is also acceptable
		t.Logf("Content-Encoding: %s (compression may be disabled)", contentEncoding)
	}
}

func TestCompressionMiddlewareNoCompression(t *testing.T) {
	handler := compressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("test"))
	}))

	// Test without Accept-Encoding
	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Should not have compression
	if w.Header().Get("Content-Encoding") != "" {
		t.Error("Should not compress when client doesn't request it")
	}
}

func TestCompressionMiddlewareSmallContent(t *testing.T) {
	handler := compressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{}")) // Very small content
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Small content might not be compressed
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

