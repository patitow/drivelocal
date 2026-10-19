package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

// Health check with detailed status
func healthHandler(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	checks := make(map[string]interface{})

	// Check media path
	mediaPathExists := exists(MEDIA_PATH)
	checks["mediaPath"] = map[string]interface{}{
		"exists": mediaPathExists,
		"path":   MEDIA_PATH,
	}
	if !mediaPathExists {
		status = "degraded"
	}

	// Check thumbnail directory
	thumbnailDirExists := exists(THUMBNAIL_DIR)
	checks["thumbnailDir"] = map[string]interface{}{
		"exists": thumbnailDirExists,
		"path":   THUMBNAIL_DIR,
	}

	// Check users file
	usersFileExists := exists(USERS_FILE)
	checks["usersFile"] = map[string]interface{}{
		"exists": usersFileExists,
		"path":   USERS_FILE,
	}

	// Check thumbnail worker pool
	pool := GetThumbnailPool()
	checks["thumbnailPool"] = map[string]interface{}{
		"active": pool != nil,
		"status": "running",
	}

	// Check token storage
	tokensMutex.RLock()
	tokenCount := len(tokens)
	refreshTokenCount := len(refreshTokens)
	tokensMutex.RUnlock()

	checks["tokens"] = map[string]interface{}{
		"activeTokens":  tokenCount,
		"refreshTokens": refreshTokenCount,
	}

	checks["rateLimiting"] = map[string]interface{}{
		"enabled": true,
	}

	response := map[string]interface{}{
		"status":      status,
		"timestamp":   time.Now().Format(time.RFC3339),
		"checks":      checks,
		"authEnabled": os.Getenv("AUTH_ENABLED") != "false",
		"version":     "1.0.0",
		"bootId":      bootID,
	}

	// Set appropriate status code
	if status == "ok" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	jsonResponse(w, response)
}

// Login
func loginHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		RememberMe bool   `json:"rememberMe"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}

	// Validate username only (password validation not needed for login)
	req.Username = strings.TrimSpace(req.Username)
	if err := validateUsername(req.Username); err != nil {
		handleValidationError(w, err)
		return
	}

	// Don't validate password format on login - just check if it matches the hash
	// Password validation is only for creating/updating users

	logDebug("Login attempt for username: %s", req.Username)
	user := getUserByUsername(req.Username)
	if user == nil {
		logDebug("User not found: %s", req.Username)
		http.Error(w, `{"error":"Invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	// Check if password is empty (invalid user)
	if user.Password == "" {
		logWarn("User %s has empty password", req.Username)
		http.Error(w, `{"error":"Invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		logDebug("Password mismatch for user: %s", req.Username)
		http.Error(w, `{"error":"Invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	logInfo("Login successful for user: %s", req.Username)

	accessToken, refreshToken, expiresAt := createTokens(user.Username, user.ID, req.RememberMe)

	// Set httpOnly cookie for API requests
	// For local network (IP access), don't set SameSite to avoid cookie issues
	secure := cookieSecure(r)

	cookie := &http.Cookie{
		Name:     "accessToken",
		Value:    accessToken,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   15 * 60, // 15 minutes
		Path:     "/",
	}
	http.SetCookie(w, cookie)

	imgCookie := &http.Cookie{
		Name:     "imgToken",
		Value:    accessToken,
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   15 * 60,
		Path:     "/",
	}
	http.SetCookie(w, imgCookie)

	refreshMaxAge := 7 * 24 * 60 * 60 // 7 days
	if req.RememberMe {
		refreshMaxAge = 30 * 24 * 60 * 60 // 30 days
	}

	refreshCookie := &http.Cookie{
		Name:     "refreshToken",
		Value:    refreshToken,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   refreshMaxAge,
		Path:     "/",
	}
	http.SetCookie(w, refreshCookie)

	// Return token in response for IP access (when cookies might not work)
	// Always return token for local network access (when not using HTTPS)
	// This allows fallback to Authorization header when cookies don't work
	response := map[string]interface{}{
		"success":     true,
		"username":    user.Username,
		"email":       user.Email,
		"role":        user.Role,
		"permissions": user.Permissions,
		"expiresAt":   expiresAt,
		"message":     "Login successful",
	}
	// Include token in response when not using HTTPS (local network)
	// Check if request is HTTPS - if not, return token for IP access
	if r.TLS == nil {
		response["token"] = accessToken
		logDebug("Token returned in login response (HTTP, not HTTPS)")
	} else {
		logDebug("Token NOT returned (HTTPS request)")
	}
	jsonResponse(w, response)
}

// Logout
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie("accessToken")
	if cookie != nil {
		revokeToken(cookie.Value)
	}

	refreshCookie, _ := r.Cookie("refreshToken")
	if refreshCookie != nil {
		tokensMutex.Lock()
		delete(refreshTokens, hashToken(refreshCookie.Value))
		tokensMutex.Unlock()
		markSessionsDirty()
	}

	http.SetCookie(w, &http.Cookie{Name: "accessToken", MaxAge: -1, Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "refreshToken", MaxAge: -1, Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "imgToken", MaxAge: -1, Path: "/"})

	jsonResponse(w, map[string]interface{}{
		"success": true,
		"message": "Logged out successfully",
	})
}

// Auth status
func authStatusHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	user := getUserByID(userID)
	if user == nil {
		http.Error(w, `{"error":"User not found"}`, http.StatusNotFound)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"authenticated": true,
		"username":      user.Username,
		"email":         user.Email,
		"role":          user.Role,
		"permissions":   user.Permissions,
		"authEnabled":   true,
	})
}

