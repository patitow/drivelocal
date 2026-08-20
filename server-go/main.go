package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
)

// Config
var (
	PORT          = getEnv("PORT", "80")
	MEDIA_PATH    = getEnv("MEDIA_PATH", `./media`)
	THUMBNAIL_DIR = "server/thumbnails"
	USERS_FILE    = "server/data/users.json"
)

// User model
type User struct {
	ID               string   `json:"id"`
	Username         string   `json:"username"`
	Password         string   `json:"password"`
	Email            string   `json:"email"`
	Role             string   `json:"role"`
	Permissions      []string `json:"permissions"`
	CreatedAt        string   `json:"createdAt"`
	Active           bool     `json:"active"`
	ResetToken       *string  `json:"resetToken"`
	ResetTokenExpiry *string  `json:"resetTokenExpiry"`
}

// Token storage
type TokenData struct {
	Username     string
	UserID       string
	ExpiresAt    int64
	RefreshToken string
}

var (
	tokens        = make(map[string]*TokenData)
	refreshTokens = make(map[string]*TokenData)
	tokensMutex   sync.RWMutex
	users         []User
	usersMutex    sync.RWMutex
)

// Rate limiting
type RateLimit struct {
	Count     int
	ResetTime int64
}

var (
	rateLimitMap     = make(map[string]*RateLimit)
	authRateLimitMap = make(map[string]*RateLimit)
	rateLimitMutex   sync.RWMutex
)

const (
	ACCESS_TOKEN_EXPIRY  = 15 * 60 * 1000           // 15 minutes
	REFRESH_TOKEN_EXPIRY = 7 * 24 * 60 * 60 * 1000  // 7 days
	REMEMBER_ME_EXPIRY   = 30 * 24 * 60 * 60 * 1000 // 30 days
	RATE_LIMIT_WINDOW    = 60000                    // 1 minute
	RATE_LIMIT_MAX       = 1000
)

func main() {
	// Load .env
	godotenv.Load()

	// Initialize logger first
	initLogger()

	// Initialize
	ensureDirectories()
	loadUsers()

	// Initialize thumbnail worker pool
	GetThumbnailPool()

	// Start token cleanup goroutine
	go startTokenCleanup()

	// Router
	r := mux.NewRouter()

	// CORS middleware
	r.Use(corsMiddleware)
	r.Use(loggingMiddleware)

	// Compression middleware (should be early in chain)
	r.Use(compressionMiddleware)

	// Priority middleware (must be before rate limiting)
	r.Use(priorityMiddleware)

	// Rate limiting (improved sliding window)
	r.Use(improvedRateLimitMiddleware)

	// Public routes
	r.HandleFunc("/api/health", healthHandler).Methods("GET", "HEAD")
	r.HandleFunc("/api/auth/login", loginHandler).Methods("POST")
	r.HandleFunc("/api/auth/refresh", refreshHandler).Methods("POST")
	r.HandleFunc("/api/auth/forgot-password", forgotPasswordHandler).Methods("POST")
	r.HandleFunc("/api/auth/reset-password", resetPasswordHandler).Methods("POST")

	// Protected routes
	protected := r.PathPrefix("/api").Subrouter()
	protected.Use(authMiddleware)
	protected.HandleFunc("/auth/status", authStatusHandler).Methods("GET")
	protected.HandleFunc("/auth/logout", logoutHandler).Methods("POST")
	protected.HandleFunc("/files", filesHandler).Methods("GET")
	protected.HandleFunc("/download/{path:.*}", downloadHandler).Methods("GET")
	protected.HandleFunc("/download-multiple", downloadMultipleHandler).Methods("POST")
	protected.HandleFunc("/stream/{path:.*}", streamHandler).Methods("GET")
	protected.HandleFunc("/thumbnails", thumbnailsBatchHandler).Methods("POST")
	protected.HandleFunc("/thumbnail/{path:.*}", thumbnailHandler).Methods("GET")
	protected.HandleFunc("/network-info", networkInfoHandler).Methods("GET")

	// Admin routes
	admin := protected.PathPrefix("/users").Subrouter()
	admin.Use(requireRole("admin"))
	admin.HandleFunc("", listUsersHandler).Methods("GET")
	admin.HandleFunc("", createUserHandler).Methods("POST")
	admin.HandleFunc("/{id}", updateUserHandler).Methods("PUT")
	admin.HandleFunc("/{id}", deleteUserHandler).Methods("DELETE")

	// Static files (serve React build)
	// Only serve static files for non-API routes
	r.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't serve static files for API routes
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		// Try to serve file from dist directory
		// In Docker, dist is at ./dist relative to the binary
		distPath := filepath.Join("dist", r.URL.Path)
		if info, err := os.Stat(distPath); err == nil && !info.IsDir() {
			http.ServeFile(w, r, distPath)
			return
		}

		// SPA fallback - serve index.html for all non-API routes
		indexPath := filepath.Join("dist", "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			http.ServeFile(w, r, indexPath)
			return
		}

		http.NotFound(w, r)
	}))

	// Start server
	addr := "0.0.0.0:" + PORT
	logInfo("🚀 DriveLocal Server starting on %s", addr)
	logInfo("📁 Media directory: %s", MEDIA_PATH)
	if ips := advertisedIPv4Addresses(); len(ips) > 0 {
		for _, ip := range ips {
			if PORT == "80" {
				logInfo("🌐 LAN: http://%s/", ip)
			} else {
				logInfo("🌐 LAN: http://%s:%s/", ip, PORT)
			}
		}
	} else {
		logInfo("🌐 LAN IP not detected inside this process; set HOST_LAN_IP")
	}
	logInfo("💻 Loopback: http://127.0.0.1:8080/ if wslrelay is holding 127.0.0.1:80")

	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal("Server failed:", err)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func ensureDirectories() {
	os.MkdirAll(THUMBNAIL_DIR, 0755)
	os.MkdirAll(filepath.Dir(USERS_FILE), 0755)
}
