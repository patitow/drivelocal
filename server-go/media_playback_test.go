package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

func TestDecidePlaybackRules(t *testing.T) {
	type tc struct {
		name, container, file, vcodec, pix string
		audio                              []string
		caps                               string
		want                               string
	}
	cases := []tc{
		{"h264 mp4", "mov,mp4,m4a,3gp,3g2,mj2", "a.mp4", "h264", "yuv420p", []string{"aac"}, "", "direct"},
		{"h264 mkv", "matroska,webm", "a.mkv", "h264", "yuv420p", []string{"aac"}, "", "direct"},
		{"mkv sem caps mkv", "matroska,webm", "a.mkv", "h264", "yuv420p", []string{"aac"}, "h264", "remux"},
		{"webm sem caps mkv", "matroska,webm", "a.webm", "vp9", "yuv420p", []string{"opus"}, "vp9", "direct"},
		{"hi10p padrão", "matroska,webm", "a.mkv", "h264", "yuv420p10le", []string{"aac"}, "", "transcode"},
		{"hi10p chrome", "matroska,webm", "a.mkv", "h264", "yuv420p10le", []string{"aac"}, chromeCaps, "direct"},
		{"hevc padrão", "matroska,webm", "a.mkv", "hevc", "yuv420p", []string{"aac"}, "", "transcode"},
		{"hevc 8bit com cap só hevc", "matroska,webm", "a.mkv", "hevc", "yuv420p", []string{"aac"}, "hevc,mkv", "direct"},
		{"hevc10 só cap hevc", "matroska,webm", "a.mkv", "hevc", "yuv420p10le", []string{"aac"}, "hevc,mkv", "transcode"},
		{"hevc10 eac3 chrome", "matroska,webm", "a.mkv", "hevc", "yuv420p10le", []string{"eac3", "aac"}, chromeCaps, "remux"},
		{"eac3 com cap eac3", "matroska,webm", "a.mkv", "h264", "yuv420p", []string{"eac3"}, "eac3,mkv", "direct"},
		{"ac3", "matroska,webm", "a.mkv", "h264", "yuv420p", []string{"ac3"}, chromeCaps, "remux"},
		{"truehd", "matroska,webm", "a.mkv", "h264", "yuv420p", []string{"truehd"}, chromeCaps, "remux"},
		{"avi h264", "avi", "a.avi", "h264", "yuv420p", []string{"mp3"}, chromeCaps, "remux"},
		{"avi xvid", "avi", "a.avi", "mpeg4", "yuv420p", []string{"mp3"}, chromeCaps, "transcode"},
		{"mpegts h264", "mpegts", "a.ts", "h264", "yuv420p", []string{"aac"}, chromeCaps, "remux"},
		{"mpeg2", "mpegts", "a.ts", "mpeg2video", "yuv420p", []string{"mp2"}, chromeCaps, "transcode"},
		{"vc1", "asf", "a.wmv", "vc1", "yuv420p", []string{"wmav2"}, chromeCaps, "transcode"},
		{"4:2:2", "matroska,webm", "a.mkv", "h264", "yuv422p", []string{"aac"}, chromeCaps, "transcode"},
		{"av1 sem cap", "matroska,webm", "a.mkv", "av1", "yuv420p", []string{"opus"}, "mkv", "transcode"},
		{"sem áudio", "mov,mp4,m4a,3gp,3g2,mj2", "a.mp4", "h264", "yuv420p", nil, "", "direct"},
	}
	for _, c := range cases {
		mi := &MediaInfo{Container: c.container, VideoIndex: 0, VideoCodec: c.vcodec, PixFmt: c.pix}
		for i, a := range c.audio {
			mi.Audio = append(mi.Audio, AudioTrack{Order: i, Codec: a, Default: i == 0})
		}
		got := decidePlayback(mi, parseCaps(c.caps), c.file)
		if got.Mode != c.want || got.Reason == "" {
			t.Errorf("%-24s got %s (%q), want %s", c.name, got.Mode, got.Reason, c.want)
		}
	}
}

// testRouter expõe as rotas de mídia sem auth (o middleware de auth tem testes próprios).
func testRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/api/stream/{path:.*}", streamHandler)
	r.HandleFunc("/api/media-info/{path:.*}", mediaInfoHandler)
	r.HandleFunc("/api/subtitle/{path:.*}", subtitleHandler)
	r.HandleFunc("/api/media-font/{path:.*}", mediaFontHandler)
	r.HandleFunc("/api/keyframe/{path:.*}", keyframeHandler)
	return r
}

