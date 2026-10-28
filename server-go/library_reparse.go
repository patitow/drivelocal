package main

import (
	"context"
	"database/sql"
	"path"
)

// refreshParsedEpisodes recalcula temporada e episódio de todo o catálogo com
// o leitor de nomes atual. O scan só lê o nome de arquivos novos; sem isto,
// uma correção no leitor nunca chegaria aos itens já catalogados. Vídeo com
// episódio e sem temporada fica na 1, como faz o preenchimento de durações.
func (l *Library) refreshParsedEpisodes(ctx context.Context) (int, error) {
	rows, err := l.r.QueryContext(ctx, `SELECT id, name, kind, season, episode FROM media WHERE missing = 0`)
	if err != nil {
		return 0, err
	}
	type fix struct {
		id      int64
		season  int
		episode float64
	}
	var fixes []fix
	for rows.Next() {
		var id int64
		var name, kind string
		var season sql.NullInt64
		var ep sql.NullFloat64
		if err := rows.Scan(&id, &name, &kind, &season, &ep); err != nil {
			rows.Close()
			return 0, err
		}
		pn := ParseMediaName(path.Base(name))
		if kind == "video" && pn.Season == 0 && pn.Episode > 0 {
			pn.Season = 1
		}
		if int(season.Int64) != pn.Season || ep.Float64 != pn.Episode {
			fixes = append(fixes, fix{id, pn.Season, pn.Episode})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(fixes) == 0 {
		return 0, nil
	}
	err = l.tx(ctx, func(t *sql.Tx) error {
		st, err := t.Prepare(`UPDATE media SET season = NULLIF(?,0), episode = NULLIF(?,0) WHERE id = ?`)
		if err != nil {
			return err
		}
		defer st.Close()
		for _, f := range fixes {
			if _, err := st.Exec(f.season, f.episode, f.id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(fixes), nil
}
