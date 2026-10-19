package main

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"sync"
	"time"
)

// Feed "Para você" da home: os vídeos visíveis ranqueados por afinidade (o que
// o usuário assiste e favorita), novidade e um pouco de acaso, sem empilhar o
// mesmo criador ou série. A ordem sai uma vez por (usuário, escopo, semente) e
// as páginas vêm do cache; outra semente embaralha de novo.

type FeedItem struct {
	ItemDTO
	Reason     string `json:"reason"`
	ReasonKind string `json:"reasonKind"` // favorite | next | affinity | new | discover
	// tópico que motivou a recomendação (abre a série/criador)
	ReasonTopic *TopicMini `json:"reasonTopic,omitempty"`
}

type FeedPage struct {
	Items []FeedItem `json:"items"`
	Next  int        `json:"next"` // offset da próxima página; -1 no fim
}

const (
	feedTTL      = 15 * time.Minute
	feedMax      = 1500 // itens ranqueados por semente
	feedWindow   = 40   // quantos candidatos de cada raiz a diversidade olha por posição
	feedHalfLife = 30.0 // dias: peso de algo assistido cai pela metade
)

type feedRanked struct {
	id         int64
	reason     string
	reasonKind string
	topic      *TopicMini
}

type feedEntry struct {
	at   time.Time
	list []feedRanked
}

var (
	feedCache sync.Map // homeKey|escopo|semente -> feedEntry
	feedBuild sync.Mutex
)

var feedScopes = map[string]bool{"": true, "all": true, "series": true, "creators": true, "films": true}

func (l *Library) Feed(ctx context.Context, v Viewer, scope string, seed int64, offset, limit int) (*FeedPage, error) {
	if !feedScopes[scope] {
		return nil, fmt.Errorf("%w: escopo inválido", errInvalid)
	}
	limit = clampLimit(limit, 24, 60)
	if offset < 0 {
		offset = 0
	}
	key := fmt.Sprintf("%s|%s|%d", l.homeKey(v), scope, seed)
	var list []feedRanked
	if e, ok := feedCache.Load(key); ok && time.Since(e.(feedEntry).at) < feedTTL {
		list = e.(feedEntry).list
	} else {
		// uma montagem por vez: a primeira página costuma chegar em rajada
		feedBuild.Lock()
		if e, ok := feedCache.Load(key); ok && time.Since(e.(feedEntry).at) < feedTTL {
			list = e.(feedEntry).list
		} else {
			var err error
			list, err = l.rankFeed(ctx, v, scope, seed)
			if err != nil {
				feedBuild.Unlock()
				return nil, err
			}
			feedCache.Range(func(k, e any) bool {
				if time.Since(e.(feedEntry).at) >= feedTTL {
					feedCache.Delete(k)
				}
				return true
			})
			feedCache.Store(key, feedEntry{at: time.Now(), list: list})
		}
		feedBuild.Unlock()
	}

	out := &FeedPage{Items: []FeedItem{}, Next: -1}
	if offset >= len(list) {
		return out, nil
	}
	end := min(offset+limit, len(list))
	page := list[offset:end]
	if end < len(list) {
		out.Next = end
	}
	ids := make([]any, len(page))
	for i, r := range page {
		ids[i] = r.id
	}
	vis, vargs := v.visibleSQL(false)
	rows, err := l.r.QueryContext(ctx, `SELECT `+itemCols+` FROM media m WHERE m.id IN (`+placeholders(len(ids))+`) AND `+vis, append(ids, vargs...)...)
	if err != nil {
		return nil, err
	}
	byID := map[int64]ItemDTO{}
	var items []ItemDTO
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := l.hydrate(ctx, v, items); err != nil {
		return nil, err
	}
	for _, it := range items {
		byID[it.ID] = it
	}
	// na ordem do ranking; o que sumiu desde a montagem fica de fora
	for _, r := range page {
		if it, ok := byID[r.id]; ok {
			out.Items = append(out.Items, FeedItem{ItemDTO: it, Reason: r.reason, ReasonKind: r.reasonKind, ReasonTopic: r.topic})
		}
	}
	return out, nil
}

// feedRand: acaso estável por semente, para a página 2 continuar a ordem da 1.
func feedRand(seed, id int64) float64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "%d:%d", seed, id)
	return float64(h.Sum64()%1_000_000) / 1_000_000
}

