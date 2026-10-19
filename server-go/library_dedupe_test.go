package main

// Tópicos duplicados: normalização, pontuação, detecção, fusão e desfazer.
// Nomes sintéticos.

import (
	"context"
	"database/sql"
	"testing"
)

func TestTopicNameScore(t *testing.T) {
	same := [][2]string{
		{"Criadora Exemplo", "criadora_exemplo"},
		{"Criadora Exemplo", "CRIADORA-EXEMPLO (2023)"},
		{"Criadora Exemplo", "criadora exemplo part 2"},
		{"n3r0 ex3mpl0", "nero exemplo"},
		{"Serie Exemplo", "Serie Exemplo The Animation"},
		{"Ção Acentuada", "cao acentuada"},
		{"𝓒𝓻𝓲𝓪𝓭𝓸𝓻𝓪 Exemplo", "criadora exemplo"},
		{"Criadora Exemplo", "criadora exemplo - pack"},
	}
	for _, p := range same {
		if s := topicNameScore(p[0], p[1]); s < topicDupReview {
			t.Errorf("%q x %q: %.3f, esperava >= %.2f", p[0], p[1], s, topicDupReview)
		}
	}
	diff := [][2]string{
		{"CODIGO-123", "CODIGO-124"},
		{"Fulana", "Beltrana"},
		{"Serie Alfa", "Serie Beta Gama"},
		{"abc", "abd"},
	}
	for _, p := range diff {
		if s := topicNameScore(p[0], p[1]); s >= topicDupReview {
			t.Errorf("%q x %q: %.3f, esperava < %.2f", p[0], p[1], s, topicDupReview)
		}
	}
}

func topicOf(t *testing.T, l *Library, mediaID int64, kind string) (int64, string) {
	t.Helper()
	var id int64
	var title string
	err := l.r.QueryRow(`SELECT tp.id, tp.title FROM topic_items ti JOIN topics tp ON tp.id = ti.topic_id WHERE ti.media_id = ? AND tp.kind = ?`, mediaID, kind).Scan(&id, &title)
	if err != nil {
		t.Fatalf("tópico %s do item %d: %v", kind, mediaID, err)
	}
	return id, title
}

func TestTopicDedupeMergeUndo(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	cyber := fx.ids["Criadores/criador-sintetico/abcd1234efgh.mp4"]
	filme := fx.ids["Filmes/Filme Teste (2020).mp4"]
	canon, _ := topicOf(t, l, cyber, "creator")

	// Uma grafia diferente do mesmo criador, com outro item.
	var dup int64
	if err := l.tx(ctx, func(tx *sql.Tx) error {
		var err error
		dup, err = ensureTopic(tx, "creator", "Criador_Sintetico (2024)", "manual")
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO topic_items(topic_id, media_id) VALUES (?,?)`, dup, filme)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if dup == canon {
		t.Fatal("ensureTopic não deveria casar slugs diferentes antes da fusão")
	}

	rep, err := l.DetectTopicDuplicates(ctx)
	if err != nil || rep.Suggested != 1 {
		t.Fatalf("detecção: %+v %v", rep, err)
	}
	if rep, _ := l.DetectTopicDuplicates(ctx); rep.Suggested != 0 {
		t.Errorf("segunda detecção repetiu sugestões: %+v", rep)
	}
	groups, err := l.TopicDupGroups(ctx)
	if err != nil || len(groups) != 1 || len(groups[0].Members) != 1 {
		t.Fatalf("grupos: %+v %v", groups, err)
	}
	g := groups[0]
	if len(g.Canonical.Examples) == 0 {
		t.Errorf("grupo sem exemplos: %+v", g)
	}

	res, err := l.AcceptTopicMerge(ctx, g.Canonical.ID, []int64{g.Members[0].ID}, "adm")
	if err != nil || res.Merged != 1 {
		t.Fatalf("fusão: %+v %v", res, err)
	}
	keep, drop := g.Canonical.ID, g.Members[0].ID
	if id, _ := topicOf(t, l, filme, "creator"); id != keep {
		t.Errorf("item do duplicado não foi para o canônico")
	}
	if id, _ := topicOf(t, l, cyber, "creator"); id != keep {
		t.Errorf("item do canônico mudou de tópico")
	}
	if groups, _ := l.TopicDupGroups(ctx); len(groups) != 0 {
		t.Errorf("sugestão continuou pendente: %+v", groups)
	}
	// Itens novos com a grafia antiga (ou outra grafia que virou alias) vão para o canônico.
	if err := l.tx(ctx, func(tx *sql.Tx) error {
		id, err := ensureTopic(tx, "creator", "criador sintetico (2024)", "auto")
		if err == nil && id != keep {
			t.Errorf("alias não resolveu para o canônico: %d", id)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	n, err := l.UndoTopicMerge(ctx, res.BatchID)
	if err != nil || n != 1 {
		t.Fatalf("desfazer: %d %v", n, err)
	}
	if id, _ := topicOf(t, l, filme, "creator"); id != drop {
		t.Errorf("desfazer não devolveu o item ao tópico original (%d)", id)
	}
	var cnt int
	l.r.QueryRow(`SELECT COUNT(*) FROM topic_items WHERE topic_id = ?`, keep).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("canônico deveria voltar a ter 1 item, tem %d", cnt)
	}
	l.r.QueryRow(`SELECT COUNT(*) FROM topic_aliases WHERE topic_id = ?`, keep).Scan(&cnt)
	if cnt != 0 {
		t.Errorf("aliases criados pela fusão não foram removidos: %d", cnt)
	}
	if _, err := l.UndoTopicMerge(ctx, res.BatchID); err == nil {
		t.Errorf("desfazer duas vezes deveria falhar")
	}

	// Rejeitar: o par não volta a ser sugerido.
	l.DetectTopicDuplicates(ctx)
	groups, _ = l.TopicDupGroups(ctx)
	if len(groups) != 1 {
		t.Fatalf("depois do desfazer, esperava a sugestão de volta: %+v", groups)
	}
	if n, err := l.RejectTopicMerge(ctx, []int64{keep, drop}, "adm"); err != nil || n != 1 {
		t.Fatalf("rejeitar: %d %v", n, err)
	}
	l.DetectTopicDuplicates(ctx)
	if groups, _ := l.TopicDupGroups(ctx); len(groups) != 0 {
		t.Errorf("par rejeitado voltou: %+v", groups)
	}
}