// Refresh token
func refreshHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("refreshToken")
	if err != nil {
		http.Error(w, `{"error":"Refresh token required"}`, http.StatusUnauthorized)
		return
	}

	newToken, expires, ok := refreshAccessToken(cookie.Value)
	if !ok {
		http.SetCookie(w, &http.Cookie{Name: "accessToken", MaxAge: -1, Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "refreshToken", MaxAge: -1, Path: "/"})
		http.Error(w, `{"error":"Invalid or expired refresh token"}`, http.StatusUnauthorized)
		return
	}

	secure := cookieSecure(r)

	cookie = &http.Cookie{
		Name:     "accessToken",
		Value:    newToken,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   15 * 60,
		Path:     "/",
	}
	http.SetCookie(w, cookie)

	imgCookie := &http.Cookie{
		Name:     "imgToken",
		Value:    newToken,
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   15 * 60,
		Path:     "/",
	}
	http.SetCookie(w, imgCookie)

	jsonResponse(w, map[string]interface{}{
		"success":   true,
		"expiresAt": expires,
	})
}

// Files listing
func filesHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Set timeout for directory reading (30 seconds)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	pathParam := decodeMediaPath(r.URL.Query().Get("path"))

	// Validate path parameter
	if err := validatePath(pathParam); err != nil {
		handleValidationError(w, err)
		return
	}

	// Parse pagination parameters
	limit := 100 // Default limit
	offset := 0  // Default offset

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 && parsedLimit <= 1000 {
			limit = parsedLimit
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if parsedOffset, err := strconv.Atoi(offsetStr); err == nil && parsedOffset >= 0 {
			offset = parsedOffset
		}
	}

	// Normalize path separators for Windows
	pathParam = strings.ReplaceAll(pathParam, "/", string(filepath.Separator))

	fullPath := filepath.Join(MEDIA_PATH, pathParam)

	// Clean the path to resolve any .. or . components
	fullPath = filepath.Clean(fullPath)
	mediaPathClean := filepath.Clean(MEDIA_PATH)

	// Validate path (must be within media directory)
	if !strings.HasPrefix(fullPath, mediaPathClean) {
		logWarn("Path validation failed: %s not in %s", fullPath, mediaPathClean)
		http.Error(w, `{"error":"Access denied"}`, http.StatusForbidden)
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		logError("Directory stat error: %v for path: %s", err, fullPath)
		http.Error(w, `{"error":"Directory not found"}`, http.StatusNotFound)
		return
	}

	if !info.IsDir() {
		http.Error(w, `{"error":"Path is not a directory"}`, http.StatusBadRequest)
		return
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		logWarn("Request cancelled: %v", ctx.Err())
		http.Error(w, `{"error":"Request timeout"}`, http.StatusRequestTimeout)
		return
	default:
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		logError("ReadDir error: %v for path: %s", err, fullPath)
		http.Error(w, `{"error":"Failed to read directory"}`, http.StatusInternalServerError)
		return
	}

	// Apply pagination
	totalEntries := len(entries)
	start := offset
	end := offset + limit
	if start > totalEntries {
		start = totalEntries
	}
	if end > totalEntries {
		end = totalEntries
	}

	items := []map[string]interface{}{}
	for i := start; i < end; i++ {
		entry := entries[i]
		// Filter out system files like desktop.ini, Thumbs.db, etc.
		entryName := entry.Name()
		hiddenFiles := []string{"desktop.ini", "thumbs.db", ".ds_store", "._.ds_store"}
		shouldSkip := false
		for _, hidden := range hiddenFiles {
			if strings.EqualFold(entryName, hidden) {
				shouldSkip = true
				break
			}
		}
		if shouldSkip {
			continue
		}

		itemPath := filepath.Join(fullPath, entry.Name())
		relPath, _ := filepath.Rel(MEDIA_PATH, itemPath)
		info, _ := entry.Info()
		isDir := resolvedIsDir(itemPath, entry)

		ext := strings.ToLower(filepath.Ext(entry.Name()))
		isVideo := contains([]string{".mp4", ".avi", ".mov", ".wmv", ".flv", ".webm", ".mkv", ".m4v", ".m2ts", ".mts", ".mpg", ".mpeg"}, ext)
		isImage := contains([]string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg", ".tiff"}, ext)

		itemData := map[string]interface{}{
			"name":        entry.Name(),
			"path":        relPath,
			"isDirectory": isDir,
			"isVideo":     isVideo,
			"isImage":     isImage,
			"size":        info.Size(),
			"createdAt":   info.ModTime().Format(time.RFC3339),
			"modifiedAt":  info.ModTime().Format(time.RFC3339),
		}

		// Count files and folders in directory if it's a directory
		if isDir {
			dirEntries, err := os.ReadDir(itemPath)
			if err == nil {
				fileCount := 0
				folderCount := 0
				for _, dirEntry := range dirEntries {
					// Skip system files when counting
					dirEntryName := dirEntry.Name()
					shouldSkip := false
					hiddenFiles := []string{"desktop.ini", "thumbs.db", ".ds_store", "._.ds_store"}
					for _, hidden := range hiddenFiles {
						if strings.EqualFold(dirEntryName, hidden) {
							shouldSkip = true
							break
						}
					}
					if shouldSkip {
						continue
					}

					if resolvedIsDir(filepath.Join(itemPath, dirEntry.Name()), dirEntry) {
						folderCount++
					} else {
						fileCount++
					}
				}
				itemData["fileCount"] = fileCount
				itemData["folderCount"] = folderCount
			} else {
				itemData["fileCount"] = 0
				itemData["folderCount"] = 0
			}
		}

		items = append(items, itemData)
	}

	jsonResponse(w, map[string]interface{}{
		"currentPath": pathParam,
		"items":       items,
		"pagination": map[string]interface{}{
			"total":   totalEntries,
			"limit":   limit,
			"offset":  offset,
			"hasMore": end < totalEntries,
		},
	})
}

