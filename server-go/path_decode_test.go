package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

func TestDecodeMediaPathKeepsPlusAndPercentDecoding(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "already decoded plus stays plus",
			raw:  `Animes/[CaféSub] Example Show [BD+EXTRAS][FLAC][EN]`,
			want: `Animes/[CaféSub] Example Show [BD+EXTRAS][FLAC][EN]`,
		},
		{
			name: "queryunescape would turn plus into space",
			raw:  `Animes/[CaféSub] Example Show [BD+EXTRAS][FLAC][EN]`,
			want: `Animes/[CaféSub] Example Show [BD+EXTRAS][FLAC][EN]`,
		},
		{
			name: "remaining percent encoding uses path unescape",
			raw:  `Animes/%5BCaf%C3%A9Sub%5D%20Show%5BBD%2BEXTRAS%5D`,
			want: "Animes/[CaféSub] Show[BD+EXTRAS]",
		},
		{
			name: "literal percent without hex stays",
			raw:  `folder/100%.mkv`,
			want: `folder/100%.mkv`,
		},
		{
			name: "empty",
			raw:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeMediaPath(tt.raw)
			if got != tt.want {
				t.Errorf("decodeMediaPath() = %q, want %q", got, tt.want)
			}
			if broken := mustQueryUnescape(tt.raw); strings.Contains(tt.raw, "+") && !strings.Contains(tt.raw, "%") && broken != tt.want {
				// Guard the regression: QueryUnescape is what used to 404 these folders.
				if got == broken {
					t.Errorf("decodeMediaPath must not match QueryUnescape for %q (got space-collapsed %q)", tt.raw, broken)
				}
			}
		})
	}
}

func mustQueryUnescape(s string) string {
	decoded, err := url.QueryUnescape(s)
	if err != nil {
		return s
	}
	return decoded
}

func TestQueryUnescapeTurnsPlusIntoSpace(t *testing.T) {
	raw := "[BD+ESPECIAIS]"
	if got := mustQueryUnescape(raw); got != "[BD ESPECIAIS]" {
		t.Fatalf("precondition: QueryUnescape(%q) = %q, want [BD ESPECIAIS]", raw, got)
	}
	if got := decodeMediaPath(raw); got != raw {
		t.Fatalf("decodeMediaPath(%q) = %q, want unchanged", raw, got)
	}
}

func specialMediaFolder() string {
	return "[CaféSub] Example Show [BD+EXTRAS][FLAC][EN]"
}

