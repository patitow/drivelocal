package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

func init() {
	// Initialize logger for tests
	initLogger()
}

func TestHealthHandler(t *testing.T) {
	// Setup - ensure MEDIA_PATH exists
	tempDir := t.TempDir()
	originalMediaPath := MEDIA_PATH
	MEDIA_PATH = tempDir
	defer func() { MEDIA_PATH = originalMediaPath }()

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()

	// Execute
	healthHandler(w, req)

	// Assert
	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 200 or 503, got %d. Body: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if status, ok := response["status"].(string); !ok || status == "" {
		t.Error("Response should contain 'status' field")
	}

	if checks, ok := response["checks"].(map[string]interface{}); ok {
		if _, ok := checks["mediaPath"]; !ok {
			t.Error("Health check should include mediaPath check")
		}
		if _, ok := checks["thumbnailPool"]; !ok {
			t.Error("Health check should include thumbnailPool check")
		}
	}
}

func TestFilesHandler(t *testing.T) {
	// Setup - create a temporary directory
	tempDir := t.TempDir()
	originalMediaPath := MEDIA_PATH
	MEDIA_PATH = tempDir
	defer func() { MEDIA_PATH = originalMediaPath }()

	// Create test directory structure
	testDir := filepath.Join(tempDir, "test")
	os.MkdirAll(testDir, 0755)

	// Create test file
	testFile := filepath.Join(testDir, "test.txt")
	os.WriteFile(testFile, []byte("test content"), 0644)

	// Test request
	req := httptest.NewRequest("GET", "/api/files?path=test", nil)
	w := httptest.NewRecorder()

	// Execute
	filesHandler(w, req)

	// Assert
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if items, ok := response["items"].([]interface{}); ok {
		if len(items) == 0 {
			t.Error("Should return at least one item")
		}
	}
}

func TestFilesHandlerWithPagination(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalMediaPath := MEDIA_PATH
	MEDIA_PATH = tempDir
	defer func() { MEDIA_PATH = originalMediaPath }()

	// Create multiple test files
	testDir := filepath.Join(tempDir, "test")
	os.MkdirAll(testDir, 0755)
	for i := 0; i < 150; i++ {
		os.WriteFile(filepath.Join(testDir, "file"+string(rune(i))+".txt"), []byte("test"), 0644)
	}

	// Test with limit
	req := httptest.NewRequest("GET", "/api/files?path=test&limit=50&offset=0", nil)
	w := httptest.NewRecorder()

	filesHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	if pagination, ok := response["pagination"].(map[string]interface{}); ok {
		if limit, ok := pagination["limit"].(float64); !ok || limit != 50 {
			t.Errorf("Expected limit 50, got %v", limit)
		}
	}
}