// Download file
func downloadHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Set timeout for file operations (5 minutes)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	vars := mux.Vars(r)
	filePath := decodeMediaPath(vars["path"])

	// Normalize path separators for Windows
	filePath = strings.ReplaceAll(filePath, "/", string(filepath.Separator))

	fullPath := filepath.Join(MEDIA_PATH, filePath)

	// Clean the path to resolve any .. or . components
	fullPath = filepath.Clean(fullPath)
	mediaPathClean := filepath.Clean(MEDIA_PATH)

	// Validate path (must be within media directory)
	if !strings.HasPrefix(fullPath, mediaPathClean) {
		logWarn("Path validation failed: %s not in %s", fullPath, mediaPathClean)
		http.Error(w, `{"error":"Access denied"}`, http.StatusForbidden)
		return
	}

	if !exists(fullPath) {
		logWarn("File not found: %s", fullPath)
		http.Error(w, `{"error":"File not found"}`, http.StatusNotFound)
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		logError("File stat error: %v for path: %s", err, fullPath)
		http.Error(w, `{"error":"File not found"}`, http.StatusNotFound)
		return
	}

	if info.IsDir() {
		http.Error(w, `{"error":"Cannot download directory"}`, http.StatusBadRequest)
		return
	}

	file, err := os.Open(fullPath)
	if err != nil {
		logError("Failed to open file: %v for path: %s", err, fullPath)
		http.Error(w, `{"error":"Failed to open file"}`, http.StatusInternalServerError)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", getMimeType(fullPath))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(fullPath)))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// Stream file directly - io.Copy handles client disconnection automatically
	written, err := io.Copy(w, file)
	if err != nil {
		// Check if error is due to client disconnection
		if ctx.Err() != nil {
			logDebug("Client disconnected during download: %v", ctx.Err())
		} else {
			logError("Error downloading file: %v (written: %d/%d bytes)", err, written, info.Size())
		}
		return
	}
	logDebug("Downloaded file: %d/%d bytes", written, info.Size())
}

