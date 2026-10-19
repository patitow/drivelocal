package main

// Modo restrito, cadastro de entradas e busca de metadados. Nomes sintéticos.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRestrictedModeCapsViewer(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	if v := l.viewerFor("u-admin"); v.MaxMaturity != MaturityExplicito {
		t.Fatalf("admin começa com %d", v.MaxMaturity)
	}
	p := l.GetPrefs(ctx, "u-admin")
	if !p.RestrictedAllowed || p.RestrictedMode == nil || !*p.RestrictedMode {
		t.Fatalf("prefs do admin: %+v", p)
	}
	if err := l.SetRestrictedMode(ctx, "u-admin", false); err != nil {
		t.Fatal(err)
	}
	if v := l.viewerFor("u-admin"); v.MaxMaturity != MaturitySugestivo {
		t.Fatalf("modo restrito desligado e o admin vê %d", v.MaxMaturity)
	}
	// o restrito some da busca com o modo desligado
	page, err := l.QueryItems(ctx, l.viewerFor("u-admin"), ItemQuery{Root: "Animes", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range page.Items {
		if it.Maturity == "explicito" {
			t.Fatalf("item explícito com modo restrito desligado: %s", it.Path)
		}
	}
	if err := l.SetRestrictedMode(ctx, "u-admin", true); err != nil {
		t.Fatal(err)
	}
	if v := l.viewerFor("u-admin"); v.MaxMaturity != MaturityExplicito {
		t.Fatal("religar o modo restrito não voltou")
	}
	// usuário sem permissão de explícito não tem o botão
	if p := l.GetPrefs(ctx, "u-new"); p.RestrictedAllowed || *p.RestrictedMode {
		t.Fatalf("usuário comum: %+v", p)
	}
}

func TestEntriesCreateEditDelete(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	cyber := fx.ids["Criadores/criador-sintetico/abcd1234efgh.mp4"]
	title := "Serie Sintetica Nova"
	ref, err := l.CreateEntry(ctx, EntryInput{Cat: "series", Title: &title, AddIDs: []int64{cyber},
		Meta: &EntryData{ExtMeta: ExtMeta{Synopsis: "Uma sinopse curada.", Year: 2001}}}, "adm")
	if err != nil {
		t.Fatal(err)
	}
	find := func(cat, q string) *Entry {
		list, err := l.ListEntries(ctx, cat, q, 0)
		if err != nil {
			t.Fatal(err)
		}
		for i := range list {
			if list[i].Ref == ref {
				return &list[i]
			}
		}
		return nil
	}
	e := find("series", "sintetica")
	if e == nil || e.Count != 1 || e.Meta == nil || e.Meta.Synopsis != "Uma sinopse curada." || e.Meta.Cat != "series" {
		t.Fatalf("série nova: %+v", e)
	}
	if find("animes", "") != nil {
		t.Fatal("série fora de Animes apareceu em animes")
	}
	// a série de anime do fixture fica em animes
	if list, _ := l.ListEntries(ctx, "animes", "exemplo", 0); len(list) != 1 {
		t.Fatalf("animes: %+v", list)
	}

	// só ela na estante de séries (a de anime fica fora)
	tv, err := l.TVSeries(ctx, l.viewerFor("u-admin"))
	if err != nil || len(tv) != 1 || tv[0].Title != title || tv[0].Count != 1 {
		t.Fatalf("séries: %+v %v", tv, err)
	}
	// editar: título, apelido, maturidade e ficha (sem perder a categoria)
	name, mat := "Serie Sintetica Renomeada", "sugestivo"
	if err := l.UpdateEntry(ctx, EntryInput{Ref: ref, Title: &name, Aliases: []string{"SSR"}, Maturity: &mat,
		Meta: &EntryData{ExtMeta: ExtMeta{Synopsis: "Outra."}}}, "adm"); err != nil {
		t.Fatal(err)
	}
	e = find("series", "ssr")
	if e == nil || e.Title != name || e.Maturity != "sugestivo" || e.Meta.Synopsis != "Outra." || e.Meta.Cat != "series" {
		t.Fatalf("editada: %+v", e)
	}
	// a ficha curada vence a busca automática em /api/meta
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/meta?kind=anime&q=x&ref="+ref, nil)
	req.Header.Set("X-User-ID", "u-admin")
	libMetaHandler(l, rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Outra.") {
		t.Fatalf("meta curada: %d %s", rec.Code, rec.Body)
	}
	// quem não vê nenhum item da entrada não recebe a ficha
	if l.entryVisible(ctx, Viewer{UserID: "x", MaxMaturity: -1}, ref) || !l.entryVisible(ctx, l.viewerFor("u-admin"), ref) {
		t.Fatal("visibilidade da ficha")
	}
	// tirar o item
	if err := l.UpdateEntry(ctx, EntryInput{Ref: ref, RemIDs: []int64{cyber}}, "adm"); err != nil {
		t.Fatal(err)
	}
	if e = find("series", ""); e == nil || e.Count != 0 {
		t.Fatalf("depois de tirar: %+v", e)
	}
	if err := l.DeleteEntry(ctx, ref, "adm"); err != nil {
		t.Fatal(err)
	}
	if find("series", "") != nil {
		t.Fatal("série apagada continua")
	}
	if d, _ := l.GetEntryMeta(ctx, ref); d != nil {
		t.Fatal("ficha ficou depois de apagar")
	}
}

func TestEntriesFilmAndManga(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	cyber := fx.ids["Criadores/criador-sintetico/abcd1234efgh.mp4"]
	title := "Filme Sintetico"
	ref, err := l.CreateEntry(ctx, EntryInput{Cat: "films", Title: &title, AddIDs: []int64{cyber}}, "adm")
	if err != nil {
		t.Fatal(err)
	}
	films, _ := l.ListEntries(ctx, "films", "", 0)
	got := map[string]string{}
	for _, f := range films {
		got[f.Ref] = f.Title
	}
	if got[ref] != title || len(got) != 2 { // + o da pasta Filmes
		t.Fatalf("filmes: %v", got)
	}
	if err := l.DeleteEntry(ctx, ref, "adm"); err != nil {
		t.Fatal(err)
	}
	if films, _ = l.ListEntries(ctx, "films", "", 0); len(films) != 1 {
		t.Fatalf("filme continuou: %+v", films)
	}

	// pasta de imagens fora de "Mangás" vira obra quando cadastrada
	admin := l.viewerFor("u-admin")
	if works, _ := l.Mangas(ctx, admin); len(works) != 0 {
		t.Fatalf("mangás antes: %+v", works)
	}
	mt := "Manga Sintetico"
	mref, err := l.CreateEntry(ctx, EntryInput{Cat: "mangas", Dir: "Fotos", Title: &mt}, "adm")
	if err != nil {
		t.Fatal(err)
	}
	works, _ := l.Mangas(ctx, admin)
	if len(works) != 1 || works[0].Dir != "Fotos" || works[0].Title != mt || works[0].Pages != 1 {
		t.Fatalf("mangá cadastrado: %+v", works)
	}
	// limpar a ficha mantém o cadastro
	if err := l.UpdateEntry(ctx, EntryInput{Ref: mref, ClearMeta: true}, "adm"); err != nil {
		t.Fatal(err)
	}
	if works, _ = l.Mangas(ctx, admin); len(works) != 1 {
		t.Fatal("limpar a ficha tirou o cadastro")
	}
	if err := l.DeleteEntry(ctx, mref, "adm"); err != nil {
		t.Fatal(err)
	}
	if works, _ = l.Mangas(ctx, admin); len(works) != 0 {
		t.Fatal("descadastrar não tirou o mangá")
	}
	if _, err := l.CreateEntry(ctx, EntryInput{Cat: "mangas", Dir: "Nao Existe"}, "adm"); err != errInvalid {
		t.Fatalf("pasta sem imagens: %v", err)
	}
}

func TestSearchMetaSources(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/anime":
			w.Write([]byte(`{"data":[{"attributes":{"slug":"serie-exemplo","canonicalTitle":"Serie Exemplo","titles":{"en":"Example Series"},
				"synopsis":"Sinopse.","startDate":"2019-04-01","averageRating":"81.6","episodeCount":12,"posterImage":{"large":"https://img/p.jpg"}}}]}`))
		case r.URL.Path == "/search/shows":
			w.Write([]byte(`[{"show":{"url":"https://tvmaze/1","name":"Serie Exemplo","premiered":"2010-01-01","summary":"<p>Uma <b>série</b>.</p>",
				"genres":["Drama"],"rating":{"average":7.5},"image":{"original":"https://img/o.jpg"}}}]`))
		case r.URL.Path == "/serie exemplo.json":
			w.Write([]byte(`{"d":[{"id":"nm1","l":"Pessoa"},{"id":"tt123","l":"Serie Exemplo","y":2015,"q":"TV series","i":{"imageUrl":"https://img/i.jpg"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	pk, pt, pi := kitsuURL, tvmazeURL, imdbSuggURL
	kitsuURL, tvmazeURL, imdbSuggURL = srv.URL, srv.URL, srv.URL
	defer func() { kitsuURL, tvmazeURL, imdbSuggURL = pk, pt, pi }()
	ctx := context.Background()

	k, err := SearchMeta(ctx, "kitsu", "anime", "Serie Exemplo")
	if err != nil || len(k) != 1 || k[0].Title != "Example Series" || k[0].Year != 2019 || k[0].Score != 82 || k[0].Episodes != 12 ||
		k[0].URL != "https://kitsu.app/anime/serie-exemplo" {
		t.Fatalf("kitsu: %+v %v", k, err)
	}
	tv, err := SearchMeta(ctx, "tvmaze", "series", "Serie Exemplo")
	if err != nil || len(tv) != 1 || tv[0].Synopsis != "Uma série." || tv[0].Score != 75 || tv[0].Cover != "https://img/o.jpg" {
		t.Fatalf("tvmaze: %+v %v", tv, err)
	}
	im, err := SearchMeta(ctx, "imdb", "movie", "Serie Exemplo")
	if err != nil || len(im) != 1 || im[0].URL != "https://www.imdb.com/title/tt123/" || im[0].Year != 2015 {
		t.Fatalf("imdb: %+v %v", im, err)
	}
	if _, err := SearchMeta(ctx, "nenhuma", "anime", "x"); err != errInvalid {
		t.Fatalf("fonte inválida: %v", err)
	}
}