// TestFilesHandlerResolvesJunctionAsDirectory guards against a regression
// where Windows directory junctions (used to union several drives/folders
// under one MEDIA_PATH, e.g. via mklink /J) were listed as 0-byte "files"
// instead of folders: os.ReadDir uses lstat semantics, so a junction's
// DirEntry.IsDir() is false even though it resolves to a real directory.
func TestFilesHandlerResolvesJunctionAsDirectory(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("directory junctions are a Windows-only concept")
	}

	tempDir := t.TempDir()
	originalMediaPath := MEDIA_PATH
	MEDIA_PATH = tempDir
	defer func() { MEDIA_PATH = originalMediaPath }()

	realDir := filepath.Join(t.TempDir(), "real-target")
	if err := os.MkdirAll(realDir, 0755); err != nil {
		t.Fatalf("failed to create real target dir: %v", err)
	}
	os.WriteFile(filepath.Join(realDir, "inside.txt"), []byte("hi"), 0644)

	junctionPath := filepath.Join(tempDir, "Linked")
	out, err := exec.Command("cmd", "/c", "mklink", "/J", junctionPath, realDir).CombinedOutput()
	if err != nil {
		t.Skipf("could not create junction (needs Windows mklink): %v, output: %s", err, out)
	}

	req := httptest.NewRequest("GET", "/api/files?path=", nil)
	w := httptest.NewRecorder()
	filesHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var response struct {
		Items []struct {
			Name        string `json:"name"`
			IsDirectory bool   `json:"isDirectory"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	found := false
	for _, item := range response.Items {
		if item.Name == "Linked" {
			found = true
			if !item.IsDirectory {
				t.Errorf("junction %q should be reported as a directory, got isDirectory=false", item.Name)
			}
		}
	}
	if !found {
		t.Fatal("junction entry not found in listing")
	}
}

func TestDownloadHandler(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalMediaPath := MEDIA_PATH
	MEDIA_PATH = tempDir
	defer func() { MEDIA_PATH = originalMediaPath }()

	// Create test file
	testFile := filepath.Join(tempDir, "test.txt")
	os.WriteFile(testFile, []byte("test content"), 0644)

	// Test request
	req := httptest.NewRequest("GET", "/api/download/test.txt", nil)
	req = mux.SetURLVars(req, map[string]string{"path": "test.txt"})
	w := httptest.NewRecorder()

	// Execute
	downloadHandler(w, req)

	// Assert
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	if w.Header().Get("Content-Type") == "" {
		t.Error("Should set Content-Type header")
	}

	if w.Body.String() != "test content" {
		t.Errorf("Expected 'test content', got '%s'", w.Body.String())
	}
}

func TestDownloadHandlerNotFound(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalMediaPath := MEDIA_PATH
	MEDIA_PATH = tempDir
	defer func() { MEDIA_PATH = originalMediaPath }()

	req := httptest.NewRequest("GET", "/api/download/nonexistent.txt", nil)
	req = mux.SetURLVars(req, map[string]string{"path": "nonexistent.txt"})
	w := httptest.NewRecorder()

	downloadHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestDownloadHandlerPathTraversal(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalMediaPath := MEDIA_PATH
	MEDIA_PATH = tempDir
	defer func() { MEDIA_PATH = originalMediaPath }()

	// Try path traversal
	req := httptest.NewRequest("GET", "/api/download/../../etc/passwd", nil)
	req = mux.SetURLVars(req, map[string]string{"path": "../../etc/passwd"})
	w := httptest.NewRecorder()

	downloadHandler(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", w.Code)
	}
}

func TestDownloadMultipleHandler(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalMediaPath := MEDIA_PATH
	MEDIA_PATH = tempDir
	defer func() { MEDIA_PATH = originalMediaPath }()

	// Create test files
	os.WriteFile(filepath.Join(tempDir, "file1.txt"), []byte("content1"), 0644)
	os.WriteFile(filepath.Join(tempDir, "file2.txt"), []byte("content2"), 0644)

	// Test request
	body := map[string]interface{}{
		"files": []string{"file1.txt", "file2.txt"},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/download/multiple", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Execute
	downloadMultipleHandler(w, req)

	// Assert
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	if w.Header().Get("Content-Type") != "application/zip" {
		t.Error("Should return ZIP content type")
	}

	if w.Body.Len() == 0 {
		t.Error("Should return ZIP content")
	}
}

func TestDownloadMultipleHandlerTooManyFiles(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalMediaPath := MEDIA_PATH
	MEDIA_PATH = tempDir
	defer func() { MEDIA_PATH = originalMediaPath }()

	// Create request with too many files
	files := make([]string, 101)
	for i := 0; i < 101; i++ {
		files[i] = "file" + string(rune(i)) + ".txt"
	}

	body := map[string]interface{}{
		"files": files,
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/download/multiple", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	downloadMultipleHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestThumbnailHandlerVideoMissReturnsAcceptedWithoutCaching(t *testing.T) {
	tempDir := t.TempDir()
	originalMediaPath := MEDIA_PATH
	originalThumbs := THUMBNAIL_DIR
	MEDIA_PATH = tempDir
	THUMBNAIL_DIR = filepath.Join(tempDir, "thumbs")
	os.MkdirAll(THUMBNAIL_DIR, 0755)
	defer func() {
		MEDIA_PATH = originalMediaPath
		THUMBNAIL_DIR = originalThumbs
	}()

	os.WriteFile(filepath.Join(tempDir, "clip.mp4"), []byte("not a real video"), 0644)

	router := mux.NewRouter()
	router.HandleFunc("/api/thumbnail/{path:.*}", thumbnailHandler)

	req := httptest.NewRequest("GET", "/api/thumbnail/clip.mp4", nil)
	w := httptest.NewRecorder()
	start := time.Now()
	router.ServeHTTP(w, req)
	elapsed := time.Since(start)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", w.Code, w.Body.String())
	}
	if elapsed > time.Second {
		t.Errorf("pending thumbnail should return immediately, took %s", elapsed)
	}
	cc := w.Header().Get("Cache-Control")
	if strings.Contains(cc, "immutable") || strings.Contains(cc, "max-age=31536000") {
		t.Errorf("pending thumbnail must not be cached, Cache-Control=%s", cc)
	}
}

func TestThumbnailHandlerServesCachedJpegWithLongCache(t *testing.T) {
	tempDir := t.TempDir()
	originalMediaPath := MEDIA_PATH
	originalThumbs := THUMBNAIL_DIR
	MEDIA_PATH = tempDir
	THUMBNAIL_DIR = filepath.Join(tempDir, "thumbs")
	os.MkdirAll(THUMBNAIL_DIR, 0755)
	defer func() {
		MEDIA_PATH = originalMediaPath
		THUMBNAIL_DIR = originalThumbs
	}()

	video := filepath.Join(tempDir, "clip.mp4")
	os.WriteFile(video, []byte("not a real video"), 0644)
	time.Sleep(20 * time.Millisecond)

	thumb := GetThumbnailPath(video)
	os.MkdirAll(filepath.Dir(thumb), 0755)
	os.WriteFile(thumb, []byte{0xff, 0xd8, 0xff, 0xd9}, 0644)

	router := mux.NewRouter()
	router.HandleFunc("/api/thumbnail/{path:.*}", thumbnailHandler)

	req := httptest.NewRequest("GET", "/api/thumbnail/clip.mp4", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %s", ct)
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "max-age=31536000") {
		t.Errorf("cached jpeg should have long Cache-Control, got %s", w.Header().Get("Cache-Control"))
	}
}

func TestThumbnailHandlerImageMissAllowsBrowserCache(t *testing.T) {
	tempDir := t.TempDir()
	originalMediaPath := MEDIA_PATH
	originalThumbs := THUMBNAIL_DIR
	MEDIA_PATH = tempDir
	THUMBNAIL_DIR = filepath.Join(tempDir, "thumbs")
	os.MkdirAll(THUMBNAIL_DIR, 0755)
	defer func() {
		MEDIA_PATH = originalMediaPath
		THUMBNAIL_DIR = originalThumbs
	}()

	os.WriteFile(filepath.Join(tempDir, "photo.jpg"), []byte("fake-jpeg"), 0644)

	router := mux.NewRouter()
	router.HandleFunc("/api/thumbnail/{path:.*}", thumbnailHandler)

	req := httptest.NewRequest("GET", "/api/thumbnail/photo.jpg", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	cc := w.Header().Get("Cache-Control")
	if strings.Contains(cc, "no-store") {
		t.Fatalf("image fallback must be cacheable on refresh, Cache-Control=%s", cc)
	}
	// Must force revalidation (no-cache), not a long max-age: this response is
	// the full-size original served only until the real thumbnail exists. A
	// long-lived max-age would let the browser keep serving the full-size
	// file from cache even after the small thumbnail is ready.
	if !strings.Contains(cc, "no-cache") {
		t.Fatalf("image fallback should force revalidation via no-cache, Cache-Control=%s", cc)
	}
	if strings.Contains(cc, "max-age=") && !strings.Contains(cc, "max-age=0") {
		t.Fatalf("image fallback must not set a long-lived max-age, Cache-Control=%s", cc)
	}
	if w.Header().Get("ETag") == "" {
		t.Fatal("image fallback should set ETag so refresh can 304")
	}

	req2 := httptest.NewRequest("GET", "/api/thumbnail/photo.jpg", nil)
	req2.Header.Set("If-None-Match", w.Header().Get("ETag"))
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotModified {
		t.Fatalf("refresh revalidation should 304, got %d", w2.Code)
	}
}

func TestFindFolderPreviewSourcesUsesImagesThenSubfolders(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.jpg"), []byte("img"), 0644)
	os.WriteFile(filepath.Join(dir, "b.png"), []byte("img"), 0644)

	nested := filepath.Join(dir, "only-folders", "inner")
	os.MkdirAll(nested, 0755)
	os.WriteFile(filepath.Join(nested, "c.jpeg"), []byte("img"), 0644)

	sources := findFolderPreviewSources(dir, 6)
	if len(sources) < 3 {
		t.Fatalf("expected images + nested preview, got %v", sources)
	}

	got := map[string]bool{}
	for _, src := range sources {
		got[filepath.Base(src)] = true
	}
	for _, name := range []string{"a.jpg", "b.png", "c.jpeg"} {
		if !got[name] {
			t.Errorf("missing %s in %v", name, sources)
		}
	}
}

func TestFindFolderPreviewSourcesCapsAtSix(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("%02d.jpg", i)), []byte("img"), 0644)
	}
	sources := findFolderPreviewSources(dir, 6)
	if len(sources) != 6 {
		t.Fatalf("expected 6 sources, got %d", len(sources))
	}
}

func TestThumbnailsBatchHandler(t *testing.T) {
	mediaDir := t.TempDir()
	thumbDir := t.TempDir()
	originalMedia := MEDIA_PATH
	originalThumbs := THUMBNAIL_DIR
	MEDIA_PATH = mediaDir
	THUMBNAIL_DIR = thumbDir
	defer func() {
		MEDIA_PATH = originalMedia
		THUMBNAIL_DIR = originalThumbs
	}()

	os.WriteFile(filepath.Join(mediaDir, "clip.mkv"), []byte("fake-video"), 0644)
	os.WriteFile(filepath.Join(mediaDir, "photo.jpg"), []byte("fake-image"), 0644)
	os.WriteFile(filepath.Join(mediaDir, "fresh.png"), []byte("fake-png"), 0644)

	readyKey := filepath.Join(mediaDir, "photo.jpg")
	thumbPath := GetThumbnailPath(readyKey)
	os.MkdirAll(filepath.Dir(thumbPath), 0755)
	os.WriteFile(thumbPath, []byte("jpeg-bytes"), 0644)
	os.Chtimes(thumbPath, time.Now().Add(time.Second), time.Now().Add(time.Second))

	body := bytes.NewBufferString(`{"paths":["clip.mkv","photo.jpg","fresh.png","missing.mp4","clip.mkv"]}`)
	req := httptest.NewRequest("POST", "/api/thumbnails", body)
	w := httptest.NewRecorder()
	thumbnailsBatchHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var response struct {
		Items []struct {
			Path   string `json:"path"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	got := map[string]string{}
	for _, item := range response.Items {
		got[filepath.ToSlash(item.Path)] = item.Status
	}
	if got["clip.mkv"] != "pending" {
		t.Errorf("clip.mkv status=%q, want pending", got["clip.mkv"])
	}
	if got["photo.jpg"] != "ready" {
		t.Errorf("photo.jpg status=%q, want ready", got["photo.jpg"])
	}
	if got["fresh.png"] != "ready" {
		t.Errorf("fresh.png without cached jpeg should still be ready, got %q", got["fresh.png"])
	}
	if got["missing.mp4"] != "missing" {
		t.Errorf("missing.mp4 status=%q, want missing", got["missing.mp4"])
	}
	if len(response.Items) != 4 {
		t.Errorf("duplicate paths should collapse, got %d items", len(response.Items))
	}
}

func TestFolderCollageLayout(t *testing.T) {
	cases := map[int][2]int{
		1: {1, 1},
		2: {2, 1},
		3: {3, 1},
		4: {2, 2},
		5: {3, 2},
		6: {3, 2},
	}
	for n, want := range cases {
		cols, rows := folderCollageLayout(n)
		if cols != want[0] || rows != want[1] {
			t.Errorf("n=%d got %dx%d want %dx%d", n, cols, rows, want[0], want[1])
		}
	}
}
