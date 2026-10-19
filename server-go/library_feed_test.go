package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestFeed(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	v := l.viewerFor("u-admin")
	done := true
	ep2 := "Animes/Serie Exemplo/[Grupo] Serie Exemplo - 02 [1080p].mkv"

	// Viu o 1º episódio inteiro: a série entra com o próximo, não com o visto.
	l.SaveProgress(ctx, v.UserID, fx.ids[epVisivel], ProgressInput{PositionS: 1400, DurationS: 1400, Completed: &done})
	p, err := l.Feed(ctx, v, "", 7, 0, 60)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]FeedItem{}
	for _, it := range p.Items {
		if it.Kind != "video" {
			t.Errorf("feed só tem vídeos: %s", it.Path)
		}
		seen[it.Path] = it
	}
	if _, ok := seen[epVisivel]; ok {
		t.Errorf("episódio já visto voltou no feed")
	}
	if it, ok := seen[ep2]; !ok || it.ReasonKind != "next" || it.ReasonTopic == nil {
		t.Errorf("faltou o próximo episódio com motivo: %+v", it)
	}
	if _, ok := seen[epExplic]; ok {
		t.Errorf("série deveria entrar com um episódio só")
	}

	// Paginação continua a mesma ordem (mesma semente) e acaba com next = -1.
	a, _ := l.Feed(ctx, v, "", 7, 0, 2)
	b, _ := l.Feed(ctx, v, "", 7, 2, 60)
	if len(a.Items) != 2 || a.Next != 2 || b.Next != -1 || len(a.Items)+len(b.Items) != len(p.Items) || a.Items[0].ID != p.Items[0].ID || b.Items[0].ID != p.Items[2].ID {
		t.Errorf("paginação: %+v / %+v", a, b)
	}

	// Escopo de filmes.
	f, _ := l.Feed(ctx, v, "films", 7, 0, 60)
	if len(f.Items) != 1 || f.Items[0].Root != "Filmes" {
		t.Errorf("escopo filmes: %+v", f.Items)
	}
	if _, err := l.Feed(ctx, v, "xpto", 7, 0, 60); err == nil {
		t.Errorf("escopo inválido deveria falhar")
	}

	rec := httptest.NewRecorder()
	libTestRouter("u-admin").ServeHTTP(rec, httptest.NewRequest("GET", "/api/catalog/feed?seed=7&limit=2", nil))
	var body FeedPage
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &body) != nil || len(body.Items) != 2 {
		t.Errorf("rota: %d %s", rec.Code, rec.Body.String())
	}
}
