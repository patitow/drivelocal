package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// httpFakeDocker simula o Docker: Up copia os mounts do override para o "container".
// Se gate não for nil, Up espera o canal antes de aplicar (para testar 409 busy).
type httpFakeDocker struct {
	overridePath string
	gate         chan struct{}

	mu     sync.Mutex
	mounts []ContainerMount
}

func (f *httpFakeDocker) BootID() (string, error) { return "boot-1", nil }
func (f *httpFakeDocker) ConfigCheck() error      { return nil }
func (f *httpFakeDocker) WaitHealthy(string) (string, error) {
	return "boot-2", nil
}

func (f *httpFakeDocker) Up() error {
	if f.gate != nil {
		<-f.gate
	}
	o, err := LoadOverride(f.overridePath)
	if err != nil {
		return err
	}
	var ms []ContainerMount
	for _, m := range o.Mounts() {
		ms = append(ms, ContainerMount{Destination: m.Target, RW: !m.ReadOnly})
	}
	f.mu.Lock()
	f.mounts = ms
	f.mu.Unlock()
	return nil
}

func (f *httpFakeDocker) InspectMounts() ([]ContainerMount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ContainerMount(nil), f.mounts...), nil
}

type httpEnv struct {
	t      *testing.T
	ts     *httptest.Server
	base   string // pasta temporária com proj/ e pastas de teste
	proj   string
	music  string // base/musica (selecionável)
	other  string // base/outra/musica (selecionável, mesmo nome-base)
	fake   *httpFakeDocker
	runner *Runner
}

