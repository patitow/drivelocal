package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// generateRandomPassword generates a secure random password
func generateRandomPassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	b := make([]byte, length)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			// Fallback: use timestamp-based seed if crypto/rand fails
			b[i] = charset[int(time.Now().UnixNano())%len(charset)]
		} else {
			b[i] = charset[n.Int64()]
		}
	}
	return string(b)
}

// Load users from JSON file
func loadUsers() {
	isDevelopment := os.Getenv("NODE_ENV") == "development"
	
	data, err := os.ReadFile(USERS_FILE)
	if err != nil {
		logWarn("Failed to read users.json: %v - Creating default admin user", err)
		createDefaultAdmin(isDevelopment)
		return
	}
	
	// Remove BOM if present (UTF-8 BOM: EF BB BF)
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	
	if err := json.Unmarshal(data, &users); err != nil {
		logError("Failed to parse users.json: %v - Creating default admin user", err)
		createDefaultAdmin(isDevelopment)
		return
	}
	
	// In development, ensure admin/admin always exists
	if isDevelopment {
		ensureDevAdmin()
	}
	
	logInfo("Loaded %d users from users.json", len(users))
	for _, u := range users {
		logDebug("  - %s (%s)", u.Username, u.Email)
	}
}

// createDefaultAdmin creates the default admin user
func createDefaultAdmin(isDevelopment bool) {
	var password string
	var passwordHash []byte
	
	if isDevelopment {
		// In development: use "admin" as password
		password = "admin"
		passwordHash, _ = bcrypt.GenerateFromPassword([]byte(password), 10)
		logInfo("🔧 Development mode: Creating admin user with password 'admin'")
	} else {
		// In production: generate random password
		password = generateRandomPassword(16)
		passwordHash, _ = bcrypt.GenerateFromPassword([]byte(password), 10)
		logWarn("⚠️  Production mode: Default admin user created with random password. Password: %s", password)
		logWarn("⚠️  IMPORTANT: Change this password immediately after first login!")
	}
	
	defaultUser := User{
		ID:          generateID(),
		Username:    "admin",
		Password:    string(passwordHash),
		Email:       "admin@drivelocal.local",
		Role:        "admin",
		Permissions: []string{"read", "write", "delete", "admin"},
		CreatedAt:   time.Now().Format(time.RFC3339),
		Active:      true,
	}
	users = []User{defaultUser}
	if err := saveUsers(); err != nil {
		logError("Failed to save default user: %v", err)
	}
}

// ensureDevAdmin ensures admin/admin exists in development mode
func ensureDevAdmin() {
	usersMutex.Lock()
	defer usersMutex.Unlock()
	
	// Check if admin user exists
	adminExists := false
	adminIndex := -1
	for i := range users {
		if users[i].Username == "admin" {
			adminExists = true
			adminIndex = i
			break
		}
	}
	
	if adminExists {
		// Update admin password to "admin" in development
		hash, _ := bcrypt.GenerateFromPassword([]byte("admin"), 10)
		users[adminIndex].Password = string(hash)
		users[adminIndex].Active = true
		users[adminIndex].Role = "admin"
		users[adminIndex].Permissions = []string{"read", "write", "delete", "admin"}
		if err := saveUsers(); err != nil {
			logError("Failed to update dev admin: %v", err)
		} else {
			logInfo("🔧 Development mode: Admin user password reset to 'admin'")
		}
	} else {
		// Create admin/admin user
		hash, _ := bcrypt.GenerateFromPassword([]byte("admin"), 10)
		adminUser := User{
			ID:          generateID(),
			Username:    "admin",
			Password:    string(hash),
			Email:       "admin@drivelocal.local",
			Role:        "admin",
			Permissions: []string{"read", "write", "delete", "admin"},
			CreatedAt:   time.Now().Format(time.RFC3339),
			Active:      true,
		}
		users = append(users, adminUser)
		if err := saveUsers(); err != nil {
			logError("Failed to create dev admin: %v", err)
		} else {
			logInfo("🔧 Development mode: Admin user created with password 'admin'")
		}
	}
}

func saveUsers() error {
	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		logError("Failed to marshal users: %v", err)
		return err
	}
	// Use 0600 permissions (read/write for owner only)
	if err := os.WriteFile(USERS_FILE, data, 0600); err != nil {
		logError("Failed to save users: %v", err)
		return err
	}
	return nil
}

func getUserByUsername(username string) *User {
	usersMutex.RLock()
	defer usersMutex.RUnlock()
	for i := range users {
		if users[i].Username == username && users[i].Active {
			return &users[i]
		}
	}
	return nil
}

