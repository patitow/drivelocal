package main

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Sincronia dos streams processados: decodifica a saída do /api/stream e mede, no próprio MP4, onde
// caem o flash (vídeo) e o bipe (áudio) de cada segundo do fixture de sincronia. Tudo tem de estar em
// tempo do arquivo - start + streamLead, com o vídeo deslocado só pelo ajuste ?adelay=.

// streamMarks devolve os instantes (s, pts do arquivo f) dos flashes e dos começos de bipe.
func streamMarks(t *testing.T, f string) (flashes, beeps []float64, firstAudio float64) {
	t.Helper()
	// vídeo: brilho médio da metade de cima em cada quadro
	out, err := exec.Command("ffmpeg", "-v", "error", "-nostdin", "-copyts", "-i", f, "-map", "0:v", "-vf",
		"crop=iw:ih/2:0:0,signalstats,metadata=print:key=lavfi.signalstats.YAVG:file=-", "-f", "null", "-").Output()
	if err != nil {
		t.Fatalf("decode video: %v", err)
	}
	reT := regexp.MustCompile(`pts_time:([0-9.]+)`)
	reY := regexp.MustCompile(`YAVG=([0-9.]+)`)
	var cur float64
	on := false
	for _, line := range strings.Split(string(out), "\n") {
		if m := reT.FindStringSubmatch(line); m != nil {
			cur, _ = strconv.ParseFloat(m[1], 64)
		} else if m := reY.FindStringSubmatch(line); m != nil {
			y, _ := strconv.ParseFloat(m[1], 64)
			if y > 128 && !on {
				flashes = append(flashes, cur)
			}
			on = y > 128
		}
	}
	// áudio: fim de cada silêncio = começo de um bipe
	cmd := exec.Command("ffmpeg", "-v", "info", "-nostdin", "-copyts", "-i", f, "-map", "0:a", "-af", "silencedetect=n=-30dB:d=0.3", "-f", "null", "-")
	se, _ := cmd.CombinedOutput()
	for _, m := range regexp.MustCompile(`silence_end: ([0-9.]+)`).FindAllStringSubmatch(string(se), -1) {
		v, _ := strconv.ParseFloat(m[1], 64)
		beeps = append(beeps, v)
	}
	pk, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "a:0", "-read_intervals", "%+#1",
		"-show_entries", "packet=pts_time", "-of", "csv=p=0", f).Output()
	firstAudio = math.NaN()
	if v := strings.Trim(strings.TrimSpace(strings.Split(string(pk), "\n")[0]), ","); err == nil && v != "" {
		firstAudio, _ = strconv.ParseFloat(v, 64)
	}
	return flashes, beeps, firstAudio
}

func nearest(xs []float64, v float64) float64 {
	best := math.Inf(1)
	for _, x := range xs {
		if math.Abs(x-v) < math.Abs(best-v) {
			best = x
		}
	}
	return best
}

func getKeyframe(t *testing.T, srv *httptest.Server, name string, at float64) float64 {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/keyframe/" + url.PathEscape(name) + "?t=" + strconv.FormatFloat(at, 'f', 3, 64))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct{ Time float64 }
	json.NewDecoder(resp.Body).Decode(&out)
	return out.Time
}

