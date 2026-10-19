package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestCleanMetaTitle(t *testing.T) {
	for in, want := range map[string]string{
		"[Grupo] Serie Exemplo - 01 [1080p]": "Serie Exemplo - 01",
		"Filme Teste (2020)":                 "Filme Teste",
		"Serie.Exemplo.S01.1080p.WEB-DL":     "Serie Exemplo S01",
	} {
		if got, _ := cleanMetaTitle(in); got != want {
			t.Errorf("%q → %q, quer %q", in, got, want)
		}
	}
	if _, y := cleanMetaTitle("Filme Teste (2020)"); y != 2020 {
		t.Errorf("ano %d", y)
	}
	if !titleMatches("Serie Exemplo", "Série Exemplo") || titleMatches("Serie Exemplo", "Outra Coisa Qualquer") {
		t.Error("titleMatches")
	}
}

func TestLookupMetaCache(t *testing.T) {
	fx := newLibFixture(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Write([]byte(`{"data":{"Page":{"media":[
			{"format":"TV","title":{"romaji":"Outra Coisa"},"startDate":{"year":2001}},
			{"format":"TV","siteUrl":"https://anilist.co/anime/1","title":{"romaji":"Serie Exemplo"},"description":"Uma <i>sinopse</i>.<br><br>(Source: x)",
			 "startDate":{"year":2019},"coverImage":{"large":"https://img/c.jpg"},"genres":["Ação"]}]}}}`))
	}))
	defer srv.Close()
	prev := aniListURL
	aniListURL = srv.URL
	defer func() { aniListURL = prev }()
	t.Setenv("TMDB_API_KEY", "")
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		m, err := fx.l.LookupMeta(ctx, "anime", "Serie Exemplo")
		if err != nil || m == nil || m.Year != 2019 || m.Cover != "https://img/c.jpg" || m.Synopsis != "Uma sinopse." {
			t.Fatalf("meta: %+v %v", m, err)
		}
	}
	if calls != 1 {
		t.Errorf("cache não usado: %d chamadas", calls)
	}
	// não encontrado também vai para o cache
	for i := 0; i < 2; i++ {
		if m, err := fx.l.LookupMeta(ctx, "anime", "Nada Parecido"); m != nil || err != nil {
			t.Fatalf("%+v %v", m, err)
		}
	}
	if calls != 2 {
		t.Errorf("não encontrado não ficou no cache: %d", calls)
	}
}
