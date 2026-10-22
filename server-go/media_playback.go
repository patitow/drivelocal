package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// Decisão de reprodução (direct/remux/transcode), stream processado pelo ffmpeg,
// extração de legendas (WebVTT, ASS cru, PGS .sup) e fontes anexadas, com cache em disco.

type Playback struct {
	Mode   string `json:"mode"` // "direct" | "remux" | "transcode"
	Reason string `json:"reason"`
}

// Caps são os codecs/contêineres que o navegador declarou tocar (?caps=h264hi10,hevc,...).
type Caps map[string]bool

// Sem ?caps= assume um navegador conservador: H.264 8-bit, VP8/VP9/AV1 e mkv (Chrome/Edge/Firefox).
const defaultCaps = "vp9,av1,mkv"

var (
	MEDIA_CACHE_DIR = getEnv("MEDIA_CACHE_DIR", filepath.Join("cache", "media"))

	remuxSlots     = make(chan struct{}, 3)
	transcodeSlots = make(chan struct{}, 2) // libx264 é caro: 2 simultâneos no máximo
	extractSlots   = make(chan struct{}, 2)
	slotWait       = 4 * time.Second // seek troca a requisição: espera a anterior liberar a vaga
	cacheLocks     sync.Map          // caminho no cache -> *sync.Mutex (evita extração duplicada)
)

func parseCaps(raw string) Caps {
	if strings.TrimSpace(raw) == "" {
		raw = defaultCaps
	}
	c := Caps{"h264": true, "vp8": true}
	for _, t := range strings.Split(raw, ",") {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			c[t] = true
		}
	}
	return c
}

func audioPlayable(codec string, c Caps) bool {
	return browserAudioCodecs[codec] || c[codec]
}

// defaultAudioTrack é a faixa que o navegador toca no modo direct (default, senão a primeira).
func defaultAudioTrack(mi *MediaInfo) *AudioTrack {
	for i := range mi.Audio {
		if mi.Audio[i].Default {
			return &mi.Audio[i]
		}
	}
	if len(mi.Audio) > 0 {
		return &mi.Audio[0]
	}
	return nil
}

// videoSupported: o navegador decodifica o vídeo (codec, profundidade de cor e croma)?
func videoSupported(mi *MediaInfo, c Caps) (bool, string) {
	pf := mi.PixFmt
	highBit := strings.Contains(pf, "10") || strings.Contains(pf, "12")
	chroma420 := pf == "" || strings.Contains(pf, "420") || pf == "nv12" || pf == "p010le"
	name := strings.ToUpper(mi.VideoCodec)
	if !chroma420 && mi.VideoCodec != "vp8" {
		return false, fmt.Sprintf("vídeo %s em %s (croma 4:2:2/4:4:4) não toca no navegador", name, pf)
	}
	switch mi.VideoCodec {
	case "h264":
		if highBit && !c["h264hi10"] {
			return false, "H.264 10-bit (Hi10P) não é suportado pelo navegador"
		}
		return true, ""
	case "hevc":
		if highBit && !c["hevc10"] {
			return false, "HEVC 10-bit não é suportado pelo navegador"
		}
		if !highBit && !c["hevc"] {
			return false, "HEVC não é suportado pelo navegador"
		}
		return true, ""
	case "av1", "vp9":
		if !c[mi.VideoCodec] {
			return false, name + " não é suportado pelo navegador"
		}
		return true, ""
	case "vp8":
		return true, ""
	}
	return false, fmt.Sprintf("codec de vídeo %s não é suportado por navegadores", mi.VideoCodec)
}

// containerSupported: o <video> abre o arquivo cru? (AVI, MPEG-TS, MPEG-PS, FLV, ASF não)
func containerSupported(mi *MediaInfo, full string, c Caps) (bool, string) {
	f := mi.Container
	switch {
	case strings.Contains(f, "mp4") || strings.Contains(f, "mov"):
		return true, ""
	case strings.Contains(f, "matroska"):
		if strings.EqualFold(filepath.Ext(full), ".webm") || c["mkv"] {
			return true, ""
		}
		return false, "o navegador não abre MKV"
	}
	return false, fmt.Sprintf("contêiner %s não é suportado pelo navegador", strings.SplitN(f, ",", 2)[0])
}

