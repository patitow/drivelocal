package main

// Nomes sintéticos.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMangaPathHelpers(t *testing.T) {
	work, chap, ok := splitMangaPath("Torrents/.Mangás/[Autor X] Obra Teste 2 [PT-BR]/9a8b7c6d5e/01.jpg")
	if !ok || work != "Torrents/.Mangás/[Autor X] Obra Teste 2 [PT-BR]" || chap != work+"/9a8b7c6d5e" {
		t.Fatalf("split: %q %q %v", work, chap, ok)
	}
	if _, _, ok := splitMangaPath("Mangas/solta.jpg"); ok {
		t.Fatal("página solta na pasta de mangás não é obra")
	}
	if _, _, ok := splitMangaPath("Fotos/Mangueira/a.jpg"); ok {
		t.Fatal("Mangueira não é pasta de mangás")
	}
	if title, author := mangaWorkTitle(work); title != "Obra Teste 2" || author != "Autor X" {
		t.Fatalf("título: %q autor: %q", title, author)
	}
	for in, want := range map[string]string{"x/9a8b7c6d5e": "Capítulo 3", "x/Cap 07": "Capítulo 7", "x/012": "Capítulo 12", "x/Extra": "Extra"} {
		if got := mangaChapterTitle(in, 3); got != want {
			t.Errorf("%s: %q, quero %q", in, got, want)
		}
	}
}

func TestMangasGroupsWorks(t *testing.T) {
	fx := newLibFixture(t)
	files := []string{
		"Torrents/.Mangás/[Autor X] Obra Teste [PT]/c2/02.jpg",
		"Torrents/.Mangás/[Autor X] Obra Teste [PT]/c2/10.jpg",
		"Torrents/.Mangás/[Autor X] Obra Teste [PT]/c10/01.jpg",
		"Mangas/Outra Obra/01.png",
	}
	for _, f := range files {
		p := filepath.Join(fx.media, filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fx.l.Scan(context.Background(), fx.media); err != nil {
		t.Fatal(err)
	}
	works, err := fx.l.Mangas(context.Background(), fx.l.viewerFor("u-admin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 2 {
		t.Fatalf("esperava 2 obras, veio %+v", works)
	}
	byTitle := map[string]MangaWork{}
	for _, w := range works {
		byTitle[w.Title] = w
	}
	w := byTitle["Obra Teste"]
	if w.Author != "Autor X" || w.Pages != 3 || len(w.Chapters) != 2 {
		t.Fatalf("obra: %+v", w)
	}
	if w.Chapters[0].Title != "Capítulo 2" || w.Chapters[0].Pages != 2 || w.Cover != "Torrents/.Mangás/[Autor X] Obra Teste [PT]/c2/02.jpg" {
		t.Fatalf("capítulos fora de ordem: %+v", w.Chapters)
	}
	if o := byTitle["Outra Obra"]; o.Pages != 1 || o.Chapters[0].Title != "Capítulo único" {
		t.Fatalf("obra de um capítulo só: %+v", o)
	}
}

func TestMergeMangaVolumes(t *testing.T) {
	vol := func(dir, title string) MangaWork {
		return MangaWork{Dir: dir, Title: title, Author: "A", Pages: 2, MTime: 1,
			Chapters: []MangaChapter{{Dir: dir + "/x", Title: "Capítulo único", Pages: 2}}}
	}
	out := mergeMangaVolumes([]MangaWork{
		vol("M/Serie 2 - Sub", "Serie 2 - Sub"),
		vol("M/Serie 1 - Sub", "Serie 1 - Sub"),
		vol("M/Avulsa 3", "Avulsa 3"),
		vol("M/Sem Numero", "Sem Numero"),
	})
	if len(out) != 3 {
		t.Fatalf("esperava 3 obras, veio %+v", out)
	}
	for _, w := range out {
		if w.Title == "Serie - Sub" {
			if w.Pages != 4 || len(w.Chapters) != 2 || w.Chapters[0].Title != "Volume 1" || w.Dir != "M/Serie 1 - Sub" {
				t.Fatalf("volumes juntados errado: %+v", w)
			}
			return
		}
	}
	t.Fatalf("obra juntada não encontrada: %+v", out)
}
