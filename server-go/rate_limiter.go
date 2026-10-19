package main

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// Improved rate limiter with sliding window
type slidingWindowLimiter struct {
	requests map[string][]int64 // IP -> timestamps
	mu       sync.RWMutex
	window   time.Duration
	maxReqs  int
}

var (
	globalLimiter *slidingWindowLimiter
	authLimiter   *slidingWindowLimiter
	limiterOnce   sync.Once
)

const (
	// Rate limit constants
	DEFAULT_RATE_LIMIT_WINDOW = 60 * time.Second // 1 minute
	// 600/min por IP: o app abre várias consultas por tela e edições em
	// sequência (mover vários itens) passavam fácil de 100.
	DEFAULT_RATE_LIMIT_MAX = 600
	AUTH_RATE_LIMIT_MAX       = 10               // 10 auth attempts per minute
)

func initRateLimiters() {
	limiterOnce.Do(func() {
		globalLimiter = &slidingWindowLimiter{
			requests: make(map[string][]int64),
			window:   DEFAULT_RATE_LIMIT_WINDOW,
			maxReqs:  DEFAULT_RATE_LIMIT_MAX,
		}

		authLimiter = &slidingWindowLimiter{
			requests: make(map[string][]int64),
			window:   DEFAULT_RATE_LIMIT_WINDOW,
			maxReqs:  AUTH_RATE_LIMIT_MAX,
		}

		// Cleanup old entries periodically
		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				globalLimiter.cleanup()
				authLimiter.cleanup()
			}
		}()
	})
}

func (rl *slidingWindowLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now().Unix()
	cutoff := now - int64(rl.window.Seconds())

	for ip, timestamps := range rl.requests {
		// Remove old timestamps
		valid := make([]int64, 0)
		for _, ts := range timestamps {
			if ts > cutoff {
				valid = append(valid, ts)
			}
		}

		if len(valid) == 0 {
			delete(rl.requests, ip)
		} else {
			rl.requests[ip] = valid
		}
	}
}

func (rl *slidingWindowLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now().Unix()
	cutoff := now - int64(rl.window.Seconds())

	// Get existing requests
	timestamps := rl.requests[ip]

	// Remove old timestamps (sliding window)
	valid := make([]int64, 0)
	for _, ts := range timestamps {
		if ts > cutoff {
			valid = append(valid, ts)
		}
	}

	// Check if limit exceeded
	if len(valid) >= rl.maxReqs {
		return false
	}

	// Add current request
	valid = append(valid, now)
	rl.requests[ip] = valid

	return true
}

func skipRateLimit(path string) bool {
	if path == "/api/health" {
		return true
	}
	if !strings.HasPrefix(path, "/api/") {
		return true
	}
	// Media asset endpoints fire many times per folder (range requests, one
	// thumbnail per card, plus /api/thumbnails batch polling). Rate-limiting
	// them returns 429 after ~100 requests/min and blanks the grid.
	if strings.HasPrefix(path, "/api/stream/") ||
		strings.HasPrefix(path, "/api/download/") ||
		strings.HasPrefix(path, "/api/thumbnail") {
		return true
	}
	return false
}

func (rl *slidingWindowLimiter) remaining(ip string) int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	now := time.Now().Unix()
	cutoff := now - int64(rl.window.Seconds())

	timestamps := rl.requests[ip]
	valid := 0
	for _, ts := range timestamps {
		if ts > cutoff {
			valid++
		}
	}

	return rl.maxReqs - valid
}

// Improved rate limiting middleware with sliding window
func improvedRateLimitMiddleware(next http.Handler) http.Handler {
	initRateLimiters()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skipRateLimit(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		clientIP := getClientIP(r)
		var allowed bool

		// Use stricter limit for auth endpoints
		if strings.HasPrefix(r.URL.Path, "/api/auth") &&
			r.URL.Path != "/api/auth/refresh" &&
			r.URL.Path != "/api/auth/status" {
			allowed = authLimiter.allow(clientIP)
		} else {
			allowed = globalLimiter.allow(clientIP)
		}

		if !allowed {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)

			var message string
			if strings.HasPrefix(r.URL.Path, "/api/auth") {
				message = "Too many authentication attempts, please try again later"
			} else {
				message = "Too many requests, please try again later"
			}

			w.Write([]byte(`{"error":"` + message + `"}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}
