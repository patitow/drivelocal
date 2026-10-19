package main

// Medição da detecção de tópicos duplicados sobre dados reais. Só roda com
// DEDUPE_EVAL_DIR apontando para uma pasta (fora do git) com:
//   library.db       cópia do banco
//   labels.json      {"clusters": [[ids...]], "uncertain": [[[ids],[ids]]]}
//   embeddings.json  {"<id>": [vetor]} (opcional; bge-m3 via hub)
// Imprime threshold × precisão/recall para string, embedding e combinações.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"
)

type evalPair struct {
	a, b     int64
	pos      bool
	str, emb float64
	ta, tb   string
}

func TestTopicDedupeEval(t *testing.T) {
	dir := os.Getenv("DEDUPE_EVAL_DIR")
	if dir == "" {
		t.Skip("DEDUPE_EVAL_DIR não definido")
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dir, "library.db"))+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT tp.id, tp.kind, tp.title,
		(SELECT COUNT(*) FROM topic_items ti JOIN media m ON m.id = ti.media_id WHERE ti.topic_id = tp.id AND m.missing = 0) FROM topics tp`)
	if err != nil {
		t.Fatal(err)
	}
	var topics []dupTopic
	for rows.Next() {
		var d dupTopic
		rows.Scan(&d.ID, &d.Kind, &d.Title, &d.Count)
		if d.Count > 0 {
			d.Key = normalizeTopicName(d.Title)
			topics = append(topics, d)
		}
	}
	rows.Close()

	var labels struct {
		Clusters  [][]int64    `json:"clusters"`
		Uncertain [][2][]int64 `json:"uncertain"`
	}
	b, err := os.ReadFile(filepath.Join(dir, "labels.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &labels); err != nil {
		t.Fatal(err)
	}
	key := func(a, b int64) [2]int64 {
		if a > b {
			a, b = b, a
		}
		return [2]int64{a, b}
	}
	pos, skip := map[[2]int64]bool{}, map[[2]int64]bool{}
	for _, c := range labels.Clusters {
		for i := range c {
			for j := i + 1; j < len(c); j++ {
				pos[key(c[i], c[j])] = true
			}
		}
	}
	for _, u := range labels.Uncertain {
		for _, x := range u[0] {
			for _, y := range u[1] {
				skip[key(x, y)] = true
			}
		}
	}
	emb := map[int64][]float64{}
	if b, err := os.ReadFile(filepath.Join(dir, "embeddings.json")); err == nil {
		raw := map[string][]float64{}
		json.Unmarshal(b, &raw)
		for k, v := range raw {
			id, _ := strconv.ParseInt(k, 10, 64)
			emb[id] = v
		}
	}
	var pairs []evalPair
	nPos := 0
	for i := range topics {
		for j := i + 1; j < len(topics); j++ {
			a, c := topics[i], topics[j]
			if a.Kind != c.Kind || skip[key(a.ID, c.ID)] {
				continue
			}
			p := evalPair{a: a.ID, b: c.ID, pos: pos[key(a.ID, c.ID)], ta: a.Title, tb: c.Title}
			p.str = topicKeyScore(a.Key, c.Key)
			p.emb = cosine(emb[a.ID], emb[c.ID])
			if p.pos {
				nPos++
			}
			pairs = append(pairs, p)
		}
	}
	t.Logf("tópicos=%d pares=%d positivos=%d (incertos excluídos=%d)", len(topics), len(pairs), nPos, len(skip))

	report := func(name string, ths []float64, pred func(p evalPair, th float64) bool) {
		t.Logf("%-28s %6s %6s %6s %4s %4s %4s", name, "th", "prec", "recall", "TP", "FP", "FN")
		for _, th := range ths {
			tp, fp := 0, 0
			for _, p := range pairs {
				if pred(p, th) {
					if p.pos {
						tp++
					} else {
						fp++
					}
				}
			}
			prec := 1.0
			if tp+fp > 0 {
				prec = float64(tp) / float64(tp+fp)
			}
			t.Logf("%-28s %6.2f %6.3f %6.3f %4d %4d %4d", "", th, prec, float64(tp)/float64(max(nPos, 1)), tp, fp, nPos-tp)
		}
	}
	ths := []float64{0.70, 0.75, 0.80, 0.84, 0.86, 0.88, 0.90, 0.92, 0.95, 0.98, 1.0}
	report("string (normalizado)", ths, func(p evalPair, th float64) bool { return p.str >= th })
	if len(emb) > 0 {
		report("embedding bge-m3 (cosseno)", []float64{0.70, 0.75, 0.80, 0.85, 0.88, 0.90, 0.92, 0.95}, func(p evalPair, th float64) bool { return p.emb >= th })
		report("média string+embedding", ths, func(p evalPair, th float64) bool { return (p.str+p.emb)/2 >= th })
		report("string>=th OU emb>=0.92", ths, func(p evalPair, th float64) bool { return p.str >= th || p.emb >= 0.92 })
		report("string>=th E emb>=0.75", ths, func(p evalPair, th float64) bool { return p.str >= th && p.emb >= 0.75 })
	}
	if os.Getenv("DEDUPE_EVAL_VERBOSE") != "" {
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].str > pairs[j].str })
		for _, p := range pairs {
			if (p.pos && p.str < topicDupReview) || (!p.pos && p.str >= 0.80) {
				t.Logf("pos=%v str=%.3f emb=%.3f  %d %q | %d %q", p.pos, p.str, p.emb, p.a, p.ta, p.b, p.tb)
			}
		}
	}
	fmt.Fprint(os.Stderr)
}

func cosine(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var d, na, nb float64
	for i := range a {
		d += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return d / math.Sqrt(na*nb)
}

// TestHomeSnapshotSmoke roda home e busca agrupada sobre uma cópia do banco real
// (mesmo DEDUPE_EVAL_DIR). Só imprime contagens e tempos, nunca títulos.
func TestHomeSnapshotSmoke(t *testing.T) {
	dir := os.Getenv("DEDUPE_EVAL_DIR")
	if dir == "" {
		t.Skip("DEDUPE_EVAL_DIR não definido")
	}
	src, err := os.ReadFile(filepath.Join(dir, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "library.db"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := OpenLibrary(tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	for _, v := range []Viewer{{UserID: "smoke-admin", IsAdmin: true, MaxMaturity: MaturityExplicito}, {UserID: "smoke-user", MaxMaturity: MaturityGeral}} {
		start := time.Now()
		h, err := l.Home(ctx, v)
		if err != nil {
			t.Fatalf("home %s: %v", v.UserID, err)
		}
		var parts []string
		for _, s := range h.Shelves {
			parts = append(parts, fmt.Sprintf("%s=%d", s.ID, len(s.Items)+len(s.Topics)+len(s.Playlists)))
		}
		t.Logf("home %s em %v: %v", v.UserID, time.Since(start).Round(time.Millisecond), parts)
		for _, q := range []string{"a", "e", "1080p", "ep"} {
			start = time.Now()
			res, err := l.SearchGrouped(ctx, v, ItemQuery{Q: q})
			if err != nil {
				t.Fatalf("busca %q: %v", q, err)
			}
			series := 0
			for _, g := range res.Groups {
				if g.Type == "series" {
					series++
				}
			}
			t.Logf("busca %q %s em %v: total=%d grupos=%d séries=%d", q, v.UserID, time.Since(start).Round(time.Millisecond), res.Total, len(res.Groups), series)
		}
	}
}