// Stream file (with range support)
func streamHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Set timeout for streaming (1 hour for large files)
	ctx, cancel := context.WithTimeout(ctx, 1*time.Hour)
	defer cancel()

	vars := mux.Vars(r)
	filePath := decodeMediaPath(vars["path"])

	// Normalize path separators for Windows
	filePath = strings.ReplaceAll(filePath, "/", string(filepath.Separator))

	fullPath := filepath.Join(MEDIA_PATH, filePath)

	// Clean the path to resolve any .. or . components
	fullPath = filepath.Clean(fullPath)
	mediaPathClean := filepath.Clean(MEDIA_PATH)

	// Validate path (must be within media directory)
	if !strings.HasPrefix(fullPath, mediaPathClean) {
		logWarn("Path validation failed: %s not in %s", fullPath, mediaPathClean)
		http.Error(w, `{"error":"Access denied"}`, http.StatusForbidden)
		return
	}

	if !exists(fullPath) {
		http.Error(w, `{"error":"File not found"}`, http.StatusNotFound)
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		logError("Failed to stat file: %v", err)
		http.Error(w, `{"error":"File not found"}`, http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, `{"error":"Cannot stream directory"}`, http.StatusBadRequest)
		return
	}

	if needsRemux(r) {
		streamProcessed(w, r, fullPath, info)
		return
	}

	file, err := os.Open(fullPath)
	if err != nil {
		logError("Failed to open file: %v", err)
		http.Error(w, `{"error":"Failed to open file"}`, http.StatusInternalServerError)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", getMimeType(fullPath))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	// ServeContent trata Range (inclusive "bytes=-N" e múltiplos), If-Range, HEAD e 416
	http.ServeContent(w, r.WithContext(ctx), "", info.ModTime(), file)
}

