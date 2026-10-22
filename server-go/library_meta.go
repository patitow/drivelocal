package main

// Metadados externos (capa, sinopse, ano) para séries, mangás e filmes:
// AniList (GraphQL público, sem chave) e TMDB (só com TMDB_API_KEY).
// Resultados — inclusive "não encontrado" — ficam em ext_meta.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type ExtMeta struct {
	Source   string   `json:"source"` // anilist | tmdb
	URL      string   `json:"url,omitempty"`
	Title    string   `json:"title"`
	Year     int      `json:"year,omitempty"`
	Synopsis string   `json:"synopsis,omitempty"`
	Cover    string   `json:"cover,omitempty"`
	Banner   string   `json:"banner,omitempty"`
	Genres   []string `json:"genres"`
	Score    int      `json:"score,omitempty"` // 0–100
	Episodes int      `json:"episodes,omitempty"`
}

func (m ExtMeta) public() ExtMeta {
	m.Cover, m.Banner = artPublic(m.Cover), artPublic(m.Banner)
	return m
}

const (
	metaFoundTTL    = 30 * 24 * time.Hour
	metaNotFoundTTL = 7 * 24 * time.Hour
)

var (
	metaHTTP = &http.Client{Timeout: 12 * time.Second}
	// AniList permite ~90 req/min; poucas em paralelo bastam (o resto vem do cache)
	metaSlots   = make(chan struct{}, 2)
	aniListURL  = "https://graphql.anilist.co"
	tmdbBaseURL = "https://api.themoviedb.org/3"
)

// cleanMetaTitle tira tags de release ([Grupo], [1080p], extensão) e separa o ano "(2020)".
var (
	reMetaBracket = regexp.MustCompile(`\[[^\]]*\]|\{[^}]*\}`)
	reMetaYear    = regexp.MustCompile(`\((19|20)\d{2}\)`)
	reMetaRelease = regexp.MustCompile(`(?i)\b(1080p|720p|2160p|4k|bluray|bdrip|web-?dl|webrip|x26[45]|hevc|aac|dual audio)\b.*$`)
)

func cleanMetaTitle(s string) (string, int) {
	year := 0
	if m := reMetaYear.FindString(s); m != "" {
		year, _ = strconv.Atoi(strings.Trim(m, "()"))
		s = strings.Replace(s, m, " ", 1)
	}
	s = reMetaBracket.ReplaceAllString(s, " ")
	s = reMetaRelease.ReplaceAllString(s, " ")
	s = strings.NewReplacer("_", " ", ".", " ").Replace(s)
	return strings.Join(strings.Fields(s), " "), year
}

func metaTokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, f := range strings.FieldsFunc(strings.ToLower(foldDiacritics(s)), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		out[f] = true
	}
	return out
}

// titleMatches evita capa de outra obra: algum título do resultado precisa
// cobrir boa parte das palavras pedidas (e vice-versa).
func titleMatches(want string, got ...string) bool {
	w := metaTokens(want)
	if len(w) == 0 {
		return false
	}
	for _, g := range got {
		gt := metaTokens(g)
		if len(gt) == 0 {
			continue
		}
		inter := 0
		for t := range w {
			if gt[t] {
				inter++
			}
		}
		union := len(w) + len(gt) - inter
		if float64(inter)/float64(union) >= 0.5 || (inter == len(w) && inter >= 2) || (inter == len(gt) && inter >= 2) {
			return true
		}
	}
	return false
}

var reMetaTags = regexp.MustCompile(`<[^>]*>`)

