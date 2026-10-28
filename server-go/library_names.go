package main

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// ParsedName é o que dá para extrair de um nome de arquivo sem IA.
type ParsedName struct {
	Title      string
	Season     int     // 0 = desconhecida
	Episode    float64 // 0 = desconhecido
	Group      string  // release group ([Grupo] no início)
	Resolution string
}

var (
	reBracketLead = regexp.MustCompile(`^\s*[\[【]([^\]】]+)[\]】]\s*`)
	reBrackets    = regexp.MustCompile(`[\[【(（][^\]】)）]*[\]】)）]`)
	reResolution  = regexp.MustCompile(`(?i)\b(2160|1080|720|480|360)p\b`)
	reSxxExx      = regexp.MustCompile(`(?i)\bS(\d{1,2})\s?E(\d{1,4}(?:\.\d)?)\b`)
	reNxEp        = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{2,3})\b`) // "1x05"
	reDashEp      = regexp.MustCompile(`\s-\s(\d{1,4}(?:\.\d)?)(?:v\d)?(?:\s|$)`)
	// "Título 07 - Nome do episódio": o número antes do traço é o episódio
	reNumDash     = regexp.MustCompile(`\s(\d{1,4}(?:\.\d)?)(?:v\d)?\s-\s\S`)
	reEpWord      = regexp.MustCompile(`(?i)\b(?:ep|episode|episódio|epis[oó]dio|e)\.?\s?(\d{1,4})\b`)
	reLeadNum     = regexp.MustCompile(`^(\d{1,3})(?:\.|\s-\s|\s)\s*`)
	reTrailingNum = regexp.MustCompile(`\s(\d{1,3})(?:v\d)?$`)
	reSeasonWord  = regexp.MustCompile(`(?i)\b(?:season|temporada|s)\s?(\d{1,2})\b`)
	reNthSeason   = regexp.MustCompile(`(?i)\b(\d{1,2})(?:st|nd|rd|th)\s+season\b`)
	reNoise       = regexp.MustCompile(`(?i)\b(x26[45]|h\.?26[45]|hevc|avc|aac\d?(\.\d)?|flac|opus|ddp?\d?\.?\d?|10bit|8bit|web-?dl|webrip|bluray|bdrip|bd|dual[- ]audio|multi[- ]?subs?|batch)\b`)
	reSpaces      = regexp.MustCompile(`\s+`)
	reHashLike    = regexp.MustCompile(`^[A-Za-z0-9_-]{8,16}(_\d{3,4}p)?$`)
)

func stripExt(name string) string {
	ext := path.Ext(name)
	if len(ext) > 0 && len(ext) <= 5 {
		return strings.TrimSuffix(name, ext)
	}
	return name
}

// cleanTitle troca separadores por espaço e remove ruído técnico.
func cleanTitle(s string) string {
	s = strings.NewReplacer("_", " ", ".", " ").Replace(s)
	s = reBrackets.ReplaceAllString(s, " ")
	s = reResolution.ReplaceAllString(s, " ")
	s = reNoise.ReplaceAllString(s, " ")
	s = reSpaces.ReplaceAllString(s, " ")
	s = strings.Trim(s, " -–_·")
	return s
}

// ParseMediaName interpreta nomes comuns de episódios de anime/série.
func ParseMediaName(name string) ParsedName {
	var p ParsedName
	base := stripExt(name)
	if m := reBracketLead.FindStringSubmatch(base); m != nil {
		p.Group = strings.TrimSpace(m[1])
		base = base[len(m[0]):]
	}
	if m := reResolution.FindStringSubmatch(base); m != nil {
		p.Resolution = m[1] + "p"
	}
	work := strings.NewReplacer("_", " ").Replace(base)
	work = reBrackets.ReplaceAllString(work, " ")
	work = reSpaces.ReplaceAllString(work, " ")
	work = strings.TrimSpace(work)

	if m := reSxxExx.FindStringSubmatchIndex(work); m != nil {
		p.Season, _ = strconv.Atoi(work[m[2]:m[3]])
		p.Episode, _ = strconv.ParseFloat(work[m[4]:m[5]], 64)
		p.Title = cleanTitle(work[:m[0]])
	} else if m := reNxEp.FindStringSubmatchIndex(work); m != nil {
		p.Season, _ = strconv.Atoi(work[m[2]:m[3]])
		p.Episode, _ = strconv.ParseFloat(work[m[4]:m[5]], 64)
		if p.Title = cleanTitle(work[:m[0]]); p.Title == "" {
			p.Title = cleanTitle(work[m[1]:]) // "1x05 - Nome do episódio"
		}
	} else if m := reDashEp.FindStringSubmatchIndex(work); m != nil {
		p.Episode, _ = strconv.ParseFloat(work[m[2]:m[3]], 64)
		p.Title = cleanTitle(work[:m[0]])
	} else if m := reNumDash.FindStringSubmatchIndex(work); m != nil && m[0] > 0 && !looksLikeYear(work[m[2]:m[3]]) {
		p.Episode, _ = strconv.ParseFloat(work[m[2]:m[3]], 64)
		p.Title = cleanTitle(work[:m[0]])
	} else if m := reLeadNum.FindStringSubmatchIndex(work); m != nil && len(work) > m[1] {
		p.Episode, _ = strconv.ParseFloat(work[m[2]:m[3]], 64)
		p.Title = cleanTitle(work[m[1]:])
	} else if m := reEpWord.FindStringSubmatchIndex(work); m != nil {
		p.Episode, _ = strconv.ParseFloat(work[m[2]:m[3]], 64)
		p.Title = cleanTitle(work[:m[0]])
	} else if m := reTrailingNum.FindStringSubmatchIndex(work); m != nil && !looksLikeYear(work[m[2]:m[3]]) {
		p.Episode, _ = strconv.ParseFloat(work[m[2]:m[3]], 64)
		p.Title = cleanTitle(work[:m[0]])
	} else {
		p.Title = cleanTitle(work)
	}
	if p.Title == "" {
		p.Title = cleanTitle(base)
	}
	if p.Season == 0 {
		if m := reNthSeason.FindStringSubmatch(base); m != nil {
			p.Season, _ = strconv.Atoi(m[1])
		} else if m := reSeasonWord.FindStringSubmatch(p.Title); m != nil {
			p.Season, _ = strconv.Atoi(m[1])
		}
	}
	return p
}

func looksLikeYear(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && len(s) == 4 && n >= 1900 && n <= 2100
}

// isOpaqueName detecta nomes aleatórios (IDs de upload) que não servem de título.
func isOpaqueName(name string) bool {
	return reHashLike.MatchString(stripExt(name))
}

// topicRoot diz como derivar um tópico automático a partir da pasta.
type topicRoot struct {
	Prefix string
	Kind   string // series | creator
	Leaf   bool   // true: usa a pasta imediata do arquivo; false: o 1º segmento após o prefixo
}

// defaultTopicRoots: estrutura das pastas (sem nomes de pessoas). A ordem
// importa: prefixos mais específicos primeiro.
var defaultTopicRoots = []topicRoot{
	{Prefix: "Animes/Restrito", Kind: "series"},
	{Prefix: "Animes", Kind: "series"},
	{Prefix: "Torrents/.Animes/Anime", Kind: "series"},
	{Prefix: "Torrents/.Animes", Kind: "series"},
	{Prefix: "Torrents/.Mangás", Kind: "series"}, // D12 (decisão do agente)
	{Prefix: "Series", Kind: "series"},
	{Prefix: "Séries", Kind: "series"},
	{Prefix: "Criadores", Kind: "creator"},
	{Prefix: "Gravacoes/Privado", Kind: "creator", Leaf: true},
}

type derivedTopic struct {
	Kind    string
	Title   string
	Folder  string // caminho da pasta que originou o tópico
	SortKey string
}

// deriveTopic devolve o tópico automático de um caminho relativo, se houver.
func deriveTopic(rel string) (derivedTopic, bool) {
	dir := path.Dir(rel)
	if dir == "." {
		return derivedTopic{}, false
	}
	for _, tr := range defaultTopicRoots {
		if !pathHasPrefix(dir, tr.Prefix) || dir == tr.Prefix {
			continue
		}
		rest := strings.TrimPrefix(dir, tr.Prefix+"/")
		var folder, title string
		if tr.Leaf {
			folder = dir
			title = path.Base(dir)
		} else {
			first := strings.SplitN(rest, "/", 2)[0]
			folder = tr.Prefix + "/" + first
			title = first
		}
		title = cleanTopicTitle(title)
		if title == "" {
			return derivedTopic{}, false
		}
		inner := strings.TrimPrefix(rel, folder+"/")
		return derivedTopic{Kind: tr.Kind, Title: title, Folder: folder, SortKey: naturalSortKey(inner)}, true
	}
	return derivedTopic{}, false
}

func cleanTopicTitle(s string) string {
	t := cleanTitle(strings.ReplaceAll(s, ".", "\x00"))
	t = strings.ReplaceAll(t, "\x00", ".")
	if t == "" {
		t = strings.TrimSpace(s)
	}
	return t
}

// naturalSortKey faz "ep 2" < "ep 10" ao comparar como texto.
func naturalSortKey(s string) string {
	var b strings.Builder
	rs := []rune(strings.ToLower(s))
	for i := 0; i < len(rs); {
		if unicode.IsDigit(rs[i]) {
			j := i
			for j < len(rs) && unicode.IsDigit(rs[j]) {
				j++
			}
			num := strings.TrimLeft(string(rs[i:j]), "0")
			if num == "" {
				num = "0"
			}
			fmt.Fprintf(&b, "%08d", len(num))
			b.WriteString(num)
			i = j
			continue
		}
		b.WriteRune(rs[i])
		i++
	}
	return b.String()
}

// slugify: minúsculas, sem acento, separado por hífen.
func slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(foldDiacritics(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

var diacriticFold = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a", "Á", "A", "À", "A", "Â", "A", "Ã", "A",
	"é", "e", "ê", "e", "è", "e", "É", "E", "Ê", "E",
	"í", "i", "î", "i", "Í", "I",
	"ó", "o", "ô", "o", "õ", "o", "ö", "o", "Ó", "O", "Ô", "O", "Õ", "O",
	"ú", "u", "ü", "u", "Ú", "U", "Ü", "U",
	"ç", "c", "Ç", "C", "ñ", "n", "Ñ", "N",
)

func foldDiacritics(s string) string { return diacriticFold.Replace(s) }

// pathTokens transforma o caminho em texto pesquisável.
func pathTokens(rel string) string {
	s := strings.NewReplacer("/", " ", "_", " ", ".", " ", "-", " ", "[", " ", "]", " ", "(", " ", ")", " ").Replace(rel)
	return reSpaces.ReplaceAllString(s, " ")
}
