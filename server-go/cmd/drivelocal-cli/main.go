// drivelocal-cli: cliente de linha de comando da biblioteca do drivelocal.
//
// Configuração por ambiente:
//
//	DRIVELOCAL_URL    (padrão http://localhost)
//	DRIVELOCAL_TOKEN  token de API (dlt_...) criado em Admin > Biblioteca > Tokens
//
// Uso: drivelocal-cli <comando> [args]   (drivelocal-cli help)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const usage = `drivelocal-cli — biblioteca do drivelocal

  search <texto> [--kind video|image] [--tag slug] [--maturity m] [--limit n]
  item <id>                         detalhes de um item
  lookup <caminho>                  item pelo caminho relativo
  tag <id> +tag -tag ...            adiciona/remove tags manuais
  fav <id> | unfav <id>             favoritos
  continue                          continuar assistindo
  topics [--kind series|creator] [texto]
  playlists                         lista playlists
  playlist <id>                     itens da playlist
  playlist-add <id> <itemId>...     adiciona itens
  stats                             (admin) números do catálogo
  scan                              (admin) inicia um scan
  rules                             (admin) lista regras
  rules-apply [ruleId] [--dry]      (admin) aplica regras (0 = todas)
  batch <arquivo.json> [--dry]      (admin) operações em lote {"ops":[...]}
  changes                           (admin) histórico de lotes
  undo <batchId>                    (admin) desfaz um lote
  policies                          (admin) políticas de pasta
  policy <prefixo> <geral|sugestivo|explicito|herda> [--hidden]
  ai-status | ai-run [n] | ai-pause | ai-resume | ai-clear
  ai-enqueue [prefixo] [--stage text|vision] [--limit n] [--requeue]
  review [--kind k] [--limit n]     (admin) sugestões pendentes
  accept <id>... | reject <id>...   (admin) revisa sugestões
  eval                              (admin) métricas contra o golden set
  export > catalogo.jsonl           (admin) exporta o catálogo curado
  raw <MÉTODO> <caminho-api> [json] chamada direta (ex.: raw GET /api/tags)
