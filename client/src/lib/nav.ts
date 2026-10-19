// Estado de navegação que vive na URL (?serie=, ?criador=, ...): voltar/avançar
// do navegador e do celular funcionam, e um link abre a mesma tela.
import { useCallback } from 'react'
import { useNavigate, useRouter, useSearch } from '@tanstack/react-router'

// Última tela visitada: "/" volta para ela.
export const LAST_KEY = 'drivelocal:last'

export interface CatalogSearch {
  serie?: string // série aberta (SeriesGroup.key)
  criador?: number // perfil de criador (topic id)
  obra?: string // obra de mangá (dir)
  cap?: number // capítulo em leitura (índice)
  topico?: number
  playlist?: number
  q?: string // termo da busca
}

const str = (v: unknown) => (typeof v === 'string' && v !== '' ? v : typeof v === 'number' ? String(v) : undefined)
const num = (v: unknown) => {
  const n = typeof v === 'number' ? v : typeof v === 'string' ? Number(v) : NaN
  return Number.isFinite(n) ? n : undefined
}

export function parseCatalogSearch(s: Record<string, unknown>): CatalogSearch {
  const out: CatalogSearch = {
    serie: str(s.serie), criador: num(s.criador), obra: str(s.obra), cap: num(s.cap),
    topico: num(s.topico), playlist: num(s.playlist), q: str(s.q),
  }
  for (const k of Object.keys(out) as Array<keyof CatalogSearch>) if (out[k] === undefined) delete out[k]
  return out
}

// Um parâmetro da URL como estado. Abrir algo empilha no histórico (o voltar
// fecha); replace serve para o que muda a cada tecla, como a busca.
export function useNavParam<K extends keyof CatalogSearch>(key: K) {
  const search = useSearch({ strict: false }) as CatalogSearch
  const navigate = useNavigate()
  const router = useRouter()
  const set = useCallback((v: CatalogSearch[K] | null | undefined, opts?: { replace?: boolean; also?: Partial<Record<keyof CatalogSearch, unknown>> }) => {
    void navigate({
      // mesma página, só muda o parâmetro
      to: router.state.location.pathname as never,
      search: ((prev: CatalogSearch) => ({ ...prev, ...opts?.also, [key]: v ?? undefined })) as never,
      replace: opts?.replace,
    })
  }, [navigate, router, key])
  return [search[key], set] as const
}
