package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Stream processado (remux/transcode) com Range.
//
// Mesmo com "Accept-Ranges: none" o Chrome larga a conexão quando o buffer enche e volta pedindo
// "Range: bytes=N-". Sem Range de verdade cada volta reiniciava o ffmpeg do zero e o player ficava
// parado até o ffmpeg regerar N bytes (no fim de um episódio, dezenas de segundos). Agora o ffmpeg
// grava num arquivo temporário (a saída é determinística para os mesmos parâmetros) e cada pedido
// lê dele: o que já foi gerado sai na hora, o resto é entregue conforme o ffmpeg avança.

var (
	procDir       = filepath.Join(os.TempDir(), "drivelocal-streams")
	procIdleGrace = 10 * time.Minute // sem leitores por esse tempo: mata o ffmpeg (libera a vaga)
	procKeep      = 30 * time.Minute // sem leitores por esse tempo: apaga o arquivo
	procMaxJobs   = 6                // arquivos guardados; os ociosos mais antigos saem primeiro
	procChunk     = int64(4 << 20)   // pedido com Range e ffmpeg ainda gerando: entrega até esse tanto
	// teto de disco somando todos os streams (inclusive os em uso): acima dele, apaga os ociosos e,
	// se não bastar, recusa stream novo (503) em vez de encher o disco
	procMaxBytes = int64(20 << 30)

	procMu   sync.Mutex
	procJobs = map[string]*procJob{}
	procSeq  int64
)

type procJob struct {
	key, file, full, mode string
	slots                 chan struct{}
	ctx                   context.Context
	cancel                context.CancelFunc
	seq                   int64

	mu         sync.Mutex
	size       int64
	done       bool
	failed     bool
	stderr     string
	changed    chan struct{} // fechado (e trocado) a cada bloco gravado
	readers    int
	idleSince  time.Time
	superseded bool // outro stream do mesmo arquivo começou depois (seek, troca de faixa)
}

// state devolve tamanho/fim atuais e o canal que avisa da próxima mudança.
func (j *procJob) state() (int64, bool, chan struct{}) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.size, j.done, j.changed
}

func (j *procJob) notify() {
	close(j.changed)
	j.changed = make(chan struct{})
}

// waitFor espera o arquivo passar de n bytes ou o ffmpeg terminar.
func (j *procJob) waitFor(ctx context.Context, n int64) (int64, bool, error) {
	for {
		size, done, ch := j.state()
		if size > n || done {
			return size, done, nil
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return size, done, ctx.Err()
		}
	}
}

func (j *procJob) isFailed() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.failed
}

func (j *procJob) acquire() {
	j.mu.Lock()
	j.readers++
	j.mu.Unlock()
}

func (j *procJob) release() {
	j.mu.Lock()
	j.readers--
	idle := j.readers == 0
	if idle {
		j.idleSince = time.Now()
	}
	sup := j.superseded
	j.mu.Unlock()
	if !idle {
		return
	}
	if sup {
		j.cancel()
		return
	}
	time.AfterFunc(procIdleGrace, func() {
		if j.idleFor() >= procIdleGrace {
			j.cancel()
		}
	})
}

// idleFor: há quanto tempo ninguém lê (0 se alguém está lendo).
func (j *procJob) idleFor() time.Duration {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.readers > 0 {
		return 0
	}
	return time.Since(j.idleSince)
}

func procKey(full string, info os.FileInfo, mode string, audio int, start float64, delayMs int) string {
	return fmt.Sprintf("%s|%d|%d|%s|%d|%s|%d", full, info.Size(), info.ModTime().UnixNano(), mode, audio,
		strconv.FormatFloat(start, 'f', 3, 64), delayMs)
}

var errProcBusy = errors.New("sem vaga para o ffmpeg")

// procGet devolve o stream já existente para a chave ou começa um novo (já com um leitor).
// fresh=true recomeça mesmo havendo um (ex.: arquivo incompleto de um ffmpeg morto por ociosidade).
func procGet(ctx context.Context, key, full, mode string, slots chan struct{}, args []string, fresh bool) (*procJob, error) {
	procMu.Lock()
	if j := procJobs[key]; j != nil && !fresh && !j.isFailed() {
		j.acquire()
		procMu.Unlock()
		return j, nil
	}
	underCap := procTrim()
	procMu.Unlock()
	if !underCap {
		logWarn("streams processados passaram de %d MB em disco; recusando stream novo", procMaxBytes>>20)
		return nil, errProcBusy
	}

	if !procAcquireSlot(ctx, slots) {
		return nil, errProcBusy
	}
	if err := procEnsureDir(); err != nil {
		<-slots
		return nil, err
	}
	f, err := os.CreateTemp(procDir, "stream-*.mp4")
	if err != nil {
		<-slots
		return nil, err
	}
	jctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	cmd := exec.CommandContext(jctx, "ffmpeg", args...)
	cmd.WaitDelay = 5 * time.Second // não fica preso se o ffmpeg demorar a sair após o kill
	stdout, err := cmd.StdoutPipe()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		cancel()
		f.Close()
		os.Remove(f.Name())
		<-slots
		logError("ffmpeg %s start failed: %v", mode, err)
		return nil, err
	}

	procMu.Lock()
	procSeq++
	j := &procJob{key: key, file: f.Name(), full: full, mode: mode, slots: slots, ctx: jctx, cancel: cancel, seq: procSeq,
		changed: make(chan struct{}), readers: 1}
	if old := procJobs[key]; old != nil {
		procDrop(old)
	}
	procJobs[key] = j
	// streams anteriores do mesmo arquivo viraram passado: morrem assim que ninguém estiver lendo
	for _, o := range procJobs {
		if o != j && o.full == full {
			o.mu.Lock()
			o.superseded = true
			idle := o.readers == 0
			o.mu.Unlock()
			if idle {
				o.cancel()
			}
		}
	}
	procTrim()
	procMu.Unlock()

	go j.run(cmd, stdout, f, &stderr)
	return j, nil
}

