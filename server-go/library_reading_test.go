package main

// Progresso de mangá por usuário, novidades de criadores e arquivos
// corrompidos. Nomes sintéticos.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMangaProgressPerUser(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	st := MangaReadState{Chapter: "Mangás/Obra/c2", Page: 4, Pages: 20, Done: []string{"Mangás/Obra/c1"}, At: 2000}
	if err := l.SaveMangaProgress(ctx, "u-old", map[string]MangaReadState{"Mangás/Obra": st}); err != nil {
		t.Fatal(err)
	}
	// estado mais antigo (outra aba, migração atrasada) não sobrescreve
	old := MangaReadState{Chapter: "Mangás/Obra/c1", Page: 1, Pages: 20, At: 1000}
	if err := l.SaveMangaProgress(ctx, "u-old", map[string]MangaReadState{"Mangás/Obra": old}); err != nil {
		t.Fatal(err)
	}
	got, err := l.MangaProgress(ctx, "u-old")
	if err != nil {
		t.Fatal(err)
	}
	g := got["Mangás/Obra"]
	if g.Chapter != st.Chapter || g.Page != 4 || len(g.Done) != 1 || g.At != 2000 {
		t.Errorf("progresso: %+v", g)
	}
	if other, _ := l.MangaProgress(ctx, "u-new"); len(other) != 0 {
		t.Errorf("progresso vazou para outro usuário: %v", other)
	}
	if err := l.SaveMangaProgress(ctx, "u-old", map[string]MangaReadState{"": st}); !errors.Is(err, errInvalid) {
		t.Errorf("obra vazia aceita: %v", err)
	}
}

func TestCreatorNews(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	v := l.viewerFor("u-old")
	n, err := l.CreatorNews(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	if n.SeenAt != 0 || n.Count == 0 {
		t.Fatalf("sem visita, arquivos recentes contam como novidade: %+v", n)
	}
	if err := l.MarkCreatorsSeen(ctx, v.UserID); err != nil {
		t.Fatal(err)
	}
	if n, _ = l.CreatorNews(ctx, v); n.SeenAt == 0 || n.Count != 0 {
		t.Fatalf("depois da visita: %+v", n)
	}
	l.w.Exec(`UPDATE media SET mtime = ? WHERE path LIKE 'Criadores/%'`, time.Now().Add(time.Hour).Unix())
	if n, _ = l.CreatorNews(ctx, v); n.Count != 1 || len(n.IDs) != 1 {
		t.Errorf("item novo depois da visita: %+v", n)
	}
	// marcar como visto não muda a maturidade do usuário
	if p := l.GetPrefs(ctx, "u-new"); p.MaxMaturity != "geral" {
		t.Fatal(p)
	}
	l.MarkCreatorsSeen(ctx, "u-new")
	if p := l.GetPrefs(ctx, "u-new"); p.MaxMaturity != "geral" {
		t.Errorf("maturidade mudou: %+v", p)
	}
}

func TestBrokenMedia(t *testing.T) {
	// ffprobe saiu com erro: o arquivo é o problema
	exitErr := exec.Command(os.Args[0], "-test.flag-que-nao-existe").Run()
	var ee *exec.ExitError
	if !errors.As(exitErr, &ee) {
		t.Skipf("não deu para gerar ExitError: %v", exitErr)
	}
	err := fmt.Errorf("ffprobe: %w: %s", exitErr, "[mov @ 0x1] moov atom not found\n/data/x.mp4: Invalid data found when processing input")
	if r := brokenReason(err, nil); r != "Invalid data found when processing input" {
		t.Errorf("motivo: %q", r)
	}
	if r := brokenReason(context.DeadlineExceeded, nil); r != "" {
		t.Errorf("timeout não é arquivo corrompido: %q", r)
	}
	if r := brokenReason(nil, &MediaInfo{VideoIndex: -1}); r == "" {
		t.Error("sem vídeo nem áudio deveria marcar")
	}

	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	v := l.viewerFor("u-admin")
	rel := "Filmes/Filme Teste (2020).mp4"
	id := fx.ids[rel]
	l.MarkBroken(ctx, rel, "moov atom not found")
	it, err := l.GetItem(ctx, v, id)
	if err != nil || it.Broken != "moov atom not found" {
		t.Fatalf("item marcado: %+v %v", it, err)
	}
	// arquivo trocado (novo download): o scan limpa a marca para sondar de novo
	p := filepath.Join(fx.media, filepath.FromSlash(rel))
	os.WriteFile(p, []byte("conteudo novo e maior"), 0o644)
	os.Chtimes(p, time.Now().Add(time.Minute), time.Now().Add(time.Minute))
	if _, err := l.Scan(ctx, fx.media); err != nil {
		t.Fatal(err)
	}
	if it, _ = l.GetItem(ctx, v, id); it.Broken != "" {
		t.Errorf("scan não limpou a marca: %q", it.Broken)
	}
}
