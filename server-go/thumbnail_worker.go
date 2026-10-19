package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"
)

// ThumbnailJob represents a thumbnail generation job
type ThumbnailJob struct {
	FilePath      string
	ThumbnailPath string
	IsVideo       bool
	IsFolder      bool
	Priority      int         // Higher = more priority
	Callback      func(error) // Called when job completes
}

// ThumbnailWorkerPool manages thumbnail generation workers
type ThumbnailWorkerPool struct {
	workers       int
	jobQueue      chan ThumbnailJob
	priorityQueue chan ThumbnailJob
	wg            sync.WaitGroup
	mu            sync.RWMutex
	activeJobs    map[string]bool      // Track active jobs to avoid duplicates
	queuedJobs    map[string]bool      // Dedup before the job hits a worker
	failedJobs    map[string]time.Time // thumbnailPath -> last failure, for backoff
}

// failureCooldown is how long a permanently-failing job (e.g. a video ffmpeg
// can't decode) is left alone before being retried, so a single broken file
// doesn't get re-run on every poll cycle forever.
const failureCooldown = 5 * time.Minute

var (
	thumbnailPool *ThumbnailWorkerPool
	poolOnce      sync.Once
)

const (
	// Priority levels
	PriorityHigh   = 100 // Streaming/download requests
	PriorityNormal = 50  // Normal thumbnail requests
	PriorityLow    = 10  // Background/prefetch thumbnails

	// Worker pool size - adjust based on CPU cores
	DefaultWorkers        = 2 // Start with 2 workers, can be increased
	thumbnailWidth        = 480
	folderCollageSlots    = 6
	folderPreviewMaxDepth = 3
)

func defaultThumbnailWorkers() int {
	if envWorkers := os.Getenv("THUMBNAIL_WORKERS"); envWorkers != "" {
		if n, err := strconv.Atoi(envWorkers); err == nil && n > 0 {
			return n
		}
	}
	n := runtime.NumCPU()
	if n < 2 {
		return 2
	}
	if n > 4 {
		return 4
	}
	return n
}

// GetThumbnailPool returns the singleton thumbnail worker pool
func GetThumbnailPool() *ThumbnailWorkerPool {
	poolOnce.Do(func() {
		workers := defaultThumbnailWorkers()

		thumbnailPool = &ThumbnailWorkerPool{
			workers:       workers,
			jobQueue:      make(chan ThumbnailJob, 100), // Buffer for normal jobs
			priorityQueue: make(chan ThumbnailJob, 20),  // Buffer for high priority jobs
			activeJobs:    make(map[string]bool),
			queuedJobs:    make(map[string]bool),
			failedJobs:    make(map[string]time.Time),
		}
		thumbnailPool.start()
	})
	return thumbnailPool
}

// start initializes the worker pool
func (p *ThumbnailWorkerPool) start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	logInfo("📸 Thumbnail worker pool started with %d workers", p.workers)
}

// worker processes thumbnail jobs
func (p *ThumbnailWorkerPool) worker(id int) {
	defer p.wg.Done()

	for {
		var job ThumbnailJob
		select {
		case job = <-p.priorityQueue:
			// High priority jobs first
		case job = <-p.jobQueue:
			// Normal priority jobs
		}

		p.mu.Lock()
		delete(p.queuedJobs, job.ThumbnailPath)
		if p.activeJobs[job.ThumbnailPath] {
			p.mu.Unlock()
			if job.Callback != nil {
				job.Callback(fmt.Errorf("job already in progress"))
			}
			continue
		}
		p.activeJobs[job.ThumbnailPath] = true
		p.mu.Unlock()

		// Process the job
		err := p.processJob(job)
		if err != nil {
			logWarn("Thumbnail job failed (%s): %v", job.FilePath, err)
		}

		// Mark job as complete, and remember failures so we back off instead
		// of re-running a permanently-broken job on every poll cycle.
		p.mu.Lock()
		delete(p.activeJobs, job.ThumbnailPath)
		if err != nil {
			p.failedJobs[job.ThumbnailPath] = time.Now()
		} else {
			delete(p.failedJobs, job.ThumbnailPath)
		}
		p.mu.Unlock()

		// Call callback if provided
		if job.Callback != nil {
			job.Callback(err)
		}
	}
}

