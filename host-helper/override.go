package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	dataPrefix   = "/data/"
	backupKeep   = 10
	backupSuffix = "-override.yml"
	skeletonYAML = "services:\n  drivelocal:\n    volumes: []\n"
)

var (
	// ErrNotFound indica que não existe biblioteca com o nome pedido.
	ErrNotFound = errors.New("mount not found")
	// ErrExists indica que já existe biblioteca com o mesmo destino /data/<nome>.
	ErrExists = errors.New("mount target already exists")

	// Sintaxe curta: [X:]<hostPath>:<target>[:<modo>] com espaços opcionais ao
	// redor das partes. A letra de drive (X:) é opcional e faz parte do hostPath,
	// assim "E:/data/cache:/app/cache" tem target /app/cache.
	shortRe = regexp.MustCompile(`^((?:[A-Za-z]:)?[^:]*?)\s*:\s*(/[^:]*?)\s*(?::\s*([A-Za-z,]+)\s*)?$`)
)

// Mount é uma biblioteca: pasta do host montada em /data/<name> do container.
type Mount struct {
	Name     string `json:"name"`
	HostPath string `json:"hostPath"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"readOnly"`
	// Legacy indica entrada escrita na sintaxe curta (anterior ao app).
	Legacy bool `json:"legacy"`
}

// Override representa o docker-compose.override.yml, preservando comentários
// e tudo que não é biblioteca (/data/*).
type Override struct {
	path string
	doc  *yaml.Node

	orig      []byte // conteúdo como estava no disco (ou esqueleto, se ausente)
	origCanon []byte // encoding do árvore original, para detectar "sem mudança"

	origHasServices bool
	origHasDrive    bool
	origHasVolumes  bool
}

