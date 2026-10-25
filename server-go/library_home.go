package main

// Busca agrupada por série/obra e a home da Coleção (prateleiras), ambas
// respeitando maturidade, política e itens escondidos pelo usuário.

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------- busca agrupada ----------

type SearchGroup struct {
	Type   string       `json:"type"` // series | item
	Item   *ItemDTO     `json:"item,omitempty"`
	Series *SeriesGroup `json:"series,omitempty"`
}

type SeriesGroup struct {
	Key       string    `json:"key"`
	TopicID   int64     `json:"topicId,omitempty"`
	Folder    string    `json:"folder,omitempty"`
	Title     string    `json:"title"`
	CoverPath string    `json:"coverPath"`
	Matched   int       `json:"matched"` // episódios que casaram com a busca
	Total     int       `json:"total"`   // episódios visíveis da série
	Year      int       `json:"year,omitempty"`
	Maturity  string    `json:"maturity"`
	Next      *ItemDTO  `json:"next,omitempty"` // próximo a assistir
	Best      *ItemDTO  `json:"best"`           // episódio mais relevante
	Items     []ItemDTO `json:"items"`          // episódios que casaram, em ordem
	Watched   int       `json:"watched"`
}

type GroupedSearch struct {
	Total     int           `json:"total"` // itens que casaram
	Groups    []SearchGroup `json:"groups"`
	Truncated bool          `json:"truncated"`
}

const searchGroupScan = 500

// Navegando uma estante (sem texto) o agrupamento precisa ver a pasta toda;
// senão o que entrou primeiro (ex.: filmes soltos na raiz) some da lista.
const browseGroupScan = 5000

var reYear = regexp.MustCompile(`(?:^|[^0-9])((?:19|20)[0-9]{2})(?:[^0-9]|$)`)

// groupKey decide a "obra" do item: tópico de série, senão a pasta da série
// (pelo parser de nomes); Criadores/Filmes/imagens ficam soltos.
func groupKey(it ItemDTO) (key, title, folder string, topicID int64) {
	for _, t := range it.Topics {
		if t.Kind == "series" {
			return fmt.Sprintf("t%d", t.ID), t.Title, "", t.ID
		}
	}
	if it.Kind != "video" || it.Root == "Criadores" || it.Root == "Filmes" {
		return "", "", "", 0
	}
	if d, ok := deriveTopic(it.Path); ok && d.Kind == "series" {
		return "d" + strings.ToLower(d.Folder), d.Title, d.Folder, 0
	}
	dir := path.Dir(it.Path)
	if dir == "." || dir == it.Root {
		return "", "", "", 0
	}
	t := cleanTopicTitle(path.Base(dir))
	if t == "" {
		return "", "", "", 0
	}
	return "d" + strings.ToLower(dir), t, dir, 0
}

// SearchGrouped roda a busca (bm25) e agrupa os resultados por obra; a ordem
// dos grupos é a do melhor episódio de cada um.
func (l *Library) SearchGrouped(ctx context.Context, v Viewer, q ItemQuery) (*GroupedSearch, error) {
	limit0 := q.Limit
	limit := clampLimit(q.Limit, 40, 200)
	q.Limit, q.Offset = searchGroupScan, 0
	if ftsQuery(q.Q) == "" {
		limit = clampLimit(limit0, 40, 1000)
		q.Limit, q.maxLimit = browseGroupScan, browseGroupScan
	}
	page, err := l.QueryItems(ctx, v, q)
	if err != nil {
		return nil, err
	}
	out := &GroupedSearch{Total: page.Total, Groups: []SearchGroup{}, Truncated: page.Total > len(page.Items)}
	byKey := map[string]*SeriesGroup{}
	type slot struct {
		key  string
		item *ItemDTO
	}
	var order []slot
	for i := range page.Items {
		it := page.Items[i]
		key, title, folder, tid := groupKey(it)
		if key == "" {
			order = append(order, slot{item: &it})
			continue
		}
		g, ok := byKey[key]
		if !ok {
			g = &SeriesGroup{Key: key, TopicID: tid, Folder: folder, Title: title, Best: &it}
			byKey[key] = g
			order = append(order, slot{key: key})
		}
		g.Items = append(g.Items, it)
		g.Matched++
	}
	for _, s := range order {
		if len(out.Groups) >= limit {
			out.Truncated = true
			break
		}
		if s.item != nil {
			out.Groups = append(out.Groups, SearchGroup{Type: "item", Item: s.item})
			continue
		}
		g := byKey[s.key]
		// Série com um episódio só na busca e sem tópico continua sendo série
		// se a pasta tiver mais episódios; senão vira item solto.
		if err := l.fillSeries(ctx, v, g); err != nil {
			return nil, err
		}
		if g.Total <= 1 {
			out.Groups = append(out.Groups, SearchGroup{Type: "item", Item: g.Best})
			continue
		}
		out.Groups = append(out.Groups, SearchGroup{Type: "series", Series: g})
	}
	return out, nil
}

