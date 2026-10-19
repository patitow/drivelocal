package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FolderPolicy define a maturidade padrão e a ocultação de uma subárvore.
// O prefixo mais longo que casa vence; Maturity nil herda do prefixo pai.
type FolderPolicy struct {
	Prefix    string `json:"prefix"`
	Maturity  *int   `json:"-"`
	Hidden    bool   `json:"hidden"`
	Note      string `json:"note,omitempty"`
	UpdatedAt int64  `json:"updatedAt"`
}

type folderPolicyJSON struct {
	Prefix   string  `json:"prefix"`
	Maturity *string `json:"maturity"`
	Hidden   bool    `json:"hidden"`
	Note     string  `json:"note,omitempty"`
}

func (p FolderPolicy) toJSON() folderPolicyJSON {
	out := folderPolicyJSON{Prefix: p.Prefix, Hidden: p.Hidden, Note: p.Note}
	if p.Maturity != nil {
		n := maturityName(*p.Maturity)
		out.Maturity = &n
	}
	return out
}

func normalizePolicyPrefix(p string) string {
	p = normalizeRelativeMediaPath(p)
	return strings.TrimSuffix(p, "/")
}

func (l *Library) reloadPolicies() error {
	rows, err := l.r.Query(`SELECT prefix, maturity, hidden, COALESCE(note,''), updated_at FROM folder_policies`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var out []FolderPolicy
	for rows.Next() {
		var p FolderPolicy
		var m sql.NullInt64
		var hidden int
		if err := rows.Scan(&p.Prefix, &m, &hidden, &p.Note, &p.UpdatedAt); err != nil {
			return err
		}
		if m.Valid {
			p.Maturity = intPtr(int(m.Int64))
		}
		p.Hidden = hidden == 1
		out = append(out, p)
	}
	// Mais longos primeiro: o primeiro que casa é o mais específico.
	sort.Slice(out, func(i, j int) bool { return len(out[i].Prefix) > len(out[j].Prefix) })
	l.policyMu.Lock()
	l.policies = out
	l.policyMu.Unlock()
	return rows.Err()
}

func pathHasPrefix(path, prefix string) bool {
	if prefix == "" {
		return true
	}
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// policyFor devolve a maturidade herdada (0 se nenhuma) e se a subárvore é oculta.
// Ocultação vale se qualquer ancestral for oculto.
func (l *Library) policyFor(path string) (maturity int, hidden bool) {
	l.policyMu.RLock()
	defer l.policyMu.RUnlock()
	matSet := false
	lp := strings.ToLower(path)
	for _, p := range l.policies {
		if !pathHasPrefix(lp, strings.ToLower(p.Prefix)) {
			continue
		}
		if p.Hidden {
			hidden = true
		}
		if !matSet && p.Maturity != nil {
			maturity = *p.Maturity
			matSet = true
		}
	}
	return maturity, hidden
}

func (l *Library) hasHiddenPolicies() bool {
	l.policyMu.RLock()
	defer l.policyMu.RUnlock()
	for _, p := range l.policies {
		if p.Hidden {
			return true
		}
	}
	return false
}

func (l *Library) listPolicies() []FolderPolicy {
	l.policyMu.RLock()
	defer l.policyMu.RUnlock()
	out := make([]FolderPolicy, len(l.policies))
	copy(out, l.policies)
	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	return out
}

// SetPolicy grava (ou remove, se maturity nil e !hidden) a política e
// recalcula a maturidade dos itens afetados.
func (l *Library) SetPolicy(ctx context.Context, prefix string, maturity *int, hidden bool, note string) error {
	prefix = normalizePolicyPrefix(prefix)
	err := l.tx(ctx, func(t *sql.Tx) error {
		if maturity == nil && !hidden {
			_, err := t.Exec(`DELETE FROM folder_policies WHERE prefix = ?`, prefix)
			return err
		}
		_, err := t.Exec(`INSERT INTO folder_policies(prefix, maturity, hidden, note, updated_at) VALUES (?,?,?,?,?)
			ON CONFLICT(prefix) DO UPDATE SET maturity=excluded.maturity, hidden=excluded.hidden, note=excluded.note, updated_at=excluded.updated_at`,
			prefix, maturity, boolInt(hidden), note, nowUnix())
		return err
	})
	if err != nil {
		return err
	}
	if err := l.reloadPolicies(); err != nil {
		return err
	}
	return l.applyPolicies(ctx, prefix)
}

// applyPolicies recalcula maturity_policy/policy_hidden (e a maturidade
// efetiva) para os itens sob prefix ("" = todos).
func (l *Library) applyPolicies(ctx context.Context, prefix string) error {
	type row struct {
		id   int64
		path string
	}
	var rowsList []row
	q := `SELECT id, path FROM media`
	args := []any{}
	if prefix != "" {
		q += ` WHERE path = ? OR path LIKE ? ESCAPE '\'`
		args = append(args, prefix, likePrefix(prefix)+"/%")
	}
	rows, err := l.r.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var rw row
		if err := rows.Scan(&rw.id, &rw.path); err != nil {
			rows.Close()
			return err
		}
		rowsList = append(rowsList, rw)
	}
	rows.Close()
	return l.tx(ctx, func(t *sql.Tx) error {
		st, err := t.Prepare(`UPDATE media SET maturity_policy=?, policy_hidden=? WHERE id=?`)
		if err != nil {
			return err
		}
		defer st.Close()
		for _, rw := range rowsList {
			m, h := l.policyFor(rw.path)
			if _, err := st.Exec(m, boolInt(h), rw.id); err != nil {
				return err
			}
		}
		return recomputeMaturity(t, "")
	})
}

// recomputeMaturity materializa a maturidade efetiva: manual > max(política, regra).
func recomputeMaturity(t *sql.Tx, where string, args ...any) error {
	q := `UPDATE media SET maturity = COALESCE(maturity_manual, MAX(maturity_policy, COALESCE(maturity_rule, 0)))`
	if where != "" {
		q += " WHERE " + where
	}
	_, err := t.Exec(q, args...)
	return err
}

// likePrefix escapa % e _ para uso em LIKE ... ESCAPE '\'.
func likePrefix(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ---------- quem está vendo ----------

type Viewer struct {
	UserID      string
	IsAdmin     bool
	MaxMaturity int
}

// userMaturity devolve o limite dado pelo admin e se o modo restrito está ligado.
func (l *Library) userMaturity(userID string) (int, bool) {
	var m, restricted int
	err := l.r.QueryRow(`SELECT max_maturity, restricted_mode FROM user_prefs WHERE user_id = ?`, userID).Scan(&m, &restricted)
	if errors.Is(err, sql.ErrNoRows) {
		return MaturityGeral, true
	}
	if err != nil {
		return MaturityGeral, true
	}
	return m, restricted == 1
}

func (l *Library) viewerFor(userID string) Viewer {
	v := Viewer{UserID: userID}
	allowed, restricted := l.userMaturity(userID)
	if u := getUserByID(userID); u != nil && u.Role == "admin" {
		v.IsAdmin = true
		allowed = MaturityExplicito
	}
	v.MaxMaturity = allowed
	// modo restrito desligado: explícito some, mesmo para o admin
	if !restricted && v.MaxMaturity > MaturitySugestivo {
		v.MaxMaturity = MaturitySugestivo
	}
	return v
}

func (l *Library) viewer(r *http.Request) Viewer {
	return l.viewerFor(r.Header.Get("X-User-ID"))
}

// visibleSQL devolve o filtro de visibilidade para a tabela media (alias m).
func (v Viewer) visibleSQL(includeUserHidden bool) (string, []any) {
	cond := "m.missing = 0 AND m.dup_of IS NULL AND m.maturity <= ?"
	args := []any{v.MaxMaturity}
	if !v.IsAdmin {
		cond += " AND m.policy_hidden = 0"
	}
	if !includeUserHidden {
		cond += " AND NOT EXISTS (SELECT 1 FROM user_hidden uh WHERE uh.user_id = ? AND uh.media_id = m.id)"
		args = append(args, v.UserID)
	}
	return cond, args
}

// canSeePath decide o acesso a um caminho (arquivo ou pasta) pelo navegador
// de pastas e pelas rotas de mídia. raw é o caminho já decodificado da URL,
// exatamente como os handlers o recebem. Fail-closed: caminho que não dá para
// canonicalizar é negado.
func (l *Library) canSeePath(v Viewer, raw string) bool {
	_, ok := l.resolveVisible(v, raw)
	return ok
}

// resolveVisible canonicaliza raw como os handlers de mídia fazem
// (resolveMediaFile) e devolve o caminho relativo canônico se o usuário pode vê-lo.
func (l *Library) resolveVisible(v Viewer, raw string) (string, bool) {
	cands, ok := canonicalMediaRel(raw)
	if !ok {
		return "", false
	}
	if v.IsAdmin {
		return cands[0], true
	}
	for _, rel := range cands {
		if !l.canSeeCanonical(v, rel) {
			return "", false
		}
	}
	return cands[0], true
}

func (l *Library) canSeeCanonical(v Viewer, rel string) bool {
	if rel == "" {
		return true
	}
	pm, hidden := l.policyFor(rel)
	if hidden {
		return false
	}
	// COLLATE NOCASE: o disco pode ser case-insensitive (NTFS/drvfs).
	var m sql.NullInt64
	var n, uh int
	err := l.r.QueryRow(`SELECT MAX(maturity), COUNT(*), MAX(policy_hidden) FROM media WHERE path = ? COLLATE NOCASE`, rel).Scan(&m, &n, &sql.NullInt64{})
	if err != nil {
		return false
	}
	if n > 0 {
		if m.Int64 > int64(v.MaxMaturity) {
			return false
		}
		if err := l.r.QueryRow(`SELECT COUNT(*) FROM user_hidden uh JOIN media mm ON mm.id = uh.media_id
			WHERE uh.user_id = ? AND mm.path = ? COLLATE NOCASE`, v.UserID, rel).Scan(&uh); err != nil || uh > 0 {
			return false
		}
		return true
	}
	return pm <= v.MaxMaturity
}

// dirHasBlocked diz se a pasta rel (canônica) contém algo que o usuário não
// pode ver: item catalogado acima da maturidade, oculto por política ou
// escondido por ele, ou subpasta com política oculta/mais restrita (cobre o
// que ainda não foi escaneado). Fail-closed: erro de consulta conta como bloqueado.
func (l *Library) dirHasBlocked(v Viewer, rel string) bool {
	if v.IsAdmin {
		return false
	}
	lrel := strings.ToLower(rel)
	l.policyMu.RLock()
	for _, p := range l.policies {
		lp := strings.ToLower(p.Prefix)
		if rel != "" && !strings.HasPrefix(lp, lrel+"/") {
			continue
		}
		if p.Hidden || (p.Maturity != nil && *p.Maturity > v.MaxMaturity) {
			l.policyMu.RUnlock()
			return true
		}
	}
	l.policyMu.RUnlock()
	like := "%"
	if rel != "" {
		like = likePrefix(rel) + "/%"
	}
	var n int
	err := l.r.QueryRow(`SELECT COUNT(*) FROM (SELECT 1 FROM media m
		WHERE m.missing = 0 AND m.path LIKE ? ESCAPE '\' COLLATE NOCASE
		AND (m.maturity > ? OR m.policy_hidden = 1
			OR EXISTS (SELECT 1 FROM user_hidden uh WHERE uh.user_id = ? AND uh.media_id = m.id))
		LIMIT 1)`, like, v.MaxMaturity, v.UserID).Scan(&n)
	return err != nil || n > 0
}

// ---------- canonicalização de caminhos ----------

type canonEntry struct {
	cands []string
	until time.Time
}

var canonCache sync.Map // raw -> canonEntry

// canonicalMediaRel replica resolveMediaFile (Join + Clean dentro de
// MEDIA_PATH, filepath.Rel, ToSlash) e rejeita formas ambíguas: segmentos
// vazios, "." e "..", barra invertida e NUL. Também resolve a caixa real das
// pastas no disco e, se houver symlink, inclui o destino como 2º candidato
// (os dois precisam ser visíveis).
func canonicalMediaRel(raw string) ([]string, bool) {
	// "\" e ":" (ADS/drive no Windows) não existem em nomes válidos do acervo.
	if strings.ContainsAny(raw, "\\:\x00") {
		return nil, false
	}
	s := strings.TrimPrefix(raw, "/")
	s = strings.TrimSuffix(s, "/")
	if s == "" {
		return []string{""}, true
	}
	for _, seg := range strings.Split(s, "/") {
		// Win32 ignora "." e espaço no fim do nome ("Pasta." == "Pasta"): nega.
		if seg == "" || seg == "." || seg == ".." || strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") {
			return nil, false
		}
	}
	if c, ok := canonCache.Load(s); ok {
		ce := c.(canonEntry)
		if time.Now().Before(ce.until) {
			return ce.cands, true
		}
		canonCache.Delete(s)
	}
	root := filepath.Clean(MEDIA_PATH)
	full := filepath.Clean(filepath.Join(root, filepath.FromSlash(s)))
	if full == root {
		return []string{""}, true
	}
	if !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return nil, false
	}
	relOS, err := filepath.Rel(root, full)
	if err != nil {
		return nil, false
	}
	rel := trueCaseRel(root, filepath.ToSlash(relOS))
	cands := []string{rel}
	if real, err := filepath.EvalSymlinks(full); err == nil {
		if rootReal, err := filepath.EvalSymlinks(root); err == nil {
			if r2, err := filepath.Rel(rootReal, real); err == nil && !strings.HasPrefix(r2, "..") && r2 != "." {
				r2 = trueCaseRel(root, filepath.ToSlash(r2))
				if !strings.EqualFold(r2, rel) {
					cands = append(cands, r2)
				}
			}
		}
	}
	canonCache.Store(s, canonEntry{cands: cands, until: time.Now().Add(30 * time.Second)})
	return cands, true
}

// trueCaseRel troca cada segmento pelo nome real no disco (match exato
// primeiro, depois case-insensitive). Segmentos inexistentes ficam como estão.
func trueCaseRel(root, rel string) string {
	segs := strings.Split(rel, "/")
	cur := root
	for i, seg := range segs {
		entries, err := os.ReadDir(cur)
		if err != nil {
			break
		}
		found := ""
		for _, e := range entries {
			if e.Name() == seg {
				found = seg
				break
			}
			if found == "" && strings.EqualFold(e.Name(), seg) {
				found = e.Name()
			}
		}
		if found == "" {
			break
		}
		segs[i] = found
		cur = filepath.Join(cur, found)
	}
	return strings.Join(segs, "/")
}