// processJob generates the thumbnail
func (p *ThumbnailWorkerPool) processJob(job ThumbnailJob) error {
	if job.IsFolder {
		return p.generateFolderThumbnail(job.FilePath, job.ThumbnailPath)
	}
	if job.IsVideo {
		return p.generateVideoThumbnail(job.FilePath, job.ThumbnailPath)
	}
	return p.generateImageThumbnail(job.FilePath, job.ThumbnailPath)
}

// generateImageThumbnail generates thumbnail for images using Go-native library
func (p *ThumbnailWorkerPool) generateImageThumbnail(filePath, thumbnailPath string) error {
	// Open image file
	src, err := imaging.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open image: %w", err)
	}

	// Resize image maintaining aspect ratio (max width 320px)
	dst := imaging.Resize(src, thumbnailWidth, 0, imaging.Lanczos)

	// Create thumbnail directory if it doesn't exist
	dir := filepath.Dir(thumbnailPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create thumbnail directory: %w", err)
	}

	// Save as JPEG with quality 85 (good balance between size and quality)
	opts := imaging.JPEGQuality(85)
	if err := imaging.Save(dst, thumbnailPath, opts); err != nil {
		return fmt.Errorf("failed to save thumbnail: %w", err)
	}

	return nil
}

func ffmpegCoverArtArgs(input, output string) []string {
	return []string{
		"-hide_banner", "-loglevel", "error",
		"-i", input,
		"-an",
		"-map", "0:v:m:attached_pic",
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale=%d:-2:flags=fast_bilinear", thumbnailWidth),
		"-q:v", "5",
		"-y", output,
	}
}

// ffmpegVideoKeyframeArgs tira o quadro em "at" segundos; o filtro thumbnail
// escolhe o mais representativo entre os próximos quadros (evita tela preta).
func ffmpegVideoKeyframeArgs(input, output string, at int) []string {
	return []string{
		"-hide_banner", "-loglevel", "error",
		"-ss", strconv.Itoa(at),
		"-noaccurate_seek",
		"-i", input,
		"-an",
		"-sn",
		"-map", "0:v:0",
		"-frames:v", "1",
		"-vf", fmt.Sprintf("thumbnail=24,scale=%d:-2:flags=fast_bilinear", thumbnailWidth),
		"-q:v", "5",
		"-y", output,
	}
}

func runFfmpeg(timeout time.Duration, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w, stderr: %s", err, stderr.String())
	}
	return nil
}

func thumbnailFileUsable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 100
}

func thumbnailCacheKey(path string) string {
	clean := filepath.Clean(path)
	media := filepath.Clean(MEDIA_PATH)
	rel, err := filepath.Rel(media, clean)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(clean)
	}
	return filepath.ToSlash(rel)
}

// generateVideoThumbnail generates thumbnail for videos using ffmpeg (still needed)
func (p *ThumbnailWorkerPool) generateVideoThumbnail(filePath, thumbnailPath string) error {
	dir := filepath.Dir(thumbnailPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create thumbnail directory: %w", err)
	}

	if err := runFfmpeg(1500*time.Millisecond, ffmpegCoverArtArgs(filePath, thumbnailPath)); err == nil && thumbnailFileUsable(thumbnailPath) {
		return nil
	}

	// Começo de episódio costuma ser preto: tenta um ponto mais adiante e volta
	// ao início quando o vídeo é curto demais para ele.
	for _, at := range []int{90, 15, 1} {
		os.Remove(thumbnailPath)
		err := runFfmpeg(12*time.Second, ffmpegVideoKeyframeArgs(filePath, thumbnailPath, at))
		if err == nil && thumbnailFileUsable(thumbnailPath) {
			return nil
		}
		if at == 1 && err != nil {
			return err
		}
	}
	if !thumbnailFileUsable(thumbnailPath) {
		return fmt.Errorf("ffmpeg produced empty thumbnail for %s", filePath)
	}
	return nil
}