func getUserByID(id string) *User {
	usersMutex.RLock()
	defer usersMutex.RUnlock()
	for i := range users {
		if users[i].ID == id && users[i].Active {
			return &users[i]
		}
	}
	return nil
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateToken(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func createTokens(username, userID string, rememberMe bool) (string, string, int64) {
	accessToken := generateToken(32)
	refreshToken := generateToken(64)
	
	now := time.Now().UnixMilli()
	accessExpires := now + ACCESS_TOKEN_EXPIRY
	refreshExpires := now + REFRESH_TOKEN_EXPIRY
	if rememberMe {
		refreshExpires = now + REMEMBER_ME_EXPIRY
	}

	// Os mapas usam hashToken(raw) como chave; o token cru só existe no cliente.
	refreshKey := hashToken(refreshToken)
	tokensMutex.Lock()
	tokens[hashToken(accessToken)] = &TokenData{
		Username:     username,
		UserID:       userID,
		ExpiresAt:    accessExpires,
		RefreshToken: refreshKey,
	}
	refreshTokens[refreshKey] = &TokenData{
		Username:  username,
		UserID:    userID,
		ExpiresAt: refreshExpires,
	}
	tokensMutex.Unlock()
	markSessionsDirty()

	return accessToken, refreshToken, accessExpires
}

func validateToken(token string) *TokenData {
	key := hashToken(token)

	tokensMutex.RLock()
	data, exists := tokens[key]
	tokensMutex.RUnlock()
	if !exists {
		return nil
	}

	if time.Now().UnixMilli() > data.ExpiresAt {
		tokensMutex.Lock()
		delete(tokens, key)
		tokensMutex.Unlock()
		markSessionsDirty()
		return nil
	}

	return data
}

func refreshAccessToken(refreshToken string) (string, int64, bool) {
	refreshKey := hashToken(refreshToken)

	tokensMutex.Lock()
	defer tokensMutex.Unlock()

	data, exists := refreshTokens[refreshKey]
	if !exists {
		return "", 0, false
	}

	if time.Now().UnixMilli() > data.ExpiresAt {
		delete(refreshTokens, refreshKey)
		markSessionsDirty()
		return "", 0, false
	}

	// Create new access token
	newAccessToken := generateToken(32)
	now := time.Now().UnixMilli()
	expires := now + ACCESS_TOKEN_EXPIRY

	tokens[hashToken(newAccessToken)] = &TokenData{
		Username:     data.Username,
		UserID:       data.UserID,
		ExpiresAt:    expires,
		RefreshToken: refreshKey,
	}
	markSessionsDirty()

	return newAccessToken, expires, true
}

func revokeToken(token string) {
	key := hashToken(token)

	tokensMutex.Lock()
	if data, exists := tokens[key]; exists {
		if data.RefreshToken != "" {
			delete(refreshTokens, data.RefreshToken)
		}
		delete(tokens, key)
	}
	tokensMutex.Unlock()
	markSessionsDirty()
}

func isMediaAuthPath(path string) bool {
	return strings.HasPrefix(path, "/api/thumbnail/") ||
		strings.HasPrefix(path, "/api/stream/") ||
		strings.HasPrefix(path, "/api/download/")
}

func cookieValue(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil || cookie == nil {
		return ""
	}
	return cookie.Value
}

// requestAuthToken reads credentials for API calls and for <img>/<video> src.
// Media URLs cannot send an Authorization header, so they use imgToken or ?token=.
func requestAuthToken(r *http.Request) string {
	if token := cookieValue(r, "accessToken"); token != "" {
		return token
	}
	if token := cookieValue(r, "imgToken"); token != "" {
		return token
	}
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	if os.Getenv("NODE_ENV") == "development" || isMediaAuthPath(r.URL.Path) {
		return r.URL.Query().Get("token")
	}
	return ""
}

// Auth middleware
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/api/auth/login" ||
			path == "/api/health" ||
			path == "/api/auth/refresh" ||
			path == "/api/auth/forgot-password" ||
			path == "/api/auth/reset-password" {
			next.ServeHTTP(w, r)
			return
		}

		token := requestAuthToken(r)
		var tokenData *TokenData
		if token != "" {
			tokenData = validateToken(token)
			if tokenData == nil && strings.HasPrefix(token, apiTokenPrefix) {
				tokenData = validateAPIToken(token) // tokens de API da biblioteca (library_tokens.go)
			}
		}

		// Sem token ou com token vencido (o navegador guarda o último e manda como Bearer):
		// renova pelo refresh token, senão o "lembrar-me" cairia a cada 15 minutos.
		if tokenData == nil {
			if refreshCookie, err := r.Cookie("refreshToken"); err == nil && refreshCookie.Value != "" {
				if newToken, _, ok := refreshAccessToken(refreshCookie.Value); ok {
					tokenData = validateToken(newToken)
					secure := cookieSecure(r)
					http.SetCookie(w, &http.Cookie{
						Name:     "accessToken",
						Value:    newToken,
						HttpOnly: true,
						Secure:   secure,
						SameSite: http.SameSiteLaxMode,
						MaxAge:   15 * 60,
						Path:     "/",
					})
					http.SetCookie(w, &http.Cookie{
						Name:     "imgToken",
						Value:    newToken,
						HttpOnly: false,
						Secure:   secure,
						SameSite: http.SameSiteLaxMode,
						MaxAge:   15 * 60,
						Path:     "/",
					})
				}
			}
		}

		if tokenData == nil {
			logDebug("Missing or expired token for %s", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"Authentication required"}`, http.StatusUnauthorized)
			return
		}

		// Add user info to request context
		r.Header.Set("X-User-ID", tokenData.UserID)
		r.Header.Set("X-Username", tokenData.Username)

		next.ServeHTTP(w, r)
	})
}

func requireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := r.Header.Get("X-User-ID")
			user := getUserByID(userID)
			if user == nil || (user.Role != role && user.Role != "admin") {
				http.Error(w, `{"error":"Insufficient role"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

