package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeHostPath(t *testing.T) {
	ok := []struct{ in, want string }{
		{`E:\Musicas\Rock`, `E:\Musicas\Rock`},
		{`E:/Musicas/Rock`, `E:\Musicas\Rock`},
		{`e:\musicas\`, `e:\musicas`},
		{`E:\a\..\b`, `E:\b`},
		{`E:\Musicas\.\Rock`, `E:\Musicas\Rock`},
		{`D:\Filmes e Séries`, `D:\Filmes e Séries`},
	}
	for _, tc := range ok {
		got, err := NormalizeHostPath(tc.in)
		if err != nil {
			t.Errorf("NormalizeHostPath(%q) erro inesperado: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeHostPath(%q) = %q, quer %q", tc.in, got, tc.want)
		}
	}

	bad := []string{
		``,
		`\\server\share\x`,
		`\\?\E:\x`,
		`\\.\E:\x`,
		`Musicas\Rock`,
		`E:Musicas`,
		`E:\`,
		`E:/`,
		`E:\Musicas:stream`,
		"E:\\Musicas\x01",
		`E:\Music<a`,
		`E:\Music>a`,
		`E:\Music"a`,
		`E:\Music|a`,
		`E:\Music*a`,
		`E:\Musicas.`,
		`E:\Musicas `,
		`E:\` + strings.Repeat("a", 261),
	}
	for _, in := range bad {
		if got, err := NormalizeHostPath(in); err == nil {
			t.Errorf("NormalizeHostPath(%q) = %q, quer erro", in, got)
		} else if !errors.Is(err, ErrInvalidPath) {
			t.Errorf("NormalizeHostPath(%q) erro = %v, quer ErrInvalidPath", in, err)
		}
	}
}

func TestSanitizeName(t *testing.T) {
	ok := map[string]string{
		"Rock":                  "Rock",
		"  Rock  ":              "Rock",
		"Músicas Été":           "Músicas Été",
		"Filmes 2024 (HD)":      "Filmes 2024 (HD)",
		strings.Repeat("a", 64): strings.Repeat("a", 64),
	}
	for in, want := range ok {
		got, err := SanitizeName(in)
		if err != nil || got != want {
			t.Errorf("SanitizeName(%q) = %q, %v; quer %q", in, got, err, want)
		}
	}

	bad := []string{
		"",
		"   ",
		".oculto",
		".",
		"..",
		"a/b",
		`a\b`,
		"a:b",
		"a*",
		"a?",
		`a"`,
		"a<",
		"a>",
		"a|",
		"a$b",
		"$",
		"a\x01",
		strings.Repeat("a", 65),
	}
	for _, in := range bad {
		if got, err := SanitizeName(in); err == nil {
			t.Errorf("SanitizeName(%q) = %q, quer erro", in, got)
		} else if !errors.Is(err, ErrInvalidName) {
			t.Errorf("SanitizeName(%q) erro = %v, quer ErrInvalidName", in, err)
		}
	}
}

func TestUniqueName(t *testing.T) {
	cases := []struct {
		base     string
		existing []string
		want     string
	}{
		{"Filmes", nil, "Filmes"},
		{"Filmes", []string{"Musicas"}, "Filmes"},
		{"Filmes", []string{"Filmes"}, "Filmes (2)"},
		{"Filmes", []string{"filmes", "FILMES (2)"}, "Filmes (3)"},
		{"Filmes", []string{"Filmes", "Filmes (2)", "Filmes (3)"}, "Filmes (4)"},
	}
	for _, tc := range cases {
		if got := UniqueName(tc.base, tc.existing); got != tc.want {
			t.Errorf("UniqueName(%q, %v) = %q, quer %q", tc.base, tc.existing, got, tc.want)
		}
	}

	long := strings.Repeat("a", 64)
	got := UniqueName(long, []string{long})
	if utf8.RuneCountInString(got) > 64 {
		t.Errorf("UniqueName com nome de 64 chars passou de 64: %q (%d)", got, len(got))
	}
	if !strings.HasSuffix(got, " (2)") {
		t.Errorf("UniqueName com nome longo = %q, quer sufixo (2)", got)
	}
}

// Regras de bloqueio sem tocar no disco.
func TestDenyReason(t *testing.T) {
	cfg := DenyConfig{
		ProjectDir:       `E:\Programação\drivelocal`,
		HelperDataDir:    `E:\Programação\drivelocal\host-helper\data`,
		UserProfile:      `C:\Users\usuario`,
		SystemRoot:       `C:\Windows`,
		ProgramFiles:     `C:\Program Files`,
		ProgramFilesX86:  `C:\Program Files (x86)`,
		ProgramData:      `C:\ProgramData`,
		LocalAppData:     `C:\Users\usuario\AppData\Local`,
		DockerDesktopDir: `C:\Program Files\Docker\Docker`,
	}
	cases := []struct {
		path   string
		denied bool
	}{
		// Sistema
		{`C:\Windows`, true},
		{`c:\WINDOWS\System32`, true},
		{`C:\Windows.old`, false},
		{`C:\Program Files\X`, true},
		{`C:\Program Files (x86)`, true},
		{`C:\ProgramData\foo`, true},
		// Perfil e dados sensíveis
		{`C:\Users\usuario`, true},
		{`C:\Users`, true},
		{`C:\Users\usuario\AppData`, true},
		{`C:\Users\usuario\AppData\Roaming\x`, true},
		{`C:\Users\usuario\AppData\Local\Docker`, true},
		{`C:\Users\usuario\.ssh`, true},
		{`C:\Users\usuario\.docker`, true},
		{`C:\Users\usuario\.aws`, true},
		{`C:\Users\usuario\.config`, true},
		{`C:\Users\usuario\Videos`, false},
		{`C:\Users\Public\Videos`, false},
		// Raiz de unidade e lixeira etc. em qualquer unidade
		{`D:\$Recycle.Bin\x`, true},
		{`D:\System Volume Information`, true},
		{`D:\Recovery`, true},
		{`D:\Users`, true},
		{`D:\Users\Public`, false},
		// Projeto e helper
		{`E:\Programação`, true},
		{`E:\Programação\drivelocal`, true},
		{`E:\Programação\drivelocal\client`, false},
		{`E:\Programação\drivelocal\host-helper`, true},
		{`E:\Programação\drivelocal\host-helper\data`, true},
		{`E:\Programação\drivelocal\host-helper\data\backups`, true},
		// Livres
		{`D:\Filmes`, false},
		{`D:\Filmes\Ação`, false},
	}
	for _, tc := range cases {
		reason := denyReason(mustNormalize(t, tc.path), cfg)
		if got := reason != ""; got != tc.denied {
			t.Errorf("denyReason(%q) = %q, bloqueado=%v; quer bloqueado=%v", tc.path, reason, got, tc.denied)
		}
	}
}

// Raiz de unidade (já rejeitada por NormalizeHostPath) também é bloqueada em denyReason.
func TestDenyReasonDriveRoot(t *testing.T) {
	if reason := denyReason(`D:\`, DenyConfig{}); reason != "raiz de unidade" {
		t.Errorf("denyReason(D:\\) = %q, quer raiz de unidade", reason)
	}
}

// Configuração vazia não pode bloquear tudo por casar com "".
func TestDenyReasonEmptyConfig(t *testing.T) {
	if reason := denyReason(`D:\Filmes`, DenyConfig{}); reason != "" {
		t.Errorf("config vazia bloqueou D:\\Filmes: %q", reason)
	}
}

func TestNewDenyConfig(t *testing.T) {
	env := map[string]string{
		"USERPROFILE":       `C:\Users\usuario`,
		"SystemRoot":        `C:\Windows`,
		"ProgramFiles":      `C:\Program Files`,
		"ProgramFiles(x86)": `C:\Program Files (x86)`,
		"ProgramData":       `C:\ProgramData`,
		"LOCALAPPDATA":      `C:\Users\usuario\AppData\Local`,
	}
	cfg := NewDenyConfig(`E:\p`, `E:\p\host-helper\data`, func(k string) string { return env[k] })
	if cfg.ProjectDir != `E:\p` || cfg.HelperDataDir != `E:\p\host-helper\data` {
		t.Errorf("ProjectDir/HelperDataDir incorretos: %+v", cfg)
	}
	if cfg.UserProfile != `C:\Users\usuario` || cfg.SystemRoot != `C:\Windows` ||
		cfg.ProgramFilesX86 != `C:\Program Files (x86)` || cfg.ProgramData != `C:\ProgramData` ||
		cfg.LocalAppData != `C:\Users\usuario\AppData\Local` {
		t.Errorf("variáveis de ambiente não carregadas: %+v", cfg)
	}
	if cfg.DockerDesktopDir != filepath.Join(`C:\Program Files`, "Docker", "Docker") {
		t.Errorf("DockerDesktopDir = %q", cfg.DockerDesktopDir)
	}
}

// Testes de integração com diretórios reais em t.TempDir().
func TestResolveSelectable(t *testing.T) {
	base := t.TempDir()
	mustMkdir(t, filepath.Join(base, "Filmes", "Ação"))
	mustMkdir(t, filepath.Join(base, "proj", "sub"))
	if err := os.WriteFile(filepath.Join(base, "arquivo.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := DenyConfig{ProjectDir: filepath.Join(base, "proj")}

	filmes := filepath.Join(base, "Filmes")
	wantFilmes, err := filepath.EvalSymlinks(filmes)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ResolveSelectable(filmes, cfg)
	if err != nil {
		t.Fatalf("ResolveSelectable(Filmes) erro: %v", err)
	}
	if !strings.EqualFold(got, wantFilmes) {
		t.Errorf("ResolveSelectable(Filmes) = %q, quer %q", got, wantFilmes)
	}

	// Barras normais também funcionam.
	if _, err := ResolveSelectable(strings.ReplaceAll(filmes, `\`, "/")+"/Ação", cfg); err != nil {
		t.Errorf("caminho com barras normais: %v", err)
	}

	if err := CheckSelectable(filepath.Join(base, "proj", "sub"), cfg); err != nil {
		t.Errorf("subpasta do projeto deveria ser permitida: %v", err)
	}

	denyCases := []string{
		filepath.Join(base, "proj"),        // o próprio projeto
		base,                               // ancestral do projeto
		filepath.Join(base, "arquivo.txt"), // não é pasta
		filepath.Join(base, "inexistente"), // não existe
	}
	for _, p := range denyCases {
		err := CheckSelectable(p, cfg)
		if err == nil {
			t.Errorf("CheckSelectable(%q) deveria falhar", p)
			continue
		}
		if !errors.Is(err, ErrPathDenied) && !errors.Is(err, ErrInvalidPath) {
			t.Errorf("CheckSelectable(%q) erro = %v", p, err)
		}
	}
	if err := CheckSelectable(base, cfg); !errors.Is(err, ErrPathDenied) {
		t.Errorf("ancestral do projeto: erro = %v, quer ErrPathDenied", err)
	}
	if err := CheckSelectable(filepath.Join(base, "arquivo.txt"), cfg); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("arquivo: erro = %v, quer ErrInvalidPath", err)
	}
}

// Junction apontando para pasta protegida deve ser bloqueada pelo destino real.
// Junction para pasta permitida deve ser resolvida para o destino.
func TestResolveSelectableJunction(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "real")
	proj := filepath.Join(base, "proj")
	mustMkdir(t, target)
	mustMkdir(t, proj)

	goodLink := filepath.Join(base, "atalho")
	badLink := filepath.Join(base, "atalho-projeto")
	if !mklinkJunction(goodLink, target) || !mklinkJunction(badLink, proj) {
		t.Skip("não foi possível criar junction com mklink /J")
	}

	cfg := DenyConfig{ProjectDir: proj}

	got, err := ResolveSelectable(goodLink, cfg)
	if err != nil {
		t.Fatalf("junction para pasta permitida: %v", err)
	}
	wantTarget, _ := filepath.EvalSymlinks(target)
	if !strings.EqualFold(got, wantTarget) {
		t.Errorf("junction resolvido = %q, quer %q", got, wantTarget)
	}

	if err := CheckSelectable(badLink, cfg); !errors.Is(err, ErrPathDenied) {
		t.Errorf("junction para o projeto: erro = %v, quer ErrPathDenied", err)
	}
}

func mustNormalize(t *testing.T, p string) string {
	t.Helper()
	n, err := NormalizeHostPath(p)
	if err != nil {
		t.Fatalf("NormalizeHostPath(%q): %v", p, err)
	}
	return n
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mklinkJunction(link, target string) bool {
	return exec.Command("cmd", "/c", "mklink", "/J", link, target).Run() == nil
}
