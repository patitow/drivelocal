package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ---------- tags ----------

type TagInfo struct {
	TagRef
	Status  string   `json:"status"`
	Count   int      `json:"count"`
	Aliases []string `json:"aliases"`
}

func (l *Library) ListTags(ctx context.Context, v Viewer, q string) ([]TagInfo, error) {
	vis, args := v.visibleSQL(false)
	cond := ""
	if q = strings.TrimSpace(q); q != "" {
		cond = ` WHERE (tg.label LIKE ? ESCAPE '\' OR tg.slug LIKE ? ESCAPE '\')`
		p := "%" + likePrefix(q) + "%"
		args = append(args, p, "%"+likePrefix(slugify(q))+"%")
	}
	rows, err := l.r.QueryContext(ctx, `SELECT tg.id, tg.slug, tg.label, tg.facet, tg.status,
		(SELECT COUNT(*) FROM media_tags mt JOIN media m ON m.id = mt.media_id WHERE mt.tag_id = tg.id AND `+vis+`)
		FROM tags tg`+cond+` ORDER BY tg.facet, tg.label COLLATE NOCASE`, args...)
	if err != nil {
		return nil, err
	}
	out := []TagInfo{}
	idx := map[int64]int{}
	for rows.Next() {
		var t TagInfo
		if err := rows.Scan(&t.ID, &t.Slug, &t.Label, &t.Facet, &t.Status, &t.Count); err != nil {
			rows.Close()
			return nil, err
		}
		// não-admin não vê tag sem item visível (o nome já revelaria o conteúdo)
		if t.Count == 0 && !v.IsAdmin {
			continue
		}
		t.Aliases = []string{}
		idx[t.ID] = len(out)
		out = append(out, t)
	}
	rows.Close()
	rows, err = l.r.QueryContext(ctx, `SELECT tag_id, alias FROM tag_aliases`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var a string
		rows.Scan(&id, &a)
		if i, ok := idx[id]; ok {
			out[i].Aliases = append(out[i].Aliases, a)
		}
	}
	return out, rows.Err()
}

type TagInput struct {
	Label   *string  `json:"label"`
	Facet   *string  `json:"facet"`
	Status  *string  `json:"status"`
	Aliases []string `json:"aliases"`
}

func (l *Library) CreateTag(ctx context.Context, in TagInput) (int64, error) {
	if in.Label == nil || slugify(*in.Label) == "" {
		return 0, errInvalid
	}
	facet := "outro"
	if in.Facet != nil && slugify(*in.Facet) != "" {
		facet = slugify(*in.Facet)
	}
	var id int64
	err := l.tx(ctx, func(t *sql.Tx) error {
		res, err := t.Exec(`INSERT INTO tags(slug, label, facet, status, created_at) VALUES (?,?,?,'active',?)`,
			slugify(*in.Label), strings.TrimSpace(*in.Label), facet, nowUnix())
		if err != nil {
			return errInvalid
		}
		id, _ = res.LastInsertId()
		return setTagAliases(t, id, in.Aliases)
	})
	return id, err
}