// decidePlayback escolhe o modo mais barato que o navegador consegue tocar.
func decidePlayback(mi *MediaInfo, c Caps, full string) Playback {
	if mi.VideoIndex >= 0 {
		if ok, why := videoSupported(mi, c); !ok {
			return Playback{"transcode", why}
		}
	} else if len(mi.Audio) == 0 {
		return Playback{"direct", "arquivo sem faixas de vídeo ou áudio"}
	}
	remux := "remux"
	if mi.VideoIndex >= 0 && !copyableToMP4(mi.VideoCodec) {
		remux = "transcode" // ex.: VP8 não entra em MP4
	}
	if ok, why := containerSupported(mi, full, c); !ok {
		return Playback{remux, why}
	}
	if a := defaultAudioTrack(mi); a != nil && !audioPlayable(a.Codec, c) {
		return Playback{remux, fmt.Sprintf("áudio %s não é suportado pelo navegador", strings.ToUpper(a.Codec))}
	}
	return Playback{"direct", "o navegador toca o arquivo original"}
}

// applyCaps preenche Playable e Playback de acordo com o navegador que pediu.
func applyCaps(mi *MediaInfo, c Caps, full string) {
	for i := range mi.Audio {
		mi.Audio[i].Playable = audioPlayable(mi.Audio[i].Codec, c)
	}
	pb := decidePlayback(mi, c, full)
	mi.Playback = &pb
}

// needsRemux diz se streamHandler deve passar pelo ffmpeg em vez de servir o arquivo cru.
func needsRemux(r *http.Request) bool {
	q := r.URL.Query()
	return q.Get("mode") != "" || q.Get("audio") != "" || q.Get("start") != ""
}

func acquireSlot(ctx context.Context, slots chan struct{}) bool {
	select {
	case slots <- struct{}{}:
		return true
	default:
	}
	t := time.NewTimer(slotWait)
	defer t.Stop()
	select {
	case slots <- struct{}{}:
		return true
	case <-t.C:
	case <-ctx.Done():
	}
	return false
}

// streamProcessed entrega MP4 fragmentado gerado pelo ffmpeg. O seek é feito pelo cliente pedindo
// outro ?start=; Range só serve para o <video> retomar o mesmo stream (ver media_stream_cache.go).
// mode=remux copia o vídeo; mode=transcode recodifica para H.264.
func streamProcessed(w http.ResponseWriter, r *http.Request, full string, info os.FileInfo) {
	q := r.URL.Query()
	mode := defaultStr(q.Get("mode"), "remux")
	if mode != "remux" && mode != "transcode" {
		http.Error(w, `{"error":"Invalid mode"}`, http.StatusBadRequest)
		return
	}
	audioOrder, err := strconv.Atoi(defaultStr(q.Get("audio"), "-1"))
	if err != nil || audioOrder < -1 || (q.Get("audio") != "" && audioOrder < 0) {
		http.Error(w, `{"error":"Invalid audio track"}`, http.StatusBadRequest)
		return
	}
	start, err := strconv.ParseFloat(defaultStr(q.Get("start"), "0"), 64)
	if err != nil || start < 0 || math.IsInf(start, 0) || math.IsNaN(start) {
		http.Error(w, `{"error":"Invalid start"}`, http.StatusBadRequest)
		return
	}
	delayMs, err := strconv.Atoi(defaultStr(q.Get("adelay"), "0"))
	if err != nil || delayMs < -maxAudioDelayMs || delayMs > maxAudioDelayMs {
		http.Error(w, `{"error":"Invalid audio delay"}`, http.StatusBadRequest)
		return
	}
	delay := float64(delayMs) / 1000

	mi, err := probeMedia(r.Context(), full, info)
	if err != nil {
		http.Error(w, `{"error":"Failed to probe media"}`, http.StatusUnprocessableEntity)
		return
	}
	if audioOrder == -1 { // sem ?audio=: a faixa que o arquivo toca por padrão
		if a := defaultAudioTrack(mi); a != nil {
			audioOrder = a.Order
		}
	} else if audioOrder >= len(mi.Audio) {
		http.Error(w, `{"error":"Audio track not found"}`, http.StatusNotFound)
		return
	}
	if mi.Duration > 0 && start >= mi.Duration {
		http.Error(w, `{"error":"Invalid start"}`, http.StatusBadRequest)
		return
	}
	if mi.VideoIndex < 0 && len(mi.Audio) == 0 {
		http.Error(w, `{"error":"No playable streams"}`, http.StatusUnprocessableEntity)
		return
	}

	if mode == "remux" && mi.VideoIndex >= 0 && !copyableToMP4(mi.VideoCodec) {
		mode = "transcode" // ex.: VP8 não entra em MP4
	}
	slots, args := remuxSlots, remuxArgs(full, start, audioOrder, mi, delay)
	if mode == "transcode" {
		slots, args = transcodeSlots, transcodeArgs(full, start, audioOrder, mi, delay)
	}
	serveProc(w, r, procKey(full, info, mode, audioOrder, start, delayMs), full, mode, slots, args)
}

