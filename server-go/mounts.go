package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// Proxy admin para o helper de montagem que roda no host (host-helper/).
// O servidor monta cada requisição do zero: nunca repassa headers ou cookies do cliente.

const (
	mountHelperDefaultURL    = "http://host.docker.internal:47123"
	mountHelperHealthTimeout = 5 * time.Second
	mountHelperCallTimeout   = 10 * time.Second
	mountHelperMaxBody       = 8 << 10
	mountHelperMaxResponse   = 1 << 20
	mountInfoDefaultPath     = "/proc/self/mountinfo"
)

var (
	// Injetável em testes.
	mountInfoPath   = mountInfoDefaultPath
	mountHelperHTTP = &http.Client{}

	errMountHelperUnavailable = errors.New("mount helper unavailable")
	errMountHelperTooLarge    = errors.New("mount helper response too large")
)

type mountHelperResponse struct {
	status int
	body   []byte
}

type mountHelperStatus struct {
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
}

type mountsListResponse struct {
	Helper          mountHelperStatus `json:"helper"`
	Revision        string            `json:"revision,omitempty"`
	Mounts          json.RawMessage   `json:"mounts"`
	ActiveJob       json.RawMessage   `json:"activeJob,omitempty"`
	ContainerMounts []string          `json:"containerMounts"`
}

type mountCreateRequest struct {
	HostPath     string `json:"hostPath"`
	Name         string `json:"name,omitempty"`
	BaseRevision string `json:"baseRevision"`
}

// mountHelperCall faz uma requisição ao helper (prefixo /v1, Bearer do ambiente).
// Retorna errMountHelperUnavailable para falha de rede, 5xx e 401 (token divergente).
func mountHelperCall(method, path string, query url.Values, body []byte, timeout time.Duration) (*mountHelperResponse, error) {
	token := os.Getenv("MOUNT_HELPER_TOKEN")
	if token == "" {
		return nil, errMountHelperUnavailable
	}

	target := getEnv("MOUNT_HELPER_URL", mountHelperDefaultURL) + "/v1" + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, errMountHelperUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := mountHelperHTTP.Do(req)
	if err != nil {
		logDebug("mount helper %s %s: %v", method, path, err)
		return nil, errMountHelperUnavailable
	}
	defer resp.Body.Close()

	// Lê um byte a mais que o limite para distinguir "exatamente no limite" de "passou do limite".
	data, err := io.ReadAll(io.LimitReader(resp.Body, mountHelperMaxResponse+1))
	if err != nil {
		return nil, errMountHelperUnavailable
	}
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusUnauthorized {
		logWarn("mount helper %s %s respondeu %d", method, path, resp.StatusCode)
		return nil, errMountHelperUnavailable
	}
	if len(data) > mountHelperMaxResponse {
		logWarn("mount helper %s %s: resposta maior que %d bytes", method, path, mountHelperMaxResponse)
		return nil, errMountHelperTooLarge
	}
	return &mountHelperResponse{status: resp.StatusCode, body: data}, nil
}

func writeMountJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeMountError(w http.ResponseWriter, status int, code, message string) {
	writeMountJSON(w, status, map[string]string{"error": code, "message": message})
}

func writeMountUnavailable(w http.ResponseWriter) {
	writeMountError(w, http.StatusServiceUnavailable, "helper_unavailable", "Helper de montagem indisponível")
}

// writeMountCallError trata falha de mountHelperCall: resposta grande demais vira 502, o resto 503.
func writeMountCallError(w http.ResponseWriter, err error) {
	if errors.Is(err, errMountHelperTooLarge) {
		writeMountError(w, http.StatusBadGateway, "helper_response_too_large", "Resposta do helper de montagem maior que o limite")
		return
	}
	writeMountUnavailable(w)
}

// writeMountHelperResponse repassa status e corpo do helper (2xx e 4xx).
func writeMountHelperResponse(w http.ResponseWriter, res *mountHelperResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.status)
	w.Write(res.body)
}

