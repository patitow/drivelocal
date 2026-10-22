package main

// Limpeza do que sumiu do disco: o arquivo que não existe mais sai do banco
// junto com o que só existia por causa dele (progresso, tags, itens de tópico,
// resultados da IA, sugestões...). Tópico com ficha ou cadastro manual fica;
// tópico automático que ficou sem nenhum item sai.

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// purgeGone apaga do banco os arquivos marcados como sumidos que de fato não
// existem mais. Só mexe numa raiz que está montada e tem arquivos presentes:
// um HD desconectado não leva o histórico junto.
func (l *Library) purgeGone(ctx context.Context) (int, error) {
	rows, err := l.r.QueryContext(ctx, `SELECT m.id, m.path, m.root FROM media m WHERE m.missing = 1
		AND EXISTS (SELECT 1 FROM media o WHERE o.root = m.root AND o.missing = 0)`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	mounted := map[string]bool{}
	for rows.Next() {
		var id int64
		var rel, root string
		if rows.Scan(&id, &rel, &root) != nil {
			continue
		}
		ok, seen := mounted[root]
		if !seen {
			ok = rootMounted(root)
			mounted[root] = ok
		}
		if ok && fileGone(rel) {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, l.dropEmptyAutoTopics(ctx)
	}
	for s := 0; s < len(ids); s += 500 {
		chunk := ids[s:min(s+500, len(ids))]
		ph, args := placeholders(len(chunk)), int64sToAny(chunk)
		err := l.tx(ctx, func(t *sql.Tx) error {
			// sem chave estrangeira: limpa à mão
			for _, q := range []string{
				`DELETE FROM ai_results WHERE media_id IN (` + ph + `)`,
				`DELETE FROM changes WHERE media_id IN (` + ph + `)`,
				`DELETE FROM media_fts WHERE rowid IN (` + ph + `)`,
				`DELETE FROM media WHERE id IN (` + ph + `)`,
			} {
				if _, err := t.Exec(q, args...); err != nil {
					return err
				}
			}
			for _, id := range chunk {
				if _, err := t.Exec(`DELETE FROM entry_meta WHERE ref=?`, "media:"+itoa64(id)); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return s, err
		}
	}
	logInfo("🧹 %d arquivos que não existem mais saíram do banco", len(ids))
	return len(ids), l.dropEmptyAutoTopics(ctx)
}

// dropEmptyAutoTopics: tópico criado pela pasta que ficou sem itens (a pasta
// foi apagada). Os com ficha (capa, sinopse) ficam.
func (l *Library) dropEmptyAutoTopics(ctx context.Context) error {
	res, err := l.w.ExecContext(ctx, `DELETE FROM topics WHERE source = 'auto'
		AND NOT EXISTS (SELECT 1 FROM topic_items ti WHERE ti.topic_id = topics.id)
		AND NOT EXISTS (SELECT 1 FROM entry_meta e WHERE e.ref = 'topic:' || topics.id)`)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		logInfo("🧹 %d tópicos automáticos vazios removidos", n)
	}
	return nil
}

// rootMounted: a pasta da raiz existe e tem alguma coisa dentro.
func rootMounted(root string) bool {
	if root == "" || strings.ContainsAny(root, `/\`) {
		return false
	}
	ents, err := os.ReadDir(filepath.Join(MEDIA_PATH, root))
	return err == nil && len(ents) > 0
}

// fileGone: o arquivo não existe (erro de acesso não conta como sumido).
func fileGone(rel string) bool {
	full := filepath.Clean(filepath.Join(MEDIA_PATH, filepath.FromSlash(rel)))
	if !strings.HasPrefix(full, filepath.Clean(MEDIA_PATH)) {
		return false
	}
	_, err := os.Stat(full)
	return errors.Is(err, fs.ErrNotExist)
}
