package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

func TestListUsersHandler(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalUsersFile := USERS_FILE
	USERS_FILE = filepath.Join(tempDir, "users.json")
	defer func() { USERS_FILE = originalUsersFile }()

	// Setup admin user first
	adminID := "admin-list"
	usersMutex.Lock()
	users = []User{
		{ID: "1", Username: "user1", Email: "user1@test.com", Role: "user", Active: true},
		{ID: adminID, Username: "admin", Role: "admin", Active: true},
	}
	saveUsers()
	usersMutex.Unlock()

	// Create admin token
	token := generateToken(32)
	tokensMutex.Lock()
	tokens[token] = &TokenData{
		Username:  "admin",
		UserID:    adminID,
		ExpiresAt: time.Now().Add(15 * time.Minute).UnixMilli(),
	}
	tokensMutex.Unlock()

	req := httptest.NewRequest("GET", "/api/users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	// Simulate auth middleware setting X-User-ID
	authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-User-ID", adminID)
		requireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			listUsersHandler(w, r)
		})).ServeHTTP(w, r)
	})).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	usersList, ok := response["users"].([]interface{})
	if !ok || len(usersList) < 2 {
		t.Errorf("Expected at least 2 users, got %d", len(usersList))
	}
}

func TestCreateUserHandler(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalUsersFile := USERS_FILE
	USERS_FILE = filepath.Join(tempDir, "users.json")
	defer func() { USERS_FILE = originalUsersFile }()

	adminID := "admin-create"
	usersMutex.Lock()
	users = []User{
		{ID: adminID, Username: "admin", Role: "admin", Active: true},
	}
	saveUsers()
	usersMutex.Unlock()

	// Create admin token
	token := generateToken(32)
	tokensMutex.Lock()
	tokens[token] = &TokenData{
		Username:  "admin",
		UserID:    adminID,
		ExpiresAt: time.Now().Add(15 * time.Minute).UnixMilli(),
	}
	tokensMutex.Unlock()

	reqBody := map[string]interface{}{
		"username":    "newuser",
		"password":    "password123",
		"email":       "newuser@test.com",
		"role":        "user",
		"permissions": []string{"read"},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/users", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-User-ID", adminID)
		requireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			createUserHandler(w, r)
		})).ServeHTTP(w, r)
	})).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify user was created
	usersMutex.RLock()
	user := getUserByUsername("newuser")
	usersMutex.RUnlock()

	if user == nil {
		t.Fatal("User should be created")
	}

	if user.Email != "newuser@test.com" {
		t.Errorf("Expected email 'newuser@test.com', got %s", user.Email)
	}

	// Verify password is hashed
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte("password123")); err != nil {
		t.Error("Password should be hashed correctly")
	}
}