// mountNameUnsafe rejeita nomes que poderiam sair do segmento de URL ou de /data.
func mountNameUnsafe(name string) bool {
	return strings.ContainsAny(name, `/\`) || strings.Contains(name, "..")
}

func validMountJobID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// readContainerMounts lista os destinos /data/* do /proc/self/mountinfo do container.
func readContainerMounts(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)

	seen := map[string]bool{}
	out := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		// id parent maj:min root mountpoint options [opcionais...] - fstype source superopts
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		mp := unescape.Replace(fields[4])
		if !strings.HasPrefix(mp, "/data/") || seen[mp] {
			continue
		}
		seen[mp] = true
		out = append(out, mp)
	}
	sort.Strings(out)
	return out, nil
}

// GET /api/admin/mounts
func adminMountsListHandler(w http.ResponseWriter, r *http.Request) {
	containerMounts, err := readContainerMounts(mountInfoPath)
	if err != nil {
		logWarn("mountinfo: %v", err)
		containerMounts = []string{}
	}
	unavailable := func() {
		writeMountJSON(w, http.StatusOK, mountsListResponse{
			Helper:          mountHelperStatus{Available: false},
			Mounts:          json.RawMessage("[]"),
			ContainerMounts: containerMounts,
		})
	}

	health, err := mountHelperCall(http.MethodGet, "/health", nil, nil, mountHelperHealthTimeout)
	if errors.Is(err, errMountHelperTooLarge) {
		writeMountCallError(w, err)
		return
	}
	if err != nil || health.status != http.StatusOK {
		unavailable()
		return
	}
	var h struct {
		Version string `json:"version"`
	}
	json.Unmarshal(health.body, &h)

	list, err := mountHelperCall(http.MethodGet, "/mounts", nil, nil, mountHelperCallTimeout)
	if errors.Is(err, errMountHelperTooLarge) {
		writeMountCallError(w, err)
		return
	}
	if err != nil || list.status != http.StatusOK {
		unavailable()
		return
	}
	var l struct {
		Revision  string          `json:"revision"`
		Mounts    json.RawMessage `json:"mounts"`
		ActiveJob json.RawMessage `json:"activeJob"`
	}
	if err := json.Unmarshal(list.body, &l); err != nil {
		unavailable()
		return
	}
	if len(l.Mounts) == 0 || string(l.Mounts) == "null" {
		l.Mounts = json.RawMessage("[]")
	}
	if string(l.ActiveJob) == "null" {
		l.ActiveJob = nil
	}

	writeMountJSON(w, http.StatusOK, mountsListResponse{
		Helper:          mountHelperStatus{Available: true, Version: h.Version},
		Revision:        l.Revision,
		Mounts:          l.Mounts,
		ActiveJob:       l.ActiveJob,
		ContainerMounts: containerMounts,
	})
}

// GET /api/admin/mounts/browse?path=
func adminMountsBrowseHandler(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	if p := r.URL.Query().Get("path"); p != "" {
		q.Set("path", p)
	}
	res, err := mountHelperCall(http.MethodGet, "/browse", q, nil, mountHelperCallTimeout)
	if err != nil {
		writeMountCallError(w, err)
		return
	}
	writeMountHelperResponse(w, res)
}

// POST /api/admin/mounts
func adminMountsCreateHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, mountHelperMaxBody)
	var req mountCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMountError(w, http.StatusBadRequest, "invalid_request", "Corpo da requisição inválido")
		return
	}
	if mountNameUnsafe(req.Name) {
		writeMountError(w, http.StatusBadRequest, "invalid_name", "Nome inválido")
		return
	}

	// Só os campos conhecidos seguem para o helper.
	body, err := json.Marshal(req)
	if err != nil {
		writeMountError(w, http.StatusBadRequest, "invalid_request", "Corpo da requisição inválido")
		return
	}
	res, err := mountHelperCall(http.MethodPost, "/mounts", nil, body, mountHelperCallTimeout)
	if err != nil {
		writeMountCallError(w, err)
		return
	}
	writeMountHelperResponse(w, res)
}

// DELETE /api/admin/mounts/{name}?baseRevision=
func adminMountsDeleteHandler(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if name == "" || mountNameUnsafe(name) {
		writeMountError(w, http.StatusBadRequest, "invalid_name", "Nome inválido")
		return
	}
	q := url.Values{}
	q.Set("baseRevision", r.URL.Query().Get("baseRevision"))
	res, err := mountHelperCall(http.MethodDelete, "/mounts/"+url.PathEscape(name), q, nil, mountHelperCallTimeout)
	if err != nil {
		writeMountCallError(w, err)
		return
	}
	writeMountHelperResponse(w, res)
}

// GET /api/admin/mounts/jobs/{id}
func adminMountsJobHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if !validMountJobID(id) {
		writeMountError(w, http.StatusBadRequest, "invalid_job_id", "Job inválido")
		return
	}
	res, err := mountHelperCall(http.MethodGet, "/jobs/"+url.PathEscape(id), nil, nil, mountHelperCallTimeout)
	if err != nil {
		writeMountCallError(w, err)
		return
	}
	writeMountHelperResponse(w, res)
}
