package main

import (
	"context"
	"database/sql"
	"testing"
)

// Itens catalogados por uma versão antiga do leitor de nomes ganham a
// temporada e o episódio da versão atual no próximo scan.
func TestScanRefreshesParsedEpisodes(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	id := fx.ids["Animes/Serie Exemplo/[Grupo] Serie Exemplo - 02 [1080p].mkv"]
	// valor velho errado
	if _, err := fx.l.w.Exec(`UPDATE media SET season = 3, episode = 9 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	// vídeo com episódio e sem temporada fica na temporada 1 (regra das durações)
	if _, err := fx.l.w.Exec(`UPDATE media SET season = 1 WHERE kind='video' AND season IS NULL AND episode IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.l.Scan(ctx, fx.media); err != nil {
		t.Fatal(err)
	}
	var season sql.NullInt64
	var ep sql.NullFloat64
	if err := fx.l.r.QueryRow(`SELECT season, episode FROM media WHERE id = ?`, id).Scan(&season, &ep); err != nil {
		t.Fatal(err)
	}
	if season.Int64 != 1 || ep.Float64 != 2 {
		t.Fatalf("season=%v episode=%v; queria 1 e 2", season, ep)
	}
	// imagem continua sem temporada/episódio
	img := fx.ids["Fotos/imagem-a.jpg"]
	if err := fx.l.r.QueryRow(`SELECT season, episode FROM media WHERE id = ?`, img).Scan(&season, &ep); err != nil {
		t.Fatal(err)
	}
	if season.Valid || ep.Valid {
		t.Fatalf("imagem ganhou season=%v episode=%v", season, ep)
	}
}