// fillSeries completa capa, total, ano, maturidade e o próximo episódio.
func (l *Library) fillSeries(ctx context.Context, v Viewer, g *SeriesGroup) error {
	eq := ItemQuery{Limit: 500, Kind: "video"}
	if g.TopicID > 0 {
		eq.TopicID, eq.Sort = g.TopicID, "episode"
	} else {
		eq.PathPrefix, eq.Sort = g.Folder, "path"
	}
	eps, err := l.QueryItems(ctx, v, eq)
	if err != nil {
		return err
	}
	g.Total = eps.Total
	list := eps.Items
	if g.TopicID == 0 {
		sort.SliceStable(list, func(i, j int) bool { return naturalSortKey(list[i].Path) < naturalSortKey(list[j].Path) })
	}
	// sem temporadas nos nomes: as pastas dividem (anos, OVAs, extras no fim)
	orderByParts(list, g.Title)
	g.Next = nextEpisode(list)
	mat := 0
	for _, it := range list {
		if it.Progress != nil && it.Progress.Completed {
			g.Watched++
		}
		if m, ok := parseMaturity(it.Maturity); ok && m > mat {
			mat = m
		}
		if g.Year == 0 {
			if m := reYear.FindStringSubmatch(it.Path); m != nil {
				fmt.Sscan(m[1], &g.Year)
			}
		}
	}
	g.Maturity = maturityName(mat)
	if len(list) > 0 {
		g.CoverPath = list[0].Path
	} else if g.Best != nil {
		g.CoverPath = g.Best.Path
	}
	// episódios que casaram, na ordem da série
	pos := map[int64]int{}
	for i, it := range list {
		pos[it.ID] = i
	}
	for i := range g.Items {
		if p, ok := pos[g.Items[i].ID]; ok {
			g.Items[i].Part = list[p].Part
		}
	}
	sort.SliceStable(g.Items, func(i, j int) bool {
		pi, oki := pos[g.Items[i].ID]
		pj, okj := pos[g.Items[j].ID]
		if oki != okj {
			return oki
		}
		return pi < pj
	})
	return nil
}

// nextEpisode: o episódio em andamento; senão o primeiro não concluído depois
// do último concluído; senão o primeiro.
func nextEpisode(list []ItemDTO) *ItemDTO {
	if len(list) == 0 {
		return nil
	}
	for i := range list {
		if p := list[i].Progress; p != nil && !p.Completed && p.PositionS >= 10 {
			return &list[i]
		}
	}
	last := -1
	for i := range list {
		if p := list[i].Progress; p != nil && p.Completed {
			last = i
		}
	}
	for i := last + 1; i < len(list); i++ {
		if p := list[i].Progress; p == nil || !p.Completed {
			return &list[i]
		}
	}
	if last == -1 {
		return &list[0]
	}
	return nil
}

// ---------- home ----------

type TopicCard struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Count     int    `json:"count"`
	Watched   int    `json:"watched"`
	CoverPath string `json:"coverPath"`
	Maturity  string `json:"maturity,omitempty"` // da capa: o que o card mostra
}

type PlaylistCard struct {
	Playlist
	CoverPath string `json:"coverPath,omitempty"`
}

type HomeShelf struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Kind      string         `json:"kind"` // items | topics | playlists
	Items     []ItemDTO      `json:"items,omitempty"`
	Topics    []TopicCard    `json:"topics,omitempty"`
	Playlists []PlaylistCard `json:"playlists,omitempty"`
	// Ações que a prateleira oferece (ex.: "dismiss" no continuar assistindo).
	Actions []string `json:"actions,omitempty"`
	// Link "ver tudo": filtro da busca/aba correspondente.
	More map[string]string `json:"more,omitempty"`
}