func setTagAliases(t *sql.Tx, id int64, aliases []string) error {
	if aliases == nil {
		return nil
	}
	if _, err := t.Exec(`DELETE FROM tag_aliases WHERE tag_id=?`, id); err != nil {
		return err
	}
	for _, a := range aliases {
		if s := slugify(a); s != "" {
			if _, err := t.Exec(`INSERT OR REPLACE INTO tag_aliases(alias, tag_id) VALUES (?,?)`, s, id); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *Library) UpdateTag(ctx context.Context, id int64, in TagInput) error {
	return l.tx(ctx, func(t *sql.Tx) error {
		var n int
		t.QueryRow(`SELECT COUNT(*) FROM tags WHERE id=?`, id).Scan(&n)
		if n == 0 {
			return errNotFound
		}
		if in.Label != nil && strings.TrimSpace(*in.Label) != "" {
			if _, err := t.Exec(`UPDATE tags SET label=? WHERE id=?`, strings.TrimSpace(*in.Label), id); err != nil {
				return err
			}
		}
		if in.Facet != nil && slugify(*in.Facet) != "" {
			if _, err := t.Exec(`UPDATE tags SET facet=? WHERE id=?`, slugify(*in.Facet), id); err != nil {
				return err
			}
		}
		if in.Status != nil && (*in.Status == "active" || *in.Status == "proposed") {
			if _, err := t.Exec(`UPDATE tags SET status=? WHERE id=?`, *in.Status, id); err != nil {
				return err
			}
		}
		if err := setTagAliases(t, id, in.Aliases); err != nil {
			return err
		}
		return refreshFTSForTag(t, id)
	})
}

func refreshFTSForTag(t *sql.Tx, tagID int64) error {
	rows, err := t.Query(`SELECT media_id FROM media_tags WHERE tag_id=?`, tagID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for s := 0; s < len(ids); s += 500 {
		e := s + 500
		if e > len(ids) {
			e = len(ids)
		}
		if err := refreshFTS(t, ids[s:e]); err != nil {
			return err
		}
	}
	return nil
}

func (l *Library) DeleteTag(ctx context.Context, id int64) error {
	return l.tx(ctx, func(t *sql.Tx) error {
		rows, err := t.Query(`SELECT media_id FROM media_tags WHERE tag_id=?`, id)
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var mid int64
			rows.Scan(&mid)
			ids = append(ids, mid)
		}
		rows.Close()
		res, err := t.Exec(`DELETE FROM tags WHERE id=?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNotFound
		}
		for s := 0; s < len(ids); s += 500 {
			e := min(s+500, len(ids))
			if err := refreshFTS(t, ids[s:e]); err != nil {
				return err
			}
		}
		return nil
	})
}

// MergeTags move os itens de src para dst e transforma o slug de src em alias.
func (l *Library) MergeTags(ctx context.Context, srcID, dstID int64) error {
	if srcID == dstID {
		return errInvalid
	}
	return l.tx(ctx, func(t *sql.Tx) error {
		var srcSlug string
		if err := t.QueryRow(`SELECT slug FROM tags WHERE id=?`, srcID).Scan(&srcSlug); err != nil {
			return errNotFound
		}
		var n int
		t.QueryRow(`SELECT COUNT(*) FROM tags WHERE id=?`, dstID).Scan(&n)
		if n == 0 {
			return errNotFound
		}
		if _, err := t.Exec(`INSERT OR IGNORE INTO media_tags(media_id, tag_id, source, created_by, created_at)
			SELECT media_id, ?, source, created_by, created_at FROM media_tags WHERE tag_id=?`, dstID, srcID); err != nil {
			return err
		}
		if _, err := t.Exec(`UPDATE tag_aliases SET tag_id=? WHERE tag_id=?`, dstID, srcID); err != nil {
			return err
		}
		if _, err := t.Exec(`DELETE FROM tags WHERE id=?`, srcID); err != nil {
			return err
		}
		if _, err := t.Exec(`INSERT OR REPLACE INTO tag_aliases(alias, tag_id) VALUES (?,?)`, srcSlug, dstID); err != nil {
			return err
		}
		return refreshFTSForTag(t, dstID)
	})
}

// ---------- tópicos ----------

type TopicInfo struct {
	ID          int64    `json:"id"`
	Kind        string   `json:"kind"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Source      string   `json:"source"`
	Count       int      `json:"count"`
	Watched     int      `json:"watched"`
	Favorite    bool     `json:"favorite"`
	CoverPath   string   `json:"coverPath,omitempty"`
	Aliases     []string `json:"aliases"`
	Recent      []string `json:"recent,omitempty"` // até 3 itens mais novos (só na listagem)
	Latest      int64    `json:"latest,omitempty"` // mtime do item mais novo
	Maturity    string   `json:"maturity,omitempty"` // a maior entre capa e recentes: o que o card mostra
}

type TopicQuery struct {
	Kind      string
	Q         string
	Favorites bool
	Limit     int
	Offset    int
}

func (l *Library) ListTopics(ctx context.Context, v Viewer, tq TopicQuery) ([]TopicInfo, int, error) {
	vis, vargs := v.visibleSQL(false)
	conds := []string{"cnt > 0"}
	var wargs []any
	if tq.Kind != "" {
		conds = append(conds, "tp.kind = ?")
		wargs = append(wargs, tq.Kind)
	}
	if q := strings.TrimSpace(tq.Q); q != "" {
		conds = append(conds, `(tp.title LIKE ? ESCAPE '\' OR tp.slug LIKE ? ESCAPE '\' OR EXISTS (SELECT 1 FROM topic_aliases ta WHERE ta.topic_id = tp.id AND ta.alias LIKE ? ESCAPE '\'))`)
		p := "%" + likePrefix(q) + "%"
		wargs = append(wargs, p, "%"+likePrefix(slugify(q))+"%", p)
	}
	if tq.Favorites {
		conds = append(conds, "fav = 1")
	}
	inner := `SELECT tp.id, tp.kind, tp.slug, tp.title, COALESCE(tp.description,''), tp.source,
		(SELECT COUNT(*) FROM topic_items ti JOIN media m ON m.id = ti.media_id WHERE ti.topic_id = tp.id AND ` + vis + `) AS cnt,
		(SELECT COUNT(*) FROM topic_items ti JOIN watch_progress wp ON wp.media_id = ti.media_id AND wp.user_id = ? AND wp.completed = 1 WHERE ti.topic_id = tp.id) AS watched,
		EXISTS (SELECT 1 FROM favorite_topics ft WHERE ft.topic_id = tp.id AND ft.user_id = ?) AS fav,
		(SELECT m.path FROM topic_items ti JOIN media m ON m.id = ti.media_id WHERE ti.topic_id = tp.id AND ` + vis + ` ORDER BY ti.sort_key LIMIT 1) AS cover
		FROM topics tp`
	args := append([]any{}, vargs...)
	args = append(args, v.UserID, v.UserID)
	args = append(args, vargs...)
	q := `SELECT id, kind, slug, title, d, source, cnt, watched, fav, cover FROM (` +
		strings.Replace(inner, "COALESCE(tp.description,''),", "COALESCE(tp.description,'') AS d,", 1) +
		`) tp WHERE ` + strings.Join(conds, " AND ")
	all := append(args, wargs...)
	var total int
	if err := l.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM (`+q+`)`, all...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := clampLimit(tq.Limit, 60, 500)
	rows, err := l.r.QueryContext(ctx, q+` ORDER BY fav DESC, title COLLATE NOCASE LIMIT ? OFFSET ?`, append(all, limit, max(tq.Offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	out := []TopicInfo{}
	idx := map[int64]int{}
	for rows.Next() {
		var t TopicInfo
		var fav int
		var cover sql.NullString
		if err := rows.Scan(&t.ID, &t.Kind, &t.Slug, &t.Title, &t.Description, &t.Source, &t.Count, &t.Watched, &fav, &cover); err != nil {
			rows.Close()
			return nil, 0, err
		}
		t.Favorite = fav == 1
		t.CoverPath = cover.String
		t.Aliases = []string{}
		idx[t.ID] = len(out)
		out = append(out, t)
	}
	rows.Close()
	if len(out) > 0 {
		ids := make([]any, 0, len(out))
		for _, t := range out {
			ids = append(ids, t.ID)
		}
		rows, err := l.r.QueryContext(ctx, `SELECT topic_id, alias FROM topic_aliases WHERE topic_id IN (`+placeholders(len(ids))+`)`, ids...)
		if err == nil {
			for rows.Next() {
				var id int64
				var a string
				rows.Scan(&id, &a)
				out[idx[id]].Aliases = append(out[idx[id]].Aliases, a)
			}
			rows.Close()
		}
		// Os três itens mais novos de cada tópico, para o card e "com novidades".
		rows, err = l.r.QueryContext(ctx, `SELECT topic_id, path, mtime FROM (
			SELECT ti.topic_id, m.path, m.mtime, ROW_NUMBER() OVER (PARTITION BY ti.topic_id ORDER BY m.mtime DESC, m.id DESC) AS rn
			FROM topic_items ti JOIN media m ON m.id = ti.media_id
			WHERE ti.topic_id IN (`+placeholders(len(ids))+`) AND `+vis+`) WHERE rn <= 3 ORDER BY topic_id, rn`, append(ids, vargs...)...)
		if err == nil {
			for rows.Next() {
				var id, mt int64
				var p string
				rows.Scan(&id, &p, &mt)
				t := &out[idx[id]]
				t.Recent = append(t.Recent, p)
				if mt > t.Latest {
					t.Latest = mt
				}
			}
			rows.Close()
		}
		var shown []string
		for _, t := range out {
			shown = append(append(shown, t.CoverPath), t.Recent...)
		}
		mat := l.maturityOfPaths(ctx, shown)
		for i := range out {
			top, seen := 0, false
			for _, p := range append([]string{out[i].CoverPath}, out[i].Recent...) {
				if m, ok := mat[p]; ok {
					top, seen = max(top, m), true
				}
			}
			if seen {
				out[i].Maturity = maturityName(top)
			}
		}
	}
	return out, total, nil
}

func (l *Library) GetTopic(ctx context.Context, v Viewer, id int64) (*TopicInfo, error) {
	var t TopicInfo
	var fav int
	err := l.r.QueryRowContext(ctx, `SELECT tp.id, tp.kind, tp.slug, tp.title, COALESCE(tp.description,''), tp.source,
		EXISTS (SELECT 1 FROM favorite_topics ft WHERE ft.topic_id = tp.id AND ft.user_id = ?) FROM topics tp WHERE tp.id = ?`, v.UserID, id).
		Scan(&t.ID, &t.Kind, &t.Slug, &t.Title, &t.Description, &t.Source, &fav)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	t.Favorite = fav == 1
	vis, vargs := v.visibleSQL(false)
	l.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM topic_items ti JOIN media m ON m.id = ti.media_id WHERE ti.topic_id = ? AND `+vis, append([]any{id}, vargs...)...).Scan(&t.Count)
	if t.Count == 0 && !v.IsAdmin {
		return nil, errNotFound
	}
	t.Aliases = []string{}
	rows, err := l.r.QueryContext(ctx, `SELECT alias FROM topic_aliases WHERE topic_id=? ORDER BY alias`, id)
	if err == nil {
		for rows.Next() {
			var a string
			rows.Scan(&a)
			t.Aliases = append(t.Aliases, a)
		}
		rows.Close()
	}
	return &t, nil
}

type TopicInput struct {
	Kind        *string  `json:"kind"`
	Title       *string  `json:"title"`
	Description *string  `json:"description"`
	Aliases     []string `json:"aliases"`
	Revert      bool     `json:"revert"` // renomear: desfaz o anterior
}

func (l *Library) CreateTopic(ctx context.Context, in TopicInput) (int64, error) {
	if in.Title == nil || slugify(*in.Title) == "" {
		return 0, errInvalid
	}
	kind := "theme"
	if in.Kind != nil {
		kind = *in.Kind
	}
	if !topicKinds[kind] {
		return 0, errInvalid
	}
	var id int64
	err := l.tx(ctx, func(t *sql.Tx) error {
		var err error
		id, err = ensureTopic(t, kind, strings.TrimSpace(*in.Title), "manual")
		if err != nil {
			return err
		}
		if in.Description != nil {
			if _, err := t.Exec(`UPDATE topics SET description=? WHERE id=?`, *in.Description, id); err != nil {
				return err
			}
		}
		return setTopicAliases(t, id, in.Aliases)
	})
	return id, err
}

func setTopicAliases(t *sql.Tx, id int64, aliases []string) error {
	if aliases == nil {
		return nil
	}
	if _, err := t.Exec(`DELETE FROM topic_aliases WHERE topic_id=?`, id); err != nil {
		return err
	}
	for _, a := range aliases {
		if a = strings.TrimSpace(a); a != "" {
			if _, err := t.Exec(`INSERT OR IGNORE INTO topic_aliases(topic_id, alias) VALUES (?,?)`, id, a); err != nil {
				return err
			}
		}
	}
	return nil
}

func topicMediaIDs(t *sql.Tx, id int64) []int64 {
	rows, err := t.Query(`SELECT media_id FROM topic_items WHERE topic_id=?`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var m int64
		rows.Scan(&m)
		ids = append(ids, m)
	}
	return ids
}

func refreshFTSIDs(t *sql.Tx, ids []int64) error {
	for s := 0; s < len(ids); s += 500 {
		e := min(s+500, len(ids))
		if err := refreshFTS(t, ids[s:e]); err != nil {
			return err
		}
	}
	return nil
}

func (l *Library) UpdateTopic(ctx context.Context, id int64, in TopicInput) error {
	return l.tx(ctx, func(t *sql.Tx) error {
		var n int
		t.QueryRow(`SELECT COUNT(*) FROM topics WHERE id=?`, id).Scan(&n)
		if n == 0 {
			return errNotFound
		}
		if in.Title != nil && strings.TrimSpace(*in.Title) != "" {
			if err := renameTopicTx(t, id, strings.TrimSpace(*in.Title), in.Revert); err != nil {
				return err
			}
		}
		if in.Description != nil {
			if _, err := t.Exec(`UPDATE topics SET description=? WHERE id=?`, *in.Description, id); err != nil {
				return err
			}
		}
		if in.Kind != nil && topicKinds[*in.Kind] {
			if _, err := t.Exec(`UPDATE topics SET kind=? WHERE id=?`, *in.Kind, id); err != nil {
				return errInvalid
			}
		}
		if err := setTopicAliases(t, id, in.Aliases); err != nil {
			return err
		}
		return refreshFTSIDs(t, topicMediaIDs(t, id))
	})
}

// renameTopicTx troca o título. O slug fica (é a identidade da pasta no scan);
// o nome novo ganha um redirecionamento, para pastas novas com esse nome
// caírem aqui, e o antigo vira apelido (busca e detecção de duplicados).
// Sugestões de duplicado calculadas pelo nome antigo saem; a próxima
// varredura recalcula com o novo. revert desfaz um renomear: o nome que sai
// não vira apelido e perde o redirecionamento.
func renameTopicTx(t *sql.Tx, id int64, title string, revert bool) error {
	var kind, slug, old string
	if err := t.QueryRow(`SELECT kind, slug, title FROM topics WHERE id=?`, id).Scan(&kind, &slug, &old); err != nil {
		return errNotFound
	}
	if old == title {
		return nil
	}
	ns := slugify(title)
	if ns == "" {
		return errInvalid
	}
	if ns != slug {
		var other int64
		t.QueryRow(`SELECT COALESCE((SELECT topic_id FROM topic_redirects WHERE kind=? AND slug=?), (SELECT id FROM topics WHERE kind=? AND slug=?), 0)`, kind, ns, kind, ns).Scan(&other)
		if other != 0 && other != id {
			return errNameTaken
		}
		if _, err := t.Exec(`INSERT OR IGNORE INTO topic_redirects(kind, slug, topic_id) VALUES (?,?,?)`, kind, ns, id); err != nil {
			return err
		}
	}
	keepOld := `INSERT OR IGNORE INTO topic_aliases(topic_id, alias) VALUES (?,?)`
	if revert {
		keepOld = `DELETE FROM topic_aliases WHERE topic_id=? AND alias=?`
		if os := slugify(old); os != slug {
			if _, err := t.Exec(`DELETE FROM topic_redirects WHERE kind=? AND slug=? AND topic_id=?`, kind, os, id); err != nil {
				return err
			}
		}
	}
	stmts := []struct {
		q    string
		args []any
	}{
		{`UPDATE topics SET title=?, source=CASE WHEN source='auto' THEN 'manual' ELSE source END WHERE id=?`, []any{title, id}},
		{keepOld, []any{id, old}},
		{`DELETE FROM topic_aliases WHERE topic_id=? AND alias=?`, []any{id, title}},
		{`DELETE FROM topic_merge_suggestions WHERE status='pending' AND method<>'user' AND (src_id=? OR dst_id=?)`, []any{id, id}},
	}
	for _, s := range stmts {
		if _, err := t.Exec(s.q, s.args...); err != nil {
			return err
		}
	}
	return nil
}

// errNameTaken: outro tópico do mesmo tipo já usa o nome; o caminho é juntar.
var errNameTaken = fmt.Errorf("%w: já existe outro com esse nome (use Juntar)", errInvalid)

func (l *Library) DeleteTopic(ctx context.Context, id int64) error {
	return l.tx(ctx, func(t *sql.Tx) error {
		ids := topicMediaIDs(t, id)
		res, err := t.Exec(`DELETE FROM topics WHERE id=?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNotFound
		}
		return refreshFTSIDs(t, ids)
	})
}

// MergeTopics junta src em dst: itens, favoritos e aliases; o título de src
// vira alias de dst e (kind, slug) de src fica em topic_redirects, para que o
// scan automático não recrie o tópico antigo (ensureTopic segue o redirect).
func (l *Library) MergeTopics(ctx context.Context, srcID, dstID int64) error {
	if srcID == dstID {
		return errInvalid
	}
	return l.tx(ctx, func(t *sql.Tx) error { return mergeTopicsTx(t, srcID, dstID) })
}

// mergeTopicsTx funde src em dst dentro de t: itens, favoritos e aliases vão
// para dst, o título antigo vira alias e o (kind, slug) antigo redireciona.
func mergeTopicsTx(t *sql.Tx, srcID, dstID int64) error {
	{
		var srcKind, srcSlug, srcTitle string
		if err := t.QueryRow(`SELECT kind, slug, title FROM topics WHERE id=?`, srcID).Scan(&srcKind, &srcSlug, &srcTitle); err != nil {
			return errNotFound
		}
		var n int
		t.QueryRow(`SELECT COUNT(*) FROM topics WHERE id=?`, dstID).Scan(&n)
		if n == 0 {
			return errNotFound
		}
		ids := topicMediaIDs(t, srcID)
		stmts := []struct {
			q    string
			args []any
		}{
			{`INSERT OR IGNORE INTO topic_items(topic_id, media_id, sort_key, source) SELECT ?, media_id, sort_key, source FROM topic_items WHERE topic_id=?`, []any{dstID, srcID}},
			{`INSERT OR IGNORE INTO favorite_topics(user_id, topic_id, created_at) SELECT user_id, ?, created_at FROM favorite_topics WHERE topic_id=?`, []any{dstID, srcID}},
			{`INSERT OR IGNORE INTO topic_aliases(topic_id, alias) SELECT ?, alias FROM topic_aliases WHERE topic_id=?`, []any{dstID, srcID}},
			{`INSERT OR IGNORE INTO topic_aliases(topic_id, alias) VALUES (?,?)`, []any{dstID, srcTitle}},
			{`INSERT OR REPLACE INTO topic_redirects(kind, slug, topic_id) VALUES (?,?,?)`, []any{srcKind, srcSlug, dstID}},
			{`UPDATE topic_redirects SET topic_id=? WHERE topic_id=?`, []any{dstID, srcID}},
			{`DELETE FROM topics WHERE id=?`, []any{srcID}},
		}
		for _, s := range stmts {
			if _, err := t.Exec(s.q, s.args...); err != nil {
				return err
			}
		}
		return refreshFTSIDs(t, ids)
	}
}
