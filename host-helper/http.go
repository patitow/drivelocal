package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	maxBodyBytes   = 8 << 10
	maxBrowseEntry = 2000
	bearerPrefix   = "Bearer "
)

// driveRootRe casa "E:" ou "E:\" (raiz de unidade, sem nada depois).
var driveRootRe = regexp.MustCompile(`^[A-Za-z]:\\?$`)

// server agrupa as dependências dos handlers HTTP.
type server struct {
	token        []byte
	runner       *Runner
	overridePath string
	deny         DenyConfig
}

// routes monta o mux /v1/* com Origin/Bearer aplicados a tudo.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("GET /v1/mounts", s.handleListMounts)
	mux.HandleFunc("POST /v1/mounts", s.handleCreateMount)
	mux.HandleFunc("DELETE /v1/mounts/{name}", s.handleDeleteMount)
	mux.HandleFunc("GET /v1/browse", s.handleBrowse)
	mux.HandleFunc("GET /v1/jobs/{id}", s.handleGetJob)
	return s.guard(mux)
}

// guard rejeita qualquer request com header Origin (403) e exige Bearer válido (401).
func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if _, ok := r.Header["Origin"]; ok {
			writeError(w, http.StatusForbidden, "origin_forbidden", "requests com header Origin não são aceitas")
			return
		}
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, bearerPrefix) ||
			subtle.ConstantTimeCompare([]byte(h[len(bearerPrefix):]), s.token) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "token ausente ou inválido")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version": helperVersion,
		"busy":    s.runner.ActiveJobID() != "",
	})
}

type mountsResponse struct {
	Revision  string  `json:"revision"`
	Mounts    []Mount `json:"mounts"`
	ActiveJob *Job    `json:"activeJob,omitempty"`
}

