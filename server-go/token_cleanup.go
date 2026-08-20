package main

import (
	"time"
)

// startTokenCleanup runs a goroutine that periodically cleans up expired tokens
func startTokenCleanup() {
	ticker := time.NewTicker(5 * time.Minute) // Clean up every 5 minutes
	defer ticker.Stop()

	for range ticker.C {
		cleanupExpiredTokens()
	}
}

// cleanupExpiredTokens removes expired tokens from memory
func cleanupExpiredTokens() {
	now := time.Now().UnixMilli()
	cleaned := 0

	tokensMutex.Lock()
	for token, data := range tokens {
		if now > data.ExpiresAt {
			delete(tokens, token)
			if data.RefreshToken != "" {
				delete(refreshTokens, data.RefreshToken)
			}
			cleaned++
		}
	}

	// Also clean up expired refresh tokens
	for token, data := range refreshTokens {
		if now > data.ExpiresAt {
			delete(refreshTokens, token)
			cleaned++
		}
	}
	tokensMutex.Unlock()

	if cleaned > 0 {
		logDebug("Cleaned up %d expired tokens", cleaned)
	}
}

