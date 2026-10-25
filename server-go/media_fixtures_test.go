package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Geração de fixtures de mídia com ffmpeg (lavfi), usada pelos testes Go e pelo e2e do Playwright.

type fxSpec struct {
	name   string   // nome do arquivo (a extensão define o contêiner)
	video  string   // encoder de vídeo; "" = sem vídeo
	vopts  []string // opções extras do vídeo (pix_fmt, profile, ...)
	audio  []string // um encoder por faixa (a 1ª é a default)
	subs   []string // "srt", "ass", "pgs"
	font   bool     // anexa uma fonte TTF (mkv)
	cover  bool     // capa (attached_pic) antes do vídeo (mp4)
	dur    int      // segundos
	wantDf string   // modo esperado com caps padrão (navegador conservador)
	wantCh string   // modo esperado com as caps do Chrome 155 desta máquina
}

// caps medidas no Chrome 155 (canPlayType + reprodução real das fixtures)
const chromeCaps = "h264hi10,hevc,hevc10,av1,vp9,mkv"

var subLangs = []string{"por", "eng", "spa"}
var audioLangs = []string{"jpn", "eng", "por"}

func requireFFmpeg(t testing.TB) {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
}

// findTestFont procura uma TTF do sistema para anexar ao mkv.
func findTestFont() string {
	for _, p := range []string{`C:\Windows\Fonts\arial.ttf`, "/usr/share/fonts/TTF/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", "/usr/share/fonts/dejavu/DejaVuSans.ttf", "/Library/Fonts/Arial.ttf"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

const fixtureSRT = "1\n00:00:00,000 --> 00:01:00,000\nLegenda SRT\n"

// ASS amarelo (&H0000FFFF) com fonte Arial: o e2e confere os pixels no canvas do JASSUB.
const fixtureASS = `[Script Info]
ScriptType: v4.00+
PlayResX: 320
PlayResY: 240

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Arial,40,&H0000FFFF,&H000000FF,&H00000000,&H00000000,-1,0,0,0,100,100,0,0,1,2,0,2,10,10,10,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:00.00,0:01:00.00,Default,,0,0,0,,LEGENDA ASS
`

// writePGS gera um .sup mínimo: um retângulo vermelho de 0,1 s a dur segundos (ffmpeg não tem encoder PGS).
func writePGS(path string, w, h, dur int) error {
	return writePGSEvents(path, w, h, [][2]float64{{0.1, float64(dur)}})
}

// writePGSEvents gera um .sup com um retângulo vermelho em cada intervalo [início, fim] (segundos).
func writePGSEvents(path string, w, h int, events [][2]float64) error {
	var buf bytes.Buffer
	seg := func(pts uint32, typ byte, data []byte) {
		buf.WriteString("PG")
		binary.Write(&buf, binary.BigEndian, pts)
		binary.Write(&buf, binary.BigEndian, uint32(0))
		buf.WriteByte(typ)
		binary.Write(&buf, binary.BigEndian, uint16(len(data)))
		buf.Write(data)
	}
	be16 := func(v int) []byte { return []byte{byte(v >> 8), byte(v)} }
	ow, oh, ox, oy := w/2, h/6, w/4, h-h/6-10

	pcs := func(num int, state byte, objects int) []byte {
		d := append(append(be16(w), be16(h)...), 0x10)
		d = append(d, be16(num)...)
		d = append(d, state, 0, 0, byte(objects))
		if objects > 0 {
			d = append(d, append(append(be16(0), 0, 0), append(be16(ox), be16(oy)...)...)...)
		}
		return d
	}
	wds := append([]byte{1, 0}, append(append(be16(ox), be16(oy)...), append(be16(ow), be16(oh)...)...)...)
	// paleta: entrada 1 = vermelho opaco (Y, Cr, Cb, A)
	pds := []byte{0, 0, 1, 81, 240, 90, 255}
	var rle []byte
	for y := 0; y < oh; y++ {
		rle = append(rle, 0, 0xC0|byte(ow>>8), byte(ow), 1, 0, 0)
	}
	ods := append(append(be16(0), 0, 0xC0), byte((len(rle)+4)>>16), byte((len(rle)+4)>>8), byte(len(rle)+4))
	ods = append(append(append(ods, be16(ow)...), be16(oh)...), rle...)

	num := 1
	for _, ev := range events {
		start, end := uint32(math.Round(ev[0]*90000)), uint32(math.Round(ev[1]*90000))
		seg(start, 0x16, pcs(num, 0x80, 1))
		seg(start, 0x17, wds)
		seg(start, 0x14, pds)
		seg(start, 0x15, ods)
		seg(start, 0x80, nil)
		num++
		seg(end, 0x16, pcs(num, 0x00, 0))
		seg(end, 0x17, wds)
		seg(end, 0x80, nil)
		num++
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// buildFixture gera o arquivo descrito por f em dir e devolve o caminho.
func buildFixture(t testing.TB, dir string, f fxSpec) string {
	t.Helper()
	dur := f.dur
	if dur == 0 {
		dur = 3
	}
	ds := strconv.Itoa(dur)
	out := filepath.Join(dir, f.name)
	isMP4 := strings.HasSuffix(f.name, ".mp4") || strings.HasSuffix(f.name, ".mov")
	work := t.TempDir() // fontes intermediárias fora de dir (senão viram "legenda externa")

	var in, maps, codecs []string
	nIn, vIdx, aIdx, sIdx := 0, 0, 0, 0
	addIn := func(args ...string) int { in = append(in, args...); nIn++; return nIn - 1 }

	if f.cover {
		cover := filepath.Join(work, "cover.png")
		runFF(t, "-f", "lavfi", "-i", "color=c=red:s=64x64", "-frames:v", "1", cover)
		i := addIn("-i", cover)
		maps = append(maps, "-map", fmt.Sprintf("%d:v", i))
		codecs = append(codecs, fmt.Sprintf("-c:v:%d", vIdx), "png", fmt.Sprintf("-disposition:v:%d", vIdx), "attached_pic")
		vIdx++
	}
	if f.video != "" {
		i := addIn("-f", "lavfi", "-i", "testsrc2=d="+ds+":s=320x240:r=24")
		maps = append(maps, "-map", fmt.Sprintf("%d:v", i))
		codecs = append(codecs, fmt.Sprintf("-c:v:%d", vIdx), f.video)
		codecs = append(codecs, f.vopts...)
		vIdx++
	}
	for n, enc := range f.audio {
		i := addIn("-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:duration=%s:sample_rate=48000", 440+110*n, ds))
		maps = append(maps, "-map", fmt.Sprintf("%d:a", i))
		codecs = append(codecs, fmt.Sprintf("-c:a:%d", aIdx), enc, fmt.Sprintf("-metadata:s:a:%d", aIdx), "language="+audioLangs[n%len(audioLangs)])
		if n == 0 {
			codecs = append(codecs, fmt.Sprintf("-disposition:a:%d", aIdx), "default")
		} else {
			codecs = append(codecs, fmt.Sprintf("-disposition:a:%d", aIdx), "0")
		}
		aIdx++
	}
	for n, kind := range f.subs {
		src := filepath.Join(work, fmt.Sprintf("sub%d.%s", n, map[string]string{"srt": "srt", "ass": "ass", "pgs": "sup"}[kind]))
		var err error
		switch kind {
		case "srt":
			err = os.WriteFile(src, []byte(fixtureSRT), 0o644)
		case "ass":
			err = os.WriteFile(src, []byte(fixtureASS), 0o644)
		case "pgs":
			err = writePGS(src, 320, 240, dur)
		}
		if err != nil {
			t.Fatal(err)
		}
		i := addIn("-i", src)
		maps = append(maps, "-map", fmt.Sprintf("%d:s", i))
		codec := map[string]string{"srt": "srt", "ass": "ass", "pgs": "copy"}[kind]
		if isMP4 {
			codec = "mov_text"
		}
		codecs = append(codecs, fmt.Sprintf("-c:s:%d", sIdx), codec, fmt.Sprintf("-metadata:s:s:%d", sIdx), "language="+subLangs[n%len(subLangs)],
			fmt.Sprintf("-metadata:s:s:%d", sIdx), "title="+strings.ToUpper(kind))
		sIdx++
	}
	args := append(in, maps...)
	args = append(args, codecs...)
	if aIdx > 0 {
		args = append(args, "-ac", "2", "-strict", "-2")
	}
	if f.font {
		font := findTestFont()
		if font == "" {
			t.Skip("no TTF font found to attach")
		}
		args = append(args, "-attach", font, "-metadata:s:t:0", "mimetype=application/x-truetype-font", "-metadata:s:t:0", "filename=arial.ttf")
	}
	runFF(t, append(append(args, "-t", ds), out)...)
	return out
}

func runFF(t testing.TB, args ...string) {
	t.Helper()
	cmd := exec.Command("ffmpeg", append([]string{"-v", "error", "-y", "-nostdin"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg %v: %v\n%s", args, err, out)
	}
}

// e2eFixtures alimenta o Playwright (tests/media-playback.spec.js): um arquivo por modo de reprodução,
// com áudio duplo e legendas SRT/ASS/PGS para trocar faixas e fazer seek.
var e2eFixtures = []fxSpec{
	{name: "direct_h264_aac.mkv", video: "libx264", vopts: []string{"-g", "48"}, audio: []string{"aac", "aac"}, subs: []string{"srt", "ass", "pgs"}, font: true, dur: 30},
	{name: "remux_h264_ac3.mkv", video: "libx264", vopts: []string{"-g", "48"}, audio: []string{"ac3", "aac"}, subs: []string{"srt", "ass", "pgs"}, dur: 30},
	{name: "transcode_mpeg4_mp3.avi", video: "mpeg4", vopts: []string{"-q:v", "5"}, audio: []string{"libmp3lame"}, dur: 30},
	{name: "transcode_hevc10_eac3.mkv", video: "libx265", vopts: []string{"-pix_fmt", "yuv420p10le", "-x265-params", "log-level=error:keyint=48"}, audio: []string{"eac3", "aac"}, subs: []string{"ass"}, font: true, dur: 30},
	{name: "remux_h264_ac3only.mkv", video: "libx264", vopts: []string{"-g", "48"}, audio: []string{"ac3"}, dur: 20},
	{name: "remux_h264_aac.m2ts", video: "libx264", vopts: []string{"-g", "48"}, audio: []string{"aac"}, dur: 30},
	{name: "direct_h264_noaudio.mp4", video: "libx264", vopts: []string{"-g", "48"}, dur: 20}, // clipe sem faixa de áudio
}

// TestWriteE2EFixtures só roda com E2E_FIXTURES_DIR (chamado pelo setup do Playwright).
func TestWriteE2EFixtures(t *testing.T) {
	dir := os.Getenv("E2E_FIXTURES_DIR")
	if dir == "" {
		t.Skip("E2E_FIXTURES_DIR not set")
	}
	requireFFmpeg(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range e2eFixtures {
		if _, err := os.Stat(filepath.Join(dir, f.name)); err == nil {
			continue
		}
		buildFixture(t, dir, f)
	}
	for _, f := range syncFixtures {
		if _, err := os.Stat(filepath.Join(dir, f.name)); err == nil {
			continue
		}
		buildSyncFixture(t, dir, f)
	}
	// legenda externa ao lado do vídeo
	os.WriteFile(filepath.Join(dir, "sync_h264_aac.pt-BR.srt"), []byte(syncSRT(40)), 0o644)
	os.WriteFile(filepath.Join(dir, "direct_h264_aac.pt-BR.srt"), []byte("1\n00:00:00,000 --> 00:01:00,000\nLegenda externa\n"), 0o644)
}

// Fixtures de sincronia: tela preta com um flash branco (2 quadros) e um bipe começando exatamente
// em cada segundo inteiro a partir de 1 s, e legendas (SRT/ASS/PGS) de k até k+0,5 s.
// O e2e (tests/media-sync.spec.js) mede no Chrome o instante do flash x bipe x legenda.
type syncSpec struct {
	name     string
	video    string   // encoder de vídeo
	vopts    []string // GOP longo (keyframes não caem nos flashes), B-frames
	audio    []string // encoders, a 1ª é a default
	channels int
	subs     []string // "srt", "ass", "pgs"
	dur      int
}

var syncFixtures = []syncSpec{
	// direct (mkv com AAC) + 2ª faixa E-AC-3 (trocar áudio => remux) + legendas dos três tipos
	{name: "sync_h264_aac.mkv", video: "libx264", vopts: []string{"-g", "120", "-keyint_min", "120", "-sc_threshold", "0", "-bf", "3"},
		audio: []string{"aac", "eac3"}, channels: 2, subs: []string{"srt", "ass", "pgs"}, dur: 40},
	// como o filme: HEVC 10-bit + E-AC-3 5.1 (remux; com caps=none vira transcode)
	{name: "sync_hevc10_eac3.mkv", video: "libx265", vopts: []string{"-pix_fmt", "yuv420p10le", "-x265-params", "log-level=error:keyint=120:min-keyint=120:scenecut=0:bframes=4"},
		audio: []string{"eac3"}, channels: 6, subs: []string{"srt"}, dur: 40},
}

func syncSRT(dur int) string {
	var b strings.Builder
	for k := 1; k < dur; k++ {
		fmt.Fprintf(&b, "%d\n00:00:%02d,000 --> 00:00:%02d,500\nSYNC %d\n\n", k, k, k, k)
	}
	return b.String()
}

func syncASS(dur int) string {
	head := fixtureASS[:strings.Index(fixtureASS, "Dialogue:")]
	var b strings.Builder
	b.WriteString(head)
	for k := 1; k < dur; k++ {
		fmt.Fprintf(&b, "Dialogue: 0,0:00:%02d.00,0:00:%02d.50,Default,,0,0,0,,SYNC %d\n", k, k, k)
	}
	return b.String()
}

func buildSyncFixture(t testing.TB, dir string, f syncSpec) string {
	t.Helper()
	ds := strconv.Itoa(f.dur)
	out := filepath.Join(dir, f.name)
	work := t.TempDir()
	// metade de cima: flash de 2 quadros em cada segundo inteiro; metade de baixo: o número do
	// segundo em binário (6 colunas), para o e2e saber QUAL segundo está na tela (o flash é periódico).
	vf := "color=c=black:s=320x240:r=24:d=" + ds +
		",drawbox=x=0:y=0:w=iw:h=ih/2:color=white:t=fill:enable='gte(t,1)*lt(mod(t+0.001,1),0.08)'"
	for bit := 0; bit < 6; bit++ {
		vf += fmt.Sprintf(",drawbox=x=%d:y=ih/2:w=53:h=ih/2:color=white:t=fill:enable='mod(floor((t+0.001)/%d),2)'", bit*320/6, 1<<bit)
	}
	args := []string{"-f", "lavfi", "-i", vf}
	maps := []string{"-map", "0:v"}
	codecs := append([]string{"-c:v", f.video}, f.vopts...)
	for n, enc := range f.audio {
		// bipe de 60 ms em cada segundo; a frequência (400..1300 Hz) identifica o segundo (módulo 7)
		args = append(args, "-f", "lavfi", "-i", "aevalsrc='if(gte(t,1)*lt(mod(t,1),0.06),0.8*sin(2*PI*(400+150*mod(floor(t),7))*t),0)':s=48000:d="+ds)
		maps = append(maps, "-map", fmt.Sprintf("%d:a", n+1))
		disp := "0"
		if n == 0 {
			disp = "default"
		}
		codecs = append(codecs, fmt.Sprintf("-c:a:%d", n), enc, fmt.Sprintf("-metadata:s:a:%d", n), "language="+audioLangs[n%len(audioLangs)],
			fmt.Sprintf("-disposition:a:%d", n), disp)
	}
	in := len(f.audio) + 1
	for n, kind := range f.subs {
		src := filepath.Join(work, fmt.Sprintf("sub%d.%s", n, map[string]string{"srt": "srt", "ass": "ass", "pgs": "sup"}[kind]))
		var err error
		switch kind {
		case "srt":
			err = os.WriteFile(src, []byte(syncSRT(f.dur)), 0o644)
		case "ass":
			err = os.WriteFile(src, []byte(syncASS(f.dur)), 0o644)
		case "pgs":
			var ev [][2]float64
			for k := 1; k < f.dur; k++ {
				ev = append(ev, [2]float64{float64(k), float64(k) + 0.5})
			}
			err = writePGSEvents(src, 320, 240, ev)
		}
		if err != nil {
			t.Fatal(err)
		}
		args = append(args, "-i", src)
		maps = append(maps, "-map", fmt.Sprintf("%d:s", in))
		in++
		codec := map[string]string{"srt": "srt", "ass": "ass", "pgs": "copy"}[kind]
		codecs = append(codecs, fmt.Sprintf("-c:s:%d", n), codec, fmt.Sprintf("-metadata:s:s:%d", n), "language="+subLangs[n%len(subLangs)],
			fmt.Sprintf("-metadata:s:s:%d", n), "title=SYNC "+strings.ToUpper(kind))
	}
	args = append(append(args, maps...), codecs...)
	args = append(args, "-ac", strconv.Itoa(f.channels), "-t", ds, out)
	runFF(t, args...)
	return out
}
