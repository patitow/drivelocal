package main

// Mover item entre criadores e renomear criador. Nomes sintéticos.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestMoveItemToCreatorSurvivesScanAndUndo(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	cyber := fx.ids["Criadores/criador-sintetico/abcd1234efgh.mp4"]
	from, _ := topicOf(t, l, cyber, "creator")

	res, err := l.ApplyActions(ctx, []int64{cyber}, Actions{
		Topic:       &TopicRef{Kind: "creator", Title: "Outra Pessoa Sintética"},
		RemoveTopic: &TopicRef{ID: from},
	}, "manual", "admin", "mover", false)
	if err != nil {
		t.Fatal(err)
	}
	to, title := topicOf(t, l, cyber, "creator")
	if to == from || title != "Outra Pessoa Sintética" {
		t.Fatalf("item em %d %q", to, title)
	}
	// o scan não devolve ao criador da pasta
	if err := l.postProcess(ctx, []int64{cyber}); err != nil {
		t.Fatal(err)
	}
	var n int
	l.r.QueryRow(`SELECT COUNT(*) FROM topic_items WHERE topic_id=? AND media_id=?`, from, cyber).Scan(&n)
	if n != 0 {
		t.Fatal("o scan devolveu o item ao criador antigo")
	}
	// desfazer volta e tira a exclusão
	if _, err := l.Undo(ctx, res.BatchID, "admin"); err != nil {
		t.Fatal(err)
	}
	if got, _ := topicOf(t, l, cyber, "creator"); got != from {
		t.Fatalf("desfazer: item em %d", got)
	}
	l.r.QueryRow(`SELECT COUNT(*) FROM topic_item_exclusions`).Scan(&n)
	if n != 0 {
		t.Fatal("exclusão ficou depois do desfazer")
	}
}

func TestRenameTopic(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	cyber := fx.ids["Criadores/criador-sintetico/abcd1234efgh.mp4"]
	id, old := topicOf(t, l, cyber, "creator")
	name := "Nome Novo Sintético"
	if err := l.UpdateTopic(ctx, id, TopicInput{Title: &name}); err != nil {
		t.Fatal(err)
	}
	if _, title := topicOf(t, l, cyber, "creator"); title != name {
		t.Fatalf("título %q", title)
	}
	var alias int
	l.r.QueryRow(`SELECT COUNT(*) FROM topic_aliases WHERE topic_id=? AND alias=?`, id, old).Scan(&alias)
	if alias != 1 {
		t.Fatal("nome antigo não virou apelido")
	}
	// scan segue no mesmo tópico; pasta com o nome novo também cai nele
	if err := l.postProcess(ctx, []int64{cyber}); err != nil {
		t.Fatal(err)
	}
	if got, _ := topicOf(t, l, cyber, "creator"); got != id {
		t.Fatal("scan separou o criador renomeado")
	}
	err := l.tx(ctx, func(tx *sql.Tx) error {
		got, err := ensureTopic(tx, "creator", name, "auto")
		if got != id {
			t.Fatalf("pasta com o nome novo foi para %d", got)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// desfazer: volta como estava, sem apelido nem redirecionamento do nome desfeito
	if err := l.UpdateTopic(ctx, id, TopicInput{Title: &old, Revert: true}); err != nil {
		t.Fatal(err)
	}
	var left int
	l.r.QueryRow(`SELECT (SELECT COUNT(*) FROM topic_aliases WHERE topic_id=?) + (SELECT COUNT(*) FROM topic_redirects WHERE topic_id=?)`, id, id).Scan(&left)
	if left != 0 {
		t.Fatalf("sobrou %d apelido/redirecionamento", left)
	}
	if err := l.UpdateTopic(ctx, id, TopicInput{Title: &name}); err != nil {
		t.Fatal(err)
	}
	// nome de outro criador: recusa (o caminho é juntar)
	other, err := l.CreateTopic(ctx, TopicInput{Kind: strp("creator"), Title: strp("Terceiro Sintético")})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.UpdateTopic(ctx, other, TopicInput{Title: &name}); !errors.Is(err, errInvalid) {
		t.Fatalf("esperava recusa, veio %v", err)
	}
}

func strp(s string) *string { return &s }