// Linha do tempo dos streams processados (remux/transcode), medida no Chrome com o fixture de sincronia.
//
// Sem cuidado, o MP4 gerado a partir de ?start= sai dessincronizado:
//   - com vídeo copiado, o -ss do ffmpeg recua até um GOP antes do pedido (heurística de B-frames) e
//     mantém esses pacotes, enquanto o áudio é cortado exatamente no ponto: o Chrome junta o buraco do
//     começo do áudio e o som fica um GOP (segundos!) adiantado em relação à imagem;
//   - o atraso de reordenação dos B-frames (e o priming do AAC) leva o muxer a deslocar todo o
//     stream em 80-170 ms, e o relógio do player (legendas) fica errado nesse tanto.
//
// Por isso todo stream processado tem a mesma forma, independente de codec e contêiner:
//   - o vídeo copiado descarta o que vem antes do ponto pedido (-copypriorss 0) e começa no primeiro
//     keyframe >= start (o cliente pede start = keyframe, ver keyframeHandler);
//   - o áudio é sempre reencodado em AAC com streamLead segundos de silêncio antes do ponto pedido
//     (aresample first_pts). Áudio contínuo desde 0 não deixa o Chrome "puxar" o som, e como o
//     silêncio é maior que qualquer atraso de B-frame, o deslocamento do muxer é exatamente streamLead;
//   - resultado: tempo no stream = tempo do arquivo - start + streamLead, para vídeo, áudio e legenda.
//     O cliente usa offset = start - streamLead (+ ajuste manual de áudio, ver timingArgs).
//
// "Tempo do arquivo" = pts cru - max(start_time, 0): o mesmo currentTime que o Chrome mostra tocando o
// arquivo cru (direct), e a mesma origem das legendas extraídas (subtitleHandler).
const (
	streamLead       = 2.0
	maxAudioDelayMs  = 1500 // ajuste manual do áudio (?adelay=, ms); precisa ficar abaixo de streamLead - atraso de B-frames
	aacEncoderDelay  = 1024 // amostras de priming do encoder AAC do ffmpeg
	streamSampleRate = 48000
)

// fileOrigin: pts cru que corresponde ao tempo 0 do arquivo.
func fileOrigin(mi *MediaInfo) float64 {
	return math.Max(mi.StartTime, 0)
}

// seekArgs: o -ss de entrada é relativo ao start_time do arquivo; converte o tempo do arquivo para ele.
func seekArgs(full string, start float64, mi *MediaInfo) []string {
	ss := math.Max(0, start+fileOrigin(mi)-mi.StartTime)
	if ss == 0 {
		// sem -ss: o resultado é o mesmo (start_time 0), e alguns demuxers (AVI) falham ao "buscar" o 0
		// e perdem o começo do vídeo
		return []string{"-v", "error", "-nostdin", "-i", full}
	}
	return []string{"-v", "error", "-nostdin", "-ss", strconv.FormatFloat(ss, 'f', 6, 64), "-i", full}
}

