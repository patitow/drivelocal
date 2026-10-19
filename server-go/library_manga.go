package main

// Mangás: páginas de imagem dentro de uma pasta "Mangás" (ou variantes) viram
// obras (primeiro nível abaixo dela) e capítulos (pastas com as páginas).

import (
	"context"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type MangaChapter struct {
	Dir   string `json:"dir"`
	Title string `json:"title"`
	Pages int    `json:"pages"`
	Cover string `json:"cover"`
}

type MangaWork struct {
	Dir      string         `json:"dir"`
	Title    string         `json:"title"`
	Author   string         `json:"author,omitempty"`
	Cover    string         `json:"cover"`
	Pages    int            `json:"pages"`
	MTime    int64          `json:"mtime"`
	Chapters []MangaChapter `json:"chapters"`
}

// isMangaDir reconhece "Mangás", ".Mangás", "Mangas", "Manga", "Mangá".
func isMangaDir(seg string) bool {
	s := strings.ToLower(strings.TrimLeft(seg, "._ "))
	s = strings.NewReplacer("á", "a", "à", "a", "ã", "a", "â", "a").Replace(s)
	return s == "manga" || s == "mangas"
}

// splitMangaPath devolve a pasta da obra e a do capítulo de uma página;
// ok=false quando a imagem não está dentro de uma obra numa pasta de mangás.
func splitMangaPath(p string) (work, chapter string, ok bool) {
	segs := strings.Split(p, "/")
	for i := 0; i < len(segs)-2; i++ {
		if isMangaDir(segs[i]) {
			return strings.Join(segs[:i+2], "/"), path.Dir(p), true
		}
	}
	return "", "", false
}

var (
	reHexID      = regexp.MustCompile(`^[0-9a-fA-F_\-]{6,}$`)
	reChapterNum = regexp.MustCompile(`(?i)^(?:cap(?:[ií]tulo)?|ch(?:apter)?|c|ep)?[\s._-]*0*(\d+(?:\.\d+)?)$`)
	reBraces     = regexp.MustCompile(`\{[^}]*\}`) // grupo de tradução: "{Grupo}"
)

// mangaWorkTitle tira as etiquetas entre colchetes; a primeira, no início, é o autor.
func mangaWorkTitle(dir string) (title, author string) {
	base := path.Base(dir)
	if m := reBracketLead.FindStringSubmatch(base); m != nil {
		author = strings.TrimSpace(m[1])
		base = base[len(m[0]):]
	}
	title = strings.Trim(reSpaces.ReplaceAllString(reBrackets.ReplaceAllString(reBraces.ReplaceAllString(strings.ReplaceAll(base, "_", " "), " "), " "), " "), " -–·")
	if title == "" {
		title = path.Base(dir)
	}
	return title, author
}

// "Obra 2 - Subtítulo" ou "Obra Vol. 2": volume numerado de uma mesma obra.
var reMangaVolume = regexp.MustCompile(`(?i)^(.+?)\s+(?:v(?:ol(?:ume)?)?\.?\s*)?0*(\d{1,3})(?:\s*[-–:]\s*(.+))?$`)

// mangaVolume separa o nome da obra e o número do volume do título.
func mangaVolume(title string) (series string, vol int, ok bool) {
	m := reMangaVolume.FindStringSubmatch(title)
	if m == nil {
		return "", 0, false
	}
	vol, _ = strconv.Atoi(m[2])
	series = strings.TrimSpace(m[1])
	if sub := strings.TrimSpace(m[3]); sub != "" {
		series += " - " + sub
	}
	return series, vol, true
}

// mergeMangaVolumes junta pastas irmãs que são volumes da mesma obra
// ("Obra 1 - X", "Obra 2 - X") numa obra só, com os volumes em ordem.
func mergeMangaVolumes(in []MangaWork) []MangaWork {
	type vol struct {
		n int
		w MangaWork
	}
	groups := map[string][]vol{}
	names := map[string]string{}
	var out []MangaWork
	for _, w := range in {
		s, n, ok := mangaVolume(w.Title)
		if !ok {
			out = append(out, w)
			continue
		}
		key := path.Dir(w.Dir) + "\x00" + strings.ToLower(s) + "\x00" + strings.ToLower(w.Author)
		groups[key] = append(groups[key], vol{n, w})
		names[key] = s
	}
	for key, vs := range groups {
		if len(vs) == 1 {
			out = append(out, vs[0].w)
			continue
		}
		sort.Slice(vs, func(i, j int) bool { return vs[i].n < vs[j].n })
		m := MangaWork{Dir: vs[0].w.Dir, Title: names[key], Author: vs[0].w.Author, Cover: vs[0].w.Cover}
		for _, v := range vs {
			m.Pages += v.w.Pages
			if v.w.MTime > m.MTime {
				m.MTime = v.w.MTime
			}
			label := "Volume " + strconv.Itoa(v.n)
			for _, c := range v.w.Chapters {
				if len(v.w.Chapters) == 1 {
					c.Title = label
				} else {
					c.Title = label + " · " + c.Title
				}
				m.Chapters = append(m.Chapters, c)
			}
		}
		out = append(out, m)
	}
	return out
}

// mangaChapterTitle: nomes de pasta que são só ids viram "Capítulo N" pela ordem.
func mangaChapterTitle(dir string, n int) string {
	base := strings.TrimSpace(reBrackets.ReplaceAllString(path.Base(dir), " "))
	if m := reChapterNum.FindStringSubmatch(base); m != nil {
		return "Capítulo " + m[1]
	}
	if base == "" || reHexID.MatchString(base) {
		return "Capítulo " + strconv.Itoa(n)
	}
	return base
}

// Mangas lista as obras visíveis para v, mais recentes primeiro.
func (l *Library) Mangas(ctx context.Context, v Viewer) ([]MangaWork, error) {
	vis, args := v.visibleSQL(false)
	// LIKE é insensível a maiúsculas só em ASCII; "mang" pega todas as variantes.
	// Pastas cadastradas à mão como mangá entram também.
	where := "m.path LIKE '%mang%'"
	registered := l.registeredMangaDirs(ctx)
	var pre []any
	for _, d := range registered {
		where += ` OR m.path LIKE ? ESCAPE '\'`
		pre = append(pre, likePrefix(d)+"/%")
	}
	rows, err := l.r.QueryContext(ctx, `SELECT m.path, m.mtime FROM media m WHERE m.kind = 'image' AND (`+where+`) AND `+vis, append(pre, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type chap struct {
		pages []string
	}
	works := map[string]*MangaWork{}
	chaps := map[string]map[string]*chap{}
	for rows.Next() {
		var p string
		var mt int64
		if err := rows.Scan(&p, &mt); err != nil {
			return nil, err
		}
		wd, cd, ok := splitMangaWork(p, registered)
		if !ok {
			continue
		}
		w := works[wd]
		if w == nil {
			w = &MangaWork{Dir: wd}
			w.Title, w.Author = mangaWorkTitle(wd)
			works[wd] = w
			chaps[wd] = map[string]*chap{}
		}
		w.Pages++
		if mt > w.MTime {
			w.MTime = mt
		}
		c := chaps[wd][cd]
		if c == nil {
			c = &chap{}
			chaps[wd][cd] = c
		}
		c.pages = append(c.pages, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]MangaWork, 0, len(works))
	for wd, w := range works {
		dirs := make([]string, 0, len(chaps[wd]))
		for d := range chaps[wd] {
			dirs = append(dirs, d)
		}
		sort.Slice(dirs, func(i, j int) bool { return naturalSortKey(dirs[i]) < naturalSortKey(dirs[j]) })
		for i, d := range dirs {
			pages := chaps[wd][d].pages
			sort.Slice(pages, func(a, b int) bool { return naturalSortKey(pages[a]) < naturalSortKey(pages[b]) })
			title := mangaChapterTitle(d, i+1)
			if d == wd {
				title = "Capítulo único"
			}
			w.Chapters = append(w.Chapters, MangaChapter{Dir: d, Title: title, Pages: len(pages), Cover: pages[0]})
		}
		w.Cover = w.Chapters[0].Cover
		out = append(out, *w)
	}
	// nome editado no cadastro vence o da pasta
	if metas, err := l.allEntryMeta(ctx); err == nil {
		for i := range out {
			if d := metas["manga:"+out[i].Dir]; d != nil && d.Title != "" {
				out[i].Title = d.Title
			}
		}
	}
	out = mergeMangaVolumes(out)
	sort.Slice(out, func(i, j int) bool {
		if out[i].MTime != out[j].MTime {
			return out[i].MTime > out[j].MTime
		}
		return out[i].Dir < out[j].Dir
	})
	return out, nil
}
