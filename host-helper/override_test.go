package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Para regenerar os golden files: UPDATE_GOLDEN=1 go test ./...
func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want := readFixture(t, name)
	if !bytes.Equal(got, want) {
		t.Errorf("%s difere do golden.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// writeTemp grava um override temporário e devolve o caminho.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "docker-compose.override.yml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMountsShortAndLongSyntax(t *testing.T) {
	o, err := LoadOverride(filepath.Join("testdata", "basic.yml"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Mount{
		{Name: "Filmes", HostPath: "E:/Filmes", Target: "/data/Filmes", ReadOnly: true, Legacy: true},
		{Name: "Meus Filmes", HostPath: "E:/Meus Filmes", Target: "/data/Meus Filmes", ReadOnly: true, Legacy: true},
		{Name: ".Fotos", HostPath: "D:/Fotos", Target: "/data/.Fotos", ReadOnly: true, Legacy: true},
		{Name: "Docs", HostPath: "C:/Docs", Target: "/data/Docs", ReadOnly: false, Legacy: true},
		{Name: "Musicas", HostPath: `E:\Musicas`, Target: "/data/Musicas", ReadOnly: true, Legacy: false},
	}
	if got := o.Mounts(); !reflect.DeepEqual(got, want) {
		t.Errorf("Mounts() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestParseShortEntries(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want Mount
	}{
		{in: "E:/x:/data/a", ok: true, want: Mount{Name: "a", HostPath: "E:/x", Target: "/data/a", Legacy: true}},
		{in: "  E:/x  :  /data/a  :  ro  ", ok: true, want: Mount{Name: "a", HostPath: "E:/x", Target: "/data/a", ReadOnly: true, Legacy: true}},
		{in: "E:/Meus Pastas/y:/data/Meu nome:rw", ok: true, want: Mount{Name: "Meu nome", HostPath: "E:/Meus Pastas/y", Target: "/data/Meu nome", Legacy: true}},
		{in: "E:/x:/data/.oculto:ro", ok: true, want: Mount{Name: ".oculto", HostPath: "E:/x", Target: "/data/.oculto", ReadOnly: true, Legacy: true}},
		{in: "./logs:/app/logs", ok: false},
	}
	for _, c := range cases {
		n := scalarEntry(c.in)
		got, ok, err := parseEntry(n)
		if err != nil {
			t.Errorf("parseEntry(%q) erro inesperado: %v", c.in, err)
			continue
		}
		if ok != c.ok {
			t.Errorf("parseEntry(%q) ok=%v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseEntry(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestUnrecognizedDataEntryIsError(t *testing.T) {
	for _, in := range []string{"E:/x:/data/a:weird", "E:/x:/data/a/b"} {
		n := scalarEntry(in)
		if _, _, err := parseEntry(n); err == nil {
			t.Errorf("parseEntry(%q) deveria falhar", in)
		}
	}
	p := writeTemp(t, "services:\n  drivelocal:\n    volumes:\n      - \"E:/x:/data/a:weird\"\n")
	if _, err := LoadOverride(p); err == nil {
		t.Error("LoadOverride com entrada /data não reconhecida deveria falhar")
	}
}

func TestAddGoldenKeepsCommentsAndOtherKeys(t *testing.T) {
	o, err := LoadOverride(filepath.Join("testdata", "basic.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Add(Mount{Name: "Novo Pasta", HostPath: `F:\Novo Pasta`}); err != nil {
		t.Fatal(err)
	}
	got := o.Bytes()
	for _, must := range []string{
		"# Override de teste",
		"# ambiente do servidor",
		"# volume que não é biblioteca",
		"TZ=America/Sao_Paulo",
		"8080:8080",
		"./logs:/app/logs",
	} {
		if !strings.Contains(string(got), must) {
			t.Errorf("saída perdeu %q", must)
		}
	}
	checkGolden(t, "basic_added.yml", got)
}

func TestAddThenRemoveRestoresOriginalFile(t *testing.T) {
	for _, fixture := range []string{"basic.yml", "no_volumes.yml"} {
		t.Run(fixture, func(t *testing.T) {
			orig := readFixture(t, fixture)
			o, err := LoadOverride(writeTemp(t, string(orig)))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(o.Bytes(), orig) {
				t.Fatal("Bytes() sem mudança deveria ser o arquivo original")
			}
			if err := o.Add(Mount{Name: "Novo", HostPath: `F:\Novo`}); err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(o.Bytes(), orig) {
				t.Fatal("Add não alterou o conteúdo")
			}
			if err := o.Remove("Novo"); err != nil {
				t.Fatal(err)
			}
			if got := o.Bytes(); !bytes.Equal(got, orig) {
				t.Errorf("add+remove não devolveu o original.\n--- got ---\n%s\n--- want ---\n%s", got, orig)
			}
		})
	}
}

func TestRemoveOnlyTargetEntry(t *testing.T) {
	o, err := LoadOverride(filepath.Join("testdata", "basic.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Remove("Filmes"); err != nil {
		t.Fatal(err)
	}
	mounts := o.Mounts()
	if len(mounts) != 4 {
		t.Fatalf("esperava 4 bibliotecas, veio %d", len(mounts))
	}
	for _, m := range mounts {
		if m.Name == "Filmes" {
			t.Error("Filmes continua presente após Remove")
		}
	}
	got := string(o.Bytes())
	for _, must := range []string{"./logs:/app/logs", "TZ=America/Sao_Paulo", "# volume que não é biblioteca", `E:\Musicas`} {
		if !strings.Contains(got, must) {
			t.Errorf("Remove apagou %q", must)
		}
	}
}

func TestRemoveMissingAndAddErrors(t *testing.T) {
	o, err := LoadOverride(filepath.Join("testdata", "basic.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Remove("Inexistente"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove inexistente: err=%v, want ErrNotFound", err)
	}
	if err := o.Add(Mount{Name: "filmes", HostPath: `F:\x`}); !errors.Is(err, ErrExists) {
		t.Errorf("Add duplicado (case-insensitive): err=%v, want ErrExists", err)
	}
	for _, bad := range []string{"", "a/b", "..", "."} {
		if err := o.Add(Mount{Name: bad, HostPath: `F:\x`}); err == nil {
			t.Errorf("Add com nome %q deveria falhar", bad)
		}
	}
	if err := o.Add(Mount{Name: "ok", HostPath: "  "}); err == nil {
		t.Error("Add com hostPath vazio deveria falhar")
	}
}

func TestMissingFileBecomesSkeleton(t *testing.T) {
	p := filepath.Join(t.TempDir(), "docker-compose.override.yml")
	o, err := LoadOverride(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Mounts()) != 0 {
		t.Fatal("esqueleto não deveria ter bibliotecas")
	}
	initial := o.Bytes()
	if err := o.Add(Mount{Name: "Filmes", HostPath: `E:\Filmes`}); err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "skeleton_added.yml", o.Bytes())
	if err := o.Remove("Filmes"); err != nil {
		t.Fatal(err)
	}
	if got := o.Bytes(); !bytes.Equal(got, initial) {
		t.Errorf("add+remove no esqueleto não voltou ao inicial.\n%s", got)
	}
}

func TestInvalidYAMLLeavesFileUntouched(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "docker-compose.override.yml")
	before := readFixture(t, "invalid.yml")
	if err := os.WriteFile(p, before, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOverride(p); err == nil {
		t.Fatal("YAML inválido deveria retornar erro")
	}
	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("arquivo inválido foi alterado")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("não deveria criar nada no diretório, há %d entradas", len(entries))
	}
}

func TestRevisionTracksContent(t *testing.T) {
	orig := readFixture(t, "basic.yml")
	o, err := LoadOverride(writeTemp(t, string(orig)))
	if err != nil {
		t.Fatal(err)
	}
	r0 := o.Revision()
	if len(r0) != 64 {
		t.Fatalf("revision deveria ser sha256 hex, veio %q", r0)
	}
	if err := o.Add(Mount{Name: "Novo", HostPath: `F:\Novo`}); err != nil {
		t.Fatal(err)
	}
	if o.Revision() == r0 {
		t.Error("revision não mudou após Add")
	}
	if err := o.Remove("Novo"); err != nil {
		t.Fatal(err)
	}
	if o.Revision() != r0 {
		t.Error("revision não voltou após Remove")
	}
}

func TestWriteAtomicKeepsTenBackups(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "docker-compose.override.yml")
	backups := filepath.Join(dir, "backups")

	if bk, err := WriteAtomic(p, []byte("v0"), backups); err != nil || bk != "" {
		t.Fatalf("primeira escrita: bk=%q err=%v (não deveria haver backup)", bk, err)
	}
	var last string
	for i := 1; i <= 12; i++ {
		bk, err := WriteAtomic(p, []byte(fmt.Sprintf("v%d", i)), backups)
		if err != nil {
			t.Fatal(err)
		}
		if bk == "" {
			t.Fatalf("escrita %d sem backup", i)
		}
		last = bk
	}
	entries, err := os.ReadDir(backups)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != backupKeep {
		t.Errorf("backups mantidos = %d, want %d", len(entries), backupKeep)
	}
	if b, _ := os.ReadFile(last); string(b) != "v11" {
		t.Errorf("último backup = %q, want v11", b)
	}
	if b, _ := os.ReadFile(p); string(b) != "v12" {
		t.Errorf("arquivo = %q, want v12", b)
	}
	top, _ := os.ReadDir(dir)
	for _, e := range top {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("sobrou arquivo temporário %s", e.Name())
		}
	}
}

// hostPath com "$" é gravado escapado ("$$") para o docker compose não interpolar,
// e lido de volta igual ao original, na sintaxe longa e na curta.
func TestDollarInHostPathRoundTrip(t *testing.T) {
	const hostPath = `E:\Jogos$Backup`
	p := writeTemp(t, skeletonYAML)
	o, err := LoadOverride(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Add(Mount{Name: "Backup", HostPath: hostPath}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	data := o.Bytes()
	if !bytes.Contains(data, []byte(`E:\Jogos$$Backup`)) {
		t.Fatalf("YAML não contém o valor escapado:\n%s", data)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	o2, err := LoadOverride(p)
	if err != nil {
		t.Fatalf("reler: %v", err)
	}
	mounts := o2.Mounts()
	if len(mounts) != 1 || mounts[0].HostPath != hostPath || mounts[0].Target != "/data/Backup" {
		t.Fatalf("mounts = %+v, quer HostPath %q", mounts, hostPath)
	}

	// sintaxe curta gravada com escape também volta ao original
	short := writeTemp(t, "services:\n  drivelocal:\n    volumes:\n      - E:\\Jogos$$Backup:/data/Curto\n      - E:\\Antigo$x:/data/Antigo\n")
	o3, err := LoadOverride(short)
	if err != nil {
		t.Fatal(err)
	}
	got := o3.Mounts()
	if len(got) != 2 || got[0].HostPath != hostPath || got[0].Name != "Curto" || got[1].HostPath != `E:\Antigo$x` {
		t.Fatalf("sintaxe curta: mounts = %+v", got)
	}
}

func scalarEntry(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}