func setupSpecialMediaTree(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	original := MEDIA_PATH
	MEDIA_PATH = tempDir
	t.Cleanup(func() { MEDIA_PATH = original })

	folder := filepath.Join(tempDir, "Animes", specialMediaFolder())
	if err := os.MkdirAll(folder, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "episode.mkv"), []byte("video-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "cover.jpg"), []byte("jpeg-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	return tempDir
}

func encodeURIComponent(s string) string {
	// QueryEscape turns space into '+' and '+' into '%2B'. encodeURIComponent
	// uses '%20' for spaces, which is what the React client sends.
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func encodePathSegments(rel string) string {
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func TestFilesHandlerSpecialCharacters(t *testing.T) {
	setupSpecialMediaTree(t)
	rel := "Animes/" + specialMediaFolder()

	encodings := []struct {
		name string
		raw  string
	}{
		{
			name: "url.Values (space as plus, plus as %2B)",
			raw:  "/api/files?" + url.Values{"path": {rel}}.Encode(),
		},
		{
			name: "encodeURIComponent-style (space as %20, plus as %2B)",
			raw:  "/api/files?path=" + encodeURIComponent(rel),
		},
	}

	for _, enc := range encodings {
		t.Run(enc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", enc.raw, nil)
			w := httptest.NewRecorder()
			filesHandler(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status %d body=%s url=%s", w.Code, w.Body.String(), enc.raw)
			}
			var response struct {
				Items []struct {
					Name string `json:"name"`
				} `json:"items"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if len(response.Items) < 2 {
				t.Fatalf("expected files in special folder, got %#v", response.Items)
			}
		})
	}
}

func TestMuxHandlersSpecialCharacters(t *testing.T) {
	setupSpecialMediaTree(t)
	rel := "Animes/" + specialMediaFolder() + "/episode.mkv"
	imageRel := "Animes/" + specialMediaFolder() + "/cover.jpg"

	router := mux.NewRouter()
	router.HandleFunc("/api/files", filesHandler).Methods("GET")
	router.HandleFunc("/api/download/{path:.*}", downloadHandler).Methods("GET")
	router.HandleFunc("/api/stream/{path:.*}", streamHandler).Methods("GET")
	router.HandleFunc("/api/thumbnail/{path:.*}", thumbnailHandler).Methods("GET")

	t.Run("list folder via mux", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/files?"+url.Values{"path": {rel[:strings.LastIndex(rel, "/")]}}.Encode(), nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("files status %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("download whole-path encodeURIComponent", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/download/"+url.PathEscape(rel), nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("download status %d body=%s", w.Code, w.Body.String())
		}
		if w.Body.String() != "video-bytes" {
			t.Fatalf("download body = %q", w.Body.String())
		}
	})

	t.Run("stream keeps plus in name", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/stream/"+encodePathSegments(rel), nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("stream status %d body=%s", w.Code, w.Body.String())
		}
		if w.Body.String() != "video-bytes" {
			t.Fatalf("stream body = %q", w.Body.String())
		}
	})

	t.Run("thumbnail image is not 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/thumbnail/"+encodePathSegments(imageRel), nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Fatalf("thumbnail 404 body=%s", w.Body.String())
		}
		if w.Code != http.StatusOK && w.Code != http.StatusAccepted {
			t.Fatalf("thumbnail status %d body=%s", w.Code, w.Body.String())
		}
	})
}

func filesItemNames(t *testing.T, path string) []string {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/files?path="+encodeURIComponent(path), nil)
	w := httptest.NewRecorder()
	filesHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/files?path=%q -> %d %s", path, w.Code, w.Body.String())
	}
	var response struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(response.Items))
	for _, item := range response.Items {
		names = append(names, item.Name)
	}
	return names
}

func containName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func TestMixedMediaTreeListingDownloadAndStream(t *testing.T) {
	tempDir := t.TempDir()
	original := MEDIA_PATH
	MEDIA_PATH = tempDir
	t.Cleanup(func() { MEDIA_PATH = original })

	writeFile := func(rel string, body string) {
		t.Helper()
		full := filepath.Join(append([]string{tempDir}, strings.Split(rel, "/")...)...)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Realistic mix: names that always worked, plus the ones that 404'd.
	writeFile("Animes/Naruto/ep01.mkv", "naruto")
	writeFile("Movies/Simple Movie.mp4", "movie")
	writeFile("Torrents/Animes/notes.txt", "notes")
	writeFile("Recorded Videos/Show Name/clip.mp4", "clip")
	writeFile("Animes/"+specialMediaFolder()+"/episode.mkv", "nuki")

	listings := []struct {
		path string
		want []string
	}{
		{path: "", want: []string{"Animes", "Movies", "Torrents", "Recorded Videos"}},
		{path: "Animes", want: []string{"Naruto", specialMediaFolder()}},
		{path: "Animes/Naruto", want: []string{"ep01.mkv"}},
		{path: "Movies", want: []string{"Simple Movie.mp4"}},
		{path: "Torrents", want: []string{"Animes"}},
		{path: "Torrents/Animes", want: []string{"notes.txt"}},
		{path: "Recorded Videos", want: []string{"Show Name"}},
		{path: "Recorded Videos/Show Name", want: []string{"clip.mp4"}},
		{path: "Animes/" + specialMediaFolder(), want: []string{"episode.mkv"}},
	}

	for _, listing := range listings {
		t.Run("list "+listing.path, func(t *testing.T) {
			names := filesItemNames(t, listing.path)
			for _, want := range listing.want {
				if !containName(names, want) {
					t.Fatalf("path %q missing %q, got %v", listing.path, want, names)
				}
			}
		})
	}

	router := mux.NewRouter()
	router.HandleFunc("/api/download/{path:.*}", downloadHandler).Methods("GET")
	router.HandleFunc("/api/stream/{path:.*}", streamHandler).Methods("GET")

	fileCases := []struct {
		rel  string
		body string
	}{
		{rel: "Animes/Naruto/ep01.mkv", body: "naruto"},
		{rel: "Movies/Simple Movie.mp4", body: "movie"},
		{rel: "Torrents/Animes/notes.txt", body: "notes"},
		{rel: "Recorded Videos/Show Name/clip.mp4", body: "clip"},
		{rel: "Animes/" + specialMediaFolder() + "/episode.mkv", body: "nuki"},
	}

	for _, fileCase := range fileCases {
		t.Run("download "+fileCase.rel, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/download/"+encodePathSegments(fileCase.rel), nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("download %q -> %d %s", fileCase.rel, w.Code, w.Body.String())
			}
			if w.Body.String() != fileCase.body {
				t.Fatalf("download %q body = %q, want %q", fileCase.rel, w.Body.String(), fileCase.body)
			}
		})
		t.Run("stream "+fileCase.rel, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/stream/"+encodePathSegments(fileCase.rel), nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("stream %q -> %d %s", fileCase.rel, w.Code, w.Body.String())
			}
			if w.Body.String() != fileCase.body {
				t.Fatalf("stream %q body = %q, want %q", fileCase.rel, w.Body.String(), fileCase.body)
			}
		})
	}
}

func TestFilesHandlerNestedSpecialNamesDoNot404(t *testing.T) {
	tempDir := t.TempDir()
	original := MEDIA_PATH
	MEDIA_PATH = tempDir
	t.Cleanup(func() { MEDIA_PATH = original })

	names := []string{
		"Animes",
		"Show [BD+ESPECIAIS]",
		"folder & extras",
		"100% free",
		"name (2024)",
	}
	full := tempDir
	relParts := []string{}
	for _, name := range names {
		full = filepath.Join(full, name)
		relParts = append(relParts, name)
	}
	if err := os.MkdirAll(full, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(full, "ok.txt"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}

	// Walk each prefix like the UI does when opening folders.
	for i := 1; i <= len(relParts); i++ {
		rel := strings.Join(relParts[:i], "/")
		req := httptest.NewRequest("GET", "/api/files?"+url.Values{"path": {rel}}.Encode(), nil)
		w := httptest.NewRecorder()
		filesHandler(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("path %q returned %d: %s", rel, w.Code, w.Body.String())
		}
	}
}
