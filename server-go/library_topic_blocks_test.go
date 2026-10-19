package main

// Extinguir um criador: some, não volta no scan, desfazer restaura. Nomes sintéticos.

import (
	"context"
	"testing"
)

func TestDissolveTopicBlocksScanAndRestores(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	cyber := fx.ids["Criadores/criador-sintetico/abcd1234efgh.mp4"]
	id, _ := topicOf(t, l, cyber, "creator")
	l.w.Exec(`INSERT INTO favorite_topics(user_id, topic_id, created_at) VALUES ('u1', ?, 1)`, id)

	b, err := l.DissolveTopic(ctx, id, "admin")
	if err != nil || b.Count == 0 {
		t.Fatalf("dissolve: %+v %v", b, err)
	}
	countTopics := func() int {
		var n int
		l.r.QueryRow(`SELECT COUNT(*) FROM topic_items ti JOIN topics tp ON tp.id = ti.topic_id WHERE ti.media_id = ? AND tp.kind = 'creator'`, cyber).Scan(&n)
		return n
	}
	if countTopics() != 0 {
		t.Fatal("o item ainda está num criador")
	}
	// o pós-processamento do scan não recria
	if err := l.postProcess(ctx, []int64{cyber}); err != nil {
		t.Fatal(err)
	}
	if countTopics() != 0 {
		t.Fatal("o scan recriou o criador extinto")
	}
	list, _ := l.ListTopicBlocks(ctx)
	if len(list) != 1 {
		t.Fatalf("blocks: %+v", list)
	}

	nid, err := l.RestoreTopic(ctx, b.Kind, b.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := topicOf(t, l, cyber, "creator"); got != nid {
		t.Fatalf("restaurado em %d, item em %d", nid, got)
	}
	var fav int
	l.r.QueryRow(`SELECT COUNT(*) FROM favorite_topics WHERE topic_id=?`, nid).Scan(&fav)
	if fav != 1 {
		t.Fatal("favorito não voltou")
	}
	if list, _ := l.ListTopicBlocks(ctx); len(list) != 0 {
		t.Fatalf("bloqueio ficou: %+v", list)
	}
}

func TestSuggestTopic(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	id, _ := topicOf(t, l, fx.ids["Criadores/criador-sintetico/abcd1234efgh.mp4"], "creator")
	user := l.viewerFor("u-old")
	if err := l.SuggestTopic(ctx, user, id, "u1", "invalid", 0); err != nil {
		t.Fatal(err)
	}
	if err := l.SuggestTopic(ctx, user, id, "u1", "invalid", 0); err != nil {
		t.Fatal("repetir a sugestão não pode falhar:", err)
	}
	flags, _ := l.TopicFlags(ctx)
	if len(flags) != 1 || flags[0].TopicID != id {
		t.Fatalf("flags: %+v", flags)
	}
	if err := l.SuggestTopic(ctx, user, id, "u1", "merge", id); err == nil {
		t.Fatal("juntar consigo mesmo deveria falhar")
	}
	if err := l.DismissTopicFlags(ctx, id, "admin"); err != nil {
		t.Fatal(err)
	}
	if flags, _ := l.TopicFlags(ctx); len(flags) != 0 {
		t.Fatalf("flags depois de manter: %+v", flags)
	}
	// rejeitada não volta pela mão do usuário; quem não vê o tópico recebe 404
	if err := l.SuggestTopic(ctx, user, id, "u1", "invalid", 0); err != nil {
		t.Fatal(err)
	}
	if flags, _ := l.TopicFlags(ctx); len(flags) != 0 {
		t.Fatalf("usuário reabriu sugestão rejeitada: %+v", flags)
	}
	if err := l.SuggestTopic(ctx, l.viewerFor("u-new"), id, "u2", "invalid", 0); err != errNotFound {
		t.Fatalf("tópico invisível: %v", err)
	}
}
