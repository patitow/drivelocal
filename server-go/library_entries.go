package main

// Entradas do catálogo (criadores, animes, séries, filmes, mangás): lista,
// cadastro e ficha curada. A ficha (entry_meta) vence a busca automática de
// metadados; a busca manual em bases públicas fica em library_meta_search.go.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
)

// EntryData é a ficha curada: os campos do ExtMeta mais a categoria
// escolhida (anime ou série) e, nos mangás, se a pasta foi cadastrada à mão.
type EntryData struct {
	ExtMeta
	Cat        string `json:"cat,omitempty"`
	Registered bool   `json:"registered,omitempty"`
	// link de onde a capa/o banner foram baixados (a ficha guarda a cópia local)
	CoverSrc  string `json:"coverSrc,omitempty"`
	BannerSrc string `json:"bannerSrc,omitempty"`
}

// public: a ficha como a página a vê (arte local vira /api/art/...).
func (d *EntryData) public() *EntryData {
	if d == nil {
		return nil
	}
	c := *d
	c.ExtMeta = c.ExtMeta.public()
	return &c
}

type Entry struct {
	Ref       string     `json:"ref"`
	Cat       string     `json:"cat"`
	Title     string     `json:"title"`
	Count     int        `json:"count"`
	CoverPath string     `json:"coverPath,omitempty"`
	TopicID   int64      `json:"topicId,omitempty"`
	MediaID   int64      `json:"mediaId,omitempty"`
	TopicKind string     `json:"topicKind,omitempty"`
	Dir       string     `json:"dir,omitempty"`
	Aliases   []string   `json:"aliases"`
	Maturity  string     `json:"maturity"`
	Meta      *EntryData `json:"meta,omitempty"`
}

var entryCats = map[string]bool{"creators": true, "animes": true, "series": true, "films": true, "franchises": true, "mangas": true}

// franquia: sequência de filmes, na ordem do cadastro
var topicKinds = map[string]bool{"series": true, "creator": true, "theme": true, "franchise": true}

// topicKindOf: o tipo de tópico de cada estante feita de agrupamentos.
func topicKindOf(cat string) string {
	switch cat {
	case "creators":
		return "creator"
	case "franchises":
		return "franchise"
	}
	return "series"
}

// ---------- ficha curada ----------

