package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDocker simula compose/docker. Ao Up bem-sucedido, o estado do "container"
// passa a refletir o override em disco (a menos que drift esteja ligado).
type fakeDocker struct {
	mu sync.Mutex

	overridePath string
	bootID       string
	upCalls      int
	upFn         func(n int) error // n = número da chamada (1-based)
	configErr    error
	healthErr    error
	healthFn     func(n int) error // n = número da chamada de WaitHealthy (1-based)
	healthCalls  int
	drift        bool // se true, o container ignora o override ao subir
	mounts       []ContainerMount
}

func (f *fakeDocker) BootID() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bootID, nil
}

func (f *fakeDocker) ConfigCheck() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.configErr
}

func (f *fakeDocker) Up() error {
	f.mu.Lock()
	f.upCalls++
	n := f.upCalls
	fn := f.upFn
	f.mu.Unlock()

	if fn != nil {
		if err := fn(n); err != nil {
			return err
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.bootID = fmt.Sprintf("boot-%d", n)
	src := f.overridePath
	if f.drift {
		return nil
	}
	o, err := LoadOverride(src)
	if err != nil {
		return err
	}
	f.mounts = nil
	for _, m := range o.Mounts() {
		f.mounts = append(f.mounts, ContainerMount{Destination: m.Target, Source: m.HostPath, RW: !m.ReadOnly})
	}
	return nil
}

func (f *fakeDocker) InspectMounts() ([]ContainerMount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ContainerMount(nil), f.mounts...), nil
}

func (f *fakeDocker) WaitHealthy(prev string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.healthCalls++
	if f.healthFn != nil {
		if err := f.healthFn(f.healthCalls); err != nil {
			return "", err
		}
	}
	if f.healthErr != nil {
		return "", f.healthErr
	}
	if f.bootID == prev {
		return "", errors.New("bootId não mudou")
	}
	return f.bootID, nil
}

func (f *fakeDocker) upCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.upCalls
}

// jobsTestEnv cria um override temporário (cópia de testdata/basic.yml) e um
// Runner com delay zero. Devolve também o fake e os diretórios.
type jobsTestEnv struct {
	r            *Runner
	fake         *fakeDocker
	overridePath string
	backupDir    string
	dataDir      string
	original     []byte
}

func newJobsTestEnv(t *testing.T, fake *fakeDocker) *jobsTestEnv {
	t.Helper()
	dir := t.TempDir()
	env := &jobsTestEnv{
		overridePath: filepath.Join(dir, "docker-compose.override.yml"),
		backupDir:    filepath.Join(dir, "backups"),
		dataDir:      filepath.Join(dir, "data"),
		original:     readFixture(t, "basic.yml"),
	}
	if err := os.WriteFile(env.overridePath, env.original, 0o644); err != nil {
		t.Fatal(err)
	}
	if fake == nil {
		fake = &fakeDocker{}
	}
	fake.overridePath = env.overridePath
	fake.bootID = "boot-0"
	// o container começa rodando o override original (mesmo com drift ligado)
	if fake.mounts == nil {
		o, err := LoadOverride(env.overridePath)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range o.Mounts() {
			fake.mounts = append(fake.mounts, ContainerMount{Destination: m.Target, Source: m.HostPath, RW: !m.ReadOnly})
		}
	}
	env.fake = fake

	r, err := NewRunner(env.overridePath, env.backupDir, env.dataDir, fake)
	if err != nil {
		t.Fatal(err)
	}
	r.Delay = 0
	env.r = r
	return env
}

func (e *jobsTestEnv) revision(t *testing.T) string {
	t.Helper()
	o, err := LoadOverride(e.overridePath)
	if err != nil {
		t.Fatal(err)
	}
	return o.Revision()
}

func (e *jobsTestEnv) fileBytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(e.overridePath)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// waitJob espera o job chegar a um estado final.
func waitJob(t *testing.T, r *Runner, id string) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		j, ok := r.Get(id)
		if !ok {
			t.Fatalf("job %s não encontrado", id)
		}
		switch j.State {
		case StateDone, StateRolledBack, StateRollbackFailed:
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s travado em %q", id, j.State)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func addOp(rev string) Op {
	return Op{Kind: OpAdd, Name: "Novo", HostPath: `F:\Novo`, BaseRevision: rev}
}

