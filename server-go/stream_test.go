package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"
)

func TestStreamHandler(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalMediaPath := MEDIA_PATH
	MEDIA_PATH = tempDir
	defer func() { MEDIA_PATH = originalMediaPath }()

	// Create test file
	testFile := filepath.Join(tempDir, "test.mp4")
	content := make([]byte, 2048)
	for i := range content {
		content[i] = byte(i % 256)
	}
	os.WriteFile(testFile, content, 0644)

	// Test request
	req := httptest.NewRequest("GET", "/api/stream/test.mp4", nil)
	req.Header.Set("Range", "bytes=0-1023")
	req = mux.SetURLVars(req, map[string]string{"path": "test.mp4"})
	w := httptest.NewRecorder()

	streamHandler(w, req)

	if w.Code != http.StatusPartialContent {
		t.Errorf("Expected status 206, got %d. Body: %s", w.Code, w.Body.String())
	}

	if w.Header().Get("Content-Range") == "" {
		t.Error("Should set Content-Range header")
	}

	if w.Header().Get("Accept-Ranges") != "bytes" {
		t.Error("Should set Accept-Ranges header")
	}
}

func TestStreamHandlerFullFile(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalMediaPath := MEDIA_PATH
	MEDIA_PATH = tempDir
	defer func() { MEDIA_PATH = originalMediaPath }()

	// Create test file
	testFile := filepath.Join(tempDir, "test.mp4")
	content := []byte("test content")
	os.WriteFile(testFile, content, 0644)

	req := httptest.NewRequest("GET", "/api/stream/test.mp4", nil)
	req = mux.SetURLVars(req, map[string]string{"path": "test.mp4"})
	w := httptest.NewRecorder()

	streamHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if w.Body.String() != string(content) {
		t.Errorf("Expected content '%s', got '%s'", string(content), w.Body.String())
	}
}

func TestStreamHandlerNotFound(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalMediaPath := MEDIA_PATH
	MEDIA_PATH = tempDir
	defer func() { MEDIA_PATH = originalMediaPath }()

	req := httptest.NewRequest("GET", "/api/stream/nonexistent.mp4", nil)
	req = mux.SetURLVars(req, map[string]string{"path": "nonexistent.mp4"})
	w := httptest.NewRecorder()

	streamHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestStreamHandlerPathTraversal(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalMediaPath := MEDIA_PATH
	MEDIA_PATH = tempDir
	defer func() { MEDIA_PATH = originalMediaPath }()

	req := httptest.NewRequest("GET", "/api/stream/../../etc/passwd", nil)
	req = mux.SetURLVars(req, map[string]string{"path": "../../etc/passwd"})
	w := httptest.NewRecorder()

	streamHandler(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", w.Code)
	}
}

