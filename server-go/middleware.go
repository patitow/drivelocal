package main

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Request priority tracking
var (
	activeStreamingRequests = make(map[string]int64) // IP -> timestamp
	streamingMutex          sync.RWMutex
)

// getAllowedOrigins returns list of allowed CORS origins
func getAllowedOrigins() []string {
	// Get from environment variable (comma-separated)
	originsStr := os.Getenv("CORS_ORIGINS")
	if originsStr == "" {
		// Default: allow same origin and localhost (for development)
		return []string{"http://localhost", "http://localhost:80", "http://127.0.0.1", "http://127.0.0.1:80"}
	}
	
	// Parse comma-separated origins
	origins := strings.Split(originsStr, ",")
	allowed := make([]string, 0, len(origins))
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowed = append(allowed, origin)
		}
	}
	return allowed
}

// isLocalNetworkOrigin allows phones and PCs on the same LAN / Tailscale / localhost.
func isLocalNetworkOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}

	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return true
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}

	ip4 := ip.To4()
	if ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return true
	}
	return false
}

func originHostMatchesRequest(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" {
		return false
	}
	reqHost := r.Host
	if h, _, err := net.SplitHostPort(reqHost); err == nil {
		reqHost = h
	}
	return strings.EqualFold(u.Hostname(), reqHost)
}

func cookieSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func isPrivateOrTailscaleIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	ip4 := ip.To4()
	return ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
}

func clientIsOnPrivateNetwork(r *http.Request) bool {
	return isPrivateOrTailscaleIP(net.ParseIP(getClientIP(r)))
}

// isOriginAllowed checks if an origin is in the allowed list
func isOriginAllowed(origin string, r *http.Request, allowedOrigins []string) bool {
	// In development, allow all origins for easier testing
	if os.Getenv("NODE_ENV") == "development" {
		return true
	}

	if isLocalNetworkOrigin(origin) {
		return true
	}
	if originHostMatchesRequest(origin, r) {
		return true
	}
	// Any device already on the LAN: allow its Origin (hostname, mDNS, etc.)
	if clientIsOnPrivateNetwork(r) {
		return true
	}
	
	// Check against whitelist
	for _, allowed := range allowedOrigins {
		if origin == allowed {
			return true
		}
		// Support wildcard subdomains (e.g., *.example.com)
		if strings.HasPrefix(allowed, "*.") {
			domain := strings.TrimPrefix(allowed, "*.")
			if strings.HasSuffix(origin, domain) {
				return true
			}
		}
	}
	return false
}

// CORS middleware
func corsMiddleware(next http.Handler) http.Handler {
	allowedOrigins := getAllowedOrigins()
	
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get origin from request
		origin := r.Header.Get("Origin")
		
		// If no Origin header, use the request's Host (for same-origin requests)
		// This handles cases where browser doesn't send Origin (like direct IP access)
		if origin == "" {
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			origin = scheme + "://" + r.Host
		}
		
		// Validate origin against whitelist (in production)
		if !isOriginAllowed(origin, r, allowedOrigins) {
			logWarn("CORS: Origin not allowed: %s", origin)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"Origin not allowed"}`))
			return
		}
		
		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Range")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Logging middleware
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusCapturingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		if shouldSkipAccessLog(r, sw.status) {
			return
		}
		logInfo("%s %s %s - %v", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	})
}

type statusCapturingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusCapturingResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusCapturingResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func shouldSkipAccessLog(r *http.Request, status int) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if !strings.HasPrefix(r.URL.Path, "/api/thumbnail") {
		return false
	}
	return status < 400
}

// Rate limiting middleware
func rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip rate limiting for health check
		if r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}

		// Skip rate limiting for static files
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		clientIP := getClientIP(r)
		now := time.Now().UnixMilli()

		// Use auth rate limit for auth endpoints
		if strings.HasPrefix(r.URL.Path, "/api/auth") && r.URL.Path != "/api/auth/refresh" && r.URL.Path != "/api/auth/status" {
			rateLimitMutex.Lock()
			limit, exists := authRateLimitMap[clientIP]
			if !exists {
				authRateLimitMap[clientIP] = &RateLimit{Count: 1, ResetTime: now + RATE_LIMIT_WINDOW}
				rateLimitMutex.Unlock()
				next.ServeHTTP(w, r)
				return
			}

			if now > limit.ResetTime {
				limit.Count = 1
				limit.ResetTime = now + RATE_LIMIT_WINDOW
			} else if limit.Count >= RATE_LIMIT_MAX {
				rateLimitMutex.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"Too many authentication attempts, please try again later"}`))
				return
			}
			limit.Count++
			rateLimitMutex.Unlock()
		} else {
			// Regular rate limiting
			rateLimitMutex.Lock()
			limit, exists := rateLimitMap[clientIP]
			if !exists {
				rateLimitMap[clientIP] = &RateLimit{Count: 1, ResetTime: now + RATE_LIMIT_WINDOW}
				rateLimitMutex.Unlock()
				next.ServeHTTP(w, r)
				return
			}

			if now > limit.ResetTime {
				limit.Count = 1
				limit.ResetTime = now + RATE_LIMIT_WINDOW
			} else if limit.Count >= RATE_LIMIT_MAX {
				rateLimitMutex.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"Too many requests, please try again later"}`))
				return
			}
			limit.Count++
			rateLimitMutex.Unlock()
		}

		next.ServeHTTP(w, r)
	})
}

// Priority middleware - ensures streaming/download requests get priority
func priorityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		clientIP := getClientIP(r)
		
		// Track streaming/download requests
		if strings.HasPrefix(path, "/api/stream/") || strings.HasPrefix(path, "/api/download/") {
			streamingMutex.Lock()
			activeStreamingRequests[clientIP] = time.Now().Unix()
			streamingMutex.Unlock()
			
			// Clean up after request completes
			defer func() {
				streamingMutex.Lock()
				delete(activeStreamingRequests, clientIP)
				streamingMutex.Unlock()
			}()
		}
		
		// For thumbnail requests, check if there are active streaming requests
		// If so, reduce priority by adding a small delay (max 50ms)
		if strings.HasPrefix(path, "/api/thumbnail/") {
			streamingMutex.RLock()
			hasActiveStreaming := len(activeStreamingRequests) > 0
			streamingMutex.RUnlock()
			
			if hasActiveStreaming {
				// Small delay to prioritize streaming requests
				// This allows streaming requests to be processed first
				time.Sleep(10 * time.Millisecond)
			}
		}
		
		next.ServeHTTP(w, r)
	})
}

func getClientIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip != "" {
		return strings.Split(ip, ",")[0]
	}
	ip = r.Header.Get("X-Real-IP")
	if ip != "" {
		return ip
	}
	ip = r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

