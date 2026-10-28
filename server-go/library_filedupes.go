package main

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"
)

// Arquivos duplicados: mesmo tamanho e mesmo hash de início+fim (1 MiB de
// cada ponta). Uma cópia fica canônica; as outras recebem dup_of e somem das
// listagens (os arquivos continuam no disco e acessíveis pelo caminho).

const dupHashChunk = 1 << 20

var copyMarker = regexp.MustCompile(`(?i)(\(\d+\)|\bcopy\b|\bcópia\b|_ ?filester|filester\.me|^download\b)`)

type FileDupMember struct {
	ID        int64  `json:"id"`
	Path      string `json:"path"`
	Canonical bool   `json:"canonical,omitempty"`
}

type FileDupGroup struct {
	Size    int64           `json:"size"`
	Members []FileDupMember `json:"members"`
}

type FileDupReport struct {
	Running   bool           `json:"running"`
	Applied   bool           `json:"applied"`
	Done      int            `json:"done"`
	Total     int            `json:"total"`
	Groups    int            `json:"groups"`
	Extra     int            `json:"extra"`
	WastedMB  int64          `json:"wastedMB"`
	Errors    int            `json:"errors"`
	StartedAt int64          `json:"startedAt,omitempty"`
	Sample    []FileDupGroup `json:"sample,omitempty"`
}

var fileDupState struct {
	sync.Mutex
	rep FileDupReport
}