`

func main() {
	if len(os.Args) < 2 || os.Args[1] == "help" || os.Args[1] == "-h" || os.Args[1] == "--help" {
		fmt.Print(usage)
		return
	}
	c := newClient()
	cmd, args := os.Args[1], os.Args[2:]
	flags, pos := parseFlags(args)
	var err error
	switch cmd {
	case "search":
		q := url.Values{"q": {strings.Join(pos, " ")}}
		for k, f := range map[string]string{"kind": "kind", "tag": "tags", "maturity": "maturity", "limit": "limit", "sort": "sort", "prefix": "pathPrefix"} {
			if v := flags[k]; v != "" {
				q.Set(f, v)
			}
		}
		err = c.print("GET", "/api/catalog/items?"+q.Encode(), nil)
	case "item":
		err = c.print("GET", "/api/catalog/items/"+arg(pos, 0), nil)
	case "lookup":
		err = c.print("GET", "/api/catalog/lookup?path="+url.QueryEscape(strings.Join(pos, " ")), nil)
	case "tag":
		body := map[string][]string{"add": {}, "remove": {}}
		for _, t := range pos[min(1, len(pos)):] {
			if strings.HasPrefix(t, "-") {
				body["remove"] = append(body["remove"], t[1:])
			} else {
				body["add"] = append(body["add"], strings.TrimPrefix(t, "+"))
			}
		}
		err = c.print("POST", "/api/catalog/items/"+arg(pos, 0)+"/tags", body)
	case "fav":
		err = c.print("PUT", "/api/me/favorites/"+arg(pos, 0), nil)
	case "unfav":
		err = c.print("DELETE", "/api/me/favorites/"+arg(pos, 0), nil)
	case "continue":
		err = c.print("GET", "/api/me/continue", nil)
	case "topics":
		q := url.Values{"q": {strings.Join(pos, " ")}, "kind": {flags["kind"]}}
		err = c.print("GET", "/api/topics?"+q.Encode(), nil)
	case "playlists":
		err = c.print("GET", "/api/playlists", nil)
	case "playlist":
		err = c.print("GET", "/api/playlists/"+arg(pos, 0), nil)
	case "playlist-add":
		ids := []int64{}
		for _, s := range pos[min(1, len(pos)):] {
			n, _ := strconv.ParseInt(s, 10, 64)
			ids = append(ids, n)
		}
		err = c.print("POST", "/api/playlists/"+arg(pos, 0)+"/items", map[string]any{"ids": ids})
	case "stats":
		err = c.print("GET", "/api/admin/library/stats", nil)
	case "scan":
		err = c.print("POST", "/api/admin/library/scan", nil)
	case "rules":
		err = c.print("GET", "/api/admin/library/rules", nil)
	case "rules-apply":
		id, _ := strconv.ParseInt(arg(pos, 0), 10, 64)
		err = c.print("POST", "/api/admin/library/rules/apply", map[string]any{"ruleId": id, "dryRun": flags["dry"] != ""})
	case "batch":
		var data []byte
		data, err = os.ReadFile(arg(pos, 0))
		if err == nil {
			var body map[string]any
			if err = json.Unmarshal(data, &body); err == nil {
				if flags["dry"] != "" {
					body["dryRun"] = true
				}
				err = c.print("POST", "/api/admin/library/batch", body)
			}
		}
	case "changes":
		err = c.print("GET", "/api/admin/library/changes", nil)
	case "undo":
		err = c.print("POST", "/api/admin/library/changes/"+url.PathEscape(arg(pos, 0))+"/undo", nil)
	case "policies":
		err = c.print("GET", "/api/admin/library/policies", nil)
	case "policy":
		body := map[string]any{"prefix": arg(pos, 0), "hidden": flags["hidden"] != ""}
		if m := arg(pos, 1); m != "" && m != "herda" {
			body["maturity"] = m
		}
		err = c.print("PUT", "/api/admin/library/policies", body)
	case "ai-status":
		err = c.print("GET", "/api/admin/library/ai/status", nil)
	case "ai-run":
		n, _ := strconv.Atoi(arg(pos, 0))
		err = c.print("POST", "/api/admin/library/ai/run", map[string]any{"max": n})
	case "ai-pause", "ai-resume":
		err = c.print("POST", "/api/admin/library/ai/pause", map[string]any{"paused": cmd == "ai-pause"})
	case "ai-clear":
		err = c.print("POST", "/api/admin/library/ai/clear", nil)
	case "ai-enqueue":
		lim, _ := strconv.Atoi(flags["limit"])
		err = c.print("POST", "/api/admin/library/ai/enqueue", map[string]any{
			"pathPrefix": arg(pos, 0), "stage": flags["stage"], "limit": lim, "requeue": flags["requeue"] != "", "onlyNew": true})
	case "review":
		q := url.Values{"status": {"pending"}, "kind": {flags["kind"]}, "limit": {flags["limit"]}}
		err = c.print("GET", "/api/admin/library/review?"+q.Encode(), nil)
	case "accept", "reject":
		ids := []int64{}
		for _, s := range pos {
			n, _ := strconv.ParseInt(s, 10, 64)
			ids = append(ids, n)
		}
		err = c.print("POST", "/api/admin/library/review", map[string]any{"ids": ids, "accept": cmd == "accept"})
	case "eval":
		err = c.print("GET", "/api/admin/library/eval", nil)
	case "export":
		err = c.stream("GET", "/api/admin/library/export")
	case "raw":
		var body any
		if len(pos) > 2 {
			body = json.RawMessage(pos[2])
		}
		err = c.print(strings.ToUpper(arg(pos, 0)), arg(pos, 1), body)
	default:
		fmt.Fprintf(os.Stderr, "comando desconhecido: %s\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

func arg(a []string, i int) string {
	if i < len(a) {
		return a[i]
	}
	return ""
}

// parseFlags separa --chave valor / --chave (booleano) dos argumentos posicionais.
func parseFlags(args []string) (map[string]string, []string) {
	flags := map[string]string{}
	var pos []string
	bools := map[string]bool{"dry": true, "hidden": true, "requeue": true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			k := strings.TrimPrefix(a, "--")
			if kv := strings.SplitN(k, "=", 2); len(kv) == 2 {
				flags[kv[0]] = kv[1]
			} else if bools[k] || i+1 >= len(args) {
				flags[k] = "1"
			} else {
				flags[k] = args[i+1]
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	return flags, pos
}

type client struct {
	base, token string
	http        *http.Client
}

func newClient() *client {
	base := strings.TrimRight(os.Getenv("DRIVELOCAL_URL"), "/")
	if base == "" {
		base = "http://localhost"
	}
	tok := os.Getenv("DRIVELOCAL_TOKEN")
	if tok == "" {
		fmt.Fprintln(os.Stderr, "defina DRIVELOCAL_TOKEN (token de API criado em Admin > Biblioteca > Tokens)")
		os.Exit(2)
	}
	return &client{base: base, token: tok, http: &http.Client{Timeout: 10 * time.Minute}}
}

func (c *client) do(method, path string, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

func (c *client) print(method, path string, body any) error {
	resp, err := c.do(method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var pretty bytes.Buffer
	if json.Indent(&pretty, data, "", "  ") == nil {
		data = pretty.Bytes()
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	os.Stdout.Write(data)
	fmt.Println()
	return nil
}

func (c *client) stream(method, path string) error {
	resp, err := c.do(method, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, b)
	}
	_, err = io.Copy(os.Stdout, resp.Body)
	return err
}
