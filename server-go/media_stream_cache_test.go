package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// O <video> larga a conexão e volta com Range: o pedaço tem que ser o mesmo do stream inteiro.
func TestProcessedStreamRanges(t *testing.T) {
	requireFFmpeg(t)
	dir := withMediaEnv(t)
	srv := httptest.NewServer(testRouter())
	defer srv.Close()
	runFF(t, "-f", "lavfi", "-i", "testsrc2=d=20:s=640x360:r=24", "-f", "lavfi", "-i", "sine=d=20",
		"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "mp3", filepath.Join(dir, "r.mkv"))
	url := srv.URL + "/api/stream/r.mkv?mode=remux"

	get := func(rng string) (*http.Response, []byte) {
		req, _ := http.NewRequest("GET", url, nil)
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp, body
	}

	resp, full := get("")
	if resp.StatusCode != 200 || len(full) < 10000 || resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("full: %d len=%d ranges=%q", resp.StatusCode, len(full), resp.Header.Get("Accept-Ranges"))
	}
	from := len(full) / 2
	resp, part := get(fmt.Sprintf("bytes=%d-", from))
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("range: got %d", resp.StatusCode)
	}
	// sem tamanho total mesmo com o ffmpeg terminado: com ele o Chrome varre os fragmentos do MP4
	if want := fmt.Sprintf("bytes %d-%d/*", from, int64(from)+procOpenEnd-1); resp.Header.Get("Content-Range") != want {
		t.Fatalf("Content-Range %q, want %q", resp.Header.Get("Content-Range"), want)
	}
	if resp.Header.Get("Content-Length") != "" {
		t.Fatalf("Content-Length %q, want none", resp.Header.Get("Content-Length"))
	}
	if !bytes.Equal(part, full[from:]) {
		t.Fatalf("range body differs from the full stream (%d vs %d bytes)", len(part), len(full)-from)
	}
	if resp, _ := get(fmt.Sprintf("bytes=%d-", len(full))); resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("past the end: got %d", resp.StatusCode)
	}

	// arquivo apagado (ex.: limpeza): o pedido com Range regera o stream do zero e entrega o mesmo trecho
	procMu.Lock()
	for _, j := range procJobs {
		procDrop(j)
	}
	procMu.Unlock()
	resp, part = get(fmt.Sprintf("bytes=%d-", from))
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(part, full[from:from+len(part)]) || len(part) == 0 {
		t.Fatalf("regenerated range: %d len=%d", resp.StatusCode, len(part))
	}
}

func TestParseRangeStart(t *testing.T) {
	for in, want := range map[string]int64{"bytes=0-": 0, "bytes=123-": 123, "bytes=5-10": 5} {
		if n, ok := parseRangeStart(in); !ok || n != want {
			t.Errorf("%q: %d %v", in, n, ok)
		}
	}
	for _, in := range []string{"", "bytes=-5", "items=1-", "bytes=1-2,4-5"} {
		if _, ok := parseRangeStart(in); ok {
			t.Errorf("%q should be rejected", in)
		}
	}
}

// Teto de disco: conta também streams em uso; ociosos saem primeiro e, sem nenhum, o novo é recusado.
func TestProcTrimDiskCap(t *testing.T) {
	procMu.Lock()
	saved, savedMax := procJobs, procMaxBytes
	procMaxBytes = 100
	mk := func(key string, seq, size int64, readers int) *procJob {
		_, cancel := context.WithCancel(context.Background())
		j := &procJob{key: key, file: filepath.Join(t.TempDir(), key), seq: seq, size: size, readers: readers, cancel: cancel}
		if readers == 0 {
			j.idleSince = time.Now().Add(-time.Second)
		}
		return j
	}
	busy, idle := mk("busy", 1, 80, 1), mk("idle", 2, 50, 0)
	procJobs = map[string]*procJob{"busy": busy, "idle": idle}
	ok := procTrim()
	_, idleLeft := procJobs["idle"]
	procJobs["big"] = mk("big", 3, 40, 1)
	ok2 := procTrim()
	procJobs, procMaxBytes = saved, savedMax
	procMu.Unlock()

	if !ok || idleLeft {
		t.Fatalf("idle stream should be dropped to get under the cap (ok=%v idleLeft=%v)", ok, idleLeft)
	}
	if ok2 {
		t.Fatal("over the cap with only busy streams: new streams must be refused")
	}
}

