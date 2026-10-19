package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/mux"
)

const testMountToken = "test-helper-token-0123456789abcdef"

type recordedHelperCall struct {
	Method   string
	Path     string
	RawQuery string
	Auth     string
	Cookie   string
	Origin   string
	UserID   string
	Body     []byte
}

type fakeHelperReply struct {
	status int
	body   string
}

type fakeMountHelper struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls []recordedHelperCall
}

// newFakeMountHelper sobe um helper falso e aponta o servidor para ele via ambiente.
// replies usa a chave "METHOD /v1/caminho".
func newFakeMountHelper(t *testing.T, replies map[string]fakeHelperReply) *fakeMountHelper {
	t.Helper()
	f := &fakeMountHelper{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, recordedHelperCall{
			Method:   r.Method,
			Path:     r.URL.Path,
			RawQuery: r.URL.RawQuery,
			Auth:     r.Header.Get("Authorization"),
			Cookie:   r.Header.Get("Cookie"),
			Origin:   r.Header.Get("Origin"),
			UserID:   r.Header.Get("X-User-ID"),
			Body:     body,
		})
		f.mu.Unlock()

		reply, ok := replies[r.Method+" "+r.URL.Path]
		if !ok {
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(reply.status)
		w.Write([]byte(reply.body))
	}))
	t.Cleanup(f.srv.Close)
	t.Setenv("MOUNT_HELPER_URL", f.srv.URL)
	t.Setenv("MOUNT_HELPER_TOKEN", testMountToken)
	return f
}

func (f *fakeMountHelper) recorded() []recordedHelperCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedHelperCall(nil), f.calls...)
}

// setMountTestUsers define um admin e um usuário comum e restaura ao final.
func setMountTestUsers(t *testing.T) {
	t.Helper()
	usersMutex.Lock()
	prev := users
	users = []User{
		{ID: "admin-mounts", Username: "admin", Role: "admin", Active: true},
		{ID: "user-mounts", Username: "user", Role: "user", Active: true},
	}
	usersMutex.Unlock()
	t.Cleanup(func() {
		usersMutex.Lock()
		users = prev
		usersMutex.Unlock()
	})
}

// newMountTestRouter monta as rotas como em main.go.
func newMountTestRouter() http.Handler {
	r := mux.NewRouter()
	m := r.PathPrefix("/api/admin/mounts").Subrouter()
	m.Use(requireRole("admin"))
	m.HandleFunc("", adminMountsListHandler).Methods("GET")
	m.HandleFunc("", adminMountsCreateHandler).Methods("POST")
	m.HandleFunc("/browse", adminMountsBrowseHandler).Methods("GET")
	m.HandleFunc("/jobs/{id}", adminMountsJobHandler).Methods("GET")
	m.HandleFunc("/{name:.+}", adminMountsDeleteHandler).Methods("DELETE")
	return r
}

// doMountRequest envia a requisição como um cliente faria: com cookie, Authorization e Origin próprios.
func doMountRequest(t *testing.T, h http.Handler, method, target string, body string, userID string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	req.Header.Set("Authorization", "Bearer client-token-do-usuario")
	req.Header.Set("Cookie", "accessToken=abc; refreshToken=def")
	req.Header.Set("Origin", "http://evil.example")
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestMountsAuthAndRoles(t *testing.T) {
	setMountTestUsers(t)
	fake := newFakeMountHelper(t, nil)
	h := newMountTestRouter()

	t.Run("usuario comum recebe 403", func(t *testing.T) {
		w := doMountRequest(t, h, "GET", "/api/admin/mounts", "", "user-mounts")
		if w.Code != http.StatusForbidden {
			t.Fatalf("esperava 403, veio %d", w.Code)
		}
	})

	t.Run("sem usuario recebe 403", func(t *testing.T) {
		w := doMountRequest(t, h, "POST", "/api/admin/mounts", `{"hostPath":"E:\\\\x","baseRevision":"r"}`, "")
		if w.Code != http.StatusForbidden {
			t.Fatalf("esperava 403, veio %d", w.Code)
		}
	})

	t.Run("sem token recebe 401 pelo authMiddleware", func(t *testing.T) {
		chain := authMiddleware(h)
		req := httptest.NewRequest("GET", "/api/admin/mounts", nil)
		w := httptest.NewRecorder()
		chain.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("esperava 401, veio %d", w.Code)
		}
	})

	if n := len(fake.recorded()); n != 0 {
		t.Errorf("helper não deveria ser chamado por requisições negadas, recebeu %d", n)
	}
}