// procAcquireSlot: como acquireSlot, mas antes de desistir mata um ffmpeg ocioso que esteja na vaga.
func procAcquireSlot(ctx context.Context, slots chan struct{}) bool {
	select {
	case slots <- struct{}{}:
		return true
	default:
	}
	procMu.Lock()
	var idle *procJob
	for _, j := range procJobs {
		if j.slots != slots {
			continue
		}
		j.mu.Lock()
		free := j.readers == 0 && !j.done
		j.mu.Unlock()
		if free && (idle == nil || j.seq < idle.seq) {
			idle = j
		}
	}
	procMu.Unlock()
	if idle != nil {
		idle.cancel()
	}
	return acquireSlot(ctx, slots)
}

func (j *procJob) run(cmd *exec.Cmd, stdout io.Reader, f *os.File, stderr *bytes.Buffer) {
	buf := make([]byte, 256*1024)
	for {
		n, rerr := stdout.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				j.cancel()
				break
			}
			j.mu.Lock()
			j.size += int64(n)
			j.notify()
			j.mu.Unlock()
		}
		if rerr != nil {
			break
		}
	}
	waitErr := cmd.Wait()
	f.Close()
	<-j.slots
	j.cancel()

	j.mu.Lock()
	j.done = true
	j.stderr = strings.TrimSpace(stderr.String())
	// morto (ociosidade, seek) ou falhou: o arquivo está incompleto e não serve para outro pedido
	j.failed = waitErr != nil
	j.notify()
	size := j.size
	j.mu.Unlock()

	if waitErr != nil {
		if size > 0 && j.ctx.Err() == nil {
			logWarn("ffmpeg %s ended with error for %s: %v: %s", j.mode, j.full, waitErr, j.stderr)
		}
		procMu.Lock()
		if procJobs[j.key] == j {
			j.mu.Lock()
			idle := j.readers == 0
			j.mu.Unlock()
			if idle {
				procDrop(j)
			}
		}
		procMu.Unlock()
	}
	time.AfterFunc(procKeep, func() {
		procMu.Lock()
		defer procMu.Unlock()
		if procJobs[j.key] == j && j.idleFor() >= procKeep {
			procDrop(j)
		}
	})
}

// procDrop tira o stream da tabela e apaga o arquivo (quem ainda lê fica com o descritor aberto).
// Chamar com procMu.
func procDrop(j *procJob) {
	if procJobs[j.key] == j {
		delete(procJobs, j.key)
	}
	j.cancel()
	os.Remove(j.file)
}

// procBytes soma o que todos os streams ocupam em disco. Chamar com procMu.
func procBytes() int64 {
	var total int64
	for _, j := range procJobs {
		j.mu.Lock()
		total += j.size
		j.mu.Unlock()
	}
	return total
}

// procTrim apaga os streams ociosos mais antigos enquanto houver mais de procMaxJobs ou mais de
// procMaxBytes em disco. false: ainda acima do teto de disco (só sobraram streams em uso). Chamar com procMu.
func procTrim() bool {
	for len(procJobs) > procMaxJobs || procBytes() > procMaxBytes {
		var oldest *procJob
		for _, j := range procJobs {
			if j.idleFor() > 0 && (oldest == nil || j.seq < oldest.seq) {
				oldest = j
			}
		}
		if oldest == nil {
			return procBytes() <= procMaxBytes
		}
		procDrop(oldest)
	}
	return true
}

