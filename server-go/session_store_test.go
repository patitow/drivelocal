package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain redireciona o arquivo de sessões para um diretório temporário para
// que nenhum teste grave em server/data/ do projeto.
func TestMain(m *testing.M) {
	initLogger()
	dir, err := os.MkdirTemp("", "drivelocal-sessions-test")
	if err != nil {
		panic(err)
	}
	SESSIONS_FILE = filepath.Join(dir, "sessions.json")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// putTestAccessToken grava um access token direto nos mapas, com a chave hasheada.
func putTestAccessToken(token string, data *TokenData) {
	tokensMutex.Lock()
	defer tokensMutex.Unlock()
	tokens[hashToken(token)] = data
}

// putTestRefreshToken grava um refresh token direto nos mapas, com a chave hasheada.
func putTestRefreshToken(token string, data *TokenData) {
	tokensMutex.Lock()
	defer tokensMutex.Unlock()
	refreshTokens[hashToken(token)] = data
}

func resetSessionState(t *testing.T) {
	t.Helper()
	stopPendingFlush()
	tokensMutex.Lock()
	tokens = make(map[string]*TokenData)
	refreshTokens = make(map[string]*TokenData)
	tokensMutex.Unlock()
	os.Remove(SESSIONS_FILE)
}

func clearInMemorySessions() {
	tokensMutex.Lock()
	tokens = make(map[string]*TokenData)
	refreshTokens = make(map[string]*TokenData)
	tokensMutex.Unlock()
}

func setTestUsers(list ...User) {
	usersMutex.Lock()
	users = list
	usersMutex.Unlock()
}

func TestSessionsSurviveRestart(t *testing.T) {
	resetSessionState(t)
	setTestUsers(User{ID: "u-restart", Username: "restart", Role: "user", Active: true})

	access, refresh, _ := createTokens("restart", "u-restart", true)
	if err := flushSessions(); err != nil {
		t.Fatalf("flushSessions: %v", err)
	}

	// Simula restart: memória zerada, depois carrega do disco.
	clearInMemorySessions()
	if validateToken(access) != nil {
		t.Fatal("token should not be valid before load")
	}
	loadSessions()

	if v := validateToken(access); v == nil || v.UserID != "u-restart" {
		t.Fatalf("access token should be valid after reload, got %+v", v)
	}
	if newAccess, _, ok := refreshAccessToken(refresh); !ok || newAccess == "" {
		t.Fatal("refresh token should work after reload")
	}
}

func TestSessionsFileHasNoRawTokens(t *testing.T) {
	resetSessionState(t)
	setTestUsers(User{ID: "u-raw", Username: "raw", Role: "user", Active: true})

	access, refresh, _ := createTokens("raw", "u-raw", false)
	if err := flushSessions(); err != nil {
		t.Fatalf("flushSessions: %v", err)
	}

	data, err := os.ReadFile(SESSIONS_FILE)
	if err != nil {
		t.Fatalf("read sessions file: %v", err)
	}
	content := string(data)
	if strings.Contains(content, access) || strings.Contains(content, refresh) {
		t.Fatal("raw token found in sessions file")
	}

	var f sessionsFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse sessions file: %v", err)
	}
	if _, ok := f.Tokens[hashToken(access)]; !ok {
		t.Error("access token hash missing from sessions file")
	}
	if _, ok := f.RefreshTokens[hashToken(refresh)]; !ok {
		t.Error("refresh token hash missing from sessions file")
	}
}

func TestSessionsLoadDropsExpiredAndInactive(t *testing.T) {
	resetSessionState(t)
	setTestUsers(
		User{ID: "u-active", Username: "active", Role: "user", Active: true},
		User{ID: "u-off", Username: "off", Role: "user", Active: false},
	)

	future := time.Now().Add(time.Hour).UnixMilli()
	past := time.Now().Add(-time.Hour).UnixMilli()
	putTestAccessToken("live", &TokenData{UserID: "u-active", Username: "active", ExpiresAt: future})
	putTestAccessToken("expired", &TokenData{UserID: "u-active", Username: "active", ExpiresAt: past})
	putTestAccessToken("inactive", &TokenData{UserID: "u-off", Username: "off", ExpiresAt: future})
	putTestRefreshToken("expired-refresh", &TokenData{UserID: "u-active", Username: "active", ExpiresAt: past})

	if err := flushSessions(); err != nil {
		t.Fatalf("flushSessions: %v", err)
	}
	clearInMemorySessions()
	loadSessions()

	if validateToken("live") == nil {
		t.Error("live token should be restored")
	}
	if validateToken("inactive") != nil {
		t.Error("token of inactive user should be dropped")
	}
	tokensMutex.RLock()
	nTokens, nRefresh := len(tokens), len(refreshTokens)
	tokensMutex.RUnlock()
	if nTokens != 1 {
		t.Errorf("expected 1 access token after load, got %d", nTokens)
	}
	if nRefresh != 0 {
		t.Errorf("expected 0 refresh tokens after load, got %d", nRefresh)
	}
}

