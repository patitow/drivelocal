package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
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

	"github.com/gorilla/mux"
)

// Faixas de áudio/legenda: o arquivo continua sendo servido cru por streamHandler;
// quando o navegador não toca o arquivo/faixa (ex.: EAC3, AVI, HEVC sem suporte) ou o
// usuário escolhe outra faixa, streamHandler delega para streamProcessed (remux/transcode).

type AudioTrack struct {
	Order    int    `json:"order"` // posição entre as faixas de áudio (0:a:N)
	Lang     string `json:"lang"`
	Title    string `json:"title"`
	Codec    string `json:"codec"`
	Channels int    `json:"channels"`
	Default  bool   `json:"default"`
	Playable bool   `json:"playable"` // o navegador toca sem transcodificar
}

type SubtitleTrack struct {
	ID        string `json:"id"` // "emb:<índice do stream>" ou "ext:<arquivo>"
	Lang      string `json:"lang"`
	Title     string `json:"title"`
	Codec     string `json:"codec"`
	External  bool   `json:"external"`
	Default   bool   `json:"default"`
	Supported bool   `json:"supported"` // o player sabe exibir (ver Kind)
	Kind      string `json:"kind"`      // "text" (WebVTT), "ass" (JASSUB), "pgs" (libpgs) ou "" (DVD etc.)
	// Ready: legenda de texto embutida já extraída para o cache (abre na hora;
	// num mkv grande a primeira extração lê o arquivo inteiro).
	Ready bool `json:"ready"`
}

// FontAttachment é uma fonte anexada ao mkv (usada pelas legendas ASS).
type FontAttachment struct {
	Index int    `json:"index"` // índice do stream de anexo
	Name  string `json:"name"`
}

type MediaInfo struct {
	Duration     float64          `json:"duration"`
	Container    string           `json:"container"` // format_name do ffprobe (ex.: "matroska,webm")
	VideoIndex   int              `json:"videoIndex"` // índice absoluto do vídeo principal; -1 = sem vídeo
	VideoCodec   string           `json:"videoCodec"`
	VideoProfile string           `json:"videoProfile"`
	PixFmt       string           `json:"pixFmt"`
	Width        int              `json:"width"`
	Height       int              `json:"height"`
	// start_time do contêiner (pts cru do início). O "tempo do arquivo" usado pelo player é
	// pts cru - max(StartTime, 0): igual ao currentTime do Chrome tocando o arquivo cru (mkv/mp4
	// com start_time <= 0) e começando em 0 em MPEG-TS (start_time típico de 1,4 s ou mais).
	StartTime    float64          `json:"-"`
	Audio        []AudioTrack     `json:"audio"`
	Subtitles    []SubtitleTrack  `json:"subtitles"`
	Fonts        []FontAttachment `json:"fonts"`
	Chapters     []Chapter        `json:"chapters"`
	// trecho da abertura (capítulo "OP"/"Opening"/"Intro"), para o botão "Pular abertura"
	Intro    *Chapter  `json:"intro,omitempty"`
	Playback     *Playback        `json:"playback,omitempty"` // calculado por requisição (depende do navegador)
}

type ffprobeStream struct {
	Index       int               `json:"index"`
	CodecType   string            `json:"codec_type"`
	CodecName   string            `json:"codec_name"`
	Channels    int               `json:"channels"`
	Profile     string            `json:"profile"`
	PixFmt      string            `json:"pix_fmt"`
	Width       int               `json:"width"`
	Height      int               `json:"height"`
	Duration    string            `json:"duration"`
	Tags        map[string]string `json:"tags"`
	Disposition map[string]int    `json:"disposition"`
}

// Chapter: capítulo do contêiner, em segundos do "tempo do arquivo".
type Chapter struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Title string  `json:"title,omitempty"`
}

type ffprobeOutput struct {
	Streams  []ffprobeStream `json:"streams"`
	Chapters []struct {
		StartTime string            `json:"start_time"`
		EndTime   string            `json:"end_time"`
		Tags      map[string]string `json:"tags"`
	} `json:"chapters"`
	Format  struct {
		Duration   string `json:"duration"`
		StartTime  string `json:"start_time"`
		FormatName string `json:"format_name"`
	} `json:"format"`
}

var (
	textSubtitleCodecs = map[string]bool{"subrip": true, "srt": true, "webvtt": true, "mov_text": true, "text": true}
	// codecs de áudio que o <video> toca sem ajuda (dentro de mkv/mp4)
	browserAudioCodecs = map[string]bool{"aac": true, "mp3": true, "opus": true, "vorbis": true, "flac": true}
	externalSubExts    = map[string]bool{".srt": true, ".ass": true, ".ssa": true, ".vtt": true}

	probeCache   sync.Map // chave path|mtime|size -> *MediaInfo
	errNotMedia  = fmt.Errorf("not a media file")
	errBadMedia  = fmt.Errorf("invalid media path")
	tagLangAlias = map[string]string{"und": ""}
)