// LoadOverride lê o override. Arquivo ausente vira o esqueleto
// services.drivelocal.volumes. YAML inválido ou entrada /data/* não reconhecida
// retorna erro (e nada é escrito).
func LoadOverride(path string) (*Override, error) {
	o := &Override{path: path}

	data, err := os.ReadFile(path)
	missing := errors.Is(err, os.ErrNotExist)
	if missing {
		data = []byte(skeletonYAML)
	} else if err != nil {
		return nil, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("override YAML inválido: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		// arquivo vazio (ou só comentários): começa do esqueleto
		if err := yaml.Unmarshal([]byte(skeletonYAML), &doc); err != nil {
			return nil, err
		}
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("override YAML inválido: raiz não é um mapa")
	}
	o.doc = &doc

	root := doc.Content[0]
	_, o.origHasServices = findKey(root, "services")
	if s, ok := findKey(root, "services"); ok && s.Kind == yaml.MappingNode {
		_, o.origHasDrive = findKey(s, "drivelocal")
		if d, ok := findKey(s, "drivelocal"); ok && d.Kind == yaml.MappingNode {
			_, o.origHasVolumes = findKey(d, "volumes")
		}
	}

	// valida: volumes precisa ser lista e toda entrada /data/* precisa ser reconhecida
	drive, err := o.drive(false)
	if err != nil {
		return nil, err
	}
	if drive != nil {
		vols, err := child(drive, "services.drivelocal", "volumes", false, yaml.SequenceNode)
		if err != nil {
			return nil, err
		}
		if vols != nil {
			for _, e := range vols.Content {
				if _, _, err := parseEntry(e); err != nil {
					return nil, err
				}
			}
		}
	}

	canon, err := encodeDoc(&doc)
	if err != nil {
		return nil, err
	}
	o.origCanon = canon
	if missing {
		o.orig = canon
	} else {
		o.orig = data
	}
	return o, nil
}

// Mounts devolve as bibliotecas na ordem do arquivo.
func (o *Override) Mounts() []Mount {
	out := []Mount{}
	vols := o.volumes()
	if vols == nil {
		return out
	}
	for _, e := range vols.Content {
		m, ok, err := parseEntry(e)
		if err == nil && ok {
			out = append(out, m)
		}
	}
	return out
}

// Add acrescenta uma biblioteca em sintaxe longa, sempre read_only: true.
func (o *Override) Add(m Mount) error {
	if m.Name == "" || strings.ContainsAny(m.Name, "/") || m.Name == "." || m.Name == ".." {
		return fmt.Errorf("nome de biblioteca inválido: %q", m.Name)
	}
	if strings.TrimSpace(m.HostPath) == "" {
		return errors.New("hostPath vazio")
	}
	target := dataPrefix + m.Name
	for _, e := range o.Mounts() {
		if strings.EqualFold(e.Target, target) {
			return ErrExists
		}
	}

	drive, err := o.drive(true)
	if err != nil {
		return err
	}
	vols, err := child(drive, "services.drivelocal", "volumes", true, yaml.SequenceNode)
	if err != nil {
		return err
	}
	vols.Style &^= yaml.FlowStyle

	entry := mappingNode(
		"type", scalarNode("bind"),
		"source", scalarNode(escapeCompose(m.HostPath)),
		"target", scalarNode(escapeCompose(target)),
		"read_only", boolNode(true),
		"bind", mappingNode("create_host_path", boolNode(false)),
	)
	vols.Content = append(vols.Content, entry)
	return nil
}

// Remove apaga somente a entrada /data/<name>.
func (o *Override) Remove(name string) error {
	vols := o.volumes()
	if vols == nil {
		return ErrNotFound
	}
	target := dataPrefix + name
	idx := -1
	for i, e := range vols.Content {
		m, ok, err := parseEntry(e)
		if err == nil && ok && strings.EqualFold(m.Target, target) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	vols.Content = append(vols.Content[:idx], vols.Content[idx+1:]...)
	o.pruneEmpty()
	return nil
}

// Bytes devolve o conteúdo atual. Se a árvore é equivalente à original, devolve
// os bytes originais, byte a byte.
func (o *Override) Bytes() []byte {
	cur, err := encodeDoc(o.doc)
	if err != nil {
		return nil
	}
	if bytes.Equal(cur, o.origCanon) {
		return append([]byte(nil), o.orig...)
	}
	return cur
}

// Revision é o sha256 (hex) do conteúdo atual.
func (o *Override) Revision() string {
	sum := sha256.Sum256(o.Bytes())
	return hex.EncodeToString(sum[:])
}

// WriteAtomic faz backup do arquivo atual em backupDir (mantendo os últimos 10),
// depois escreve data em um tmp no mesmo diretório e troca com os.Rename.
// Devolve o caminho do backup ("" se o arquivo não existia ou backupDir é vazio).
func WriteAtomic(path string, data []byte, backupDir string) (string, error) {
	var backup string
	cur, err := os.ReadFile(path)
	switch {
	case err == nil:
		if backupDir != "" {
			if err := os.MkdirAll(backupDir, 0o755); err != nil {
				return "", err
			}
			backup = filepath.Join(backupDir, backupName())
			if err := os.WriteFile(backup, cur, 0o644); err != nil {
				return "", err
			}
			if err := pruneBackups(backupDir, backupKeep); err != nil {
				return backup, err
			}
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return "", err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".override-*.tmp")
	if err != nil {
		return backup, err
	}
	tmpName := tmp.Name()
	fail := func(err error) (string, error) {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return backup, err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return backup, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return backup, err
	}
	return backup, nil
}

// --- internos ---------------------------------------------------------------

// drive devolve services.drivelocal; com create=true cria o que faltar.
func (o *Override) drive(create bool) (*yaml.Node, error) {
	root := o.doc.Content[0]
	svc, err := child(root, "", "services", create, yaml.MappingNode)
	if err != nil || svc == nil {
		return nil, err
	}
	return child(svc, "services", "drivelocal", create, yaml.MappingNode)
}

// volumes devolve a lista services.drivelocal.volumes sem criar nada (nil se não existe).
func (o *Override) volumes() *yaml.Node {
	drive, err := o.drive(false)
	if err != nil || drive == nil {
		return nil
	}
	vols, err := child(drive, "services.drivelocal", "volumes", false, yaml.SequenceNode)
	if err != nil {
		return nil
	}
	return vols
}

// pruneEmpty remove chaves vazias que não existiam no original, para que
// add+remove reproduza o arquivo original.
func (o *Override) pruneEmpty() {
	root := o.doc.Content[0]
	svc, ok := findKey(root, "services")
	if !ok || svc.Kind != yaml.MappingNode {
		return
	}
	drive, ok := findKey(svc, "drivelocal")
	if ok && drive.Kind == yaml.MappingNode {
		if vols, ok := findKey(drive, "volumes"); ok && vols.Kind == yaml.SequenceNode && len(vols.Content) == 0 && !o.origHasVolumes {
			removeKey(drive, "volumes")
		}
		if len(drive.Content) == 0 && !o.origHasDrive {
			removeKey(svc, "drivelocal")
		}
	}
	if len(svc.Content) == 0 && !o.origHasServices {
		removeKey(root, "services")
	}
}

// parseEntry interpreta uma entrada de volumes. ok=false se o destino não é /data/*.
func parseEntry(n *yaml.Node) (Mount, bool, error) {
	switch n.Kind {
	case yaml.ScalarNode:
		mm := shortRe.FindStringSubmatch(strings.TrimSpace(n.Value))
		if mm == nil {
			return Mount{}, false, nil
		}
		hostPath, target, mode := unescapeCompose(strings.TrimSpace(mm[1])), unescapeCompose(mm[2]), mm[3]
		if !strings.HasPrefix(target, dataPrefix) {
			return Mount{}, false, nil
		}
		name := strings.TrimPrefix(target, dataPrefix)
		if hostPath == "" || name == "" || strings.Contains(name, "/") {
			return Mount{}, false, fmt.Errorf("entrada de biblioteca não reconhecida: %q", n.Value)
		}
		if mode != "" && mode != "ro" && mode != "rw" {
			return Mount{}, false, fmt.Errorf("modo de biblioteca não suportado em %q", n.Value)
		}
		return Mount{
			Name:     name,
			HostPath: hostPath,
			Target:   target,
			ReadOnly: mode == "ro",
			Legacy:   true,
		}, true, nil

	case yaml.MappingNode:
		target := unescapeCompose(mapValue(n, "target"))
		if !strings.HasPrefix(target, dataPrefix) {
			return Mount{}, false, nil
		}
		name := strings.TrimPrefix(target, dataPrefix)
		if name == "" || strings.Contains(name, "/") {
			return Mount{}, false, fmt.Errorf("destino de biblioteca não reconhecido: %q", target)
		}
		if typ := mapValue(n, "type"); typ != "bind" {
			return Mount{}, false, fmt.Errorf("biblioteca %q deve ser type: bind", name)
		}
		return Mount{
			Name:     name,
			HostPath: unescapeCompose(mapValue(n, "source")),
			Target:   target,
			ReadOnly: mapValue(n, "read_only") == "true",
			Legacy:   false,
		}, true, nil
	}
	return Mount{}, false, fmt.Errorf("entrada de volume inválida (linha %d)", n.Line)
}

// child devolve parent[key]. Com create=true cria a chave se faltar, ou troca
// um valor nulo pelo tipo pedido. Erro se o valor existe com outro tipo.
func child(parent *yaml.Node, path, key string, create bool, kind yaml.Kind) (*yaml.Node, error) {
	where := key
	if path != "" {
		where = path + "." + key
	}
	if parent.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s deve ser um mapa", path)
	}
	if v, ok := findKey(parent, key); ok {
		if v.Kind == yaml.ScalarNode && v.Tag == "!!null" && create {
			v.Kind = kind
			v.Tag = tagFor(kind)
			v.Value = ""
		}
		if v.Kind != kind {
			return nil, fmt.Errorf("%s tem tipo inesperado", where)
		}
		return v, nil
	}
	if !create {
		return nil, nil
	}
	v := &yaml.Node{Kind: kind, Tag: tagFor(kind)}
	parent.Content = append(parent.Content, scalarNode(key), v)
	return v, nil
}

func findKey(m *yaml.Node, key string) (*yaml.Node, bool) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1], true
		}
	}
	return nil, false
}