func TestRevokeUserTokensOnlyAffectsThatUser(t *testing.T) {
	resetSessionState(t)
	setTestUsers(
		User{ID: "u-x", Username: "x", Role: "user", Active: true},
		User{ID: "u-y", Username: "y", Role: "user", Active: true},
	)

	accessX, refreshX, _ := createTokens("x", "u-x", false)
	accessY, _, _ := createTokens("y", "u-y", false)

	revokeUserTokens("u-x")

	if validateToken(accessX) != nil {
		t.Error("revoked user's access token should be invalid")
	}
	if _, _, ok := refreshAccessToken(refreshX); ok {
		t.Error("revoked user's refresh token should be invalid")
	}
	if validateToken(accessY) == nil {
		t.Error("other user's token should remain valid")
	}
}

func TestHealthIncludesBootID(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	healthHandler(w, req)

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse health response: %v", err)
	}
	if got, _ := body["bootId"].(string); got == "" || got != bootID {
		t.Fatalf("expected bootId %q in health response, got %v", bootID, body["bootId"])
	}
}

func TestCleanupKeepsRefreshTokenAfterAccessExpires(t *testing.T) {
	resetSessionState(t)
	setTestUsers(User{ID: "u-cleanup", Username: "cleanup", Role: "user", Active: true})

	access, refresh, _ := createTokens("cleanup", "u-cleanup", false)
	// Access token expira; o refresh token continua válido pelo próprio prazo.
	tokensMutex.Lock()
	tokens[hashToken(access)].ExpiresAt = time.Now().Add(-time.Minute).UnixMilli()
	tokensMutex.Unlock()

	cleanupExpiredTokens()

	if validateToken(access) != nil {
		t.Fatal("expired access token should be invalid")
	}
	if newAccess, _, ok := refreshAccessToken(refresh); !ok || newAccess == "" {
		t.Fatal("refresh token should still work after access expiry and cleanup")
	}
}

func TestResetPasswordRevokesUserSessions(t *testing.T) {
	resetSessionState(t)
	originalUsersFile := USERS_FILE
	USERS_FILE = filepath.Join(t.TempDir(), "users.json")
	t.Cleanup(func() { USERS_FILE = originalUsersFile })

	resetToken := generateToken(32)
	expiry := time.Now().Add(time.Hour).Format(time.RFC3339)
	setTestUsers(
		User{ID: "u-reset", Username: "reset", Email: "reset@example.com", Password: "oldhash", Role: "user", Active: true, ResetToken: &resetToken, ResetTokenExpiry: &expiry},
		User{ID: "u-keep", Username: "keep", Email: "keep@example.com", Password: "oldhash", Role: "user", Active: true},
	)
	usersMutex.Lock()
	saveUsers()
	usersMutex.Unlock()

	accessReset, refreshReset, _ := createTokens("reset", "u-reset", false)
	accessKeep, _, _ := createTokens("keep", "u-keep", false)

	body := `{"email":"reset@example.com","token":"` + resetToken + `","newPassword":"newpassword123"}`
	req := httptest.NewRequest("POST", "/api/auth/reset-password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	resetPasswordHandler(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if validateToken(accessReset) != nil {
		t.Error("access token of user with reset password should be revoked")
	}
	if _, _, ok := refreshAccessToken(refreshReset); ok {
		t.Error("refresh token of user with reset password should be revoked")
	}
	if validateToken(accessKeep) == nil {
		t.Error("other user's token should remain valid")
	}
}