type HomeData struct {
	Shelves     []HomeShelf `json:"shelves"`
	GeneratedAt int64       `json:"generatedAt"`
}

const homeTTL = 45 * time.Second

type homeEntry struct {
	at   time.Time
	data *HomeData
}

var homeCache sync.Map // "<lib ptr>|<user>|<maturity>" -> homeEntry

func (l *Library) homeKey(v Viewer) string {
	return fmt.Sprintf("%p|%s|%d|%v", l, v.UserID, v.MaxMaturity, v.IsAdmin)
}

// invalidateHome descarta a home em cache de um usuário ("" = de todos).
func (l *Library) invalidateHome(userID string) {
	prefix := fmt.Sprintf("%p|", l)
	if userID != "" {
		prefix += userID + "|"
	}
	homeCache.Range(func(k, _ any) bool {
		if strings.HasPrefix(k.(string), prefix) {
			homeCache.Delete(k)
		}
		return true
	})
}

func (l *Library) Home(ctx context.Context, v Viewer) (*HomeData, error) {
	key := l.homeKey(v)
	if e, ok := homeCache.Load(key); ok && time.Since(e.(homeEntry).at) < homeTTL {
		return e.(homeEntry).data, nil
	}
	h, err := l.buildHome(ctx, v)
	if err != nil {
		return nil, err
	}
	homeCache.Store(key, homeEntry{at: time.Now(), data: h})
	return h, nil
}

func (l *Library) buildHome(ctx context.Context, v Viewer) (*HomeData, error) {
	h := &HomeData{Shelves: []HomeShelf{}, GeneratedAt: nowUnix()}
	add := func(s HomeShelf) {
		if len(s.Items)+len(s.Topics)+len(s.Playlists) > 0 {
			h.Shelves = append(h.Shelves, s)
		}
	}
	items := func(q ItemQuery) ([]ItemDTO, error) {
		p, err := l.QueryItems(ctx, v, q)
		if err != nil {
			return nil, err
		}
		return p.Items, nil
	}

	cont, err := l.ContinueWatching(ctx, v, 24)
	if err != nil {
		return nil, err
	}
	add(HomeShelf{ID: "continue", Title: "Continuar assistindo", Kind: "items", Items: cont, Actions: []string{"dismiss"}})

	fav, err := items(ItemQuery{Favorites: true, Sort: "added", Limit: 24})
	if err != nil {
		return nil, err
	}
	add(HomeShelf{ID: "favorites", Title: "Favoritos", Kind: "items", Items: fav, More: map[string]string{"favorites": "1"}})

	scan, err := items(ItemQuery{Sort: "added", Limit: 500, noManga: true})
	if err != nil {
		return nil, err
	}
	recent := recentByFolder(scan, 24)
	add(HomeShelf{ID: "recent", Title: "Adicionados recentemente", Kind: "items", Items: recent, More: map[string]string{"sort": "added"}})

	for _, kind := range []string{"series", "creator"} {
		tc, err := l.topTopics(ctx, v, kind, 20)
		if err != nil {
			return nil, err
		}
		title := "Séries em destaque"
		if kind == "creator" {
			title = "Criadores em destaque"
		}
		add(HomeShelf{ID: "top-" + kind, Title: title, Kind: "topics", Topics: tc})
	}

	pls, err := l.ListPlaylists(ctx, v)
	if err != nil {
		return nil, err
	}
	var pcs []PlaylistCard
	vis, vargs := v.visibleSQL(false)
	for _, p := range pls {
		if p.ItemCount == 0 || len(pcs) >= 20 {
			continue
		}
		pc := PlaylistCard{Playlist: p}
		var cover sql.NullString
		l.r.QueryRowContext(ctx, `SELECT m.path FROM playlist_items pi JOIN media m ON m.id = pi.media_id WHERE pi.playlist_id = ? AND `+vis+` ORDER BY pi.pos LIMIT 1`,
			append([]any{p.ID}, vargs...)...).Scan(&cover)
		pc.CoverPath = cover.String
		pcs = append(pcs, pc)
	}
	add(HomeShelf{ID: "playlists", Title: "Playlists", Kind: "playlists", Playlists: pcs})

	return h, nil
}

