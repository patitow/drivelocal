package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// Tópicos extintos ("não é um criador válido"): o tópico some, os arquivos
// ficam, e (kind, slug) vai para topic_blocks para o scan e a IA não o
// recriarem. A foto do que havia permite desfazer.

var errTopicBlocked = errors.New("tópico extinto")

func topicBlocked(t *sql.Tx, kind, slug string) bool {
	var n int
	t.QueryRow(`SELECT COUNT(*) FROM topic_blocks WHERE kind=? AND slug=?`, kind, slug).Scan(&n)
	return n > 0
}

type topicSnapshot struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Source      string   `json:"source"`
	Aliases     []string `json:"aliases,omitempty"`
	Favorites   []string `json:"favorites,omitempty"`
	Redirects   []string `json:"redirects,omitempty"` // slugs fundidos que apontavam para ele
	Items       []struct {
		MediaID int64  `json:"m"`
		SortKey string `json:"k"`
		Source  string `json:"s"`
	} `json:"items"`
}

type TopicBlock struct {
	Kind      string `json:"kind"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Count     int    `json:"count"`
	Actor     string `json:"actor,omitempty"`
	CreatedAt int64  `json:"createdAt"`
}

// DissolveTopic extingue o tópico: guarda a foto, bloqueia o nome (e os nomes
// fundidos nele) e apaga o tópico. Os arquivos não são tocados.
func (l *Library) DissolveTopic(ctx context.Context, id int64, actor string) (*TopicBlock, error) {
	var out *TopicBlock
	err := l.tx(ctx, func(t *sql.Tx) error {
		var kind, slug string
		var snap topicSnapshot
		var desc sql.NullString
		if err := t.QueryRow(`SELECT kind, slug, title, description, source FROM topics WHERE id=?`, id).Scan(&kind, &slug, &snap.Title, &desc, &snap.Source); err != nil {
			return errNotFound
		}
		snap.Description = desc.String
		collect := func(q string, f func(*sql.Rows) error) error {
			rows, err := t.Query(q, id)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				if err := f(rows); err != nil {
					return err
				}
			}
			return rows.Err()
		}
		if err := collect(`SELECT alias FROM topic_aliases WHERE topic_id=?`, func(r *sql.Rows) error {
			var a string
			err := r.Scan(&a)
			snap.Aliases = append(snap.Aliases, a)
			return err
		}); err != nil {
			return err
		}
		if err := collect(`SELECT user_id FROM favorite_topics WHERE topic_id=?`, func(r *sql.Rows) error {
			var u string
			err := r.Scan(&u)
			snap.Favorites = append(snap.Favorites, u)
			return err
		}); err != nil {
			return err
		}
		if err := collect(`SELECT slug FROM topic_redirects WHERE topic_id=?`, func(r *sql.Rows) error {
			var s string
			err := r.Scan(&s)
			snap.Redirects = append(snap.Redirects, s)
			return err
		}); err != nil {
			return err
		}
		if err := collect(`SELECT media_id, sort_key, source FROM topic_items WHERE topic_id=?`, func(r *sql.Rows) error {
			var it struct {
				MediaID int64  `json:"m"`
				SortKey string `json:"k"`
				Source  string `json:"s"`
			}
			err := r.Scan(&it.MediaID, &it.SortKey, &it.Source)
			snap.Items = append(snap.Items, it)
			return err
		}); err != nil {
			return err
		}
		js, _ := json.Marshal(snap)
		now := nowUnix()
		if _, err := t.Exec(`INSERT OR REPLACE INTO topic_blocks(kind, slug, title, snapshot, actor, created_at) VALUES (?,?,?,?,?,?)`, kind, slug, snap.Title, string(js), actor, now); err != nil {
			return err
		}
		// nomes fundidos neste também não voltam (sem foto própria: desfazer pelo principal)
		for _, s := range snap.Redirects {
			if _, err := t.Exec(`INSERT OR IGNORE INTO topic_blocks(kind, slug, title, snapshot, actor, created_at) VALUES (?,?,?,'',?,?)`, kind, s, s, actor, now); err != nil {
				return err
			}
		}
		ids := topicMediaIDs(t, id)
		if _, err := t.Exec(`DELETE FROM topics WHERE id=?`, id); err != nil {
			return err
		}
		if _, err := t.Exec(`UPDATE topic_flags SET status='accepted', reviewed_by=?, reviewed_at=? WHERE topic_id=? AND status='pending'`, actor, now, id); err != nil {
			return err
		}
		out = &TopicBlock{Kind: kind, Slug: slug, Title: snap.Title, Count: len(snap.Items), Actor: actor, CreatedAt: now}
		return refreshFTSIDs(t, ids)
	})
	return out, err
}

// RestoreTopic desfaz a extinção: recria o tópico com os mesmos itens,
// apelidos, favoritos e redirecionamentos.
func (l *Library) RestoreTopic(ctx context.Context, kind, slug string) (int64, error) {
	var id int64
	err := l.tx(ctx, func(t *sql.Tx) error {
		var raw string
		if err := t.QueryRow(`SELECT snapshot FROM topic_blocks WHERE kind=? AND slug=?`, kind, slug).Scan(&raw); err != nil || raw == "" {
			return errNotFound
		}
		var snap topicSnapshot
		if err := json.Unmarshal([]byte(raw), &snap); err != nil {
			return err
		}
		var exists int
		t.QueryRow(`SELECT COUNT(*) FROM topics WHERE kind=? AND slug=?`, kind, slug).Scan(&exists)
		if exists > 0 {
			return errInvalid
		}
		res, err := t.Exec(`INSERT INTO topics(kind, slug, title, description, source, created_at) VALUES (?,?,?,NULLIF(?,''),?,?)`, kind, slug, snap.Title, snap.Description, snap.Source, nowUnix())
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		var ids []int64
		for _, it := range snap.Items {
			// arquivo que saiu da biblioteca nesse meio tempo fica de fora
			if _, err := t.Exec(`INSERT OR IGNORE INTO topic_items(topic_id, media_id, sort_key, source) SELECT ?,?,?,? WHERE EXISTS (SELECT 1 FROM media WHERE id=?)`, id, it.MediaID, it.SortKey, it.Source, it.MediaID); err != nil {
				return err
			}
			ids = append(ids, it.MediaID)
		}
		for _, a := range snap.Aliases {
			if _, err := t.Exec(`INSERT OR IGNORE INTO topic_aliases(topic_id, alias) VALUES (?,?)`, id, a); err != nil {
				return err
			}
		}
		for _, u := range snap.Favorites {
			if _, err := t.Exec(`INSERT OR IGNORE INTO favorite_topics(user_id, topic_id, created_at) VALUES (?,?,?)`, u, id, nowUnix()); err != nil {
				return err
			}
		}
		for _, s := range snap.Redirects {
			if _, err := t.Exec(`INSERT OR IGNORE INTO topic_redirects(kind, slug, topic_id) VALUES (?,?,?)`, kind, s, id); err != nil {
				return err
			}
			if _, err := t.Exec(`DELETE FROM topic_blocks WHERE kind=? AND slug=? AND snapshot=''`, kind, s); err != nil {
				return err
			}
		}
		if _, err := t.Exec(`DELETE FROM topic_blocks WHERE kind=? AND slug=?`, kind, slug); err != nil {
			return err
		}
		return refreshFTSIDs(t, ids)
	})
	return id, err
}

func (l *Library) ListTopicBlocks(ctx context.Context) ([]TopicBlock, error) {
	rows, err := l.r.QueryContext(ctx, `SELECT kind, slug, title, snapshot, COALESCE(actor,''), created_at FROM topic_blocks WHERE snapshot <> '' ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TopicBlock{}
	for rows.Next() {
		var b TopicBlock
		var raw string
		if err := rows.Scan(&b.Kind, &b.Slug, &b.Title, &raw, &b.Actor, &b.CreatedAt); err != nil {
			return nil, err
		}
		var snap topicSnapshot
		if json.Unmarshal([]byte(raw), &snap) == nil {
			b.Count = len(snap.Items)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SuggestTopic: quem não é admin sugere juntar (into > 0) ou extinguir; vai
// para a fila de Revisão.
// Só sugere sobre tópicos que o usuário enxerga (senão 404, sem revelar que existem);
// o que o admin já rejeitou só o admin reabre.
func (l *Library) SuggestTopic(ctx context.Context, v Viewer, id int64, user, action string, into int64) error {
	if !l.entryVisible(ctx, v, "topic:"+strconv.FormatInt(id, 10)) ||
		(action == "merge" && into != id && !l.entryVisible(ctx, v, "topic:"+strconv.FormatInt(into, 10))) {
		return errNotFound
	}
	return l.tx(ctx, func(t *sql.Tx) error {
		var kind string
		if err := t.QueryRow(`SELECT kind FROM topics WHERE id=?`, id).Scan(&kind); err != nil {
			return errNotFound
		}
		now := nowUnix()
		switch action {
		case "merge":
			var k2 string
			if into == id || t.QueryRow(`SELECT kind FROM topics WHERE id=?`, into).Scan(&k2) != nil || k2 != kind {
				return errInvalid
			}
			// par já sugerido (em qualquer ordem) volta para pendente
			res, err := t.Exec(`UPDATE topic_merge_suggestions SET status = CASE WHEN status='rejected' AND NOT ? THEN status ELSE 'pending' END, score=1, created_by=?
				WHERE (src_id=? AND dst_id=?) OR (src_id=? AND dst_id=?)`, v.IsAdmin, user, id, into, into, id)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				return nil
			}
			_, err = t.Exec(`INSERT INTO topic_merge_suggestions(src_id, dst_id, score, method, created_at, created_by) VALUES (?,?,1,'user',?,?)`, id, into, now, user)
			return err
		case "invalid":
			_, err := t.Exec(`INSERT INTO topic_flags(topic_id, created_by, created_at) VALUES (?,?,?)
				ON CONFLICT(topic_id, created_by) DO UPDATE SET status = CASE WHEN topic_flags.status='rejected' AND NOT ? THEN topic_flags.status ELSE 'pending' END,
				created_at=excluded.created_at`, id, user, now, v.IsAdmin)
			return err
		}
		return errInvalid
	})
}

type TopicFlag struct {
	TopicID int64    `json:"topicId"`
	Kind    string   `json:"kind"`
	Title   string   `json:"title"`
	Count   int      `json:"count"`
	By      []string `json:"by"`
}

func (l *Library) TopicFlags(ctx context.Context) ([]TopicFlag, error) {
	rows, err := l.r.QueryContext(ctx, `SELECT f.topic_id, tp.kind, tp.title, (SELECT COUNT(*) FROM topic_items WHERE topic_id = tp.id), group_concat(COALESCE(f.created_by,''), ',')
		FROM topic_flags f JOIN topics tp ON tp.id = f.topic_id WHERE f.status='pending' GROUP BY f.topic_id ORDER BY MAX(f.created_at) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TopicFlag{}
	for rows.Next() {
		var f TopicFlag
		var by string
		if err := rows.Scan(&f.TopicID, &f.Kind, &f.Title, &f.Count, &by); err != nil {
			return nil, err
		}
		for _, b := range strings.Split(by, ",") {
			if b != "" {
				f.By = append(f.By, b)
			}
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// DismissTopicFlags: o admin decidiu manter o tópico.
func (l *Library) DismissTopicFlags(ctx context.Context, topicID int64, actor string) error {
	_, err := l.w.ExecContext(ctx, `UPDATE topic_flags SET status='rejected', reviewed_by=?, reviewed_at=? WHERE topic_id=? AND status='pending'`, actor, nowUnix(), topicID)
	return err
}
