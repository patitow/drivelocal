package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Actions é a unidade de mudança usada por edição manual, regras, lotes
// (CLI) e aceite de sugestões. Toda mudança fica em `changes` com batch_id.
type Actions struct {
	AddTags     []string  `json:"add_tags,omitempty"`
	RemoveTags  []string  `json:"remove_tags,omitempty"`
	Topic       *TopicRef `json:"topic,omitempty"`
	RemoveTopic *TopicRef `json:"remove_topic,omitempty"`
	Maturity    *string   `json:"maturity,omitempty"` // geral | sugestivo | explicito | auto (limpa)
	Category    *string   `json:"category,omitempty"`
	Title       *string   `json:"title,omitempty"`
}

type TopicRef struct {
	ID      int64    `json:"id,omitempty"`
	Kind    string   `json:"kind,omitempty"`
	Title   string   `json:"title,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
}

func (a Actions) empty() bool {
	return len(a.AddTags) == 0 && len(a.RemoveTags) == 0 && a.Topic == nil && a.RemoveTopic == nil &&
		a.Maturity == nil && a.Category == nil && a.Title == nil
}

type ChangeSample struct {
	MediaID int64  `json:"mediaId"`
	Path    string `json:"path"`
	Op      string `json:"op"`
	Detail  string `json:"detail"`
}

type ApplyResult struct {
	BatchID string         `json:"batchId"`
	DryRun  bool           `json:"dryRun"`
	Matched int            `json:"matched"`
	Changes int            `json:"changes"`
	ByOp    map[string]int `json:"byOp"`
	Sample  []ChangeSample `json:"sample"`
}

var allowedFields = map[string]bool{"maturity_rule": true, "maturity_manual": true, "category": true, "title": true, "title_manual": true}

type applier struct {
	t       *sql.Tx
	batch   string
	actor   string
	reason  string
	source  string // manual | rule | ai
	res     *ApplyResult
	touched map[int64]bool
	tagIDs  map[string]int64
}

func newBatchID() string { return "b" + generateToken(6) }

// ApplyActions aplica as ações aos itens. Com dryRun, faz tudo numa
// transação e desfaz no fim, devolvendo a contagem e uma amostra.
func (l *Library) ApplyActions(ctx context.Context, ids []int64, acts Actions, source, actor, reason string, dryRun bool) (*ApplyResult, error) {
	res := &ApplyResult{BatchID: newBatchID(), DryRun: dryRun, Matched: len(ids), ByOp: map[string]int{}}
	err := l.withApplier(ctx, res, source, actor, reason, dryRun, func(a *applier) error {
		return a.apply(ids, acts)
	})
	return res, err
}

var errDryRun = errors.New("dry-run")

func (l *Library) withApplier(ctx context.Context, res *ApplyResult, source, actor, reason string, dryRun bool, fn func(*applier) error) error {
	err := l.tx(ctx, func(t *sql.Tx) error {
		a := &applier{t: t, batch: res.BatchID, actor: actor, reason: reason, source: source, res: res, touched: map[int64]bool{}, tagIDs: map[string]int64{}}
		if err := fn(a); err != nil {
			return err
		}
		if err := a.finish(); err != nil {
			return err
		}
		if dryRun {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		return nil
	}
	return err
}

func (a *applier) finish() error {
	if len(a.touched) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(a.touched))
	for id := range a.touched {
		ids = append(ids, id)
	}
	for s := 0; s < len(ids); s += 500 {
		e := s + 500
		if e > len(ids) {
			e = len(ids)
		}
		if err := recomputeMaturity(a.t, "id IN ("+placeholders(e-s)+")", int64sToAny(ids[s:e])...); err != nil {
			return err
		}
		if err := refreshFTS(a.t, ids[s:e]); err != nil {
			return err
		}
	}
	return nil
}

func (a *applier) record(op string, mediaID int64, refID any, field string, oldV, newV any, detail string) error {
	_, err := a.t.Exec(`INSERT INTO changes(batch_id, op, media_id, ref_id, field, old_value, new_value, actor, reason, created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		a.batch, op, mediaID, refID, nullStr(field), oldV, newV, a.actor, nullStr(a.reason), nowUnix())
	if err != nil {
		return err
	}
	a.touched[mediaID] = true
	a.res.Changes++
	a.res.ByOp[op]++
	if len(a.res.Sample) < 25 {
		var p string
		a.t.QueryRow(`SELECT path FROM media WHERE id=?`, mediaID).Scan(&p)
		a.res.Sample = append(a.res.Sample, ChangeSample{MediaID: mediaID, Path: p, Op: op, Detail: detail})
	}
	return nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// tagID resolve slug/rótulo/alias; cria a tag (ativa) se não existir.
func (a *applier) tagID(label string, create bool) (int64, error) {
	key := strings.TrimSpace(label)
	if id, ok := a.tagIDs[key]; ok {
		return id, nil
	}
	id, err := resolveTag(a.t, key, create)
	if err == nil {
		a.tagIDs[key] = id
	}
	return id, err
}

var errNotFound = errors.New("não encontrado")

func resolveTag(t *sql.Tx, label string, create bool) (int64, error) {
	slug := slugify(label)
	if slug == "" {
		return 0, fmt.Errorf("tag inválida: %q", label)
	}
	var id int64
	err := t.QueryRow(`SELECT id FROM tags WHERE slug=?`, slug).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	err = t.QueryRow(`SELECT tag_id FROM tag_aliases WHERE alias=?`, slug).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if !create {
		return 0, errNotFound
	}
	facet, lbl := "outro", label
	if i := strings.Index(label, ":"); i > 0 && i < len(label)-1 {
		facet = slugify(label[:i])
		lbl = strings.TrimSpace(label[i+1:])
		slug = slugify(lbl)
		if err := t.QueryRow(`SELECT id FROM tags WHERE slug=?`, slug).Scan(&id); err == nil {
			return id, nil
		}
	}
	res, err := t.Exec(`INSERT INTO tags(slug, label, facet, status, created_at) VALUES (?,?,?,'active',?)`, slug, lbl, facet, nowUnix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func topicExcluded(t *sql.Tx, topicID, mediaID int64) bool {
	var n int
	t.QueryRow(`SELECT COUNT(*) FROM topic_item_exclusions WHERE topic_id=? AND media_id=?`, topicID, mediaID).Scan(&n)
	return n > 0
}

func (a *applier) topicID(ref *TopicRef, create bool) (int64, error) {
	if ref.ID > 0 {
		var id int64
		if err := a.t.QueryRow(`SELECT id FROM topics WHERE id=?`, ref.ID).Scan(&id); err != nil {
			return 0, fmt.Errorf("tópico %d: %w", ref.ID, errNotFound)
		}
		return id, nil
	}
	kind := ref.Kind
	if kind == "" {
		kind = "theme"
	}
	if kind != "series" && kind != "creator" && kind != "theme" {
		return 0, fmt.Errorf("tipo de tópico inválido: %q", kind)
	}
	if strings.TrimSpace(ref.Title) == "" {
		return 0, errors.New("tópico sem título")
	}
	if !create {
		var id int64
		if err := a.t.QueryRow(`SELECT id FROM topics WHERE kind=? AND slug=?`, kind, slugify(ref.Title)).Scan(&id); err != nil {
			return 0, errNotFound
		}
		return id, nil
	}
	id, err := ensureTopic(a.t, kind, strings.TrimSpace(ref.Title), a.source)
	if err != nil {
		return 0, err
	}
	for _, al := range ref.Aliases {
		if al = strings.TrimSpace(al); al != "" {
			if _, err := a.t.Exec(`INSERT OR IGNORE INTO topic_aliases(topic_id, alias) VALUES (?,?)`, id, al); err != nil {
				return 0, err
			}
		}
	}
	return id, nil
}

func (a *applier) apply(ids []int64, acts Actions) error {
	if acts.Maturity != nil && *acts.Maturity != "auto" {
		if _, ok := parseMaturity(*acts.Maturity); !ok {
			return fmt.Errorf("maturidade inválida: %q", *acts.Maturity)
		}
	}
	tagSrc := a.source
	if tagSrc == "ai" {
		tagSrc = "ai"
	}
	var addTags, delTags []int64
	for _, tg := range acts.AddTags {
		id, err := a.tagID(tg, true)
		if err != nil {
			return err
		}
		addTags = append(addTags, id)
	}
	for _, tg := range acts.RemoveTags {
		id, err := a.tagID(tg, false)
		if errors.Is(err, errNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		delTags = append(delTags, id)
	}
	var topicAdd, topicDel int64
	if acts.Topic != nil {
		id, err := a.topicID(acts.Topic, true)
		if err != nil && !errors.Is(err, errTopicBlocked) {
			return err
		}
		topicAdd = id
	}
	if acts.RemoveTopic != nil {
		id, err := a.topicID(acts.RemoveTopic, false)
		if err != nil && !errors.Is(err, errNotFound) {
			return err
		}
		topicDel = id
	}
	for _, mid := range ids {
		for _, tid := range addTags {
			res, err := a.t.Exec(`INSERT OR IGNORE INTO media_tags(media_id, tag_id, source, created_by, created_at) VALUES (?,?,?,?,?)`, mid, tid, tagSrc, a.actor, nowUnix())
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				if err := a.record("tag_add", mid, tid, "", nil, tagSrc, fmt.Sprintf("+tag %d", tid)); err != nil {
					return err
				}
			}
		}
		for _, tid := range delTags {
			var src string
			if err := a.t.QueryRow(`SELECT source FROM media_tags WHERE media_id=? AND tag_id=?`, mid, tid).Scan(&src); err != nil {
				continue
			}
			if _, err := a.t.Exec(`DELETE FROM media_tags WHERE media_id=? AND tag_id=?`, mid, tid); err != nil {
				return err
			}
			if err := a.record("tag_del", mid, tid, "", src, nil, fmt.Sprintf("-tag %d", tid)); err != nil {
				return err
			}
		}
		if topicAdd > 0 {
			// à mão: desfaz uma exclusão anterior; regra/IA respeitam a exclusão
			if a.source == "manual" {
				if _, err := a.t.Exec(`DELETE FROM topic_item_exclusions WHERE topic_id=? AND media_id=?`, topicAdd, mid); err != nil {
					return err
				}
			} else if topicExcluded(a.t, topicAdd, mid) {
				continue
			}
			var p string
			a.t.QueryRow(`SELECT path FROM media WHERE id=?`, mid).Scan(&p)
			res, err := a.t.Exec(`INSERT OR IGNORE INTO topic_items(topic_id, media_id, sort_key, source) VALUES (?,?,?,?)`, topicAdd, mid, naturalSortKey(p), a.source)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				if err := a.record("topic_add", mid, topicAdd, "", nil, nil, fmt.Sprintf("+tópico %d", topicAdd)); err != nil {
					return err
				}
			}
		}
		if topicDel > 0 {
			var sk, src string
			if err := a.t.QueryRow(`SELECT sort_key, source FROM topic_items WHERE topic_id=? AND media_id=?`, topicDel, mid).Scan(&sk, &src); err == nil {
				if _, err := a.t.Exec(`DELETE FROM topic_items WHERE topic_id=? AND media_id=?`, topicDel, mid); err != nil {
					return err
				}
				// tirado à mão: o scan não devolve
				if a.source == "manual" {
					if _, err := a.t.Exec(`INSERT OR IGNORE INTO topic_item_exclusions(topic_id, media_id, created_at) VALUES (?,?,?)`, topicDel, mid, nowUnix()); err != nil {
						return err
					}
				}
				if err := a.record("topic_del", mid, topicDel, "", sk, src, fmt.Sprintf("-tópico %d", topicDel)); err != nil {
					return err
				}
			}
		}
		if acts.Maturity != nil {
			field := "maturity_rule"
			if a.source == "manual" || a.source == "ai" {
				field = "maturity_manual"
			}
			var nv any
			if *acts.Maturity != "auto" {
				m, _ := parseMaturity(*acts.Maturity)
				nv = int64(m)
			}
			if err := a.setField(mid, field, nv); err != nil {
				return err
			}
		}
		if acts.Category != nil {
			var nv any
			if c := strings.TrimSpace(*acts.Category); c != "" {
				nv = slugify(c)
			}
			if err := a.setField(mid, "category", nv); err != nil {
				return err
			}
		}
		if acts.Title != nil {
			field := "title"
			if a.source == "manual" || a.source == "ai" {
				field = "title_manual"
			}
			var nv any
			if s := strings.TrimSpace(*acts.Title); s != "" {
				nv = s
			} else if field == "title" {
				continue
			}
			if err := a.setField(mid, field, nv); err != nil {
				return err
			}
		}
	}
	return nil
}

// setField grava media.field = nv se mudou, registrando o valor antigo.
func (a *applier) setField(mid int64, field string, nv any) error {
	if !allowedFields[field] {
		return fmt.Errorf("campo não permitido: %s", field)
	}
	var old sql.NullString
	if err := a.t.QueryRow(`SELECT CAST(`+field+` AS TEXT) FROM media WHERE id=?`, mid).Scan(&old); err != nil {
		return err
	}
	newStr, newNull := "", nv == nil
	if !newNull {
		newStr = fmt.Sprint(nv)
	}
	if old.Valid == !newNull && old.String == newStr {
		return nil
	}
	if _, err := a.t.Exec(`UPDATE media SET `+field+`=? WHERE id=?`, nv, mid); err != nil {
		return err
	}
	var oldV any
	if old.Valid {
		oldV = old.String
	}
	var newV any
	if !newNull {
		newV = newStr
	}
	return a.record("field", mid, nil, field, oldV, newV, fmt.Sprintf("%s: %v → %v", field, oldV, newV))
}

// Undo reverte um lote (na ordem inversa). Lotes já desfeitos são ignorados.
func (l *Library) Undo(ctx context.Context, batchID, actor string) (*ApplyResult, error) {
	res := &ApplyResult{BatchID: newBatchID(), ByOp: map[string]int{}}
	err := l.withApplier(ctx, res, "manual", actor, "undo "+batchID, false, func(a *applier) error {
		rows, err := a.t.Query(`SELECT id, op, media_id, ref_id, COALESCE(field,''), old_value, new_value FROM changes WHERE batch_id=? AND undone=0 ORDER BY id DESC`, batchID)
		if err != nil {
			return err
		}
		type ch struct {
			id, mediaID int64
			ref         sql.NullInt64
			op, field   string
			old, nw     sql.NullString
		}
		var list []ch
		for rows.Next() {
			var c ch
			if err := rows.Scan(&c.id, &c.op, &c.mediaID, &c.ref, &c.field, &c.old, &c.nw); err != nil {
				rows.Close()
				return err
			}
			list = append(list, c)
		}
		rows.Close()
		if len(list) == 0 {
			return fmt.Errorf("lote %s: %w (ou já desfeito)", batchID, errNotFound)
		}
		res.Matched = len(list)
		for _, c := range list {
			switch c.op {
			case "tag_add":
				if _, err := a.t.Exec(`DELETE FROM media_tags WHERE media_id=? AND tag_id=?`, c.mediaID, c.ref.Int64); err != nil {
					return err
				}
			case "tag_del":
				if _, err := a.t.Exec(`INSERT OR IGNORE INTO media_tags(media_id, tag_id, source, created_by, created_at) VALUES (?,?,?,?,?)`, c.mediaID, c.ref.Int64, c.old.String, actor, nowUnix()); err != nil {
					return err
				}
			case "topic_add":
				if _, err := a.t.Exec(`DELETE FROM topic_items WHERE topic_id=? AND media_id=?`, c.ref.Int64, c.mediaID); err != nil {
					return err
				}
			case "topic_del":
				src := c.nw.String
				if src == "" {
					src = "manual"
				}
				if _, err := a.t.Exec(`INSERT OR IGNORE INTO topic_items(topic_id, media_id, sort_key, source) VALUES (?,?,?,?)`, c.ref.Int64, c.mediaID, c.old.String, src); err != nil {
					return err
				}
				if _, err := a.t.Exec(`DELETE FROM topic_item_exclusions WHERE topic_id=? AND media_id=?`, c.ref.Int64, c.mediaID); err != nil {
					return err
				}
			case "field":
				if !allowedFields[c.field] {
					return fmt.Errorf("campo não permitido: %s", c.field)
				}
				var v any
				if c.old.Valid {
					v = c.old.String
					if strings.HasPrefix(c.field, "maturity") {
						var n int64
						fmt.Sscan(c.old.String, &n)
						v = n
					}
				}
				if _, err := a.t.Exec(`UPDATE media SET `+c.field+`=? WHERE id=?`, v, c.mediaID); err != nil {
					return err
				}
			}
			if _, err := a.t.Exec(`UPDATE changes SET undone=1 WHERE id=?`, c.id); err != nil {
				return err
			}
			a.touched[c.mediaID] = true
			res.Changes++
			res.ByOp["undo_"+c.op]++
		}
		return nil
	})
	return res, err
}

// ---------- regras ----------

type RuleMatch struct {
	PathPrefix string `json:"path_prefix,omitempty"`
	PathRegex  string `json:"path_regex,omitempty"`
	NameRegex  string `json:"name_regex,omitempty"`
	Kind       string `json:"kind,omitempty"` // video | image
	TopicID    int64  `json:"topic_id,omitempty"`
}

type Rule struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	Priority  int       `json:"priority"`
	Match     RuleMatch `json:"match"`
	Actions   Actions   `json:"actions"`
	CreatedAt int64     `json:"createdAt"`
	UpdatedAt int64     `json:"updatedAt"`
}

func (m RuleMatch) validate() error {
	if m.PathPrefix == "" && m.PathRegex == "" && m.NameRegex == "" && m.TopicID == 0 {
		return errors.New("regra precisa de pelo menos um critério (path_prefix, path_regex, name_regex ou topic_id)")
	}
	if m.Kind != "" && m.Kind != "video" && m.Kind != "image" {
		return fmt.Errorf("kind inválido: %q", m.Kind)
	}
	for _, re := range []string{m.PathRegex, m.NameRegex} {
		if re != "" {
			if _, err := regexp.Compile(re); err != nil {
				return fmt.Errorf("regex inválida %q: %w", re, err)
			}
		}
	}
	return nil
}

// matchIDs devolve os itens (não sumidos) que casam com a regra.
func (l *Library) matchIDs(ctx context.Context, m RuleMatch) ([]int64, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	q := `SELECT m.id, m.path, m.name FROM media m WHERE m.missing = 0`
	var args []any
	if p := normalizePolicyPrefix(m.PathPrefix); p != "" {
		q += ` AND (m.path = ? OR m.path LIKE ? ESCAPE '\')`
		args = append(args, p, likePrefix(p)+"/%")
	}
	if m.Kind != "" {
		q += ` AND m.kind = ?`
		args = append(args, m.Kind)
	}
	if m.TopicID > 0 {
		q += ` AND EXISTS (SELECT 1 FROM topic_items ti WHERE ti.media_id = m.id AND ti.topic_id = ?)`
		args = append(args, m.TopicID)
	}
	var pathRe, nameRe *regexp.Regexp
	if m.PathRegex != "" {
		pathRe = regexp.MustCompile(m.PathRegex)
	}
	if m.NameRegex != "" {
		nameRe = regexp.MustCompile(m.NameRegex)
	}
	rows, err := l.r.QueryContext(ctx, q+` ORDER BY m.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		var p, n string
		if err := rows.Scan(&id, &p, &n); err != nil {
			return nil, err
		}
		if pathRe != nil && !pathRe.MatchString(p) {
			continue
		}
		if nameRe != nil && !nameRe.MatchString(n) {
			continue
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (l *Library) ListRules(ctx context.Context) ([]Rule, error) {
	rows, err := l.r.QueryContext(ctx, `SELECT id, name, enabled, priority, match_json, actions_json, created_at, updated_at FROM rules ORDER BY priority, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		var r Rule
		var en int
		var mj, aj string
		if err := rows.Scan(&r.ID, &r.Name, &en, &r.Priority, &mj, &aj, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Enabled = en == 1
		json.Unmarshal([]byte(mj), &r.Match)
		json.Unmarshal([]byte(aj), &r.Actions)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (l *Library) SaveRule(ctx context.Context, r *Rule) error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("regra sem nome")
	}
	if err := r.Match.validate(); err != nil {
		return err
	}
	if r.Actions.empty() {
		return errors.New("regra sem ações")
	}
	mj, _ := json.Marshal(r.Match)
	aj, _ := json.Marshal(r.Actions)
	now := nowUnix()
	return l.tx(ctx, func(t *sql.Tx) error {
		if r.ID > 0 {
			res, err := t.Exec(`UPDATE rules SET name=?, enabled=?, priority=?, match_json=?, actions_json=?, updated_at=? WHERE id=?`,
				r.Name, boolInt(r.Enabled), r.Priority, string(mj), string(aj), now, r.ID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return errNotFound
			}
			r.UpdatedAt = now
			return nil
		}
		res, err := t.Exec(`INSERT INTO rules(name, enabled, priority, match_json, actions_json, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
			r.Name, boolInt(r.Enabled), r.Priority, string(mj), string(aj), now, now)
		if err != nil {
			return err
		}
		r.ID, _ = res.LastInsertId()
		r.CreatedAt, r.UpdatedAt = now, now
		return nil
	})
}

func (l *Library) DeleteRule(ctx context.Context, id int64) error {
	return l.tx(ctx, func(t *sql.Tx) error {
		_, err := t.Exec(`DELETE FROM rules WHERE id=?`, id)
		return err
	})
}

// ApplyRules aplica as regras ativas (ou só ruleID) num único lote.
func (l *Library) ApplyRules(ctx context.Context, ruleID int64, actor string, dryRun bool) (*ApplyResult, error) {
	rules, err := l.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	type planned struct {
		ids  []int64
		acts Actions
	}
	var plan []planned
	matched := map[int64]bool{}
	for _, r := range rules {
		if ruleID > 0 && r.ID != ruleID {
			continue
		}
		if ruleID == 0 && !r.Enabled {
			continue
		}
		ids, err := l.matchIDs(ctx, r.Match)
		if err != nil {
			return nil, fmt.Errorf("regra %q: %w", r.Name, err)
		}
		for _, id := range ids {
			matched[id] = true
		}
		plan = append(plan, planned{ids, r.Actions})
	}
	if ruleID > 0 && len(plan) == 0 {
		return nil, errNotFound
	}
	res := &ApplyResult{BatchID: newBatchID(), DryRun: dryRun, ByOp: map[string]int{}}
	err = l.withApplier(ctx, res, "rule", actor, "regras", dryRun, func(a *applier) error {
		for _, p := range plan {
			if err := a.apply(p.ids, p.acts); err != nil {
				return err
			}
		}
		return nil
	})
	res.Matched = len(matched)
	return res, err
}

// BatchOp é uma operação do lote enviado pelo CLI: itens por id, caminho ou regra.
type BatchOp struct {
	IDs     []int64    `json:"ids,omitempty"`
	Paths   []string   `json:"paths,omitempty"`
	Match   *RuleMatch `json:"match,omitempty"`
	Actions Actions    `json:"actions"`
}

func (l *Library) ApplyBatch(ctx context.Context, ops []BatchOp, actor string, dryRun bool) (*ApplyResult, error) {
	type planned struct {
		ids  []int64
		acts Actions
	}
	var plan []planned
	matched := map[int64]bool{}
	for i, op := range ops {
		if op.Actions.empty() {
			return nil, fmt.Errorf("operação %d sem ações", i)
		}
		ids := append([]int64{}, op.IDs...)
		for _, p := range op.Paths {
			var id int64
			if err := l.r.QueryRowContext(ctx, `SELECT id FROM media WHERE path=?`, normalizeRelativeMediaPath(p)).Scan(&id); err != nil {
				return nil, fmt.Errorf("operação %d: caminho não catalogado: %s", i, p)
			}
			ids = append(ids, id)
		}
		if op.Match != nil {
			mids, err := l.matchIDs(ctx, *op.Match)
			if err != nil {
				return nil, fmt.Errorf("operação %d: %w", i, err)
			}
			ids = append(ids, mids...)
		}
		for _, id := range ids {
			matched[id] = true
		}
		plan = append(plan, planned{ids, op.Actions})
	}
	res := &ApplyResult{BatchID: newBatchID(), DryRun: dryRun, ByOp: map[string]int{}}
	err := l.withApplier(ctx, res, "rule", actor, "lote", dryRun, func(a *applier) error {
		for _, p := range plan {
			if err := a.apply(p.ids, p.acts); err != nil {
				return err
			}
		}
		return nil
	})
	res.Matched = len(matched)
	return res, err
}
