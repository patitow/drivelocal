package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // AI_TZ funciona na imagem alpine sem tzdata
)

// Fase 3: classificação por IA local via agent-hub (API compatível com OpenAI).
// Tudo que a IA produz vira sugestão pendente; nada é aplicado sem revisão.

const aiPromptVersion = "v1"

type AIConfig struct {
	BaseURL     string `json:"baseUrl"`
	APIKey      string `json:"-"`
	TextModel   string `json:"textModel"`
	VisionModel string `json:"visionModel"`
	Window      string `json:"window"`
	TZ          string `json:"tz"`
	Enabled     bool   `json:"enabled"`
	winStart    int    // minutos desde 00:00
	winEnd      int
	loc         *time.Location
}

func loadAIConfig() AIConfig {
	c := AIConfig{
		BaseURL:     strings.TrimRight(getEnv("AI_BASE_URL", ""), "/"),
		APIKey:      os.Getenv("AI_API_KEY"),
		TextModel:   getEnv("AI_TEXT_MODEL", "gemma4-12b"),
		VisionModel: getEnv("AI_VISION_MODEL", "gemma4-12b-vision"),
		Window:      getEnv("AI_WINDOW", "01:00-08:00"),
		TZ:          getEnv("AI_TZ", "America/Sao_Paulo"),
	}
	c.Enabled = c.BaseURL != "" && getEnv("AI_ENABLED", "true") != "false"
	c.winStart, c.winEnd = parseWindow(c.Window)
	if loc, err := time.LoadLocation(c.TZ); err == nil {
		c.loc = loc
	} else {
		c.loc = time.Local
	}
	return c
}

// parseWindow lê "HH:MM-HH:MM"; janela inválida vira 01:00-08:00.
func parseWindow(s string) (int, int) {
	parse := func(p string) (int, bool) {
		hm := strings.SplitN(strings.TrimSpace(p), ":", 2)
		if len(hm) != 2 {
			return 0, false
		}
		h, e1 := strconv.Atoi(hm[0])
		m, e2 := strconv.Atoi(hm[1])
		if e1 != nil || e2 != nil || h < 0 || h > 24 || m < 0 || m > 59 {
			return 0, false
		}
		return h*60 + m, true
	}
	parts := strings.SplitN(s, "-", 2)
	if len(parts) == 2 {
		a, ok1 := parse(parts[0])
		b, ok2 := parse(parts[1])
		if ok1 && ok2 && a != b {
			return a, b
		}
	}
	return 60, 480
}

// inWindow aceita janelas que cruzam a meia-noite (ex.: 22:00-06:00).
func inWindow(t time.Time, start, end int) bool {
	m := t.Hour()*60 + t.Minute()
	if start < end {
		return m >= start && m < end
	}
	return m >= start || m < end
}

type aiState struct {
	mu       sync.Mutex
	cfg      AIConfig
	paused   bool
	running  bool
	current  string
	lastErr  string
	runLimit int // > 0: "rodar agora" fora da janela, até N jobs
	client   *http.Client
	wake     chan struct{}
}

var aiS = &aiState{wake: make(chan struct{}, 1)}

func startAIScheduler(l *Library) {
	aiS.mu.Lock()
	aiS.cfg = loadAIConfig()
	aiS.client = &http.Client{Timeout: 5 * time.Minute}
	cfg := aiS.cfg
	aiS.mu.Unlock()
	if !cfg.Enabled {
		logInfo("IA da biblioteca desativada (AI_BASE_URL vazio)")
		return
	}
	logInfo("IA da biblioteca: %s, janela %s (%s)", cfg.BaseURL, cfg.Window, cfg.TZ)
	// Jobs que ficaram "running" após um reinício voltam para a fila.
	l.w.Exec(`UPDATE ai_jobs SET status='queued' WHERE status='running'`)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		l.aiTick()
		select {
		case <-ticker.C:
		case <-aiS.wake:
		}
	}
}

func (l *Library) aiTick() {
	for {
		aiS.mu.Lock()
		cfg := aiS.cfg
		allowed := !aiS.paused && (aiS.runLimit > 0 || inWindow(time.Now().In(cfg.loc), cfg.winStart, cfg.winEnd))
		if !allowed || aiS.running {
			aiS.mu.Unlock()
			return
		}
		aiS.running = true
		aiS.mu.Unlock()

		did, err := l.runNextAIJob(context.Background(), cfg)

		aiS.mu.Lock()
		aiS.running = false
		aiS.current = ""
		if err != nil {
			aiS.lastErr = err.Error()
		}
		if did && aiS.runLimit > 0 {
			aiS.runLimit--
		}
		if !did {
			aiS.runLimit = 0
		}
		aiS.mu.Unlock()
		if !did {
			return
		}
		if err != nil {
			// Hub fora do ar: espera o próximo tique em vez de girar.
			return
		}
	}
}

func aiWake() {
	select {
	case aiS.wake <- struct{}{}:
	default:
	}
}