func TestMountsListHelperOnline(t *testing.T) {
	setMountTestUsers(t)
	fake := newFakeMountHelper(t, map[string]fakeHelperReply{
		"GET /v1/health": {200, `{"version":"1.2.3","busy":false}`},
		"GET /v1/mounts": {200, `{"revision":"rev1","mounts":[{"name":"Filmes","hostPath":"E:\\Filmes","target":"/data/Filmes","legacy":true}],"activeJob":null}`},
	})
	mountInfoPath = writeMountInfo(t, "")
	t.Cleanup(func() { mountInfoPath = mountInfoDefaultPath })

	w := doMountRequest(t, newMountTestRouter(), "GET", "/api/admin/mounts", "", "admin-mounts")
	if w.Code != http.StatusOK {
		t.Fatalf("esperava 200, veio %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Helper struct {
			Available bool
			Version   string
		}
		Revision  string
		Mounts    []map[string]interface{}
		ActiveJob interface{}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta inválida: %v", err)
	}
	if !resp.Helper.Available || resp.Helper.Version != "1.2.3" {
		t.Errorf("helper: %+v", resp.Helper)
	}
	if resp.Revision != "rev1" || len(resp.Mounts) != 1 || resp.ActiveJob != nil {
		t.Errorf("revision/mounts/activeJob inesperados: %+v", resp)
	}

	calls := fake.recorded()
	if len(calls) != 2 {
		t.Fatalf("esperava 2 chamadas ao helper, veio %d", len(calls))
	}
	for _, c := range calls {
		if c.Auth != "Bearer "+testMountToken {
			t.Errorf("Authorization do helper = %q", c.Auth)
		}
		if c.Cookie != "" || c.Origin != "" || c.UserID != "" {
			t.Errorf("helper recebeu header do cliente: cookie=%q origin=%q user=%q", c.Cookie, c.Origin, c.UserID)
		}
	}
}

func TestMountsListHelperOfflineUsesMountinfo(t *testing.T) {
	setMountTestUsers(t)
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	t.Setenv("MOUNT_HELPER_URL", deadURL)
	t.Setenv("MOUNT_HELPER_TOKEN", testMountToken)

	mountInfoPath = writeMountInfo(t, "")
	t.Cleanup(func() { mountInfoPath = mountInfoDefaultPath })

	w := doMountRequest(t, newMountTestRouter(), "GET", "/api/admin/mounts", "", "admin-mounts")
	if w.Code != http.StatusOK {
		t.Fatalf("esperava 200, veio %d", w.Code)
	}
	var resp struct {
		Helper          struct{ Available bool }
		ContainerMounts []string
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Helper.Available {
		t.Error("helper deveria estar indisponível")
	}
	want := []string{"/data/Filmes", "/data/Series HD"}
	if strings.Join(resp.ContainerMounts, "|") != strings.Join(want, "|") {
		t.Errorf("containerMounts = %v, quero %v", resp.ContainerMounts, want)
	}
}

func TestMountsMutationsHelperOffline(t *testing.T) {
	setMountTestUsers(t)
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	t.Setenv("MOUNT_HELPER_URL", deadURL)
	t.Setenv("MOUNT_HELPER_TOKEN", testMountToken)
	h := newMountTestRouter()

	cases := []struct{ method, target, body string }{
		{"POST", "/api/admin/mounts", `{"hostPath":"E:\\\\x","baseRevision":"r"}`},
		{"DELETE", "/api/admin/mounts/Filmes?baseRevision=r", ""},
		{"GET", "/api/admin/mounts/browse?path=E:%5C", ""},
		{"GET", "/api/admin/mounts/jobs/job1", ""},
	}
	for _, c := range cases {
		w := doMountRequest(t, h, c.method, c.target, c.body, "admin-mounts")
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "helper_unavailable") {
			t.Errorf("%s %s: esperava 503 helper_unavailable, veio %d %s", c.method, c.target, w.Code, w.Body.String())
		}
	}
}

func TestMountsHelperWithoutTokenIsUnavailable(t *testing.T) {
	setMountTestUsers(t)
	t.Setenv("MOUNT_HELPER_TOKEN", "")
	w := doMountRequest(t, newMountTestRouter(), "GET", "/api/admin/mounts/browse", "", "admin-mounts")
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("sem token configurado esperava 503, veio %d", w.Code)
	}
}