func (s *server) handleListMounts(w http.ResponseWriter, r *http.Request) {
	o, err := LoadOverride(s.overridePath)
	if err != nil {
		writeMapped(w, err)
		return
	}
	resp := mountsResponse{Revision: o.Revision(), Mounts: o.Mounts()}
	if id := s.runner.ActiveJobID(); id != "" {
		if j, ok := s.runner.Get(id); ok {
			resp.ActiveJob = &j
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type browseEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Selectable bool   `json:"selectable"`
	Reason     string `json:"reason,omitempty"`
}

type browseResponse struct {
	Path      string        `json:"path"`
	Parent    string        `json:"parent"`
	Entries   []browseEntry `json:"entries"`
	Truncated bool          `json:"truncated,omitempty"`
}

func (s *server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("path")
	if strings.TrimSpace(raw) == "" {
		writeJSON(w, http.StatusOK, browseResponse{Entries: s.drives()})
		return
	}

	var dir string
	if driveRootRe.MatchString(strings.TrimSpace(raw)) {
		// NormalizeHostPath exige algo após a unidade; a raiz é listada à parte.
		dir = strings.ToUpper(strings.TrimSpace(raw)[:1]) + `:\`
	} else {
		n, err := NormalizeHostPath(raw)
		if err != nil {
			writeMapped(w, err)
			return
		}
		dir = n
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "invalid_path", "pasta não encontrada ou inacessível")
		return
	}
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", "não foi possível listar a pasta")
		return
	}

	resp := browseResponse{Path: dir, Parent: parentOf(dir), Entries: []browseEntry{}}
	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		if len(resp.Entries) >= maxBrowseEntry {
			resp.Truncated = true
			break
		}
		child := filepath.Join(dir, de.Name())
		e := browseEntry{Name: de.Name(), Path: child, Selectable: true}
		if err := CheckSelectable(child, s.deny); err != nil {
			e.Selectable = false
			e.Reason = reasonOf(err)
		}
		resp.Entries = append(resp.Entries, e)
	}
	sort.Slice(resp.Entries, func(i, j int) bool {
		return strings.ToLower(resp.Entries[i].Name) < strings.ToLower(resp.Entries[j].Name)
	})
	writeJSON(w, http.StatusOK, resp)
}

// drives lista as unidades A-Z existentes. Raízes nunca são selecionáveis.
func (s *server) drives() []browseEntry {
	out := []browseEntry{}
	for c := 'A'; c <= 'Z'; c++ {
		root := string(c) + `:\`
		if _, err := os.Stat(root); err != nil {
			continue
		}
		out = append(out, browseEntry{
			Name:       string(c) + ":",
			Path:       root,
			Selectable: false,
			Reason:     "raiz de unidade",
		})
	}
	return out
}

func parentOf(dir string) string {
	p := filepath.Dir(dir)
	if p == dir || len(dir) <= 3 {
		return ""
	}
	return p
}

// reasonOf tira o código sentinela da mensagem de erro, deixando o motivo legível.
func reasonOf(err error) string {
	for _, sentinel := range []error{ErrPathDenied, ErrInvalidPath, ErrInvalidName} {
		if errors.Is(err, sentinel) {
			return strings.TrimPrefix(err.Error(), sentinel.Error()+": ")
		}
	}
	return err.Error()
}

type createMountRequest struct {
	HostPath     string `json:"hostPath"`
	Name         string `json:"name"`
	BaseRevision string `json:"baseRevision"`
}

func (s *server) handleCreateMount(w http.ResponseWriter, r *http.Request) {
	if !jsonRequest(w, r) {
		return
	}
	if s.busyResponse(w) {
		return
	}
	var req createMountRequest
	if !decodeBody(w, r, &req) {
		return
	}

	o, err := LoadOverride(s.overridePath)
	if err != nil {
		writeMapped(w, err)
		return
	}
	if req.BaseRevision == "" || req.BaseRevision != o.Revision() {
		writeError(w, http.StatusConflict, "revision_conflict", "o override mudou; recarregue a lista")
		return
	}

	resolved, err := ResolveSelectable(req.HostPath, s.deny)
	if err != nil {
		writeMapped(w, err)
		return
	}
	mounts := o.Mounts()
	names := make([]string, 0, len(mounts))
	for _, m := range mounts {
		if sameHostPath(m.HostPath, resolved) {
			writeError(w, http.StatusConflict, "already_mounted", "esta pasta já é uma biblioteca")
			return
		}
		names = append(names, m.Name)
	}

	var name string
	if strings.TrimSpace(req.Name) == "" {
		base, err := SanitizeName(filepath.Base(resolved))
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid_name", "nome automático inválido; informe um nome")
			return
		}
		name = UniqueName(base, names)
	} else {
		name, err = SanitizeName(req.Name)
		if err != nil {
			writeMapped(w, err)
			return
		}
		for _, n := range names {
			if strings.EqualFold(n, name) {
				writeError(w, http.StatusUnprocessableEntity, "name_conflict", "já existe uma biblioteca com este nome")
				return
			}
		}
	}

	op := Op{Kind: OpAdd, Name: name, HostPath: resolved, BaseRevision: req.BaseRevision}
	rev, err := previewRevision(s.overridePath, op)
	if err != nil {
		writeMapped(w, err)
		return
	}
	id, err := s.runner.Start(op)
	if err != nil {
		if errors.Is(err, ErrBusy) {
			s.busyResponse(w)
			return
		}
		writeMapped(w, err)
		return
	}
	logf("job %s: add %s -> %s", id, name, resolved)
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": id, "revision": rev})
}

func (s *server) handleDeleteMount(w http.ResponseWriter, r *http.Request) {
	if s.busyResponse(w) {
		return
	}
	name := r.PathValue("name")
	o, err := LoadOverride(s.overridePath)
	if err != nil {
		writeMapped(w, err)
		return
	}
	if base := r.URL.Query().Get("baseRevision"); base == "" || base != o.Revision() {
		writeError(w, http.StatusConflict, "revision_conflict", "o override mudou; recarregue a lista")
		return
	}
	found := false
	for _, m := range o.Mounts() {
		if strings.EqualFold(m.Name, name) {
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "not_found", "biblioteca não encontrada")
		return
	}

	op := Op{Kind: OpRemove, Name: name, BaseRevision: r.URL.Query().Get("baseRevision")}
	rev, err := previewRevision(s.overridePath, op)
	if err != nil {
		writeMapped(w, err)
		return
	}
	id, err := s.runner.Start(op)
	if err != nil {
		if errors.Is(err, ErrBusy) {
			s.busyResponse(w)
			return
		}
		writeMapped(w, err)
		return
	}
	logf("job %s: remove %s", id, name)
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": id, "revision": rev})
}

func (s *server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	j, ok := s.runner.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "job não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, j)
}

// busyResponse responde 409 busy (com jobId) se houver job em andamento.
func (s *server) busyResponse(w http.ResponseWriter) bool {
	id := s.runner.ActiveJobID()
	if id == "" {
		return false
	}
	writeJSON(w, http.StatusConflict, map[string]string{
		"error":   "busy",
		"message": "já existe uma alteração em andamento",
		"jobId":   id,
	})
	return true
}

// previewRevision calcula a revisão que o override terá após a operação, sem gravar.
func previewRevision(overridePath string, op Op) (string, error) {
	o, err := LoadOverride(overridePath)
	if err != nil {
		return "", err
	}
	switch op.Kind {
	case OpAdd:
		err = o.Add(Mount{Name: op.Name, HostPath: op.HostPath})
	case OpRemove:
		err = o.Remove(op.Name)
	}
	if err != nil {
		return "", err
	}
	return o.Revision(), nil
}

// jsonRequest exige Content-Type application/json em requests com corpo.
func jsonRequest(w http.ResponseWriter, r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "use Content-Type: application/json")
		return false
	}
	return true
}

// decodeBody lê o JSON com limite de 8KB; corpo grande vira 413.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "corpo maior que 8KB")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_body", "JSON inválido")
		return false
	}
	return true
}

// writeMapped traduz os erros do pacote para os códigos da API.
func writeMapped(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidPath):
		writeError(w, http.StatusBadRequest, "invalid_path", reasonOf(err))
	case errors.Is(err, ErrPathDenied):
		writeError(w, http.StatusForbidden, "path_denied", reasonOf(err))
	case errors.Is(err, ErrInvalidName):
		writeError(w, http.StatusUnprocessableEntity, "invalid_name", reasonOf(err))
	case errors.Is(err, ErrBusy):
		writeError(w, http.StatusConflict, "busy", "já existe uma alteração em andamento")
	case errors.Is(err, ErrRevisionConflict):
		writeError(w, http.StatusConflict, "revision_conflict", "o override mudou; recarregue a lista")
	case errors.Is(err, ErrAlreadyMounted):
		writeError(w, http.StatusConflict, "already_mounted", "esta pasta já é uma biblioteca")
	case errors.Is(err, ErrExists):
		writeError(w, http.StatusUnprocessableEntity, "name_conflict", "já existe uma biblioteca com este nome")
	case errors.Is(err, ErrNotFound), errors.Is(err, fs.ErrNotExist):
		writeError(w, http.StatusNotFound, "not_found", "não encontrado")
	default:
		logf("erro interno: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "erro interno do helper")
	}
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// logf escreve no log do helper (data/helper.log).
func logf(format string, args ...any) {
	log.Printf(format, args...)
}