type AIStatus struct {
	Config    AIConfig       `json:"config"`
	Paused    bool           `json:"paused"`
	Running   bool           `json:"running"`
	Current   string         `json:"current,omitempty"`
	InWindow  bool           `json:"inWindow"`
	RunLimit  int            `json:"runLimit"`
	LastError string         `json:"lastError,omitempty"`
	Queue     map[string]int `json:"queue"`
	Metrics   []AIMetric     `json:"metrics"`
	Pending   int            `json:"pendingSuggestions"`
}

type AIMetric struct {
	Stage      string  `json:"stage"`
	Model      string  `json:"model"`
	Total      int     `json:"total"`
	OK         int     `json:"ok"`
	Refused    int     `json:"refused"`
	AvgMs      float64 `json:"avgMs"`
	P50Ms      int64   `json:"p50Ms"`
	P95Ms      int64   `json:"p95Ms"`
	ValidRate  float64 `json:"validRate"`
	RefuseRate float64 `json:"refuseRate"`
}

func (l *Library) AIStatus(ctx context.Context) AIStatus {
	aiS.mu.Lock()
	st := AIStatus{Config: aiS.cfg, Paused: aiS.paused, Running: aiS.running, Current: aiS.current, RunLimit: aiS.runLimit, LastError: aiS.lastErr}
	aiS.mu.Unlock()
	if st.Config.loc != nil {
		st.InWindow = inWindow(time.Now().In(st.Config.loc), st.Config.winStart, st.Config.winEnd)
	}
	st.Queue = map[string]int{}
	rows, err := l.r.QueryContext(ctx, `SELECT status, COUNT(*) FROM ai_jobs GROUP BY status`)
	if err == nil {
		for rows.Next() {
			var s string
			var n int
			rows.Scan(&s, &n)
			st.Queue[s] = n
		}
		rows.Close()
	}
	st.Metrics = l.aiMetrics(ctx)
	l.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM suggestions WHERE status='pending'`).Scan(&st.Pending)
	return st
}

func (l *Library) aiMetrics(ctx context.Context) []AIMetric {
	out := []AIMetric{}
	rows, err := l.r.QueryContext(ctx, `SELECT stage, model, ok, refused, latency_ms FROM ai_results ORDER BY stage, model`)
	if err != nil {
		return out
	}
	defer rows.Close()
	type acc struct {
		m   AIMetric
		lat []int64
	}
	byKey := map[string]*acc{}
	var keys []string
	for rows.Next() {
		var stage, model string
		var ok, refused int
		var lat int64
		if rows.Scan(&stage, &model, &ok, &refused, &lat) != nil {
			continue
		}
		k := stage + "|" + model
		a := byKey[k]
		if a == nil {
			a = &acc{m: AIMetric{Stage: stage, Model: model}}
			byKey[k] = a
			keys = append(keys, k)
		}
		a.m.Total++
		a.m.OK += ok
		a.m.Refused += refused
		a.lat = append(a.lat, lat)
	}
	for _, k := range keys {
		a := byKey[k]
		sort.Slice(a.lat, func(i, j int) bool { return a.lat[i] < a.lat[j] })
		var sum int64
		for _, v := range a.lat {
			sum += v
		}
		n := len(a.lat)
		a.m.AvgMs = float64(sum) / float64(n)
		a.m.P50Ms = a.lat[n/2]
		a.m.P95Ms = a.lat[min(n-1, n*95/100)]
		a.m.ValidRate = float64(a.m.OK) / float64(n)
		a.m.RefuseRate = float64(a.m.Refused) / float64(n)
		out = append(out, a.m)
	}
	return out
}

func AISetPaused(p bool) {
	aiS.mu.Lock()
	aiS.paused = p
	aiS.mu.Unlock()
	aiWake()
}

// AIRunNow processa até n jobs da fila agora, mesmo fora da janela.
func AIRunNow(n int) error {
	aiS.mu.Lock()
	enabled := aiS.cfg.Enabled
	if enabled {
		aiS.runLimit = n
		aiS.paused = false
	}
	aiS.mu.Unlock()
	if !enabled {
		return errors.New("IA desativada: defina AI_BASE_URL")
	}
	aiWake()
	return nil
}

// aiPriority: decisão D10 — começa pelo conteúdo mais fácil.
func aiPriority(rel string) int {
	switch {
	case pathHasPrefix(rel, "Filmes"):
		return 0
	case pathHasPrefix(rel, "Animes"):
		return 1
	case pathHasPrefix(rel, "Torrents/.Animes"):
		return 2
	case pathHasPrefix(rel, ".VIDEOS GRAVADOS/OBS"):
		return 3
	case pathHasPrefix(rel, "Torrents"):
		return 4
	case pathHasPrefix(rel, "Gravacoes/Privado"):
		return 8
	case pathHasPrefix(rel, "Criadores"):
		return 9
	}
	return 5
}

type EnqueueRequest struct {
	IDs        []int64 `json:"ids"`
	PathPrefix string  `json:"pathPrefix"`
	Stage      string  `json:"stage"` // text | vision
	Limit      int     `json:"limit"`
	Requeue    bool    `json:"requeue"` // refaz itens já processados
	OnlyNoAI   bool    `json:"onlyNew"`
}

// Enqueue põe itens na fila (nunca em massa automática: o admin escolhe).
func (l *Library) AIEnqueue(ctx context.Context, req EnqueueRequest) (int, error) {
	stage := req.Stage
	if stage == "" {
		stage = "text"
	}
	if stage != "text" && stage != "vision" {
		return 0, errInvalid
	}
	limit := clampLimit(req.Limit, 100, 20000)
	var ids []int64
	if len(req.IDs) > 0 {
		ids = req.IDs
	} else {
		p := normalizePolicyPrefix(req.PathPrefix)
		q := `SELECT id, path FROM media WHERE missing = 0 AND kind = 'video'`
		var args []any
		if p != "" {
			q += ` AND (path = ? OR path LIKE ? ESCAPE '\')`
			args = append(args, p, likePrefix(p)+"/%")
		}
		q += ` AND NOT EXISTS (SELECT 1 FROM ai_jobs j WHERE j.media_id = media.id AND j.stage = ?)`
		args = append(args, stage)
		rows, err := l.r.QueryContext(ctx, q+` ORDER BY path`, args...)
		if err != nil {
			return 0, err
		}
		type cand struct {
			id  int64
			pri int
		}
		var cs []cand
		for rows.Next() {
			var id int64
			var rel string
			rows.Scan(&id, &rel)
			cs = append(cs, cand{id, aiPriority(rel)})
		}
		rows.Close()
		sort.SliceStable(cs, func(i, j int) bool { return cs[i].pri < cs[j].pri })
		for _, c := range cs {
			if len(ids) >= limit {
				break
			}
			ids = append(ids, c.id)
		}
	}
	n := 0
	err := l.tx(ctx, func(t *sql.Tx) error {
		now := nowUnix()
		for _, id := range ids {
			var rel string
			if t.QueryRow(`SELECT path FROM media WHERE id=?`, id).Scan(&rel) != nil {
				continue
			}
			conflict := `DO NOTHING`
			if req.Requeue {
				conflict = `DO UPDATE SET status='queued', attempts=0, error=NULL, priority=excluded.priority, updated_at=excluded.updated_at`
			}
			res, err := t.Exec(`INSERT INTO ai_jobs(media_id, stage, status, priority, created_at, updated_at) VALUES (?,?,'queued',?,?,?)
				ON CONFLICT(media_id, stage) `+conflict, id, stage, aiPriority(rel), now, now)
			if err != nil {
				return err
			}
			if k, _ := res.RowsAffected(); k > 0 {
				n++
			}
		}
		return nil
	})
	if err == nil && n > 0 {
		aiWake()
	}
	return n, err
}