func newHTTPEnv(t *testing.T) *httpEnv {
	t.Helper()
	base := t.TempDir()
	proj := filepath.Join(base, "proj")
	dataDir := filepath.Join(proj, "host-helper", "data")
	overridePath := filepath.Join(proj, "docker-compose.override.yml")
	music := filepath.Join(base, "musica")
	other := filepath.Join(base, "outra", "musica")
	for _, d := range []string{proj, music, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	fake := &httpFakeDocker{overridePath: overridePath}
	runner, err := NewRunner(overridePath, filepath.Join(dataDir, "backups"), dataDir, fake)
	if err != nil {
		t.Fatal(err)
	}
	runner.Delay = 0

	srv := &server{
		token:        []byte(testToken),
		runner:       runner,
		overridePath: overridePath,
		deny:         DenyConfig{ProjectDir: proj, HelperDataDir: dataDir},
	}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return &httpEnv{t: t, ts: ts, base: base, proj: proj, music: music, other: other, fake: fake, runner: runner}
}

// do faz uma request autenticada. body: nil, []byte (bruto) ou valor JSON.
func (e *httpEnv) do(method, path string, body any, headers map[string]string) (int, []byte) {
	e.t.Helper()
	var rd *strings.Reader
	switch b := body.(type) {
	case nil:
		rd = strings.NewReader("")
	case []byte:
		rd = strings.NewReader(string(b))
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(method, e.ts.URL+path, rd)
	if err != nil {
		e.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf strings.Builder
	b := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(b)
		buf.Write(b[:n])
		if rerr != nil {
			break
		}
	}
	return resp.StatusCode, []byte(buf.String())
}

func (e *httpEnv) expectError(status int, code string, method, path string, body any) {
	e.t.Helper()
	got, raw := e.do(method, path, body, nil)
	if got != status {
		e.t.Fatalf("%s %s: status %d, quer %d (%s)", method, path, got, status, raw)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil || v["error"] != code {
		e.t.Fatalf("%s %s: erro %q, quer %q (%s)", method, path, v["error"], code, raw)
	}
}

func (e *httpEnv) revision() string {
	e.t.Helper()
	_, raw := e.do("GET", "/v1/mounts", nil, nil)
	var v struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(raw, &v); err != nil || v.Revision == "" {
		e.t.Fatalf("GET /v1/mounts sem revision: %s", raw)
	}
	return v.Revision
}

// createMount faz POST /v1/mounts e espera 202; devolve o jobId.
func (e *httpEnv) createMount(hostPath, name string) string {
	e.t.Helper()
	status, raw := e.do("POST", "/v1/mounts", map[string]string{
		"hostPath": hostPath, "name": name, "baseRevision": e.revision(),
	}, nil)
	if status != http.StatusAccepted {
		e.t.Fatalf("POST /v1/mounts: %d %s", status, raw)
	}
	var v struct {
		JobID string `json:"jobId"`
	}
	_ = json.Unmarshal(raw, &v)
	return v.JobID
}

// waitJob consulta /v1/jobs/{id} até o estado final.
func (e *httpEnv) waitJob(id string) string {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, raw := e.do("GET", "/v1/jobs/"+id, nil, nil)
		var v struct {
			State string `json:"state"`
		}
		_ = json.Unmarshal(raw, &v)
		switch v.State {
		case StateDone, StateRolledBack, StateRollbackFailed:
			return v.State
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("job %s não terminou (último estado %q)", id, v.State)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (e *httpEnv) mountNames() []string {
	e.t.Helper()
	_, raw := e.do("GET", "/v1/mounts", nil, nil)
	var v struct {
		Mounts []Mount `json:"mounts"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		e.t.Fatal(err)
	}
	var names []string
	for _, m := range v.Mounts {
		names = append(names, m.Name)
	}
	return names
}

func TestAuthRequired(t *testing.T) {
	e := newHTTPEnv(t)
	status, _ := e.do("GET", "/v1/health", nil, map[string]string{"Authorization": ""})
	if status != http.StatusUnauthorized {
		t.Fatalf("sem token: %d", status)
	}
	status, _ = e.do("GET", "/v1/health", nil, map[string]string{"Authorization": "Bearer nope"})
	if status != http.StatusUnauthorized {
		t.Fatalf("token errado: %d", status)
	}
	status, _ = e.do("GET", "/v1/health", nil, map[string]string{"Authorization": "Basic " + testToken})
	if status != http.StatusUnauthorized {
		t.Fatalf("esquema errado: %d", status)
	}
}

func TestOriginHeaderRejected(t *testing.T) {
	e := newHTTPEnv(t)
	status, raw := e.do("GET", "/v1/health", nil, map[string]string{"Origin": "http://evil.example"})
	if status != http.StatusForbidden {
		t.Fatalf("Origin: status %d (%s)", status, raw)
	}
}

func TestHealthAndMountsList(t *testing.T) {
	e := newHTTPEnv(t)
	status, raw := e.do("GET", "/v1/health", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(raw), `"busy":false`) {
		t.Fatalf("health: %d %s", status, raw)
	}
	_, raw = e.do("GET", "/v1/mounts", nil, nil)
	var v struct {
		Revision string  `json:"revision"`
		Mounts   []Mount `json:"mounts"`
		Active   *Job    `json:"activeJob"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Mounts) != 0 || v.Active != nil || len(v.Revision) != 64 {
		t.Fatalf("mounts inicial: %s", raw)
	}
}

func TestBodyLimitAndJSONOnly(t *testing.T) {
	e := newHTTPEnv(t)
	big := `{"hostPath":"` + strings.Repeat("a", 9000) + `"}`
	e.expectError(http.StatusRequestEntityTooLarge, "too_large", "POST", "/v1/mounts", []byte(big))

	status, raw := e.do("POST", "/v1/mounts", []byte(`{}`), map[string]string{"Content-Type": "text/plain"})
	if status != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain: %d %s", status, raw)
	}
	e.expectError(http.StatusBadRequest, "invalid_body", "POST", "/v1/mounts", []byte(`{"hostPath":`))
}

func TestBrowseDrives(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drives só existem no Windows")
	}
	e := newHTTPEnv(t)
	_, raw := e.do("GET", "/v1/browse", nil, nil)
	var v browseResponse
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Entries) == 0 {
		t.Fatal("nenhuma unidade listada")
	}
	drive := regexp.MustCompile(`^[A-Z]:$`)
	for _, en := range v.Entries {
		if !drive.MatchString(en.Name) || en.Selectable {
			t.Fatalf("unidade inesperada: %+v", en)
		}
	}
}

func TestBrowseFolderSelectableAndReasons(t *testing.T) {
	e := newHTTPEnv(t)
	q := "/v1/browse?path=" + strings.ReplaceAll(e.base, `\`, `%5C`)
	status, raw := e.do("GET", q, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("browse: %d %s", status, raw)
	}
	var v browseResponse
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	byName := map[string]browseEntry{}
	for _, en := range v.Entries {
		byName[en.Name] = en
	}
	if m, ok := byName["musica"]; !ok || !m.Selectable || m.Reason != "" {
		t.Fatalf("musica deveria ser selecionável: %+v", byName["musica"])
	}
	p, ok := byName["proj"]
	if !ok || p.Selectable || !strings.Contains(p.Reason, "projeto") {
		t.Fatalf("proj (pasta do projeto) deveria ser bloqueada: %+v", p)
	}
}

func TestBrowseInvalidPaths(t *testing.T) {
	e := newHTTPEnv(t)
	for _, p := range []string{`relativo\x`, `\\servidor\share`, `E:\x?`} {
		e.expectError(http.StatusBadRequest, "invalid_path", "GET", "/v1/browse?path="+strings.NewReplacer(`\`, `%5C`, "?", "%3F").Replace(p), nil)
	}
	e.expectError(http.StatusBadRequest, "invalid_path", "GET", "/v1/browse?path="+strings.ReplaceAll(filepath.Join(e.base, "nao-existe"), `\`, `%5C`), nil)
}

func TestCreateMountFlow(t *testing.T) {
	e := newHTTPEnv(t)

	// sucesso: aplica, verifica e aparece na lista
	id := e.createMount(e.music, "")
	if st := e.waitJob(id); st != StateDone {
		t.Fatalf("job terminou em %s", st)
	}
	if names := e.mountNames(); len(names) != 1 || names[0] != "musica" {
		t.Fatalf("nomes após add: %v", names)
	}

	// nome automático em colisão vira "musica (2)"
	id = e.createMount(e.other, "")
	if st := e.waitJob(id); st != StateDone {
		t.Fatalf("segundo job terminou em %s", st)
	}
	if names := e.mountNames(); len(names) != 2 || names[1] != "musica (2)" {
		t.Fatalf("nomes após colisão automática: %v", names)
	}

	// mesma pasta de novo: already_mounted
	rev := e.revision()
	e.expectError(http.StatusConflict, "already_mounted", "POST", "/v1/mounts",
		map[string]string{"hostPath": e.music, "name": "outra", "baseRevision": rev})

	// nome explícito colidindo (case-insensitive) com pasta ainda não montada: 422 name_conflict
	third := filepath.Join(e.base, "terceira")
	if err := os.MkdirAll(third, 0o755); err != nil {
		t.Fatal(err)
	}
	e.expectError(http.StatusUnprocessableEntity, "name_conflict", "POST", "/v1/mounts",
		map[string]string{"hostPath": third, "name": "MUSICA", "baseRevision": rev})
}

func TestCreateMountValidationErrors(t *testing.T) {
	e := newHTTPEnv(t)
	rev := e.revision()

	e.expectError(http.StatusConflict, "revision_conflict", "POST", "/v1/mounts",
		map[string]string{"hostPath": e.music, "baseRevision": "00"})
	e.expectError(http.StatusConflict, "revision_conflict", "POST", "/v1/mounts",
		map[string]string{"hostPath": e.music})

	e.expectError(http.StatusBadRequest, "invalid_path", "POST", "/v1/mounts",
		map[string]string{"hostPath": "relativo", "baseRevision": rev})
	e.expectError(http.StatusForbidden, "path_denied", "POST", "/v1/mounts",
		map[string]string{"hostPath": e.proj, "baseRevision": rev})
	e.expectError(http.StatusUnprocessableEntity, "invalid_name", "POST", "/v1/mounts",
		map[string]string{"hostPath": e.music, "name": "a/b", "baseRevision": rev})
	e.expectError(http.StatusUnprocessableEntity, "invalid_name", "POST", "/v1/mounts",
		map[string]string{"hostPath": e.music, "name": ".oculta", "baseRevision": rev})
	e.expectError(http.StatusBadRequest, "invalid_body", "POST", "/v1/mounts",
		map[string]string{"hostPath": e.music, "extra": "x", "baseRevision": rev})
}

func TestBusyWhileJobRuns(t *testing.T) {
	e := newHTTPEnv(t)
	e.fake.gate = make(chan struct{})
	id := e.createMount(e.music, "")

	status, raw := e.do("GET", "/v1/health", nil, nil)
	if status != http.StatusOK || !strings.Contains(string(raw), `"busy":true`) {
		t.Fatalf("health durante job: %d %s", status, raw)
	}
	status, raw = e.do("POST", "/v1/mounts", map[string]string{
		"hostPath": e.other, "baseRevision": e.revision(),
	}, nil)
	var v struct {
		Error string `json:"error"`
		JobID string `json:"jobId"`
	}
	_ = json.Unmarshal(raw, &v)
	if status != http.StatusConflict || v.Error != "busy" || v.JobID != id {
		t.Fatalf("segundo POST durante job: %d %s", status, raw)
	}

	close(e.fake.gate)
	if st := e.waitJob(id); st != StateDone {
		t.Fatalf("job terminou em %s", st)
	}
}

func TestDeleteMount(t *testing.T) {
	e := newHTTPEnv(t)
	if st := e.waitJob(e.createMount(e.music, "")); st != StateDone {
		t.Fatalf("add terminou em %s", st)
	}

	e.expectError(http.StatusConflict, "revision_conflict", "DELETE", "/v1/mounts/musica?baseRevision=00", nil)
	e.expectError(http.StatusNotFound, "not_found", "DELETE", "/v1/mounts/nao-existe?baseRevision="+e.revision(), nil)

	status, raw := e.do("DELETE", "/v1/mounts/musica?baseRevision="+e.revision(), nil, nil)
	if status != http.StatusAccepted {
		t.Fatalf("delete: %d %s", status, raw)
	}
	var v struct {
		JobID string `json:"jobId"`
	}
	_ = json.Unmarshal(raw, &v)
	if st := e.waitJob(v.JobID); st != StateDone {
		t.Fatalf("remove terminou em %s", st)
	}
	if names := e.mountNames(); len(names) != 0 {
		t.Fatalf("ainda há bibliotecas: %v", names)
	}
}

func TestGetJobUnknown(t *testing.T) {
	e := newHTTPEnv(t)
	e.expectError(http.StatusNotFound, "not_found", "GET", "/v1/jobs/deadbeef", nil)
}

func TestCheckLoopbackListen(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:47123", "[::1]:47123", "127.0.0.2:1"} {
		if err := checkLoopbackListen(ok); err != nil {
			t.Errorf("%s deveria ser aceito: %v", ok, err)
		}
	}
	for _, bad := range []string{"0.0.0.0:47123", ":47123", "192.168.0.10:47123", "localhost:47123", "47123"} {
		if err := checkLoopbackListen(bad); err == nil {
			t.Errorf("%s deveria ser recusado", bad)
		}
	}
}

func TestUpsertToken(t *testing.T) {
	valid := strings.Repeat("ab", 32)
	// sem arquivo: acrescenta
	out, changed := upsertToken("", valid)
	if !changed || out != "MOUNT_HELPER_TOKEN="+valid+"\n" {
		t.Fatalf("vazio: %q %v", out, changed)
	}
	// token válido já existe: não mexe
	in := "A=1\nMOUNT_HELPER_TOKEN=" + valid + "\n"
	if out, changed := upsertToken(in, strings.Repeat("cd", 32)); changed || out != in {
		t.Fatalf("válido existente foi alterado: %q", out)
	}
	// token inválido e duplicado: substitui sem duplicar, preservando o resto
	in = "A=1\r\nMOUNT_HELPER_TOKEN=\r\nMOUNT_HELPER_TOKEN=x\r\nB=2\r\n"
	out, changed = upsertToken(in, valid)
	if !changed || strings.Count(out, "MOUNT_HELPER_TOKEN=") != 1 || !strings.Contains(out, "A=1\r\nB=2\r\n") {
		t.Fatalf("substituição: %q", out)
	}
	if !strings.HasSuffix(out, "MOUNT_HELPER_TOKEN="+valid+"\r\n") {
		t.Fatalf("token não está no fim com CRLF: %q", out)
	}
}

func TestRunInitTwiceDoesNotDuplicate(t *testing.T) {
	proj := t.TempDir()
	if err := runInit([]string{"--project-dir", proj}); err != nil {
		t.Fatal(err)
	}
	if err := runInit([]string{"--project-dir", proj}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(proj, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "MOUNT_HELPER_TOKEN=") != 1 {
		t.Fatalf(".env com token duplicado: %q", data)
	}
	if !validToken(readEnvValue(filepath.Join(proj, ".env"), "MOUNT_HELPER_TOKEN")) {
		t.Fatalf("token gravado é inválido: %q", data)
	}
}