// resolveMediaFile valida o caminho vindo da URL e devolve o caminho absoluto de um arquivo dentro de MEDIA_PATH.
func resolveMediaFile(raw string) (string, os.FileInfo, error) {
	rel := strings.ReplaceAll(decodeMediaPath(raw), "/", string(filepath.Separator))
	root := filepath.Clean(MEDIA_PATH)
	full := filepath.Clean(filepath.Join(root, rel))
	if full != root && !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return "", nil, errBadMedia
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", nil, err
	}
	if info.IsDir() {
		return "", nil, errNotMedia
	}
	return full, info, nil
}

func probeMedia(ctx context.Context, full string, info os.FileInfo) (*MediaInfo, error) {
	key := fmt.Sprintf("%s|%d|%d", full, info.ModTime().UnixNano(), info.Size())
	if v, ok := probeCache.Load(key); ok {
		return cloneMediaInfo(v.(*MediaInfo)), nil
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-print_format", "json", "-show_streams", "-show_format", "-show_chapters", full)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var out ffprobeOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, err
	}
	mi := buildMediaInfo(out, full)
	probeCache.Store(key, mi)
	return cloneMediaInfo(mi), nil
}

func cloneMediaInfo(mi *MediaInfo) *MediaInfo {
	c := *mi
	// make (não append sobre nil): lista vazia tem de serializar como [], nunca null
	c.Audio = append(make([]AudioTrack, 0, len(mi.Audio)), mi.Audio...)
	c.Subtitles = append(make([]SubtitleTrack, 0, len(mi.Subtitles)), mi.Subtitles...)
	c.Fonts = append(make([]FontAttachment, 0, len(mi.Fonts)), mi.Fonts...)
	c.Chapters = append(make([]Chapter, 0, len(mi.Chapters)), mi.Chapters...)
	c.Playback = nil
	return &c
}

func buildMediaInfo(out ffprobeOutput, full string) *MediaInfo {
	mi := &MediaInfo{Audio: []AudioTrack{}, Subtitles: []SubtitleTrack{}, Fonts: []FontAttachment{}, VideoIndex: -1}
	mi.Duration, _ = strconv.ParseFloat(out.Format.Duration, 64)
	if st, err := strconv.ParseFloat(out.Format.StartTime, 64); err == nil && !math.IsNaN(st) && !math.IsInf(st, 0) {
		mi.StartTime = st
	}
	mi.Container = out.Format.FormatName

	audioOrder := 0
	for _, s := range out.Streams {
		switch s.CodecType {
		case "video":
			// capa (attached_pic) não é o vídeo: mapear 0:v:0 pegaria a imagem
			if mi.VideoIndex < 0 && s.Disposition["attached_pic"] == 0 {
				mi.VideoIndex, mi.VideoCodec, mi.VideoProfile = s.Index, s.CodecName, s.Profile
				mi.PixFmt, mi.Width, mi.Height = s.PixFmt, s.Width, s.Height
				if mi.Duration <= 0 {
					mi.Duration, _ = strconv.ParseFloat(s.Duration, 64)
				}
			}
		case "attachment":
			if name := s.Tags["filename"]; isFontAttachment(name, s.Tags["mimetype"]) {
				mi.Fonts = append(mi.Fonts, FontAttachment{Index: s.Index, Name: name})
			}
		case "audio":
			mi.Audio = append(mi.Audio, AudioTrack{
				Order:    audioOrder,
				Lang:     normalizeLang(s.Tags["language"]),
				Title:    s.Tags["title"],
				Codec:    s.CodecName,
				Channels: s.Channels,
				Default:  s.Disposition["default"] == 1,
				Playable: browserAudioCodecs[s.CodecName],
			})
			audioOrder++
		case "subtitle":
			kind := subtitleKind(s.CodecName)
			mi.Subtitles = append(mi.Subtitles, SubtitleTrack{
				ID:        "emb:" + strconv.Itoa(s.Index),
				Lang:      normalizeLang(s.Tags["language"]),
				Title:     s.Tags["title"],
				Codec:     s.CodecName,
				Default:   s.Disposition["default"] == 1,
				Supported: kind != "",
				Kind:      kind,
			})
		}
	}
	mi.Subtitles = append(mi.Subtitles, externalSubtitles(full)...)
	mi.Chapters = []Chapter{}
	base := math.Max(mi.StartTime, 0)
	for _, c := range out.Chapters {
		st, err1 := strconv.ParseFloat(c.StartTime, 64)
		en, err2 := strconv.ParseFloat(c.EndTime, 64)
		if err1 != nil || err2 != nil || en <= st {
			continue
		}
		ch := Chapter{Start: math.Max(st-base, 0), End: en - base, Title: c.Tags["title"]}
		mi.Chapters = append(mi.Chapters, ch)
		if mi.Intro == nil && isIntroChapter(ch) {
			intro := ch
			mi.Intro = &intro
		}
	}
	return mi
}

var introTitle = regexp.MustCompile(`(?i)^\s*(op|opening|intro|abertura)(\s*\d+|\s+song)?\s*$`)

