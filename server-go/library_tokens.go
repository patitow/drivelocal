package main

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"
)

// Tokens de API de longa duração para o drivelocal-cli e automações.
// Formato: "dlt_" + 40 hex. Só o hash sha256 fica no banco.
const apiTokenPrefix = "dlt_"

type APIToken struct {
	ID        int64  `json:"id"`
	UserID    string `json:"userId"`
	Username  string `json:"username,omitempty"`
	Name      string `json:"name"`
	Prefix    string `json:"prefix"`
	CreatedAt int64  `json:"createdAt"`
	LastUsed  *int64 `json:"lastUsed,omitempty"`
	Revoked   bool   `json:"revoked"`
}

func (l *Library) CreateAPIToken(ctx context.Context, userID, name string) (string, *APIToken, error) {
	name = strings.TrimSpace(name)
	if name == "" || getUserByID(userID) == nil {
		return "", nil, errInvalid
	}
	raw := apiTokenPrefix + generateToken(20)
	tok := &APIToken{UserID: userID, Name: name, Prefix: raw[:10], CreatedAt: nowUnix()}
	err := l.tx(ctx, func(t *sql.Tx) error {
		res, err := t.Exec(`INSERT INTO api_tokens(user_id, name, token_hash, prefix, created_at) VALUES (?,?,?,?,?)`,
			userID, name, hashToken(raw), tok.Prefix, tok.CreatedAt)
		if err != nil {
			return err
		}
		tok.ID, _ = res.LastInsertId()
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return raw, tok, nil
}

func (l *Library) ListAPITokens(ctx context.Context) ([]APIToken, error) {
	rows, err := l.r.QueryContext(ctx, `SELECT id, user_id, name, prefix, created_at, last_used, revoked FROM api_tokens ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		var t APIToken
		var lu sql.NullInt64
		var rv int
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &t.CreatedAt, &lu, &rv); err != nil {
			return nil, err
		}
		if lu.Valid {
			t.LastUsed = &lu.Int64
		}
		t.Revoked = rv == 1
		if u := getUserByID(t.UserID); u != nil {
			t.Username = u.Username
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (l *Library) RevokeAPIToken(ctx context.Context, id int64) error {
	return l.tx(ctx, func(t *sql.Tx) error {
		res, err := t.Exec(`UPDATE api_tokens SET revoked=1 WHERE id=?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNotFound
		}
		apiTokenCache.Range(func(k, _ any) bool { apiTokenCache.Delete(k); return true })
		return nil
	})
}

type cachedAPIToken struct {
	data  *TokenData
	until time.Time
}

var apiTokenCache sync.Map // hash -> cachedAPIToken

// validateAPIToken é chamado pelo authMiddleware quando o token tem o prefixo dlt_.
func validateAPIToken(raw string) *TokenData {
	l := getLibrary()
	if l == nil || !strings.HasPrefix(raw, apiTokenPrefix) {
		return nil
	}
	h := hashToken(raw)
	if c, ok := apiTokenCache.Load(h); ok {
		ct := c.(cachedAPIToken)
		if time.Now().Before(ct.until) {
			return ct.data
		}
		apiTokenCache.Delete(h)
	}
	var id int64
	var userID string
	if err := l.r.QueryRow(`SELECT id, user_id FROM api_tokens WHERE token_hash=? AND revoked=0`, h).Scan(&id, &userID); err != nil {
		return nil
	}
	u := getUserByID(userID)
	if u == nil || !u.Active {
		return nil
	}
	go func() {
		l.w.Exec(`UPDATE api_tokens SET last_used=? WHERE id=?`, nowUnix(), id)
	}()
	td := &TokenData{UserID: u.ID, Username: u.Username, ExpiresAt: time.Now().Add(time.Hour).UnixMilli()}
	apiTokenCache.Store(h, cachedAPIToken{data: td, until: time.Now().Add(time.Minute)})
	return td
}