func TestCreateUserHandlerValidation(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalUsersFile := USERS_FILE
	USERS_FILE = filepath.Join(tempDir, "users.json")
	defer func() { USERS_FILE = originalUsersFile }()

	adminID := "admin-validate"
	usersMutex.Lock()
	users = []User{
		{ID: adminID, Username: "admin", Role: "admin", Active: true},
	}
	saveUsers()
	usersMutex.Unlock()

	token := generateToken(32)
	tokensMutex.Lock()
	tokens[token] = &TokenData{
		Username:  "admin",
		UserID:    adminID,
		ExpiresAt: time.Now().Add(15 * time.Minute).UnixMilli(),
	}
	tokensMutex.Unlock()

	// Test with invalid username (too short)
	reqBody := map[string]interface{}{
		"username": "ab",
		"password": "password123",
		"email":    "test@test.com",
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/users", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-User-ID", adminID)
		requireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			createUserHandler(w, r)
		})).ServeHTTP(w, r)
	})).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestUpdateUserHandler(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalUsersFile := USERS_FILE
	USERS_FILE = filepath.Join(tempDir, "users.json")
	defer func() { USERS_FILE = originalUsersFile }()

	adminID := "admin-update"
	usersMutex.Lock()
	users = []User{
		{ID: "1", Username: "user1", Email: "user1@test.com", Role: "user", Active: true},
		{ID: adminID, Username: "admin", Role: "admin", Active: true},
	}
	saveUsers()
	usersMutex.Unlock()

	token := generateToken(32)
	tokensMutex.Lock()
	tokens[token] = &TokenData{
		Username:  "admin",
		UserID:    adminID,
		ExpiresAt: time.Now().Add(15 * time.Minute).UnixMilli(),
	}
	tokensMutex.Unlock()

	reqBody := map[string]interface{}{
		"email": "updated@test.com",
		"role":  "admin",
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("PUT", "/api/users/1", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req = mux.SetURLVars(req, map[string]string{"id": "1"})
	w := httptest.NewRecorder()

	authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-User-ID", adminID)
		requireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			updateUserHandler(w, r)
		})).ServeHTTP(w, r)
	})).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify user was updated
	usersMutex.RLock()
	user := getUserByID("1")
	usersMutex.RUnlock()

	if user == nil {
		t.Fatal("User should exist")
	}

	if user.Email != "updated@test.com" {
		t.Errorf("Expected email 'updated@test.com', got %s", user.Email)
	}

	if user.Role != "admin" {
		t.Errorf("Expected role 'admin', got %s", user.Role)
	}
}

func TestDeleteUserHandler(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	originalUsersFile := USERS_FILE
	USERS_FILE = filepath.Join(tempDir, "users.json")
	defer func() { USERS_FILE = originalUsersFile }()

	adminID := "admin-delete"
	usersMutex.Lock()
	users = []User{
		{ID: "1", Username: "user1", Email: "user1@test.com", Role: "user", Active: true},
		{ID: adminID, Username: "admin", Role: "admin", Active: true},
	}
	saveUsers()
	usersMutex.Unlock()

	token := generateToken(32)
	tokensMutex.Lock()
	tokens[token] = &TokenData{
		Username:  "admin",
		UserID:    adminID,
		ExpiresAt: time.Now().Add(15 * time.Minute).UnixMilli(),
	}
	tokensMutex.Unlock()

	req := httptest.NewRequest("DELETE", "/api/users/1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req = mux.SetURLVars(req, map[string]string{"id": "1"})
	w := httptest.NewRecorder()

	authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-User-ID", adminID)
		requireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			deleteUserHandler(w, r)
		})).ServeHTTP(w, r)
	})).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify user was deactivated
	usersMutex.RLock()
	var user *User
	for i := range users {
		if users[i].ID == "1" {
			user = &users[i]
			break
		}
	}
	usersMutex.RUnlock()

	if user == nil {
		t.Fatal("User should exist")
	}

	if user.Active {
		t.Error("User should be deactivated")
	}
}

func TestRequireRoleMiddleware(t *testing.T) {
	// Setup users
	tempDir := t.TempDir()
	originalUsersFile := USERS_FILE
	USERS_FILE = filepath.Join(tempDir, "users.json")
	defer func() { USERS_FILE = originalUsersFile }()

	usersMutex.Lock()
	users = []User{
		{ID: "admin-1", Username: "admin", Role: "admin", Active: true},
		{ID: "user-1", Username: "user", Role: "user", Active: true},
	}
	saveUsers()
	usersMutex.Unlock()

	handler := requireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Test with admin user ID
	req := httptest.NewRequest("GET", "/api/users", nil)
	req.Header.Set("X-User-ID", "admin-1")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Test with user role (should fail)
	req = httptest.NewRequest("GET", "/api/users", nil)
	req.Header.Set("X-User-ID", "user-1")
	w = httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", w.Code)
	}

	// Test with non-existent user
	req = httptest.NewRequest("GET", "/api/users", nil)
	req.Header.Set("X-User-ID", "non-existent")
	w = httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", w.Code)
	}
}

