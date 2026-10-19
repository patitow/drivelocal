package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"unicode"
)

type TagRef struct {
	ID     int64  `json:"id"`
	Slug   string `json:"slug"`
	Label  string `json:"label"`
	Facet  string `json:"facet"`
	Source string `json:"source,omitempty"`
}

type TopicMini struct {
	ID    int64  `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
}

type ProgressDTO struct {
	PositionS     float64 `json:"positionS"`
	DurationS     float64 `json:"durationS"`
	Completed     bool    `json:"completed"`
	AudioTrack    string  `json:"audioTrack,omitempty"`
	SubtitleTrack string  `json:"subtitleTrack,omitempty"`
	UpdatedAt     int64   `json:"updatedAt"`
}

type ItemDTO struct {
	ID        int64        `json:"id"`
	Path      string       `json:"path"`
	Name      string       `json:"name"`
	Title     string       `json:"title"`
	Kind      string       `json:"kind"`
	Root      string       `json:"root"`
	Size      int64        `json:"size"`
	Mtime     int64        `json:"mtime"`
	DurationS *float64     `json:"durationS,omitempty"`
	Season    *int64       `json:"season,omitempty"`
	Episode   *float64     `json:"episode,omitempty"`
	Category  *string      `json:"category,omitempty"`
	Maturity  string       `json:"maturity"`
	Missing   bool         `json:"missing,omitempty"`
	Broken    string       `json:"broken,omitempty"` // motivo, quando o ffprobe não abre o arquivo
	Favorite  bool         `json:"favorite"`
	Hidden    bool         `json:"hidden,omitempty"`
	Progress  *ProgressDTO `json:"progress,omitempty"`
	Tags      []TagRef     `json:"tags"`
	Topics    []TopicMini  `json:"topics"`
	Part      string       `json:"part,omitempty"` // parte da série pela pasta (só na página da série)
}

type ItemQuery struct {
	Q          string   `json:"q,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Root       string   `json:"root,omitempty"`
	PathPrefix string   `json:"pathPrefix,omitempty"`
	Tags       []string `json:"tags,omitempty"` // slugs (E)
	TopicID    int64    `json:"topicId,omitempty"`
	Maturity   string   `json:"maturity,omitempty"`
	Category   string   `json:"category,omitempty"`
	Favorites  bool     `json:"favorites,omitempty"`
	Unwatched  bool     `json:"unwatched,omitempty"`
	MaxDurS    float64  `json:"maxDurationS,omitempty"`
	MinDurS    float64  `json:"minDurationS,omitempty"`
	Since      int64    `json:"since,omitempty"`      // mtime (unix s) mínimo: "adicionados esta semana"
	InProgress bool     `json:"inProgress,omitempty"` // começados e não terminados pelo usuário
	OnlyHidden bool     `json:"onlyHidden,omitempty"`
	Sort       string   `json:"sort,omitempty"` // relevance | recent | title | size | episode | random | added
	Limit      int      `json:"limit,omitempty"`
	Offset     int      `json:"offset,omitempty"`
	maxLimit   int      // uso interno: teto acima dos 500 da API (agrupamento)
	noManga    bool     // uso interno: sem páginas de mangá (têm estante própria)
}

type ItemPage struct {
	Items  []ItemDTO `json:"items"`
	Total  int       `json:"total"`
	Limit  int       `json:"limit"`
	Offset int       `json:"offset"`
}

// ftsQuery transforma texto livre em consulta FTS5 segura: cada palavra vira
// prefixo entre aspas, todas obrigatórias.
func ftsQuery(q string) string {
	var toks []string
	cur := strings.Builder{}
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, `"`+cur.String()+`"*`)
			cur.Reset()
		}
	}
	for _, r := range q {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	if len(toks) > 12 {
		toks = toks[:12]
	}
	return strings.Join(toks, " ")
}

const itemCols = `m.id, m.path, m.name, COALESCE(m.title_manual, m.title), m.kind, m.root, m.size, m.mtime, m.duration_s, m.season, m.episode, m.category, m.maturity, m.missing, COALESCE(m.broken, '')`

func scanItem(sc interface{ Scan(...any) error }) (ItemDTO, error) {
	var it ItemDTO
	var dur, ep sql.NullFloat64
	var season sql.NullInt64
	var cat sql.NullString
	var mat, missing int
	if err := sc.Scan(&it.ID, &it.Path, &it.Name, &it.Title, &it.Kind, &it.Root, &it.Size, &it.Mtime, &dur, &season, &ep, &cat, &mat, &missing, &it.Broken); err != nil {
		return it, err
	}
	if dur.Valid {
		it.DurationS = &dur.Float64
	}
	if season.Valid {
		it.Season = &season.Int64
	}
	if ep.Valid {
		it.Episode = &ep.Float64
	}
	if cat.Valid {
		it.Category = &cat.String
	}
	it.Maturity = maturityName(mat)
	it.Missing = missing == 1
	it.Tags = []TagRef{}
	it.Topics = []TopicMini{}
	return it, nil
}