// QueueJob queues a thumbnail generation job
func (p *ThumbnailWorkerPool) QueueJob(sourceFile, cacheKey string, isVideo bool, priority int, callback func(error)) {
	p.enqueueThumbnailJob(sourceFile, cacheKey, isVideo, false, priority, callback)
}

func (p *ThumbnailWorkerPool) QueueFolderJobAsync(folderPath, cacheKey string, priority int) {
	p.enqueueThumbnailJob(folderPath, cacheKey, false, true, priority, nil)
}

func (p *ThumbnailWorkerPool) enqueueThumbnailJob(sourceFile, cacheKey string, isVideo, isFolder bool, priority int, callback func(error)) {
	if cacheKey == "" {
		cacheKey = sourceFile
	}
	thumbnailPath := GetThumbnailPath(cacheKey)

	p.mu.Lock()
	if p.queuedJobs == nil {
		p.queuedJobs = make(map[string]bool)
	}
	if p.activeJobs[thumbnailPath] || p.queuedJobs[thumbnailPath] {
		p.mu.Unlock()
		return
	}
	if failedAt, ok := p.failedJobs[thumbnailPath]; ok && time.Since(failedAt) < failureCooldown {
		p.mu.Unlock()
		return
	}
	p.queuedJobs[thumbnailPath] = true
	p.mu.Unlock()

	job := ThumbnailJob{
		FilePath:      sourceFile,
		ThumbnailPath: thumbnailPath,
		IsVideo:       isVideo,
		IsFolder:      isFolder,
		Priority:      priority,
		Callback:      callback,
	}

	enqueued := false
	if priority >= PriorityHigh {
		select {
		case p.priorityQueue <- job:
			enqueued = true
		default:
			select {
			case p.jobQueue <- job:
				enqueued = true
			default:
			}
		}
	} else {
		select {
		case p.jobQueue <- job:
			enqueued = true
		default:
		}
	}

	if !enqueued {
		p.mu.Lock()
		delete(p.queuedJobs, thumbnailPath)
		p.mu.Unlock()
		logWarn("⚠️ Thumbnail queue full, dropping job for: %s", sourceFile)
		if callback != nil {
			callback(fmt.Errorf("queue full"))
		}
	}
}

// QueueJobAsync queues a job and returns immediately (fire and forget)
func (p *ThumbnailWorkerPool) QueueJobAsync(sourceFile, cacheKey string, isVideo bool, priority int) {
	p.QueueJob(sourceFile, cacheKey, isVideo, priority, nil)
}

// IsJobActive checks if a thumbnail job is currently being processed
func (p *ThumbnailWorkerPool) IsJobActive(cacheKey string) bool {
	return p.HasThumbnailJob(cacheKey)
}

// HasThumbnailJob reports whether a thumbnail is already queued or running.
func (p *ThumbnailWorkerPool) HasThumbnailJob(cacheKey string) bool {
	thumbnailPath := GetThumbnailPath(cacheKey)

	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.activeJobs[thumbnailPath] || p.queuedJobs[thumbnailPath]
}

// GetThumbnailPath returns the expected thumbnail path for a file
func GetThumbnailPath(filePath string) string {
	key := thumbnailCacheKey(filePath)
	hash := md5.Sum([]byte(key))
	hashStr := hex.EncodeToString(hash[:])
	return filepath.Join(THUMBNAIL_DIR, hashStr+".jpg")
}

// ThumbnailExists checks if thumbnail exists and is newer than source file
func ThumbnailExists(filePath string) (bool, bool) {
	thumbnailPath := GetThumbnailPath(filePath)

	thumbnailInfo, err := os.Stat(thumbnailPath)
	if err != nil {
		return false, false
	}

	sourceInfo, err := os.Stat(filePath)
	if err != nil {
		return false, false
	}

	// Thumbnail exists and is newer than source
	isValid := !sourceInfo.ModTime().After(thumbnailInfo.ModTime())
	return true, isValid
}

