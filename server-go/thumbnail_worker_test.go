package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestThumbnailWorkerPool(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalThumbnailDir := THUMBNAIL_DIR
	THUMBNAIL_DIR = filepath.Join(tempDir, "thumbnails")
	os.MkdirAll(THUMBNAIL_DIR, 0755)
	defer func() { THUMBNAIL_DIR = originalThumbnailDir }()

	pool := GetThumbnailPool()
	if pool == nil {
		t.Fatal("Thumbnail pool should be initialized")
	}

	// Test pool is running
	if pool.workers <= 0 {
		t.Error("Pool should have workers")
	}
}

func TestThumbnailExists(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalThumbnailDir := THUMBNAIL_DIR
	THUMBNAIL_DIR = filepath.Join(tempDir, "thumbnails")
	os.MkdirAll(THUMBNAIL_DIR, 0755)
	defer func() { THUMBNAIL_DIR = originalThumbnailDir }()

	// Create a test source file (older than thumbnail)
	sourceFile := filepath.Join(tempDir, "test-source.jpg")
	os.WriteFile(sourceFile, []byte("source file"), 0644)
	
	// Wait a bit to ensure thumbnail is newer
	time.Sleep(100 * time.Millisecond)
	
	// Create a test thumbnail file (newer than source)
	thumbnailPath := GetThumbnailPath(sourceFile)
	os.MkdirAll(filepath.Dir(thumbnailPath), 0755)
	os.WriteFile(thumbnailPath, []byte("fake thumbnail"), 0644)

	exists, isValid := ThumbnailExists(sourceFile)
	if !exists {
		t.Error("Thumbnail should exist")
	}
	if !isValid {
		t.Error("Thumbnail should be valid (newer than source)")
	}

	// Test non-existent thumbnail
	exists, isValid = ThumbnailExists(filepath.Join(tempDir, "non-existent.jpg"))
	if exists {
		t.Error("Thumbnail should not exist")
	}
}

func TestGetThumbnailPath(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalThumbnailDir := THUMBNAIL_DIR
	THUMBNAIL_DIR = filepath.Join(tempDir, "thumbnails")
	defer func() { THUMBNAIL_DIR = originalThumbnailDir }()

	cacheKey := "test/path/to/file"
	thumbnailPath := GetThumbnailPath(cacheKey)

	if thumbnailPath == "" {
		t.Error("Thumbnail path should not be empty")
	}

	if !strings.Contains(thumbnailPath, THUMBNAIL_DIR) {
		t.Errorf("Thumbnail path should be in thumbnail directory. Got: %s, Expected to contain: %s", thumbnailPath, THUMBNAIL_DIR)
	}
}

func TestFfmpegVideoKeyframeArgsSeekBeforeInput(t *testing.T) {
	args := ffmpegVideoKeyframeArgs("in.mkv", "out.jpg", 90)

	ssIdx, inputIdx := -1, -1
	for i, arg := range args {
		if arg == "-ss" && ssIdx == -1 {
			ssIdx = i
		}
		if arg == "-i" && inputIdx == -1 {
			inputIdx = i
		}
	}

	if ssIdx < 0 || inputIdx < 0 {
		t.Fatalf("missing -ss or -i in ffmpeg args: %v", args)
	}
	if ssIdx > inputIdx {
		t.Errorf("fast seek requires -ss before -i, got %v", args)
	}
}

func TestFfmpegVideoKeyframeArgsSkipAudio(t *testing.T) {
	args := ffmpegVideoKeyframeArgs("in.mkv", "out.jpg", 90)
	for _, arg := range args {
		if arg == "-an" {
			return
		}
	}
	t.Errorf("expected -an to skip audio decode, got %v", args)
}

func TestThumbnailCacheKeyIsRelativeAndStable(t *testing.T) {
	original := MEDIA_PATH
	defer func() { MEDIA_PATH = original }()

	dirA := t.TempDir()
	dirB := t.TempDir()

	MEDIA_PATH = dirA
	keyA := thumbnailCacheKey(filepath.Join(dirA, "Animes", "show.mkv"))

	MEDIA_PATH = dirB
	keyB := thumbnailCacheKey(filepath.Join(dirB, "Animes", "show.mkv"))

	if keyA != keyB {
		t.Errorf("cache key must survive MEDIA_PATH changes: %q vs %q", keyA, keyB)
	}
	if strings.Contains(keyA, dirA) || strings.Contains(keyA, dirB) {
		t.Errorf("cache key should be relative, got %q", keyA)
	}
	if filepath.ToSlash(keyA) != "Animes/show.mkv" {
		t.Errorf("expected Animes/show.mkv, got %q", keyA)
	}
}

func TestThumbnailWorkerPoolJobQueue(t *testing.T) {
	pool := GetThumbnailPool()
	if pool == nil {
		t.Fatal("Pool should be initialized")
	}

	// Test that pool has required fields
	if pool.jobQueue == nil {
		t.Error("Job queue should be initialized")
	}
	if pool.priorityQueue == nil {
		t.Error("Priority queue should be initialized")
	}
	if pool.activeJobs == nil {
		t.Error("Active jobs map should be initialized")
	}
}

func TestEnqueueThumbnailJobDeduplicatesBeforeQueue(t *testing.T) {
	pool := &ThumbnailWorkerPool{
		jobQueue:      make(chan ThumbnailJob, 100),
		priorityQueue: make(chan ThumbnailJob, 20),
		activeJobs:    make(map[string]bool),
		queuedJobs:    make(map[string]bool),
	}
	src := filepath.Join(t.TempDir(), "clip.mkv")

	pool.enqueueThumbnailJob(src, src, true, false, PriorityNormal, nil)
	pool.enqueueThumbnailJob(src, src, true, false, PriorityNormal, nil)
	pool.enqueueThumbnailJob(src, src, true, false, PriorityHigh, nil)

	if got := len(pool.jobQueue) + len(pool.priorityQueue); got != 1 {
		t.Fatalf("duplicate jobs must not be enqueued, got %d", got)
	}
	if !pool.HasThumbnailJob(src) {
		t.Fatal("queued job should be visible to HasThumbnailJob")
	}
}