func TestMountsPassThroughHelperErrors(t *testing.T) {
	setMountTestUsers(t)
	h := newMountTestRouter()

	cases := []struct {
		name      string
		method    string
		target    string
		body      string
		reply     fakeHelperReply
		helperKey string
		wantCode  int
	}{
		{"409 revision_conflict", "DELETE", "/api/admin/mounts/Filmes?baseRevision=old", "", fakeHelperReply{409, `{"error":"revision_conflict","message":"recarregue"}`}, "DELETE /v1/mounts/Filmes", 409},
		{"409 busy", "POST", "/api/admin/mounts", `{"hostPath":"E:\\\\x","baseRevision":"r"}`, fakeHelperReply{409, `{"error":"busy","message":"job em andamento"}`}, "POST /v1/mounts", 409},
		{"422 name_conflict", "POST", "/api/admin/mounts", `{"hostPath":"E:\\\\x","name":"Filmes","baseRevision":"r"}`, fakeHelperReply{422, `{"error":"name_conflict","message":"nome já existe"}`}, "POST /v1/mounts", 422},
		{"403 path_denied", "POST", "/api/admin/mounts", `{"hostPath":"C:\\\\Windows","baseRevision":"r"}`, fakeHelperReply{403, `{"error":"path_denied","message":"bloqueado"}`}, "POST /v1/mounts", 403},
		{"helper 500 vira 503", "GET", "/api/admin/mounts/jobs/job1", "", fakeHelperReply{500, `boom`}, "GET /v1/jobs/job1", 503},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := newFakeMountHelper(t, map[string]fakeHelperReply{c.helperKey: c.reply})
			w := doMountRequest(t, h, c.method, c.target, c.body, "admin-mounts")
			if w.Code != c.wantCode {
				t.Fatalf("esperava %d, veio %d: %s", c.wantCode, w.Code, w.Body.String())
			}
			if c.wantCode != 503 && w.Body.String() != c.reply.body {
				t.Errorf("corpo não repassado: %s", w.Body.String())
			}
			if len(fake.recorded()) != 1 {
				t.Errorf("esperava 1 chamada ao helper")
			}
		})
	}
}

func TestMountsCreateForwardsOnlyKnownFields(t *testing.T) {
	setMountTestUsers(t)
	fake := newFakeMountHelper(t, map[string]fakeHelperReply{
		"POST /v1/mounts": {202, `{"jobId":"job1","revision":"r2"}`},
	})
	body := `{"hostPath":"E:\\Filmes","name":"Filmes","baseRevision":"r1","evil":{"x":1}}`
	w := doMountRequest(t, newMountTestRouter(), "POST", "/api/admin/mounts", body, "admin-mounts")
	if w.Code != http.StatusAccepted {
		t.Fatalf("esperava 202, veio %d: %s", w.Code, w.Body.String())
	}

	calls := fake.recorded()
	if len(calls) != 1 {
		t.Fatalf("esperava 1 chamada, veio %d", len(calls))
	}
	var sent map[string]interface{}
	if err := json.Unmarshal(calls[0].Body, &sent); err != nil {
		t.Fatalf("corpo enviado inválido: %v", err)
	}
	if _, ok := sent["evil"]; ok {
		t.Errorf("campo desconhecido foi repassado: %s", calls[0].Body)
	}
	if sent["hostPath"] != `E:\Filmes` || sent["name"] != "Filmes" || sent["baseRevision"] != "r1" {
		t.Errorf("corpo enviado = %s", calls[0].Body)
	}
}

