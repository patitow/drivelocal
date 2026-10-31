// Títulos de filme a partir do nome do arquivo e agrupamento em franquias.
import { library, type LibraryItem } from '@/lib/library'
import { ANIME_FILM_MIN_S, ANIME_ROOT, isWatched } from '@/lib/mediaRules'

// Filmes de anime: soltos e longos (40 min+) da pasta Animes; episódios de série ficam de fora.
// Aparecem nas duas estantes.
export const animeFilms = (flags: { unwatched?: boolean; favorites?: boolean } = {}) =>
  library.items({ root: ANIME_ROOT, kind: 'video', minDurationS: ANIME_FILM_MIN_S, ...flags, limit: 200, sort: 'title' })
    .then((p) => p.items.filter((it) => !it.topics?.some((t) => t.kind === 'series')))

// Marcas de release: daqui em diante o nome é só ficha técnica.
const JUNK = /(^|[\s.\-_])(2160p|1080p|720p|480p|4k|uhd|web[-. ]?dl|webrip|web|blu-?ray|bdrip|brrip|hdrip|dvdrip|remux|dual(?:[\s.]audio)?|dublado|legendado|x26[45]|h\.?\s?26[45]|hevc|aac|ddp?\s?5\.1|atmos|10bit|hdr)(?=$|[\s.\-_])/i
const SEP = /^[\s\-–_.:]+|[\s\-–_.:]+$/g

export interface FilmInfo { title: string; year?: number; part?: number }

export function filmInfo(it: Pick<LibraryItem, 'name' | 'title'>): FilmInfo {
  let s = it.name.replace(/\.[a-z0-9]{2,4}$/i, '')
  // ano entre parênteses vale; o resto entre colchetes/parênteses é etiqueta
  const paren = s.match(/\((19[3-9]\d|20[0-4]\d)\)/)
  s = s.replace(/\[[^\]]*\]|\([^)]*\)/g, ' ')
  if (!/\s/.test(s.trim())) s = s.replace(/[._]/g, ' ')
  s = s.replace(/\s-\s*by\s+.*$/i, '')
  const junk = s.match(JUNK)
  if (junk?.index != null) s = s.slice(0, junk.index)
  s = s.replace(/\s+/g, ' ').replace(SEP, '')
  let year = paren ? Number(paren[1]) : undefined
  const tail = s.match(/\s(19[3-9]\d|20[0-4]\d)$/)
  if (tail && !paren && s.length > 5) {
    year = Number(tail[1])
    s = s.slice(0, tail.index).replace(SEP, '')
  }
  // "Nome - 1.0 - Subtítulo" vira "Nome 1: Subtítulo"
  let part: number | undefined
  s = s.replace(/\s-\s(\d{1,2})(?:\.0)?\s-\s/, (_, n: string) => { part = Number(n); return ` ${n}: ` })
  return { title: s || it.title || it.name, year, part }
}

const STOP = new Set(['the', 'a', 'an', 'o', 'os', 'as', 'el', 'la', 'le', 'les', 'de', 'do', 'da'])
const words = (t: string) => t.split(/\s+/).filter(Boolean)
const keyOf = (t: string) => {
  const w = words(t.toLowerCase().replace(/[^\p{L}\p{N}\s]/gu, ' '))
  const n = STOP.has(w[0]) ? 3 : 2
  return w.length > n ? w.slice(0, n).join(' ') : ''
}

// id: a cadastrada no Gerenciar; sem id, a achada pelo começo do nome
export interface Franchise { id?: number; name: string; items: LibraryItem[] }

// Franquia: dois ou mais filmes que começam com as mesmas palavras
// ("Carros Velozes ..."). O nome é o maior começo comum entre eles.
export function franchises(items: LibraryItem[]): Franchise[] {
  const byKey = new Map<string, LibraryItem[]>()
  for (const it of items) {
    const k = keyOf(filmInfo(it).title)
    if (k) byKey.set(k, [...(byKey.get(k) ?? []), it])
  }
  const cmp = new Intl.Collator('pt-BR', { numeric: true, sensitivity: 'base' })
  const out: Franchise[] = []
  for (const group of byKey.values()) {
    if (group.length < 2) continue
    const infos = group.map(filmInfo)
    let common = words(infos[0].title)
    for (const f of infos.slice(1)) {
      const w = words(f.title)
      let i = 0
      while (i < common.length && i < w.length && common[i].toLowerCase() === w[i].toLowerCase()) i++
      common = common.slice(0, i)
    }
    const sorted = group.map((it, i) => ({ it, f: infos[i] })).sort((a, b) =>
      (a.f.year ?? 0) - (b.f.year ?? 0) || cmp.compare(a.f.title, b.f.title))
    out.push({ name: common.join(' ').replace(SEP, ''), items: sorted.map((x) => x.it) })
  }
  return out.sort((a, b) => cmp.compare(a.name, b.name))
}

// Título dentro da franquia, sem repetir o nome dela (nem o número, que o card já mostra).
export function inFranchise(title: string, name: string): string {
  if (!title.toLowerCase().startsWith(name.toLowerCase())) return title
  return title.slice(name.length).replace(SEP, '').replace(/^\d{1,2}:\s*/, '') || title
}

// Filmes do destaque: sorteio a cada visita entre os não vistos (todos, se já viu tudo),
// sem dar lugar fixo ao que está pela metade; esse já tem o "Continuar" do player e a barra no card.
export function pickHeroes(items: LibraryItem[], n = 6, rnd = Math.random): LibraryItem[] {
  const fresh = items.filter((it) => !isWatched(it))
  const pool = [...(fresh.length ? fresh : items)]
  for (let i = pool.length - 1; i > 0; i--) {
    const j = Math.floor(rnd() * (i + 1))
    ;[pool[i], pool[j]] = [pool[j], pool[i]]
  }
  return pool.slice(0, n)
}
