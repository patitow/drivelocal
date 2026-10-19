package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Operações de mutação aceitas pelo Runner.
const (
	OpAdd    = "add"
	OpRemove = "remove"
)

// Estados de um job.
const (
	StateQueued         = "queued"
	StateApplying       = "applying"
	StateVerifying      = "verifying"
	StateDone           = "done"
	StateRolledBack     = "rolled_back"
	StateRollbackFailed = "rollback_failed"
)

const (
	jobsFileName = "jobs.json"
	jobsKeep     = 20
)

var (
	// ErrBusy indica que já existe um job em andamento (ver ActiveJobID).
	ErrBusy = errors.New("busy")
	// ErrRevisionConflict indica que baseRevision não é a revisão atual do override.
	ErrRevisionConflict = errors.New("revision_conflict")
	// ErrAlreadyMounted indica que o mesmo hostPath já é uma biblioteca.
	ErrAlreadyMounted = errors.New("already_mounted")
)

// ContainerMount é um item de `docker inspect <container> .Mounts`.
type ContainerMount struct {
	Destination string `json:"Destination"`
	Source      string `json:"Source"`
	RW          bool   `json:"RW"`
}

// Docker isola as chamadas ao Docker/compose para permitir testes com falso.
type Docker interface {
	// BootID devolve o bootId atual do servidor (erro se estiver fora do ar).
	BootID() (string, error)
	// ConfigCheck roda `docker compose config -q`.
	ConfigCheck() error
	// Up roda `docker compose up -d --no-build --no-deps drivelocal`.
	Up() error
	// InspectMounts devolve os mounts do container do servidor.
	InspectMounts() ([]ContainerMount, error)
	// WaitHealthy espera o servidor responder 200 com bootId diferente de prevBootID.
	WaitHealthy(prevBootID string) (string, error)
}

// Op é uma mutação pedida pela API. Para OpAdd, Name e HostPath devem já estar
// validados e normalizados pelo chamador; para OpRemove só Name é usado.
type Op struct {
	Kind         string
	Name         string
	HostPath     string
	BaseRevision string
}