func TestProcessedStreamSync(t *testing.T) {
	requireFFmpeg(t)
	if testing.Short() {
		t.Skip("decodifica vários streams")
	}
	dir := withMediaEnv(t)
	srv := httptest.NewServer(testRouter())
	defer srv.Close()

	h264 := syncSpec{name: "s.mkv", video: "libx264", vopts: []string{"-g", "120", "-keyint_min", "120", "-sc_threshold", "0", "-bf", "3"},
		audio: []string{"aac", "eac3"}, channels: 2, subs: []string{"srt"}, dur: 24}
	hevc := syncSpec{name: "h.mkv", video: "libx265", vopts: []string{"-x265-params", "log-level=error:keyint=120:min-keyint=120:scenecut=0:bframes=4"},
		audio: []string{"eac3"}, channels: 6, dur: 24}
	ts := syncSpec{name: "t.ts", video: "libx264", vopts: []string{"-g", "120", "-keyint_min", "120", "-sc_threshold", "0", "-bf", "3"},
		audio: []string{"aac"}, channels: 2, dur: 24}
	for _, f := range []syncSpec{h264, hevc, ts} {
		buildSyncFixture(t, dir, f)
	}
	runFF(t, "-i", filepath.Join(dir, "s.mkv"), "-map", "0:v", "-c", "copy", filepath.Join(dir, "v.mkv")) // sem áudio
	type mk struct{ flashes, beeps []float64 }
	marks := map[string]mk{}
	for _, name := range []string{"s.mkv", "h.mkv", "t.ts", "v.mkv"} {
		full := filepath.Join(dir, name)
		info, _ := os.Stat(full)
		mi, err := probeMedia(t.Context(), full, info)
		if err != nil {
			t.Fatal(err)
		}
		fl, bp, _ := streamMarks(t, full)
		o := fileOrigin(mi)
		for i := range fl {
			fl[i] -= o
		}
		for i := range bp {
			bp[i] -= o
		}
		marks[name] = mk{fl, bp}
		t.Logf("%s: start_time=%.3f flash do 5º s em %.3f", name, mi.StartTime, nearest(fl, 5))
	}

	const tol = 0.005 // 5 ms: arredondamento do keyframe (1 ms) + resolução do silencedetect
	cases := []struct {
		name, file, mode string
		at               float64 // instante pedido (tempo do arquivo)
		audio            string
		delayMs          int
	}{
		{"h264 remux início", "s.mkv", "remux", 0, "", 0},
		{"h264 remux seek", "s.mkv", "remux", 12.4, "", 0},
		{"h264 remux 2ª faixa eac3", "s.mkv", "remux", 7.7, "1", 0},
		{"h264 remux áudio +300ms", "s.mkv", "remux", 12.4, "", 300},
		{"h264 remux áudio -450ms", "s.mkv", "remux", 12.4, "", -450},
		{"hevc remux seek (eac3 5.1)", "h.mkv", "remux", 16.2, "", 0},
		{"hevc transcode seek", "h.mkv", "transcode", 9.3, "", 0},
		{"hevc transcode áudio -200ms", "h.mkv", "transcode", 9.3, "", -200},
		{"mpeg-ts remux seek", "t.ts", "remux", 11.1, "", 0},
		{"sem áudio remux seek", "v.mkv", "remux", 6.6, "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start := c.at
			if c.mode == "remux" && c.at > 0 {
				start = getKeyframe(t, srv, c.file, c.at)
				if start > c.at || c.at-start > 5.01 {
					t.Fatalf("keyframe(%.1f) = %.3f", c.at, start)
				}
			}
			q := "mode=" + c.mode + "&start=" + strconv.FormatFloat(start, 'f', 3, 64)
			if c.audio != "" {
				q += "&audio=" + c.audio
			}
			if c.delayMs != 0 {
				q += "&adelay=" + strconv.Itoa(c.delayMs)
			}
			resp, err := http.Get(srv.URL + "/api/stream/" + url.PathEscape(c.file) + "?" + q)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("status %d: %s", resp.StatusCode, body)
			}
			if resp.Header.Get("X-Stream-Lead") != "2" {
				t.Errorf("X-Stream-Lead = %q", resp.Header.Get("X-Stream-Lead"))
			}
			out := filepath.Join(t.TempDir(), "s.mp4")
			os.WriteFile(out, body, 0o644)
			flashes, beeps, firstAudio := streamMarks(t, out)
			delay := float64(c.delayMs) / 1000
			// onde o próprio arquivo tem o flash/bipe do segundo k (tempo do arquivo = pts - max(start_time, 0)).
			// Num .ts o priming do AAC empurra o conteúdo ~21 ms; num mkv só de vídeo o atraso dos B-frames
			// empurra ~83 ms: isso é do arquivo (o direct também toca assim), não do player.
			src := marks[c.file]
			k := math.Ceil(start + 0.5)
			wantFlash := nearest(src.flashes, k) - start + streamLead - delay
			wantBeep := wantFlash + delay
			if len(src.beeps) > 0 {
				wantBeep = nearest(src.beeps, k) - start + streamLead
			}
			gotFlash := nearest(flashes, wantFlash)
			if math.Abs(gotFlash-wantFlash) > tol {
				t.Errorf("flash do segundo %.0f em %.3f, esperado %.3f (flashes %v)", k, gotFlash, wantFlash, flashes)
			}
			if c.file == "v.mkv" {
				// só silêncio, começando em 0 (o silencedetect fecha um "silence_end" no fim do arquivo)
				if len(beeps) > 1 || math.IsNaN(firstAudio) || math.Abs(firstAudio) > 0.001 {
					t.Errorf("stream sem áudio: bipes %v, primeiro pacote %v", beeps, firstAudio)
				}
				return
			}
			gotBeep := nearest(beeps, wantBeep)
			if math.Abs(gotBeep-wantBeep) > tol {
				t.Errorf("bipe do segundo %.0f em %.3f, esperado %.3f (bipes %v)", k, gotBeep, wantBeep, beeps)
			}
			// o áudio começa em 0 (sem buraco inicial que o Chrome "puxaria")
			if math.IsNaN(firstAudio) || math.Abs(firstAudio) > 0.001 {
				t.Errorf("primeiro pacote de áudio em %v, esperado 0", firstAudio)
			}
			t.Logf("start=%.3f flash-esperado=%+.1fms bipe-esperado=%+.1fms a/v=%+.1fms", start,
				(gotFlash-wantFlash)*1000, (gotBeep-wantBeep)*1000, (gotBeep-gotFlash)*1000)
		})
	}
}