func TestJobAddSucceeds(t *testing.T) {
	env := newJobsTestEnv(t, nil)
	before := env.fileBytes(t)

	id, err := env.r.Start(addOp(env.revision(t)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	j := waitJob(t, env.r, id)
	if j.State != StateDone || j.Error != "" {
		t.Fatalf("estado = %q (%s), want done", j.State, j.Error)
	}
	if j.FinishedAt == nil || j.Op != OpAdd || j.Name != "Novo" {
		t.Fatalf("job incompleto: %+v", j)
	}
	if env.fake.upCount() != 1 {
		t.Fatalf("Up chamado %d vezes, want 1", env.fake.upCount())
	}
	if bytes.Equal(env.fileBytes(t), before) {
		t.Fatal("override não foi alterado")
	}
	o, _ := LoadOverride(env.overridePath)
	found := false
	for _, m := range o.Mounts() {
		if m.Target == "/data/Novo" && m.HostPath == `F:\Novo` && m.ReadOnly {
			found = true
		}
	}
	if !found {
		t.Fatal("biblioteca nova não está no override")
	}
	backups, _ := filepath.Glob(filepath.Join(env.backupDir, "*"+backupSuffix))
	if len(backups) != 1 {
		t.Fatalf("backups = %d, want 1", len(backups))
	}
	if data, _ := os.ReadFile(backups[0]); !bytes.Equal(data, before) {
		t.Fatal("backup não tem o conteúdo original")
	}
	if env.r.ActiveJobID() != "" {
		t.Fatal("job ativo depois de done")
	}
}

func TestJobRemoveSucceeds(t *testing.T) {
	env := newJobsTestEnv(t, nil)
	id, err := env.r.Start(Op{Kind: OpRemove, Name: "Filmes", BaseRevision: env.revision(t)})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if j := waitJob(t, env.r, id); j.State != StateDone {
		t.Fatalf("estado = %q (%s), want done", j.State, j.Error)
	}
	o, _ := LoadOverride(env.overridePath)
	for _, m := range o.Mounts() {
		if m.Name == "Filmes" {
			t.Fatal("Filmes ainda está no override")
		}
	}
}

func TestUpFailureRollsBack(t *testing.T) {
	env := newJobsTestEnv(t, &fakeDocker{upFn: func(n int) error {
		if n == 1 {
			return errors.New("boom")
		}
		return nil
	}})
	original := env.fileBytes(t)

	id, err := env.r.Start(addOp(env.revision(t)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	j := waitJob(t, env.r, id)
	if j.State != StateRolledBack {
		t.Fatalf("estado = %q, want rolled_back", j.State)
	}
	if env.fake.upCount() != 2 {
		t.Fatalf("Up chamado %d vezes, want 2 (aplicar + restaurar)", env.fake.upCount())
	}
	if !bytes.Equal(env.fileBytes(t), original) {
		t.Fatal("override não foi restaurado byte a byte")
	}
}

func TestHealthTimeoutRollsBack(t *testing.T) {
	// só a primeira espera de saúde falha; o rollback deve ficar saudável
	env := newJobsTestEnv(t, &fakeDocker{healthFn: func(n int) error {
		if n == 1 {
			return errors.New("timeout de saúde")
		}
		return nil
	}})
	original := env.fileBytes(t)

	id, err := env.r.Start(addOp(env.revision(t)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	j := waitJob(t, env.r, id)
	if j.State != StateRolledBack {
		t.Fatalf("estado = %q (%s), want rolled_back", j.State, j.Error)
	}
	if !bytes.Equal(env.fileBytes(t), original) {
		t.Fatal("override não foi restaurado")
	}
	if env.fake.healthCalls != 2 {
		t.Fatalf("WaitHealthy chamado %d vezes, want 2 (aplicar + verificar rollback)", env.fake.healthCalls)
	}
}

func TestRollbackUnhealthyIsRollbackFailed(t *testing.T) {
	env := newJobsTestEnv(t, &fakeDocker{healthErr: errors.New("servidor fora")})

	id, err := env.r.Start(addOp(env.revision(t)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	j := waitJob(t, env.r, id)
	if j.State != StateRollbackFailed {
		t.Fatalf("estado = %q (%s), want rollback_failed", j.State, j.Error)
	}
	if !strings.Contains(j.Error, "rollback") || !strings.Contains(j.Error, "servidor fora") {
		t.Fatalf("mensagem sem contexto do rollback: %q", j.Error)
	}
}

func TestRollbackMountMismatchIsRollbackFailed(t *testing.T) {
	// a 1ª subida passa, a espera de saúde falha; o rollback sobe com mounts errados
	fake := &fakeDocker{
		healthFn: func(n int) error {
			if n == 1 {
				return errors.New("timeout de saúde")
			}
			return nil
		},
	}
	fake.upFn = func(n int) error {
		if n != 2 {
			return nil
		}
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.drift = true
		fake.mounts = []ContainerMount{{Destination: "/data/Errado", RW: true}}
		return nil
	}
	env := newJobsTestEnv(t, fake)

	id, err := env.r.Start(addOp(env.revision(t)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	j := waitJob(t, env.r, id)
	if j.State != StateRollbackFailed {
		t.Fatalf("estado = %q (%s), want rollback_failed", j.State, j.Error)
	}
	if !strings.Contains(j.Error, "mounts divergentes") {
		t.Fatalf("mensagem = %q, quer menção a mounts divergentes", j.Error)
	}
}

func TestDivergentMountsRollBack(t *testing.T) {
	// o container sobe ignorando o override: mounts não batem com o desejado
	env := newJobsTestEnv(t, &fakeDocker{drift: true})
	original := env.fileBytes(t)

	id, err := env.r.Start(addOp(env.revision(t)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	j := waitJob(t, env.r, id)
	if j.State != StateRolledBack {
		t.Fatalf("estado = %q (%s), want rolled_back", j.State, j.Error)
	}
	if !bytes.Equal(env.fileBytes(t), original) {
		t.Fatal("override não foi restaurado")
	}
}

func TestRollbackFailureIsReported(t *testing.T) {
	env := newJobsTestEnv(t, &fakeDocker{upFn: func(int) error { return errors.New("docker fora") }})

	id, err := env.r.Start(addOp(env.revision(t)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	j := waitJob(t, env.r, id)
	if j.State != StateRollbackFailed {
		t.Fatalf("estado = %q, want rollback_failed", j.State)
	}
	if j.Error == "" {
		t.Fatal("rollback_failed sem mensagem de erro")
	}
}

func TestStartBusyAndRevisionChecks(t *testing.T) {
	release := make(chan struct{})
	env := newJobsTestEnv(t, &fakeDocker{upFn: func(n int) error {
		if n == 1 {
			<-release
		}
		return nil
	}})
	rev := env.revision(t)

	if _, err := env.r.Start(addOp("")); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("baseRevision vazio: err = %v, want ErrRevisionConflict", err)
	}
	if _, err := env.r.Start(addOp("0000")); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("baseRevision errada: err = %v, want ErrRevisionConflict", err)
	}

	id, err := env.r.Start(addOp(rev))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := env.r.Start(Op{Kind: OpRemove, Name: "Filmes", BaseRevision: rev}); !errors.Is(err, ErrBusy) {
		t.Fatalf("segundo Start: err = %v, want ErrBusy", err)
	}
	if active := env.r.ActiveJobID(); active != id {
		t.Fatalf("ActiveJobID = %q, want %q", active, id)
	}
	close(release)
	waitJob(t, env.r, id)
}

func TestStartValidatesAgainstOverride(t *testing.T) {
	env := newJobsTestEnv(t, nil)
	rev := env.revision(t)

	// mesmo caminho já montado (outra grafia)
	if _, err := env.r.Start(Op{Kind: OpAdd, Name: "Outro", HostPath: `e:\filmes`, BaseRevision: rev}); !errors.Is(err, ErrAlreadyMounted) {
		t.Fatalf("already mounted: err = %v", err)
	}
	// nome já existente
	if _, err := env.r.Start(Op{Kind: OpAdd, Name: "filmes", HostPath: `G:\x`, BaseRevision: rev}); err == nil {
		t.Fatal("nome duplicado aceito")
	}
	if _, err := env.r.Start(Op{Kind: OpRemove, Name: "Inexistente", BaseRevision: rev}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove inexistente: err = %v, want ErrNotFound", err)
	}
	if _, err := env.r.Start(Op{Kind: "xyz", BaseRevision: rev}); err == nil {
		t.Fatal("operação desconhecida aceita")
	}
	// nada foi escrito pelas validações
	if !bytes.Equal(env.fileBytes(t), env.original) {
		t.Fatal("validação alterou o override")
	}
	if env.fake.upCount() != 0 {
		t.Fatal("validação chamou Up")
	}
}

func TestJobsPersistAndReload(t *testing.T) {
	env := newJobsTestEnv(t, nil)
	id, err := env.r.Start(addOp(env.revision(t)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitJob(t, env.r, id)

	r2, err := NewRunner(env.overridePath, env.backupDir, env.dataDir, env.fake)
	if err != nil {
		t.Fatal(err)
	}
	j, ok := r2.Get(id)
	if !ok || j.State != StateDone {
		t.Fatalf("job recarregado: ok=%v state=%q", ok, j.State)
	}
}

func TestInterruptedJobMarkedOnReload(t *testing.T) {
	env := newJobsTestEnv(t, nil)
	stale := []Job{{ID: "abc", Op: OpAdd, Name: "X", State: StateApplying, StartedAt: time.Now().UTC()}}
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(filepath.Join(env.dataDir, jobsFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	r2, err := NewRunner(env.overridePath, env.backupDir, env.dataDir, env.fake)
	if err != nil {
		t.Fatal(err)
	}
	j, ok := r2.Get("abc")
	if !ok || j.State != StateRollbackFailed || j.Error == "" || j.FinishedAt == nil {
		t.Fatalf("job interrompido não marcado: ok=%v %+v", ok, j)
	}
	if r2.ActiveJobID() != "" {
		t.Fatal("job interrompido ficou ativo e bloqueia novas mutações")
	}
}

func TestNewRunnerRequiresDocker(t *testing.T) {
	if _, err := NewRunner("a", "b", t.TempDir(), nil); err == nil {
		t.Fatal("NewRunner aceitou Docker nil")
	}
}

func TestSameHostPath(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{`E:\Filmes`, `e:/filmes/`, true},
		{`E:\Filmes`, `E:\Filmes2`, false},
		{`E:/Meus Filmes`, `E:\Meus Filmes`, true},
	}
	for _, c := range cases {
		if got := sameHostPath(c.a, c.b); got != c.want {
			t.Errorf("sameHostPath(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