func (l *Library) rankFeed(ctx context.Context, v Viewer, scope string, seed int64) ([]feedRanked, error) {
	now := float64(nowUnix())
	decay := func(at int64) float64 { return math.Pow(0.5, (now-float64(at))/86400/feedHalfLife) }

	// Tópicos (séries e criadores) e os itens de cada um.
	topics := map[int64]TopicMini{}
	rows, err := l.r.QueryContext(ctx, `SELECT id, kind, title FROM topics WHERE kind IN ('series','creator')`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t TopicMini
		if err := rows.Scan(&t.ID, &t.Kind, &t.Title); err != nil {
			rows.Close()
			return nil, err
		}
		topics[t.ID] = t
	}
	rows.Close()
	type link struct {
		topic int64
		sort  string
	}
	itemTopics := map[int64][]link{}
	rows, err = l.r.QueryContext(ctx, `SELECT ti.media_id, ti.topic_id, ti.sort_key FROM topic_items ti JOIN topics tp ON tp.id = ti.topic_id WHERE tp.kind IN ('series','creator')`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var mid int64
		var lk link
		if err := rows.Scan(&mid, &lk.topic, &lk.sort); err != nil {
			rows.Close()
			return nil, err
		}
		itemTopics[mid] = append(itemTopics[mid], lk)
	}
	rows.Close()

	// Afinidade bruta por tópico e por pasta raiz: o que foi assistido (quanto
	// mais completo e recente, mais pesa) e o que foi favoritado.
	raw := map[int64]float64{}
	rootRaw := map[string]float64{}
	started := map[int64]bool{} // tópicos que o usuário já começou
	rows, err = l.r.QueryContext(ctx, `SELECT wp.media_id, m.root, wp.completed, wp.position_s, wp.duration_s, wp.updated_at
		FROM watch_progress wp JOIN media m ON m.id = wp.media_id WHERE wp.user_id = ?`, v.UserID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var mid, at int64
		var root string
		var done bool
		var pos, dur float64
		if err := rows.Scan(&mid, &root, &done, &pos, &dur, &at); err != nil {
			rows.Close()
			return nil, err
		}
		eng := 1.0
		if !done {
			eng = 0.3
			if dur > 0 {
				eng += 0.7 * math.Min(pos/dur, 1)
			}
		}
		w := eng * decay(at)
		rootRaw[root] += w
		for _, lk := range itemTopics[mid] {
			raw[lk.topic] += w
			started[lk.topic] = true
		}
	}
	rows.Close()
	rows, err = l.r.QueryContext(ctx, `SELECT f.media_id, m.root FROM favorites f JOIN media m ON m.id = f.media_id WHERE f.user_id = ?`, v.UserID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var mid int64
		var root string
		if err := rows.Scan(&mid, &root); err != nil {
			rows.Close()
			return nil, err
		}
		rootRaw[root] += 1
		for _, lk := range itemTopics[mid] {
			raw[lk.topic] += 1.5
		}
	}
	rows.Close()
	favTopic := map[int64]bool{}
	rows, err = l.r.QueryContext(ctx, `SELECT topic_id FROM favorite_topics WHERE user_id = ?`, v.UserID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		favTopic[id] = true
		raw[id] += 4
	}
	rows.Close()
	// satura em 0..1: dez vídeos de um criador não valem dez vezes um
	aff := func(x float64) float64 { return 1 - math.Exp(-x/3) }
	var rootTotal float64
	for _, x := range rootRaw {
		rootTotal += x
	}

	// Candidatos: vídeos visíveis ainda não vistos (os começados estão no
	// "Continuar assistindo").
	type cand struct {
		id     int64
		root   string
		score  float64
		topic  int64 // tópico que melhor explica a recomendação (0 = nenhum)
		group  int64 // série/criador para a diversidade
		reason string
		rkind  string
	}
	vis, vargs := v.visibleSQL(false)
	rows, err = l.r.QueryContext(ctx, `SELECT m.id, m.root, m.mtime, COALESCE(wp.completed, 0), COALESCE(wp.position_s, 0)
		FROM media m LEFT JOIN watch_progress wp ON wp.media_id = m.id AND wp.user_id = ?
		WHERE m.kind = 'video' AND `+vis, append([]any{v.UserID}, vargs...)...)
	if err != nil {
		return nil, err
	}
	type seriesNext struct {
		sort string
		idx  int
	}
	nextOf := map[int64]seriesNext{} // série -> primeiro episódio não visto
	var cands []cand
	for rows.Next() {
		var id, mtime int64
		var root string
		var done bool
		var pos float64
		if err := rows.Scan(&id, &root, &mtime, &done, &pos); err != nil {
			rows.Close()
			return nil, err
		}
		if done || pos >= 10 {
			continue
		}
		var series, creator int64
		var seriesSort string
		for _, lk := range itemTopics[id] {
			switch topics[lk.topic].Kind {
			case "series":
				series, seriesSort = lk.topic, lk.sort
			case "creator":
				if creator == 0 || raw[lk.topic] > raw[creator] {
					creator = lk.topic
				}
			}
		}
		switch scope {
		case "series":
			if series == 0 && root != "Animes" {
				continue
			}
		case "creators":
			if creator == 0 {
				continue
			}
		case "films":
			if root != "Filmes" {
				continue
			}
		}
		c := cand{id: id, root: root, group: creator}
		if series != 0 {
			c.group = series
		}
		best := series
		if best == 0 || raw[creator] > raw[series] {
			best = creator
		}
		a := 0.0
		if best != 0 {
			a = aff(raw[best])
		}
		ra := 0.0
		if rootTotal > 0 {
			ra = rootRaw[root] / rootTotal
		}
		ageDays := (now - float64(mtime)) / 86400
		fresh := math.Exp(-math.Max(ageDays, 0) / 30)
		c.score = 0.5*a + 0.2*fresh + 0.1*ra + 0.25*feedRand(seed, id)
		switch {
		case best != 0 && favTopic[best]:
			c.topic, c.rkind, c.reason = best, "favorite", "De "+topics[best].Title+", nos seus favoritos"
		case best != 0 && best == series && started[series]:
			c.topic, c.rkind, c.reason = series, "next", "Próximo de "+topics[series].Title
		case best != 0 && a >= 0.15:
			c.topic, c.rkind, c.reason = best, "affinity", "Porque você assiste "+topics[best].Title
		case ageDays <= 14:
			c.topic, c.rkind, c.reason = best, "new", "Novo na coleção"
		default:
			c.topic, c.rkind, c.reason = best, "discover", "Para descobrir"
		}
		// Série entra com um episódio só: o primeiro ainda não visto.
		if series != 0 {
			if n, ok := nextOf[series]; ok {
				if seriesSort < n.sort {
					cands[n.idx] = c
					nextOf[series] = seriesNext{seriesSort, n.idx}
				}
				continue
			}
			nextOf[series] = seriesNext{seriesSort, len(cands)}
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Uma fila por pasta raiz, cada uma na ordem da pontuação: assim os
	// criadores (muito mais numerosos) não enterram animes e filmes.
	sort.Slice(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	byRoot := map[string][]cand{}
	var roots []string
	for _, c := range cands {
		if _, ok := byRoot[c.root]; !ok {
			roots = append(roots, c.root)
		}
		if len(byRoot[c.root]) < feedMax {
			byRoot[c.root] = append(byRoot[c.root], c)
		}
	}
	// Diversidade: a cada posição, entre os primeiros de cada fila, o melhor com
	// penalidade pelo que já apareceu do mesmo grupo e da mesma raiz (mais
	// forte se foi agora há pouco).
	used := map[int64]int{}
	var recent []int64
	var recentRoots []string
	out := make([]feedRanked, 0, feedMax)
	for len(out) < feedMax {
		br, bi, bs := "", -1, math.Inf(-1)
		for _, root := range roots {
			q := byRoot[root]
			rootPen := 1.0
			for _, r := range recentRoots {
				if r == root {
					rootPen *= 0.75
				}
			}
			for j := 0; j < len(q) && j < feedWindow; j++ {
				c := q[j]
				s := c.score * rootPen
				if c.group != 0 {
					s *= math.Pow(0.85, float64(used[c.group]))
					for _, g := range recent {
						if g == c.group {
							s *= 0.4
						}
					}
				}
				if s > bs {
					br, bi, bs = root, j, s
				}
			}
		}
		if bi < 0 {
			break
		}
		q := byRoot[br]
		c := q[bi]
		byRoot[br] = append(q[:bi], q[bi+1:]...)
		r := feedRanked{id: c.id, reason: c.reason, reasonKind: c.rkind}
		if c.topic != 0 {
			t := topics[c.topic]
			r.topic = &t
		}
		out = append(out, r)
		if c.group != 0 {
			used[c.group]++
		}
		recent = append(recent, c.group)
		if len(recent) > 4 {
			recent = recent[1:]
		}
		recentRoots = append(recentRoots, c.root)
		if len(recentRoots) > 4 {
			recentRoots = recentRoots[1:]
		}
	}
	return out, nil
}
