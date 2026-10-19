package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestLoginHandler(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalUsersFile := USERS_FILE
	USERS_FILE = filepath.Join(tempDir, "users.json")
	defer func() { USERS_FILE = originalUsersFile }()

	// Create test user
	usersMutex.Lock()
	hash, _ := bcrypt.GenerateFromPassword([]byte("testpass"), 10)
	users = []User{
		{
			ID:       "1",
			Username: "testuser",
			Password: string(hash),
			Email:    "test@example.com",
			Role:     "user",
			Active:   true,
		},
	}
	saveUsers()
	usersMutex.Unlock()

	// Test successful login
	reqBody := map[string]interface{}{
		"username": "testuser",
		"password": "testpass",
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	loginHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if success, ok := response["success"].(bool); !ok || !success {
		t.Error("Login should be successful")
	}

	// Test failed login
	reqBody["password"] = "wrongpass"
	bodyBytes, _ = json.Marshal(reqBody)
	req = httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()

	loginHandler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

func TestAuthStatusHandler(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalUsersFile := USERS_FILE
	USERS_FILE = filepath.Join(tempDir, "users.json")
	defer func() { USERS_FILE = originalUsersFile }()

	// Create test user and token
	usersMutex.Lock()
	hash, _ := bcrypt.GenerateFromPassword([]byte("testpass"), 10)
	users = []User{
		{
			ID:       "1",
			Username: "testuser",
			Password: string(hash),
			Email:    "test@example.com",
			Role:     "user",
			Active:   true,
		},
	}
	saveUsers()
	usersMutex.Unlock()

	// Setup user first
	usersMutex.Lock()
	userID := "test-user-id"
	users = []User{
		{ID: userID, Username: "testuser", Email: "test@test.com", Role: "user", Active: true},
	}
	saveUsers()
	usersMutex.Unlock()

	token := generateToken(32)
	putTestAccessToken(token, &TokenData{
		Username:  "testuser",
		UserID:    userID,
		ExpiresAt: time.Now().Add(15 * time.Minute).UnixMilli(),
	})

	// Test with valid token
	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authStatusHandler(w, r)
	})).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if username, ok := response["username"].(string); !ok || username != "testuser" {
		t.Errorf("Expected username 'testuser', got %v", username)
	}

	// Test without token (should return 404 because user not found without X-User-ID)
	req = httptest.NewRequest("GET", "/api/auth/status", nil)
	w = httptest.NewRecorder()

	authStatusHandler(w, req)

	// authStatusHandler returns 404 when user not found (no X-User-ID header)
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestLogoutHandler(t *testing.T) {
	// Setup
	token := generateToken(32)
	putTestAccessToken(token, &TokenData{
		Username:  "testuser",
		UserID:    "test-user-id",
		ExpiresAt: time.Now().Add(15 * time.Minute).UnixMilli(),
	})

	req := httptest.NewRequest("POST", "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{
		Name:  "accessToken",
		Value: token,
	})
	w := httptest.NewRecorder()

	logoutHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Verify token was removed
	tokensMutex.RLock()
	_, exists := tokens[hashToken(token)]
	tokensMutex.RUnlock()

	if exists {
		t.Error("Token should be removed after logout")
	}
}

func TestRefreshHandler(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalUsersFile := USERS_FILE
	USERS_FILE = filepath.Join(tempDir, "users.json")
	defer func() { USERS_FILE = originalUsersFile }()

	usersMutex.Lock()
	hash, _ := bcrypt.GenerateFromPassword([]byte("testpass"), 10)
	users = []User{
		{
			ID:       "1",
			Username: "testuser",
			Password: string(hash),
			Email:    "test@example.com",
			Role:     "user",
			Active:   true,
		},
	}
	saveUsers()
	usersMutex.Unlock()

	refreshToken := generateToken(32)
	putTestRefreshToken(refreshToken, &TokenData{
		Username:  "testuser",
		UserID:    "test-user-id",
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour).UnixMilli(),
	})

	req := httptest.NewRequest("POST", "/api/auth/refresh", nil)
	req.AddCookie(&http.Cookie{
		Name:  "refreshToken",
		Value: refreshToken,
	})
	w := httptest.NewRecorder()

	refreshHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Check for new access token cookie
	cookies := w.Header().Get("Set-Cookie")
	if cookies == "" {
		t.Error("Should set new access token cookie")
	}
}

func TestForgotPasswordHandler(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalUsersFile := USERS_FILE
	USERS_FILE = filepath.Join(tempDir, "users.json")
	defer func() { USERS_FILE = originalUsersFile }()

	usersMutex.Lock()
	users = []User{
		{
			ID:       "1",
			Username: "testuser",
			Password: "hash",
			Email:    "test@example.com",
			Role:     "user",
			Active:   true,
		},
	}
	saveUsers()
	usersMutex.Unlock()

	reqBody := map[string]interface{}{
		"email": "test@example.com",
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/auth/forgot-password", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	forgotPasswordHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify reset token was created
	usersMutex.RLock()
	user := getUserByEmail("test@example.com")
	usersMutex.RUnlock()

	if user == nil || user.ResetToken == nil {
		t.Error("Reset token should be created")
	}
}

func TestResetPasswordHandler(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalUsersFile := USERS_FILE
	USERS_FILE = filepath.Join(tempDir, "users.json")
	defer func() { USERS_FILE = originalUsersFile }()

	token := generateToken(32)
	expiry := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	usersMutex.Lock()
	users = []User{
		{
			ID:              "1",
			Username:        "testuser",
			Password:        "oldhash",
			Email:           "test@example.com",
			Role:            "user",
			Active:          true,
			ResetToken:      &token,
			ResetTokenExpiry: &expiry,
		},
	}
	saveUsers()
	usersMutex.Unlock()

	reqBody := map[string]interface{}{
		"email":       "test@example.com",
		"token":       token,
		"newPassword": "newpassword123",
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/auth/reset-password", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	resetPasswordHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify password was changed
	usersMutex.RLock()
	user := getUserByEmail("test@example.com")
	usersMutex.RUnlock()

	if user == nil {
		t.Fatal("User should exist")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte("newpassword123")); err != nil {
		t.Error("Password should be updated")
	}

	if user.ResetToken != nil {
		t.Error("Reset token should be cleared after use")
	}
}

func TestAuthMiddlewareMediaToken(t *testing.T) {
	t.Setenv("NODE_ENV", "production")

	token := generateToken(32)
	putTestAccessToken(token, &TokenData{
		Username:  "testuser",
		UserID:    "test-user-id",
		ExpiresAt: time.Now().Add(15 * time.Minute).UnixMilli(),
	})

	okHandler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("thumbnail accepts query token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/thumbnail/folder/file.jpg?token="+token, nil)
		w := httptest.NewRecorder()
		okHandler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("thumbnail accepts imgToken cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/thumbnail/folder/file.jpg", nil)
		req.AddCookie(&http.Cookie{Name: "imgToken", Value: token})
		w := httptest.NewRecorder()
		okHandler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("status rejects query token in production", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/auth/status?token="+token, nil)
		w := httptest.NewRecorder()
		okHandler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})
}

