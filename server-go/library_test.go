package main

// Testes da biblioteca. Todos os nomes de pastas e arquivos são sintéticos.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

var libTestFiles = []string{
	"Filmes/Filme Teste (2020).mp4",
	"Animes/Serie Exemplo/[Grupo] Serie Exemplo - 01 [1080p].mkv",
	"Animes/Serie Exemplo/[Grupo] Serie Exemplo - 02 [1080p].mkv",
	"Animes/Serie Exemplo/[Grupo] Serie Exemplo - 03 [1080p].mkv",
	"Animes/Restrito/Titulo Restrito/Titulo Restrito 01.mkv",
	"Criadores/criador-sintetico/abcd1234efgh.mp4",
	"Fotos/imagem-a.jpg",
}

type libFixture struct {
	l     *Library
	media string
	ids   map[string]int64
}

func newLibFixture(t *testing.T) *libFixture {
	t.Helper()
	root := t.TempDir()
	media := filepath.Join(root, "media")
	for _, f := range libTestFiles {
		p := filepath.Join(media, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("conteudo "+f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Usuários que existem quando o banco nasce mantêm acesso total.
	usersMutex.Lock()
	prev := users
	users = []User{
		{ID: "u-admin", Username: "adm", Role: "admin", Active: true},
		{ID: "u-old", Username: "antigo", Role: "user", Active: true},
	}
	usersMutex.Unlock()
	prevMedia := MEDIA_PATH
	MEDIA_PATH = media
	l, err := OpenLibrary(filepath.Join(root, "db"))
	if err != nil {
		t.Fatal(err)
	}
	usersMutex.Lock()
	users = append(users, User{ID: "u-new", Username: "novo", Role: "user", Active: true})
	usersMutex.Unlock()
	setLibrary(l)
	t.Cleanup(func() {
		setLibrary(nil)
		l.Close()
		MEDIA_PATH = prevMedia
		canonCache.Range(func(k, _ any) bool { canonCache.Delete(k); return true })
		usersMutex.Lock()
		users = prev
		usersMutex.Unlock()
	})
	if _, err := l.Scan(context.Background(), media); err != nil {
		t.Fatal(err)
	}
	fx := &libFixture{l: l, media: media, ids: map[string]int64{}}
	rows, err := l.r.Query(`SELECT id, path FROM media`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var p string
		rows.Scan(&id, &p)
		fx.ids[p] = id
	}
	if len(fx.ids) != len(libTestFiles) {
		t.Fatalf("esperava %d itens catalogados, veio %d", len(libTestFiles), len(fx.ids))
	}
	return fx
}

func (fx *libFixture) paths(t *testing.T, v Viewer, q ItemQuery) []string {
	t.Helper()
	q.Limit = 200
	page, err := fx.l.QueryItems(context.Background(), v, q)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, it := range page.Items {
		out = append(out, it.Path)
	}
	sort.Strings(out)
	return out
}

func TestParseMediaName(t *testing.T) {
	cases := []struct {
		in      string
		title   string
		season  int
		episode float64
	}{
		{"[Grupo] Serie Exemplo - 05 [1080p].mkv", "Serie Exemplo", 0, 5},
		{"Outra.Serie.S02E11.1080p.WEB-DL.mkv", "Outra Serie", 2, 11},
		{"Filme Teste (2020).mp4", "Filme Teste", 0, 0},
		{"Serie Qualquer 2nd Season - 03.mkv", "Serie Qualquer 2nd Season", 2, 3},
	}
	for _, c := range cases {
		p := ParseMediaName(c.in)
		if p.Title != c.title || p.Season != c.season || p.Episode != c.episode {
			t.Errorf("%q: got %+v", c.in, p)
		}
	}
	if !isOpaqueName("abcd1234efgh.mp4") || isOpaqueName("Filme Teste.mp4") {
		t.Error("isOpaqueName incorreto")
	}
}

func TestKeyBetween(t *testing.T) {
	keys := []string{keyBetween("", "")}
	for i := 0; i < 50; i++ { // sempre no fim
		keys = append(keys, keyBetween(keys[len(keys)-1], ""))
	}
	for i := 0; i < 50; i++ { // sempre no começo
		keys = append([]string{keyBetween("", keys[0])}, keys...)
	}
	for i := 0; i < 50; i++ { // sempre no meio dos dois primeiros
		k := keyBetween(keys[0], keys[1])
		keys = append([]string{keys[0], k}, keys[1:]...)
	}
	for i := 1; i < len(keys); i++ {
		if !(keys[i-1] < keys[i]) {
			t.Fatalf("ordem quebrada em %d: %q !< %q", i, keys[i-1], keys[i])
		}
	}
}

func TestAIWindow(t *testing.T) {
	s, e := parseWindow("01:00-08:00")
	if s != 60 || e != 480 {
		t.Fatalf("parse: %d %d", s, e)
	}
	if s, e := parseWindow("lixo"); s != 60 || e != 480 {
		t.Fatalf("padrão: %d %d", s, e)
	}
	at := func(h, m int) time.Time { return time.Date(2026, 1, 1, h, m, 0, 0, time.UTC) }
	if !inWindow(at(1, 0), 60, 480) || inWindow(at(8, 0), 60, 480) || inWindow(at(0, 59), 60, 480) {
		t.Error("janela simples")
	}
	s, e = parseWindow("22:00-06:00")
	if !inWindow(at(23, 0), s, e) || !inWindow(at(5, 59), s, e) || inWindow(at(12, 0), s, e) {
		t.Error("janela que cruza meia-noite")
	}
}

func TestParseAIOutput(t *testing.T) {
	out, refused, err := parseAIOutput("```json\n{\"title\":\"X\",\"maturity\":\"sugestivo\",\"tags\":[\"a\"],\"confidence\":0.8}\n```")
	if err != nil || refused || out.Maturity != "sugestivo" {
		t.Fatalf("válido: %v %v %+v", err, refused, out)
	}
	if _, refused, _ := parseAIOutput("I'm sorry, I can't help with that."); !refused {
		t.Error("recusa em texto não detectada")
	}
	if _, refused, _ := parseAIOutput(`{"refused":true}`); !refused {
		t.Error("recusa explícita não detectada")
	}
	if _, _, err := parseAIOutput(`{"maturity":"proibido"}`); err == nil {
		t.Error("maturidade inválida aceita")
	}
}

func TestLibraryVisibilityAndPolicies(t *testing.T) {
	fx := newLibFixture(t)
	l := fx.l
	admin, old, nw := l.viewerFor("u-admin"), l.viewerFor("u-old"), l.viewerFor("u-new")
	if !admin.IsAdmin || old.MaxMaturity != MaturityExplicito || nw.MaxMaturity != MaturityGeral {
		t.Fatalf("viewers: %+v %+v %+v", admin, old, nw)
	}
	all := fx.paths(t, admin, ItemQuery{})
	if len(all) != len(libTestFiles) {
		t.Fatalf("admin vê %d", len(all))
	}
	newSees := fx.paths(t, nw, ItemQuery{})
	for _, p := range []string{"Animes/Restrito/Titulo Restrito/Titulo Restrito 01.mkv", "Criadores/criador-sintetico/abcd1234efgh.mp4"} {
		if contains(newSees, p) || l.canSeePath(nw, p) {
			t.Errorf("usuário novo não deveria ver %s", p)
		}
		if !l.canSeePath(old, p) {
			t.Errorf("usuário antigo deveria ver %s", p)
		}
	}
	if !contains(newSees, "Filmes/Filme Teste (2020).mp4") || !l.canSeePath(nw, "Filmes") {
		t.Error("usuário novo deveria ver Filmes")
	}
	if l.canSeePath(nw, "Criadores") || l.canSeePath(nw, "Criadores/outra-pasta-nao-catalogada") {
		t.Error("pasta explícita visível para usuário geral")
	}

	// Pasta oculta pela política some para quem não é admin.
	if err := l.SetPolicy(context.Background(), "Fotos", nil, true, "teste"); err != nil {
		t.Fatal(err)
	}
	if l.canSeePath(old, "Fotos/imagem-a.jpg") || contains(fx.paths(t, old, ItemQuery{}), "Fotos/imagem-a.jpg") {
		t.Error("política hidden não aplicada")
	}
	if !l.canSeePath(admin, "Fotos/imagem-a.jpg") {
		t.Error("admin deve ver pasta oculta")
	}

	// Ocultar por usuário.
	id := fx.ids["Filmes/Filme Teste (2020).mp4"]
	if err := l.SetHidden(context.Background(), "u-new", id, true); err != nil {
		t.Fatal(err)
	}
	if l.canSeePath(nw, "Filmes/Filme Teste (2020).mp4") || contains(fx.paths(t, nw, ItemQuery{}), "Filmes/Filme Teste (2020).mp4") {
		t.Error("ocultar por usuário falhou")
	}
	if got := fx.paths(t, nw, ItemQuery{OnlyHidden: true}); !contains(got, "Filmes/Filme Teste (2020).mp4") {
		t.Errorf("lista de ocultos: %v", got)
	}
}

func TestLibrarySearchTopicsAndMerge(t *testing.T) {
	fx := newLibFixture(t)
	l := fx.l
	ctx := context.Background()
	admin := l.viewerFor("u-admin")
	got := fx.paths(t, admin, ItemQuery{Q: "exemplo"})
	if len(got) != 3 {
		t.Fatalf("busca FTS: %v", got)
	}
	topics, _, err := l.ListTopics(ctx, admin, TopicQuery{Kind: "series"})
	if err != nil {
		t.Fatal(err)
	}
	var serie, restrito int64
	for _, tp := range topics {
		switch tp.Title {
		case "Serie Exemplo":
			serie = tp.ID
			if len(tp.Recent) != 3 || tp.Latest == 0 {
				t.Fatalf("itens recentes do tópico: %+v", tp)
			}
		case "Titulo Restrito":
			restrito = tp.ID
		}
	}
	if serie == 0 || restrito == 0 {
		t.Fatalf("tópicos automáticos: %+v", topics)
	}
	page, err := l.QueryItems(ctx, admin, ItemQuery{TopicID: serie, Sort: "episode"})
	if err != nil || len(page.Items) != 3 || page.Items[0].Episode == nil || *page.Items[0].Episode != 1 {
		t.Fatalf("itens do tópico: %v %+v", err, page)
	}
	// Mesclar e reescanear: o tópico de origem não pode voltar.
	if err := l.MergeTopics(ctx, restrito, serie); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(fx.media, "Animes", "Restrito", "Titulo Restrito", "Titulo Restrito 02.mkv"), []byte("novo"), 0o644)
	if _, err := l.Scan(ctx, fx.media); err != nil {
		t.Fatal(err)
	}
	topics, _, _ = l.ListTopics(ctx, admin, TopicQuery{Kind: "series"})
	for _, tp := range topics {
		if tp.Title == "Titulo Restrito" {
			t.Fatal("tópico mesclado foi recriado pelo scan")
		}
	}
	page, _ = l.QueryItems(ctx, admin, ItemQuery{TopicID: serie})
	if page.Total != 5 {
		t.Fatalf("tópico mesclado deveria ter 5 itens, tem %d", page.Total)
	}
}

func TestLibraryActionsRulesUndo(t *testing.T) {
	fx := newLibFixture(t)
	l := fx.l
	ctx := context.Background()
	nw := l.viewerFor("u-new")
	film := fx.ids["Filmes/Filme Teste (2020).mp4"]
	sug := "sugestivo"

	dry, err := l.ApplyActions(ctx, []int64{film}, Actions{Maturity: &sug, AddTags: []string{"Tag Teste"}}, "manual", "adm", "teste", true)
	if err != nil || !dry.DryRun || dry.Changes == 0 {
		t.Fatalf("dry-run: %v %+v", err, dry)
	}
	if !l.canSeePath(nw, "Filmes/Filme Teste (2020).mp4") {
		t.Fatal("dry-run alterou o banco")
	}
	res, err := l.ApplyActions(ctx, []int64{film}, Actions{Maturity: &sug, AddTags: []string{"Tag Teste"}}, "manual", "adm", "teste", false)
	if err != nil {
		t.Fatal(err)
	}
	if l.canSeePath(nw, "Filmes/Filme Teste (2020).mp4") {
		t.Fatal("maturidade manual não aplicada")
	}
	if got := fx.paths(t, l.viewerFor("u-admin"), ItemQuery{Tags: []string{"tag-teste"}}); len(got) != 1 {
		t.Fatalf("filtro por tag: %v", got)
	}
	if _, err := l.Undo(ctx, res.BatchID, "adm"); err != nil {
		t.Fatal(err)
	}
	if !l.canSeePath(nw, "Filmes/Filme Teste (2020).mp4") {
		t.Fatal("undo não restaurou a maturidade")
	}
	if got := fx.paths(t, l.viewerFor("u-admin"), ItemQuery{Tags: []string{"tag-teste"}}); len(got) != 0 {
		t.Fatalf("undo não removeu a tag: %v", got)
	}

	rule := &Rule{Name: "regra", Enabled: true, Match: RuleMatch{PathPrefix: "Animes/Serie Exemplo"}, Actions: Actions{AddTags: []string{"regra-x"}}}
	if err := l.SaveRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	r1, err := l.ApplyRules(ctx, rule.ID, "adm", true)
	if err != nil || r1.Matched != 3 {
		t.Fatalf("regra dry-run: %v %+v", err, r1)
	}
	r2, err := l.ApplyRules(ctx, rule.ID, "adm", false)
	if err != nil || r2.Matched != 3 {
		t.Fatalf("regra: %v %+v", err, r2)
	}
	if got := fx.paths(t, l.viewerFor("u-admin"), ItemQuery{Tags: []string{"regra-x"}}); len(got) != 3 {
		t.Fatalf("regra não marcou: %v", got)
	}
	changes, err := l.ListChanges(ctx, 10)
	if err != nil || len(changes) < 2 {
		t.Fatalf("histórico: %v %+v", err, changes)
	}
}

func TestLibraryProgressFavoritesPlaylists(t *testing.T) {
	fx := newLibFixture(t)
	l := fx.l
	ctx := context.Background()
	nw := l.viewerFor("u-new")
	ep1 := fx.ids["Animes/Serie Exemplo/[Grupo] Serie Exemplo - 01 [1080p].mkv"]
	ep2 := fx.ids["Animes/Serie Exemplo/[Grupo] Serie Exemplo - 02 [1080p].mkv"]
	ep3 := fx.ids["Animes/Serie Exemplo/[Grupo] Serie Exemplo - 03 [1080p].mkv"]

	if _, err := l.SaveProgress(ctx, "u-new", ep1, ProgressInput{PositionS: 300, DurationS: 1400}); err != nil {
		t.Fatal(err)
	}
	cont, err := l.ContinueWatching(ctx, nw, 10)
	if err != nil || len(cont) != 1 || cont[0].ID != ep1 {
		t.Fatalf("continuar: %v %+v", err, cont)
	}
	p, _ := l.SaveProgress(ctx, "u-new", ep1, ProgressInput{PositionS: 1350, DurationS: 1400})
	if !p.Completed {
		t.Fatal("deveria marcar como assistido")
	}
	if cont, _ = l.ContinueWatching(ctx, nw, 10); len(cont) != 0 {
		t.Fatalf("assistido não deve aparecer em continuar: %+v", cont)
	}
	// Progresso é por usuário.
	if it, _ := l.GetItem(ctx, l.viewerFor("u-old"), ep1); it.Progress != nil {
		t.Fatal("progresso vazou para outro usuário")
	}

	// Faixas escolhidas valem para os outros episódios da série (a mais recente de cada tipo)
	if tp := l.TrackPrefs(ctx, "u-new", ep3, "Animes/Serie Exemplo/x.mkv"); tp != (TrackPrefs{}) {
		t.Fatalf("sem escolha ainda: %+v", tp)
	}
	l.SaveProgress(ctx, "u-new", ep1, ProgressInput{PositionS: 1350, DurationS: 1400, AudioTrack: "jpn", SubtitleTrack: "por"})
	l.w.Exec(`UPDATE watch_progress SET updated_at = updated_at - 10`)
	l.SaveProgress(ctx, "u-new", ep2, ProgressInput{PositionS: 10, DurationS: 1400, AudioTrack: "por|Dublado"})
	if tp := l.TrackPrefs(ctx, "u-new", ep3, "Animes/Serie Exemplo/x.mkv"); tp.Audio != "por|Dublado" || tp.Sub != "por" {
		t.Fatalf("faixas da série: %+v", tp)
	}
	if tp := l.TrackPrefs(ctx, "u-old", ep3, "Animes/Serie Exemplo/x.mkv"); tp != (TrackPrefs{}) {
		t.Fatalf("faixas vazaram para outro usuário: %+v", tp)
	}

	if err := l.SetFavorite(ctx, "u-new", ep2, true); err != nil {
		t.Fatal(err)
	}
	if got := fx.paths(t, nw, ItemQuery{Favorites: true}); len(got) != 1 {
		t.Fatalf("favoritos: %v", got)
	}

	name := "Minha lista"
	pl, err := l.CreatePlaylist(ctx, nw, PlaylistInput{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := l.AddPlaylistItems(ctx, nw, pl.ID, []int64{ep1, ep2, ep3}); err != nil || n != 3 {
		t.Fatalf("add: %v %d", err, n)
	}
	if err := l.MovePlaylistItem(ctx, nw, pl.ID, ep3, 0); err != nil { // para o início
		t.Fatal(err)
	}
	if err := l.MovePlaylistItem(ctx, nw, pl.ID, ep1, ep2); err != nil { // depois do 2
		t.Fatal(err)
	}
	items, err := l.PlaylistItems(ctx, nw, pl.ID)
	if err != nil || len(items) != 3 || items[0].ID != ep3 || items[1].ID != ep2 || items[2].ID != ep1 {
		t.Fatalf("ordem da playlist: %v %+v", err, items)
	}
	// Outro usuário não vê nem edita playlist privada.
	if _, err := l.GetPlaylist(ctx, l.viewerFor("u-old"), pl.ID); err == nil {
		t.Fatal("playlist privada visível para outro usuário")
	}
	global := "global"
	if _, err := l.UpdatePlaylist(ctx, nw, pl.ID, PlaylistInput{Visibility: &global}); err == nil {
		t.Fatal("usuário comum não pode criar playlist global")
	}
}

func TestLibraryAPITokensAndSuggestions(t *testing.T) {
	fx := newLibFixture(t)
	l := fx.l
	ctx := context.Background()
	raw, tok, err := l.CreateAPIToken(ctx, "u-old", "cli")
	if err != nil || !strings.HasPrefix(raw, apiTokenPrefix) {
		t.Fatalf("token: %v", err)
	}
	if td := validateAPIToken(raw); td == nil || td.UserID != "u-old" {
		t.Fatal("token válido rejeitado")
	}
	if validateAPIToken(raw+"x") != nil {
		t.Fatal("token inválido aceito")
	}
	if err := l.RevokeAPIToken(ctx, tok.ID); err != nil {
		t.Fatal(err)
	}
	if validateAPIToken(raw) != nil {
		t.Fatal("token revogado aceito")
	}

	film := fx.ids["Filmes/Filme Teste (2020).mp4"]
	if err := l.storeSuggestions(ctx, film, "modelo-teste", &AIOutput{Title: "Filme Teste", Maturity: "sugestivo", Category: "filme", Tags: []string{"drama"}, Confidence: 0.9}); err != nil {
		t.Fatal(err)
	}
	list, total, err := l.ListSuggestions(ctx, ReviewQuery{Status: "pending"})
	if err != nil || total == 0 {
		t.Fatalf("sugestões: %v %d", err, total)
	}
	// Sugestão pendente não muda nada.
	if !l.canSeePath(l.viewerFor("u-new"), "Filmes/Filme Teste (2020).mp4") {
		t.Fatal("sugestão pendente foi aplicada")
	}
	var matID int64
	for _, s := range list {
		if s.Kind == "maturity" {
			matID = s.ID
		}
	}
	res, err := l.ReviewSuggestions(ctx, []int64{matID}, true, "adm")
	if err != nil || res.Accepted != 1 {
		t.Fatalf("aceitar: %v %+v", err, res)
	}
	if l.canSeePath(l.viewerFor("u-new"), "Filmes/Filme Teste (2020).mp4") {
		t.Fatal("sugestão aceita não aplicada")
	}
	if _, err := l.Undo(ctx, res.BatchID, "adm"); err != nil {
		t.Fatal(err)
	}
	if !l.canSeePath(l.viewerFor("u-new"), "Filmes/Filme Teste (2020).mp4") {
		t.Fatal("undo da sugestão falhou")
	}
}

// Rotas HTTP: catálogo filtrado e middleware de acesso nas rotas antigas.
func TestLibraryHTTP(t *testing.T) {
	fx := newLibFixture(t)
	r := mux.NewRouter()
	api := r.PathPrefix("/api").Subrouter()
	api.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			req.Header.Set("X-User-ID", req.URL.Query().Get("as"))
			next.ServeHTTP(w, req)
		})
	})
	api.Use(libraryAccessMiddleware)
	ok := func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) }
	api.HandleFunc("/stream/{path:.*}", ok)
	api.HandleFunc("/files", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, map[string]any{"currentPath": "", "items": []map[string]any{
			{"name": "Filmes", "path": "Filmes", "isDirectory": true},
			{"name": "Criadores", "path": "Criadores", "isDirectory": true},
		}})
	})
	registerLibraryRoutes(api)
	do := func(method, url string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, url, nil))
		return rec
	}

	if rec := do("GET", "/api/stream/Criadores/criador-sintetico/abcd1234efgh.mp4?as=u-new"); rec.Code != 404 {
		t.Fatalf("stream proibido: %d", rec.Code)
	}
	if rec := do("GET", "/api/stream/Criadores/criador-sintetico/abcd1234efgh.mp4?as=u-old"); rec.Code != 200 {
		t.Fatalf("stream permitido: %d", rec.Code)
	}
	rec := do("GET", "/api/files?as=u-new")
	if strings.Contains(rec.Body.String(), "Criadores") || !strings.Contains(rec.Body.String(), "Filmes") {
		t.Fatalf("listagem não filtrada: %s", rec.Body.String())
	}
	if rec := do("GET", "/api/files?as=u-admin"); !strings.Contains(rec.Body.String(), "Criadores") {
		t.Fatal("admin deve ver tudo na listagem")
	}

	rec = do("GET", "/api/catalog/items?as=u-new&limit=100")
	var page ItemPage
	json.Unmarshal(rec.Body.Bytes(), &page)
	for _, it := range page.Items {
		if it.Maturity == "explicito" {
			t.Fatalf("item explícito para usuário geral: %s", it.Path)
		}
	}
	if rec := do("GET", "/api/admin/library/stats?as=u-new"); rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
		t.Fatalf("rota admin aberta para usuário comum: %d", rec.Code)
	}
	if rec := do("PUT", "/api/me/favorites/"+itoa64(fx.ids["Filmes/Filme Teste (2020).mp4"])+"?as=u-new"); rec.Code != 200 {
		t.Fatalf("favoritar: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do("PUT", "/api/me/favorites/"+itoa64(fx.ids["Criadores/criador-sintetico/abcd1234efgh.mp4"])+"?as=u-new"); rec.Code != 404 {
		t.Fatalf("favoritar item invisível deveria dar 404: %d", rec.Code)
	}
	if rec := do("GET", "/api/me/progress?as=u-new&path="+"Filmes/Filme%20Teste%20(2020).mp4"); rec.Code != 200 {
		t.Fatalf("progresso por caminho: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCanonicalMediaRel(t *testing.T) {
	fx := newLibFixture(t)
	_ = fx
	bad := []string{
		"Filmes/../Criadores/x.mp4", "./Criadores/x.mp4", "Criadores/./x.mp4", "..", "../fora.mp4",
		"Criadores//x.mp4", `Criadores\x.mp4`, "Criadores./x.mp4", "Criadores /x.mp4",
		"C:/Windows/x.mp4", "Criadores/x.mp4:stream", "Criadores/x\x00.mp4",
	}
	for _, b := range bad {
		if _, ok := canonicalMediaRel(b); ok {
			t.Errorf("deveria rejeitar %q", b)
		}
	}
	got, ok := canonicalMediaRel("criadores/CRIADOR-SINTETICO/abcd1234efgh.mp4")
	if !ok || got[0] != "Criadores/criador-sintetico/abcd1234efgh.mp4" {
		t.Errorf("caixa real não resolvida: %v %v", got, ok)
	}
	if got, ok := canonicalMediaRel("/Filmes/"); !ok || got[0] != "Filmes" {
		t.Errorf("barras nas pontas: %v", got)
	}
}

// Todas as rotas que servem mídia passam pelo mesmo gate, com qualquer grafia do caminho.
func TestLibraryAccessBypassAttempts(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	exp := "explicito"
	// Item com maturidade manual (fora de pasta com política) também precisa valer com outra caixa.
	if _, err := fx.l.ApplyActions(ctx, []int64{fx.ids["Filmes/Filme Teste (2020).mp4"]}, Actions{Maturity: &exp}, "manual", "adm", "t", false); err != nil {
		t.Fatal(err)
	}
	r := mux.NewRouter()
	api := r.PathPrefix("/api").Subrouter()
	api.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			req.Header.Set("X-User-ID", "u-new")
			next.ServeHTTP(w, req)
		})
	})
	api.Use(libraryAccessMiddleware)
	ok := func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("SERVIDO")) }
	routes := []string{"stream", "download", "media-info", "subtitle", "media-font", "keyframe", "thumbnail"}
	for _, rt := range routes {
		api.HandleFunc("/"+rt+"/{path:.*}", ok)
	}
	api.HandleFunc("/thumbnails", ok).Methods("POST")
	api.HandleFunc("/download-multiple", ok).Methods("POST")

	attempts := []string{
		"Criadores/criador-sintetico/abcd1234efgh.mp4",
		"criadores/criador-sintetico/ABCD1234EFGH.MP4",
		"CRIADORES/criador-sintetico/abcd1234efgh.mp4",
		"Filmes/..%2FCriadores/criador-sintetico/abcd1234efgh.mp4",
		"Filmes/%2E%2E/Criadores/criador-sintetico/abcd1234efgh.mp4",
		"Criadores%5Ccriador-sintetico%5Cabcd1234efgh.mp4",
		"Criadores%2F%2Fcriador-sintetico/abcd1234efgh.mp4",
		"Criadores./criador-sintetico/abcd1234efgh.mp4",
		"Animes/Restrito/Titulo%20Restrito/Titulo%20Restrito%2001.mkv",
		"animes/restrito/titulo%20restrito/TITULO%20RESTRITO%2001.MKV",
		"Filmes/Filme%20Teste%20(2020).mp4",
		"filmes/FILME%20TESTE%20(2020).MP4",
	}
	for _, rt := range routes {
		for _, a := range attempts {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/"+rt+"/"+a, nil))
			if strings.Contains(rec.Body.String(), "SERVIDO") {
				t.Errorf("%s/%s serviu mídia restrita (%d)", rt, a, rec.Code)
			}
		}
	}
	for _, rt := range []string{"/api/thumbnails", "/api/download-multiple"} {
		for _, a := range []string{"criadores/criador-sintetico/abcd1234efgh.mp4", "Filmes/../Criadores/criador-sintetico/abcd1234efgh.mp4", `Criadores\criador-sintetico\abcd1234efgh.mp4`} {
			q := `["` + strings.ReplaceAll(a, "\\", "\\\\") + `"]`
			// chaves em outra caixa ou duplicadas: encoding/json dos handlers
			// aceita todas, então o filtro também tem que pegá-las
			for _, body := range []string{
				`{"paths":` + q + `,"files":[]}`,
				`{"Paths":` + q + `}`,
				`{"FILES":` + q + `}`,
				`{"files":[],"Files":` + q + `}`,
				`{"paths":[],"paths":` + q + `}`,
			} {
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, httptest.NewRequest("POST", rt, strings.NewReader(body)))
				if strings.Contains(rec.Body.String(), "SERVIDO") {
					t.Errorf("%s %s serviu mídia restrita", rt, body)
				}
			}
		}
	}
	// Controle: caminho permitido continua funcionando.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/stream/Animes/Serie%20Exemplo/%5BGrupo%5D%20Serie%20Exemplo%20-%2001%20%5B1080p%5D.mkv", nil))
	if !strings.Contains(rec.Body.String(), "SERVIDO") {
		t.Fatalf("caminho permitido bloqueado: %d %s", rec.Code, rec.Body.String())
	}
}

