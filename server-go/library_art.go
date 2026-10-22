package main

// Acervo local de arte (capas e banners das fichas). Toda imagem que entra numa
// ficha é guardada aqui, com o nome igual ao hash do conteúdo: as baixadas de
// bases públicas (links que expiram), as enviadas do computador e as tiradas de
// um arquivo da biblioteca. Na ficha fica "art:<arquivo>"; a página mostra
// /api/art/<arquivo>.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
)

const (
	artPrefix   = "art:"
	artMaxBytes = 15 << 20
	// arte tirada de um arquivo da biblioteca: largura máxima (serve de banner)
	artFrameWidth = 1920
)

var reArtFile = regexp.MustCompile(`^[0-9a-f]{64}\.(jpg|png|webp|gif)$`)

func (l *Library) artDir() string { return filepath.Join(filepath.Dir(l.path), "art") }

func isWebURL(u string) bool {
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")
}

// artPublic: o endereço que a página usa para um valor de capa/banner.
func artPublic(v string) string {
	if name, ok := strings.CutPrefix(v, artPrefix); ok && reArtFile.MatchString(name) {
		return "/api/art/" + name
	}
	if isWebURL(v) {
		return v
	}
	return ""
}

// artStored: o valor que a ficha guarda; aceita de volta o endereço público.
func artStored(v string) string {
	v = strings.TrimSpace(v)
	if name, ok := strings.CutPrefix(v, "/api/art/"); ok && reArtFile.MatchString(name) {
		return artPrefix + name
	}
	if name, ok := strings.CutPrefix(v, artPrefix); ok && reArtFile.MatchString(name) {
		return v
	}
	if isWebURL(v) {
		return v
	}
	return ""
}

// saveArt grava a imagem no acervo e devolve "art:<arquivo>".
func (l *Library) saveArt(b []byte) (string, error) {
	ext := ""
	switch http.DetectContentType(b) {
	case "image/jpeg":
		ext = "jpg"
	case "image/png":
		ext = "png"
	case "image/webp":
		ext = "webp"
	case "image/gif":
		ext = "gif"
	default:
		return "", fmt.Errorf("%w: o arquivo não é uma imagem", errInvalid)
	}
	sum := sha256.Sum256(b)
	name := hex.EncodeToString(sum[:]) + "." + ext
	dir := l.artDir()
	dst := filepath.Join(dir, name)
	if _, err := os.Stat(dst); err == nil {
		return artPrefix + name, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if !artReserve(dir, int64(len(b))) {
		return "", fmt.Errorf("%w: o acervo de arte está cheio", errInvalid)
	}
	tmp, err := os.CreateTemp(dir, ".art-*")
	if err != nil {
		return "", err
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), dst)
	}
	if werr != nil {
		os.Remove(tmp.Name())
		return "", werr
	}
	return artPrefix + name, nil
}

// O acervo tem teto de espaço: as buscas automáticas de ficha (que qualquer
// usuário dispara) baixam arte, e não podem encher o disco.
const artDirMaxBytes = 2 << 30

var (
	artUseMu sync.Mutex
	artUse   = map[string]int64{} // pasta → bytes em uso (medido na primeira gravação)
)

func artReserve(dir string, n int64) bool {
	artUseMu.Lock()
	defer artUseMu.Unlock()
	used, ok := artUse[dir]
	if !ok {
		filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if fi, e := d.Info(); e == nil {
					used += fi.Size()
				}
			}
			return nil
		})
	}
	if used+n > artDirMaxBytes {
		artUse[dir] = used
		return false
	}
	artUse[dir] = used + n
	return true
}

// artHTTP só conecta em endereços públicos: um link de capa não pode apontar
// para a rede local, o próprio servidor ou o metadata da nuvem. A checagem é
// no momento da conexão, então vale também para redirecionamentos e para DNS
// que muda de resposta.
var (
	artAllowPrivate = false // testes (httptest escuta em 127.0.0.1)
	artHTTP         = &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: (&net.Dialer{Timeout: 10 * time.Second, Control: func(_, addr string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					return err
				}
				ip := net.ParseIP(host)
				if ip == nil || (!artAllowPrivate && !isPublicIP(ip)) {
					return fmt.Errorf("%w: endereço não público", errInvalid)
				}
				return nil
			}}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || !isWebURL(req.URL.String()) {
				return errors.New("redirecionamento recusado")
			}
			return nil
		},
	}
)

func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		// 100.64/10 (CGNAT), 0/8, 198.18/15 (benchmark), 240/4 (reservado)
		return !(v4[0] == 0 || (v4[0] == 100 && v4[1]&0xc0 == 64) || (v4[0] == 198 && v4[1]&0xfe == 18) || v4[0] >= 240)
	}
	return true
}