// Download multiple files as ZIP (streaming to avoid memory issues)
func downloadMultipleHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		Files []string `json:"files"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logError("Failed to decode download request: %v", err)
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}

	if len(req.Files) == 0 {
		http.Error(w, `{"error":"No files specified"}`, http.StatusBadRequest)
		return
	}

	// Limit number of files to prevent abuse
	if len(req.Files) > 100 {
		http.Error(w, `{"error":"Too many files (maximum 100)"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="media-files.zip"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// Use streaming ZIP writer
	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()

	mediaPathClean := filepath.Clean(MEDIA_PATH)
	addedCount := 0

	for _, filePath := range req.Files {
		// Check context cancellation
		select {
		case <-ctx.Done():
			logWarn("Download cancelled: %v", ctx.Err())
			return
		default:
		}

		// Validate path
		if err := validatePath(filePath); err != nil {
			logWarn("Invalid path in download request: %s", filePath)
			continue
		}

		fullPath := filepath.Join(MEDIA_PATH, filePath)
		fullPath = filepath.Clean(fullPath)

		// Validate path is within media directory
		if !strings.HasPrefix(fullPath, mediaPathClean) {
			logWarn("Path validation failed for download: %s", fullPath)
			continue
		}

		if !exists(fullPath) {
			logWarn("File not found for download: %s", fullPath)
			continue
		}

		info, err := os.Stat(fullPath)
		if err != nil {
			logError("Failed to stat file: %v", err)
			continue
		}

		if info.IsDir() {
			logWarn("Skipping directory in download: %s", fullPath)
			continue
		}

		file, err := os.Open(fullPath)
		if err != nil {
			logError("Failed to open file: %v", err)
			continue
		}

		// Create entry in ZIP
		zipFile, err := zipWriter.Create(filepath.Base(filePath))
		if err != nil {
			file.Close()
			logError("Failed to create ZIP entry: %v", err)
			continue
		}

		// Stream file to ZIP (chunked to avoid loading entire file in memory)
		const chunkSize = 32 * 1024 // 32KB chunks
		buffer := make([]byte, chunkSize)

		for {
			select {
			case <-ctx.Done():
				file.Close()
				logWarn("Download cancelled during file copy")
				return
			default:
			}

			n, err := file.Read(buffer)
			if n > 0 {
				if _, writeErr := zipFile.Write(buffer[:n]); writeErr != nil {
					file.Close()
					logError("Failed to write to ZIP: %v", writeErr)
					return
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				file.Close()
				logError("Failed to read file: %v", err)
				return
			}
		}

		file.Close()
		addedCount++
	}

	logInfo("Download ZIP created with %d files", addedCount)
}

// Thumbnail
func thumbnailHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	filePath := decodeMediaPath(vars["path"])

	// Normalize path separators for Windows (handle both / and \)
	filePath = strings.ReplaceAll(filePath, "/", string(filepath.Separator))
	filePath = strings.ReplaceAll(filePath, "\\", string(filepath.Separator))

	// Build full path - handle empty path (file in root) and relative paths
	var fullPath string
	if filePath == "" || filePath == "." {
		// This shouldn't happen for thumbnails, but handle it
		logWarn("Thumbnail requested with empty path")
		http.Error(w, `{"error":"Invalid path"}`, http.StatusBadRequest)
		return
	} else {
		fullPath = filepath.Join(MEDIA_PATH, filePath)
	}

	// Clean the path to resolve any .. or . components
	fullPath = filepath.Clean(fullPath)
	mediaPathClean := filepath.Clean(MEDIA_PATH)

	// Validate path (must be within media directory)
	if !strings.HasPrefix(fullPath, mediaPathClean) {
		logWarn("Path validation failed: %s not in %s (requested: %s)", fullPath, mediaPathClean, vars["path"])
		http.Error(w, `{"error":"Access denied"}`, http.StatusForbidden)
		return
	}

	if !exists(fullPath) {
		logWarn("Thumbnail file not found: %s (requested: %s, decoded: %s, MEDIA_PATH: %s)", fullPath, vars["path"], filePath, MEDIA_PATH)
		http.Error(w, `{"error":"File not found"}`, http.StatusNotFound)
		return
	}

	// Check if it's a directory (shouldn't happen, but handle it)
	info, err := os.Stat(fullPath)
	if err != nil {
		logError("Failed to stat thumbnail file: %v for path: %s", err, fullPath)
		http.Error(w, `{"error":"File not found"}`, http.StatusNotFound)
		return
	}

	// Handle directories as a collage of the first media inside (and one per subfolder).
	if info.IsDir() {
		thumbnailPath := GetThumbnailPath(fullPath)
		exists, isValid := ThumbnailExists(fullPath)
		if exists && isValid && serveCachedThumbnail(w, r, thumbnailPath) {
			return
		}

		pool := GetThumbnailPool()
		if !pool.HasThumbnailJob(fullPath) {
			pool.QueueFolderJobAsync(fullPath, fullPath, PriorityNormal)
		}

		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Retry-After", "1")
		http.Error(w, `{"error":"Thumbnail generation in progress"}`, http.StatusAccepted)
		return
	}

	originalPath := fullPath
	previewFile := ""

	// Determine file type
	ext := strings.ToLower(filepath.Ext(fullPath))
	isVideo := contains([]string{".mp4", ".avi", ".mov", ".wmv", ".flv", ".webm", ".mkv", ".m4v", ".m2ts", ".mts", ".mpg", ".mpeg"}, ext)
	isImage := contains([]string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg", ".tiff"}, ext)

	// Log for debugging
	if previewFile != "" {
		logDebug("Thumbnail request: folder=%s, previewFile=%s, isImage=%v, isVideo=%v", originalPath, fullPath, isImage, isVideo)
	} else {
		logDebug("Thumbnail request: file=%s, isImage=%v, isVideo=%v", fullPath, isImage, isVideo)
	}

	w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
	w.Header().Set("Access-Control-Allow-Credentials", "true")

	cacheKey := fullPath
	if previewFile != "" {
		cacheKey = originalPath
	}

	if isImage {
		isSVG := ext == ".svg"
		if isSVG {
			file, err := os.Open(fullPath)
			if err != nil {
				http.Error(w, `{"error":"Failed to open file"}`, http.StatusInternalServerError)
				return
			}
			defer file.Close()
			mimeType := getMimeType(fullPath)
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.Header().Set("Content-Type", mimeType)
			w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
			io.Copy(w, file)
			return
		}

		thumbnailPath := GetThumbnailPath(cacheKey)
		exists, isValid := ThumbnailExists(cacheKey)
		if exists && isValid && serveCachedThumbnail(w, r, thumbnailPath) {
			return
		}

		pool := GetThumbnailPool()
		if !pool.HasThumbnailJob(cacheKey) {
			pool.QueueJobAsync(fullPath, cacheKey, false, PriorityNormal)
		}

		file, err := os.Open(fullPath)
		if err != nil {
			http.Error(w, `{"error":"Failed to open file"}`, http.StatusInternalServerError)
			return
		}
		defer file.Close()
		mimeType := getMimeType(fullPath)
		etag := fmt.Sprintf(`"%x-%x"`, info.ModTime().Unix(), info.Size())
		// This is the full-size original, served only until the real thumbnail
		// finishes encoding. Must revalidate on every load (not max-age cache
		// the original bytes) so that once the small thumbnail exists, the next
		// request actually reaches serveCachedThumbnail's own ETag above instead
		// of the browser silently reusing the full-size copy for a day.
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", mimeType)
		if match := r.Header.Get("If-None-Match"); match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		io.Copy(w, file)
		return
	}

	if isVideo {
		thumbnailPath := GetThumbnailPath(cacheKey)
		exists, isValid := ThumbnailExists(cacheKey)
		if exists && isValid && serveCachedThumbnail(w, r, thumbnailPath) {
			return
		}

		pool := GetThumbnailPool()
		if !pool.HasThumbnailJob(cacheKey) {
			pool.QueueJobAsync(fullPath, cacheKey, true, PriorityNormal)
		}

		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Retry-After", "1")
		http.Error(w, `{"error":"Thumbnail generation in progress"}`, http.StatusAccepted)
		return
	}

	http.Error(w, `{"error":"Thumbnail not supported for this file type"}`, http.StatusBadRequest)
}

const maxThumbnailBatchPaths = 80

func thumbnailsBatchHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}
	if len(req.Paths) > maxThumbnailBatchPaths {
		req.Paths = req.Paths[:maxThumbnailBatchPaths]
	}

	seen := make(map[string]bool)
	items := make([]map[string]string, 0, len(req.Paths))
	for _, raw := range req.Paths {
		rel := normalizeRelativeMediaPath(raw)
		if rel == "" || seen[rel] {
			continue
		}
		seen[rel] = true
		items = append(items, map[string]string{
			"path":   rel,
			"status": thumbnailBatchStatus(rel),
		})
	}
	jsonResponse(w, map[string]interface{}{"items": items})
}

func normalizeRelativeMediaPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "/")
	if p == "." {
		return ""
	}
	return p
}

func thumbnailBatchStatus(rel string) string {
	fullPath, ok := resolveMediaPath(rel)
	if !ok {
		return "missing"
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		return "missing"
	}

	existsThumb, valid := ThumbnailExists(fullPath)
	if existsThumb && valid {
		return "ready"
	}

	ext := strings.ToLower(filepath.Ext(fullPath))
	isVideo := contains([]string{".mp4", ".avi", ".mov", ".wmv", ".flv", ".webm", ".mkv", ".m4v", ".m2ts", ".mts", ".mpg", ".mpeg"}, ext)
	isImage := contains([]string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg", ".tiff"}, ext)
	pool := GetThumbnailPool()

	if info.IsDir() {
		if !pool.HasThumbnailJob(fullPath) {
			pool.QueueFolderJobAsync(fullPath, fullPath, PriorityLow)
		}
		return "pending"
	}
	if ext == ".svg" {
		return "ready"
	}
	if isImage {
		if !pool.HasThumbnailJob(fullPath) {
			pool.QueueJobAsync(fullPath, fullPath, false, PriorityLow)
		}
		// GET already streams the original while the JPEG encodes, so the
		// browser can paint immediately instead of waiting on the worker pool.
		return "ready"
	}
	if isVideo {
		if !pool.HasThumbnailJob(fullPath) {
			pool.QueueJobAsync(fullPath, fullPath, true, PriorityLow)
		}
		return "pending"
	}
	return "missing"
}

func resolveMediaPath(rel string) (string, bool) {
	if rel == "" {
		return "", false
	}
	fullPath := filepath.Clean(filepath.Join(MEDIA_PATH, filepath.FromSlash(rel)))
	mediaPathClean := filepath.Clean(MEDIA_PATH)
	if !strings.HasPrefix(fullPath, mediaPathClean) {
		return "", false
	}
	if !exists(fullPath) {
		return "", false
	}
	return fullPath, true
}

func serveCachedThumbnail(w http.ResponseWriter, r *http.Request, thumbnailPath string) bool {
	thumbnailFile, err := os.Open(thumbnailPath)
	if err != nil {
		return false
	}
	defer thumbnailFile.Close()

	thumbnailInfo, err := thumbnailFile.Stat()
	if err != nil {
		return false
	}

	etag := fmt.Sprintf(`"%x-%x"`, thumbnailInfo.ModTime().Unix(), thumbnailInfo.Size())
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", thumbnailInfo.ModTime().UTC().Format(http.TimeFormat))

	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	if modifiedSince := r.Header.Get("If-Modified-Since"); modifiedSince != "" {
		if t, err := http.ParseTime(modifiedSince); err == nil && thumbnailInfo.ModTime().Before(t.Add(time.Second)) {
			w.WriteHeader(http.StatusNotModified)
			return true
		}
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.FormatInt(thumbnailInfo.Size(), 10))
	io.Copy(w, thumbnailFile)
	return true
}

// Network info
func networkInfoHandler(w http.ResponseWriter, r *http.Request) {
	port := advertisedPort()
	addresses := []map[string]interface{}{}
	for _, ip := range advertisedIPv4Addresses() {
		addresses = append(addresses, map[string]interface{}{
			"address": ip,
			"port":    port,
		})
	}
	jsonResponse(w, map[string]interface{}{
		"addresses": addresses,
		"port":      port,
	})
}

// User management handlers
func listUsersHandler(w http.ResponseWriter, r *http.Request) {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	userList := []map[string]interface{}{}
	for _, user := range users {
		userList = append(userList, map[string]interface{}{
			"id":          user.ID,
			"username":    user.Username,
			"email":       user.Email,
			"role":        user.Role,
			"permissions": user.Permissions,
			"createdAt":   user.CreatedAt,
			"active":      user.Active,
		})
	}

	jsonResponse(w, map[string]interface{}{"users": userList})
}

func createUserHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string   `json:"username"`
		Password    string   `json:"password"`
		Email       string   `json:"email"`
		Role        string   `json:"role"`
		Permissions []string `json:"permissions"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}

	// Validate inputs
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)

	if err := validateUsername(req.Username); err != nil {
		handleValidationError(w, err)
		return
	}
	if err := validatePassword(req.Password); err != nil {
		handleValidationError(w, err)
		return
	}
	if err := validateEmail(req.Email); err != nil {
		handleValidationError(w, err)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
	if err != nil {
		logError("Failed to hash password: %v", err)
		http.Error(w, `{"error":"Failed to create user"}`, http.StatusInternalServerError)
		return
	}
	newUser := User{
		ID:          generateID(),
		Username:    req.Username,
		Password:    string(hash),
		Email:       req.Email,
		Role:        req.Role,
		Permissions: req.Permissions,
		CreatedAt:   time.Now().Format(time.RFC3339),
		Active:      true,
	}

	usersMutex.Lock()
	users = append(users, newUser)
	if err := saveUsers(); err != nil {
		usersMutex.Unlock()
		logError("Failed to save user: %v", err)
		http.Error(w, `{"error":"Failed to create user"}`, http.StatusInternalServerError)
		return
	}
	usersMutex.Unlock()

	jsonResponse(w, map[string]interface{}{
		"success": true,
		"user": map[string]interface{}{
			"id":          newUser.ID,
			"username":    newUser.Username,
			"email":       newUser.Email,
			"role":        newUser.Role,
			"permissions": newUser.Permissions,
		},
	})
}

func updateUserHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}

	usersMutex.Lock()
	for i := range users {
		if users[i].ID == id {
			if email, ok := req["email"].(string); ok {
				email = strings.TrimSpace(email)
				if err := validateEmail(email); err != nil {
					usersMutex.Unlock()
					handleValidationError(w, err)
					return
				}
				users[i].Email = email
			}
			if role, ok := req["role"].(string); ok {
				// Validate role
				validRoles := []string{"admin", "user", "viewer"}
				isValid := false
				for _, validRole := range validRoles {
					if role == validRole {
						isValid = true
						break
					}
				}
				if !isValid {
					usersMutex.Unlock()
					http.Error(w, `{"error":"Invalid role"}`, http.StatusBadRequest)
					return
				}
				users[i].Role = role
			}
			if perms, ok := req["permissions"].([]interface{}); ok {
				users[i].Permissions = []string{}
				for _, p := range perms {
					users[i].Permissions = append(users[i].Permissions, p.(string))
				}
			}
			deactivated := false
			if active, ok := req["active"].(bool); ok {
				users[i].Active = active
				deactivated = !active
			}
			if err := saveUsers(); err != nil {
				usersMutex.Unlock()
				logError("Failed to save user: %v", err)
				http.Error(w, `{"error":"Failed to update user"}`, http.StatusInternalServerError)
				return
			}
			usersMutex.Unlock()
			if deactivated {
				revokeUserTokens(id)
			}

			jsonResponse(w, map[string]interface{}{
				"success": true,
				"user": map[string]interface{}{
					"id":          users[i].ID,
					"username":    users[i].Username,
					"email":       users[i].Email,
					"role":        users[i].Role,
					"permissions": users[i].Permissions,
				},
			})
			return
		}
	}
	usersMutex.Unlock()

	http.Error(w, `{"error":"User not found"}`, http.StatusNotFound)
}

func deleteUserHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if id == "" {
		http.Error(w, `{"error":"User ID required"}`, http.StatusBadRequest)
		return
	}

	usersMutex.Lock()
	found := false
	for i := range users {
		if users[i].ID == id {
			users[i].Active = false
			if err := saveUsers(); err != nil {
				usersMutex.Unlock()
				logError("Failed to save users after delete: %v", err)
				http.Error(w, `{"error":"Failed to delete user"}`, http.StatusInternalServerError)
				return
			}
			found = true
			break
		}
	}
	usersMutex.Unlock()

	if !found {
		http.Error(w, `{"error":"User not found"}`, http.StatusNotFound)
		return
	}
	revokeUserTokens(id)

	jsonResponse(w, map[string]interface{}{
		"success": true,
		"message": "User deleted",
	})
}

// Forgot/Reset password
func forgotPasswordHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	user := getUserByEmail(req.Email)
	if user == nil {
		// Don't reveal if user exists
		jsonResponse(w, map[string]interface{}{
			"success": true,
			"message": "If the email exists, a reset link has been sent",
		})
		return
	}

	// Generate reset token (simplified)
	token := generateToken(32)
	expiry := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	usersMutex.Lock()
	for i := range users {
		if users[i].ID == user.ID {
			users[i].ResetToken = &token
			expiryStr := expiry
			users[i].ResetTokenExpiry = &expiryStr
			saveUsers()
			break
		}
	}
	usersMutex.Unlock()

	// In production, send email here
	logInfo("Password reset token generated for %s", req.Email)
	logDebug("Password reset token: %s", token)

	jsonResponse(w, map[string]interface{}{
		"success": true,
		"message": "If the email exists, a reset link has been sent",
	})
}

func resetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email       string `json:"email"`
		Token       string `json:"token"`
		NewPassword string `json:"newPassword"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	user := getUserByEmail(req.Email)
	if user == nil || user.ResetToken == nil || *user.ResetToken != req.Token {
		http.Error(w, `{"error":"Invalid or expired reset token"}`, http.StatusBadRequest)
		return
	}

	if time.Now().After(parseTime(*user.ResetTokenExpiry)) {
		http.Error(w, `{"error":"Invalid or expired reset token"}`, http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 10)
	if err != nil {
		logError("Failed to hash password: %v", err)
		http.Error(w, `{"error":"Failed to reset password"}`, http.StatusInternalServerError)
		return
	}

	saved := false
	usersMutex.Lock()
	for i := range users {
		if users[i].ID == user.ID {
			users[i].Password = string(hash)
			users[i].ResetToken = nil
			users[i].ResetTokenExpiry = nil
			if err := saveUsers(); err != nil {
				usersMutex.Unlock()
				logError("Failed to save password reset: %v", err)
				http.Error(w, `{"error":"Failed to reset password"}`, http.StatusInternalServerError)
				return
			}
			saved = true
			break
		}
	}
	usersMutex.Unlock()

	// Senha trocada: derruba as sessões abertas com a senha antiga.
	if saved {
		revokeUserTokens(user.ID)
	}

	jsonResponse(w, map[string]interface{}{
		"success": true,
		"message": "Password reset successfully",
	})
}

// Helper functions
func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// resolvedIsDir reports whether an entry is a directory, following reparse
// points (Windows junctions/symlinks) that os.DirEntry.IsDir() does not:
// ReadDir uses lstat semantics, so a junction pointing at a folder otherwise
// comes back as a non-directory "file". Go reports Windows junctions as
// os.ModeIrregular (not os.ModeSymlink), so fall back to a real Stat for
// anything that's neither a plain directory nor a plain file.
func resolvedIsDir(itemPath string, entry os.DirEntry) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type().IsRegular() {
		return false
	}
	if info, err := os.Stat(itemPath); err == nil {
		return info.IsDir()
	}
	return false
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func getMimeType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	mimeTypes := map[string]string{
		".mp4":  "video/mp4",
		".avi":  "video/x-msvideo",
		".mov":  "video/quicktime",
		".webm": "video/webm",
		".mkv":  "video/x-matroska",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".gif":  "image/gif",
		".webp": "image/webp",
	}
	if mime, ok := mimeTypes[ext]; ok {
		return mime
	}
	return "application/octet-stream"
}

func getUserByEmail(email string) *User {
	usersMutex.RLock()
	defer usersMutex.RUnlock()
	for i := range users {
		if users[i].Email == email && users[i].Active {
			return &users[i]
		}
	}
	return nil
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// findFolderPreview finds the first image or video file in a directory for folder preview
func findFolderPreview(dirPath string) string {
	sources := findFolderPreviewSources(dirPath, 1)
	if len(sources) == 0 {
		return ""
	}
	return sources[0]
}