func plainSynopsis(s string) string {
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = reMetaTags.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	// "(Source: ...)" no fim das sinopses do AniList
	if i := strings.LastIndex(s, "(Source:"); i > len(s)/2 {
		s = s[:i]
	}
	s = regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func fetchAniList(ctx context.Context, title, mediaType string, year int, movie bool) (*ExtMeta, error) {
	const q = `query ($s: String, $t: MediaType) { Page(perPage: 6) { media(search: $s, type: $t, isAdult: false) {
		id siteUrl format episodes averageScore genres bannerImage description(asHtml: false)
		title { romaji english native } synonyms startDate { year } coverImage { extraLarge large } } } }`
	body, _ := json.Marshal(map[string]any{"query": q, "variables": map[string]any{"s": title, "t": mediaType}})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, aniListURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := metaHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anilist: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Data struct {
			Page struct {
				Media []struct {
					SiteURL      string   `json:"siteUrl"`
					Format       string   `json:"format"`
					Episodes     int      `json:"episodes"`
					AverageScore int      `json:"averageScore"`
					Genres       []string `json:"genres"`
					BannerImage  string   `json:"bannerImage"`
					Description  string   `json:"description"`
					Synonyms     []string `json:"synonyms"`
					Title        struct{ Romaji, English, Native string }
					StartDate    struct{ Year int }                 `json:"startDate"`
					CoverImage   struct{ ExtraLarge, Large string } `json:"coverImage"`
				} `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	for _, m := range out.Data.Page.Media {
		if movie != (m.Format == "MOVIE") {
			continue
		}
		if year > 0 && m.StartDate.Year > 0 && abs(m.StartDate.Year-year) > 1 {
			continue
		}
		if !titleMatches(title, append([]string{m.Title.Romaji, m.Title.English, m.Title.Native}, m.Synonyms...)...) {
			continue
		}
		t := m.Title.English
		if t == "" {
			t = m.Title.Romaji
		}
		cover := m.CoverImage.ExtraLarge
		if cover == "" {
			cover = m.CoverImage.Large
		}
		return &ExtMeta{
			Source: "anilist", URL: m.SiteURL, Title: t, Year: m.StartDate.Year, Synopsis: plainSynopsis(m.Description),
			Cover: cover, Banner: m.BannerImage, Genres: nonNil(m.Genres), Score: m.AverageScore, Episodes: m.Episodes,
		}, nil
	}
	return nil, nil
}

func fetchTMDB(ctx context.Context, key, title string, year int) (*ExtMeta, error) {
	v := url.Values{"query": {title}, "language": {"pt-BR"}, "include_adult": {"false"}}
	if year > 0 {
		v.Set("year", strconv.Itoa(year))
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, tmdbBaseURL+"/search/movie?"+v.Encode(), nil)
	// chave v4 (token longo) vai no header; v3 (32 hex) na query
	if len(key) > 40 {
		req.Header.Set("Authorization", "Bearer "+key)
	} else {
		q := req.URL.Query()
		q.Set("api_key", key)
		req.URL.RawQuery = q.Encode()
	}
	resp, err := metaHTTP.Do(req)
	if err != nil {
		// o erro do net/http traz a URL, que pode ter a chave v3: não deixa ir para o log
		var ue *url.Error
		if errors.As(err, &ue) {
			ue.URL = tmdbBaseURL + "/search/movie"
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tmdb: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Results []struct {
			ID            int64   `json:"id"`
			Title         string  `json:"title"`
			OriginalTitle string  `json:"original_title"`
			Overview      string  `json:"overview"`
			ReleaseDate   string  `json:"release_date"`
			PosterPath    string  `json:"poster_path"`
			BackdropPath  string  `json:"backdrop_path"`
			VoteAverage   float64 `json:"vote_average"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	for _, m := range out.Results {
		if !titleMatches(title, m.Title, m.OriginalTitle) {
			continue
		}
		e := &ExtMeta{Source: "tmdb", URL: fmt.Sprintf("https://www.themoviedb.org/movie/%d", m.ID), Title: m.Title,
			Synopsis: strings.TrimSpace(m.Overview), Genres: []string{}, Score: int(m.VoteAverage*10 + 0.5)}
		if len(m.ReleaseDate) >= 4 {
			e.Year, _ = strconv.Atoi(m.ReleaseDate[:4])
		}
		if m.PosterPath != "" {
			e.Cover = "https://image.tmdb.org/t/p/w500" + m.PosterPath
		}
		if m.BackdropPath != "" {
			e.Banner = "https://image.tmdb.org/t/p/w1280" + m.BackdropPath
		}
		return e, nil
	}
	return nil, nil
}

// LookupMeta: kind anime | manga | movie. nil sem erro = não encontrado.
func (l *Library) LookupMeta(ctx context.Context, kind, rawTitle string) (*ExtMeta, error) {
	title, year := cleanMetaTitle(rawTitle)
	if title == "" || len([]rune(title)) > 200 {
		return nil, errInvalid
	}
	tmdbKey := strings.TrimSpace(os.Getenv("TMDB_API_KEY"))
	src := "anilist"
	if kind == "movie" && tmdbKey != "" {
		src = "tmdb"
	}
	key := fmt.Sprintf("%s|%s|%s|%d", src, kind, strings.ToLower(foldDiacritics(title)), year)

	var data sql.NullString
	var fetched int64
	err := l.r.QueryRowContext(ctx, `SELECT data, fetched_at FROM ext_meta WHERE key = ?`, key).Scan(&data, &fetched)
	if err == nil {
		age := time.Since(time.Unix(fetched, 0))
		if data.Valid && age < metaFoundTTL {
			var m ExtMeta
			if json.Unmarshal([]byte(data.String), &m) == nil {
				m = m.public()
				return &m, nil
			}
		} else if !data.Valid && age < metaNotFoundTTL {
			return nil, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	select {
	case metaSlots <- struct{}{}:
		defer func() { <-metaSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	var m *ExtMeta
	switch {
	case src == "tmdb":
		m, err = fetchTMDB(ctx, tmdbKey, title, year)
	case kind == "manga":
		m, err = fetchAniList(ctx, title, "MANGA", year, false)
	default:
		m, err = fetchAniList(ctx, title, "ANIME", year, kind == "movie")
	}
	if err != nil {
		return nil, err // falha de rede não vai para o cache
	}
	var stored any
	if m != nil {
		// a capa e o banner ficam no acervo local (links de CDN expiram)
		m.Cover, m.Banner = l.localArt(ctx, m.Cover), l.localArt(ctx, m.Banner)
		b, _ := json.Marshal(m)
		stored = string(b)
		*m = m.public()
	}
	l.w.ExecContext(ctx, `INSERT INTO ext_meta(key, data, fetched_at) VALUES (?,?,?)
		ON CONFLICT(key) DO UPDATE SET data=excluded.data, fetched_at=excluded.fetched_at`, key, stored, nowUnix())
	return m, nil
}

// GET /api/meta/art → {art: {ref: {cover, banner}}}: a arte das fichas, para os cards.
func libArtHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	art, err := l.EntryArts(r.Context(), l.viewer(r))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"art": art})
}

// GET /api/meta?kind=anime|manga|movie&q=Título → {meta: ExtMeta|null}
func libMetaHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	// ficha curada no cadastro vence a busca automática
	if ref := r.URL.Query().Get("ref"); ref != "" && validEntryRef(ref) && l.entryVisible(r.Context(), l.viewer(r), ref) {
		if d, err := l.GetEntryMeta(r.Context(), ref); err == nil && d != nil && (d.Synopsis != "" || d.Cover != "" || d.Banner != "" || d.URL != "") {
			libJSON(w, map[string]any{"meta": d.ExtMeta.public(), "curated": true})
			return
		}
		if kind == "" {
			// só a ficha foi pedida (série que não é anime): sem busca automática
			libJSON(w, map[string]any{"meta": nil})
			return
		}
	}
	if kind != "anime" && kind != "manga" && kind != "movie" {
		libFail(w, errInvalid)
		return
	}
	m, err := l.LookupMeta(r.Context(), kind, r.URL.Query().Get("q"))
	if err != nil && !errors.Is(err, errInvalid) {
		logWarn("metadados externos (%s %q): %v", kind, r.URL.Query().Get("q"), err)
		libError(w, http.StatusBadGateway, "metadados indisponíveis")
		return
	}
	if err != nil {
		libFail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	libJSON(w, map[string]any{"meta": m})
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