// fetchArt baixa uma imagem da web para o acervo.
func (l *Library) fetchArt(ctx context.Context, u string) (string, error) {
	if !isWebURL(u) {
		return "", errInvalid
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", errInvalid
	}
	req.Header.Set("User-Agent", "DriveLocal/1.0")
	resp, err := artHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, artMaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(b) > artMaxBytes {
		return "", fmt.Errorf("%w: imagem grande demais", errInvalid)
	}
	return l.saveArt(b)
}

// localArt troca um link da web pela cópia local; se não der para baixar,
// mantém o link (a próxima rodada de localizeArtBacklog tenta de novo).
func (l *Library) localArt(ctx context.Context, v string) string {
	if !isWebURL(v) {
		return v
	}
	ref, err := l.fetchArt(ctx, v)
	if err != nil {
		logWarn("arte: não deu para baixar %s: %v", v, err)
		return v
	}
	return ref
}

// localizeEntry baixa capa e banner da ficha, guardando o link de origem.
func (l *Library) localizeEntry(ctx context.Context, d *EntryData) {
	if isWebURL(d.Cover) {
		src := d.Cover
		if d.Cover = l.localArt(ctx, src); d.Cover != src {
			d.CoverSrc = src
		}
	}
	if isWebURL(d.Banner) {
		src := d.Banner
		if d.Banner = l.localArt(ctx, src); d.Banner != src {
			d.BannerSrc = src
		}
	}
}

// artCrop: recorte escolhido no Gerenciar, em frações da imagem (0 a 1).
type artCrop struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

func (c *artCrop) valid() bool {
	return c.W > 0.01 && c.H > 0.01 && c.X >= 0 && c.Y >= 0 && c.X+c.W <= 1.0001 && c.Y+c.H <= 1.0001
}

// mediaArt tira a arte de um arquivo da biblioteca: a própria imagem ou um
// quadro do vídeo (at em segundos; sem ele, a 25% da duração), em até 1920px,
// com o recorte escolhido quando houver.
func (l *Library) mediaArt(ctx context.Context, id int64, at float64, crop *artCrop) (string, error) {
	b, err := l.mediaFrame(ctx, id, at, crop, artFrameWidth)
	if err != nil {
		return "", err
	}
	return l.saveArt(b)
}

// mediaFrame: o quadro em JPEG, sem guardar (também serve de prévia).
func (l *Library) mediaFrame(ctx context.Context, id int64, at float64, crop *artCrop, width int) ([]byte, error) {
	if crop != nil && !crop.valid() {
		return nil, fmt.Errorf("%w: recorte fora da imagem", errInvalid)
	}
	var rel, kind string
	var dur sql.NullFloat64
	err := l.r.QueryRowContext(ctx, `SELECT path, kind, duration_s FROM media WHERE id=? AND missing=0`, id).Scan(&rel, &kind, &dur)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	full, ok := resolveMediaPath(rel)
	if !ok {
		return nil, errNotFound
	}
	args := []string{"-hide_banner", "-loglevel", "error"}
	if kind == "video" {
		if at <= 0 {
			at = 60
			if dur.Valid && dur.Float64 > 0 {
				at = dur.Float64 * 0.25
			}
		}
		args = append(args, "-ss", strconv.FormatFloat(at, 'f', 1, 64))
	}
	vf := fmt.Sprintf("scale='min(%d,iw)':-2", width)
	if crop != nil {
		vf = fmt.Sprintf("crop=iw*%.5f:ih*%.5f:iw*%.5f:ih*%.5f,", crop.W, crop.H, crop.X, crop.Y) + vf
	}
	args = append(args, "-i", full, "-an", "-sn", "-map", "0:v:0", "-frames:v", "1",
		"-vf", vf,
		"-q:v", "3", "-f", "image2", "-c:v", "mjpeg", "pipe:1")
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "ffmpeg", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w", err)
	}
	if out.Len() == 0 {
		return nil, errors.New("ffmpeg não gerou imagem")
	}
	return out.Bytes(), nil
}