func (l *Library) GetEntryMeta(ctx context.Context, ref string) (*EntryData, error) {
	var s string
	err := l.r.QueryRowContext(ctx, `SELECT data FROM entry_meta WHERE ref=?`, ref).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d EntryData
	if err := json.Unmarshal([]byte(s), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// EntryArt: capa e banner da ficha, para os cards das estantes.
type EntryArt struct {
	Cover  string `json:"cover,omitempty"`
	Banner string `json:"banner,omitempty"`
}

// EntryArts devolve, por ref, a arte das fichas que o usuário enxerga.
func (l *Library) EntryArts(ctx context.Context, v Viewer) (map[string]EntryArt, error) {
	all, err := l.allEntryMeta(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]EntryArt{}
	for ref, d := range all {
		a := EntryArt{Cover: artPublic(d.Cover), Banner: artPublic(d.Banner)}
		if (a.Cover != "" || a.Banner != "") && l.entryVisible(ctx, v, ref) {
			out[ref] = a
		}
	}
	return out, nil
}

func (l *Library) allEntryMeta(ctx context.Context) (map[string]*EntryData, error) {
	rows, err := l.r.QueryContext(ctx, `SELECT ref, data FROM entry_meta`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*EntryData{}
	for rows.Next() {
		var ref, s string
		if err := rows.Scan(&ref, &s); err != nil {
			return nil, err
		}
		var d EntryData
		if json.Unmarshal([]byte(s), &d) == nil {
			out[ref] = &d
		}
	}
	return out, rows.Err()
}

func (l *Library) SetEntryMeta(ctx context.Context, ref string, d EntryData, actor string) error {
	if !validEntryRef(ref) {
		return errInvalid
	}
	if d.Source == "" {
		d.Source = "manual"
	}
	d.Genres = nonNil(d.Genres)
	b, _ := json.Marshal(d)
	return l.tx(ctx, func(t *sql.Tx) error {
		_, err := t.Exec(`INSERT INTO entry_meta(ref, data, updated_by, updated_at) VALUES (?,?,?,?)
			ON CONFLICT(ref) DO UPDATE SET data=excluded.data, updated_by=excluded.updated_by, updated_at=excluded.updated_at`,
			ref, string(b), actor, nowUnix())
		return err
	})
}

func (l *Library) DeleteEntryMeta(ctx context.Context, ref string) error {
	return l.tx(ctx, func(t *sql.Tx) error {
		_, err := t.Exec(`DELETE FROM entry_meta WHERE ref=?`, ref)
		return err
	})
}

func validEntryRef(ref string) bool {
	kind, val, ok := strings.Cut(ref, ":")
	if !ok || val == "" {
		return false
	}
	switch kind {
	case "topic", "media":
		n, err := strconv.ParseInt(val, 10, 64)
		return err == nil && n > 0
	case "manga":
		return !strings.Contains(val, "..")
	}
	return false
}

// registeredMangaDirs: pastas cadastradas à mão como mangá (fora de "Mangás").
func (l *Library) registeredMangaDirs(ctx context.Context) []string {
	metas, err := l.allEntryMeta(ctx)
	if err != nil {
		return nil
	}
	var dirs []string
	for ref, d := range metas {
		if dir, ok := strings.CutPrefix(ref, "manga:"); ok && d.Registered {
			dirs = append(dirs, dir)
		}
	}
	// a mais funda primeiro: uma obra dentro de outra pasta cadastrada fica com a dela
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	return dirs
}

// ---------- lista ----------

func (l *Library) ListEntries(ctx context.Context, cat, q string, limit int) ([]Entry, error) {
	if !entryCats[cat] {
		return nil, errInvalid
	}
	metas, err := l.allEntryMeta(ctx)
	if err != nil {
		return nil, err
	}
	var out []Entry
	switch cat {
	case "creators", "animes", "series", "franchises":
		out, err = l.topicEntries(ctx, cat, metas)
	case "films":
		out, err = l.filmEntries(ctx, metas)
	case "mangas":
		out, err = l.mangaEntries(ctx, metas)
	}
	if err != nil {
		return nil, err
	}
	if q = strings.ToLower(foldDiacritics(strings.TrimSpace(q))); q != "" {
		kept := out[:0]
		for _, e := range out {
			hay := strings.ToLower(foldDiacritics(e.Title + " " + strings.Join(e.Aliases, " ")))
			if strings.Contains(hay, q) {
				kept = append(kept, e)
			}
		}
		out = kept
	}
	coll := func(s string) string { return strings.ToLower(foldDiacritics(s)) }
	sort.SliceStable(out, func(i, j int) bool { return coll(out[i].Title) < coll(out[j].Title) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// topicEntries: criadores, e séries divididas em animes (algum episódio na
// pasta Animes, ou marcadas assim na ficha) e as demais.
func (l *Library) topicEntries(ctx context.Context, cat string, metas map[string]*EntryData) ([]Entry, error) {
	kind := topicKindOf(cat)
	rows, err := l.r.QueryContext(ctx, `
		SELECT t.id, t.title, COUNT(m.id), COALESCE(MAX(m.maturity), 0),
			COALESCE(MIN(CASE WHEN m.kind='video' THEN m.path END), MIN(m.path), ''),
			COALESCE(SUM(m.root='Animes' OR m.path LIKE '%anime%'), 0), COALESCE(SUM(m.kind='video'), 0),
			(SELECT group_concat(alias, char(31)) FROM topic_aliases a WHERE a.topic_id=t.id)
		FROM topics t
		LEFT JOIN topic_items ti ON ti.topic_id = t.id
		LEFT JOIN media m ON m.id = ti.media_id AND m.missing = 0 AND m.dup_of IS NULL
		WHERE t.kind = ?
		GROUP BY t.id`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var mat, animes, videos int
		var aliases sql.NullString
		if err := rows.Scan(&e.TopicID, &e.Title, &e.Count, &mat, &e.CoverPath, &animes, &videos, &aliases); err != nil {
			return nil, err
		}
		e.Ref = "topic:" + strconv.FormatInt(e.TopicID, 10)
		e.Meta = metas[e.Ref].public()
		e.Cat = cat
		if kind == "series" {
			// série só de imagens é mangá agrupado pelo scan: tem estante própria
			if e.Count > 0 && videos == 0 && (e.Meta == nil || e.Meta.Cat == "") {
				continue
			}
			c := "series"
			if animes > 0 {
				c = "animes"
			}
			if e.Meta != nil && (e.Meta.Cat == "animes" || e.Meta.Cat == "series") {
				c = e.Meta.Cat
			}
			if c != cat {
				continue
			}
		}
		e.Maturity = maturityName(mat)
		e.Aliases = splitAliases(aliases.String)
		out = append(out, e)
	}
	return out, rows.Err()
}

func splitAliases(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "\x1f")
}

// filmSQL: o que conta como filme — a pasta Filmes, a categoria "filme" e os
// filmes de anime (longos e fora de qualquer série na pasta Animes).
const filmSQL = `m.kind = 'video' AND m.missing = 0 AND m.dup_of IS NULL AND (
	m.root = 'Filmes' OR m.category = 'filme' OR (m.root = 'Animes' AND m.duration_s >= 2400 AND NOT EXISTS (
		SELECT 1 FROM topic_items ti JOIN topics t ON t.id = ti.topic_id WHERE ti.media_id = m.id AND t.kind = 'series')))`

func (l *Library) filmEntries(ctx context.Context, metas map[string]*EntryData) ([]Entry, error) {
	rows, err := l.r.QueryContext(ctx, `SELECT m.id, COALESCE(m.title_manual, m.title), m.path, m.maturity FROM media m WHERE `+filmSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var mat int
		if err := rows.Scan(&e.MediaID, &e.Title, &e.CoverPath, &mat); err != nil {
			return nil, err
		}
		e.Ref = "media:" + strconv.FormatInt(e.MediaID, 10)
		e.Cat, e.Count, e.Maturity, e.Aliases, e.Meta = "films", 1, maturityName(mat), []string{}, metas[e.Ref].public()
		out = append(out, e)
	}
	return out, rows.Err()
}

func (l *Library) mangaEntries(ctx context.Context, metas map[string]*EntryData) ([]Entry, error) {
	works, err := l.Mangas(ctx, Viewer{IsAdmin: true, MaxMaturity: MaturityExplicito})
	if err != nil {
		return nil, err
	}
	mats := l.dirMaturities(ctx)
	out := make([]Entry, 0, len(works))
	for _, w := range works {
		e := Entry{Ref: "manga:" + w.Dir, Cat: "mangas", Title: w.Title, Count: w.Pages, CoverPath: w.Cover, Dir: w.Dir, Aliases: []string{}}
		e.Meta = metas[e.Ref].public()
		e.Maturity = maturityName(mats[w.Dir])
		out = append(out, e)
	}
	return out, nil
}

// dirMaturities: maior maturidade das imagens de cada pasta de obra.
func (l *Library) dirMaturities(ctx context.Context) map[string]int {
	out := map[string]int{}
	dirs := l.registeredMangaDirs(ctx)
	rows, err := l.r.QueryContext(ctx, `SELECT path, maturity FROM media WHERE kind='image' AND missing=0 AND maturity > 0`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		var m int
		if rows.Scan(&p, &m) != nil {
			continue
		}
		if wd, _, ok := splitMangaWork(p, dirs); ok && m > out[wd] {
			out[wd] = m
		}
	}
	return out
}

// splitMangaWork é splitMangaPath com as pastas cadastradas à mão.
func splitMangaWork(p string, registered []string) (work, chapter string, ok bool) {
	for _, d := range registered {
		if strings.HasPrefix(p, d+"/") {
			return d, path.Dir(p), true
		}
	}
	return splitMangaPath(p)
}

// FindEntry: a entrada de um ref, em qualquer estante (link "Editar" das páginas).
func (l *Library) FindEntry(ctx context.Context, ref string) (*Entry, error) {
	if !validEntryRef(ref) {
		return nil, errInvalid
	}
	for _, cat := range []string{"animes", "series", "creators", "films", "franchises", "mangas"} {
		list, err := l.ListEntries(ctx, cat, "", 0)
		if err != nil {
			return nil, err
		}
		for i := range list {
			if list[i].Ref == ref {
				return &list[i], nil
			}
		}
	}
	return nil, errNotFound
}

// ---------- cadastro e edição ----------

type EntryInput struct {
	Cat       string     `json:"cat"`
	Ref       string     `json:"ref,omitempty"` // edição
	Title     *string    `json:"title,omitempty"`
	Aliases   []string   `json:"aliases,omitempty"`
	Dir       string     `json:"dir,omitempty"` // mangá: pasta da obra
	AddIDs    []int64    `json:"addIds,omitempty"`
	RemIDs    []int64    `json:"removeIds,omitempty"`
	Order     []int64    `json:"order,omitempty"` // agrupamento: os itens na ordem escolhida
	Maturity  *string    `json:"maturity,omitempty"` // aplica a todos os itens
	Meta      *EntryData `json:"meta,omitempty"`
	ClearMeta bool       `json:"clearMeta,omitempty"`
}

// CreateEntry cadastra e devolve o ref da entrada nova.
func (l *Library) CreateEntry(ctx context.Context, in EntryInput, actor string) (string, error) {
	if !entryCats[in.Cat] {
		return "", errInvalid
	}
	title := ""
	if in.Title != nil {
		title = strings.TrimSpace(*in.Title)
	}
	var ref string
	switch in.Cat {
	case "creators", "animes", "series", "franchises":
		if title == "" {
			return "", errInvalid
		}
		kind := topicKindOf(in.Cat)
		id, err := l.CreateTopic(ctx, TopicInput{Kind: &kind, Title: &title, Aliases: in.Aliases})
		if err != nil {
			return "", err
		}
		ref = "topic:" + strconv.FormatInt(id, 10)
		if in.Cat == "animes" || in.Cat == "series" {
			// a categoria fica na ficha: uma série nova e vazia já sabe sua estante
			d := EntryData{}
			if in.Meta != nil {
				d = *in.Meta
			}
			d.Cat = in.Cat
			in.Meta = &d
		}
	case "films":
		if len(in.AddIDs) == 0 {
			return "", errInvalid
		}
		cat := "filme"
		acts := Actions{Category: &cat}
		if title != "" && len(in.AddIDs) == 1 {
			acts.Title = &title
		}
		if _, err := l.ApplyActions(ctx, in.AddIDs, acts, "manual", actor, "cadastro de filme", false); err != nil {
			return "", err
		}
		ref = "media:" + strconv.FormatInt(in.AddIDs[0], 10)
		in.AddIDs = nil
		in.Title = nil
	case "mangas":
		dir := strings.Trim(strings.TrimSpace(in.Dir), "/")
		if dir == "" || strings.Contains(dir, "..") {
			return "", errInvalid
		}
		var n int
		like := likePrefix(dir) + "/%"
		l.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM media WHERE kind='image' AND missing=0 AND path LIKE ? ESCAPE '\'`, like).Scan(&n)
		if n == 0 {
			return "", errInvalid
		}
		ref = "manga:" + dir
		d := EntryData{}
		if in.Meta != nil {
			d = *in.Meta
		}
		if title != "" {
			d.Title = title
		}
		// pasta fora de "Mangás" precisa do cadastro para virar obra
		_, _, auto := splitMangaPath(dir + "/x/p.jpg")
		d.Registered = !auto
		in.Meta = &d
		in.Title = nil
	}
	in.Ref = ref
	in.Aliases = nil
	if err := l.UpdateEntry(ctx, in, actor); err != nil {
		return ref, err
	}
	return ref, nil
}

// UpdateEntry muda título, apelidos, itens, maturidade e ficha de uma entrada.
func (l *Library) UpdateEntry(ctx context.Context, in EntryInput, actor string) error {
	if !validEntryRef(in.Ref) {
		return errInvalid
	}
	kind, val, _ := strings.Cut(in.Ref, ":")
	id, _ := strconv.ParseInt(val, 10, 64)
	switch kind {
	case "topic":
		var tkind string
		if err := l.r.QueryRowContext(ctx, `SELECT kind FROM topics WHERE id=?`, id).Scan(&tkind); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errNotFound
			}
			return err
		}
		if in.Title != nil || in.Aliases != nil {
			if err := l.UpdateTopic(ctx, id, TopicInput{Title: in.Title, Aliases: in.Aliases}); err != nil {
				return err
			}
		}
		ref := &TopicRef{ID: id}
		if len(in.AddIDs) > 0 {
			if _, err := l.ApplyActions(ctx, in.AddIDs, Actions{Topic: ref}, "manual", actor, "cadastro: incluir itens", false); err != nil {
				return err
			}
		}
		if len(in.RemIDs) > 0 {
			if _, err := l.ApplyActions(ctx, in.RemIDs, Actions{RemoveTopic: ref}, "manual", actor, "cadastro: tirar itens", false); err != nil {
				return err
			}
		}
		if len(in.Order) > 0 {
			if err := l.setTopicOrder(ctx, id, in.Order); err != nil {
				return err
			}
		}
		if in.Maturity != nil {
			if err := l.applyEntryMaturity(ctx, `SELECT media_id FROM topic_items WHERE topic_id=?`, []any{id}, *in.Maturity, actor); err != nil {
				return err
			}
		}
	case "media":
		acts := Actions{Title: in.Title, Maturity: in.Maturity}
		if !acts.empty() {
			if _, err := l.ApplyActions(ctx, []int64{id}, acts, "manual", actor, "cadastro: editar filme", false); err != nil {
				return err
			}
		}
	case "manga":
		if in.Maturity != nil {
			like := likePrefix(val) + "/%"
			if err := l.applyEntryMaturity(ctx, `SELECT id FROM media WHERE kind='image' AND path LIKE ? ESCAPE '\'`, []any{like}, *in.Maturity, actor); err != nil {
				return err
			}
		}
		if in.Title != nil {
			// o nome do mangá vem da pasta; o editado fica na ficha
			d, err := l.GetEntryMeta(ctx, in.Ref)
			if err != nil {
				return err
			}
			if in.Meta == nil {
				if d == nil {
					d = &EntryData{}
				}
				in.Meta = d
			} else if d != nil {
				in.Meta.Registered = d.Registered
			}
			in.Meta.Title = strings.TrimSpace(*in.Title)
		}
	}
	if in.ClearMeta {
		// mangá cadastrado à mão mantém só o cadastro
		if d, _ := l.GetEntryMeta(ctx, in.Ref); d != nil && d.Registered {
			return l.SetEntryMeta(ctx, in.Ref, EntryData{Registered: true, Cat: d.Cat}, actor)
		}
		return l.DeleteEntryMeta(ctx, in.Ref)
	}
	if in.Meta != nil {
		d := *in.Meta
		d.Cover, d.Banner = artStored(d.Cover), artStored(d.Banner)
		if old, _ := l.GetEntryMeta(ctx, in.Ref); old != nil {
			// a ficha editada não perde a categoria nem o cadastro
			if d.Cat == "" {
				d.Cat = old.Cat
			}
			d.Registered = d.Registered || old.Registered
			if d.Cover == old.Cover && d.CoverSrc == "" {
				d.CoverSrc = old.CoverSrc
			}
			if d.Banner == old.Banner && d.BannerSrc == "" {
				d.BannerSrc = old.BannerSrc
			}
		}
		// links da web expiram: a ficha guarda a cópia local
		l.localizeEntry(ctx, &d)
		return l.SetEntryMeta(ctx, in.Ref, d, actor)
	}
	return nil
}

func (l *Library) applyEntryMaturity(ctx context.Context, q string, args []any, mat, actor string) error {
	rows, err := l.r.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if len(ids) == 0 {
		return nil
	}
	_, err = l.ApplyActions(ctx, ids, Actions{Maturity: &mat}, "manual", actor, "cadastro: maturidade", false)
	return err
}

// DeleteEntry: tópico é apagado (os arquivos ficam); filme perde a categoria;
// mangá perde a ficha (e o cadastro, se era uma pasta fora de "Mangás").
func (l *Library) DeleteEntry(ctx context.Context, ref, actor string) error {
	if !validEntryRef(ref) {
		return errInvalid
	}
	kind, val, _ := strings.Cut(ref, ":")
	id, _ := strconv.ParseInt(val, 10, 64)
	switch kind {
	case "topic":
		if err := l.DeleteTopic(ctx, id); err != nil {
			return err
		}
	case "media":
		empty := ""
		if _, err := l.ApplyActions(ctx, []int64{id}, Actions{Category: &empty}, "manual", actor, "cadastro: tirar dos filmes", false); err != nil {
			return err
		}
	}
	return l.DeleteEntryMeta(ctx, ref)
}

// ImageFolders lista pastas com imagens (para cadastrar um mangá novo).
func (l *Library) ImageFolders(ctx context.Context, q string, limit int) ([]map[string]any, error) {
	q = strings.ToLower(foldDiacritics(strings.TrimSpace(q)))
	rows, err := l.r.QueryContext(ctx, `SELECT path FROM media WHERE kind='image' AND missing=0 AND dup_of IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	best := map[string]int{}
	for rows.Next() {
		var p string
		if rows.Scan(&p) != nil {
			continue
		}
		// a pasta da obra pode ser a de cima, quando as imagens estão em capítulos
		d := path.Dir(p)
		best[d]++
		if up := path.Dir(d); strings.Contains(up, "/") {
			best[up]++
		}
	}
	for d := range best {
		if q != "" && !strings.Contains(strings.ToLower(foldDiacritics(d)), q) {
			delete(best, d)
		}
	}
	dirs := make([]string, 0, len(best))
	for d := range best {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	if limit > 0 && len(dirs) > limit {
		dirs = dirs[:limit]
	}
	out := make([]map[string]any, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, map[string]any{"dir": d, "pages": best[d]})
	}
	return out, nil
}

// ---------- handlers ----------

func libEntriesHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		if ref := r.URL.Query().Get("ref"); ref != "" {
			e, err := l.FindEntry(ctx, ref)
			if err != nil {
				libFail(w, err)
				return
			}
			libJSON(w, map[string]any{"entry": e})
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 2000 {
			limit = 500
		}
		list, err := l.ListEntries(ctx, r.URL.Query().Get("cat"), r.URL.Query().Get("q"), limit)
		if err != nil {
			libFail(w, err)
			return
		}
		libJSON(w, map[string]any{"entries": list})
	case http.MethodPost:
		var in EntryInput
		if err := decodeBody(r, &in); err != nil {
			libFail(w, err)
			return
		}
		ref, err := l.CreateEntry(ctx, in, actorOf(r))
		if err != nil {
			libFail(w, err)
			return
		}
		libJSON(w, map[string]any{"ref": ref})
	case http.MethodPut:
		var in EntryInput
		if err := decodeBody(r, &in); err != nil {
			libFail(w, err)
			return
		}
		if err := l.UpdateEntry(ctx, in, actorOf(r)); err != nil {
			libFail(w, err)
			return
		}
		libJSON(w, map[string]any{"ref": in.Ref})
	case http.MethodDelete:
		if err := l.DeleteEntry(ctx, r.URL.Query().Get("ref"), actorOf(r)); err != nil {
			libFail(w, err)
			return
		}
		libJSON(w, map[string]any{"ok": true})
	}
}

func libImageFoldersHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	list, err := l.ImageFolders(r.Context(), r.URL.Query().Get("q"), 200)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"folders": list})
}

// setTopicOrder grava a ordem escolhida dos itens (sort_key "o0001"...): ela
// vence a ordem do nome do arquivo.
func (l *Library) setTopicOrder(ctx context.Context, topicID int64, ids []int64) error {
	return l.tx(ctx, func(t *sql.Tx) error {
		for i, mid := range ids {
			if _, err := t.Exec(`UPDATE topic_items SET sort_key=? WHERE topic_id=? AND media_id=?`, fmt.Sprintf("o%04d", i+1), topicID, mid); err != nil {
				return err
			}
		}
		return nil
	})
}

// Franchises: as franquias cadastradas, com os filmes na ordem, para a estante
// Filmes e Séries (o resto dos filmes ainda se agrupa pelo começo do nome).
type Franchise struct {
	ID    int64   `json:"id"`
	Title string  `json:"title"`
	Items []int64 `json:"items"`
}

func (l *Library) Franchises(ctx context.Context, v Viewer) ([]Franchise, error) {
	vis, vargs := v.visibleSQL(false)
	rows, err := l.r.QueryContext(ctx, `SELECT tp.id, tp.title, ti.media_id FROM topics tp
		JOIN topic_items ti ON ti.topic_id = tp.id JOIN media m ON m.id = ti.media_id
		WHERE tp.kind = 'franchise' AND `+vis+` ORDER BY tp.title COLLATE NOCASE, tp.id, ti.sort_key, m.path`, vargs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Franchise{}
	for rows.Next() {
		var id, mid int64
		var title string
		if err := rows.Scan(&id, &title, &mid); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].ID != id {
			out = append(out, Franchise{ID: id, Title: title})
		}
		out[len(out)-1].Items = append(out[len(out)-1].Items, mid)
	}
	return out, rows.Err()
}

func libFranchisesHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	list, err := l.Franchises(r.Context(), l.viewer(r))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"franchises": list})
}

// TVSeries: séries para a estante Filmes e Séries — as cadastradas como série
// e as com episódios nas pastas Filmes/Séries. Os agrupamentos automáticos de
// pastas de torrent (clipes em partes) não entram.
func (l *Library) TVSeries(ctx context.Context, v Viewer) ([]TopicCard, error) {
	metas, err := l.allEntryMeta(ctx)
	if err != nil {
		return nil, err
	}
	vis, vargs := v.visibleSQL(false)
	q := `SELECT id, title, cnt, animes, own, watched, cover FROM (
		SELECT tp.id, tp.title,
		  (SELECT COUNT(*) FROM topic_items ti JOIN media m ON m.id = ti.media_id WHERE ti.topic_id = tp.id AND m.kind = 'video' AND ` + vis + `) AS cnt,
		  (SELECT COUNT(*) FROM topic_items ti JOIN media m ON m.id = ti.media_id WHERE ti.topic_id = tp.id AND (m.root = 'Animes' OR m.path LIKE '%anime%')) AS animes,
		  (SELECT COUNT(*) FROM topic_items ti JOIN media m ON m.id = ti.media_id WHERE ti.topic_id = tp.id AND m.root IN ('Filmes', 'Series', 'Séries')) AS own,
		  (SELECT COUNT(*) FROM topic_items ti JOIN watch_progress wp ON wp.media_id = ti.media_id AND wp.user_id = ? WHERE ti.topic_id = tp.id) AS watched,
		  (SELECT m.path FROM topic_items ti JOIN media m ON m.id = ti.media_id WHERE ti.topic_id = tp.id AND m.kind = 'video' AND ` + vis + ` ORDER BY ti.sort_key LIMIT 1) AS cover
		FROM topics tp WHERE tp.kind = 'series') WHERE cnt > 0 ORDER BY title COLLATE NOCASE`
	args := append([]any{}, vargs...)
	args = append(args, v.UserID)
	args = append(args, vargs...)
	rows, err := l.r.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TopicCard{}
	for rows.Next() {
		var t TopicCard
		var animes, own int
		var cover sql.NullString
		if err := rows.Scan(&t.ID, &t.Title, &t.Count, &animes, &own, &t.Watched, &cover); err != nil {
			return nil, err
		}
		cat := ""
		if d := metas["topic:"+strconv.FormatInt(t.ID, 10)]; d != nil {
			cat = d.Cat
		}
		if cat != "series" && (cat == "animes" || animes > 0 || own == 0) {
			continue
		}
		t.Kind, t.CoverPath = "series", cover.String
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	l.markCardMaturity(ctx, out)
	return out, nil
}

func libTVSeriesHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	list, err := l.TVSeries(r.Context(), l.viewer(r))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"series": list})
}

// entryVisible: o leitor vê ao menos um item da entrada (a ficha de uma obra
// modo restrito não vaza para quem não pode vê-la).
func (l *Library) entryVisible(ctx context.Context, v Viewer, ref string) bool {
	kind, val, _ := strings.Cut(ref, ":")
	vis, vargs := v.visibleSQL(false)
	var q string
	var args []any
	switch kind {
	case "topic":
		q = `SELECT EXISTS(SELECT 1 FROM topic_items ti JOIN media m ON m.id = ti.media_id WHERE ti.topic_id = ? AND ` + vis + `)`
		args = append([]any{val}, vargs...)
	case "media":
		q = `SELECT EXISTS(SELECT 1 FROM media m WHERE m.id = ? AND ` + vis + `)`
		args = append([]any{val}, vargs...)
	case "manga":
		q = `SELECT EXISTS(SELECT 1 FROM media m WHERE m.path LIKE ? ESCAPE '\' AND ` + vis + `)`
		args = append([]any{likePrefix(val) + "/%"}, vargs...)
	default:
		return false
	}
	var ok bool
	return l.r.QueryRowContext(ctx, q, args...).Scan(&ok) == nil && ok
}