// isIntroChapter: capítulo com nome de abertura, curto (até 3 min) e nos primeiros 10 min.
func isIntroChapter(c Chapter) bool {
	return introTitle.MatchString(c.Title) && c.End-c.Start <= 180 && c.Start < 600
}

// subtitleKind diz como o player exibe a legenda; "" = não suportada (ex.: dvd_subtitle).
func subtitleKind(codec string) string {
	switch {
	case codec == "ass" || codec == "ssa":
		return "ass"
	case codec == "hdmv_pgs_subtitle" || codec == "sup":
		return "pgs"
	case textSubtitleCodecs[codec] || codec == "vtt":
		return "text"
	}
	return ""
}

func isFontAttachment(name, mime string) bool {
	mime = strings.ToLower(mime)
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ttf", ".otf", ".ttc", ".otc", ".woff", ".woff2":
		return true
	}
	return strings.Contains(mime, "font") || strings.Contains(mime, "truetype") || strings.Contains(mime, "opentype")
}

func normalizeLang(l string) string {
	l = strings.ToLower(strings.TrimSpace(l))
	if alias, ok := tagLangAlias[l]; ok {
		return alias
	}
	return l
}

// externalSubtitles lista legendas ao lado do vídeo que começam com o mesmo nome (sem extensão).
func externalSubtitles(videoPath string) []SubtitleTrack {
	dir := filepath.Dir(videoPath)
	stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var res []SubtitleTrack
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !externalSubExts[ext] || !strings.HasPrefix(e.Name(), stem) {
			continue
		}
		suffix := strings.Trim(strings.TrimSuffix(strings.TrimPrefix(e.Name(), stem), filepath.Ext(e.Name())), " ._-[]()")
		codec := strings.TrimPrefix(ext, ".")
		res = append(res, SubtitleTrack{
			ID:        "ext:" + e.Name(),
			Lang:      guessLangFromSuffix(suffix),
			Title:     suffix,
			Codec:     codec,
			External:  true,
			Supported: true,
			Kind:      subtitleKind(codec),
		})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].ID < res[j].ID })
	return res
}

func guessLangFromSuffix(s string) string {
	s = strings.ToLower(s)
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '_' || r == '-' || r == ' ' }) {
		switch part {
		case "pt", "por", "ptbr", "pob", "br":
			return "por"
		case "en", "eng":
			return "eng"
		case "es", "spa":
			return "spa"
		case "ja", "jpn", "jp":
			return "jpn"
		}
	}
	return ""
}

func mediaInfoHandler(w http.ResponseWriter, r *http.Request) {
	full, info, err := resolveMediaFile(mux.Vars(r)["path"])
	if err != nil {
		writeMediaError(w, err)
		return
	}
	mi, err := probeMedia(r.Context(), full, info)
	if reason := brokenReason(err, mi); reason != "" {
		if l := getLibrary(); l != nil {
			l.MarkBroken(context.Background(), mediaRelPath(full), reason)
		}
		// o player mostra o aviso em vez de tentar tocar
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{"error": "Broken media", "broken": reason})
		return
	}
	if err != nil {
		logWarn("media-info failed for %s: %v", full, err)
		http.Error(w, `{"error":"Failed to probe media"}`, http.StatusUnprocessableEntity)
		return
	}
	prefetchTextSubs(full, info, mi)
	applyCaps(mi, parseCaps(r.URL.Query().Get("caps")), full)
	pending := markReadySubs(full, info, mi)
	w.Header().Set("Content-Type", "application/json")
	if pending {
		w.Header().Set("Cache-Control", "no-store") // o player pergunta de novo até ficarem prontas
	} else {
		w.Header().Set("Cache-Control", "private, max-age=300")
	}
	json.NewEncoder(w).Encode(mi)
}

// markReadySubs marca as legendas que abrem sem esperar extração; devolve
// true se alguma de texto embutida ainda está sendo extraída.
func markReadySubs(full string, info os.FileInfo, mi *MediaInfo) (pending bool) {
	dir := mediaCacheDir(full, info)
	for i := range mi.Subtitles {
		s := &mi.Subtitles[i]
		if s.External || s.Kind != "text" {
			s.Ready = s.Supported
			continue
		}
		_, err := os.Stat(filepath.Join(dir, "sub-"+sanitizeCacheName(s.ID)+".vtt"))
		s.Ready = err == nil
		pending = pending || !s.Ready
	}
	return pending
}

func writeMediaError(w http.ResponseWriter, err error) {
	switch {
	case err == errBadMedia:
		http.Error(w, `{"error":"Access denied"}`, http.StatusForbidden)
	case err == errNotMedia:
		http.Error(w, `{"error":"Not a file"}`, http.StatusBadRequest)
	case os.IsNotExist(err):
		http.Error(w, `{"error":"File not found"}`, http.StatusNotFound)
	default:
		http.Error(w, `{"error":"Failed to open file"}`, http.StatusInternalServerError)
	}
}

func defaultStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// flushWriter envia cada bloco ao cliente imediatamente (stream sem Content-Length).
type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if fl, ok := f.w.(http.Flusher); ok {
		fl.Flush()
	}
	return n, err
}
