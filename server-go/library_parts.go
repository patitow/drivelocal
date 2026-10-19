package main

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// Partes de uma série pelas pastas (ex.: Fairy Tail "(2009)", "(2011) - OVAs",
// "(2014)", e a "(2018)" dividida em "01 ~ 25" e "26 ~ 51"). Só vale quando o
// número de temporada dos arquivos não separa nada.

var (
	reBracketTag = regexp.MustCompile(`\[[^\]]*\]|\{[^}]*\}`)
	// "- 01 ~ 25", "01-25", "(Seasons 1-8 + OVAs)", "Season 1-8"
	reEpRange    = regexp.MustCompile(`(?i)(?:^|[\s\-–])\d{1,4}\s*[~–-]\s*\d{1,4}(?:\s|$)`)
	reSeasonPack = regexp.MustCompile(`(?i)\(\s*seasons?\b[^)]*\)|\bseasons?\s+\d+\s*[-~]\s*\d+`)
	reParenYear  = regexp.MustCompile(`\(\s*((?:19|20)\d{2})(?:\s*[-–]\s*(?:19|20)\d{2})?\s*\)`)
	reYearSpan   = regexp.MustCompile(`(?:19|20)\d{2}\s*[-–]\s*(?:19|20)\d{2}`)
	reMultiSep   = regexp.MustCompile(`[\s\-–_.·:]+`)
)

// Pastas que só guardam material extra: vão para o fim da série e levam o nome
// da pasta de cima ("2009 · Extras").
var extraDirs = map[string]bool{
	"extras": true, "extra": true, "bonus": true, "nc": true, "ncop": true, "nced": true,
	"creditless": true, "menus": true, "menu": true, "trailers": true, "pv": true,
}

func isExtraDir(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return extraDirs[n] || strings.HasPrefix(n, "extra") || strings.HasPrefix(n, "nc ") || strings.HasPrefix(n, "bonus")
}

// partName limpa o nome de uma pasta: tira etiquetas entre colchetes, o título
// da série, intervalos de episódios e pacotes de temporadas.
func partName(dir, title string) string {
	s := reBracketTag.ReplaceAllString(dir, " ")
	s = reSeasonPack.ReplaceAllString(s, " ")
	if title != "" {
		s = regexp.MustCompile(`(?i)`+regexp.QuoteMeta(title)).ReplaceAllString(s, " ")
	}
	s = reEpRange.ReplaceAllString(s, " ")
	// pacote com vários anos ("2009-2014") não é uma parte
	if reYearSpan.MatchString(s) && !reParenYear.MatchString(strings.Replace(s, "-", "", 1)) {
		s = reYearSpan.ReplaceAllString(s, " ")
	}
	s = reParenYear.ReplaceAllStringFunc(s, func(m string) string {
		if reYearSpan.MatchString(m) {
			return " "
		}
		return " " + reParenYear.FindStringSubmatch(m)[1] + " "
	})
	s = strings.NewReplacer("(", " ", ")", " ").Replace(s)
	s = strings.Trim(reMultiSep.ReplaceAllString(s, " "), " ")
	return s
}

type seriesPart struct {
	label string
	extra bool
	year  string
	first int // posição do primeiro episódio (desempate)
}

// labelParts devolve a parte de cada item (por ID) e a ordem das partes, ou nil
// quando as pastas não dividem a série em duas ou mais partes.
func labelParts(list []ItemDTO, title string) (map[int64]string, []string) {
	if len(list) < 2 {
		return nil, nil
	}
	seasons := map[int64]bool{}
	for _, it := range list {
		if it.Season != nil && *it.Season > 0 {
			seasons[*it.Season] = true
		}
	}
	if len(seasons) > 1 {
		return nil, nil // as temporadas já dividem
	}
	// pasta comum a todos: a raiz da série
	base := path.Dir(list[0].Path)
	for _, it := range list[1:] {
		for d := path.Dir(it.Path); !pathHasPrefix(d, base) && base != "." && base != "/"; {
			base = path.Dir(base)
		}
	}
	parts := map[string]*seriesPart{}
	of := map[int64]string{}
	for i, it := range list {
		rel := strings.TrimPrefix(path.Dir(it.Path), base)
		rel = strings.Trim(rel, "/")
		var segs []string
		if rel != "" {
			segs = strings.Split(rel, "/")
		}
		extra := false
		var names []string
		for _, sg := range segs {
			if isExtraDir(sg) {
				extra = true
				names = append(names, strings.TrimSpace(sg))
				continue
			}
			if n := partName(sg, title); n != "" {
				names = append(names, n)
			}
		}
		// fica o nome mais específico da pasta principal + o da pasta extra
		label := "Episódios"
		var main string
		for _, n := range names {
			if !isExtraDir(n) {
				main = n
			}
		}
		switch {
		case extra && main != "":
			label = main + " · " + names[len(names)-1]
		case extra:
			label = names[len(names)-1]
		case main != "":
			label = main
		}
		p := parts[label]
		if p == nil {
			y := ""
			if m := reYear.FindStringSubmatch(label); m != nil {
				y = m[1]
			}
			p = &seriesPart{label: label, extra: extra, year: y, first: i}
			parts[label] = p
		}
		of[it.ID] = label
	}
	if len(parts) < 2 {
		return nil, nil
	}
	order := make([]*seriesPart, 0, len(parts))
	for _, p := range parts {
		order = append(order, p)
	}
	// conteúdo principal em ordem de ano (sem ano: na ordem das pastas); extras no fim
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if a.extra != b.extra {
			return !a.extra
		}
		if a.year != "" && b.year != "" && a.year != b.year {
			return a.year < b.year
		}
		return a.first < b.first
	})
	labels := make([]string, len(order))
	for i, p := range order {
		labels[i] = p.label
	}
	return of, labels
}

// orderByParts reordena a lista pelas partes, mantendo a ordem de episódios
// dentro de cada uma, e grava a parte em cada item.
func orderByParts(list []ItemDTO, title string) {
	of, labels := labelParts(list, title)
	if of == nil {
		return
	}
	rank := map[string]int{}
	for i, l := range labels {
		rank[l] = i
	}
	for i := range list {
		list[i].Part = of[list[i].ID]
	}
	sort.SliceStable(list, func(i, j int) bool { return rank[list[i].Part] < rank[list[j].Part] })
}
