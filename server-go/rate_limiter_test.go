package main

import (
	"sync"
	"testing"
	"time"
)

func TestSlidingWindowLimiter(t *testing.T) {
	limiter := &slidingWindowLimiter{
		requests: make(map[string][]int64),
		window:   60 * time.Second,
		maxReqs:  10,
	}

	ip := "127.0.0.1"

	// First 10 requests should be allowed
	for i := 0; i < 10; i++ {
		if !limiter.allow(ip) {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// 11th request should be denied
	if limiter.allow(ip) {
		t.Error("11th request should be denied")
	}

	// Check remaining
	remaining := limiter.remaining(ip)
	if remaining != 0 {
		t.Errorf("Expected 0 remaining, got %d", remaining)
	}
}

func TestSlidingWindowLimiterCleanup(t *testing.T) {
	limiter := &slidingWindowLimiter{
		requests: make(map[string][]int64),
		window:   1 * time.Second,
		maxReqs:  10,
	}

	ip := "127.0.0.1"

	// Add old timestamps
	limiter.mu.Lock()
	limiter.requests[ip] = []int64{
		time.Now().Unix() - 10, // 10 seconds ago
		time.Now().Unix() - 5,  // 5 seconds ago
		time.Now().Unix(),      // now
	}
	limiter.mu.Unlock()

	// Cleanup
	limiter.cleanup()

	// Verify only recent timestamps remain
	limiter.mu.RLock()
	timestamps := limiter.requests[ip]
	limiter.mu.RUnlock()

	if len(timestamps) > 1 {
		t.Errorf("Expected 1 timestamp after cleanup, got %d", len(timestamps))
	}
}

func TestSlidingWindowLimiterConcurrent(t *testing.T) {
	limiter := &slidingWindowLimiter{
		requests: make(map[string][]int64),
		window:   60 * time.Second,
		maxReqs:  100,
	}

	ip := "127.0.0.1"
	var wg sync.WaitGroup
	concurrent := 50

	// Make concurrent requests
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limiter.allow(ip)
		}()
	}

	wg.Wait()

	// Verify we didn't exceed the limit
	remaining := limiter.remaining(ip)
	if remaining < 0 {
		t.Error("Should not exceed limit")
	}
}

func TestInitRateLimiters(t *testing.T) {
	// Reset globals
	globalLimiter = nil
	authLimiter = nil
	limiterOnce = sync.Once{}

	initRateLimiters()

	if globalLimiter == nil {
		t.Error("globalLimiter should be initialized")
	}

	if authLimiter == nil {
		t.Error("authLimiter should be initialized")
	}

	// Test idempotency
	initRateLimiters()
	initRateLimiters()

	if globalLimiter == nil || authLimiter == nil {
		t.Error("Should be idempotent")
	}
}