func hasAudio(audioOrder int, mi *MediaInfo) bool { return audioOrder >= 0 && audioOrder < len(mi.Audio) }

// Vídeo sem áudio: o MP4 fragmentado zera o primeiro dts, então não dá para criar a folga de streamLead
// só com o vídeo (sobrava o atraso dos B-frames, ~83 ms). Uma faixa de silêncio gerada ancora o tempo
// exatamente como uma faixa de áudio real.
func silenceInput(audioOrder int, mi *MediaInfo, start float64) []string {
	if hasAudio(audioOrder, mi) || mi.VideoIndex < 0 {
		return nil
	}
	args := []string{"-f", "lavfi"}
	if mi.Duration > 0 {
		args = append(args, "-t", strconv.FormatFloat(math.Max(1, mi.Duration-start+1), 'f', 3, 64))
	}
	return append(args, "-i", fmt.Sprintf("anullsrc=r=%d:cl=stereo", streamSampleRate))
}

func mapArgs(audioOrder int, mi *MediaInfo) []string {
	var args []string
	if mi.VideoIndex >= 0 {
		args = append(args, "-map", "0:"+strconv.Itoa(mi.VideoIndex))
	}
	if hasAudio(audioOrder, mi) {
		args = append(args, "-map", "0:a:"+strconv.Itoa(audioOrder))
	} else if mi.VideoIndex >= 0 {
		args = append(args, "-map", "1:a")
	}
	return append(args, "-sn", "-dn", "-map_chapters", "-1")
}

// timingArgs alinha áudio e vídeo (ver streamLead). delay > 0 atrasa o som em relação à imagem: quem
// anda é o vídeo, porque o áudio define a linha do tempo e precisa começar em 0.
func timingArgs(audioOrder int, mi *MediaInfo, delay float64, copyVideo bool) []string {
	if !hasAudio(audioOrder, mi) && mi.VideoIndex < 0 {
		return nil
	}
	ch := 2 // silêncio gerado (silenceInput) é estéreo
	if hasAudio(audioOrder, mi) {
		if c := mi.Audio[audioOrder].Channels; c >= 1 && c <= 2 {
			ch = c
		}
	}
	firstPts := aacEncoderDelay - int(math.Round(streamLead*streamSampleRate))
	args := []string{
		"-af", fmt.Sprintf("aresample=%d:async=1:first_pts=%d", streamSampleRate, firstPts),
		"-c:a", "aac", "-b:a", "192k", "-ac", strconv.Itoa(ch),
	}
	// vídeo copiado: desloca os pacotes (base de tempo do arquivo, fina). Recodificado: o deslocamento vai
	// no filtro (ver transcodeArgs), porque a base do encoder é 1/fps e arredondaria para o quadro.
	if copyVideo && mi.VideoIndex >= 0 && delay != 0 {
		args = append(args, "-bsf:v", "setts=pts=PTS-("+delayExpr(delay)+")/TB:dts=DTS-("+delayExpr(delay)+")/TB")
	}
	return args
}

var fragmentedMP4 = []string{"-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1"}

func remuxArgs(full string, start float64, audioOrder int, mi *MediaInfo, delay float64) []string {
	args := append(append(seekArgs(full, start, mi), silenceInput(audioOrder, mi, start)...), mapArgs(audioOrder, mi)...)
	if mi.VideoIndex >= 0 {
		args = append(args, "-c:v", "copy", "-copypriorss", "0")
		if mi.VideoCodec == "hevc" {
			args = append(args, "-tag:v", "hvc1")
		}
	}
	args = append(args, timingArgs(audioOrder, mi, delay, true)...)
	return append(args, fragmentedMP4...)
}

