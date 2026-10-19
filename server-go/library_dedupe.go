package main

// Detecção de tópicos duplicados (mesma série/criador com nomes diferentes)
// e o fluxo de fusão revisável: sugestões agrupadas, aceitar = fundir no
// canônico (o nome antigo vira alias e redirect), com desfazer.
//
// A similaridade é só de texto (normalização + token set ratio + Jaro-Winkler
// + contenção). Embeddings foram medidos e não entraram: ver
// docs/superpowers/specs/2026-10-17-organizacao-biblioteca-design.md (D13).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Limiares medidos sobre os tópicos reais (pares rotulados à mão, fora do git):
// topicDupReview manda para revisão; acima de topicDupAuto a fusão seria
// automática, mas isso só vale se a medição der 0 falso-positivo (D13).
const (
	topicDupReview = 0.86
	topicDupAuto   = 2.0 // desligado: nunca funde sozinho (ver D13)
)

var (
	reTopicDate   = regexp.MustCompile(`\b\d{4}[-_.]\d{1,2}[-_.]\d{1,2}\b`)
	reTopicParens = regexp.MustCompile(`[(\[{][^)\]}]*[)\]}]`)
	reTopicPartNo = regexp.MustCompile(`^(s|pt|part|parte|vol|ep|e|cap)\d{1,3}$`)
	leetFold      = strings.NewReplacer("0", "o", "1", "i", "3", "e", "4", "a", "5", "s", "7", "t")
)

// Palavras que não identificam a obra/pessoa: partes, formatos, sites.
var topicNoise = map[string]bool{
	"the": true, "animation": true, "part": true, "pt": true, "parte": true, "vol": true, "volume": true,
	"season": true, "temporada": true, "ova": true, "oad": true, "especial": true, "special": true, "specials": true,
	"misc": true, "compilation": true, "ofc": true, "official": true, "oficial": true, "and": true, "e": true,
	"gofile": true, "vostfr": true, "legendado": true, "dublado": true, "dual": true, "audio": true,
}

// topicKey é o nome normalizado: tokens significativos, a forma compacta
// (sem espaços) e os números longos (códigos tipo ABC-123, que nunca podem
// divergir entre duplicatas).
type topicKey struct {
	Tokens  []string
	Compact string
	Codes   map[string]bool
}

func normalizeTopicName(s string) topicKey {
	s = norm.NFKC.String(s) // letras "estilizadas" (𝔖, 𝓢, ...) viram ASCII
	s = strings.ToLower(foldDiacritics(s))
	s = reTopicDate.ReplaceAllString(s, " ")
	s = reTopicParens.ReplaceAllString(s, " ")
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	k := topicKey{Codes: map[string]bool{}}
	for _, t := range strings.Fields(b.String()) {
		allDigits := strings.IndexFunc(t, func(r rune) bool { return !unicode.IsDigit(r) }) < 0
		switch {
		case allDigits && len(t) >= 3:
			k.Codes[t] = true
			k.Tokens = append(k.Tokens, t)
			continue
		case allDigits, topicNoise[t], reTopicPartNo.MatchString(t):
			continue
		}
		if strings.IndexFunc(t, unicode.IsDigit) >= 0 && strings.IndexFunc(t, unicode.IsLetter) >= 0 {
			t = leetFold.Replace(t) // n3r0k0 -> neroko
		}
		k.Tokens = append(k.Tokens, t)
	}
	k.Compact = strings.Join(k.Tokens, "")
	return k
}

// topicNameScore: 0..1, quanto os dois nomes parecem a mesma coisa.
func topicNameScore(a, b string) float64 {
	return topicKeyScore(normalizeTopicName(a), normalizeTopicName(b))
}

