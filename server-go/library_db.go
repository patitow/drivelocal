package main

// Biblioteca (organização): banco SQLite (modernc, sem CGO) com catálogo,
// tags, tópicos, favoritos, progresso, playlists, regras e fila de IA.
// Desenho completo em docs/superpowers/specs/2026-10-17-organizacao-biblioteca-design.md.
//
// O banco fica em server/data/library/library.db (volume drivelocal-data,
// fora do git). Um único conn de escrita serializa as gravações; leituras
// usam um pool separado (WAL permite leitura concorrente).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

var LIBRARY_DIR = "server/data/library"

// Níveis de maturidade. A ordem importa: comparações usam <=.
const (
	MaturityGeral     = 0
	MaturitySugestivo = 1
	MaturityExplicito = 2
)

var maturityNames = []string{"geral", "sugestivo", "explicito"}

func maturityName(m int) string {
	if m < 0 || m >= len(maturityNames) {
		return "geral"
	}
	return maturityNames[m]
}

func parseMaturity(s string) (int, bool) {
	for i, n := range maturityNames {
		if n == s {
			return i, true
		}
	}
	return 0, false
}

type Library struct {
	path string
	w    *sql.DB // 1 conexão: escritas serializadas
	r    *sql.DB // leituras

	policyMu sync.RWMutex
	policies []FolderPolicy

	scanMu      sync.Mutex
	scanRunning bool
	lastScan    *ScanReport
}

var (
	libMu sync.RWMutex
	lib   *Library
	// libraryFailed: o banco deveria existir mas não abriu (middleware fail-closed).
	libraryFailed atomic.Bool
)

// getLibrary devolve a biblioteca aberta ou nil (servidor sobe mesmo se o banco falhar).
func getLibrary() *Library {
	libMu.RLock()
	defer libMu.RUnlock()
	return lib
}

func setLibrary(l *Library) {
	libMu.Lock()
	lib = l
	libMu.Unlock()
}

func libraryDSN(path string) string {
	return "file:" + filepath.ToSlash(path) +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
}

// OpenLibrary abre (ou cria) o banco em dir/library.db e aplica as migrações.
func OpenLibrary(dir string) (*Library, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "library.db")
	w, err := sql.Open("sqlite", libraryDSN(path))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	r, err := sql.Open("sqlite", libraryDSN(path))
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(4)
	l := &Library{path: path, w: w, r: r}
	if err := l.migrate(); err != nil {
		l.Close()
		return nil, fmt.Errorf("migração da biblioteca: %w", err)
	}
	if err := l.reloadPolicies(); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

func (l *Library) Close() error {
	var errs []error
	if l.r != nil {
		errs = append(errs, l.r.Close())
	}
	if l.w != nil {
		errs = append(errs, l.w.Close())
	}
	return errors.Join(errs...)
}

// tx executa fn numa transação de escrita.
func (l *Library) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	t, err := l.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(t); err != nil {
		t.Rollback()
		return err
	}
	return t.Commit()
}

func nowUnix() int64 { return time.Now().Unix() }