// buildItemWhere monta o WHERE comum à listagem, busca e facetas.
func buildItemWhere(v Viewer, q ItemQuery) (joins, where string, args []any) {
	vis, vargs := v.visibleSQL(q.OnlyHidden)
	conds := []string{vis}
	args = append(args, vargs...)
	if q.OnlyHidden {
		conds = append(conds, "EXISTS (SELECT 1 FROM user_hidden uh2 WHERE uh2.user_id = ? AND uh2.media_id = m.id)")
		args = append(args, v.UserID)
	}
	if q.noManga {
		// mesmo critério da estante de mangás (Mangas)
		conds = append(conds, "NOT (m.kind = 'image' AND m.path LIKE '%mang%')")
	}
	if fq := ftsQuery(q.Q); fq != "" {
		joins = " JOIN media_fts f ON f.rowid = m.id"
		conds = append(conds, "media_fts MATCH ?")
		args = append(args, fq)
	}
	if q.Kind != "" {
		conds = append(conds, "m.kind = ?")
		args = append(args, q.Kind)
	}
	if q.Root != "" {
		conds = append(conds, "m.root = ?")
		args = append(args, q.Root)
	}
	if p := normalizePolicyPrefix(q.PathPrefix); p != "" {
		conds = append(conds, `(m.path = ? OR m.path LIKE ? ESCAPE '\')`)
		args = append(args, p, likePrefix(p)+"/%")
	}
	for _, t := range q.Tags {
		conds = append(conds, "EXISTS (SELECT 1 FROM media_tags mt JOIN tags tg ON tg.id = mt.tag_id WHERE mt.media_id = m.id AND tg.slug = ?)")
		args = append(args, t)
	}
	if q.TopicID > 0 {
		conds = append(conds, "EXISTS (SELECT 1 FROM topic_items ti WHERE ti.media_id = m.id AND ti.topic_id = ?)")
		args = append(args, q.TopicID)
	}
	if mt, ok := parseMaturity(q.Maturity); ok && q.Maturity != "" {
		conds = append(conds, "m.maturity = ?")
		args = append(args, mt)
	}
	if q.Category != "" {
		conds = append(conds, "m.category = ?")
		args = append(args, q.Category)
	}
	if q.Favorites {
		conds = append(conds, "EXISTS (SELECT 1 FROM favorites fv WHERE fv.user_id = ? AND fv.media_id = m.id)")
		args = append(args, v.UserID)
	}
	if q.Unwatched {
		conds = append(conds, "NOT EXISTS (SELECT 1 FROM watch_progress wp WHERE wp.user_id = ? AND wp.media_id = m.id AND wp.completed = 1)")
		args = append(args, v.UserID)
	}
	if q.MaxDurS > 0 {
		conds = append(conds, "m.duration_s IS NOT NULL AND m.duration_s <= ?")
		args = append(args, q.MaxDurS)
	}
	if q.MinDurS > 0 {
		conds = append(conds, "m.duration_s >= ?")
		args = append(args, q.MinDurS)
	}
	if q.Since > 0 {
		conds = append(conds, "m.mtime >= ?")
		args = append(args, q.Since)
	}
	if q.InProgress {
		conds = append(conds, "EXISTS (SELECT 1 FROM watch_progress wp2 WHERE wp2.user_id = ? AND wp2.media_id = m.id AND wp2.completed = 0 AND wp2.position_s > 0)")
		args = append(args, v.UserID)
	}
	return joins, " WHERE " + strings.Join(conds, " AND "), args
}

func orderClause(q ItemQuery) string {
	switch q.Sort {
	case "recent":
		return " ORDER BY m.mtime DESC, m.id DESC"
	case "added":
		return " ORDER BY m.first_seen DESC, m.id DESC"
	case "title":
		return " ORDER BY COALESCE(m.title_manual, m.title) COLLATE NOCASE, m.path"
	case "size":
		return " ORDER BY m.size DESC"
	case "random":
		return " ORDER BY random()"
	case "episode", "path":
		return " ORDER BY m.path COLLATE NOCASE"
	}
	if ftsQuery(q.Q) != "" {
		return " ORDER BY bm25(media_fts, 10.0, 2.0, 6.0, 4.0), m.id"
	}
	return " ORDER BY m.path COLLATE NOCASE"
}

