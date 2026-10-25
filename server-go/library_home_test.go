package main

// Continuar assistindo dispensável, busca agrupada por série e home.
// Nomes sintéticos.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

func TestContinueDismiss(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	v := l.viewerFor("u-old")
	ep1, ep2 := fx.ids[epVisivel], fx.ids[epEscondid]
	for _, id := range []int64{ep1, ep2} {
		if _, err := l.SaveProgress(ctx, v.UserID, id, ProgressInput{PositionS: 300, DurationS: 1400}); err != nil {
			t.Fatal(err)
		}
	}
	l.SetFavorite(ctx, v.UserID, ep1, true)
	ids := func() map[int64]bool {
		items, err := l.ContinueWatching(ctx, v, 50)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]bool{}
		for _, it := range items {
			out[it.ID] = true
		}
		return out
	}
	if got := ids(); !got[ep1] || !got[ep2] {
		t.Fatalf("antes de dispensar: %v", got)
	}
	if _, err := l.DismissContinue(ctx, v.UserID, ep1); err != nil {
		t.Fatal(err)
	}
	if got := ids(); got[ep1] || !got[ep2] {
		t.Errorf("depois de dispensar um: %v", got)
	}
	// Progresso, favorito e tags continuam.
	it, err := l.GetItem(ctx, v, ep1)
	if err != nil || it.Progress == nil || it.Progress.PositionS != 300 || !it.Favorite {
		t.Errorf("dispensar mexeu no item: %+v %v", it, err)
	}
	// Progresso novo traz de volta.
	l.w.Exec(`UPDATE continue_dismissed SET dismissed_at = dismissed_at - 10 WHERE media_id = ?`, ep1)
	if _, err := l.SaveProgress(ctx, v.UserID, ep1, ProgressInput{PositionS: 400, DurationS: 1400}); err != nil {
		t.Fatal(err)
	}
	if got := ids(); !got[ep1] {
		t.Errorf("progresso novo deveria trazer o item de volta: %v", got)
	}
	// Limpar tudo, pela rota.
	r := libTestRouter("u-old")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/me/continue", nil))
	if rec.Code != 200 {
		t.Fatalf("limpar tudo: %d %s", rec.Code, rec.Body.String())
	}
	if got := ids(); len(got) != 0 {
		t.Errorf("depois de limpar tudo: %v", got)
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/me/continue/999999", nil))
	if rec.Code != 404 {
		t.Errorf("item inexistente: %d", rec.Code)
	}
}

func TestSearchGroupedBySeries(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	v := l.viewerFor("u-old")
	l.SaveProgress(ctx, v.UserID, fx.ids[epVisivel], ProgressInput{PositionS: 1390, DurationS: 1400})

	res, err := l.SearchGrouped(ctx, v, ItemQuery{Q: "exemplo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 1 || res.Groups[0].Type != "series" {
		t.Fatalf("esperava 1 série: %+v", res.Groups)
	}
	s := res.Groups[0].Series
	if s.Total != 3 || s.Matched != 3 || s.Title == "" || s.CoverPath == "" || s.Watched != 1 {
		t.Errorf("série: %+v", s)
	}
	if s.Next == nil || s.Next.Path != epEscondid {
		t.Errorf("próximo episódio deveria ser o 02: %+v", s.Next)
	}
	if s.Items[0].Path != epVisivel {
		t.Errorf("episódios fora de ordem: %v", s.Items[0].Path)
	}

	// Filme e Criadores ficam soltos.
	res, _ = l.SearchGrouped(ctx, v, ItemQuery{Q: "filme"})
	if len(res.Groups) != 1 || res.Groups[0].Type != "item" {
		t.Errorf("filme deveria ser item solto: %+v", res.Groups)
	}

	// Usuário novo (maturidade padrão) com um episódio explícito: some da série.
	exp := "explicito"
	l.ApplyActions(ctx, []int64{fx.ids[epExplic]}, Actions{Maturity: &exp}, "manual", "adm", "t", false)
	res, _ = l.SearchGrouped(ctx, l.viewerFor("u-new"), ItemQuery{Q: "exemplo"})
	if len(res.Groups) != 1 || res.Groups[0].Series == nil || res.Groups[0].Series.Total != 2 {
		t.Errorf("série para o usuário novo: %+v", res.Groups)
	}
	for _, it := range res.Groups[0].Series.Items {
		if it.Path == epExplic {
			t.Errorf("episódio explícito apareceu")
		}
	}

	// Rota.
	rec := httptest.NewRecorder()
	libTestRouter("u-old").ServeHTTP(rec, httptest.NewRequest("GET", "/api/catalog/search?q=exemplo&group=series", nil))
	var body GroupedSearch
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &body) != nil || len(body.Groups) != 1 {
		t.Errorf("rota: %d %s", rec.Code, rec.Body.String())
	}
}

