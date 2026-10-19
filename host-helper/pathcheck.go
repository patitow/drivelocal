package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Erros sentinela. O handler HTTP mapeia para os códigos da API
// (invalid_path, path_denied, invalid_name).
var (
	ErrInvalidPath = errors.New("invalid_path")
	ErrPathDenied  = errors.New("path_denied")
	ErrInvalidName = errors.New("invalid_name")
)

const (
	maxHostPathLen = 260
	maxNameLen     = 64
)

var drivePathRe = regexp.MustCompile(`^[A-Za-z]:\\.+`)

// DenyConfig contém os diretórios de referência usados nas regras de bloqueio.
// Strings vazias são ignoradas (nunca casam com nada).
type DenyConfig struct {
	ProjectDir       string // raiz do projeto: não pode ser escolhida nem ser ancestral de algo escolhido
	HelperDataDir    string // host-helper/data: bloqueada (igual/dentro) e como ancestral
	UserProfile      string // %USERPROFILE%
	SystemRoot       string // %SystemRoot%
	ProgramFiles     string // %ProgramFiles%
	ProgramFilesX86  string // %ProgramFiles(x86)%
	ProgramData      string // %ProgramData%
	LocalAppData     string // %LOCALAPPDATA%
	DockerDesktopDir string // pasta do Docker Desktop (ProgramFiles\Docker\Docker)
}

// NewDenyConfig monta a configuração a partir do ambiente (getenv pode ser os.Getenv).
func NewDenyConfig(projectDir, helperDataDir string, getenv func(string) string) DenyConfig {
	cfg := DenyConfig{
		ProjectDir:      projectDir,
		HelperDataDir:   helperDataDir,
		UserProfile:     getenv("USERPROFILE"),
		SystemRoot:      getenv("SystemRoot"),
		ProgramFiles:    getenv("ProgramFiles"),
		ProgramFilesX86: getenv("ProgramFiles(x86)"),
		ProgramData:     getenv("ProgramData"),
		LocalAppData:    getenv("LOCALAPPDATA"),
	}
	if cfg.ProgramFiles != "" {
		cfg.DockerDesktopDir = filepath.Join(cfg.ProgramFiles, "Docker", "Docker")
	}
	return cfg
}

// NormalizeHostPath valida e normaliza um caminho absoluto do Windows
// (ex.: E:/Musicas/Rock -> E:\Musicas\Rock). Não toca no disco.
func NormalizeHostPath(p string) (string, error) {
	for _, r := range p {
		if unicode.IsControl(r) {
			return "", invalidPath("caractere de controle no caminho")
		}
	}
	s := strings.ReplaceAll(p, "/", `\`)
	if strings.HasPrefix(s, `\\`) {
		return "", invalidPath("caminhos UNC e estendidos não são suportados")
	}
	if strings.ContainsAny(s, `<>"|?*`) {
		return "", invalidPath("caractere inválido no caminho")
	}
	s = filepath.Clean(s)
	if len(s) > maxHostPathLen {
		return "", invalidPath("caminho com mais de 260 caracteres")
	}
	if !drivePathRe.MatchString(s) {
		return "", invalidPath(`use um caminho absoluto com letra de unidade, ex.: E:\Musicas`)
	}
	if strings.Contains(s[2:], ":") {
		return "", invalidPath("dois-pontos não é permitido após a letra da unidade")
	}
	for _, comp := range strings.Split(s[3:], `\`) {
		if strings.HasSuffix(comp, ".") || strings.HasSuffix(comp, " ") {
			return "", invalidPath("nome de pasta não pode terminar com ponto ou espaço")
		}
	}
	return s, nil
}

// CheckSelectable retorna nil se a pasta pode ser montada.
func CheckSelectable(p string, cfg DenyConfig) error {
	_, err := ResolveSelectable(p, cfg)
	return err
}

// ResolveSelectable valida a pasta e devolve o caminho resolvido (sem links
// simbólicos/junctions). Todas as regras valem tanto para o caminho informado
// quanto para o resolvido; é o resolvido que deve ir para o override.
func ResolveSelectable(p string, cfg DenyConfig) (string, error) {
	n, err := NormalizeHostPath(p)
	if err != nil {
		return "", err
	}
	if reason := denyReason(n, cfg); reason != "" {
		return "", denied(reason)
	}
	info, err := os.Stat(n)
	if err != nil {
		return "", invalidPath("pasta não encontrada ou inacessível")
	}
	if !info.IsDir() {
		return "", invalidPath("o caminho não é uma pasta")
	}
	r, err := filepath.EvalSymlinks(n)
	if err != nil {
		return "", invalidPath("não foi possível resolver o caminho")
	}
	r, err = NormalizeHostPath(r)
	if err != nil {
		return "", fmt.Errorf("caminho resolvido: %w", err)
	}
	if reason := denyReason(r, cfg); reason != "" {
		return "", denied("o destino real da pasta é protegido: " + reason)
	}
	return r, nil
}

// SanitizeName valida o nome da montagem dentro de /data.
func SanitizeName(s string) (string, error) {
	n := strings.TrimSpace(s)
	if n == "" {
		return "", invalidName("nome vazio")
	}
	if utf8.RuneCountInString(n) > maxNameLen {
		return "", invalidName("nome com mais de 64 caracteres")
	}
	if strings.HasPrefix(n, ".") {
		return "", invalidName("nome não pode começar com ponto")
	}
	for _, r := range n {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|$`, r) {
			return "", invalidName("caractere inválido no nome")
		}
	}
	return n, nil
}

