package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

// ---------- progresso de leitura de mangá (por usuário) ----------

// MangaReadState: posição na obra; Done são os capítulos já terminados e At
// (ms) ordena "lendo agora".
type MangaReadState struct {
	Chapter string   `json:"chapter"`
	Page    int      `json:"page"`
	Pages   int      `json:"pages"`
	Done    []string `json:"done"`
	At      int64    `json:"at"`
}

const (
	mangaMaxBatch = 500
	mangaMaxStr   = 1024
	mangaMaxDone  = 5000
)

func (l *Library) MangaProgress(ctx context.Context, userID string) (map[string]MangaReadState, error) {
	rows, err := l.r.QueryContext(ctx, `SELECT work, chapter, page, pages, done, updated_at FROM manga_progress WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]MangaReadState{}
	for rows.Next() {
		var work, done string
		var s MangaReadState
		if err := rows.Scan(&work, &s.Chapter, &s.Page, &s.Pages, &done, &s.At); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(done), &s.Done) != nil || s.Done == nil {
			s.Done = []string{}
		}
		out[work] = s
	}
	return out, rows.Err()
}

// SaveMangaProgress grava várias obras de uma vez (a migração do navegador
// manda tudo); estado mais antigo que o salvo é ignorado.
func (l *Library) SaveMangaProgress(ctx context.Context, userID string, in map[string]MangaReadState) error {
	if len(in) > mangaMaxBatch {
		return errInvalid
	}
	for work, s := range in {
		if work == "" || len(work) > mangaMaxStr || len(s.Chapter) > mangaMaxStr || len(s.Done) > mangaMaxDone || s.Page < 0 || s.Pages < 0 {
			return errInvalid
		}
	}
	return l.tx(ctx, func(t *sql.Tx) error {
		st, err := t.Prepare(`INSERT INTO manga_progress(user_id, work, chapter, page, pages, done, updated_at) VALUES (?,?,?,?,?,?,?)
			ON CONFLICT(user_id, work) DO UPDATE SET chapter=excluded.chapter, page=excluded.page, pages=excluded.pages,
			done=excluded.done, updated_at=excluded.updated_at WHERE excluded.updated_at >= manga_progress.updated_at`)
		if err != nil {
			return err
		}
		defer st.Close()
		for work, s := range in {
			if s.Done == nil {
				s.Done = []string{}
			}
			if s.At <= 0 {
				s.At = time.Now().UnixMilli()
			}
			done, _ := json.Marshal(s.Done)
			if _, err := st.Exec(userID, work, s.Chapter, s.Page, s.Pages, string(done), s.At); err != nil {
				return err
			}
		}
		return nil
	})
}

func libMangaProgressHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	uid := r.Header.Get("X-User-ID")
	if r.Method == http.MethodPut {
		var in map[string]MangaReadState
		if err := decodeBody(r, &in); err != nil {
			libFail(w, err)
			return
		}
		if err := l.SaveMangaProgress(r.Context(), uid, in); err != nil {
			libFail(w, err)
			return
		}
	}
	p, err := l.MangaProgress(r.Context(), uid)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, p)
}

// ---------- novidades dos criadores ----------

// CreatorNews: criadores com item mais novo que a última visita do usuário à
// estante (sem visita registrada, conta a última semana).
type CreatorNews struct {
	SeenAt    int64   `json:"seenAt"`
	Since     int64   `json:"since"`
	Count     int     `json:"count"`
	Favorites int     `json:"favorites"` // dos criadores favoritos
	IDs       []int64 `json:"ids"`
}

func (l *Library) CreatorNews(ctx context.Context, v Viewer) (CreatorNews, error) {
	n := CreatorNews{IDs: []int64{}}
	l.r.QueryRowContext(ctx, `SELECT creators_seen_at FROM user_prefs WHERE user_id = ?`, v.UserID).Scan(&n.SeenAt)
	n.Since = n.SeenAt
	if n.Since <= 0 {
		n.Since = time.Now().Add(-7 * 24 * time.Hour).Unix()
	}
	vis, vargs := v.visibleSQL(false)
	rows, err := l.r.QueryContext(ctx, `SELECT tp.id, EXISTS (SELECT 1 FROM favorite_topics ft WHERE ft.topic_id = tp.id AND ft.user_id = ?)
		FROM topics tp WHERE tp.kind = 'creator' AND EXISTS (
			SELECT 1 FROM topic_items ti JOIN media m ON m.id = ti.media_id
			WHERE ti.topic_id = tp.id AND m.mtime > ? AND `+vis+`)`, append([]any{v.UserID, n.Since}, vargs...)...)
	if err != nil {
		return n, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var fav int
		if err := rows.Scan(&id, &fav); err != nil {
			return n, err
		}
		n.IDs = append(n.IDs, id)
		n.Count++
		if fav == 1 {
			n.Favorites++
		}
	}
	return n, rows.Err()
}

func (l *Library) MarkCreatorsSeen(ctx context.Context, userID string) error {
	return l.tx(ctx, func(t *sql.Tx) error {
		_, err := t.Exec(`INSERT INTO user_prefs(user_id, max_maturity, creators_seen_at, updated_at) VALUES (?,?,?,?)
			ON CONFLICT(user_id) DO UPDATE SET creators_seen_at=excluded.creators_seen_at`, userID, MaturityGeral, nowUnix(), nowUnix())
		return err
	})
}

// GET: contagem de novidades; POST: marca a estante como vista agora
// (devolve o estado anterior, para a tela ainda destacar o que era novo).
func libCreatorNewsHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	v := l.viewer(r)
	n, err := l.CreatorNews(r.Context(), v)
	if err != nil {
		libFail(w, err)
		return
	}
	if r.Method == http.MethodPost {
		if err := l.MarkCreatorsSeen(r.Context(), v.UserID); err != nil {
			libFail(w, err)
			return
		}
	}
	libJSON(w, n)
}