// fakeProcJob põe na tabela um stream "ainda gerando" com o arquivo dado; grow acrescenta bytes
// e finish encerra como o ffmpeg faria (ok ou morto no meio).
func fakeProcJob(t *testing.T, key string, first []byte) (grow func([]byte), finish func(failed bool)) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.mp4")
	if err := os.WriteFile(path, first, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &procJob{key: key, file: path, mode: "remux", ctx: ctx, cancel: cancel, size: int64(len(first)), changed: make(chan struct{})}
	procMu.Lock()
	procJobs[key] = j
	procMu.Unlock()
	t.Cleanup(func() {
		cancel()
		procMu.Lock()
		delete(procJobs, key)
		procMu.Unlock()
	})
	grow = func(b []byte) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Error(err)
			return
		}
		_, err = f.Write(b)
		f.Close()
		if err != nil {
			t.Error(err)
			return
		}
		j.mu.Lock()
		j.size += int64(len(b))
		j.notify()
		j.mu.Unlock()
	}
	finish = func(failed bool) {
		j.mu.Lock()
		j.done, j.failed = true, failed
		j.notify()
		j.mu.Unlock()
	}
	return grow, finish
}

// No celular o primeiro pedido cai e o Chrome volta com Range enquanto o ffmpeg ainda gera.
// Se o 206 de tamanho "*" terminar antes do fim real, o Chrome toma isso como fim do vídeo
// (som some e pula para o fim). A resposta tem que acompanhar o ffmpeg até ele terminar.
func TestProcRangeWhileGeneratingFollowsToEnd(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), 512<<10) // 8 MB
	grow, finish := fakeProcJob(t, "fake-range", data[:1<<20])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveProc(w, r, "fake-range", "x.mkv", "remux", nil, nil)
	}))
	defer srv.Close()

	grown := make(chan struct{})
	defer func() { <-grown }() // o grow usa o t: termina antes do teste
	go func() {
		defer close(grown)
		for off := 1 << 20; off < len(data); off += 1 << 20 {
			time.Sleep(20 * time.Millisecond)
			grow(data[off : off+1<<20])
		}
		finish(false)
	}()
	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Range", "bytes=100-")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || !strings.HasPrefix(resp.Header.Get("Content-Range"), "bytes 100-") {
		t.Fatalf("status %d Content-Range %q", resp.StatusCode, resp.Header.Get("Content-Range"))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, data[100:]) {
		t.Fatalf("got %d bytes, want the rest of the stream (%d)", len(body), len(data)-100)
	}
}

// ffmpeg morto no meio: o arquivo parcial não pode parecer o fim do vídeo, então a conexão cai.
func TestProcPartialStreamAbortsConnection(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 1<<20)
	_, finish := fakeProcJob(t, "fake-partial", data)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveProc(w, r, "fake-partial", "x.mkv", "remux", nil, nil)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	go func() {
		time.Sleep(50 * time.Millisecond)
		finish(true)
	}()
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Fatal("partial stream ended cleanly; the browser would take it as the end of the video")
	}
}

// Safari sonda com "bytes=0-1" e depois pede pedaços fechados; sem um 206 exato o vídeo nem toca.
// Com o ffmpeg terminado o tamanho total vai junto; o Chrome (Range aberto) segue sem ele.
func TestProcClosedRangeServesExactSlice(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), 1<<12) // 64 KB
	_, finish := fakeProcJob(t, "fake-closed", data)
	finish(false)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveProc(w, r, "fake-closed", "x.mkv", "remux", nil, nil)
	}))
	defer srv.Close()

	for _, c := range []struct {
		rng, cr string
		body    []byte
	}{
		{"bytes=0-1", fmt.Sprintf("bytes 0-1/%d", len(data)), data[:2]},
		{"bytes=100-199", fmt.Sprintf("bytes 100-199/%d", len(data)), data[100:200]},
		{fmt.Sprintf("bytes=%d-%d", len(data)-10, len(data)+50), fmt.Sprintf("bytes %d-%d/%d", len(data)-10, len(data)-1, len(data)), data[len(data)-10:]},
	} {
		req, _ := http.NewRequest("GET", srv.URL, nil)
		req.Header.Set("Range", c.rng)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Range") != c.cr || !bytes.Equal(body, c.body) {
			t.Fatalf("%s: status %d Content-Range %q, %d bytes", c.rng, resp.StatusCode, resp.Header.Get("Content-Range"), len(body))
		}
	}

	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Range", "bytes=100-")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !strings.HasSuffix(resp.Header.Get("Content-Range"), "/*") {
		t.Fatalf("open range must stay without total, got %q", resp.Header.Get("Content-Range"))
	}
}