// parseRangeStart lê "bytes=N-" (o único formato que o <video> manda). ok=false: sem Range útil.
func parseRangeStart(h string) (int64, bool) {
	h = strings.TrimSpace(h)
	if !strings.HasPrefix(h, "bytes=") || strings.Contains(h, ",") {
		return 0, false
	}
	from, _, found := strings.Cut(strings.TrimPrefix(h, "bytes="), "-")
	if !found {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(from), 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// serveProc entrega o stream a partir do Range pedido. Sem Range (ou bytes=0-): 200 com tudo,
// acompanhando o ffmpeg. Com Range: 206 até o fim (se pronto) ou até procChunk bytes já gerados,
// com tamanho total "*" enquanto o ffmpeg não termina; o <video> pede o resto em seguida.
func serveProc(w http.ResponseWriter, r *http.Request, key, full, mode string, slots chan struct{}, args []string) {
	from, ranged := parseRangeStart(r.Header.Get("Range"))
	if from == 0 {
		ranged = false
	}
	var j *procJob
	for attempt := 0; ; attempt++ {
		var err error
		j, err = procGet(r.Context(), key, full, mode, slots, args, attempt > 0)
		if errors.Is(err, errProcBusy) {
			w.Header().Set("Retry-After", "5")
			http.Error(w, `{"error":"Too many concurrent transcodes"}`, http.StatusServiceUnavailable)
			return
		}
		if err != nil {
			http.Error(w, `{"error":"Failed to start transcode"}`, http.StatusInternalServerError)
			return
		}
		size, done, werr := j.waitFor(r.Context(), from)
		if werr != nil {
			j.release()
			return
		}
		failed := j.isFailed()
		// ffmpeg morto antes de chegar onde o pedido quer: recomeça uma vez do zero
		if done && failed && size <= from && attempt == 0 && r.Context().Err() == nil {
			j.release()
			continue
		}
		if size == 0 {
			j.release()
			logWarn("ffmpeg %s produced no output for %s: %s", mode, full, j.stderr)
			http.Error(w, `{"error":"Failed to process media"}`, http.StatusUnprocessableEntity)
			return
		}
		if size <= from {
			j.release()
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
			http.Error(w, `{"error":"Range not satisfiable"}`, http.StatusRequestedRangeNotSatisfiable)
			return
		}
		break
	}
	defer j.release()

	f, err := os.Open(j.file)
	if err != nil {
		// arquivo já apagado (limpeza): tira o stream da tabela para o próximo pedido refazer
		procMu.Lock()
		if procJobs[j.key] == j {
			procDrop(j)
		}
		procMu.Unlock()
		http.Error(w, `{"error":"Failed to process media"}`, http.StatusInternalServerError)
		return
	}
	defer f.Close()

	h := w.Header()
	h.Set("Content-Type", "video/mp4")
	h.Set("Cache-Control", "no-store")
	h.Set("Accept-Ranges", "bytes")
	h.Set("X-Playback-Mode", mode)
	h.Set("X-Stream-Lead", strconv.FormatFloat(streamLead, 'f', -1, 64))

	end := int64(-1) // último byte a entregar (-1: até o ffmpeg terminar)
	size, done, _ := j.state()
	// ffmpeg morto no meio: o arquivo parcial não é o vídeo inteiro, então não informa o tamanho total
	partial := done && j.isFailed()
	if done {
		end = size - 1
	}
	if ranged {
		if !done {
			// espera juntar um bloco razoável para não virar uma enxurrada de pedidos minúsculos
			size, done, _ = j.waitFor(r.Context(), from+procChunk-1)
			end = min(size, from+procChunk) - 1
		}
		total := "*"
		if done && !partial {
			total = strconv.FormatInt(size, 10)
		}
		h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%s", from, end, total))
		h.Set("Content-Length", strconv.FormatInt(end-from+1, 10))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		if end >= 0 {
			h.Set("Content-Length", strconv.FormatInt(end+1, 10))
		}
		w.WriteHeader(http.StatusOK)
	}
	if r.Method == http.MethodHead {
		return
	}

	out := flushWriter{w}
	buf := make([]byte, 256*1024)
	pos := from
	if !ranged {
		pos = 0
	}
	for end < 0 || pos <= end {
		size, done, ch := j.state()
		if pos >= size {
			if done {
				return
			}
			select {
			case <-ch:
				continue
			case <-r.Context().Done():
				return
			}
		}
		want := size - pos
		if end >= 0 {
			want = min(want, end-pos+1)
		}
		n, rerr := f.ReadAt(buf[:min(int64(len(buf)), want)], pos)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return
			}
			pos += int64(n)
		}
		if rerr != nil && rerr != io.EOF {
			return
		}
	}
}

// procReset apaga os streams guardados (início do servidor: sobras de uma execução anterior).
func procReset() {
	os.RemoveAll(procDir)
}

// procEnsureDir cria a pasta só para este usuário e recusa link simbólico no lugar dela
// (o /tmp é compartilhado: outro usuário poderia ler os streams ou apontar a pasta para outro lugar).
func procEnsureDir() error {
	if err := os.Mkdir(procDir, 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	st, err := os.Lstat(procDir)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("pasta de streams inválida: %s", procDir)
	}
	return os.Chmod(procDir, 0o700)
}