func TestMountsDeleteAndBrowseForwarding(t *testing.T) {
	setMountTestUsers(t)
	fake := newFakeMountHelper(t, map[string]fakeHelperReply{
		"DELETE /v1/mounts/Filmes": {202, `{"jobId":"job2"}`},
		"GET /v1/browse":           {200, `{"path":"E:\\","parent":"","entries":[]}`},
		"GET /v1/jobs/job2":        {200, `{"id":"job2","state":"done"}`},
	})
	h := newMountTestRouter()

	w := doMountRequest(t, h, "DELETE", "/api/admin/mounts/Filmes?baseRevision=r1&extra=x", "", "admin-mounts")
	if w.Code != http.StatusAccepted {
		t.Fatalf("delete: esperava 202, veio %d", w.Code)
	}
	w = doMountRequest(t, h, "GET", "/api/admin/mounts/browse?path=E:%5C&outro=1", "", "admin-mounts")
	if w.Code != http.StatusOK {
		t.Fatalf("browse: esperava 200, veio %d", w.Code)
	}
	w = doMountRequest(t, h, "GET", "/api/admin/mounts/jobs/job2", "", "admin-mounts")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"done"`) {
		t.Fatalf("job: %d %s", w.Code, w.Body.String())
	}

	calls := fake.recorded()
	if len(calls) != 3 {
		t.Fatalf("esperava 3 chamadas, veio %d", len(calls))
	}
	if calls[0].RawQuery != "baseRevision=r1" {
		t.Errorf("query do delete = %q (só baseRevision deve seguir)", calls[0].RawQuery)
	}
	if calls[1].RawQuery != "path=E%3A%5C" {
		t.Errorf("query do browse = %q", calls[1].RawQuery)
	}
}

func TestMountsRejectUnsafeNames(t *testing.T) {
	setMountTestUsers(t)
	fake := newFakeMountHelper(t, nil)
	h := newMountTestRouter()

	cases := []struct{ method, target, body string }{
		{"DELETE", "/api/admin/mounts/a%2Fb?baseRevision=r", ""},
		{"DELETE", "/api/admin/mounts/a/b?baseRevision=r", ""},
		{"DELETE", "/api/admin/mounts/..%2Fetc?baseRevision=r", ""},
		{"DELETE", "/api/admin/mounts/..x?baseRevision=r", ""},
		{"POST", "/api/admin/mounts", `{"hostPath":"E:\\\\x","name":"../x","baseRevision":"r"}`},
		{"POST", "/api/admin/mounts", `{"hostPath":"E:\\\\x","name":"a\\\\b","baseRevision":"r"}`},
	}
	for _, c := range cases {
		w := doMountRequest(t, h, c.method, c.target, c.body, "admin-mounts")
		// O mux limpa caminhos com ".." e responde 301 antes do handler; também é rejeição.
		if w.Code != http.StatusBadRequest && w.Code != http.StatusMovedPermanently {
			t.Errorf("%s %s %s: esperava 400 ou 301, veio %d", c.method, c.target, c.body, w.Code)
		}
	}

	// Caso a rota não case o nome (ex.: barra não escapada), também não pode chegar ao helper.
	if n := len(fake.recorded()); n != 0 {
		t.Errorf("nomes inválidos chegaram ao helper: %d chamadas", n)
	}
}

func TestMountsJobIDValidation(t *testing.T) {
	setMountTestUsers(t)
	fake := newFakeMountHelper(t, nil)
	w := doMountRequest(t, newMountTestRouter(), "GET", "/api/admin/mounts/jobs/..%2Fmounts", "", "admin-mounts")
	if w.Code != http.StatusBadRequest && w.Code != http.StatusMovedPermanently {
		t.Errorf("esperava 400 ou 301, veio %d", w.Code)
	}
	if n := len(fake.recorded()); n != 0 {
		t.Errorf("id inválido chegou ao helper")
	}
}

func TestReadContainerMountsParsesMountinfo(t *testing.T) {
	path := writeMountInfo(t, "")
	got, err := readContainerMounts(path)
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if strings.Join(got, "|") != "/data/Filmes|/data/Series HD" {
		t.Errorf("got %v", got)
	}

	if _, err := readContainerMounts(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("arquivo ausente deveria dar erro")
	}
}

// writeMountInfo cria um mountinfo de teste com duas pastas /data e uma raiz.
func writeMountInfo(t *testing.T, extra string) string {
	t.Helper()
	content := strings.Join([]string{
		"36 35 98:0 /E/Filmes /data/Filmes ro,noatime shared:1 - ext4 /dev/sdb1 ro",
		"37 35 98:0 /E/Series /data/Series\\040HD ro,noatime - ext4 /dev/sdb1 ro",
		"38 35 98:0 / / rw,relatime - ext4 /dev/root rw",
		"39 35 0:5 / /dev ro - devtmpfs udev rw",
		extra,
	}, "\n")
	path := filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}


func TestMountsResponseOverLimitIs502(t *testing.T) {
	setMountTestUsers(t)
	newFakeMountHelper(t, map[string]fakeHelperReply{
		"GET /v1/browse": {200, strings.Repeat("x", mountHelperMaxResponse+1)},
	})
	w := doMountRequest(t, newMountTestRouter(), "GET", "/api/admin/mounts/browse?path=/", "", "admin-mounts")
	if w.Code != http.StatusBadGateway {
		t.Fatalf("esperava 502, veio %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp["error"] != "helper_response_too_large" {
		t.Fatalf("corpo inesperado: %s", w.Body.String())
	}
}

func TestMountsResponseUnderLimitNotTruncated(t *testing.T) {
	setMountTestUsers(t)
	body := `{"entries":["` + strings.Repeat("a", 300<<10) + `"]}`
	newFakeMountHelper(t, map[string]fakeHelperReply{
		"GET /v1/browse": {200, body},
	})
	w := doMountRequest(t, newMountTestRouter(), "GET", "/api/admin/mounts/browse?path=/", "", "admin-mounts")
	if w.Code != http.StatusOK {
		t.Fatalf("esperava 200, veio %d", w.Code)
	}
	if w.Body.String() != body {
		t.Fatalf("resposta truncada: %d bytes de %d", w.Body.Len(), len(body))
	}
}