// copyableToMP4: codecs de vídeo tocáveis que o remux pode copiar para MP4.
func copyableToMP4(codec string) bool {
	switch codec {
	case "h264", "hevc", "av1", "vp9":
		return true
	}
	return false
}

// transcodeArgs: H.264 8-bit 4:2:0 (toca em qualquer navegador), no máximo 1920 no lado maior,
// keyframe a cada 2s (fragmentos curtos = começa a tocar rápido) e áudio AAC.
func transcodeArgs(full string, start float64, audioOrder int, mi *MediaInfo, delay float64) []string {
	args := append(append(seekArgs(full, start, mi), silenceInput(audioOrder, mi, start)...), mapArgs(audioOrder, mi)...)
	if mi.VideoIndex >= 0 {
		args = append(args,
			"-vf", videoDelayFilter(delay)+"scale=w='min(iw,1920)':h='min(ih,1920)':force_original_aspect_ratio=decrease:force_divisible_by=2,format=yuv420p",
			// base de tempo fina no encoder: com a padrão (1/fps) os quadros caem na grade de 1/fps contada
			// do início do stream e um seek fora da grade erra até meio quadro (~21 ms a 24 fps)
			"-enc_time_base:v", "1:90000",
			"-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-profile:v", "high",
			"-force_key_frames", "expr:gte(t,n_forced*2)")
	}
	args = append(args, timingArgs(audioOrder, mi, delay, false)...)
	return append(args, fragmentedMP4...)
}

func delayExpr(delay float64) string { return strconv.FormatFloat(delay, 'f', 3, 64) }

func videoDelayFilter(delay float64) string {
	if delay == 0 {
		return ""
	}
	return "setpts=PTS-(" + delayExpr(delay) + ")/TB,"
}

// keyframeHandler devolve o keyframe de vídeo em ou antes de ?t= (tempo do arquivo, segundos). O remux
// copia o vídeo e a imagem só começa num keyframe: o cliente pede start=<keyframe> e o stream tem
// imagem desde o primeiro instante.
func keyframeHandler(w http.ResponseWriter, r *http.Request) {
	full, info, err := resolveMediaFile(mux.Vars(r)["path"])
	if err != nil {
		writeMediaError(w, err)
		return
	}
	t, err := strconv.ParseFloat(r.URL.Query().Get("t"), 64)
	if err != nil || t < 0 || math.IsInf(t, 0) || math.IsNaN(t) {
		http.Error(w, `{"error":"Invalid time"}`, http.StatusBadRequest)
		return
	}
	mi, err := probeMedia(r.Context(), full, info)
	if err != nil {
		http.Error(w, `{"error":"Failed to probe media"}`, http.StatusUnprocessableEntity)
		return
	}
	kf := t
	if mi.VideoIndex >= 0 && t > 0 {
		origin := fileOrigin(mi)
		if found, ok := findKeyframe(r.Context(), full, mi.VideoIndex, t+origin); ok {
			// arredonda para baixo (com folga de 0,5 ms): o -copypriorss 0 descarta o que vem antes de
			// start, então start não pode passar do pts do keyframe nem por arredondamento
			kf = math.Max(0, math.Floor((found-origin)*1000-0.5)/1000)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]float64{"time": kf})
}

// findKeyframe devolve o pts cru do último keyframe <= raw. O seek do ffprobe pode cair alguns keyframes
// antes do ponto (ex.: 5 s antes em mp4, 3 s num mkv real), então lê todos os pacotes até raw; se não
// achar keyframe (seek caiu depois do ponto), repete com uma janela de 30 s para trás.
func findKeyframe(ctx context.Context, full string, videoIndex int, raw float64) (float64, bool) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for _, from := range []float64{raw, math.Max(0, raw-30)} {
		interval := strconv.FormatFloat(from, 'f', 6, 64) + "%" + strconv.FormatFloat(raw+0.001, 'f', 6, 64)
		out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", strconv.Itoa(videoIndex),
			"-read_intervals", interval, "-show_entries", "packet=pts_time,flags", "-of", "csv=p=0", full).Output()
		if err != nil {
			return 0, false
		}
		if kf, ok := lastKeyframe(string(out), raw); ok {
			return kf, true
		}
	}
	return 0, false
}

