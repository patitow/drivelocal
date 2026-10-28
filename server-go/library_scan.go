package main

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	libraryVideoExts = map[string]bool{".mp4": true, ".avi": true, ".mov": true, ".wmv": true, ".flv": true, ".webm": true, ".mkv": true, ".m4v": true, ".ts": true}
	libraryImageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true, ".webp": true, ".tiff": true}
)

func mediaKindOf(name string) string {
	ext := strings.ToLower(path.Ext(name))
	switch {
	case libraryVideoExts[ext]:
		return "video"
	case libraryImageExts[ext]:
		return "image"
	}
	return ""
}

type ScanReport struct {
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	Seen       int       `json:"seen"`
	Added      int       `json:"added"`
	Updated    int       `json:"updated"`
	Moved      int       `json:"moved"`
	Missing    int       `json:"missing"`
	Restored   int       `json:"restored"`
	Errors     int       `json:"errors"`
	Error      string    `json:"error,omitempty"`
}

type scanFile struct {
	rel   string
	kind  string
	size  int64
	mtime int64
	fsid  string
}

type existingMedia struct {
	id      int64
	size    int64
	mtime   int64
	fsid    string
	missing bool
	seen    bool
}

var errScanRunning = errors.New("scan já em andamento")

func (l *Library) scanLoop() {
	time.Sleep(45 * time.Second)
	for {
		l.scanAndProbe()
		time.Sleep(6 * time.Hour)
	}
}

// scanAndProbe: scan completo e, em seguida, ffprobe nos vídeos novos ou
// alterados (duração e marcação de arquivos que não abrem).
func (l *Library) scanAndProbe() {
	if _, err := l.Scan(context.Background(), MEDIA_PATH); err != nil {
		if !errors.Is(err, errScanRunning) {
			logWarn("Scan da biblioteca falhou: %v", err)
		}
		return
	}
	l.FillDurations(context.Background())
	if n := l.countBroken(); n > 0 {
		logWarn("📚 %d vídeo(s) não abrem no ffprobe (corrompidos ou incompletos)", n)
	}
}

func (l *Library) countBroken() int {
	var n int
	l.r.QueryRow(`SELECT COUNT(*) FROM media WHERE broken IS NOT NULL AND missing = 0`).Scan(&n)
	return n
}

func (l *Library) ScanStatus() (bool, *ScanReport) {
	l.scanMu.Lock()
	defer l.scanMu.Unlock()
	return l.scanRunning, l.lastScan
}

// Scan percorre root e sincroniza o catálogo. Identidade: caminho; se o
// caminho sumiu, (fs_file_id, tamanho) detecta movimentos e renomeações.
func (l *Library) Scan(ctx context.Context, root string) (*ScanReport, error) {
	l.scanMu.Lock()
	if l.scanRunning {
		l.scanMu.Unlock()
		return nil, errScanRunning
	}
	l.scanRunning = true
	l.scanMu.Unlock()
	rep := &ScanReport{StartedAt: time.Now()}
	defer func() {
		rep.FinishedAt = time.Now()
		l.scanMu.Lock()
		l.scanRunning = false
		l.lastScan = rep
		l.scanMu.Unlock()
	}()
	err := l.scan(ctx, root, rep)
	if err != nil {
		rep.Error = err.Error()
	} else {
		logInfo("📚 Scan: %d vistos, +%d novos, %d atualizados, %d movidos, %d sumidos (%s)",
			rep.Seen, rep.Added, rep.Updated, rep.Moved, rep.Missing, time.Since(rep.StartedAt).Round(time.Second))
		if n, rerr := l.refreshParsedEpisodes(ctx); rerr != nil {
			logWarn("Temporadas e episódios: %v", rerr)
		} else if n > 0 {
			logInfo("📚 %d itens com temporada/episódio recalculados", n)
		}
		// Tópicos novos podem ser grafias de tópicos existentes: só sugere
		// (a fusão é sempre revisada).
		if d, derr := l.DetectTopicDuplicates(ctx); derr != nil {
			logWarn("Detecção de tópicos duplicados: %v", derr)
		} else if d.Suggested > 0 {
			logInfo("📚 %d possíveis tópicos duplicados para revisar", d.Suggested)
		}
		// o que sumiu do disco sai do banco (raiz desmontada não conta)
		if _, perr := l.purgeGone(ctx); perr != nil {
			logWarn("Limpeza de arquivos sumidos: %v", perr)
		}
		l.invalidateHome("")
	}
	return rep, err
}

