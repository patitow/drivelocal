package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// Marcar e desmarcar como assistido, um episódio ou uma temporada de uma vez.
func TestSetWatched(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	v := l.viewerFor("u-new")
	ep1, ep2, restrito := fx.ids[epVisivel], fx.ids[epEscondid], fx.ids[epExplic]
	l.w.Exec(`UPDATE media SET duration_s = 1400 WHERE id IN (?, ?)`, ep1, ep2)
	maxima := "explicito"
	if _, err := l.ApplyActions(ctx, []int64{restrito}, Actions{Maturity: &maxima}, "manual", "adm", "t", false); err != nil {
		t.Fatal(err)
	}
	l.SaveProgress(ctx, v.UserID, ep1, ProgressInput{PositionS: 300, DurationS: 1400, AudioTrack: "a:1"})

	r := libTestRouter("u-new")
	rec := httptest.NewRecorder()
	body := `{"ids":[` + itoa64(ep1) + `,` + itoa64(ep2) + `,` + itoa64(restrito) + `],"watched":true}`
	r.ServeHTTP(rec, httptest.NewRequest("POST", "/api/me/watched", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("marcar: %d %s", rec.Code, rec.Body.String())
	}
	for _, id := range []int64{ep1, ep2} {
		it, err := l.GetItem(ctx, v, id)
		if err != nil || it.Progress == nil || !it.Progress.Completed || it.Progress.PositionS != 1400 {
			t.Errorf("item %d não ficou assistido: %+v %v", id, it.Progress, err)
		}
	}
	if it, _ := l.GetItem(ctx, v, ep1); it.Progress.AudioTrack != "a:1" {
		t.Errorf("marcar apagou a faixa escolhida: %+v", it.Progress)
	}
	// o que o usuário não vê não ganha progresso
	var n int
	l.r.QueryRow(`SELECT COUNT(*) FROM watch_progress WHERE media_id = ?`, restrito).Scan(&n)
	if n != 0 {
		t.Errorf("item invisível recebeu progresso")
	}
	if items, _ := l.ContinueWatching(ctx, v, 50); len(items) != 0 {
		t.Errorf("assistido não sai do continuar: %d", len(items))
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("POST", "/api/me/watched", strings.NewReader(`{"ids":[`+itoa64(ep1)+`,`+itoa64(ep2)+`],"watched":false}`)))
	if rec.Code != 200 {
		t.Fatalf("desmarcar: %d %s", rec.Code, rec.Body.String())
	}
	for _, id := range []int64{ep1, ep2} {
		if it, _ := l.GetItem(ctx, v, id); it.Progress != nil {
			t.Errorf("item %d continua com progresso: %+v", id, it.Progress)
		}
	}
}

// Duração corrigida (relida do arquivo) leva junto o progresso salvo com a duração esticada.
func TestFixProgressDuration(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	ep1, ep2 := fx.ids[epVisivel], fx.ids[epEscondid]
	l.SaveProgress(ctx, "u-new", ep1, ProgressInput{PositionS: 1400, DurationS: 2739})
	l.SaveProgress(ctx, "u-new", ep2, ProgressInput{PositionS: 300, DurationS: 2739})
	l.fixProgressDuration(ctx, ep1, 1436)
	l.fixProgressDuration(ctx, ep2, 1436)
	var dur, pos float64
	var done bool
	l.r.QueryRow(`SELECT duration_s, completed FROM watch_progress WHERE media_id = ?`, ep1).Scan(&dur, &done)
	if dur != 1436 || !done {
		t.Errorf("fim do episódio: duração %v concluído %v", dur, done)
	}
	l.r.QueryRow(`SELECT duration_s, position_s, completed FROM watch_progress WHERE media_id = ?`, ep2).Scan(&dur, &pos, &done)
	if dur != 1436 || pos != 300 || done {
		t.Errorf("meio do episódio: duração %v posição %v concluído %v", dur, pos, done)
	}
}
