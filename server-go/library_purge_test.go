package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPurgeGoneKeepsUnmountedRoot(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	gone := "Animes/Serie Exemplo/[Grupo] Serie Exemplo - 03 [1080p].mkv"
	id := fx.ids[gone]
	if _, err := l.SaveProgress(ctx, "u-admin", id, ProgressInput{PositionS: 10, DurationS: 100}); err != nil {
		t.Fatal(err)
	}
	// a pasta de fotos some inteira: raiz vazia parece HD desconectado
	os.Remove(filepath.Join(fx.media, filepath.FromSlash(gone)))
	os.RemoveAll(filepath.Join(fx.media, "Fotos"))
	os.MkdirAll(filepath.Join(fx.media, "Fotos"), 0o755)
	if _, err := l.Scan(ctx, fx.media); err != nil {
		t.Fatal(err)
	}
	var n int
	l.r.QueryRow(`SELECT COUNT(*) FROM media WHERE id=?`, id).Scan(&n)
	if n != 0 {
		t.Fatal("arquivo apagado continuou no banco")
	}
	l.r.QueryRow(`SELECT COUNT(*) FROM watch_progress WHERE media_id=?`, id).Scan(&n)
	if n != 0 {
		t.Fatal("progresso do arquivo apagado ficou")
	}
	l.r.QueryRow(`SELECT COUNT(*) FROM media WHERE path='Fotos/imagem-a.jpg' AND missing=1`).Scan(&n)
	if n != 1 {
		t.Fatal("raiz sem arquivos (desmontada) foi limpa")
	}
	l.r.QueryRow(`SELECT COUNT(*) FROM topics WHERE title='Serie Exemplo'`).Scan(&n)
	if n != 1 {
		t.Fatal("série com itens restantes saiu")
	}
	// a série inteira some: o tópico automático vai junto
	os.RemoveAll(filepath.Join(fx.media, "Animes", "Serie Exemplo"))
	if _, err := l.Scan(ctx, fx.media); err != nil {
		t.Fatal(err)
	}
	l.r.QueryRow(`SELECT COUNT(*) FROM topics WHERE title='Serie Exemplo'`).Scan(&n)
	if n != 0 {
		t.Fatal("tópico automático vazio ficou")
	}
}