func (l *Library) scan(ctx context.Context, root string, rep *ScanReport) error {
	existing := map[string]*existingMedia{}
	byFsid := map[string][]string{}
	rows, err := l.r.QueryContext(ctx, `SELECT id, path, size, mtime, COALESCE(fs_file_id,''), missing FROM media`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p string
		e := &existingMedia{}
		var missing int
		if err := rows.Scan(&e.id, &p, &e.size, &e.mtime, &e.fsid, &missing); err != nil {
			rows.Close()
			return err
		}
		e.missing = missing == 1
		existing[p] = e
		if e.fsid != "" {
			k := e.fsid + "|" + itoa64(e.size)
			byFsid[k] = append(byFsid[k], p)
		}
	}
	rows.Close()

	var fresh []scanFile
	var changed []scanFile
	rootClean := filepath.Clean(root)
	walkErr := filepath.WalkDir(rootClean, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			rep.Errors++
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		kind := mediaKindOf(d.Name())
		if kind == "" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			rep.Errors++
			return nil
		}
		rel, err := filepath.Rel(rootClean, p)
		if err != nil {
			return nil
		}
		f := scanFile{rel: filepath.ToSlash(rel), kind: kind, size: info.Size(), mtime: info.ModTime().Unix(), fsid: fsFileID(info)}
		rep.Seen++
		if e, ok := existing[f.rel]; ok {
			e.seen = true
			if e.size != f.size || e.mtime != f.mtime || e.missing || (e.fsid == "" && f.fsid != "") {
				changed = append(changed, f)
			}
			return nil
		}
		fresh = append(fresh, f)
		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	now := nowUnix()
	var touched []int64
	err = l.tx(ctx, func(t *sql.Tx) error {
		upd, err := t.Prepare(`UPDATE media SET size=?, mtime=?, fs_file_id=COALESCE(NULLIF(?,''), fs_file_id), missing=0, last_seen=?, broken=NULL, duration_s=NULL WHERE id=?`)
		if err != nil {
			return err
		}
		defer upd.Close()
		for _, f := range changed {
			e := existing[f.rel]
			if e.missing {
				rep.Restored++
			} else {
				rep.Updated++
			}
			if _, err := upd.Exec(f.size, f.mtime, f.fsid, now, e.id); err != nil {
				return err
			}
			touched = append(touched, e.id)
		}
		move, err := t.Prepare(`UPDATE media SET path=?, root=?, name=?, size=?, mtime=?, missing=0, last_seen=? WHERE id=?`)
		if err != nil {
			return err
		}
		defer move.Close()
		ins, err := t.Prepare(`INSERT INTO media(path, root, kind, name, title, size, mtime, fs_file_id, season, episode, first_seen, last_seen)
			VALUES (?,?,?,?,?,?,?,NULLIF(?,''),NULLIF(?,0),NULLIF(?,0),?,?)`)
		if err != nil {
			return err
		}
		defer ins.Close()
		for _, f := range fresh {
			// Movimento: mesmo (fs_file_id, tamanho) de um item cujo caminho antigo sumiu.
			if f.fsid != "" {
				moved := false
				for _, old := range byFsid[f.fsid+"|"+itoa64(f.size)] {
					e := existing[old]
					if e == nil || e.seen {
						continue
					}
					e.seen = true
					if _, err := move.Exec(f.rel, rootSegment(f.rel), path.Base(f.rel), f.size, f.mtime, now, e.id); err != nil {
						return err
					}
					touched = append(touched, e.id)
					rep.Moved++
					moved = true
					break
				}
				if moved {
					continue
				}
			}
			pn := ParseMediaName(path.Base(f.rel))
			title := pn.Title
			if isOpaqueName(path.Base(f.rel)) || title == "" {
				title = path.Base(path.Dir(f.rel)) + " · " + path.Base(f.rel)
			}
			res, err := ins.Exec(f.rel, rootSegment(f.rel), f.kind, path.Base(f.rel), title, f.size, f.mtime, f.fsid, pn.Season, pn.Episode, now, now)
			if err != nil {
				return err
			}
			id, _ := res.LastInsertId()
			touched = append(touched, id)
			rep.Added++
		}
		var gone []int64
		for _, e := range existing {
			if !e.seen && !e.missing {
				gone = append(gone, e.id)
			}
		}
		for _, id := range gone {
			if _, err := t.Exec(`UPDATE media SET missing=1 WHERE id=?`, id); err != nil {
				return err
			}
		}
		rep.Missing = len(gone)
		return nil
	})
	if err != nil {
		return err
	}
	if len(touched) == 0 {
		return nil
	}
	return l.postProcess(ctx, touched)
}