func (l *Library) AIClearQueue(ctx context.Context) (int64, error) {
	var n int64
	err := l.tx(ctx, func(t *sql.Tx) error {
		res, err := t.Exec(`DELETE FROM ai_jobs WHERE status IN ('queued','error')`)
		if err == nil {
			n, _ = res.RowsAffected()
		}
		return err
	})
	return n, err
}

// ---------- execução ----------

type aiJob struct {
	id, mediaID int64
	stage       string
	attempts    int
	path, name  string
	durationS   sql.NullFloat64
}

func (l *Library) runNextAIJob(ctx context.Context, cfg AIConfig) (bool, error) {
	var j aiJob
	err := l.w.QueryRowContext(ctx, `SELECT j.id, j.media_id, j.stage, j.attempts, m.path, m.name, m.duration_s
		FROM ai_jobs j JOIN media m ON m.id = j.media_id
		WHERE j.status = 'queued' AND m.missing = 0 ORDER BY j.priority, j.id LIMIT 1`).
		Scan(&j.id, &j.mediaID, &j.stage, &j.attempts, &j.path, &j.name, &j.durationS)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	l.w.ExecContext(ctx, `UPDATE ai_jobs SET status='running', attempts=attempts+1, updated_at=? WHERE id=?`, nowUnix(), j.id)
	aiS.mu.Lock()
	aiS.current = j.stage + ": " + j.path
	aiS.mu.Unlock()

	model := cfg.TextModel
	if j.stage == "vision" {
		model = cfg.VisionModel
	}
	start := time.Now()
	out, refused, callErr := l.classify(ctx, cfg, j)
	lat := time.Since(start).Milliseconds()
	ok := callErr == nil && out != nil
	var outJSON any
	if out != nil {
		b, _ := json.Marshal(out)
		outJSON = string(b)
	}
	var errText any
	if callErr != nil {
		errText = callErr.Error()
	}
	l.w.ExecContext(ctx, `INSERT INTO ai_results(job_id, media_id, stage, model, prompt_version, output_json, ok, refused, latency_ms, error, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		j.id, j.mediaID, j.stage, model, aiPromptVersion, outJSON, boolInt(ok), boolInt(refused), lat, errText, nowUnix())

	if callErr != nil {
		var hubDown *hubUnavailableError
		if errors.As(callErr, &hubDown) {
			// Não conta como tentativa: o hub está fora; volta para a fila.
			l.w.ExecContext(ctx, `UPDATE ai_jobs SET status='queued', attempts=attempts-1, error=?, updated_at=? WHERE id=?`, callErr.Error(), nowUnix(), j.id)
			return true, callErr
		}
		status := "queued"
		if j.attempts+1 >= 3 {
			status = "error"
		}
		l.w.ExecContext(ctx, `UPDATE ai_jobs SET status=?, error=?, updated_at=? WHERE id=?`, status, callErr.Error(), nowUnix(), j.id)
		return true, nil
	}
	if refused {
		l.w.ExecContext(ctx, `UPDATE ai_jobs SET status='done', error='recusa do modelo', updated_at=? WHERE id=?`, nowUnix(), j.id)
		return true, nil
	}
	if err := l.storeSuggestions(ctx, j.mediaID, model, out); err != nil {
		l.w.ExecContext(ctx, `UPDATE ai_jobs SET status='error', error=?, updated_at=? WHERE id=?`, err.Error(), nowUnix(), j.id)
		return true, nil
	}
	l.w.ExecContext(ctx, `UPDATE ai_jobs SET status='done', error=NULL, updated_at=? WHERE id=?`, nowUnix(), j.id)
	return true, nil
}

// AIOutput é o formato pedido ao modelo (json_schema).
type AIOutput struct {
	Title       string   `json:"title"`
	SeriesTitle string   `json:"series_title"`
	Season      int      `json:"season"`
	Episode     float64  `json:"episode"`
	Creator     string   `json:"creator"`
	Category    string   `json:"category"`
	Maturity    string   `json:"maturity"`
	Tags        []string `json:"tags"`
	Confidence  float64  `json:"confidence"`
	Refused     bool     `json:"refused"`
}

var aiCategories = []string{"filme", "episodio", "anime", "musica", "gravacao", "clipe", "tutorial", "jogo", "restrito", "outro"}

func aiSchema() map[string]any {
	str := map[string]any{"type": "string"}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":        str,
			"series_title": str,
			"season":       map[string]any{"type": "integer"},
			"episode":      map[string]any{"type": "number"},
			"creator":      str,
			"category":     map[string]any{"type": "string", "enum": aiCategories},
			"maturity":     map[string]any{"type": "string", "enum": maturityNames},
			"tags":         map[string]any{"type": "array", "items": str, "maxItems": 8},
			"confidence":   map[string]any{"type": "number"},
			"refused":      map[string]any{"type": "boolean"},
		},
		"required":             []string{"title", "series_title", "season", "episode", "creator", "category", "maturity", "tags", "confidence", "refused"},
		"additionalProperties": false,
	}
}

const aiSystemPrompt = `Você cataloga uma biblioteca de mídia pessoal. Recebe o caminho de um arquivo (e às vezes um quadro do vídeo) e devolve SOMENTE JSON no esquema pedido.
Regras:
- title: título limpo e legível (sem extensão, grupo de release, resolução, hash).
- series_title: nome da série/anime se for episódio, senão "". season/episode: 0 se não houver.
- creator: canal/criador/estúdio só se estiver claro no caminho, senão "".
- category: uma das opções.
- maturity: "geral" (livre), "sugestivo" (violência forte, insinuação), "explicito" (conteúdo explícito, só para quem liberou). Na dúvida entre dois níveis, escolha o MAIS ALTO.
- tags: até 8 tags curtas em português, minúsculas; prefira as da lista de vocabulário quando couber.
- confidence: 0 a 1.
- refused: true apenas se não puder responder; nunca escreva texto fora do JSON.`

type hubUnavailableError struct{ err error }

func (e *hubUnavailableError) Error() string { return "hub indisponível: " + e.err.Error() }

func (l *Library) classify(ctx context.Context, cfg AIConfig, j aiJob) (*AIOutput, bool, error) {
	vocab := l.tagVocabulary(ctx, 150)
	folder := path.Dir(j.path)
	user := fmt.Sprintf("Caminho: %s\nPasta: %s\nArquivo: %s\n", j.path, folder, j.name)
	if j.durationS.Valid && j.durationS.Float64 > 0 {
		user += fmt.Sprintf("Duração: %.0f min\n", j.durationS.Float64/60)
	}
	if len(vocab) > 0 {
		user += "Vocabulário de tags: " + strings.Join(vocab, ", ") + "\n"
	}
	content := []map[string]any{{"type": "text", "text": user}}
	model := cfg.TextModel
	if j.stage == "vision" {
		model = cfg.VisionModel
		img, err := extractFrameJPEG(ctx, j.path, j.durationS)
		if err != nil {
			return nil, false, fmt.Errorf("quadro: %w", err)
		}
		content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(img)}})
	}
	body := map[string]any{
		"model":       model,
		"temperature": 0,
		"max_tokens":  400,
		"messages": []map[string]any{
			{"role": "system", "content": aiSystemPrompt},
			{"role": "user", "content": content},
		},
		"response_format": map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "catalogo", "strict": true, "schema": aiSchema()},
		},
	}
	raw, err := hubChat(ctx, cfg, body)
	if err != nil {
		return nil, false, err
	}
	return parseAIOutput(raw)
}

// parseAIOutput valida a resposta; texto fora do JSON ou recusa explícita contam como recusa/erro.
func parseAIOutput(raw string) (*AIOutput, bool, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	var out AIOutput
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		low := strings.ToLower(raw)
		for _, w := range []string{"i can't", "i cannot", "não posso", "nao posso", "i'm sorry", "desculpe"} {
			if strings.Contains(low, w) {
				return nil, true, nil
			}
		}
		return nil, false, fmt.Errorf("JSON inválido: %w", err)
	}
	if out.Refused {
		return &out, true, nil
	}
	if _, ok := parseMaturity(out.Maturity); !ok || out.Maturity == "" {
		return nil, false, fmt.Errorf("maturidade inválida: %q", out.Maturity)
	}
	return &out, false, nil
}

func hubChat(ctx context.Context, cfg AIConfig, body map[string]any) (string, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	aiS.mu.Lock()
	client := aiS.client
	aiS.mu.Unlock()
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", &hubUnavailableError{err}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusUnauthorized {
		return "", &hubUnavailableError{fmt.Errorf("HTTP %d", resp.StatusCode)}
	}
	if resp.StatusCode != http.StatusOK {
		msg := string(data)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}
	var cr struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &cr); err != nil || len(cr.Choices) == 0 {
		return "", errors.New("resposta do hub sem choices")
	}
	if cr.Choices[0].Message.Refusal != "" {
		return `{"refused":true}`, nil
	}
	return cr.Choices[0].Message.Content, nil
}

func (l *Library) tagVocabulary(ctx context.Context, n int) []string {
	rows, err := l.r.QueryContext(ctx, `SELECT tg.label FROM tags tg WHERE tg.status='active'
		ORDER BY (SELECT COUNT(*) FROM media_tags mt WHERE mt.tag_id = tg.id) DESC, tg.label LIMIT ?`, n)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

// extractFrameJPEG tira um quadro a ~25% da duração, reduzido para 512px.
func extractFrameJPEG(ctx context.Context, rel string, dur sql.NullFloat64) ([]byte, error) {
	full, ok := resolveMediaPath(rel)
	if !ok {
		return nil, errNotFound
	}
	at := 60.0
	if dur.Valid && dur.Float64 > 0 {
		at = dur.Float64 * 0.25
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-ss", strconv.FormatFloat(at, 'f', 1, 64), "-i", full,
		"-frames:v", "1", "-vf", "scale=512:512:force_original_aspect_ratio=decrease",
		"-q:v", "4", "-f", "image2", "-c:v", "mjpeg", "pipe:1")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	if out.Len() == 0 {
		return nil, errors.New("ffmpeg não gerou quadro")
	}
	return out.Bytes(), nil
}

// ---------- sugestões e revisão ----------

func (l *Library) storeSuggestions(ctx context.Context, mediaID int64, model string, out *AIOutput) error {
	type sug struct {
		kind string
		val  map[string]any
	}
	var list []sug
	if out.Maturity != "" {
		list = append(list, sug{"maturity", map[string]any{"value": out.Maturity}})
	}
	if t := strings.TrimSpace(out.Title); t != "" {
		list = append(list, sug{"title", map[string]any{"value": t}})
	}
	if c := strings.TrimSpace(out.Category); c != "" && c != "outro" {
		list = append(list, sug{"category", map[string]any{"value": c}})
	}
	if s := strings.TrimSpace(out.SeriesTitle); s != "" {
		list = append(list, sug{"topic", map[string]any{"kind": "series", "title": s}})
	}
	if s := strings.TrimSpace(out.Creator); s != "" {
		list = append(list, sug{"topic", map[string]any{"kind": "creator", "title": s}})
	}
	seen := map[string]bool{}
	for _, t := range out.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if slugify(t) == "" || seen[slugify(t)] {
			continue
		}
		seen[slugify(t)] = true
		list = append(list, sug{"tag", map[string]any{"label": t}})
	}
	return l.tx(ctx, func(t *sql.Tx) error {
		for _, s := range list {
			b, _ := json.Marshal(s.val)
			if _, err := t.Exec(`INSERT OR IGNORE INTO suggestions(media_id, kind, value, confidence, source, model, status, created_at) VALUES (?,?,?,?,'ai',?,'pending',?)`,
				mediaID, s.kind, string(b), out.Confidence, model, nowUnix()); err != nil {
				return err
			}
		}
		return nil
	})
}

type Suggestion struct {
	ID         int64           `json:"id"`
	MediaID    int64           `json:"mediaId"`
	Path       string          `json:"path"`
	Title      string          `json:"title"`
	Maturity   string          `json:"maturity"`
	Kind       string          `json:"kind"`
	Value      json.RawMessage `json:"value"`
	Confidence *float64        `json:"confidence,omitempty"`
	Source     string          `json:"source"`
	Model      string          `json:"model,omitempty"`
	Status     string          `json:"status"`
	CreatedAt  int64           `json:"createdAt"`
}

type ReviewQuery struct {
	Status     string
	Kind       string
	PathPrefix string
	MinConf    float64
	Limit      int
	Offset     int
}

func (l *Library) ListSuggestions(ctx context.Context, q ReviewQuery) ([]Suggestion, int, error) {
	if q.Status == "" {
		q.Status = "pending"
	}
	conds := []string{"s.status = ?"}
	args := []any{q.Status}
	if q.Kind != "" {
		conds = append(conds, "s.kind = ?")
		args = append(args, q.Kind)
	}
	if p := normalizePolicyPrefix(q.PathPrefix); p != "" {
		conds = append(conds, `(m.path = ? OR m.path LIKE ? ESCAPE '\')`)
		args = append(args, p, likePrefix(p)+"/%")
	}
	if q.MinConf > 0 {
		conds = append(conds, "COALESCE(s.confidence, 0) >= ?")
		args = append(args, q.MinConf)
	}
	where := " WHERE " + strings.Join(conds, " AND ")
	var total int
	if err := l.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM suggestions s JOIN media m ON m.id = s.media_id`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := clampLimit(q.Limit, 100, 1000)
	rows, err := l.r.QueryContext(ctx, `SELECT s.id, s.media_id, m.path, COALESCE(m.title_manual, m.title), m.maturity, s.kind, s.value, s.confidence, s.source, COALESCE(s.model,''), s.status, s.created_at
		FROM suggestions s JOIN media m ON m.id = s.media_id`+where+` ORDER BY m.path, s.kind, s.id LIMIT ? OFFSET ?`, append(args, limit, max(q.Offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Suggestion{}
	for rows.Next() {
		var s Suggestion
		var mat int
		var val string
		var conf sql.NullFloat64
		if err := rows.Scan(&s.ID, &s.MediaID, &s.Path, &s.Title, &mat, &s.Kind, &val, &conf, &s.Source, &s.Model, &s.Status, &s.CreatedAt); err != nil {
			return nil, 0, err
		}
		s.Maturity = maturityName(mat)
		s.Value = json.RawMessage(val)
		if conf.Valid {
			s.Confidence = &conf.Float64
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

// suggestionActions traduz uma sugestão em Actions.
func suggestionActions(kind, value string) (Actions, error) {
	var v struct {
		Value string `json:"value"`
		Label string `json:"label"`
		Facet string `json:"facet"`
		Kind  string `json:"kind"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(value), &v); err != nil {
		return Actions{}, err
	}
	var a Actions
	switch kind {
	case "tag":
		lbl := v.Label
		if v.Facet != "" {
			lbl = v.Facet + ":" + v.Label
		}
		a.AddTags = []string{lbl}
	case "topic":
		a.Topic = &TopicRef{Kind: v.Kind, Title: v.Title}
	case "maturity":
		a.Maturity = &v.Value
	case "title":
		a.Title = &v.Value
	case "category":
		a.Category = &v.Value
	default:
		return a, errInvalid
	}
	return a, nil
}

