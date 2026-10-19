package main

// Testes de acesso a pastas (zip, listagem, capa de pasta) e do fail-closed
// com o banco indisponível. Nomes sintéticos.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

const (
	epVisivel  = "Animes/Serie Exemplo/[Grupo] Serie Exemplo - 01 [1080p].mkv"
	epExplic   = "Animes/Serie Exemplo/[Grupo] Serie Exemplo - 03 [1080p].mkv"
	epEscondid = "Animes/Serie Exemplo/[Grupo] Serie Exemplo - 02 [1080p].mkv"
)

func accessRouter(as string) *mux.Router {
	r := mux.NewRouter()
	api := r.PathPrefix("/api").Subrouter()
	api.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			req.Header.Set("X-User-ID", as)
			next.ServeHTTP(w, req)
		})
	})
	api.Use(libraryAccessMiddleware)
	api.HandleFunc("/files", filesHandler).Methods("GET")
	api.HandleFunc("/download-multiple", downloadMultipleHandler).Methods("POST")
	ok := func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("SERVIDO")) }
	api.HandleFunc("/thumbnail/{path:.*}", ok)
	api.HandleFunc("/download/{path:.*}", ok)
	api.HandleFunc("/network-info", ok)
	return r
}

func listNames(t *testing.T, r *mux.Router, dir string) []string {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/files?path="+strings.ReplaceAll(dir, " ", "%20"), nil))
	if rec.Code != 200 {
		t.Fatalf("listagem %s: %d %s", dir, rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct{ Path string } `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	out := []string{}
	for _, it := range resp.Items {
		out = append(out, strings.ReplaceAll(it.Path, "\\", "/"))
	}
	return out
}

func zipNames(t *testing.T, r *mux.Router, files []string) (int, []string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"files": files})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("POST", "/api/download-multiple", bytes.NewReader(b)))
	if rec.Code != 200 {
		return rec.Code, nil
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("zip inválido: %v", err)
	}
	out := []string{}
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	return 200, out
}

func has(list []string, s string) bool {
	for _, x := range list {
		if strings.Contains(x, s) {
			return true
		}
	}
	return false
}

func TestLibraryDirectoryContentFiltering(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	exp := "explicito"
	if _, err := fx.l.ApplyActions(ctx, []int64{fx.ids[epExplic]}, Actions{Maturity: &exp, AddTags: []string{"tag-so-explicita"}}, "manual", "adm", "t", false); err != nil {
		t.Fatal(err)
	}
	if err := fx.l.SetHidden(ctx, "u-new", fx.ids[epEscondid], true); err != nil {
		t.Fatal(err)
	}
	user, admin := accessRouter("u-new"), accessRouter("u-admin")

	// Listagem: usuário comum não vê o explícito nem o que escondeu; admin vê tudo.
	got := listNames(t, user, "Animes/Serie Exemplo")
	if !has(got, " - 01 ") || has(got, " - 03 ") || has(got, " - 02 ") {
		t.Errorf("listagem do usuário: %v", got)
	}
	if got := listNames(t, user, "Animes"); has(got, "Restrito") {
		t.Errorf("pasta oculta/explícita listada: %v", got)
	}
	if got := listNames(t, admin, "Animes/Serie Exemplo"); !has(got, " - 03 ") || !has(got, " - 02 ") {
		t.Errorf("admin deveria ver tudo: %v", got)
	}

	// Pasta com item bloqueado: capa de pasta e download da pasta negados ao usuário.
	for _, rt := range []string{"thumbnail", "download"} {
		rec := httptest.NewRecorder()
		user.ServeHTTP(rec, httptest.NewRequest("GET", "/api/"+rt+"/Animes/Serie%20Exemplo", nil))
		if strings.Contains(rec.Body.String(), "SERVIDO") {
			t.Errorf("%s de pasta com item bloqueado serviu (%d)", rt, rec.Code)
		}
		rec = httptest.NewRecorder()
		admin.ServeHTTP(rec, httptest.NewRequest("GET", "/api/"+rt+"/Animes/Serie%20Exemplo", nil))
		if !strings.Contains(rec.Body.String(), "SERVIDO") {
			t.Errorf("admin: %s de pasta bloqueado (%d)", rt, rec.Code)
		}
	}
	// Pasta sem nada bloqueado continua com capa.
	rec := httptest.NewRecorder()
	user.ServeHTTP(rec, httptest.NewRequest("GET", "/api/thumbnail/Filmes", nil))
	if !strings.Contains(rec.Body.String(), "SERVIDO") {
		t.Errorf("capa de pasta limpa bloqueada: %d", rec.Code)
	}

	// Zip: a pasta com conteúdo bloqueado sai da lista; o explícito nunca entra.
	code, names := zipNames(t, user, []string{epVisivel, "Animes/Serie Exemplo", "Animes"})
	if code != 200 || !has(names, " - 01 ") || has(names, " - 03 ") || has(names, " - 02 ") {
		t.Errorf("zip do usuário: %d %v", code, names)
	}
	if code, _ := zipNames(t, user, []string{epVisivel, epExplic}); code == 200 {
		t.Errorf("zip com arquivo explícito listado deveria falhar")
	}
	if code, names := zipNames(t, admin, []string{epVisivel, epExplic}); code != 200 || !has(names, " - 03 ") {
		t.Errorf("zip do admin: %d %v", code, names)
	}

	// Catálogo, tags, continuar assistindo e playlists só devolvem itens visíveis.
	v := fx.l.viewerFor("u-new")
	for _, p := range fx.paths(t, v, ItemQuery{}) {
		if p == epExplic || p == epEscondid {
			t.Errorf("catálogo devolveu %s", p)
		}
	}
	tags, _ := fx.l.ListTags(ctx, v, "")
	for _, tg := range tags {
		if tg.Slug == "tag-so-explicita" {
			t.Errorf("tag só de item explícito exposta ao usuário")
		}
	}
	atags, _ := fx.l.ListTags(ctx, fx.l.viewerFor("u-admin"), "")
	found := false
	for _, tg := range atags {
		found = found || tg.Slug == "tag-so-explicita"
	}
	if !found {
		t.Errorf("admin deveria ver a tag")
	}
	for _, p := range []string{epVisivel, epExplic} {
		if _, err := fx.l.SaveProgress(ctx, "u-new", fx.ids[p], ProgressInput{PositionS: 300, DurationS: 1400}); err != nil {
			t.Fatal(err)
		}
	}
	cont, _ := fx.l.ContinueWatching(ctx, v, 50)
	for _, it := range cont {
		if it.Path == epExplic {
			t.Errorf("continuar assistindo devolveu item explícito")
		}
	}
	name := "fila"
	pl, err := fx.l.CreatePlaylist(ctx, v, PlaylistInput{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	fx.l.AddPlaylistItems(ctx, v, pl.ID, []int64{fx.ids[epVisivel], fx.ids[epExplic]})
	items, _ := fx.l.PlaylistItems(ctx, v, pl.ID)
	for _, it := range items {
		if it.Path == epExplic {
			t.Errorf("playlist devolveu item explícito")
		}
	}
}

func TestLibraryFailClosedWhenDBDown(t *testing.T) {
	newLibFixture(t) // usuários e MEDIA_PATH
	setLibrary(nil)
	libraryFailed.Store(true)
	t.Cleanup(func() { libraryFailed.Store(false) })

	user, admin := accessRouter("u-new"), accessRouter("u-admin")
	for _, req := range []*http.Request{
		httptest.NewRequest("GET", "/api/thumbnail/Filmes", nil),
		httptest.NewRequest("GET", "/api/download/Filmes/x.mp4", nil),
		httptest.NewRequest("GET", "/api/files?path=Animes", nil),
		httptest.NewRequest("POST", "/api/download-multiple", strings.NewReader(`{"files":["Filmes"]}`)),
	} {
		rec := httptest.NewRecorder()
		user.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: esperava 503, veio %d", req.Method, req.URL.Path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	admin.ServeHTTP(rec, httptest.NewRequest("GET", "/api/thumbnail/Filmes", nil))
	if !strings.Contains(rec.Body.String(), "SERVIDO") {
		t.Errorf("admin deveria continuar funcionando: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	user.ServeHTTP(rec, httptest.NewRequest("GET", "/api/network-info", nil))
	if !strings.Contains(rec.Body.String(), "SERVIDO") {
		t.Errorf("rota sem mídia não deveria ser afetada: %d", rec.Code)
	}
	// Biblioteca apenas não inicializada (sem falha) mantém o comportamento antigo.
	libraryFailed.Store(false)
	rec = httptest.NewRecorder()
	user.ServeHTTP(rec, httptest.NewRequest("GET", "/api/thumbnail/Filmes", nil))
	if !strings.Contains(rec.Body.String(), "SERVIDO") {
		t.Errorf("sem falha registrada: %d", rec.Code)
	}
}