// Job é o estado de uma mutação (serializado em GET /v1/jobs/{id}).
type Job struct {
	ID         string     `json:"id"`
	Op         string     `json:"op"`
	Name       string     `json:"name"`
	State      string     `json:"state"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// Runner executa uma mutação por vez: backup, escrita atômica, validação,
// `docker compose up`, espera de saúde e conferência dos mounts. Qualquer falha
// restaura o override original e roda `up` de novo.
type Runner struct {
	OverridePath string
	BackupDir    string
	DataDir      string
	Docker       Docker
	// Delay é a espera antes de aplicar (padrão 1s; testes usam 0).
	Delay time.Duration

	mu     sync.Mutex
	active string
	jobs   map[string]*Job
	order  []string
}

// NewRunner cria o Runner e carrega data/jobs.json (se existir). Jobs que
// estavam em andamento quando o helper caiu são marcados como rollback_failed.
func NewRunner(overridePath, backupDir, dataDir string, d Docker) (*Runner, error) {
	if d == nil {
		return nil, errors.New("docker nil")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	r := &Runner{
		OverridePath: overridePath,
		BackupDir:    backupDir,
		DataDir:      dataDir,
		Docker:       d,
		Delay:        time.Second,
		jobs:         map[string]*Job{},
	}
	if data, err := os.ReadFile(filepath.Join(dataDir, jobsFileName)); err == nil {
		var list []Job
		if json.Unmarshal(data, &list) == nil {
			for i := range list {
				j := list[i]
				if j.State == StateQueued || j.State == StateApplying || j.State == StateVerifying {
					j.State = StateRollbackFailed
					j.Error = "helper reiniciou durante o job; confira o override e o container"
					now := time.Now().UTC()
					j.FinishedAt = &now
				}
				r.jobs[j.ID] = &j
				r.order = append(r.order, j.ID)
			}
		}
	}
	return r, nil
}

// Start valida a operação contra o override atual e, se estiver tudo certo,
// enfileira o job e devolve o id. Erros: ErrBusy, ErrRevisionConflict,
// ErrAlreadyMounted, ErrExists (nome), ErrNotFound (remove), ou erro de validação.
func (r *Runner) Start(op Op) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.active != "" {
		return "", ErrBusy
	}
	o, err := LoadOverride(r.OverridePath)
	if err != nil {
		return "", err
	}
	if op.BaseRevision == "" || op.BaseRevision != o.Revision() {
		return "", ErrRevisionConflict
	}
	// validação a seco: nada é escrito aqui
	switch op.Kind {
	case OpAdd:
		for _, m := range o.Mounts() {
			if sameHostPath(m.HostPath, op.HostPath) {
				return "", ErrAlreadyMounted
			}
		}
		if err := o.Add(Mount{Name: op.Name, HostPath: op.HostPath}); err != nil {
			return "", err
		}
	case OpRemove:
		if err := o.Remove(op.Name); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("operação desconhecida: %q", op.Kind)
	}

	id, err := newJobID()
	if err != nil {
		return "", err
	}
	r.jobs[id] = &Job{ID: id, Op: op.Kind, Name: op.Name, State: StateQueued, StartedAt: time.Now().UTC()}
	r.order = append(r.order, id)
	for len(r.order) > jobsKeep {
		delete(r.jobs, r.order[0])
		r.order = r.order[1:]
	}
	r.active = id
	r.persistLocked()

	go r.run(id, op)
	return id, nil
}

// Get devolve uma cópia do job.
func (r *Runner) Get(id string) (Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *j, true
}

// ActiveJobID devolve o id do job em andamento ("" se não há).
func (r *Runner) ActiveJobID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

func (r *Runner) run(id string, op Op) {
	if r.Delay > 0 {
		time.Sleep(r.Delay)
	}
	state, msg := r.execute(id, op)

	r.mu.Lock()
	defer r.mu.Unlock()
	j := r.jobs[id]
	j.State = state
	j.Error = msg
	now := time.Now().UTC()
	j.FinishedAt = &now
	r.active = ""
	r.persistLocked()
}

// execute roda o fluxo sem segurar o mutex (chamadas ao Docker podem demorar).
func (r *Runner) execute(id string, op Op) (string, string) {
	r.setState(id, StateApplying, "")

	prevBoot, err := r.Docker.BootID()
	if err != nil {
		prevBoot = "" // servidor fora: sem bootId anterior para comparar
	}

	prev, err := os.ReadFile(r.OverridePath)
	existed := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return StateRolledBack, err.Error()
	}

	o, err := LoadOverride(r.OverridePath)
	if err != nil {
		return StateRolledBack, err.Error()
	}
	switch op.Kind {
	case OpAdd:
		err = o.Add(Mount{Name: op.Name, HostPath: op.HostPath})
	case OpRemove:
		err = o.Remove(op.Name)
	default:
		err = fmt.Errorf("operação desconhecida: %q", op.Kind)
	}
	if err != nil {
		return StateRolledBack, err.Error()
	}

	if _, err := WriteAtomic(r.OverridePath, o.Bytes(), r.BackupDir); err != nil {
		return r.rollback(prev, existed, fmt.Errorf("escrita do override: %w", err))
	}
	if err := r.Docker.ConfigCheck(); err != nil {
		return r.rollback(prev, existed, err)
	}
	if err := r.Docker.Up(); err != nil {
		return r.rollback(prev, existed, err)
	}

	r.setState(id, StateVerifying, "")
	if _, err := r.Docker.WaitHealthy(prevBoot); err != nil {
		return r.rollback(prev, existed, err)
	}
	if err := r.checkMounts(o); err != nil {
		return r.rollback(prev, existed, err)
	}
	return StateDone, ""
}

// rollback restaura os bytes originais, roda `up` de novo e só então confirma
// que o servidor está saudável e com os mounts do override restaurado. Se
// qualquer passo do rollback falhar, o estado é rollback_failed.
func (r *Runner) rollback(prev []byte, existed bool, cause error) (string, string) {
	var err error
	if existed {
		_, err = WriteAtomic(r.OverridePath, prev, "")
	} else if rmErr := os.Remove(r.OverridePath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		err = rmErr
	}
	if err == nil {
		err = r.Docker.Up()
	}
	if err != nil {
		return StateRollbackFailed, fmt.Sprintf("%v; rollback falhou: %v", cause, err)
	}
	if err := r.verifyRestored(); err != nil {
		return StateRollbackFailed, fmt.Sprintf("%v; rollback subiu mas não foi verificado: %v", cause, err)
	}
	return StateRolledBack, cause.Error()
}

// verifyRestored espera o servidor do rollback responder e confere os mounts
// contra o override restaurado. Não compara bootId: após um rollback sem
// mudança de configuração o compose pode não recriar o container.
func (r *Runner) verifyRestored() error {
	o, err := LoadOverride(r.OverridePath)
	if err != nil {
		return fmt.Errorf("override restaurado: %w", err)
	}
	if _, err := r.Docker.WaitHealthy(""); err != nil {
		return err
	}
	return r.checkMounts(o)
}

// checkMounts confere que os mounts /data/* do container são os desejados
// (destino e somente-leitura). A origem não é comparada: o Docker Desktop
// reporta caminhos traduzidos (ex.: /run/desktop/mnt/host/e/...).
func (r *Runner) checkMounts(o *Override) error {
	actual, err := r.Docker.InspectMounts()
	if err != nil {
		return fmt.Errorf("docker inspect: %w", err)
	}
	var want, got []string
	for _, m := range o.Mounts() {
		want = append(want, mountKey(m.Target, m.ReadOnly))
	}
	for _, m := range actual {
		if strings.HasPrefix(strings.ToLower(m.Destination), dataPrefix) {
			got = append(got, mountKey(m.Destination, !m.RW))
		}
	}
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(want, "\n") != strings.Join(got, "\n") {
		return fmt.Errorf("mounts divergentes: esperado %v, obtido %v", want, got)
	}
	return nil
}

func (r *Runner) setState(id, state, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j := r.jobs[id]; j != nil {
		j.State = state
		if msg != "" {
			j.Error = msg
		}
		r.persistLocked()
	}
}

// persistLocked grava os últimos jobs em data/jobs.json. Melhor esforço: falha
// de disco não derruba a operação. Chamar com r.mu seguro.
func (r *Runner) persistLocked() {
	list := make([]Job, 0, len(r.order))
	for _, id := range r.order {
		if j := r.jobs[id]; j != nil {
			list = append(list, *j)
		}
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	_, _ = WriteAtomic(filepath.Join(r.DataDir, jobsFileName), data, "")
}

func newJobID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func mountKey(target string, readOnly bool) string {
	return fmt.Sprintf("%s|ro=%t", strings.ToLower(target), readOnly)
}

// sameHostPath compara caminhos Windows ignorando caixa, separador e barra final.
func sameHostPath(a, b string) bool {
	norm := func(s string) string {
		s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "/", `\`))
		return strings.TrimRight(s, `\`)
	}
	return norm(a) == norm(b)
}
