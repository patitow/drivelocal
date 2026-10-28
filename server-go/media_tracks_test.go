package main

import (
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

func TestBuildMediaInfoClassifiesTracks(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Ep 01.mkv")
	os.WriteFile(video, []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "Ep 01.pt-BR.srt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "Ep 02.srt"), []byte("x"), 0o644)

	out := ffprobeOutput{Streams: []ffprobeStream{
		{Index: 0, CodecType: "video", CodecName: "hevc"},
		{Index: 1, CodecType: "audio", CodecName: "eac3", Channels: 6, Tags: map[string]string{"language": "jpn"}, Disposition: map[string]int{"default": 1}},
		{Index: 2, CodecType: "audio", CodecName: "opus", Channels: 2, Tags: map[string]string{"language": "eng"}},
		{Index: 3, CodecType: "subtitle", CodecName: "subrip", Tags: map[string]string{"language": "por"}},
		{Index: 4, CodecType: "subtitle", CodecName: "hdmv_pgs_subtitle", Tags: map[string]string{"language": "eng"}},
	}}
	out.Format.Duration = "1420.5"

	mi := buildMediaInfo(out, video)
	if mi.VideoCodec != "hevc" || mi.Duration != 1420.5 {
		t.Fatalf("video/duration wrong: %+v", mi)
	}
	if len(mi.Audio) != 2 || mi.Audio[0].Playable || !mi.Audio[1].Playable || mi.Audio[1].Order != 1 {
		t.Fatalf("audio wrong: %+v", mi.Audio)
	}
	if len(mi.Subtitles) != 3 {
		t.Fatalf("expected 2 embedded + 1 external subtitle, got %+v", mi.Subtitles)
	}
	if mi.Subtitles[0].Kind != "text" || mi.Subtitles[1].Kind != "pgs" || !mi.Subtitles[1].Supported {
		t.Fatalf("subtitle kinds wrong: %+v", mi.Subtitles)
	}
	ext := mi.Subtitles[2]
	if !ext.External || ext.ID != "ext:Ep 01.pt-BR.srt" || ext.Lang != "por" {
		t.Fatalf("external subtitle wrong: %+v", ext)
	}
}

func TestRemuxArgs(t *testing.T) {
	// vídeo no índice 1: o 0 é uma capa (attached_pic) e não pode ser mapeado como 0:v:0
	mi := &MediaInfo{VideoIndex: 1, VideoCodec: "hevc", Audio: []AudioTrack{{Codec: "eac3"}, {Codec: "aac"}}}

	args := strings.Join(remuxArgs("/m/a.mkv", 12.5, 0, mi, 0), " ")
	for _, want := range []string{"-ss 12.500000", "-map 0:1 ", "-map 0:a:0", "-c:v copy", "-copypriorss 0", "-tag:v hvc1",
		"aresample=48000:async=1:first_pts=-94976", "-c:a aac -b:a 192k -ac 2", "frag_keyframe"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q: %s", want, args)
		}
	}
	if strings.Index(args, "-ss") > strings.Index(args, "-i ") {
		t.Errorf("-ss must come before -i for fast seek: %s", args)
	}
	if strings.Contains(args, "setts") || strings.Contains(args, "-output_ts_offset") {
		t.Errorf("sem ajuste não desloca nada: %s", args)
	}

	// mesmo AAC é recodificado (o áudio precisa começar em 0 com o silêncio de streamLead); início sem -ss
	args = strings.Join(remuxArgs("/m/a.mkv", 0, 1, mi, 0), " ")
	if strings.Contains(args, "-ss") || !strings.Contains(args, "-c:a aac") {
		t.Errorf("start=0: %s", args)
	}

	// start_time > 0 (MPEG-TS): o tempo do arquivo conta a partir do start_time, e o -ss de entrada do
	// ffmpeg já é relativo ao start_time
	ts := &MediaInfo{VideoIndex: 0, VideoCodec: "h264", StartTime: 1.4, Audio: []AudioTrack{{Codec: "aac"}}}
	if args = strings.Join(remuxArgs("/m/a.ts", 10, 0, ts, 0), " "); !strings.Contains(args, "-ss 10.000000") {
		t.Errorf("ts start_time não somado: %s", args)
	}
	// start_time < 0 (priming do AAC): o tempo 0 do arquivo é o pts 0
	neg := &MediaInfo{VideoIndex: 0, VideoCodec: "h264", StartTime: -0.021, Audio: []AudioTrack{{Codec: "aac"}}}
	if args = strings.Join(remuxArgs("/m/a.mkv", 10, 0, neg, 0), " "); !strings.Contains(args, "-ss 10.021000") {
		t.Errorf("start_time negativo: %s", args)
	}

	// ajuste de áudio: desloca o vídeo, o áudio fica no lugar
	args = strings.Join(remuxArgs("/m/a.mkv", 5, 0, mi, 0.25), " ")
	if !strings.Contains(args, "-bsf:v setts=pts=PTS-(0.250)/TB:dts=DTS-(0.250)/TB") {
		t.Errorf("adelay: %s", args)
	}
	args = strings.Join(transcodeArgs("/m/a.mkv", 5, 0, mi, -0.1), " ")
	if !strings.Contains(args, "setpts=PTS-(-0.100)/TB,scale") || strings.Contains(args, "setts") || !strings.Contains(args, "first_pts=-94976") {
		t.Errorf("transcode adelay: %s", args)
	}

	// sem áudio: faixa de silêncio gerada ancora o tempo como um áudio real
	vid := &MediaInfo{VideoIndex: 0, VideoCodec: "h264", Duration: 30}
	args = strings.Join(remuxArgs("/m/v.mkv", 3, -1, vid, 0), " ")
	for _, want := range []string{"-f lavfi -t 28.000 -i anullsrc=r=48000:cl=stereo", "-map 1:a", "first_pts=-94976", "-c:a aac"} {
		if !strings.Contains(args, want) {
			t.Errorf("sem áudio, falta %q: %s", want, args)
		}
	}
}

func TestResolveMediaFileRejectsTraversal(t *testing.T) {
	original := MEDIA_PATH
	MEDIA_PATH = t.TempDir()
	t.Cleanup(func() { MEDIA_PATH = original })

	if _, _, err := resolveMediaFile("../../etc/passwd"); err != errBadMedia {
		t.Fatalf("expected errBadMedia, got %v", err)
	}
	if _, _, err := resolveMediaFile(""); err != errNotMedia {
		t.Fatalf("root should not be a media file, got %v", err)
	}
}

func TestSubtitleHandlerValidatesID(t *testing.T) {
	original := MEDIA_PATH
	MEDIA_PATH = t.TempDir()
	t.Cleanup(func() { MEDIA_PATH = original })
	os.WriteFile(filepath.Join(MEDIA_PATH, "v.mkv"), []byte("x"), 0o644)

	cases := map[string]int{
		"":                  http.StatusBadRequest,
		"ext:../secret.srt": http.StatusBadRequest,
		"ext:notasub.txt":   http.StatusBadRequest,
		"ext:missing.srt":   http.StatusNotFound,
		"emb:abc":           http.StatusBadRequest,
		"emb:-1":            http.StatusBadRequest,
	}
	for id, want := range cases {
		req := httptest.NewRequest("GET", "/api/subtitle/v.mkv?id="+id, nil)
		req = mux.SetURLVars(req, map[string]string{"path": "v.mkv"})
		rec := httptest.NewRecorder()
		subtitleHandler(rec, req)
		if rec.Code != want {
			t.Errorf("id %q: got %d, want %d", id, rec.Code, want)
		}
	}
}

func TestStreamRemuxedRejectsBadParams(t *testing.T) {
	original := MEDIA_PATH
	MEDIA_PATH = t.TempDir()
	t.Cleanup(func() { MEDIA_PATH = original })
	full := filepath.Join(MEDIA_PATH, "v.mkv")
	os.WriteFile(full, []byte("x"), 0o644)
	info, _ := os.Stat(full)

	for _, q := range []string{"audio=-1", "audio=x", "start=-5", "start=abc", "start=NaN", "mode=bogus"} {
		rec := httptest.NewRecorder()
		streamProcessed(rec, httptest.NewRequest("GET", "/api/stream/v.mkv?"+q, nil), full, info)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", q, rec.Code)
		}
	}
}

