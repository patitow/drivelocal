package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	if want := fmt.Sprintf("bytes %d-%d/%d", from, len(full)-1, len(full)); resp.Header.Get("Content-Range") != want {
		t.Fatalf("Content-Range %q, want %q", resp.Header.Get("Content-Range"), want)
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
