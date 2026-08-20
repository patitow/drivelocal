package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func isBenchImage(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".tiff":
		return true
	default:
		return false
	}
}

func isBenchVideo(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".mp4", ".avi", ".mov", ".wmv", ".flv", ".webm", ".mkv", ".m4v":
		return true
	default:
		return false
	}
}

func copyDiscard(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(io.Discard, f)
	return err
}

func parallelUntil(paths []string, workers int, n int) (first, tenth, all time.Duration) {
	if n > len(paths) {
		n = len(paths)
	}
	if n == 0 {
		return
	}
	start := time.Now()
	var mu sync.Mutex
	done := 0
	var firstOnce, tenthOnce sync.Once
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		p := paths[i]
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			_ = copyDiscard(p)
			<-sem
			mu.Lock()
			done++
			d := done
			elapsed := time.Since(start)
			mu.Unlock()
			if d == 1 {
				firstOnce.Do(func() { first = elapsed })
			}
			if d == 10 || (n < 10 && d == n) {
				tenthOnce.Do(func() { tenth = elapsed })
			}
		}()
	}
	wg.Wait()
	all = time.Since(start)
	if n < 10 {
		tenth = all
	}
	return
}

func TestBloomThumbnailLatency(t *testing.T) {
	if os.Getenv("BLOOM_BENCH") != "1" {
		t.Skip("set BLOOM_BENCH=1 to run")
	}

	dir := os.Getenv("BLOOM_BENCH_DIR")
	if dir == "" {
		t.Skip("set BLOOM_BENCH_DIR to a media folder")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > 100 {
		entries = entries[:100]
	}

	var images []string
	var videos []string
	var listed []string
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		listed = append(listed, p)
		if isBenchImage(e.Name()) {
			images = append(images, p)
		}
		if isBenchVideo(e.Name()) {
			videos = append(videos, p)
		}
	}

	t.Logf("page items=%d images=%d videos=%d", len(listed), len(images), len(videos))

	first, tenth, all := parallelUntil(images, 6, len(images))
	t.Logf("OLD images (6 conn, serve original): first=%s first10=%s all%d=%s", first, tenth, len(images), all)

	thumbDir := t.TempDir()
	originalThumb := THUMBNAIL_DIR
	THUMBNAIL_DIR = thumbDir
	defer func() { THUMBNAIL_DIR = originalThumb }()

	pool := &ThumbnailWorkerPool{
		workers:       4,
		jobQueue:      make(chan ThumbnailJob, 200),
		priorityQueue: make(chan ThumbnailJob, 20),
		activeJobs:    make(map[string]bool),
		queuedJobs:    make(map[string]bool),
	}
	pool.start()

	start := time.Now()
	var mu sync.Mutex
	imageDone := 0
	var firstImg, tenImg, allImg time.Duration
	var firstImgOnce, tenImgOnce, allImgOnce sync.Once
	doneCh := make(chan struct{}, 1)

	targetImages := len(images)
	for _, p := range listed {
		isVid := isBenchVideo(filepath.Base(p))
		isImg := isBenchImage(filepath.Base(p))
		if !isVid && !isImg {
			continue
		}
		src := p
		imgJob := isImg
		pool.QueueJob(src, src, isVid, PriorityLow, func(error) {
			if !imgJob {
				return
			}
			mu.Lock()
			imageDone++
			n := imageDone
			elapsed := time.Since(start)
			mu.Unlock()
			if n == 1 {
				firstImgOnce.Do(func() { firstImg = elapsed })
			}
			if n == 10 {
				tenImgOnce.Do(func() { tenImg = elapsed })
			}
			if n == targetImages {
				allImgOnce.Do(func() {
					allImg = elapsed
					select {
					case doneCh <- struct{}{}:
					default:
					}
				})
			}
		})
	}

	select {
	case <-doneCh:
	case <-time.After(3 * time.Minute):
		t.Fatalf("timeout waiting for image thumbs, done=%d/%d first=%s ten=%s", imageDone, targetImages, firstImg, tenImg)
	}

	t.Logf("NEW images (4 workers, queued behind listing order incl videos): first=%s first10=%s all%d=%s", firstImg, tenImg, targetImages, allImg)

	if len(images) > 0 {
		out := filepath.Join(thumbDir, "one.jpg")
		oneStart := time.Now()
		err := pool.generateImageThumbnail(images[0], out)
		t.Logf("single image JPEG encode: %s err=%v", time.Since(oneStart), err)
	}
	if len(videos) > 0 {
		out := filepath.Join(thumbDir, "onevid.jpg")
		oneStart := time.Now()
		err := pool.generateVideoThumbnail(videos[0], out)
		t.Logf("single video ffmpeg thumb: %s err=%v", time.Since(oneStart), err)
	}
}