var libraryMigrations = []string{
	// v1: esquema inicial (fases 1-3)
	`
CREATE TABLE media (
  id INTEGER PRIMARY KEY,
  path TEXT NOT NULL UNIQUE,          -- relativo a MEDIA_PATH, com "/"
  root TEXT NOT NULL,                 -- primeiro segmento do caminho
  kind TEXT NOT NULL,                 -- video | image
  name TEXT NOT NULL,
  title TEXT NOT NULL,
  title_manual TEXT,
  size INTEGER NOT NULL DEFAULT 0,
  mtime INTEGER NOT NULL DEFAULT 0,
  fs_file_id TEXT,
  content_id TEXT,
  duration_s REAL,
  season INTEGER,
  episode REAL,
  category TEXT,
  maturity_policy INTEGER NOT NULL DEFAULT 0,
  maturity_rule INTEGER,
  maturity_manual INTEGER,
  maturity INTEGER NOT NULL DEFAULT 0,  -- efetiva (materializada)
  policy_hidden INTEGER NOT NULL DEFAULT 0,
  missing INTEGER NOT NULL DEFAULT 0,
  first_seen INTEGER NOT NULL,
  last_seen INTEGER NOT NULL
);
CREATE INDEX media_fsid ON media(fs_file_id, size);
CREATE INDEX media_root ON media(root, kind);
CREATE INDEX media_maturity ON media(maturity, policy_hidden, missing);

CREATE TABLE folder_policies (
  prefix TEXT PRIMARY KEY,
  maturity INTEGER,                   -- NULL = herda
  hidden INTEGER NOT NULL DEFAULT 0,  -- oculto para não-admins
  note TEXT,
  updated_at INTEGER NOT NULL
);

CREATE TABLE tags (
  id INTEGER PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  label TEXT NOT NULL,
  facet TEXT NOT NULL DEFAULT 'outro',
  status TEXT NOT NULL DEFAULT 'active',  -- active | proposed
  parent_id INTEGER REFERENCES tags(id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE tag_aliases (
  alias TEXT PRIMARY KEY,
  tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE
);
CREATE TABLE media_tags (
  media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
  tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  source TEXT NOT NULL,               -- manual | rule | ai(aceita)
  created_by TEXT,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (media_id, tag_id)
);
CREATE INDEX media_tags_tag ON media_tags(tag_id);

CREATE TABLE topics (
  id INTEGER PRIMARY KEY,
  kind TEXT NOT NULL,                 -- series | creator | theme | franchise
  slug TEXT NOT NULL,
  title TEXT NOT NULL,
  description TEXT,
  source TEXT NOT NULL DEFAULT 'manual', -- manual | auto | rule | ai
  meta_json TEXT,
  created_at INTEGER NOT NULL,
  UNIQUE(kind, slug)
);
CREATE TABLE topic_aliases (
  topic_id INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  alias TEXT NOT NULL,
  PRIMARY KEY (topic_id, alias)
);
CREATE TABLE topic_items (
  topic_id INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
  sort_key TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT 'auto',
  PRIMARY KEY (topic_id, media_id)
);
CREATE INDEX topic_items_media ON topic_items(media_id);
CREATE TABLE topic_redirects (         -- tópicos fundidos: (kind, slug) antigo -> destino
  kind TEXT NOT NULL,
  slug TEXT NOT NULL,
  topic_id INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  PRIMARY KEY (kind, slug)
);

CREATE TABLE favorites (
  user_id TEXT NOT NULL,
  media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, media_id)
);
CREATE TABLE favorite_topics (
  user_id TEXT NOT NULL,
  topic_id INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, topic_id)
);
CREATE TABLE watch_progress (
  user_id TEXT NOT NULL,
  media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
  position_s REAL NOT NULL DEFAULT 0,
  duration_s REAL NOT NULL DEFAULT 0,
  completed INTEGER NOT NULL DEFAULT 0,
  audio_track TEXT,
  subtitle_track TEXT,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, media_id)
);
CREATE INDEX watch_progress_recent ON watch_progress(user_id, updated_at DESC);
CREATE TABLE user_hidden (
  user_id TEXT NOT NULL,
  media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, media_id)
);
CREATE TABLE user_prefs (
  user_id TEXT PRIMARY KEY,
  max_maturity INTEGER NOT NULL DEFAULT 0,
  auto_resume INTEGER NOT NULL DEFAULT 1,
  autoplay INTEGER NOT NULL DEFAULT 1,
  updated_at INTEGER NOT NULL
);

CREATE TABLE playlists (
  id INTEGER PRIMARY KEY,
  owner_id TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  visibility TEXT NOT NULL DEFAULT 'private', -- private | shared | global
  smart_query_json TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE playlist_items (
  playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
  media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
  pos TEXT NOT NULL,                  -- índice fracionário
  added_at INTEGER NOT NULL,
  PRIMARY KEY (playlist_id, media_id)
);
CREATE INDEX playlist_items_pos ON playlist_items(playlist_id, pos);

CREATE TABLE rules (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  priority INTEGER NOT NULL DEFAULT 100,
  match_json TEXT NOT NULL,
  actions_json TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE changes (
  id INTEGER PRIMARY KEY,
  batch_id TEXT NOT NULL,
  op TEXT NOT NULL,                   -- tag_add | tag_del | topic_add | topic_del | field
  media_id INTEGER,
  ref_id INTEGER,
  field TEXT,
  old_value TEXT,
  new_value TEXT,
  actor TEXT NOT NULL,
  reason TEXT,
  created_at INTEGER NOT NULL,
  undone INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX changes_batch ON changes(batch_id);

CREATE TABLE api_tokens (
  id INTEGER PRIMARY KEY,
  user_id TEXT NOT NULL,
  name TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  prefix TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_used INTEGER,
  revoked INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE ai_jobs (
  id INTEGER PRIMARY KEY,
  media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
  stage TEXT NOT NULL,                -- text | vision
  status TEXT NOT NULL DEFAULT 'queued', -- queued | running | done | error | skipped
  priority INTEGER NOT NULL DEFAULT 100,
  attempts INTEGER NOT NULL DEFAULT 0,
  error TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE(media_id, stage)
);
CREATE INDEX ai_jobs_queue ON ai_jobs(status, priority, id);
CREATE TABLE ai_results (
  id INTEGER PRIMARY KEY,
  job_id INTEGER,
  media_id INTEGER NOT NULL,
  stage TEXT NOT NULL,
  model TEXT NOT NULL,
  prompt_version TEXT NOT NULL,
  output_json TEXT,
  ok INTEGER NOT NULL,
  refused INTEGER NOT NULL DEFAULT 0,
  latency_ms INTEGER NOT NULL,
  error TEXT,
  created_at INTEGER NOT NULL
);
CREATE TABLE suggestions (
  id INTEGER PRIMARY KEY,
  media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,                 -- tag | topic | maturity | title | category
  value TEXT NOT NULL,                -- JSON
  confidence REAL,
  source TEXT NOT NULL,               -- ai | user
  model TEXT,
  status TEXT NOT NULL DEFAULT 'pending', -- pending | accepted | rejected
  created_by TEXT,
  created_at INTEGER NOT NULL,
  reviewed_by TEXT,
  reviewed_at INTEGER
);
CREATE INDEX suggestions_pending ON suggestions(status, kind);
CREATE UNIQUE INDEX suggestions_dedupe ON suggestions(media_id, kind, value, source);

CREATE TABLE golden_items (
  media_id INTEGER PRIMARY KEY REFERENCES media(id) ON DELETE CASCADE,
  stratum TEXT NOT NULL,
  labels_json TEXT,                   -- rótulos do usuário (NULL = ainda não rotulado)
  labeled_by TEXT,
  labeled_at INTEGER,
  created_at INTEGER NOT NULL
);

CREATE VIRTUAL TABLE media_fts USING fts5(
  title, path_tokens, topics, tags,
  tokenize = 'unicode61 remove_diacritics 2',
  prefix = '2 3'
);
`,
	// v2: "continuar assistindo" dispensável, sugestões de fusão de tópicos
	// duplicados e o histórico dessas fusões (para desfazer).
	`
CREATE TABLE continue_dismissed (       -- hidden_from_continue: volta com progresso novo
  user_id TEXT NOT NULL,
  media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
  dismissed_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, media_id)
);
CREATE TABLE topic_merge_suggestions (
  id INTEGER PRIMARY KEY,
  src_id INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  dst_id INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  score REAL NOT NULL,
  method TEXT NOT NULL,               -- string | embedding | combo
  status TEXT NOT NULL DEFAULT 'pending', -- pending | accepted | rejected
  created_at INTEGER NOT NULL,
  reviewed_by TEXT,
  reviewed_at INTEGER,
  UNIQUE (src_id, dst_id)
);
CREATE INDEX topic_merge_suggestions_status ON topic_merge_suggestions(status);
CREATE TABLE topic_merges (
  id INTEGER PRIMARY KEY,
  batch_id TEXT NOT NULL,
  src_id INTEGER NOT NULL,            -- id original (o tópico some na fusão)
  dst_id INTEGER NOT NULL,
  snapshot TEXT NOT NULL,             -- JSON com o necessário para desfazer
  actor TEXT,
  created_at INTEGER NOT NULL,
  undone INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX topic_merges_batch ON topic_merges(batch_id);
`,
	// v3: arquivos duplicados (mesmo conteúdo) apontam para a cópia canônica;
	// só a canônica aparece nas listagens, os arquivos seguem no disco.
	`
ALTER TABLE media ADD COLUMN dup_of INTEGER REFERENCES media(id) ON DELETE SET NULL;
CREATE INDEX media_dup_of ON media(dup_of);
`,
	// v4: arquivo que o ffprobe não abre (corrompido/incompleto), progresso de
	// leitura de mangá por usuário, última visita à estante de criadores e
	// metadados externos (AniList/TMDB) em cache.
	`
ALTER TABLE media ADD COLUMN broken TEXT;
ALTER TABLE user_prefs ADD COLUMN creators_seen_at INTEGER NOT NULL DEFAULT 0;
CREATE TABLE manga_progress (
  user_id TEXT NOT NULL,
  work TEXT NOT NULL,                 -- pasta da obra
  chapter TEXT NOT NULL,              -- pasta do capítulo atual
  page INTEGER NOT NULL,
  pages INTEGER NOT NULL,
  done TEXT NOT NULL DEFAULT '[]',    -- JSON: capítulos lidos
  updated_at INTEGER NOT NULL,        -- ms (o cliente ordena "lendo agora" por isso)
  PRIMARY KEY (user_id, work)
);
CREATE TABLE ext_meta (
  key TEXT PRIMARY KEY,               -- fonte|tipo|título normalizado
  data TEXT,                          -- JSON; NULL = buscado e não encontrado
  fetched_at INTEGER NOT NULL
);
`,
	// v5: tópicos extintos (não voltam no scan; a foto permite desfazer) e
	// sugestões de quem não é admin (juntar / não é um criador válido).
	`
ALTER TABLE topic_merge_suggestions ADD COLUMN created_by TEXT;
CREATE TABLE topic_blocks (
  kind TEXT NOT NULL,
  slug TEXT NOT NULL,
  title TEXT NOT NULL,
  snapshot TEXT NOT NULL,             -- JSON (topicSnapshot); '' = nome fundido no bloqueado
  actor TEXT,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (kind, slug)
);
CREATE TABLE topic_flags (
  id INTEGER PRIMARY KEY,
  topic_id INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  created_by TEXT,
  status TEXT NOT NULL DEFAULT 'pending', -- pending | accepted | rejected
  created_at INTEGER NOT NULL,
  reviewed_by TEXT,
  reviewed_at INTEGER,
  UNIQUE (topic_id, created_by)
);
CREATE INDEX topic_flags_status ON topic_flags(status);
`,
	// v6: item tirado de um tópico à mão (mover para outro criador): o scan e
	// as regras não o colocam de volta.
	`
CREATE TABLE topic_item_exclusions (
  topic_id INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (topic_id, media_id)
);
`,
	// v7: modo restrito (o próprio usuário liga/desliga dentro do limite que o admin
	// deu) e ficha curada das entradas (série, criador, filme, mangá).
	`
ALTER TABLE user_prefs ADD COLUMN restricted_mode INTEGER NOT NULL DEFAULT 1;
CREATE TABLE entry_meta (
  ref TEXT PRIMARY KEY, -- topic:<id> | media:<id> | manga:<pasta>
  data TEXT NOT NULL,
  updated_by TEXT,
  updated_at INTEGER NOT NULL
);
`,
}

