package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	helperVersion   = "1.0.0"
	defaultListen   = "127.0.0.1:47123"
	tokenEnvKey     = "MOUNT_HELPER_TOKEN"
	overrideFile    = "docker-compose.override.yml"
	envFile         = ".env"
	dataDirRelative = "host-helper/data"
	logFileName     = "helper.log"
)

var tokenRe = regexp.MustCompile(`^[0-9a-fA-F]{64,}$`)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "init" {
		if err := runInit(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "erro:", err)
			os.Exit(1)
		}
		return
	}
	if err := runServer(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

// runServer sobe o helper em loopback até receber SIGINT/SIGTERM.
func runServer(args []string) error {
	fs := flag.NewFlagSet("drivelocal-helper", flag.ContinueOnError)
	projectFlag := fs.String("project-dir", "", "raiz do projeto (padrão: pasta pai da pasta do executável)")
	listen := fs.String("listen", defaultListen, "endereço de escuta (somente loopback)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := checkLoopbackListen(*listen); err != nil {
		return err
	}

	projectDir, err := resolveProjectDir(*projectFlag)
	if err != nil {
		return err
	}
	dataDir := filepath.Join(projectDir, filepath.FromSlash(dataDirRelative))
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(dataDir, logFileName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	log.SetOutput(logFile)
	log.SetFlags(log.LstdFlags)

	token := os.Getenv(tokenEnvKey)
	if token == "" {
		token = readEnvValue(filepath.Join(projectDir, envFile), tokenEnvKey)
	}
	if !validToken(token) {
		return fmt.Errorf("%s ausente ou inválido (esperado 64+ caracteres hex); rode 'drivelocal-helper init'", tokenEnvKey)
	}

	overridePath := filepath.Join(projectDir, overrideFile)
	runner, err := NewRunner(overridePath, filepath.Join(dataDir, "backups"), dataDir,
		NewComposeDocker(projectDir, overridePath))
	if err != nil {
		return err
	}
	srv := &server{
		token:        []byte(token),
		runner:       runner,
		overridePath: overridePath,
		deny:         NewDenyConfig(projectDir, dataDir, os.Getenv),
	}

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	httpSrv := &http.Server{Handler: srv.routes(), ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("helper %s ouvindo em %s (projeto %s)", helperVersion, ln.Addr(), projectDir)

	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		log.Printf("encerrando")
		return httpSrv.Shutdown(shutdownCtx)
	}
}

// runInit gera o token (se ainda não houver um válido) e grava MOUNT_HELPER_TOKEN no .env.
func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	projectFlag := fs.String("project-dir", "", "raiz do projeto (padrão: pasta pai da pasta do executável)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	projectDir, err := resolveProjectDir(*projectFlag)
	if err != nil {
		return err
	}
	path := filepath.Join(projectDir, envFile)
	current, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	token, err := newToken()
	if err != nil {
		return err
	}
	updated, changed := upsertToken(string(current), token)
	if !changed {
		fmt.Println("MOUNT_HELPER_TOKEN já configurado em", path)
		return nil
	}
	if _, err := WriteAtomic(path, []byte(updated), ""); err != nil {
		return err
	}
	fmt.Println("MOUNT_HELPER_TOKEN gravado em", path)
	return nil
}

// resolveProjectDir devolve o caminho absoluto da raiz do projeto. Sem flag, usa
// a pasta pai da pasta onde está o executável (o exe fica em host-helper/).
func resolveProjectDir(flagValue string) (string, error) {
	dir := flagValue
	if dir == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		dir = filepath.Dir(filepath.Dir(exe))
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("raiz do projeto inválida: %s", abs)
	}
	return abs, nil
}

// checkLoopbackListen recusa endereços que não sejam IP literal de loopback.
func checkLoopbackListen(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("--listen inválido: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("--listen deve ser um IP de loopback (127.0.0.1 ou ::1), recebido %q", host)
	}
	return nil
}

func validToken(t string) bool {
	return tokenRe.MatchString(t)
}

func newToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// readEnvValue lê KEY=valor de um arquivo .env simples ("" se ausente).
func readEnvValue(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, key+"=") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(t, key+"=")), `"'`)
		}
	}
	return ""
}

// upsertToken acrescenta MOUNT_HELPER_TOKEN ao conteúdo do .env sem duplicar.
// Se já existir um token válido, devolve o conteúdo intacto e changed=false.
// Linhas antigas com a chave (inválidas ou repetidas) são removidas.
func upsertToken(content, token string) (string, bool) {
	crlf := strings.Contains(content, "\r\n")
	var kept []string
	existing := ""
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSuffix(line, "\r")
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, tokenEnvKey+"=") {
			if existing == "" {
				existing = strings.Trim(strings.TrimSpace(strings.TrimPrefix(t, tokenEnvKey+"=")), `"'`)
			}
			continue
		}
		kept = append(kept, line)
	}
	if validToken(existing) {
		return content, false
	}
	// remove linhas vazias no fim para não acumular quebras
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	nl := "\n"
	if crlf {
		nl = "\r\n"
	}
	var b strings.Builder
	for _, l := range kept {
		b.WriteString(l)
		b.WriteString(nl)
	}
	b.WriteString(tokenEnvKey + "=" + token + nl)
	return b.String(), true
}