var imagePreviewExts = []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp"}
var videoPreviewExts = []string{".mp4", ".avi", ".mov", ".wmv", ".flv", ".webm", ".mkv", ".m4v", ".m2ts", ".mts", ".mpg", ".mpeg"}

func folderCollageLayout(n int) (cols, rows int) {
	switch {
	case n <= 1:
		return 1, 1
	case n == 2:
		return 2, 1
	case n == 3:
		return 3, 1
	case n == 4:
		return 2, 2
	default:
		return 3, 2
	}
}

func isSkippedPreviewName(name string) bool {
	lower := strings.ToLower(name)
	return name == "." || name == ".." || lower == "desktop.ini" || lower == "thumbs.db"
}

func appendLimited(dst, src []string, max int) []string {
	for _, item := range src {
		if len(dst) >= max {
			break
		}
		dst = append(dst, item)
	}
	return dst
}

func findFolderPreviewSources(dirPath string, max int) []string {
	return collectFolderPreviewSources(dirPath, max, 0)
}

func collectFolderPreviewSources(dirPath string, max, depth int) []string {
	if max <= 0 || depth > folderPreviewMaxDepth {
		return nil
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil
	}

	var images, videos, subdirs []string
	for _, entry := range entries {
		name := entry.Name()
		if isSkippedPreviewName(name) {
			continue
		}
		full := filepath.Join(dirPath, name)
		if entry.IsDir() {
			subdirs = append(subdirs, full)
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if contains(imagePreviewExts, ext) {
			images = append(images, full)
		} else if contains(videoPreviewExts, ext) {
			videos = append(videos, full)
		}
	}

	sources := make([]string, 0, max)
	sources = appendLimited(sources, images, max)
	sources = appendLimited(sources, videos, max)

	if len(sources) < max {
		for _, sub := range subdirs {
			nested := collectFolderPreviewSources(sub, 1, depth+1)
			sources = appendLimited(sources, nested, max)
			if len(sources) >= max {
				break
			}
		}
	}
	return sources
}

func (p *ThumbnailWorkerPool) loadPreviewImage(path string) (image.Image, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if contains(videoPreviewExts, ext) {
		thumb := GetThumbnailPath(path)
		if !thumbnailFileUsable(thumb) {
			if err := p.generateVideoThumbnail(path, thumb); err != nil {
				return nil, err
			}
		}
		path = thumb
	}
	return imaging.Open(path)
}

func (p *ThumbnailWorkerPool) generateFolderThumbnail(dirPath, thumbnailPath string) error {
	sources := findFolderPreviewSources(dirPath, folderCollageSlots)
	if len(sources) == 0 {
		return fmt.Errorf("no preview media in folder")
	}

	dir := filepath.Dir(thumbnailPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create thumbnail directory: %w", err)
	}

	width := thumbnailWidth
	height := thumbnailWidth * 9 / 16
	cols, rows := folderCollageLayout(len(sources))
	gap := 2
	cellW := (width - gap*(cols-1)) / cols
	cellH := (height - gap*(rows-1)) / rows

	dst := imaging.New(width, height, color.NRGBA{R: 24, G: 24, B: 28, A: 255})
	placed := 0
	for _, src := range sources {
		img, err := p.loadPreviewImage(src)
		if err != nil {
			continue
		}
		cell := imaging.Fill(img, cellW, cellH, imaging.Center, imaging.Lanczos)
		col := placed % cols
		row := placed / cols
		x := col * (cellW + gap)
		y := row * (cellH + gap)
		dst = imaging.Paste(dst, cell, image.Pt(x, y))
		placed++
		if placed >= cols*rows {
			break
		}
	}
	if placed == 0 {
		return fmt.Errorf("failed to decode any folder preview source")
	}

	opts := imaging.JPEGQuality(80)
	if err := imaging.Save(dst, thumbnailPath, opts); err != nil {
		return fmt.Errorf("failed to save folder thumbnail: %w", err)
	}
	return nil
}