// GET /api/admin/library/art/frame?mediaId=&at=: prévia do quadro do vídeo
// (para o vídeo que o navegador não toca), sem guardar.
func libArtFrameHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("mediaId"), 10, 64)
	at, _ := strconv.ParseFloat(r.URL.Query().Get("at"), 64)
	if id <= 0 {
		libFail(w, errInvalid)
		return
	}
	b, err := l.mediaFrame(r.Context(), id, at, nil, 1280)
	if err != nil {
		libFail(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(b)
}

// localizeArtBacklog baixa a arte que ainda aponta para a web: fichas salvas
// antes do acervo local e fichas cuja cópia falhou. Roda uma vez na abertura.
func (l *Library) localizeArtBacklog(ctx context.Context) {
	rows, err := l.r.QueryContext(ctx, `SELECT ref, data FROM entry_meta`)
	if err != nil {
		return
	}
	type row struct {
		ref string
		d   EntryData
	}
	var todo []row
	for rows.Next() {
		var ref, s string
		var d EntryData
		if rows.Scan(&ref, &s) == nil && json.Unmarshal([]byte(s), &d) == nil && (isWebURL(d.Cover) || isWebURL(d.Banner)) {
			todo = append(todo, row{ref, d})
		}
	}
	rows.Close()
	done := 0
	for _, t := range todo {
		if ctx.Err() != nil {
			return
		}
		before := t.d
		l.localizeEntry(ctx, &t.d)
		if t.d.Cover == before.Cover && t.d.Banner == before.Banner {
			continue
		}
		b, _ := json.Marshal(t.d)
		// só troca se a ficha não mudou enquanto baixava
		if res, err := l.w.ExecContext(ctx, `UPDATE entry_meta SET data=? WHERE ref=? AND data=?`, string(b), t.ref, mustJSON(before)); err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				done++
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if done > 0 {
		logInfo("🖼️ arte: %d fichas passaram a usar cópia local", done)
	}
	l.localizeMetaCache(ctx)
}

// localizeMetaCache faz o mesmo com o cache da busca automática.
func (l *Library) localizeMetaCache(ctx context.Context) {
	rows, err := l.r.QueryContext(ctx, `SELECT key, data FROM ext_meta WHERE data IS NOT NULL AND data LIKE '%"http%'`)
	if err != nil {
		return
	}
	type row struct{ key, data string }
	var todo []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.key, &r.data) == nil {
			todo = append(todo, r)
		}
	}
	rows.Close()
	for _, t := range todo {
		if ctx.Err() != nil {
			return
		}
		var m ExtMeta
		if json.Unmarshal([]byte(t.data), &m) != nil || (!isWebURL(m.Cover) && !isWebURL(m.Banner)) {
			continue
		}
		c, b := l.localArt(ctx, m.Cover), l.localArt(ctx, m.Banner)
		if c == m.Cover && b == m.Banner {
			continue
		}
		m.Cover, m.Banner = c, b
		l.w.ExecContext(ctx, `UPDATE ext_meta SET data=? WHERE key=? AND data=?`, mustJSON(m), t.key, t.data)
		time.Sleep(200 * time.Millisecond)
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// GET /api/art/{file}: imagem do acervo (o nome é o hash; nunca muda).
// Quem vê a ficha vê a arte; o nome não se adivinha.
func libArtFileHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["file"]
	if !reArtFile.MatchString(name) {
		libFail(w, errNotFound)
		return
	}
	f, err := os.Open(filepath.Join(l.artDir(), name))
	if err != nil {
		libFail(w, errNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		libFail(w, errNotFound)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, st.ModTime(), f)
}

// POST /api/admin/library/art: guarda uma imagem no acervo e devolve a ref.
// JSON {url} baixa da web; {mediaId, at?, crop?} tira de um arquivo da biblioteca;
// multipart com "file" guarda o enviado.
func libArtStoreHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var ref string
	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		r.Body = http.MaxBytesReader(w, r.Body, artMaxBytes+(1<<20))
		f, _, ferr := r.FormFile("file")
		if ferr != nil {
			libFail(w, fmt.Errorf("%w: envie a imagem no campo file", errInvalid))
			return
		}
		defer f.Close()
		b, rerr := io.ReadAll(io.LimitReader(f, artMaxBytes+1))
		switch {
		case rerr != nil:
			err = rerr
		case len(b) > artMaxBytes:
			err = fmt.Errorf("%w: imagem grande demais (máx. 15 MB)", errInvalid)
		default:
			ref, err = l.saveArt(b)
		}
	} else {
		var body struct {
			URL     string  `json:"url"`
			MediaID int64    `json:"mediaId"`
			At      float64  `json:"at"`
			Crop    *artCrop `json:"crop"`
		}
		if err = decodeBody(r, &body); err == nil {
			switch {
			case body.MediaID > 0:
				ref, err = l.mediaArt(r.Context(), body.MediaID, body.At, body.Crop)
			case body.URL != "":
				ref, err = l.fetchArt(r.Context(), strings.TrimSpace(body.URL))
			default:
				err = errInvalid
			}
		}
	}
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"ref": ref, "url": artPublic(ref)})
}