// topTopics: tópicos com mais itens vistos pelo usuário, depois os maiores.
func (l *Library) topTopics(ctx context.Context, v Viewer, kind string, limit int) ([]TopicCard, error) {
	vis, vargs := v.visibleSQL(false)
	// série é de vídeo: tópico de série só com páginas de mangá não abre na estante Animes
	only := ""
	if kind == "series" {
		only = " AND m.kind = 'video'"
	}
	q := `SELECT id, kind, title, cnt, watched, cover FROM (
		SELECT tp.id, tp.kind, tp.title,
		  (SELECT COUNT(*) FROM topic_items ti JOIN media m ON m.id = ti.media_id WHERE ti.topic_id = tp.id AND ` + vis + only + `) AS cnt,
		  (SELECT COUNT(*) FROM topic_items ti JOIN watch_progress wp ON wp.media_id = ti.media_id AND wp.user_id = ? WHERE ti.topic_id = tp.id) AS watched,
		  (SELECT m.path FROM topic_items ti JOIN media m ON m.id = ti.media_id WHERE ti.topic_id = tp.id AND ` + vis + ` ORDER BY m.kind = 'video' DESC, ti.sort_key LIMIT 1) AS cover
		FROM topics tp WHERE tp.kind = ?) WHERE cnt > 0 ORDER BY watched DESC, cnt DESC, title COLLATE NOCASE LIMIT ?`
	args := append([]any{}, vargs...)
	args = append(args, v.UserID)
	args = append(args, vargs...)
	args = append(args, kind, limit)
	rows, err := l.r.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TopicCard
	for rows.Next() {
		var t TopicCard
		var cover sql.NullString
		if err := rows.Scan(&t.ID, &t.Kind, &t.Title, &t.Count, &t.Watched, &cover); err != nil {
			return nil, err
		}
		t.CoverPath = cover.String
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	l.markCardMaturity(ctx, out)
	return out, nil
}

// markCardMaturity preenche a classificação de cada card pela capa.
func (l *Library) markCardMaturity(ctx context.Context, cards []TopicCard) {
	paths := make([]string, 0, len(cards))
	for _, c := range cards {
		paths = append(paths, c.CoverPath)
	}
	mat := l.maturityOfPaths(ctx, paths)
	for i := range cards {
		if m, ok := mat[cards[i].CoverPath]; ok {
			cards[i].Maturity = maturityName(m)
		}
	}
}

// maturityOfPaths: a classificação de cada caminho que existe no catálogo.
func (l *Library) maturityOfPaths(ctx context.Context, paths []string) map[string]int {
	out := map[string]int{}
	args := make([]any, 0, len(paths))
	for _, p := range paths {
		if p != "" {
			args = append(args, p)
		}
	}
	for len(args) > 0 {
		n := min(len(args), 500)
		rows, err := l.r.QueryContext(ctx, `SELECT path, maturity FROM media WHERE path IN (`+placeholders(n)+`)`, args[:n]...)
		args = args[n:]
		if err != nil {
			continue
		}
		for rows.Next() {
			var p string
			var m int
			if rows.Scan(&p, &m) == nil {
				out[p] = m
			}
		}
		rows.Close()
	}
	return out
}

// recentByFolder deixa um item por pasta (um capítulo com 30 páginas ou uma
// galeria não lotam a prateleira) e tira páginas de mangá, que têm estante própria.
func recentByFolder(items []ItemDTO, n int) []ItemDTO {
	out := []ItemDTO{}
	seen := map[string]int{} // pasta -> posição em out
	for _, it := range items {
		if it.Kind == "image" {
			if _, _, ok := splitMangaPath(it.Path); ok {
				continue
			}
		}
		dir := path.Dir(it.Path)
		if dir != "." && dir != it.Root {
			if i, ok := seen[dir]; ok {
				// a capa .jpg de um torrent não representa a pasta; o vídeo sim
				if out[i].Kind == "image" && it.Kind == "video" {
					out[i] = it
				}
				continue
			}
			if len(out) == n {
				continue
			}
			seen[dir] = len(out)
		} else if len(out) == n {
			continue
		}
		out = append(out, it)
	}
	return out
}