func hashHeadTail(full string, size int64) (string, error) {
	f, err := os.Open(full)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	fmt.Fprintf(h, "%d", size)
	if _, err := io.CopyN(h, f, minInt64(size, dupHashChunk)); err != nil && err != io.EOF {
		return "", err
	}
	if size > 2*dupHashChunk {
		if _, err := f.Seek(size-dupHashChunk, io.SeekStart); err != nil {
			return "", err
		}
		if _, err := io.CopyN(h, f, dupHashChunk); err != nil && err != io.EOF {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

type dupCand struct {
	id         int64
	path, name string
	size       int64
	hasUser    bool
	topics     int
}

// pickCanonical prefere a cópia com dados do usuário (progresso, favorito,
// playlist), depois a que está em tópico, a de nome sem marcador de cópia e
// o caminho mais curto.
func pickCanonical(g []dupCand) int {
	best := 0
	score := func(c dupCand) (int, int, int, int, int64) {
		u, t, m := 0, 0, 0
		if c.hasUser {
			u = 1
		}
		if c.topics > 0 {
			t = 1
		}
		if !copyMarker.MatchString(c.name) {
			m = 1
		}
		return u, t, m, -len(c.path), -c.id
	}
	for i := 1; i < len(g); i++ {
		a1, a2, a3, a4, a5 := score(g[i])
		b1, b2, b3, b4, b5 := score(g[best])
		switch {
		case a1 != b1:
			if a1 > b1 {
				best = i
			}
		case a2 != b2:
			if a2 > b2 {
				best = i
			}
		case a3 != b3:
			if a3 > b3 {
				best = i
			}
		case a4 != b4:
			if a4 > b4 {
				best = i
			}
		case a5 > b5:
			best = i
		}
	}
	return best
}

// ScanFileDupes calcula os grupos; com apply grava content_id/dup_of.
func (l *Library) ScanFileDupes(ctx context.Context, apply bool) {
	fileDupState.Lock()
	if fileDupState.rep.Running {
		fileDupState.Unlock()
		return
	}
	fileDupState.rep = FileDupReport{Running: true, Applied: apply, StartedAt: time.Now().Unix()}
	fileDupState.Unlock()
	set := func(f func(r *FileDupReport)) {
		fileDupState.Lock()
		f(&fileDupState.rep)
		fileDupState.Unlock()
	}
	finish := func() { set(func(r *FileDupReport) { r.Running = false }) }
	defer finish()

	rows, err := l.r.QueryContext(ctx, `
		SELECT m.id, m.path, m.name, m.size,
		  EXISTS(SELECT 1 FROM watch_progress w WHERE w.media_id=m.id)
		  OR EXISTS(SELECT 1 FROM favorites f WHERE f.media_id=m.id)
		  OR EXISTS(SELECT 1 FROM playlist_items p WHERE p.media_id=m.id),
		  (SELECT COUNT(*) FROM topic_items ti WHERE ti.media_id=m.id)
		FROM media m WHERE m.missing=0 AND m.size > ?`, dupHashChunk)
	if err != nil {
		set(func(r *FileDupReport) { r.Errors++ })
		return
	}
	bySize := map[int64][]dupCand{}
	for rows.Next() {
		var c dupCand
		var u int
		if err := rows.Scan(&c.id, &c.path, &c.name, &c.size, &u, &c.topics); err == nil {
			c.hasUser = u == 1
			bySize[c.size] = append(bySize[c.size], c)
		}
	}
	rows.Close()
	total := 0
	for s, v := range bySize {
		if len(v) < 2 {
			delete(bySize, s)
			continue
		}
		total += len(v)
	}
	set(func(r *FileDupReport) { r.Total = total })

	root := filepath.Clean(MEDIA_PATH)
	var groups [][]dupCand
	hashes := map[int64]string{}
	for _, v := range bySize {
		by := map[string][]dupCand{}
		for _, c := range v {
			if ctx.Err() != nil {
				return
			}
			h, err := hashHeadTail(filepath.Join(root, filepath.FromSlash(c.path)), c.size)
			set(func(r *FileDupReport) { r.Done++ })
			if err != nil {
				set(func(r *FileDupReport) { r.Errors++ })
				continue
			}
			hashes[c.id] = h
			by[h] = append(by[h], c)
		}
		for _, g := range by {
			if len(g) > 1 {
				groups = append(groups, g)
			}
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i][0].size > groups[j][0].size })

	var wasted int64
	extra := 0
	sample := []FileDupGroup{}
	type link struct{ id, canon int64 }
	var links []link
	for _, g := range groups {
		ci := pickCanonical(g)
		fg := FileDupGroup{Size: g[0].size}
		for i, c := range g {
			fg.Members = append(fg.Members, FileDupMember{ID: c.id, Path: c.path, Canonical: i == ci})
			if i != ci {
				links = append(links, link{c.id, g[ci].id})
				extra++
				wasted += c.size
			}
		}
		if len(sample) < 25 {
			sample = append(sample, fg)
		}
	}
	set(func(r *FileDupReport) {
		r.Groups, r.Extra, r.WastedMB, r.Sample = len(groups), extra, wasted>>20, sample
	})
	if !apply {
		return
	}
	err = l.tx(ctx, func(t *sql.Tx) error {
		if _, err := t.Exec(`UPDATE media SET dup_of = NULL`); err != nil {
			return err
		}
		for id, h := range hashes {
			if _, err := t.Exec(`UPDATE media SET content_id = ? WHERE id = ?`, h, id); err != nil {
				return err
			}
		}
		for _, lk := range links {
			if _, err := t.Exec(`UPDATE media SET dup_of = ? WHERE id = ?`, lk.canon, lk.id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		set(func(r *FileDupReport) { r.Errors++ })
	}
}

func libFileDupesHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		apply, _ := strconv.ParseBool(r.URL.Query().Get("apply"))
		if r.URL.Query().Get("clear") == "1" {
			if _, err := l.w.ExecContext(r.Context(), `UPDATE media SET dup_of = NULL`); err != nil {
				libFail(w, err)
				return
			}
			libJSON(w, map[string]any{"cleared": true})
			return
		}
		go l.ScanFileDupes(context.Background(), apply)
		libJSON(w, map[string]any{"started": true, "apply": apply})
		return
	}
	fileDupState.Lock()
	rep := fileDupState.rep
	fileDupState.Unlock()
	var linked int
	l.r.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM media WHERE dup_of IS NOT NULL`).Scan(&linked)
	libJSON(w, map[string]any{"report": rep, "linked": linked, "note": strings.TrimSpace("POST ?apply=1 grava; POST ?clear=1 desfaz")})
}

// Durações: vídeos sem duration_s são sondados em lote (ffprobe) e a
// temporada vira 1 quando há número de episódio mas nenhuma temporada.
var durState struct {
	sync.Mutex
	running     bool
	done, total int
	errs        int
}

func (l *Library) FillDurations(ctx context.Context) {
	durState.Lock()
	if durState.running {
		durState.Unlock()
		return
	}
	durState.running, durState.done, durState.total, durState.errs = true, 0, 0, 0
	durState.Unlock()
	defer func() { durState.Lock(); durState.running = false; durState.Unlock() }()

	l.w.ExecContext(ctx, `UPDATE media SET season = 1 WHERE kind='video' AND season IS NULL AND episode IS NOT NULL`)
	rows, err := l.r.QueryContext(ctx, `SELECT id, path FROM media WHERE kind='video' AND missing=0 AND broken IS NULL AND (duration_s IS NULL OR duration_s <= 0)`)
	if err != nil {
		return
	}
	type job struct {
		id   int64
		path string
	}
	var jobs []job
	for rows.Next() {
		var j job
		if rows.Scan(&j.id, &j.path) == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()
	durState.Lock()
	durState.total = len(jobs)
	durState.Unlock()

	root := filepath.Clean(MEDIA_PATH)
	ch := make(chan job)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				full := filepath.Join(root, filepath.FromSlash(j.path))
				info, err := os.Stat(full)
				var mi *MediaInfo
				if err == nil {
					pctx, cancel := context.WithTimeout(ctx, 60*time.Second)
					mi, err = probeMedia(pctx, full, info)
					cancel()
				}
				if err == nil && mi != nil && mi.Duration > 0 {
					l.w.ExecContext(ctx, `UPDATE media SET duration_s=? WHERE id=?`, mi.Duration, j.id)
					l.fixProgressDuration(ctx, j.id, mi.Duration)
				} else {
					if reason := brokenReason(err, mi); reason != "" {
						l.w.ExecContext(ctx, `UPDATE media SET broken=? WHERE id=?`, reason, j.id)
					}
					durState.Lock()
					durState.errs++
					durState.Unlock()
				}
				durState.Lock()
				durState.done++
				durState.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		if ctx.Err() != nil {
			break
		}
		ch <- j
	}
	close(ch)
	wg.Wait()
}

func libDurationsHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		go l.FillDurations(context.Background())
		libJSON(w, map[string]any{"started": true})
		return
	}
	durState.Lock()
	out := map[string]any{"running": durState.running, "done": durState.done, "total": durState.total, "errors": durState.errs}
	durState.Unlock()
	libJSON(w, out)
}

// Miniaturas escuras (geradas no primeiro segundo, quase sempre preto) são
// apagadas do cache; o worker gera de novo, num ponto mais adiante, no próximo pedido.
func darkThumb(path string) bool {
	img, err := imaging.Open(path)
	if err != nil {
		return false
	}
	small := imaging.Resize(img, 16, 0, imaging.Box)
	b := small.Bounds()
	var sum, n float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := small.At(x, y).RGBA()
			sum += 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(bl>>8)
			n++
		}
	}
	return n > 0 && sum/n < 14
}

func libDarkThumbsHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	apply := r.Method == http.MethodPost
	scanned, dark := 0, 0
	filepath.WalkDir(THUMBNAIL_DIR, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(p), ".jpg") {
			return nil
		}
		scanned++
		if darkThumb(p) {
			dark++
			if apply {
				os.Remove(p)
			}
		}
		return nil
	})
	libJSON(w, map[string]any{"scanned": scanned, "dark": dark, "removed": apply})
}

// brokenReason: motivo para marcar o arquivo como corrompido, ou "" quando a
// falha não diz nada sobre o arquivo (timeout, ffprobe ausente, arquivo sumiu).
func brokenReason(err error, mi *MediaInfo) string {
	if err == nil {
		if mi != nil && mi.VideoIndex < 0 && len(mi.Audio) == 0 {
			return "sem faixas de vídeo ou áudio"
		}
		return ""
	}
	var exitErr *exec.ExitError
	// morto por sinal (-1) = timeout/cancelamento, não defeito do arquivo
	if !errors.As(err, &exitErr) || exitErr.ExitCode() < 0 {
		return ""
	}
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 && i+2 < len(msg) {
		msg = msg[i+2:]
	}
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return strings.TrimSpace(msg)
}

// MarkBroken grava (ou limpa, com reason "") o estado de corrompido de um caminho.
func (l *Library) MarkBroken(ctx context.Context, rel, reason string) {
	var v any
	if reason != "" {
		v = reason
	}
	l.w.ExecContext(ctx, `UPDATE media SET broken=? WHERE path=? AND COALESCE(broken,'') <> COALESCE(?,'')`, v, rel, v)
}

// mediaRelPath: caminho absoluto dentro de MEDIA_PATH → caminho do catálogo.
func mediaRelPath(full string) string {
	rel, err := filepath.Rel(filepath.Clean(MEDIA_PATH), full)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}
