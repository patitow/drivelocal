package main

// Acervo local de arte: baixar, guardar, servir e trocar links da ficha.

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

func testPNG(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := 0; x < 4; x++ {
		for y := 0; y < 4; y++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestArtStoreAndLocalize(t *testing.T) {
	fx := newLibFixture(t)
	ctx := context.Background()
	l := fx.l
	red, blue := testPNG(t, color.RGBA{255, 0, 0, 255}), testPNG(t, color.RGBA{0, 0, 255, 255})
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/capa.png":
			w.Write(red)
		case "/banner.png":
			w.Write(blue)
		case "/pagina.html":
			w.Write([]byte("<html>não é imagem</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// por padrão, nada de rede local (SSRF)
	if _, err := l.fetchArt(ctx, srv.URL+"/capa.png"); err == nil || hits != 0 {
		t.Fatalf("baixou de 127.0.0.1: %v (hits %d)", err, hits)
	}
	artAllowPrivate = true
	defer func() { artAllowPrivate = false }()

	// o que não é imagem não entra
	if _, err := l.saveArt([]byte("texto")); err == nil {
		t.Fatal("texto virou arte")
	}
	if _, err := l.fetchArt(ctx, srv.URL+"/pagina.html"); err == nil {
		t.Fatal("html virou arte")
	}
	// mesma imagem, mesmo arquivo
	a1, err := l.saveArt(red)
	if err != nil || !strings.HasPrefix(a1, artPrefix) || !strings.HasSuffix(a1, ".png") {
		t.Fatalf("saveArt: %q %v", a1, err)
	}
	if a2, _ := l.fetchArt(ctx, srv.URL+"/capa.png"); a2 != a1 {
		t.Fatalf("mesma imagem, nomes diferentes: %q %q", a1, a2)
	}

	// a ficha salva com links da web guarda as cópias locais e a origem
	title := "Serie Sintetica Arte"
	ref, err := l.CreateEntry(ctx, EntryInput{Cat: "series", Title: &title, AddIDs: []int64{fx.ids["Criadores/criador-sintetico/abcd1234efgh.mp4"]},
		Meta: &EntryData{ExtMeta: ExtMeta{Cover: srv.URL + "/capa.png", Banner: srv.URL + "/banner.png"}}}, "adm")
	if err != nil {
		t.Fatal(err)
	}
	d, _ := l.GetEntryMeta(ctx, ref)
	if d.Cover != a1 || !strings.HasPrefix(d.Banner, artPrefix) || d.CoverSrc != srv.URL+"/capa.png" || d.BannerSrc != srv.URL+"/banner.png" {
		t.Fatalf("ficha não localizou: %+v", d)
	}
	if e, err := l.FindEntry(ctx, ref); err != nil || e.Cat != "series" || e.Meta == nil || !strings.HasPrefix(e.Meta.Cover, "/api/art/") {
		t.Fatalf("FindEntry: %+v %v", e, err)
	}
	if _, err := l.FindEntry(ctx, "topic:999999"); err != errNotFound {
		t.Fatalf("FindEntry inexistente: %v", err)
	}
	// a página vê o endereço local, e ele volta para a ficha sem perder a origem
	arts, _ := l.EntryArts(ctx, l.viewerFor("u-admin"))
	pub := arts[ref].Cover
	if pub != "/api/art/"+strings.TrimPrefix(a1, artPrefix) {
		t.Fatalf("arte pública: %+v", arts[ref])
	}
	if err := l.UpdateEntry(ctx, EntryInput{Ref: ref, Meta: &EntryData{ExtMeta: ExtMeta{Cover: pub, Synopsis: "x"}}}, "adm"); err != nil {
		t.Fatal(err)
	}
	d, _ = l.GetEntryMeta(ctx, ref)
	if d.Cover != a1 || d.CoverSrc != srv.URL+"/capa.png" || d.Banner != "" {
		t.Fatalf("reenvio do endereço público: %+v", d)
	}
	// link quebrado fica como está (a rodada de fundo tenta de novo)
	if err := l.UpdateEntry(ctx, EntryInput{Ref: ref, Meta: &EntryData{ExtMeta: ExtMeta{Cover: srv.URL + "/sumiu.png"}}}, "adm"); err != nil {
		t.Fatal(err)
	}
	if d, _ = l.GetEntryMeta(ctx, ref); d.Cover != srv.URL+"/sumiu.png" {
		t.Fatalf("link quebrado: %+v", d)
	}
	// algo que não é link nem arte local é descartado
	if artStored("javascript:alert(1)") != "" || artStored("/api/art/../../x.png") != "" {
		t.Fatal("artStored aceitou lixo")
	}
	// rodada de fundo: ficha antiga com link
	l.w.Exec(`UPDATE entry_meta SET data=? WHERE ref=?`, mustJSON(EntryData{ExtMeta: ExtMeta{Banner: srv.URL + "/banner.png"}}), ref)
	l.localizeArtBacklog(ctx)
	if d, _ = l.GetEntryMeta(ctx, ref); !strings.HasPrefix(d.Banner, artPrefix) || d.BannerSrc == "" {
		t.Fatalf("backlog: %+v", d)
	}

	// servir: nome válido responde, nome estranho não
	r := mux.NewRouter()
	r.HandleFunc("/api/art/{file}", func(w http.ResponseWriter, r *http.Request) { libArtFileHandler(l, w, r) })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", pub, nil))
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), red) || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("GET arte: %d %q", rec.Code, rec.Header())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/art/..%2Flibrary.db", nil))
	if rec.Code == 200 {
		t.Fatal("serviu fora do acervo")
	}

	// envio do computador
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "x.png")
	fw.Write(blue)
	mw.Close()
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/admin/library/art", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	libArtStoreHandler(l, rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"url":"/api/art/`) {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	files, _ := filepath.Glob(filepath.Join(l.artDir(), "*"))
	if len(files) != 2 {
		t.Fatalf("acervo com %d arquivos (esperado 2): %v", len(files), files)
	}
	if _, err := os.Stat(l.artDir()); err != nil {
		t.Fatal(err)
	}
}