func withMediaEnv(t *testing.T) string {
	t.Helper()
	origMedia, origCache := MEDIA_PATH, MEDIA_CACHE_DIR
	MEDIA_PATH, MEDIA_CACHE_DIR = t.TempDir(), t.TempDir()
	t.Cleanup(func() { MEDIA_PATH, MEDIA_CACHE_DIR = origMedia, origCache })
	return MEDIA_PATH
}

type probed struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		PixFmt    string `json:"pix_fmt"`
		Width     int    `json:"width"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
}

func ffprobeFile(t *testing.T, path string) probed {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", "-show_format", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	var p probed
	json.Unmarshal(out, &p)
	return p
}

// fetchStream baixa /api/stream com os parâmetros e devolve o corpo (mp4 fragmentado).
func fetchStream(t *testing.T, srv *httptest.Server, name, query string) []byte {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/stream/" + url.PathEscape(name) + "?" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("%s?%s: status %d %s", name, query, resp.StatusCode, body)
	}
	return body
}

// checkBrowserMP4 confere que a saída é MP4 fragmentado com vídeo/áudio que qualquer navegador toca.
func checkBrowserMP4(t *testing.T, body []byte, wantVideo string, wantAudio bool, minDur float64) {
	checkMP4(t, body, wantVideo, wantVideo == "h264", wantAudio, minDur)
}

func checkMP4(t *testing.T, body []byte, wantVideo string, want420 bool, wantAudio bool, minDur float64) {
	t.Helper()
	if len(body) < 12 || string(body[4:8]) != "ftyp" || !bytes.Contains(body, []byte("moof")) {
		t.Fatalf("output is not fragmented mp4 (%d bytes)", len(body))
	}
	f := filepath.Join(t.TempDir(), "out.mp4")
	os.WriteFile(f, body, 0o644)
	p := ffprobeFile(t, f)
	var v, a string
	var pix string
	for _, s := range p.Streams {
		switch s.CodecType {
		case "video":
			v, pix = s.CodecName, s.PixFmt
		case "audio":
			a = s.CodecName
		}
	}
	if v != wantVideo {
		t.Errorf("video codec %q, want %q", v, wantVideo)
	}
	if want420 && pix != "yuv420p" {
		t.Errorf("pix_fmt %q, want yuv420p", pix)
	}
	// todo stream processado leva AAC: vídeo sem áudio ganha uma faixa de silêncio que ancora o tempo
	// (ver silenceInput); wantAudio fica só como documentação de que o original tinha som
	_ = wantAudio
	if a != "aac" {
		t.Errorf("audio codec %q, want aac", a)
	}
	if d, _ := strconv.ParseFloat(p.Format.Duration, 64); d < minDur {
		t.Errorf("duration %.2f < %.2f", d, minDur)
	}
}

// Matriz: codec de vídeo × áudio × contêiner × nº de faixas × legendas. Para cada arquivo confere a
// decisão (caps padrão e caps do Chrome) e passa a saída do modo escolhido e do transcode pelo ffprobe.
func TestPlaybackFixtureMatrix(t *testing.T) {
	requireFFmpeg(t)
	dir := withMediaEnv(t)
	srv := httptest.NewServer(testRouter())
	defer srv.Close()

	x264 := "libx264"
	matrix := []fxSpec{
		{name: "h264_aac.mp4", video: x264, audio: []string{"aac"}, wantDf: "direct", wantCh: "direct"},
		{name: "h264_2aac_srt_ass_pgs_font.mkv", video: x264, audio: []string{"aac", "aac"}, subs: []string{"srt", "ass", "pgs"}, font: true, wantDf: "direct", wantCh: "direct"},
		{name: "h264_aac_movtext.mp4", video: x264, audio: []string{"aac"}, subs: []string{"srt"}, wantDf: "direct", wantCh: "direct"},
		{name: "h264hi10_aac.mkv", video: x264, vopts: []string{"-pix_fmt", "yuv420p10le"}, audio: []string{"aac"}, wantDf: "transcode", wantCh: "direct"},
		{name: "h264_422_aac.mkv", video: x264, vopts: []string{"-pix_fmt", "yuv422p"}, audio: []string{"aac"}, wantDf: "transcode", wantCh: "transcode"},
		{name: "hevc_aac.mp4", video: "libx265", vopts: []string{"-tag:v", "hvc1", "-x265-params", "log-level=error"}, audio: []string{"aac"}, wantDf: "transcode", wantCh: "direct"},
		{name: "hevc10_eac3_aac_ass.mkv", video: "libx265", vopts: []string{"-pix_fmt", "yuv420p10le", "-x265-params", "log-level=error"}, audio: []string{"eac3", "aac"}, subs: []string{"ass"}, wantDf: "transcode", wantCh: "remux"},
		{name: "h264_ac3.mkv", video: x264, audio: []string{"ac3"}, wantDf: "remux", wantCh: "remux"},
		{name: "h264_dts_aac.mkv", video: x264, audio: []string{"dca", "aac"}, wantDf: "remux", wantCh: "remux"},
		{name: "h264_flac.mkv", video: x264, audio: []string{"flac"}, wantDf: "direct", wantCh: "direct"},
		{name: "h264_opus.mp4", video: x264, audio: []string{"libopus"}, wantDf: "direct", wantCh: "direct"},
		{name: "h264_pcm.mov", video: x264, audio: []string{"pcm_s16le"}, wantDf: "remux", wantCh: "remux"},
		{name: "h264_mp3.avi", video: x264, audio: []string{"libmp3lame"}, wantDf: "remux", wantCh: "remux"},
		{name: "mpeg4_mp3.avi", video: "mpeg4", audio: []string{"libmp3lame"}, wantDf: "transcode", wantCh: "transcode"},
		{name: "mpeg4_aac.mp4", video: "mpeg4", audio: []string{"aac"}, wantDf: "transcode", wantCh: "transcode"},
		{name: "h264_aac.ts", video: x264, audio: []string{"aac"}, wantDf: "remux", wantCh: "remux"},
		{name: "mpeg2_mp2.ts", video: "mpeg2video", audio: []string{"mp2"}, wantDf: "transcode", wantCh: "transcode"},
		{name: "wmv2_wmav2.wmv", video: "wmv2", audio: []string{"wmav2"}, wantDf: "transcode", wantCh: "transcode"},
		{name: "vp9_opus.webm", video: "libvpx-vp9", vopts: []string{"-deadline", "realtime", "-cpu-used", "8"}, audio: []string{"libopus"}, wantDf: "direct", wantCh: "direct"},
		{name: "vp8_vorbis.webm", video: "libvpx", vopts: []string{"-deadline", "realtime", "-cpu-used", "8"}, audio: []string{"libvorbis"}, wantDf: "direct", wantCh: "direct"},
		{name: "av1_opus.mkv", video: "libaom-av1", vopts: []string{"-cpu-used", "8", "-row-mt", "1", "-b:v", "200k"}, audio: []string{"libopus"}, wantDf: "direct", wantCh: "direct"},
		{name: "h264_noaudio.mp4", video: x264, wantDf: "direct", wantCh: "direct"},
		{name: "cover_h264_aac.mp4", video: x264, audio: []string{"aac"}, cover: true, wantDf: "direct", wantCh: "direct"},
	}

	t.Logf("%-32s %-10s %-9s %-9s %s", "arquivo", "vídeo", "padrão", "chrome", "motivo (padrão)")
	for _, f := range matrix {
		f := f
		t.Run(f.name, func(t *testing.T) {
			full := buildFixture(t, dir, f)
			info, _ := os.Stat(full)
			mi, err := probeMedia(context.Background(), full, info)
			if err != nil {
				t.Fatal(err)
			}
			if mi.VideoIndex < 0 || mi.VideoCodec == "png" {
				t.Fatalf("main video not found: %+v", mi)
			}
			df := decidePlayback(mi, parseCaps(""), full)
			ch := decidePlayback(mi, parseCaps(chromeCaps), full)
			t.Logf("%-32s %-10s %-9s %-9s %s", f.name, mi.VideoCodec+"/"+mi.PixFmt, df.Mode, ch.Mode, df.Reason)
			if df.Mode != f.wantDf || ch.Mode != f.wantCh {
				t.Fatalf("decision default=%s (%s) chrome=%s (%s); want %s/%s", df.Mode, df.Reason, ch.Mode, ch.Reason, f.wantDf, f.wantCh)
			}

			// media-info expõe playback para o cliente
			resp, err := http.Get(srv.URL + "/api/media-info/" + url.PathEscape(f.name) + "?caps=" + chromeCaps)
			if err != nil {
				t.Fatal(err)
			}
			var got MediaInfo
			json.NewDecoder(resp.Body).Decode(&got)
			resp.Body.Close()
			if got.Playback == nil || got.Playback.Mode != f.wantCh {
				t.Fatalf("media-info playback = %+v", got.Playback)
			}

			hasAudio := len(f.audio) > 0
			// transcode funciona para qualquer arquivo (último recurso do cliente)
			checkBrowserMP4(t, fetchStream(t, srv, f.name, "mode=transcode"), "h264", hasAudio, 2.5)
			// remux: vídeo copiado; só vale quando o codec de vídeo é tocável
			// (VP8 não cabe em MP4: o servidor troca para transcode sozinho)
			remuxVideo := mi.VideoCodec
			if !copyableToMP4(mi.VideoCodec) {
				remuxVideo = "h264"
			}
			if ok, _ := videoSupported(mi, parseCaps(chromeCaps)); ok {
				checkMP4(t, fetchStream(t, srv, f.name, "mode=remux"), remuxVideo, false, hasAudio, 2.5)
			}
			if len(f.audio) > 1 { // troca de faixa: a 2ª faixa via remux
				checkMP4(t, fetchStream(t, srv, f.name, "mode=remux&audio=1"), remuxVideo, false, true, 2.5)
			}
		})
	}
}

func TestTranscodeSeekAndScale(t *testing.T) {
	requireFFmpeg(t)
	dir := withMediaEnv(t)
	srv := httptest.NewServer(testRouter())
	defer srv.Close()
	full := filepath.Join(dir, "big.mkv")
	runFF(t, "-f", "lavfi", "-i", "testsrc2=d=6:s=2560x1440:r=24", "-f", "lavfi", "-i", "sine=d=6", "-c:v", "libx265",
		"-preset", "ultrafast", "-x265-params", "log-level=error", "-c:a", "ac3", full)

	body := fetchStream(t, srv, "big.mkv", "mode=transcode&start=4")
	f := filepath.Join(t.TempDir(), "o.mp4")
	os.WriteFile(f, body, 0o644)
	p := ffprobeFile(t, f)
	d, _ := strconv.ParseFloat(p.Format.Duration, 64)
	// 2 s de conteúdo + streamLead (2 s de silêncio/folga antes do ponto pedido)
	if d < 3.5 || d > 4.5 {
		t.Errorf("seek start=4 of 6s should give ~2s + streamLead, got %.2f", d)
	}
	for _, s := range p.Streams {
		if s.CodecType == "video" && (s.CodecName != "h264" || s.Width != 1920) {
			t.Errorf("want h264 scaled to 1920 wide, got %s %d", s.CodecName, s.Width)
		}
	}
}

func TestKeyframeEndpoint(t *testing.T) {
	requireFFmpeg(t)
	dir := withMediaEnv(t)
	srv := httptest.NewServer(testRouter())
	defer srv.Close()
	// keyframe a cada 2s (48 quadros a 24fps)
	runFF(t, "-f", "lavfi", "-i", "testsrc2=d=8:s=160x120:r=24", "-c:v", "libx264", "-g", "48", "-keyint_min", "48", "-sc_threshold", "0", filepath.Join(dir, "k.mkv"))
	for _, c := range []struct{ t, want float64 }{{0, 0}, {3.1, 2}, {4, 4}, {7.9, 6}} {
		resp, err := http.Get(srv.URL + "/api/keyframe/k.mkv?t=" + strconv.FormatFloat(c.t, 'f', 2, 64))
		if err != nil {
			t.Fatal(err)
		}
		var out struct{ Time float64 }
		json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if out.Time < c.want-0.01 || out.Time > c.want+0.01 {
			t.Errorf("keyframe(%.1f) = %.3f, want %.1f", c.t, out.Time, c.want)
		}
	}
	resp, _ := http.Get(srv.URL + "/api/keyframe/k.mkv?t=abc")
	if resp.StatusCode != 400 {
		t.Errorf("bad t: %d", resp.StatusCode)
	}
}

func TestSubtitleAndFontEndpoints(t *testing.T) {
	requireFFmpeg(t)
	dir := withMediaEnv(t)
	srv := httptest.NewServer(testRouter())
	defer srv.Close()
	f := fxSpec{name: "subs.mkv", video: "libx264", audio: []string{"aac"}, subs: []string{"srt", "ass", "pgs"}, font: true}
	full := buildFixture(t, dir, f)
	os.WriteFile(filepath.Join(dir, "subs.en.ass"), []byte(fixtureASS), 0o644)
	info, _ := os.Stat(full)
	mi, _ := probeMedia(context.Background(), full, info)
	if len(mi.Fonts) != 1 || mi.Fonts[0].Name != "arial.ttf" {
		t.Fatalf("fonts: %+v", mi.Fonts)
	}
	kinds := map[string]string{}
	for _, s := range mi.Subtitles {
		kinds[s.Kind+"/"+strconv.FormatBool(s.External)] = s.ID
	}

	get := func(q string) (int, []byte) {
		resp, err := http.Get(srv.URL + q)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	cases := []struct {
		id, format string
		status     int
		prefix     string
	}{
		{kinds["text/false"], "vtt", 200, "WEBVTT"},
		{kinds["ass/false"], "ass", 200, "[Script Info]"},
		{kinds["ass/false"], "vtt", 200, "WEBVTT"}, // fallback do ASS em texto
		{kinds["pgs/false"], "sup", 200, "PG"},
		{kinds["ass/true"], "ass", 200, "[Script Info]"},
		{kinds["pgs/false"], "vtt", 422, ""},
		{kinds["text/false"], "ass", 422, ""},
		{kinds["text/false"], "xml", 400, ""},
	}
	for _, c := range cases {
		for round := 0; round < 2; round++ { // 2ª rodada sai do cache em disco
			code, body := get("/api/subtitle/subs.mkv?id=" + url.QueryEscape(c.id) + "&format=" + c.format)
			if code != c.status || !strings.HasPrefix(strings.TrimPrefix(string(body), "\xef\xbb\xbf"), c.prefix) {
				t.Errorf("%s %s round %d: %d %.40q", c.id, c.format, round, code, body)
			}
		}
	}

	font, _ := os.ReadFile(findTestFont())
	code, body := get("/api/media-font/subs.mkv?i=" + strconv.Itoa(mi.Fonts[0].Index))
	if code != 200 || !bytes.Equal(body, font) {
		t.Errorf("font: %d, %d bytes (want %d)", code, len(body), len(font))
	}
	if code, _ := get("/api/media-font/subs.mkv?i=0"); code != 404 {
		t.Errorf("non-font stream should 404, got %d", code)
	}
}

func TestStreamConcurrencyAndDisconnect(t *testing.T) {
	requireFFmpeg(t)
	dir := withMediaEnv(t)
	srv := httptest.NewServer(testRouter())
	defer srv.Close()
	runFF(t, "-f", "lavfi", "-i", "testsrc2=d=120:s=1280x720:r=24", "-c:v", "mpeg4", "-q:v", "10", filepath.Join(dir, "long.avi"))

	// vagas ocupadas: depois de esperar slotWait responde 503
	origWait := slotWait
	slotWait = 200 * time.Millisecond
	t.Cleanup(func() { slotWait = origWait })
	for i := 0; i < cap(transcodeSlots); i++ {
		transcodeSlots <- struct{}{}
	}
	resp, _ := http.Get(srv.URL + "/api/stream/long.avi?mode=transcode")
	resp.Body.Close()
	for i := 0; i < cap(transcodeSlots); i++ {
		<-transcodeSlots
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("full slots: got %d, want 503", resp.StatusCode)
	}

	// cliente desconecta no meio: ocioso, o ffmpeg morre e a vaga volta
	origGrace := procIdleGrace
	procIdleGrace = 100 * time.Millisecond
	t.Cleanup(func() { procIdleGrace = origGrace })
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/stream/long.avi?mode=transcode", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("stream: %v %v", err, resp)
	}
	io.ReadFull(resp.Body, make([]byte, 64*1024))
	cancel()
	resp.Body.Close()
	deadline := time.Now().Add(10 * time.Second)
	for len(transcodeSlots) > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(transcodeSlots); n != 0 {
		t.Fatalf("slot not released after disconnect (%d busy)", n)
	}
}

func TestDirectStreamRanges(t *testing.T) {
	dir := withMediaEnv(t)
	srv := httptest.NewServer(testRouter())
	defer srv.Close()
	data := bytes.Repeat([]byte("0123456789"), 100)
	os.WriteFile(filepath.Join(dir, "r.mp4"), data, 0o644)
	for _, c := range []struct {
		rng    string
		status int
		body   string
	}{
		{"", 200, string(data)},
		{"bytes=10-19", 206, "0123456789"},
		{"bytes=995-", 206, "56789"},
		{"bytes=-3", 206, "789"},
		{"bytes=2000-", 416, ""},
	} {
		req, _ := http.NewRequest("GET", srv.URL+"/api/stream/r.mp4", nil)
		if c.rng != "" {
			req.Header.Set("Range", c.rng)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != c.status || (c.status != 416 && string(b) != c.body) {
			t.Errorf("Range %q: %d %q", c.rng, resp.StatusCode, b)
		}
		if c.status == 206 && resp.Header.Get("Content-Length") != strconv.Itoa(len(c.body)) {
			t.Errorf("Range %q: Content-Length %s", c.rng, resp.Header.Get("Content-Length"))
		}
	}
}