// As legendas extraídas ficam no tempo do arquivo: sem -copyts o ffmpeg subtraía o start_time
// (-0,021 s num mkv com AAC) e a legenda embutida saía 21 ms atrasada.
func TestEmbeddedSubtitleKeepsFileTime(t *testing.T) {
	requireFFmpeg(t)
	dir := withMediaEnv(t)
	srv := httptest.NewServer(testRouter())
	defer srv.Close()
	f := syncSpec{name: "c.mkv", video: "libx264", vopts: []string{"-bf", "3"}, audio: []string{"aac"}, channels: 2, subs: []string{"srt", "ass"}, dur: 4}
	buildSyncFixture(t, dir, f)
	full := filepath.Join(dir, f.name)
	info, _ := os.Stat(full)
	mi, err := probeMedia(t.Context(), full, info)
	if err != nil {
		t.Fatal(err)
	}
	if mi.StartTime >= 0 {
		t.Fatalf("fixture deveria ter start_time negativo (priming do AAC), tem %v", mi.StartTime)
	}
	for _, s := range mi.Subtitles {
		format := map[string]string{"text": "vtt", "ass": "ass"}[s.Kind]
		resp, err := http.Get(srv.URL + "/api/subtitle/c.mkv?id=" + url.QueryEscape(s.ID) + "&format=" + format)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		want := map[string]string{"vtt": "00:01.000 --> 00:01.500", "ass": "Dialogue: 0,0:00:01.00,0:00:01.50"}[format]
		if !strings.Contains(string(b), want) {
			t.Errorf("%s (%s): primeira fala fora do tempo, quero %q:\n%.300s", s.ID, format, want, b)
		}
	}
}

func TestLastKeyframe(t *testing.T) {
	// o seek do ffprobe caiu dois keyframes antes do ponto: vale o último <= t
	csv := "10.021000,K__\n10.063000,___\n15.021000,K__\n17.000000,___\n20.021000,K__\n"
	if kf, ok := lastKeyframe(csv, 17.5); !ok || kf != 15.021 {
		t.Errorf("lastKeyframe = %v %v, want 15.021", kf, ok)
	}
	if kf, ok := lastKeyframe(csv, 15.021); !ok || kf != 15.021 {
		t.Errorf("exato no keyframe: %v %v", kf, ok)
	}
	if _, ok := lastKeyframe("12.0,___\n", 13); ok {
		t.Error("sem keyframe deveria falhar")
	}
}

func TestStreamRejectsBadAudioDelay(t *testing.T) {
	dir := withMediaEnv(t)
	os.WriteFile(filepath.Join(dir, "a.mkv"), []byte("x"), 0o644)
	srv := httptest.NewServer(testRouter())
	defer srv.Close()
	for _, d := range []string{"abc", "1501", "-1501", "1.5"} {
		resp, err := http.Get(srv.URL + "/api/stream/a.mkv?mode=remux&adelay=" + d)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Errorf("adelay=%s: %d, want 400", d, resp.StatusCode)
		}
	}
}