func removeKey(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

func mapValue(m *yaml.Node, key string) string {
	if v, ok := findKey(m, key); ok && v.Kind == yaml.ScalarNode {
		return strings.TrimSpace(v.Value)
	}
	return ""
}

func tagFor(kind yaml.Kind) string {
	if kind == yaml.SequenceNode {
		return "!!seq"
	}
	return "!!map"
}

func scalarNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func boolNode(b bool) *yaml.Node {
	v := "false"
	if b {
		v = "true"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: v}
}

// mappingNode monta um mapa a partir de pares chave, valor.
func mappingNode(pairs ...interface{}) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for i := 0; i+1 < len(pairs); i += 2 {
		n.Content = append(n.Content, scalarNode(pairs[i].(string)), pairs[i+1].(*yaml.Node))
	}
	return n
}

func encodeDoc(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// escapeCompose protege o valor contra a interpolação do docker compose, que
// trata "$" como início de variável (o escape é "$$"). Usado ao gravar.
func escapeCompose(s string) string {
	return strings.ReplaceAll(s, "$", "$$")
}

// unescapeCompose desfaz escapeCompose ao ler. Entradas antigas gravadas sem
// escape não têm "$$" e continuam iguais.
func unescapeCompose(s string) string {
	return strings.ReplaceAll(s, "$$", "$")
}

func backupName() string {
	var r [4]byte
	_, _ = rand.Read(r[:])
	return time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(r[:]) + backupSuffix
}

func pruneBackups(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), backupSuffix) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keep {
		if err := os.Remove(filepath.Join(dir, names[0])); err != nil {
			return err
		}
		names = names[1:]
	}
	return nil
}