func topicKeyScore(ka, kb topicKey) float64 {
	if len(ka.Tokens) == 0 || len(kb.Tokens) == 0 {
		return 0
	}
	// Códigos numéricos diferentes = obras diferentes (ABC-123 x ABC-124).
	if len(ka.Codes) > 0 || len(kb.Codes) > 0 {
		if len(ka.Codes) != len(kb.Codes) {
			return 0
		}
		for c := range ka.Codes {
			if !kb.Codes[c] {
				return 0
			}
		}
	}
	if ka.Compact == kb.Compact {
		return 1
	}
	s := tokenSetRatio(ka.Tokens, kb.Tokens)
	// Jaro-Winkler só quando um lado é uma palavra só: entre nomes compostos
	// o prefixo comum ("serie alfa" x "serie beta") gerava falso-positivo.
	if len(ka.Compact) >= 4 && len(kb.Compact) >= 4 && (len(ka.Tokens) == 1 || len(kb.Tokens) == 1) {
		s = max(s, jaroWinkler(ka.Compact, kb.Compact))
	}
	if tokensContained(ka, kb) || tokensContained(kb, ka) {
		s = max(s, 0.9)
	}
	return s
}

// tokensContained: todos os tokens de a aparecem (iguais ou quase) em b, e a
// tem conteúdo suficiente para não casar por acaso.
func tokensContained(a, b topicKey) bool {
	if len([]rune(a.Compact)) < 5 || len(a.Tokens) > len(b.Tokens) {
		return false
	}
	for _, t := range a.Tokens {
		found := false
		for _, u := range b.Tokens {
			if t == u || (len(t) >= 5 && jaroWinkler(t, u) >= 0.94) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// tokenSetRatio no estilo fuzzywuzzy: compara a interseção ordenada com
// cada lado, o que ignora ordem e tokens repetidos.
func tokenSetRatio(a, b []string) float64 {
	sa, sb := map[string]bool{}, map[string]bool{}
	for _, t := range a {
		sa[t] = true
	}
	for _, t := range b {
		sb[t] = true
	}
	var inter, onlyA, onlyB []string
	for t := range sa {
		if sb[t] {
			inter = append(inter, t)
		} else {
			onlyA = append(onlyA, t)
		}
	}
	for t := range sb {
		if !sa[t] {
			onlyB = append(onlyB, t)
		}
	}
	sort.Strings(inter)
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	t0 := strings.Join(inter, " ")
	t1 := strings.TrimSpace(t0 + " " + strings.Join(onlyA, " "))
	t2 := strings.TrimSpace(t0 + " " + strings.Join(onlyB, " "))
	r := levRatio(t1, t2)
	if t0 != "" {
		r = max(r, levRatio(t0, t1), levRatio(t0, t2))
	}
	return r
}

func levRatio(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra)+len(rb) == 0 {
		return 1
	}
	return 1 - float64(levenshtein(ra, rb))/float64(max(len(ra), len(rb)))
}

func levenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func jaroWinkler(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 || len(rb) == 0 {
		return 0
	}
	win := max(len(ra), len(rb))/2 - 1
	if win < 0 {
		win = 0
	}
	ma, mb := make([]bool, len(ra)), make([]bool, len(rb))
	m := 0
	for i := range ra {
		lo, hi := max(0, i-win), min(len(rb), i+win+1)
		for j := lo; j < hi; j++ {
			if !mb[j] && ra[i] == rb[j] {
				ma[i], mb[j] = true, true
				m++
				break
			}
		}
	}
	if m == 0 {
		return 0
	}
	t, k := 0, 0
	for i := range ra {
		if !ma[i] {
			continue
		}
		for !mb[k] {
			k++
		}
		if ra[i] != rb[k] {
			t++
		}
		k++
	}
	fm := float64(m)
	jaro := (fm/float64(len(ra)) + fm/float64(len(rb)) + (fm-float64(t)/2)/fm) / 3
	p := 0
	for p < min(4, len(ra), len(rb)) && ra[p] == rb[p] {
		p++
	}
	return jaro + float64(p)*0.1*(1-jaro)
}

// ---------- detecção e revisão ----------

type dupTopic struct {
	ID    int64
	Kind  string
	Title string
	Count int
	Key   topicKey
}

func (l *Library) loadDupTopics(ctx context.Context) ([]dupTopic, error) {
	rows, err := l.r.QueryContext(ctx, `SELECT tp.id, tp.kind, tp.title,
		(SELECT COUNT(*) FROM topic_items ti JOIN media m ON m.id = ti.media_id WHERE ti.topic_id = tp.id AND m.missing = 0)
		FROM topics tp`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dupTopic
	for rows.Next() {
		var t dupTopic
		if err := rows.Scan(&t.ID, &t.Kind, &t.Title, &t.Count); err != nil {
			return nil, err
		}
		if t.Count == 0 {
			continue
		}
		t.Key = normalizeTopicName(t.Title)
		out = append(out, t)
	}
	return out, rows.Err()
}

// canonicalFirst: o tópico com mais itens fica (empate: título mais curto).
func canonicalFirst(a, b dupTopic) (dupTopic, dupTopic) {
	if b.Count > a.Count || (b.Count == a.Count && len(b.Title) < len(a.Title)) {
		return b, a
	}
	return a, b
}

type TopicDupReport struct {
	Topics    int `json:"topics"`
	Pairs     int `json:"pairs"`
	Suggested int `json:"suggested"`
}

// DetectTopicDuplicates compara os tópicos do mesmo tipo e grava os pares
// acima do limiar como sugestões pendentes (pares já rejeitados ficam).
func (l *Library) DetectTopicDuplicates(ctx context.Context) (*TopicDupReport, error) {
	topics, err := l.loadDupTopics(ctx)
	if err != nil {
		return nil, err
	}
	rep := &TopicDupReport{Topics: len(topics)}
	type pair struct {
		src, dst int64
		score    float64
	}
	var found []pair
	for i := range topics {
		for j := i + 1; j < len(topics); j++ {
			a, b := topics[i], topics[j]
			if a.Kind != b.Kind {
				continue
			}
			rep.Pairs++
			s := topicKeyScore(a.Key, b.Key)
			if s < topicDupReview {
				continue
			}
			dst, src := canonicalFirst(a, b)
			found = append(found, pair{src.ID, dst.ID, s})
		}
	}
	err = l.tx(ctx, func(t *sql.Tx) error {
		for _, p := range found {
			var n int
			// já existe (em qualquer direção e status): não repete
			t.QueryRow(`SELECT COUNT(*) FROM topic_merge_suggestions WHERE (src_id=? AND dst_id=?) OR (src_id=? AND dst_id=?)`, p.src, p.dst, p.dst, p.src).Scan(&n)
			if n > 0 {
				continue
			}
			if _, err := t.Exec(`INSERT INTO topic_merge_suggestions(src_id, dst_id, score, method, created_at) VALUES (?,?,?,?,?)`, p.src, p.dst, p.score, "string", nowUnix()); err != nil {
				return err
			}
			rep.Suggested++
		}
		return nil
	})
	return rep, err
}

type TopicDupMember struct {
	ID       int64    `json:"id"`
	Title    string   `json:"title"`
	Count    int      `json:"count"`
	Score    float64  `json:"score"`
	Examples []string `json:"examples"`
}

type TopicDupGroup struct {
	Kind      string           `json:"kind"`
	Canonical TopicDupMember   `json:"canonical"`
	Members   []TopicDupMember `json:"members"`
	Score     float64          `json:"score"`
}

// TopicDupGroups junta as sugestões pendentes em grupos (componentes
// conexos); o canônico é o tópico com mais itens.
func (l *Library) TopicDupGroups(ctx context.Context) ([]TopicDupGroup, error) {
	rows, err := l.r.QueryContext(ctx, `SELECT s.src_id, s.dst_id, s.score FROM topic_merge_suggestions s WHERE s.status = 'pending'`)
	if err != nil {
		return nil, err
	}
	parent := map[int64]int64{}
	var find func(int64) int64
	find = func(x int64) int64 {
		if p, ok := parent[x]; ok && p != x {
			r := find(p)
			parent[x] = r
			return r
		}
		parent[x] = x
		return x
	}
	best := map[int64]float64{}
	for rows.Next() {
		var a, b int64
		var s float64
		if err := rows.Scan(&a, &b, &s); err != nil {
			rows.Close()
			return nil, err
		}
		parent[find(a)] = find(b)
		best[a] = max(best[a], s)
		best[b] = max(best[b], s)
	}
	rows.Close()
	comps := map[int64][]int64{}
	for id := range parent {
		r := find(id)
		comps[r] = append(comps[r], id)
	}
	out := []TopicDupGroup{}
	for _, ids := range comps {
		var members []TopicDupMember
		kind := ""
		for _, id := range ids {
			m, k, err := l.dupMember(ctx, id)
			if err != nil {
				continue
			}
			kind = k
			m.Score = best[id]
			members = append(members, m)
		}
		if len(members) < 2 {
			continue
		}
		sort.Slice(members, func(i, j int) bool {
			if members[i].Count != members[j].Count {
				return members[i].Count > members[j].Count
			}
			return len(members[i].Title) < len(members[j].Title)
		})
		g := TopicDupGroup{Kind: kind, Canonical: members[0], Members: members[1:]}
		for _, m := range g.Members {
			g.Score = max(g.Score, m.Score)
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Canonical.ID < out[j].Canonical.ID
	})
	return out, nil
}

func (l *Library) dupMember(ctx context.Context, id int64) (TopicDupMember, string, error) {
	m := TopicDupMember{ID: id, Examples: []string{}}
	var kind string
	if err := l.r.QueryRowContext(ctx, `SELECT title, kind, (SELECT COUNT(*) FROM topic_items WHERE topic_id = topics.id) FROM topics WHERE id = ?`, id).Scan(&m.Title, &kind, &m.Count); err != nil {
		return m, "", err
	}
	rows, err := l.r.QueryContext(ctx, `SELECT COALESCE(m.title_manual, m.title) FROM topic_items ti JOIN media m ON m.id = ti.media_id WHERE ti.topic_id = ? ORDER BY ti.sort_key LIMIT 3`, id)
	if err == nil {
		for rows.Next() {
			var s string
			rows.Scan(&s)
			m.Examples = append(m.Examples, s)
		}
		rows.Close()
	}
	return m, kind, nil
}

// mergeSnapshot guarda o que a fusão apaga ou mexe, para o desfazer.
type mergeSnapshot struct {
	Kind        string      `json:"kind"`
	Slug        string      `json:"slug"`
	Title       string      `json:"title"`
	Description *string     `json:"description,omitempty"`
	Source      string      `json:"source"`
	Meta        *string     `json:"meta,omitempty"`
	CreatedAt   int64       `json:"createdAt"`
	Aliases     []string    `json:"aliases"`
	Items       []snapItem  `json:"items"`
	NewInDst    []int64     `json:"newInDst"`   // itens que só entraram no destino por causa da fusão
	Favorites   []snapFav   `json:"favorites"`  // favorite_topics do tópico antigo
	NewFavDst   []string    `json:"newFavDst"`  // usuários que só ganharam o favorito no destino
	NewAliases  []string    `json:"newAliases"` // aliases que a fusão criou no destino
	Redirects   [][2]string `json:"redirects"`  // (kind, slug) que apontavam para o antigo
}

type snapItem struct {
	MediaID int64  `json:"m"`
	SortKey string `json:"k"`
	Source  string `json:"s"`
}

type snapFav struct {
	UserID    string `json:"u"`
	CreatedAt int64  `json:"t"`
}

// MergeTopicsRecorded funde src em dst como MergeTopics, mas registra um
// snapshot no lote batch para permitir desfazer.
func (l *Library) mergeTopicsRecorded(t *sql.Tx, srcID, dstID int64, batch, actor string) error {
	if srcID == dstID {
		return errInvalid
	}
	var sn mergeSnapshot
	var desc, meta sql.NullString
	if err := t.QueryRow(`SELECT kind, slug, title, description, source, meta_json, created_at FROM topics WHERE id=?`, srcID).
		Scan(&sn.Kind, &sn.Slug, &sn.Title, &desc, &sn.Source, &meta, &sn.CreatedAt); err != nil {
		return errNotFound
	}
	if desc.Valid {
		sn.Description = &desc.String
	}
	if meta.Valid {
		sn.Meta = &meta.String
	}
	var n int
	t.QueryRow(`SELECT COUNT(*) FROM topics WHERE id=?`, dstID).Scan(&n)
	if n == 0 {
		return errNotFound
	}
	q := func(query string, args []any, scan func(*sql.Rows) error) error {
		rows, err := t.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	sn.Aliases, sn.NewAliases, sn.NewFavDst = []string{}, []string{}, []string{}
	if err := q(`SELECT alias FROM topic_aliases WHERE topic_id=?`, []any{srcID}, func(r *sql.Rows) error {
		var a string
		err := r.Scan(&a)
		sn.Aliases = append(sn.Aliases, a)
		return err
	}); err != nil {
		return err
	}
	if err := q(`SELECT media_id, sort_key, source, EXISTS (SELECT 1 FROM topic_items d WHERE d.topic_id=? AND d.media_id=topic_items.media_id) FROM topic_items WHERE topic_id=?`, []any{dstID, srcID}, func(r *sql.Rows) error {
		var it snapItem
		var inDst int
		err := r.Scan(&it.MediaID, &it.SortKey, &it.Source, &inDst)
		sn.Items = append(sn.Items, it)
		if inDst == 0 {
			sn.NewInDst = append(sn.NewInDst, it.MediaID)
		}
		return err
	}); err != nil {
		return err
	}
	if err := q(`SELECT user_id, created_at, EXISTS (SELECT 1 FROM favorite_topics d WHERE d.topic_id=? AND d.user_id=favorite_topics.user_id) FROM favorite_topics WHERE topic_id=?`, []any{dstID, srcID}, func(r *sql.Rows) error {
		var f snapFav
		var inDst int
		err := r.Scan(&f.UserID, &f.CreatedAt, &inDst)
		sn.Favorites = append(sn.Favorites, f)
		if inDst == 0 {
			sn.NewFavDst = append(sn.NewFavDst, f.UserID)
		}
		return err
	}); err != nil {
		return err
	}
	for _, a := range append(append([]string{}, sn.Aliases...), sn.Title) {
		var e int
		t.QueryRow(`SELECT COUNT(*) FROM topic_aliases WHERE topic_id=? AND alias=?`, dstID, a).Scan(&e)
		if e == 0 && !containsStr(sn.NewAliases, a) {
			sn.NewAliases = append(sn.NewAliases, a)
		}
	}
	if err := q(`SELECT kind, slug FROM topic_redirects WHERE topic_id=?`, []any{srcID}, func(r *sql.Rows) error {
		var k, s string
		err := r.Scan(&k, &s)
		sn.Redirects = append(sn.Redirects, [2]string{k, s})
		return err
	}); err != nil {
		return err
	}
	if err := mergeTopicsTx(t, srcID, dstID); err != nil {
		return err
	}
	js, _ := json.Marshal(sn)
	_, err := t.Exec(`INSERT INTO topic_merges(batch_id, src_id, dst_id, snapshot, actor, created_at) VALUES (?,?,?,?,?,?)`, batch, srcID, dstID, string(js), actor, nowUnix())
	return err
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

type TopicMergeResult struct {
	BatchID string `json:"batchId"`
	Merged  int    `json:"merged"`
}

// AcceptTopicMerge funde members no canônico (um lote, desfazível).
func (l *Library) AcceptTopicMerge(ctx context.Context, canonical int64, members []int64, actor string) (*TopicMergeResult, error) {
	res := &TopicMergeResult{BatchID: "m" + generateToken(6)}
	err := l.tx(ctx, func(t *sql.Tx) error {
		for _, src := range members {
			if src == canonical {
				continue
			}
			if err := l.mergeTopicsRecorded(t, src, canonical, res.BatchID, actor); err != nil {
				return fmt.Errorf("tópico %d: %w", src, err)
			}
			res.Merged++
		}
		// as sugestões dos tópicos fundidos somem por cascade
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// RejectTopicMerge marca como rejeitados os pares entre os tópicos dados
// (não voltam a ser sugeridos).
func (l *Library) RejectTopicMerge(ctx context.Context, ids []int64, actor string) (int, error) {
	if len(ids) < 2 {
		return 0, errInvalid
	}
	n := 0
	err := l.tx(ctx, func(t *sql.Tx) error {
		ph := placeholders(len(ids))
		args := append([]any{actor, nowUnix()}, int64sToAny(ids)...)
		args = append(args, int64sToAny(ids)...)
		res, err := t.Exec(`UPDATE topic_merge_suggestions SET status='rejected', reviewed_by=?, reviewed_at=? WHERE status='pending' AND src_id IN (`+ph+`) AND dst_id IN (`+ph+`)`, args...)
		if err != nil {
			return err
		}
		k, _ := res.RowsAffected()
		n = int(k)
		return nil
	})
	return n, err
}

// UndoTopicMerge recria os tópicos fundidos no lote, na ordem inversa.
func (l *Library) UndoTopicMerge(ctx context.Context, batch string) (int, error) {
	n := 0
	err := l.tx(ctx, func(t *sql.Tx) error {
		rows, err := t.Query(`SELECT id, src_id, dst_id, snapshot FROM topic_merges WHERE batch_id=? AND undone=0 ORDER BY id DESC`, batch)
		if err != nil {
			return err
		}
		type rec struct {
			id, src, dst int64
			snap         string
		}
		var list []rec
		for rows.Next() {
			var r rec
			if err := rows.Scan(&r.id, &r.src, &r.dst, &r.snap); err != nil {
				rows.Close()
				return err
			}
			list = append(list, r)
		}
		rows.Close()
		if len(list) == 0 {
			return fmt.Errorf("lote %s: %w (ou já desfeito)", batch, errNotFound)
		}
		var touched []int64
		for _, r := range list {
			var sn mergeSnapshot
			if err := json.Unmarshal([]byte(r.snap), &sn); err != nil {
				return err
			}
			// o (kind, slug) antigo deixa de redirecionar
			if _, err := t.Exec(`DELETE FROM topic_redirects WHERE kind=? AND slug=? AND topic_id=?`, sn.Kind, sn.Slug, r.dst); err != nil {
				return err
			}
			id := r.src
			var used int
			t.QueryRow(`SELECT COUNT(*) FROM topics WHERE id=?`, id).Scan(&used)
			if used > 0 {
				id = 0
			}
			var newID int64
			if id > 0 {
				_, err = t.Exec(`INSERT INTO topics(id, kind, slug, title, description, source, meta_json, created_at) VALUES (?,?,?,?,?,?,?,?)`, id, sn.Kind, sn.Slug, sn.Title, sn.Description, sn.Source, sn.Meta, sn.CreatedAt)
				newID = id
			} else {
				var res sql.Result
				res, err = t.Exec(`INSERT INTO topics(kind, slug, title, description, source, meta_json, created_at) VALUES (?,?,?,?,?,?,?)`, sn.Kind, sn.Slug, sn.Title, sn.Description, sn.Source, sn.Meta, sn.CreatedAt)
				if err == nil {
					newID, _ = res.LastInsertId()
				}
			}
			if err != nil {
				return fmt.Errorf("recriar tópico %q: %w", sn.Title, err)
			}
			for _, a := range sn.Aliases {
				if _, err := t.Exec(`INSERT OR IGNORE INTO topic_aliases(topic_id, alias) VALUES (?,?)`, newID, a); err != nil {
					return err
				}
			}
			for _, a := range sn.NewAliases {
				if _, err := t.Exec(`DELETE FROM topic_aliases WHERE topic_id=? AND alias=?`, r.dst, a); err != nil {
					return err
				}
			}
			for _, it := range sn.Items {
				if _, err := t.Exec(`INSERT OR IGNORE INTO topic_items(topic_id, media_id, sort_key, source) VALUES (?,?,?,?)`, newID, it.MediaID, it.SortKey, it.Source); err != nil {
					return err
				}
				touched = append(touched, it.MediaID)
			}
			for _, m := range sn.NewInDst {
				if _, err := t.Exec(`DELETE FROM topic_items WHERE topic_id=? AND media_id=?`, r.dst, m); err != nil {
					return err
				}
			}
			for _, f := range sn.Favorites {
				if _, err := t.Exec(`INSERT OR IGNORE INTO favorite_topics(user_id, topic_id, created_at) VALUES (?,?,?)`, f.UserID, newID, f.CreatedAt); err != nil {
					return err
				}
			}
			for _, u := range sn.NewFavDst {
				if _, err := t.Exec(`DELETE FROM favorite_topics WHERE user_id=? AND topic_id=?`, u, r.dst); err != nil {
					return err
				}
			}
			for _, rd := range sn.Redirects {
				if _, err := t.Exec(`INSERT OR REPLACE INTO topic_redirects(kind, slug, topic_id) VALUES (?,?,?)`, rd[0], rd[1], newID); err != nil {
					return err
				}
			}
			if _, err := t.Exec(`UPDATE topic_merges SET undone=1 WHERE id=?`, r.id); err != nil {
				return err
			}
			n++
		}
		return refreshFTSIDs(t, touched)
	})
	return n, err
}

// ListTopicMerges devolve os lotes de fusão recentes (para o desfazer na UI).
type TopicMergeBatch struct {
	BatchID   string   `json:"batchId"`
	Into      string   `json:"into"`
	Merged    []string `json:"merged"`
	Actor     string   `json:"actor"`
	CreatedAt int64    `json:"createdAt"`
	Undone    bool     `json:"undone"`
}

func (l *Library) ListTopicMerges(ctx context.Context, limit int) ([]TopicMergeBatch, error) {
	rows, err := l.r.QueryContext(ctx, `SELECT tm.batch_id, COALESCE(tp.title, ''), tm.snapshot, COALESCE(tm.actor,''), tm.created_at, tm.undone
		FROM topic_merges tm LEFT JOIN topics tp ON tp.id = tm.dst_id ORDER BY tm.id DESC LIMIT ?`, clampLimit(limit, 100, 1000))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TopicMergeBatch{}
	idx := map[string]int{}
	for rows.Next() {
		var b, into, snap, actor string
		var at int64
		var undone int
		if err := rows.Scan(&b, &into, &snap, &actor, &at, &undone); err != nil {
			return nil, err
		}
		var sn mergeSnapshot
		json.Unmarshal([]byte(snap), &sn)
		i, ok := idx[b]
		if !ok {
			i = len(out)
			idx[b] = i
			out = append(out, TopicMergeBatch{BatchID: b, Into: into, Merged: []string{}, Actor: actor, CreatedAt: at, Undone: undone == 1})
		}
		out[i].Merged = append(out[i].Merged, sn.Title)
	}
	return out, rows.Err()
}

// resolveTopicAlias: um nome novo cujo slug bate com o alias de um tópico
// existente (do mesmo tipo) vai para esse tópico. Só roda quando o
// (kind, slug) não existe, então o custo fica nos tópicos novos.
func resolveTopicAlias(t *sql.Tx, kind, slug string) (int64, error) {
	rows, err := t.Query(`SELECT ta.topic_id, ta.alias FROM topic_aliases ta JOIN topics tp ON tp.id = ta.topic_id WHERE tp.kind = ?`, kind)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var a string
		if err := rows.Scan(&id, &a); err != nil {
			return 0, err
		}
		if slugify(a) == slug {
			return id, nil
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return 0, sql.ErrNoRows
}