// backfillAutoTopics põe nos tópicos automáticos os arquivos que ficaram sem
// nenhum (regra de pasta nova, como Series/): o scan só reprocessa o que mudou.
// Item tirado à mão e tópico excluído não voltam (exclusões e bloqueios).
func (l *Library) backfillAutoTopics(ctx context.Context) {
	rows, err := l.r.QueryContext(ctx, `SELECT id, path FROM media m WHERE m.missing = 0
		AND NOT EXISTS (SELECT 1 FROM topic_items ti WHERE ti.media_id = m.id)`)
	if err != nil {
		logWarn("tópicos automáticos: %v", err)
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		var p string
		if rows.Scan(&id, &p) == nil {
			if _, ok := deriveTopic(p); ok {
				ids = append(ids, id)
			}
		}
	}
	rows.Close()
	if len(ids) == 0 {
		return
	}
	if err := l.postProcess(ctx, ids); err != nil {
		logWarn("tópicos automáticos: %v", err)
		return
	}
	logInfo("📚 %d arquivos entraram em tópicos automáticos", len(ids))
}

// postProcess aplica política de pasta, tópico automático e índice de busca.
func (l *Library) postProcess(ctx context.Context, ids []int64) error {
	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		err := l.tx(ctx, func(t *sql.Tx) error {
			topicCache := map[string]int64{}
			for _, id := range chunk {
				var rel string
				if err := t.QueryRow(`SELECT path FROM media WHERE id=?`, id).Scan(&rel); err != nil {
					return err
				}
				m, h := l.policyFor(rel)
				if _, err := t.Exec(`UPDATE media SET maturity_policy=?, policy_hidden=? WHERE id=?`, m, boolInt(h), id); err != nil {
					return err
				}
				if dt, ok := deriveTopic(rel); ok {
					key := dt.Kind + "|" + dt.Title
					tid, ok := topicCache[key]
					if !ok {
						var err error
						tid, err = ensureTopic(t, dt.Kind, dt.Title, "auto")
						if err != nil && !errors.Is(err, errTopicBlocked) {
							return err
						}
						topicCache[key] = tid
					}
					if tid == 0 {
						continue
					}
					// item tirado deste tópico à mão não volta
					if _, err := t.Exec(`INSERT INTO topic_items(topic_id, media_id, sort_key, source) SELECT ?,?,?,'auto'
						WHERE NOT EXISTS (SELECT 1 FROM topic_item_exclusions WHERE topic_id=? AND media_id=?)
						ON CONFLICT(topic_id, media_id) DO UPDATE SET sort_key=excluded.sort_key`, tid, id, dt.SortKey, tid, id); err != nil {
						return err
					}
				}
			}
			if err := recomputeMaturity(t, "id IN ("+placeholders(len(chunk))+")", int64sToAny(chunk)...); err != nil {
				return err
			}
			return refreshFTS(t, chunk)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// ensureTopic acha ou cria o tópico (kind, slug(title)).
func ensureTopic(t *sql.Tx, kind, title, source string) (int64, error) {
	slug := slugify(title)
	if slug == "" {
		slug = "sem-nome"
	}
	// Tópicos fundidos redirecionam para o destino.
	var id sql.NullInt64
	err := t.QueryRow(`SELECT COALESCE((SELECT topic_id FROM topic_redirects WHERE kind=? AND slug=?), (SELECT id FROM topics WHERE kind=? AND slug=?))`, kind, slug, kind, slug).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if id.Valid && id.Int64 > 0 {
		return id.Int64, nil
	}
	// Nome que é alias de um tópico existente (ex.: grafia de um tópico
	// fundido) vai para o canônico.
	if aid, err := resolveTopicAlias(t, kind, slug); err == nil {
		return aid, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	// Extinto ("não é um criador"): scan, regras e IA não recriam; criar à mão
	// desfaz o bloqueio.
	if source == "manual" {
		if _, err := t.Exec(`DELETE FROM topic_blocks WHERE kind=? AND slug=?`, kind, slug); err != nil {
			return 0, err
		}
	} else if topicBlocked(t, kind, slug) {
		return 0, errTopicBlocked
	}
	res, err := t.Exec(`INSERT INTO topics(kind, slug, title, source, created_at) VALUES (?,?,?,?,?)`, kind, slug, title, source, nowUnix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// refreshFTS reescreve as linhas do índice de busca para os ids dados.
func refreshFTS(t *sql.Tx, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	ph := placeholders(len(ids))
	args := int64sToAny(ids)
	if _, err := t.Exec(`DELETE FROM media_fts WHERE rowid IN (`+ph+`)`, args...); err != nil {
		return err
	}
	rows, err := t.Query(`SELECT m.id, COALESCE(m.title_manual, m.title), m.path,
		COALESCE((SELECT group_concat(tp.title || ' ' || COALESCE((SELECT group_concat(alias, ' ') FROM topic_aliases ta WHERE ta.topic_id = tp.id), ''), ' ')
		          FROM topic_items ti JOIN topics tp ON tp.id = ti.topic_id WHERE ti.media_id = m.id), ''),
		COALESCE((SELECT group_concat(tg.label || ' ' || tg.slug, ' ') FROM media_tags mt JOIN tags tg ON tg.id = mt.tag_id WHERE mt.media_id = m.id), '')
		FROM media m WHERE m.id IN (`+ph+`)`, args...)
	if err != nil {
		return err
	}
	type ftsRow struct {
		id                        int64
		title, path, topics, tags string
	}
	var list []ftsRow
	for rows.Next() {
		var r ftsRow
		if err := rows.Scan(&r.id, &r.title, &r.path, &r.topics, &r.tags); err != nil {
			rows.Close()
			return err
		}
		list = append(list, r)
	}
	rows.Close()
	for _, r := range list {
		if _, err := t.Exec(`INSERT INTO media_fts(rowid, title, path_tokens, topics, tags) VALUES (?,?,?,?,?)`,
			r.id, r.title, pathTokens(r.path), r.topics, r.tags); err != nil {
			return err
		}
	}
	return nil
}

func rootSegment(rel string) string {
	return strings.SplitN(rel, "/", 2)[0]
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

func int64sToAny(ids []int64) []any {
	out := make([]any, len(ids))
	for i, v := range ids {
		out[i] = v
	}
	return out
}

func itoa64(v int64) string {
	return strconv.FormatInt(v, 10)
}