func TestHomeShelves(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	v := l.viewerFor("u-new")
	exp := "explicito"
	l.ApplyActions(ctx, []int64{fx.ids[epExplic]}, Actions{Maturity: &exp}, "manual", "adm", "t", false)
	l.SaveProgress(ctx, v.UserID, fx.ids[epVisivel], ProgressInput{PositionS: 300, DurationS: 1400})
	l.SetFavorite(ctx, v.UserID, fx.ids["Filmes/Filme Teste (2020).mp4"], true)
	l.SetHidden(ctx, v.UserID, fx.ids[epEscondid], true)
	// série feita só de imagens (páginas de mangá) não abre na estante Animes
	if err := l.tx(ctx, func(tx *sql.Tx) error {
		id, err := ensureTopic(tx, "series", "Obra Ilustrada", "manual")
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO topic_items(topic_id, media_id) VALUES (?,?)`, id, fx.ids["Fotos/imagem-a.jpg"])
		return err
	}); err != nil {
		t.Fatal(err)
	}

	h, err := l.Home(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	shelves := map[string]HomeShelf{}
	for _, s := range h.Shelves {
		shelves[s.ID] = s
		for _, it := range s.Items {
			if it.Path == epExplic || it.Path == epEscondid {
				t.Errorf("prateleira %s mostrou item proibido/escondido %s", s.ID, it.Path)
			}
		}
	}
	for _, id := range []string{"continue", "favorites", "recent", "top-series"} {
		if _, ok := shelves[id]; !ok {
			t.Errorf("faltou a prateleira %s (tem %v)", id, keys(shelves))
		}
	}
	if s := shelves["continue"]; len(s.Items) != 1 || len(s.Actions) == 0 {
		t.Errorf("continuar: %+v", s)
	}
	if s := shelves["top-series"]; len(s.Topics) == 0 || s.Topics[0].Count != 1 {
		t.Errorf("série em destaque deveria contar só o visível: %+v", s.Topics)
	}
	for _, tc := range shelves["top-series"].Topics {
		if tc.Title == "Obra Ilustrada" {
			t.Errorf("série só de imagens não deveria ir para o destaque")
		}
	}

	// Cache curto, invalidado por escrita do usuário.
	h2, _ := l.Home(ctx, v)
	if h2 != h {
		t.Errorf("segunda chamada deveria vir do cache")
	}
	l.DismissContinue(ctx, v.UserID, fx.ids[epVisivel])
	h3, _ := l.Home(ctx, v)
	if h3 == h {
		t.Errorf("dispensar deveria invalidar a home")
	}
	for _, s := range h3.Shelves {
		if s.ID == "continue" {
			t.Errorf("continuar deveria sumir depois de dispensar: %+v", s.Items)
		}
	}
	homeCache.Range(func(k, val any) bool {
		e := val.(homeEntry)
		e.at = time.Now().Add(-time.Hour)
		homeCache.Store(k, e)
		return true
	})
	if h4, _ := l.Home(ctx, v); h4 == h3 {
		t.Errorf("cache vencido deveria ser refeito")
	}
}

func keys(m map[string]HomeShelf) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

func libTestRouter(as string) *mux.Router {
	r := mux.NewRouter()
	api := r.PathPrefix("/api").Subrouter()
	api.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			req.Header.Set("X-User-ID", as)
			next.ServeHTTP(w, req)
		})
	})
	registerLibraryRoutes(api)
	return r
}

// A home muda quando o usuário troca uma preferência de visibilidade: guardada no navegador, o
// refetch logo depois da troca recebia a home de antes e mostrava o que devia sumir.
func TestHomeNotCachedByBrowser(t *testing.T) {
	fx := newLibFixture(t)
	r := httptest.NewRequest("GET", "/api/catalog/home", nil)
	r.Header.Set("X-User-ID", "u-new")
	rec := httptest.NewRecorder()
	libHomeHandler(fx.l, rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, quer no-store", got)
	}
}

// Cards de tópico levam a classificação do que mostram (capa e itens recentes): o cliente usa isso como
// segunda barreira para tirar o explícito de uma resposta velha quando o modo está desligado.
func TestTopicCardsCarryMaturity(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	all := Viewer{UserID: "u-admin", IsAdmin: true, MaxMaturity: MaturityExplicito}
	want := map[string]string{"Serie Exemplo": "geral", "Titulo Restrito": "explicito"}

	infos, _, err := fx.l.ListTopics(ctx, all, TopicQuery{Kind: "series"})
	if err != nil {
		t.Fatal(err)
	}
	cards, err := fx.l.topTopics(ctx, all, "series", 20)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][2]string{}
	for _, tp := range infos {
		got[tp.Title] = [2]string{tp.Maturity, got[tp.Title][1]}
	}
	for _, c := range cards {
		got[c.Title] = [2]string{got[c.Title][0], c.Maturity}
	}
	for title, m := range want {
		if got[title] != [2]string{m, m} {
			t.Errorf("%s: listagem/card = %v, quer %s nos dois", title, got[title], m)
		}
	}
}