func TestMediaInfoEmptyListsSerializeAsArrays(t *testing.T) {
	original := MEDIA_PATH
	MEDIA_PATH = t.TempDir()
	t.Cleanup(func() { MEDIA_PATH = original })
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	// mp4 sem legendas e com um único áudio: o caso que antes serializava "subtitles": null
	file := filepath.Join(MEDIA_PATH, "plain.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=d=1:s=64x64:r=10", "-f", "lavfi", "-i", "sine=d=1", "-shortest", "-c:v", "libx264", "-c:a", "aac", file)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot generate fixture: %v %s", err, out)
	}
	for i := 0; i < 2; i++ { // 2ª chamada passa pelo cache/clone
		req := httptest.NewRequest("GET", "/api/media-info/plain.mp4", nil)
		req = mux.SetURLVars(req, map[string]string{"path": "plain.mp4"})
		rec := httptest.NewRecorder()
		mediaInfoHandler(rec, req)
		body := rec.Body.String()
		if rec.Code != 200 || !strings.Contains(body, `"subtitles":[]`) || strings.Contains(body, "null") {
			t.Fatalf("call %d: status %d body %s", i, rec.Code, body)
		}
	}
}

func TestBuildMediaInfoDurationIgnoresLongSubtitle(t *testing.T) {
	// legenda com tempo errado estica o contêiner até 45 min; vídeo e áudio têm 24
	out := ffprobeOutput{Streams: []ffprobeStream{
		{Index: 0, CodecType: "video", CodecName: "hevc", Duration: "N/A", Tags: map[string]string{"DURATION": "00:23:55.976000000"}},
		{Index: 1, CodecType: "audio", CodecName: "aac", Tags: map[string]string{"DURATION": "00:23:55.989000000"}},
		{Index: 2, CodecType: "subtitle", CodecName: "subrip", Duration: "2739.387", Tags: map[string]string{"DURATION": "00:45:31.937000000"}},
	}}
	out.Format.Duration = "2739.387"
	mi := buildMediaInfo(out, filepath.Join(t.TempDir(), "Ep.mkv"))
	if math.Abs(mi.Duration-1435.989) > 0.01 {
		t.Fatalf("duração = %v, queria a do vídeo/áudio (1435.989)", mi.Duration)
	}

	// sem duração por faixa, fica a do contêiner
	out = ffprobeOutput{Streams: []ffprobeStream{{Index: 0, CodecType: "video", CodecName: "h264"}}}
	out.Format.Duration = "1420.5"
	if mi := buildMediaInfo(out, filepath.Join(t.TempDir(), "Ep.mp4")); mi.Duration != 1420.5 {
		t.Fatalf("duração = %v, queria 1420.5", mi.Duration)
	}
}