// UniqueName devolve base, ou base com sufixo " (2)", " (3)"... se já existir
// (comparação case-insensitive), respeitando o limite de 64 caracteres.
func UniqueName(base string, existing []string) string {
	taken := make(map[string]bool, len(existing))
	for _, e := range existing {
		taken[strings.ToLower(e)] = true
	}
	if !taken[strings.ToLower(base)] {
		return base
	}
	runes := []rune(base)
	for i := 2; ; i++ {
		suffix := fmt.Sprintf(" (%d)", i)
		room := maxNameLen - utf8.RuneCountInString(suffix)
		b := runes
		if len(b) > room {
			b = b[:room]
		}
		cand := strings.TrimSpace(string(b)) + suffix
		if !taken[strings.ToLower(cand)] {
			return cand
		}
	}
}

func invalidPath(msg string) error {
	return fmt.Errorf("%w: %s", ErrInvalidPath, msg)
}

func invalidName(msg string) error {
	return fmt.Errorf("%w: %s", ErrInvalidName, msg)
}

func denied(reason string) error {
	return fmt.Errorf("%w: %s", ErrPathDenied, reason)
}

type denyRule struct {
	dir, reason string
}

// denyReason devolve o motivo do bloqueio para um caminho já normalizado,
// ou "" se a pasta pode ser escolhida. Não acessa o disco.
func denyReason(p string, cfg DenyConfig) string {
	c := canon(p)
	if c == "" {
		return "caminho vazio"
	}
	if len(c) <= 3 {
		return "raiz de unidade"
	}
	drive := c[:2] + `\`

	// Igual a, ou dentro de: diretórios do sistema e dados sensíveis.
	inside := []denyRule{
		{cfg.SystemRoot, "pasta do Windows"},
		{cfg.ProgramFiles, "Program Files"},
		{cfg.ProgramFilesX86, "Program Files (x86)"},
		{cfg.ProgramData, "ProgramData"},
		{sub(cfg.UserProfile, "AppData"), "dados de aplicativos do usuário"},
		{sub(cfg.UserProfile, ".ssh"), "chaves SSH"},
		{sub(cfg.UserProfile, ".docker"), "configuração do Docker"},
		{sub(cfg.UserProfile, ".aws"), "credenciais AWS"},
		{sub(cfg.UserProfile, ".config"), "configurações do usuário"},
		{sub(cfg.LocalAppData, "Docker"), "dados do Docker Desktop"},
		{cfg.HelperDataDir, "dados internos do helper"},
		{drive + `$Recycle.Bin`, "lixeira do Windows"},
		{drive + `System Volume Information`, "informações de volume do sistema"},
		{drive + `Recovery`, "partição de recuperação"},
	}
	for _, r := range inside {
		if within(c, canon(r.dir)) {
			return r.reason
		}
	}
	// Raiz de usuários de uma unidade (X:\Users), sem as subpastas.
	if c == canon(drive+"Users") {
		return "pasta de usuários (raiz)"
	}

	// Ancestrais (ou a própria pasta) de algo que não pode ser exposto.
	ancestors := []denyRule{
		{cfg.ProjectDir, "é ou contém a pasta do projeto"},
		{cfg.HelperDataDir, "contém dados internos do helper"},
		{cfg.UserProfile, "é ou contém o perfil do usuário"},
		{cfg.DockerDesktopDir, "contém o Docker Desktop"},
	}
	for _, r := range ancestors {
		if ancestorOrSelf(c, canon(r.dir)) {
			return r.reason
		}
	}
	return ""
}

// canon normaliza para comparação: barras invertidas, sem barra final, minúsculas.
func canon(p string) string {
	if p == "" {
		return ""
	}
	s := strings.ToLower(filepath.Clean(strings.ReplaceAll(p, "/", `\`)))
	if len(s) > 3 {
		s = strings.TrimSuffix(s, `\`)
	}
	return s
}

// within: c é igual a t ou está dentro de t (t já canônico).
func within(c, t string) bool {
	return t != "" && (c == t || strings.HasPrefix(c, t+`\`))
}

// ancestorOrSelf: t é igual a c ou está dentro de c (c contém t).
func ancestorOrSelf(c, t string) bool {
	return t != "" && (c == t || strings.HasPrefix(t, c+`\`))
}

func sub(base, name string) string {
	if base == "" {
		return ""
	}
	return filepath.Join(base, name)
}
