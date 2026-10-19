package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SESSIONS_FILE guarda as sessões em disco para que um restart do servidor
// não derrube os logins. Só são gravados hashes SHA-256 dos tokens, nunca os tokens crus.
var SESSIONS_FILE = "server/data/sessions.json"

const sessionsDebounce = 2 * time.Second

var (
	// bootID muda a cada inicialização do processo; o cliente o usa para saber se o servidor reiniciou.
	bootID = generateToken(16)

	sessionsMu     sync.Mutex // protege sessionsTimer
	sessionsTimer  *time.Timer
	sessionsFileMu sync.Mutex // serializa a escrita em disco
)

type sessionsFile struct {
	Version       int                   `json:"version"`
	Tokens        map[string]*TokenData `json:"tokens"`
	RefreshTokens map[string]*TokenData `json:"refreshTokens"`
}

// hashToken devolve a chave usada nos mapas de tokens e no arquivo de sessões.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// markSessionsDirty agenda uma gravação em disco (no máximo uma a cada 2s).
// Só usa sessionsMu, então pode ser chamado com tokensMutex travado.
func markSessionsDirty() {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	if sessionsTimer != nil {
		return
	}
	sessionsTimer = time.AfterFunc(sessionsDebounce, func() {
		sessionsMu.Lock()
		sessionsTimer = nil
		sessionsMu.Unlock()
		if err := flushSessions(); err != nil {
			logError("Falha ao gravar sessões: %v", err)
		}
	})
}

func stopPendingFlush() {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	if sessionsTimer != nil {
		sessionsTimer.Stop()
		sessionsTimer = nil
	}
}

// flushSessions grava imediatamente todas as sessões (escrita atômica: tmp + rename).
func flushSessions() error {
	stopPendingFlush()

	snap := sessionsFile{
		Version:       1,
		Tokens:        make(map[string]*TokenData),
		RefreshTokens: make(map[string]*TokenData),
	}
	tokensMutex.RLock()
	for k, v := range tokens {
		c := *v
		snap.Tokens[k] = &c
	}
	for k, v := range refreshTokens {
		c := *v
		snap.RefreshTokens[k] = &c
	}
	tokensMutex.RUnlock()

	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}

	sessionsFileMu.Lock()
	defer sessionsFileMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(SESSIONS_FILE), 0700); err != nil {
		return err
	}
	tmp := SESSIONS_FILE + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, SESSIONS_FILE)
}

// loadSessions restaura as sessões do disco. Descarta expiradas e de usuários inativos
// ou inexistentes. Deve rodar depois de loadUsers().
func loadSessions() {
	data, err := os.ReadFile(SESSIONS_FILE)
	if err != nil {
		if !os.IsNotExist(err) {
			logWarn("Não foi possível ler %s: %v", SESSIONS_FILE, err)
		}
		return
	}

	var f sessionsFile
	if err := json.Unmarshal(data, &f); err != nil {
		logError("Arquivo de sessões inválido, iniciando sem sessões: %v", err)
		return
	}

	// Filtra antes de pegar tokensMutex (getUserByID usa usersMutex).
	now := time.Now().UnixMilli()
	keep := func(src map[string]*TokenData) map[string]*TokenData {
		dst := make(map[string]*TokenData, len(src))
		for k, v := range src {
			if v == nil || now > v.ExpiresAt || getUserByID(v.UserID) == nil {
				continue
			}
			dst[k] = v
		}
		return dst
	}
	loadedTokens := keep(f.Tokens)
	loadedRefresh := keep(f.RefreshTokens)

	tokensMutex.Lock()
	tokens = loadedTokens
	refreshTokens = loadedRefresh
	tokensMutex.Unlock()

	logInfo("Sessões restauradas: %d tokens, %d refresh tokens", len(loadedTokens), len(loadedRefresh))
}

// revokeUserTokens invalida todos os tokens de um usuário (exclusão ou desativação).
func revokeUserTokens(userID string) {
	removed := 0
	tokensMutex.Lock()
	for k, v := range tokens {
		if v.UserID == userID {
			delete(tokens, k)
			removed++
		}
	}
	for k, v := range refreshTokens {
		if v.UserID == userID {
			delete(refreshTokens, k)
			removed++
		}
	}
	tokensMutex.Unlock()

	if removed > 0 {
		markSessionsDirty()
	}
}