func (l *Library) migrate() error {
	ctx := context.Background()
	if _, err := l.w.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var v int
	err := l.w.QueryRowContext(ctx, `SELECT version FROM schema_version`).Scan(&v)
	fresh := false
	if errors.Is(err, sql.ErrNoRows) {
		fresh = true
		if _, err := l.w.ExecContext(ctx, `INSERT INTO schema_version(version) VALUES (0)`); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	for i := v; i < len(libraryMigrations); i++ {
		err := l.tx(ctx, func(t *sql.Tx) error {
			if _, err := t.Exec(libraryMigrations[i]); err != nil {
				return err
			}
			_, err := t.Exec(`UPDATE schema_version SET version = ?`, i+1)
			return err
		})
		if err != nil {
			return fmt.Errorf("v%d: %w", i+1, err)
		}
	}
	if fresh {
		return l.seedDefaults(ctx)
	}
	return nil
}

// defaultFolderPolicies: decisão D4 do usuário. Só pastas explícitas têm
// padrão; o resto de Animes fica livre (marcado item a item quando preciso).
var defaultFolderPolicies = []FolderPolicy{
	{Prefix: "Animes/Restrito", Maturity: intPtr(MaturityExplicito)},
	{Prefix: "Gravacoes/Privado", Maturity: intPtr(MaturityExplicito)},
	{Prefix: "Criadores", Maturity: intPtr(MaturityExplicito)},
}

func intPtr(v int) *int { return &v }

// seedDefaults roda só na criação do banco: políticas D4 e preferências dos
// usuários que já existem (mantêm o acesso total que tinham antes).
// Usuários criados depois começam em "geral".
func (l *Library) seedDefaults(ctx context.Context) error {
	return l.tx(ctx, func(t *sql.Tx) error {
		now := nowUnix()
		for _, p := range defaultFolderPolicies {
			if _, err := t.Exec(`INSERT OR IGNORE INTO folder_policies(prefix, maturity, hidden, note, updated_at) VALUES (?,?,?,?,?)`,
				p.Prefix, p.Maturity, boolInt(p.Hidden), "padrão D4", now); err != nil {
				return err
			}
		}
		usersMutex.RLock()
		defer usersMutex.RUnlock()
		for _, u := range users {
			if _, err := t.Exec(`INSERT OR IGNORE INTO user_prefs(user_id, max_maturity, updated_at) VALUES (?,?,?)`,
				u.ID, MaturityExplicito, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// initLibrary abre o banco no diretório padrão e agenda o primeiro scan.
// Falhas só desativam a biblioteca; o navegador de pastas continua.
func initLibrary() {
	l, err := OpenLibrary(LIBRARY_DIR)
	if err != nil {
		// Fail-closed: sem o banco não há como aplicar maturidade/ocultação,
		// então não-admin perde acesso às rotas de mídia até o banco abrir.
		libraryFailed.Store(true)
		logError("Biblioteca indisponível (%v): rotas de mídia negadas a não-admin até o banco abrir; nova tentativa a cada minuto", err)
		go func() {
			for {
				time.Sleep(time.Minute)
				if l, err := OpenLibrary(LIBRARY_DIR); err == nil {
					startLibrary(l)
					return
				}
			}
		}()
		return
	}
	startLibrary(l)
}

func startLibrary(l *Library) {
	setLibrary(l)
	libraryFailed.Store(false)
	logInfo("📚 Biblioteca aberta em %s", l.path)
	go l.backupLoop()
	go l.scanLoop()
	go func() {
		time.Sleep(20 * time.Second)
		if _, err := l.purgeGone(context.Background()); err != nil {
			logWarn("Limpeza de arquivos sumidos: %v", err)
		}
		l.backfillAutoTopics(context.Background())
		l.localizeArtBacklog(context.Background())
	}()
	go startAIScheduler(l)
}

// backupLoop grava uma cópia diária com VACUUM INTO (mantém as 7 últimas).
func (l *Library) backupLoop() {
	time.Sleep(2 * time.Minute)
	for {
		if err := l.Backup(); err != nil {
			logWarn("Backup da biblioteca falhou: %v", err)
		}
		time.Sleep(24 * time.Hour)
	}
}

func (l *Library) Backup() error {
	dir := filepath.Join(filepath.Dir(l.path), "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := filepath.Join(dir, "library-"+time.Now().Format("20060102")+".db")
	os.Remove(name)
	if _, err := l.w.Exec(`VACUUM INTO ?`, name); err != nil {
		return err
	}
	entries, _ := filepath.Glob(filepath.Join(dir, "library-*.db"))
	if len(entries) > 7 {
		for _, old := range entries[:len(entries)-7] {
			os.Remove(old)
		}
	}
	return nil
}
