package main

// Busca manual de metadados em bases públicas, para o cadastro: devolve
// vários candidatos de uma fonte e o admin escolhe qual preenche a ficha.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var (
	kitsuURL     = "https://kitsu.app/api/edge"
	jikanURL     = "https://api.jikan.moe/v4"
	imdbSuggURL  = "https://v3.sg.media-imdb.com/suggestion/x"
	tvmazeURL    = "https://api.tvmaze.com"
	wikipediaURL = "https://pt.wikipedia.org"
)

// MetaSources: fontes na ordem em que o cadastro as mostra.
var MetaSources = []string{"anilist", "mal", "kitsu", "imdb", "tmdb", "tvmaze", "wikipedia"}

const metaSearchMax = 8

var reYear4 = regexp.MustCompile(`\b(19|20)\d{2}\b`)

func metaGet(ctx context.Context, u string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if strings.HasPrefix(u, kitsuURL) {
		req.Header.Set("Accept", "application/vnd.api+json") // JSON:API; com application/json o Kitsu responde 406
	} else {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("User-Agent", "DriveLocal/1.0 (catalogo pessoal)")
	resp, err := metaHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// SearchMeta busca q em source; kind é anime | manga | movie | series | person.
func SearchMeta(ctx context.Context, source, kind, q string) ([]ExtMeta, error) {
	q = strings.TrimSpace(q)
	if q == "" || len([]rune(q)) > 200 {
		return nil, errInvalid
	}
	switch source {
	case "anilist":
		return searchAniList(ctx, kind, q)
	case "mal":
		return searchJikan(ctx, kind, q)
	case "kitsu":
		return searchKitsu(ctx, kind, q)
	case "imdb":
		return searchIMDb(ctx, q)
	case "tmdb":
		return searchTMDB(ctx, kind, q)
	case "tvmaze":
		return searchTVmaze(ctx, q)
	case "wikipedia":
		return searchWikipedia(ctx, q)
	}
	return nil, errInvalid
}

func searchAniList(ctx context.Context, kind, q string) ([]ExtMeta, error) {
	const gql = `query ($s: String, $t: MediaType) { Page(perPage: 8) { media(search: $s, type: $t) {
		siteUrl format episodes chapters averageScore genres bannerImage description(asHtml: false)
		title { romaji english } startDate { year } coverImage { extraLarge large } } } }`
	t := "ANIME"
	if kind == "manga" {
		t = "MANGA"
	}
	body, _ := json.Marshal(map[string]any{"query": gql, "variables": map[string]any{"s": q, "t": t}})
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
					Chapters     int      `json:"chapters"`
					AverageScore int      `json:"averageScore"`
					Genres       []string `json:"genres"`
					BannerImage  string   `json:"bannerImage"`
					Description  string   `json:"description"`
					Title        struct{ Romaji, English string }
					StartDate    struct{ Year int }                 `json:"startDate"`
					CoverImage   struct{ ExtraLarge, Large string } `json:"coverImage"`
				} `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	res := []ExtMeta{}
	for _, m := range out.Data.Page.Media {
		title := m.Title.English
		if title == "" {
			title = m.Title.Romaji
		}
		cover := m.CoverImage.ExtraLarge
		if cover == "" {
			cover = m.CoverImage.Large
		}
		eps := m.Episodes
		if kind == "manga" {
			eps = m.Chapters
		}
		res = append(res, ExtMeta{Source: "anilist", URL: m.SiteURL, Title: title, Year: m.StartDate.Year,
			Synopsis: plainSynopsis(m.Description), Cover: cover, Banner: m.BannerImage, Genres: nonNil(m.Genres),
			Score: m.AverageScore, Episodes: eps})
	}
	return res, nil
}

func searchJikan(ctx context.Context, kind, q string) ([]ExtMeta, error) {
	t := "anime"
	if kind == "manga" {
		t = "manga"
	}
	var out struct {
		Data []struct {
			URL          string  `json:"url"`
			Title        string  `json:"title"`
			TitleEnglish string  `json:"title_english"`
			Synopsis     string  `json:"synopsis"`
			Year         int     `json:"year"`
			Score        float64 `json:"score"`
			Episodes     int     `json:"episodes"`
			Chapters     int     `json:"chapters"`
			Aired        struct {
				Prop struct{ From struct{ Year int } } `json:"prop"`
			} `json:"aired"`
			Published struct {
				Prop struct{ From struct{ Year int } } `json:"prop"`
			} `json:"published"`
			Images struct {
				JPG struct {
					Large string `json:"large_image_url"`
				} `json:"jpg"`
			} `json:"images"`
			Genres []struct{ Name string } `json:"genres"`
		} `json:"data"`
	}
	u := fmt.Sprintf("%s/%s?limit=%d&q=%s", jikanURL, t, metaSearchMax, url.QueryEscape(q))
	if err := metaGet(ctx, u, &out); err != nil {
		return nil, fmt.Errorf("myanimelist: %w", err)
	}
	res := []ExtMeta{}
	for _, m := range out.Data {
		e := ExtMeta{Source: "mal", URL: m.URL, Title: m.Title, Year: m.Year, Synopsis: plainSynopsis(m.Synopsis),
			Cover: m.Images.JPG.Large, Score: int(m.Score*10 + 0.5), Episodes: m.Episodes, Genres: []string{}}
		if m.TitleEnglish != "" {
			e.Title = m.TitleEnglish
		}
		if e.Year == 0 {
			e.Year = m.Aired.Prop.From.Year
		}
		if e.Year == 0 {
			e.Year = m.Published.Prop.From.Year
		}
		if t == "manga" {
			e.Episodes = m.Chapters
		}
		for _, g := range m.Genres {
			e.Genres = append(e.Genres, g.Name)
		}
		res = append(res, e)
	}
	return res, nil
}

func searchKitsu(ctx context.Context, kind, q string) ([]ExtMeta, error) {
	t := "anime"
	if kind == "manga" {
		t = "manga"
	}
	var out struct {
		Data []struct {
			Attributes struct {
				Slug           string                            `json:"slug"`
				CanonicalTitle string                            `json:"canonicalTitle"`
				Titles         map[string]string                 `json:"titles"`
				Synopsis       string                            `json:"synopsis"`
				StartDate      string                            `json:"startDate"`
				AverageRating  string                            `json:"averageRating"`
				EpisodeCount   int                               `json:"episodeCount"`
				ChapterCount   int                               `json:"chapterCount"`
				PosterImage    *struct{ Large, Original string } `json:"posterImage"`
				CoverImage     *struct{ Large, Original string } `json:"coverImage"`
			} `json:"attributes"`
		} `json:"data"`
	}
	u := fmt.Sprintf("%s/%s?page[limit]=%d&filter[text]=%s", kitsuURL, t, metaSearchMax, url.QueryEscape(q))
	if err := metaGet(ctx, u, &out); err != nil {
		return nil, fmt.Errorf("kitsu: %w", err)
	}
	res := []ExtMeta{}
	for _, d := range out.Data {
		a := d.Attributes
		e := ExtMeta{Source: "kitsu", URL: "https://kitsu.app/" + t + "/" + a.Slug, Title: a.CanonicalTitle,
			Synopsis: strings.TrimSpace(a.Synopsis), Episodes: a.EpisodeCount, Genres: []string{}}
		if en := a.Titles["en"]; en != "" {
			e.Title = en
		}
		if t == "manga" {
			e.Episodes = a.ChapterCount
		}
		if len(a.StartDate) >= 4 {
			e.Year, _ = strconv.Atoi(a.StartDate[:4])
		}
		if f, err := strconv.ParseFloat(a.AverageRating, 64); err == nil {
			e.Score = int(f + 0.5)
		}
		if a.PosterImage != nil {
			e.Cover = a.PosterImage.Large
		}
		if a.CoverImage != nil {
			e.Banner = a.CoverImage.Large
		}
		res = append(res, e)
	}
	return res, nil
}

// searchIMDb usa o endpoint de sugestões (sem chave): título, ano e pôster,
// sem sinopse.
func searchIMDb(ctx context.Context, q string) ([]ExtMeta, error) {
	var out struct {
		D []struct {
			ID    string `json:"id"`
			L     string `json:"l"`
			Q     string `json:"q"`
			S     string `json:"s"`
			Y     int    `json:"y"`
			Image *struct {
				ImageURL string `json:"imageUrl"`
			} `json:"i"`
		} `json:"d"`
	}
	u := imdbSuggURL + "/" + url.PathEscape(strings.ToLower(q)) + ".json"
	if err := metaGet(ctx, u, &out); err != nil {
		return nil, fmt.Errorf("imdb: %w", err)
	}
	res := []ExtMeta{}
	for _, d := range out.D {
		if !strings.HasPrefix(d.ID, "tt") {
			continue // pessoas e empresas
		}
		e := ExtMeta{Source: "imdb", URL: "https://www.imdb.com/title/" + d.ID + "/", Title: d.L, Year: d.Y, Genres: []string{}}
		if d.Image != nil {
			e.Cover = d.Image.ImageURL
		}
		// tipo e elenco no lugar da sinopse, que o endpoint não traz
		e.Synopsis = strings.Trim(strings.Join([]string{d.Q, d.S}, " · "), " ·")
		res = append(res, e)
		if len(res) == metaSearchMax {
			break
		}
	}
	return res, nil
}

func searchTMDB(ctx context.Context, kind, q string) ([]ExtMeta, error) {
	key := strings.TrimSpace(os.Getenv("TMDB_API_KEY"))
	if key == "" {
		return nil, fmt.Errorf("tmdb: defina TMDB_API_KEY no servidor")
	}
	path := "/search/movie"
	if kind == "series" || kind == "anime" {
		path = "/search/tv"
	}
	v := url.Values{"query": {q}, "language": {"pt-BR"}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, tmdbBaseURL+path+"?"+v.Encode(), nil)
	if len(key) > 40 {
		req.Header.Set("Authorization", "Bearer "+key)
	} else {
		qq := req.URL.Query()
		qq.Set("api_key", key)
		req.URL.RawQuery = qq.Encode()
	}
	resp, err := metaHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tmdb: falha de rede") // o erro traz a URL com a chave
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tmdb: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Results []struct {
			ID           int64   `json:"id"`
			Title        string  `json:"title"`
			Name         string  `json:"name"`
			Overview     string  `json:"overview"`
			ReleaseDate  string  `json:"release_date"`
			FirstAir     string  `json:"first_air_date"`
			PosterPath   string  `json:"poster_path"`
			BackdropPath string  `json:"backdrop_path"`
			VoteAverage  float64 `json:"vote_average"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	res := []ExtMeta{}
	for _, m := range out.Results {
		e := ExtMeta{Source: "tmdb", Title: m.Title, Synopsis: strings.TrimSpace(m.Overview), Genres: []string{}, Score: int(m.VoteAverage*10 + 0.5)}
		date, kindPath := m.ReleaseDate, "movie"
		if path == "/search/tv" {
			e.Title, date, kindPath = m.Name, m.FirstAir, "tv"
		}
		e.URL = fmt.Sprintf("https://www.themoviedb.org/%s/%d", kindPath, m.ID)
		if len(date) >= 4 {
			e.Year, _ = strconv.Atoi(date[:4])
		}
		if m.PosterPath != "" {
			e.Cover = "https://image.tmdb.org/t/p/w500" + m.PosterPath
		}
		if m.BackdropPath != "" {
			e.Banner = "https://image.tmdb.org/t/p/w1280" + m.BackdropPath
		}
		res = append(res, e)
		if len(res) == metaSearchMax {
			break
		}
	}
	return res, nil
}

func searchTVmaze(ctx context.Context, q string) ([]ExtMeta, error) {
	var out []struct {
		Show struct {
			URL       string   `json:"url"`
			Name      string   `json:"name"`
			Premiered string   `json:"premiered"`
			Summary   string   `json:"summary"`
			Genres    []string `json:"genres"`
			Rating    struct {
				Average float64 `json:"average"`
			} `json:"rating"`
			Image *struct{ Medium, Original string } `json:"image"`
		} `json:"show"`
	}
	if err := metaGet(ctx, tvmazeURL+"/search/shows?q="+url.QueryEscape(q), &out); err != nil {
		return nil, fmt.Errorf("tvmaze: %w", err)
	}
	res := []ExtMeta{}
	for _, r := range out {
		s := r.Show
		e := ExtMeta{Source: "tvmaze", URL: s.URL, Title: s.Name, Synopsis: plainSynopsis(s.Summary),
			Genres: nonNil(s.Genres), Score: int(s.Rating.Average*10 + 0.5)}
		if len(s.Premiered) >= 4 {
			e.Year, _ = strconv.Atoi(s.Premiered[:4])
		}
		if s.Image != nil {
			e.Cover = s.Image.Original
		}
		res = append(res, e)
		if len(res) == metaSearchMax {
			break
		}
	}
	return res, nil
}

// searchWikipedia: busca na Wikipédia em português e pega o resumo de cada página.
func searchWikipedia(ctx context.Context, q string) ([]ExtMeta, error) {
	var out struct {
		Pages []struct {
			Key   string `json:"key"`
			Title string `json:"title"`
		} `json:"pages"`
	}
	if err := metaGet(ctx, wikipediaURL+"/w/rest.php/v1/search/page?limit=5&q="+url.QueryEscape(q), &out); err != nil {
		return nil, fmt.Errorf("wikipedia: %w", err)
	}
	res := []ExtMeta{}
	for _, p := range out.Pages {
		var s struct {
			Title         string `json:"title"`
			Extract       string `json:"extract"`
			Description   string `json:"description"`
			OriginalImage *struct {
				Source string `json:"source"`
			} `json:"originalimage"`
			ContentURLs struct {
				Desktop struct {
					Page string `json:"page"`
				} `json:"desktop"`
			} `json:"content_urls"`
		}
		if err := metaGet(ctx, wikipediaURL+"/api/rest_v1/page/summary/"+url.PathEscape(p.Key), &s); err != nil {
			continue
		}
		e := ExtMeta{Source: "wikipedia", URL: s.ContentURLs.Desktop.Page, Title: s.Title, Synopsis: strings.TrimSpace(s.Extract), Genres: []string{}}
		if e.Title == "" {
			e.Title = p.Title
		}
		if s.OriginalImage != nil {
			e.Cover = s.OriginalImage.Source
		}
		if m := reYear4.FindString(s.Description); m != "" {
			e.Year, _ = strconv.Atoi(m)
		}
		res = append(res, e)
	}
	return res, nil
}

func libMetaSearchHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("source") == "" {
		libJSON(w, map[string]any{"sources": MetaSources})
		return
	}
	res, err := SearchMeta(r.Context(), q.Get("source"), q.Get("kind"), q.Get("q"))
	if err == errInvalid {
		libFail(w, err)
		return
	}
	if err != nil {
		logWarn("busca de metadados (%s %q): %v", q.Get("source"), q.Get("q"), err)
		libError(w, http.StatusBadGateway, "fonte indisponível: "+err.Error())
		return
	}
	libJSON(w, map[string]any{"results": res})
}