// Aceite automático: confiança alta aplica sozinha (menos maturidade), baixa
// fica pendente, e o lote pode ser desfeito.
func TestAIAutoAccept(t *testing.T) {
	fx := newLibFixture(t)
	l, ctx := fx.l, context.Background()
	film := fx.ids["Filmes/Filme Teste (2020).mp4"]
	title := func() string {
		var s string
		l.r.QueryRow(`SELECT COALESCE(title_manual, title) FROM media WHERE id=?`, film).Scan(&s)
		return s
	}
	before := title()

	if err := l.storeSuggestions(ctx, film, "m", &AIOutput{Title: "Talvez", Maturity: "sugestivo", Confidence: 0.5}); err != nil {
		t.Fatal(err)
	}
	res, err := l.autoAcceptSuggestions(ctx, film, 0.9)
	if err != nil || res.Accepted != 0 || title() != before {
		t.Fatalf("confiança baixa foi aplicada: %v %+v", err, res)
	}
	l.w.Exec(`DELETE FROM suggestions`)

	if err := l.storeSuggestions(ctx, film, "m", &AIOutput{Title: "Título Certo", Maturity: "sugestivo", Confidence: 0.95}); err != nil {
		t.Fatal(err)
	}
	res, err = l.autoAcceptSuggestions(ctx, film, 0.9)
	if err != nil || res.Accepted != 1 || title() != "Título Certo" {
		t.Fatalf("aceite automático: %v %+v %q", err, res, title())
	}
	var pending int
	l.r.QueryRow(`SELECT COUNT(*) FROM suggestions WHERE kind='maturity' AND status='pending'`).Scan(&pending)
	if pending != 1 {
		t.Fatal("maturidade não ficou para revisão")
	}
	if _, err := l.Undo(ctx, res.BatchID, "adm"); err != nil || title() != before {
		t.Fatalf("undo: %v %q", err, title())
	}
	if parseAutoAccept("0") != 0 || parseAutoAccept("1.5") != 0 || parseAutoAccept("x") != 0 || parseAutoAccept("0.85") != 0.85 {
		t.Fatal("parseAutoAccept")
	}
}
