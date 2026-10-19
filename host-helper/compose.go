package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// ContainerName é o container do servidor (container_name no compose).
	ContainerName = "drivelocal-media-server"
	// ServiceName é o serviço do servidor no compose.
	ServiceName = "drivelocal"
	// DefaultHealthURL é o health check do servidor, publicado na porta do host.
	DefaultHealthURL = "http://127.0.0.1:8080/api/health"

	dockerBinDir     = `C:\Program Files\Docker\Docker\resources\bin`
	stderrLimit      = 4096
	configTimeout    = 60 * time.Second
	upTimeout        = 300 * time.Second
	inspectTimeout   = 15 * time.Second
	healthReqTimeout = 5 * time.Second
)

// ComposeDocker implementa Docker com o docker/compose reais.
type ComposeDocker struct {
	ProjectDir    string        // cwd do compose (raiz do projeto, onde fica docker-compose.yml)
	OverrideFile  string        // caminho do docker-compose.override.yml
	HealthURL     string        // padrão DefaultHealthURL
	HealthTimeout time.Duration // padrão 120s
	PollInterval  time.Duration // padrão 1.5s
	HTTP          *http.Client
}

// NewComposeDocker cria a implementação real com os padrões do helper.
func NewComposeDocker(projectDir, overrideFile string) *ComposeDocker {
	return &ComposeDocker{
		ProjectDir:    projectDir,
		OverrideFile:  overrideFile,
		HealthURL:     DefaultHealthURL,
		HealthTimeout: 120 * time.Second,
		PollInterval:  1500 * time.Millisecond,
		HTTP:          &http.Client{Timeout: healthReqTimeout},
	}
}

// BootID consulta o bootId atual do servidor.
func (c *ComposeDocker) BootID() (string, error) {
	return c.fetchBootID()
}

// ConfigCheck valida a configuração final do compose.
func (c *ComposeDocker) ConfigCheck() error {
	_, err := c.compose(configTimeout, "config", "-q")
	return err
}

// Up recria só o serviço do servidor, sem build e sem dependências.
func (c *ComposeDocker) Up() error {
	_, err := c.compose(upTimeout, "up", "-d", "--no-build", "--no-deps", ServiceName)
	return err
}

// InspectMounts lê os mounts do container do servidor.
func (c *ComposeDocker) InspectMounts() ([]ContainerMount, error) {
	out, err := c.docker(inspectTimeout, "inspect", "--format", "{{json .Mounts}}", ContainerName)
	if err != nil {
		return nil, err
	}
	var mounts []ContainerMount
	if err := json.Unmarshal(bytes.TrimSpace(out), &mounts); err != nil {
		return nil, fmt.Errorf("docker inspect: JSON inválido: %w", err)
	}
	return mounts, nil
}

// WaitHealthy faz polling em HealthURL até responder 200 com bootId novo.
func (c *ComposeDocker) WaitHealthy(prevBootID string) (string, error) {
	timeout := c.HealthTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	interval := c.PollInterval
	if interval <= 0 {
		interval = 1500 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		id, err := c.fetchBootID()
		if err == nil && id != "" && id != prevBootID {
			return id, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = errors.New("bootId não mudou")
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("servidor não ficou saudável em %s: %w", timeout, lastErr)
		}
		time.Sleep(interval)
	}
}

func (c *ComposeDocker) fetchBootID() (string, error) {
	url := c.HealthURL
	if url == "" {
		url = DefaultHealthURL
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: healthReqTimeout}
	}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("health: status %d", resp.StatusCode)
	}
	var body struct {
		BootID string `json:"bootId"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil {
		return "", fmt.Errorf("health: JSON inválido: %w", err)
	}
	return body.BootID, nil
}

// compose roda `docker compose -f <base> -f <override> args...` no diretório do projeto.
func (c *ComposeDocker) compose(timeout time.Duration, args ...string) ([]byte, error) {
	full := []string{"compose",
		"-f", filepath.Join(c.ProjectDir, "docker-compose.yml"),
		"-f", c.OverrideFile,
	}
	return c.docker(timeout, append(full, args...)...)
}

// docker executa o binário do Docker e devolve stdout. Stderr é cortado em 4KB
// e usado na mensagem de erro.
func (c *ComposeDocker) docker(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, dockerExe(), args...)
	cmd.Dir = c.ProjectDir
	cmd.Env = c.env()
	var stdout bytes.Buffer
	stderr := &cappedBuffer{max: stderrLimit}
	cmd.Stdout = &stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		label := strings.Join(args[:min(len(args), 2)], " ")
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("docker %s: tempo esgotado", label)
		}
		return nil, fmt.Errorf("docker %s: %w: %s", label, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// env devolve o ambiente do processo com o PATH do Docker Desktop e HOST_LAN_IP.
func (c *ComposeDocker) env() []string {
	var out []string
	hasPath, hasIP := false, false
	for _, kv := range os.Environ() {
		key, val, _ := strings.Cut(kv, "=")
		switch {
		case strings.EqualFold(key, "PATH"):
			hasPath = true
			out = append(out, key+"="+dockerBinDir+";"+val)
		case key == "HOST_LAN_IP":
			hasIP = true
			out = append(out, kv)
		default:
			out = append(out, kv)
		}
	}
	if !hasPath {
		out = append(out, "PATH="+dockerBinDir)
	}
	if !hasIP {
		if ip := detectLANIP(); ip != "" {
			out = append(out, "HOST_LAN_IP="+ip)
		}
	}
	return out
}

// dockerExe usa o docker.exe do Docker Desktop quando existe.
func dockerExe() string {
	p := filepath.Join(dockerBinDir, "docker.exe")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return "docker"
}

// detectLANIP segue a regra de scripts/docker-autostart.bat: menor 192.168.*
// e, se não houver, menor 10.*.
func detectLANIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	var lan, ten string
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipn.IP.To4()
		if ip4 == nil {
			continue
		}
		s := ip4.String()
		if strings.HasPrefix(s, "192.168.") && (lan == "" || s < lan) {
			lan = s
		}
		if ip4[0] == 10 && (ten == "" || s < ten) {
			ten = s
		}
	}
	if lan != "" {
		return lan
	}
	return ten
}

// cappedBuffer guarda até max bytes e descarta o resto sem erro.
type cappedBuffer struct {
	buf bytes.Buffer
	max int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil
}

func (c *cappedBuffer) String() string { return c.buf.String() }