func clampLimit(n, def, max int) int {
	if n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func (l *Library) QueryItems(ctx context.Context, v Viewer, q ItemQuery) (*ItemPage, error) {
	q.Limit = clampLimit(q.Limit, 60, max(500, q.maxLimit))
	if q.Offset < 0 {
		q.Offset = 0
	}
	joins, where, args := buildItemWhere(v, q)
	page := &ItemPage{Items: []ItemDTO{}, Limit: q.Limit, Offset: q.Offset}
	if err := l.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM media m`+joins+where, args...).Scan(&page.Total); err != nil {
		return nil, err
	}
	sqlq := `SELECT ` + itemCols + ` FROM media m` + joins + where
	if q.Sort == "episode" && q.TopicID > 0 {
		sqlq = `SELECT ` + itemCols + ` FROM media m JOIN topic_items tio ON tio.media_id = m.id AND tio.topic_id = ` + fmt.Sprint(q.TopicID) + joins + where + ` ORDER BY tio.sort_key, m.path`
	} else {
		sqlq += orderClause(q)
	}
	sqlq += ` LIMIT ? OFFSET ?`
	rows, err := l.r.QueryContext(ctx, sqlq, append(args, q.Limit, q.Offset)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		page.Items = append(page.Items, it)
	}
	rows.Close()
	if err := l.hydrate(ctx, v, page.Items); err != nil {
		return nil, err
	}
	return page, nil
}

// hydrate preenche tags, tópicos, favorito, oculto e progresso de uma página.
func (l *Library) hydrate(ctx context.Context, v Viewer, items []ItemDTO) error {
	if len(items) == 0 {
		return nil
	}
	idx := map[int64]int{}
	ids := make([]any, len(items))
	for i, it := range items {
		idx[it.ID] = i
		ids[i] = it.ID
	}
	ph := placeholders(len(ids))
	rows, err := l.r.QueryContext(ctx, `SELECT mt.media_id, tg.id, tg.slug, tg.label, tg.facet, mt.source FROM media_tags mt JOIN tags tg ON tg.id = mt.tag_id WHERE mt.media_id IN (`+ph+`) ORDER BY tg.facet, tg.label`, ids...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var mid int64
		var t TagRef
		if err := rows.Scan(&mid, &t.ID, &t.Slug, &t.Label, &t.Facet, &t.Source); err != nil {
			rows.Close()
			return err
		}
		items[idx[mid]].Tags = append(items[idx[mid]].Tags, t)
	}
	rows.Close()
	rows, err = l.r.QueryContext(ctx, `SELECT ti.media_id, tp.id, tp.kind, tp.title FROM topic_items ti JOIN topics tp ON tp.id = ti.topic_id WHERE ti.media_id IN (`+ph+`) ORDER BY tp.kind, tp.title`, ids...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var mid int64
		var t TopicMini
		if err := rows.Scan(&mid, &t.ID, &t.Kind, &t.Title); err != nil {
			rows.Close()
			return err
		}
		items[idx[mid]].Topics = append(items[idx[mid]].Topics, t)
	}
	rows.Close()
	uargs := append([]any{v.UserID}, ids...)
	rows, err = l.r.QueryContext(ctx, `SELECT media_id FROM favorites WHERE user_id = ? AND media_id IN (`+ph+`)`, uargs...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var mid int64
		rows.Scan(&mid)
		items[idx[mid]].Favorite = true
	}
	rows.Close()
	rows, err = l.r.QueryContext(ctx, `SELECT media_id FROM user_hidden WHERE user_id = ? AND media_id IN (`+ph+`)`, uargs...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var mid int64
		rows.Scan(&mid)
		items[idx[mid]].Hidden = true
	}
	rows.Close()
	rows, err = l.r.QueryContext(ctx, `SELECT media_id, position_s, duration_s, completed, COALESCE(audio_track,''), COALESCE(subtitle_track,''), updated_at FROM watch_progress WHERE user_id = ? AND media_id IN (`+ph+`)`, uargs...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var mid int64
		var p ProgressDTO
		var c int
		if err := rows.Scan(&mid, &p.PositionS, &p.DurationS, &c, &p.AudioTrack, &p.SubtitleTrack, &p.UpdatedAt); err != nil {
			rows.Close()
			return err
		}
		p.Completed = c == 1
		pp := p
		items[idx[mid]].Progress = &pp
	}
	rows.Close()
	return nil
}

// GetItem devolve um item visível para o viewer (errNotFound caso contrário).
func (l *Library) GetItem(ctx context.Context, v Viewer, id int64) (*ItemDTO, error) {
	vis, args := v.visibleSQL(true)
	row := l.r.QueryRowContext(ctx, `SELECT `+itemCols+` FROM media m WHERE m.id = ? AND `+vis, append([]any{id}, args...)...)
	it, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	items := []ItemDTO{it}
	if err := l.hydrate(ctx, v, items); err != nil {
		return nil, err
	}
	return &items[0], nil
}

// EnsureMediaForPath cataloga na hora um arquivo ainda não visto pelo scan
// (ex.: favoritar logo após copiar). Devolve o id.
func (l *Library) EnsureMediaForPath(ctx context.Context, rel string) (int64, error) {
	rel = normalizeRelativeMediaPath(rel)
	if err := validatePath(rel); err != nil || rel == "" {
		return 0, errNotFound
	}
	var id int64
	err := l.r.QueryRowContext(ctx, `SELECT id FROM media WHERE path = ?`, rel).Scan(&id)
	if err == nil {
		return id, nil
	}
	kind := mediaKindOf(rel)
	if kind == "" {
		return 0, errNotFound
	}
	full, ok := resolveMediaPath(rel)
	if !ok {
		return 0, errNotFound
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		return 0, errNotFound
	}
	pn := ParseMediaName(path.Base(rel))
	title := pn.Title
	if isOpaqueName(path.Base(rel)) || title == "" {
		title = path.Base(path.Dir(rel)) + " · " + path.Base(rel)
	}
	now := nowUnix()
	err = l.tx(ctx, func(t *sql.Tx) error {
		res, err := t.Exec(`INSERT INTO media(path, root, kind, name, title, size, mtime, fs_file_id, season, episode, first_seen, last_seen)
			VALUES (?,?,?,?,?,?,?,NULLIF(?,''),NULLIF(?,0),NULLIF(?,0),?,?) ON CONFLICT(path) DO NOTHING`,
			rel, rootSegment(rel), kind, path.Base(rel), title, info.Size(), info.ModTime().Unix(), fsFileID(info), pn.Season, pn.Episode, now, now)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		return nil
	})
	if err != nil {
		return 0, err
	}
	if err := l.r.QueryRowContext(ctx, `SELECT id FROM media WHERE path = ?`, rel).Scan(&id); err != nil {
		return 0, err
	}
	return id, l.postProcess(ctx, []int64{id})
}

type FacetCount struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
	Count int    `json:"count"`
}

type Facets struct {
	Kind     []FacetCount `json:"kind"`
	Root     []FacetCount `json:"root"`
	Maturity []FacetCount `json:"maturity"`
	Tags     []FacetCount `json:"tags"`
	Category []FacetCount `json:"category"`
}

func (l *Library) Facets(ctx context.Context, v Viewer, q ItemQuery) (*Facets, error) {
	joins, where, args := buildItemWhere(v, q)
	base := ` FROM media m` + joins + where
	f := &Facets{Kind: []FacetCount{}, Root: []FacetCount{}, Maturity: []FacetCount{}, Tags: []FacetCount{}, Category: []FacetCount{}}
	collect := func(sqlq string, dst *[]FacetCount, withLabel bool) error {
		rows, err := l.r.QueryContext(ctx, sqlq, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var fc FacetCount
			if withLabel {
				if err := rows.Scan(&fc.Value, &fc.Label, &fc.Count); err != nil {
					return err
				}
			} else if err := rows.Scan(&fc.Value, &fc.Count); err != nil {
				return err
			}
			*dst = append(*dst, fc)
		}
		return rows.Err()
	}
	if err := collect(`SELECT m.kind, COUNT(*)`+base+` GROUP BY m.kind ORDER BY 2 DESC`, &f.Kind, false); err != nil {
		return nil, err
	}
	if err := collect(`SELECT m.root, COUNT(*)`+base+` GROUP BY m.root ORDER BY 2 DESC`, &f.Root, false); err != nil {
		return nil, err
	}
	var mats []FacetCount
	if err := collect(`SELECT CAST(m.maturity AS TEXT), COUNT(*)`+base+` GROUP BY m.maturity ORDER BY m.maturity`, &mats, false); err != nil {
		return nil, err
	}
	for _, m := range mats {
		var n int
		fmt.Sscan(m.Value, &n)
		f.Maturity = append(f.Maturity, FacetCount{Value: maturityName(n), Count: m.Count})
	}
	if err := collect(`SELECT COALESCE(m.category,''), COUNT(*)`+base+` AND m.category IS NOT NULL GROUP BY m.category ORDER BY 2 DESC LIMIT 30`, &f.Category, false); err != nil {
		return nil, err
	}
	rows, err := l.r.QueryContext(ctx, `SELECT tg.slug, tg.label, COUNT(*) FROM media_tags mt JOIN tags tg ON tg.id = mt.tag_id WHERE mt.media_id IN (SELECT m.id`+base+`) GROUP BY tg.id ORDER BY 3 DESC LIMIT 60`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var fc FacetCount
		if err := rows.Scan(&fc.Value, &fc.Label, &fc.Count); err != nil {
			return nil, err
		}
		f.Tags = append(f.Tags, fc)
	}
	return f, rows.Err()
}