type ReviewResult struct {
	Accepted int    `json:"accepted"`
	Rejected int    `json:"rejected"`
	BatchID  string `json:"batchId,omitempty"`
	Errors   int    `json:"errors"`
}

// ReviewSuggestions aceita ou rejeita sugestões; os aceites viram um lote desfazível.
func (l *Library) ReviewSuggestions(ctx context.Context, ids []int64, accept bool, actor string) (*ReviewResult, error) {
	rr := &ReviewResult{}
	if len(ids) == 0 {
		return rr, nil
	}
	type row struct {
		id, mediaID int64
		kind, value string
	}
	var list []row
	for s := 0; s < len(ids); s += 500 {
		e := min(s+500, len(ids))
		rows, err := l.r.QueryContext(ctx, `SELECT id, media_id, kind, value FROM suggestions WHERE status='pending' AND id IN (`+placeholders(e-s)+`)`, int64sToAny(ids[s:e])...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r row
			rows.Scan(&r.id, &r.mediaID, &r.kind, &r.value)
			list = append(list, r)
		}
		rows.Close()
	}
	if !accept {
		err := l.tx(ctx, func(t *sql.Tx) error {
			for _, r := range list {
				if _, err := t.Exec(`UPDATE suggestions SET status='rejected', reviewed_by=?, reviewed_at=? WHERE id=?`, actor, nowUnix(), r.id); err != nil {
					return err
				}
				rr.Rejected++
			}
			return nil
		})
		return rr, err
	}
	res := &ApplyResult{BatchID: newBatchID(), ByOp: map[string]int{}}
	err := l.withApplier(ctx, res, "ai", actor, "sugestão aceita", false, func(a *applier) error {
		for _, r := range list {
			acts, err := suggestionActions(r.kind, r.value)
			if err != nil {
				rr.Errors++
				continue
			}
			if err := a.apply([]int64{r.mediaID}, acts); err != nil {
				return err
			}
			if _, err := a.t.Exec(`UPDATE suggestions SET status='accepted', reviewed_by=?, reviewed_at=? WHERE id=?`, actor, nowUnix(), r.id); err != nil {
				return err
			}
			// Aceitar uma maturidade descarta as outras pendentes do mesmo item.
			if r.kind == "maturity" || r.kind == "title" || r.kind == "category" {
				a.t.Exec(`UPDATE suggestions SET status='rejected', reviewed_by=?, reviewed_at=? WHERE media_id=? AND kind=? AND status='pending' AND id<>?`, actor, nowUnix(), r.mediaID, r.kind, r.id)
			}
			rr.Accepted++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	rr.BatchID = res.BatchID
	return rr, nil
}

// ---------- conjunto de teste (golden) ----------

type GoldenItem struct {
	MediaID  int64           `json:"mediaId"`
	Path     string          `json:"path"`
	Stratum  string          `json:"stratum"`
	Labels   json.RawMessage `json:"labels,omitempty"`
	Labeled  bool            `json:"labeled"`
	Maturity string          `json:"maturity"`
}

// GoldenLabels: o que o usuário marca para cada item do conjunto de teste.
type GoldenLabels struct {
	Maturity string   `json:"maturity"`
	Tags     []string `json:"tags,omitempty"`
	Series   string   `json:"series,omitempty"`
	Title    string   `json:"title,omitempty"`
}

// GoldenSample sorteia ~n itens estratificados por raiz (proporcional, com mínimo por estrato).
func (l *Library) GoldenSample(ctx context.Context, n int) (int, error) {
	n = clampLimit(n, 200, 2000)
	rows, err := l.r.QueryContext(ctx, `SELECT root, COUNT(*) FROM media WHERE missing=0 AND kind='video' GROUP BY root`)
	if err != nil {
		return 0, err
	}
	type st struct {
		root  string
		count int
	}
	var strata []st
	total := 0
	for rows.Next() {
		var s st
		rows.Scan(&s.root, &s.count)
		strata = append(strata, s)
		total += s.count
	}
	rows.Close()
	if total == 0 {
		return 0, nil
	}
	added := 0
	err = l.tx(ctx, func(t *sql.Tx) error {
		for _, s := range strata {
			k := n * s.count / total
			if k < 5 {
				k = min(5, s.count)
			}
			res, err := t.Exec(`INSERT OR IGNORE INTO golden_items(media_id, stratum, created_at)
				SELECT id, root, ? FROM media WHERE missing=0 AND kind='video' AND root=? AND id NOT IN (SELECT media_id FROM golden_items)
				ORDER BY random() LIMIT ?`, nowUnix(), s.root, k)
			if err != nil {
				return err
			}
			c, _ := res.RowsAffected()
			added += int(c)
		}
		return nil
	})
	if err == nil {
		l.exportGolden(ctx)
	}
	return added, err
}

func (l *Library) ListGolden(ctx context.Context) ([]GoldenItem, error) {
	rows, err := l.r.QueryContext(ctx, `SELECT g.media_id, m.path, g.stratum, g.labels_json, m.maturity FROM golden_items g JOIN media m ON m.id = g.media_id ORDER BY g.stratum, m.path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GoldenItem{}
	for rows.Next() {
		var g GoldenItem
		var lab sql.NullString
		var mat int
		if err := rows.Scan(&g.MediaID, &g.Path, &g.Stratum, &lab, &mat); err != nil {
			return nil, err
		}
		if lab.Valid {
			g.Labels = json.RawMessage(lab.String)
			g.Labeled = true
		}
		g.Maturity = maturityName(mat)
		out = append(out, g)
	}
	return out, rows.Err()
}

func (l *Library) LabelGolden(ctx context.Context, mediaID int64, lab GoldenLabels, actor string) error {
	if _, ok := parseMaturity(lab.Maturity); !ok || lab.Maturity == "" {
		return errInvalid
	}
	b, _ := json.Marshal(lab)
	err := l.tx(ctx, func(t *sql.Tx) error {
		res, err := t.Exec(`UPDATE golden_items SET labels_json=?, labeled_by=?, labeled_at=? WHERE media_id=?`, string(b), actor, nowUnix(), mediaID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNotFound
		}
		return nil
	})
	if err == nil {
		l.exportGolden(ctx)
	}
	return err
}

// exportGolden grava golden.jsonl ao lado do banco (fora do git; D7).
func (l *Library) exportGolden(ctx context.Context) {
	items, err := l.ListGolden(ctx)
	if err != nil {
		return
	}
	var buf bytes.Buffer
	for _, g := range items {
		b, _ := json.Marshal(g)
		buf.Write(b)
		buf.WriteByte('\n')
	}
	dir := path.Dir(strings.ReplaceAll(l.path, `\`, "/"))
	os.WriteFile(dir+"/golden.jsonl", buf.Bytes(), 0o600)
}

type EvalReport struct {
	Labeled          int                `json:"labeled"`
	WithAI           int                `json:"withAI"`
	MaturityAccuracy float64            `json:"maturityAccuracy"`
	ExplicitRecall   float64            `json:"explicitRecall"` // dos explícitos rotulados, quantos a IA marcou explícito
	UnderRate        float64            `json:"underRate"`      // IA abaixo do rótulo (o erro perigoso)
	TagPrecision     float64            `json:"tagPrecision"`
	TagRecall        float64            `json:"tagRecall"`
	SeriesAccuracy   float64            `json:"seriesAccuracy"`
	Confusion        map[string]int     `json:"confusion"` // "rotulo->ia"
	ByStratum        map[string]float64 `json:"byStratum"` // acurácia de maturidade
}

// Eval compara as sugestões de IA mais recentes com os rótulos do conjunto de teste.
func (l *Library) Eval(ctx context.Context) (*EvalReport, error) {
	items, err := l.ListGolden(ctx)
	if err != nil {
		return nil, err
	}
	rep := &EvalReport{Confusion: map[string]int{}, ByStratum: map[string]float64{}}
	var matOK, explicitTotal, explicitHit, under, tagTP, tagFP, tagFN, seriesTotal, seriesOK int
	strTot, strOK := map[string]int{}, map[string]int{}
	for _, g := range items {
		if !g.Labeled {
			continue
		}
		rep.Labeled++
		var lab GoldenLabels
		json.Unmarshal(g.Labels, &lab)
		sugs := map[string][]string{}
		rows, err := l.r.QueryContext(ctx, `SELECT kind, value FROM suggestions WHERE media_id=? AND source='ai'`, g.MediaID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var k, v string
			rows.Scan(&k, &v)
			sugs[k] = append(sugs[k], v)
		}
		rows.Close()
		if len(sugs["maturity"]) == 0 {
			continue
		}
		rep.WithAI++
		var mv struct {
			Value string `json:"value"`
		}
		json.Unmarshal([]byte(sugs["maturity"][len(sugs["maturity"])-1]), &mv)
		lm, _ := parseMaturity(lab.Maturity)
		am, _ := parseMaturity(mv.Value)
		rep.Confusion[lab.Maturity+"->"+mv.Value]++
		strTot[g.Stratum]++
		if lm == am {
			matOK++
			strOK[g.Stratum]++
		}
		if am < lm {
			under++
		}
		if lm == MaturityExplicito {
			explicitTotal++
			if am == MaturityExplicito {
				explicitHit++
			}
		}
		want := map[string]bool{}
		for _, t := range lab.Tags {
			want[slugify(t)] = true
		}
		got := map[string]bool{}
		for _, v := range sugs["tag"] {
			var tv struct {
				Label string `json:"label"`
			}
			json.Unmarshal([]byte(v), &tv)
			got[slugify(tv.Label)] = true
		}
		for t := range got {
			if want[t] {
				tagTP++
			} else {
				tagFP++
			}
		}
		for t := range want {
			if !got[t] {
				tagFN++
			}
		}
		if lab.Series != "" {
			seriesTotal++
			for _, v := range sugs["topic"] {
				var tv struct {
					Kind, Title string
				}
				json.Unmarshal([]byte(v), &tv)
				if tv.Kind == "series" && slugify(tv.Title) == slugify(lab.Series) {
					seriesOK++
					break
				}
			}
		}
	}
	ratio := func(a, b int) float64 {
		if b == 0 {
			return 0
		}
		return float64(a) / float64(b)
	}
	rep.MaturityAccuracy = ratio(matOK, rep.WithAI)
	rep.ExplicitRecall = ratio(explicitHit, explicitTotal)
	rep.UnderRate = ratio(under, rep.WithAI)
	rep.TagPrecision = ratio(tagTP, tagTP+tagFP)
	rep.TagRecall = ratio(tagTP, tagTP+tagFN)
	rep.SeriesAccuracy = ratio(seriesOK, seriesTotal)
	for s, n := range strTot {
		rep.ByStratum[s] = ratio(strOK[s], n)
	}
	return rep, nil
}