// lastKeyframe lê a saída csv "pts_time,flags" do ffprobe e devolve o maior pts de keyframe <= raw.
func lastKeyframe(csv string, raw float64) (float64, bool) {
	best, found := 0.0, false
	for _, line := range strings.Split(csv, "\n") {
		parts := strings.Split(strings.TrimSpace(line), ",")
		if len(parts) < 2 || !strings.HasPrefix(parts[1], "K") {
			continue
		}
		if v, err := strconv.ParseFloat(parts[0], 64); err == nil && v <= raw+0.0005 && (!found || v > best) {
			best, found = v, true
		}
	}
	return best, found
}

// mediaCacheDir é a pasta de cache de um arquivo (muda se o arquivo mudar).
func mediaCacheDir(full string, info os.FileInfo) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", full, info.ModTime().UnixNano(), info.Size())))
	return filepath.Join(MEDIA_CACHE_DIR, hex.EncodeToString(sum[:10]))
}

func lockFor(key string) *sync.Mutex {
	m, _ := cacheLocks.LoadOrStore(key, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// cachedExtract roda o ffmpeg (saída em pipe:1) uma vez e guarda o resultado em disco.
func cachedExtract(ctx context.Context, target string, args []string) error {
	if _, err := os.Stat(target); err == nil {
		return nil
	}
	l := lockFor(target)
	l.Lock()
	defer l.Unlock()
	if _, err := os.Stat(target); err == nil {
		return nil
	}
	if !acquireSlot(ctx, extractSlots) {
		return fmt.Errorf("extraction busy")
	}
	defer func() { <-extractSlots }()

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.WaitDelay = 5 * time.Second
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = f, &stderr
	runErr := cmd.Run()
	f.Close()
	if runErr != nil {
		os.Remove(tmp)
		return fmt.Errorf("%w: %s", runErr, strings.TrimSpace(stderr.String()))
	}
	return os.Rename(tmp, target)
}

// subtitleHandler entrega uma legenda (embutida ou ao lado do vídeo).
// ?id=emb:<índice>|ext:<arquivo>  &format=vtt (padrão, texto/ASS) | ass (cru, p/ JASSUB) | sup (PGS, p/ libpgs)
func subtitleHandler(w http.ResponseWriter, r *http.Request) {
	full, info, err := resolveMediaFile(mux.Vars(r)["path"])
	if err != nil {
		writeMediaError(w, err)
		return
	}
	format := defaultStr(r.URL.Query().Get("format"), "vtt")
	contentType := map[string]string{"vtt": "text/vtt; charset=utf-8", "ass": "text/plain; charset=utf-8", "sup": "application/octet-stream"}[format]
	if contentType == "" {
		http.Error(w, `{"error":"Invalid format"}`, http.StatusBadRequest)
		return
	}

	id := r.URL.Query().Get("id")
	var src string // arquivo de entrada do ffmpeg
	var mapArg []string
	var kind string
	var mi *MediaInfo
	switch {
	case strings.HasPrefix(id, "emb:"):
		idx, convErr := strconv.Atoi(strings.TrimPrefix(id, "emb:"))
		if convErr != nil || idx < 0 {
			http.Error(w, `{"error":"Invalid subtitle id"}`, http.StatusBadRequest)
			return
		}
		var probeErr error
		mi, probeErr = probeMedia(r.Context(), full, info)
		if probeErr != nil {
			http.Error(w, `{"error":"Subtitle not supported"}`, http.StatusUnprocessableEntity)
			return
		}
		kind = subtitleKindByID(mi, id)
		src, mapArg = full, []string{"-map", "0:" + strconv.Itoa(idx)}
	case strings.HasPrefix(id, "ext:"):
		name := strings.TrimPrefix(id, "ext:")
		if name != filepath.Base(name) || !externalSubExts[strings.ToLower(filepath.Ext(name))] {
			http.Error(w, `{"error":"Invalid subtitle id"}`, http.StatusBadRequest)
			return
		}
		src = filepath.Join(filepath.Dir(full), name)
		if _, statErr := os.Stat(src); statErr != nil {
			http.Error(w, `{"error":"Subtitle not found"}`, http.StatusNotFound)
			return
		}
		kind = subtitleKind(strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), "."))
		if format == "ass" && kind == "ass" { // .ass externo vai cru
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Cache-Control", "private, max-age=3600")
			http.ServeFile(w, r, src)
			return
		}
	default:
		http.Error(w, `{"error":"Invalid subtitle id"}`, http.StatusBadRequest)
		return
	}

	// vtt serve texto e ASS (fallback); ass só ASS; sup só PGS
	allowed := (format == "vtt" && (kind == "text" || kind == "ass")) || (format == "ass" && kind == "ass") || (format == "sup" && kind == "pgs")
	if !allowed {
		http.Error(w, `{"error":"Subtitle not supported"}`, http.StatusUnprocessableEntity)
		return
	}
	args := []string{"-v", "error", "-nostdin"}
	if src == full && mi != nil && mi.StartTime <= 0 {
		// sem -copyts o ffmpeg subtrai o start_time (ex.: -0,021 s em mkv com AAC) e a legenda sai
		// atrasada; com -copyts fica no pts cru, que é o tempo do arquivo quando start_time <= 0.
		// Com start_time > 0 (MPEG-TS) a subtração é justamente a origem do tempo do arquivo.
		args = append(args, "-copyts")
	}
	args = append(append(args, "-i", src), mapArg...)
	switch format {
	case "vtt":
		args = append(args, "-f", "webvtt", "pipe:1")
	case "ass":
		args = append(args, "-c", "copy", "-f", "ass", "pipe:1")
	case "sup":
		args = append(args, "-c", "copy", "-f", "sup", "pipe:1")
	}

	target := filepath.Join(mediaCacheDir(full, info), "sub-"+sanitizeCacheName(id)+"."+format)
	if src == full && format == "vtt" && kind == "text" {
		// Uma passada só para todas as faixas de texto: num mkv grande o ffmpeg lê o arquivo
		// inteiro de qualquer jeito. Fora do contexto da requisição, para terminar e ficar no
		// cache mesmo se o player desistir de esperar.
		extractTextSubs(full, info, mi.StartTime, mi.Subtitles)
	}
	if err := cachedExtract(r.Context(), target, args); err != nil {
		logWarn("subtitle extraction failed for %s (%s, %s): %v", full, id, format, err)
		http.Error(w, `{"error":"Failed to convert subtitle"}`, http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, target)
}

// extractTextSubs converte para WebVTT, numa execução do ffmpeg, toda legenda de texto
// embutida que ainda não está no cache. Bloqueia até terminar; chamadas simultâneas esperam a primeira.
func extractTextSubs(full string, info os.FileInfo, startTime float64, subs []SubtitleTrack) {
	dir := mediaCacheDir(full, info)
	l := lockFor(dir + "|subs")
	l.Lock()
	defer l.Unlock()
	args := []string{"-v", "error", "-nostdin", "-y"}
	if startTime <= 0 {
		args = append(args, "-copyts") // mesmo critério do subtitleHandler
	}
	args = append(args, "-i", full)
	var outs []string
	for _, s := range subs {
		if s.External || s.Kind != "text" {
			continue
		}
		target := filepath.Join(dir, "sub-"+sanitizeCacheName(s.ID)+".vtt")
		if _, err := os.Stat(target); err == nil {
			continue
		}
		outs = append(outs, target)
		args = append(args, "-map", "0:"+strings.TrimPrefix(s.ID, "emb:"), "-f", "webvtt", target+".tmp")
	}
	if len(outs) == 0 || os.MkdirAll(dir, 0o755) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if !acquireSlot(ctx, extractSlots) {
		return
	}
	defer func() { <-extractSlots }()
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.WaitDelay = 5 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	for _, t := range outs {
		if err == nil {
			os.Rename(t+".tmp", t)
		} else {
			os.Remove(t + ".tmp")
		}
	}
	if err != nil {
		logWarn("subtitle batch extraction failed for %s: %v: %s", full, err, strings.TrimSpace(stderr.String()))
	}
}

// prefetchTextSubs adianta, em segundo plano, a extração das legendas de texto embutidas.
func prefetchTextSubs(full string, info os.FileInfo, mi *MediaInfo) {
	for _, s := range mi.Subtitles {
		if !s.External && s.Kind == "text" {
			go extractTextSubs(full, info, mi.StartTime, append([]SubtitleTrack(nil), mi.Subtitles...))
			return
		}
	}
}

func subtitleKindByID(mi *MediaInfo, id string) string {
	for _, s := range mi.Subtitles {
		if s.ID == id {
			return s.Kind
		}
	}
	return ""
}

func sanitizeCacheName(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// mediaFontHandler entrega uma fonte anexada ao mkv (?i=<índice do stream>), usada pelo JASSUB.
// Na primeira chamada todas as fontes do arquivo são extraídas de uma vez para o cache em disco.
func mediaFontHandler(w http.ResponseWriter, r *http.Request) {
	full, info, err := resolveMediaFile(mux.Vars(r)["path"])
	if err != nil {
		writeMediaError(w, err)
		return
	}
	idx, err := strconv.Atoi(r.URL.Query().Get("i"))
	if err != nil || idx < 0 {
		http.Error(w, `{"error":"Invalid font"}`, http.StatusBadRequest)
		return
	}
	mi, err := probeMedia(r.Context(), full, info)
	if err != nil {
		http.Error(w, `{"error":"Failed to probe media"}`, http.StatusUnprocessableEntity)
		return
	}
	var font *FontAttachment
	for i := range mi.Fonts {
		if mi.Fonts[i].Index == idx {
			font = &mi.Fonts[i]
		}
	}
	if font == nil {
		http.Error(w, `{"error":"Font not found"}`, http.StatusNotFound)
		return
	}
	dir := filepath.Join(mediaCacheDir(full, info), "fonts")
	target := filepath.Join(dir, strconv.Itoa(idx)+".font")
	if err := dumpFonts(r.Context(), full, dir, mi.Fonts); err != nil {
		logWarn("font extraction failed for %s: %v", full, err)
		http.Error(w, `{"error":"Failed to extract font"}`, http.StatusUnprocessableEntity)
		return
	}
	if _, err := os.Stat(target); err != nil {
		http.Error(w, `{"error":"Font not found"}`, http.StatusNotFound)
		return
	}
	ct := "application/octet-stream"
	switch strings.ToLower(filepath.Ext(font.Name)) {
	case ".ttf", ".ttc":
		ct = "font/ttf"
	case ".otf", ".otc":
		ct = "font/otf"
	case ".woff":
		ct = "font/woff"
	case ".woff2":
		ct = "font/woff2"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, target)
}

// dumpFonts extrai todos os anexos de fonte numa pasta temporária e a renomeia (atômico).
func dumpFonts(ctx context.Context, full, dir string, fonts []FontAttachment) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	l := lockFor(dir)
	l.Lock()
	defer l.Unlock()
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), "fonts-tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	args := []string{"-v", "error", "-nostdin", "-y"}
	for _, f := range fonts {
		args = append(args, "-dump_attachment:"+strconv.Itoa(f.Index), filepath.Join(tmp, strconv.Itoa(f.Index)+".font"))
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// sem arquivo de saída o ffmpeg sai com erro, mas os anexos já foram gravados: confere os arquivos
	exec.CommandContext(ctx, "ffmpeg", append(args, "-i", full)...).Run()
	for _, f := range fonts {
		if _, err := os.Stat(filepath.Join(tmp, strconv.Itoa(f.Index)+".font")); err != nil {
			return fmt.Errorf("attachment %d not dumped", f.Index)
		}
	}
	return os.Rename(tmp, dir)
}
