// Progresso de leitura (servidor, por usuário) e capítulos baixados para ler
// sem conexão (Cache API do navegador).
import { useCallback, useEffect, useRef, useState } from 'react'
import { useAuthUsername } from '@/contexts/AuthContext'
import { library, type LibraryItem, type MangaReadState } from '@/lib/library'
import { buildStreamUrlSync } from '@/lib/utils'
import { MANGA_CACHE } from '@/lib/offlineCache'

export type ReadState = MangaReadState
export type Progress = Record<string, ReadState>

// Chave antiga (só no navegador, sem usuário): migrada para o servidor uma vez.
const LEGACY_KEY = 'drivelocal:manga:progress'
// Espelho local por usuário: mostra o progresso na hora e guarda o que foi
// lido sem conexão até o servidor aceitar.
const mirrorKey = (uid: string) => `drivelocal:manga:progress:${uid}`

function readJSON(key: string): Progress {
  try {
    return JSON.parse(localStorage.getItem(key) || '{}') as Progress
  } catch {
    return {}
  }
}

function writeJSON(key: string, p: Progress) {
  try {
    localStorage.setItem(key, JSON.stringify(p))
  } catch {
    // sem armazenamento: o servidor continua guardando
  }
}

// Junta dois mapas ficando com o estado mais recente de cada obra; devolve
// também as obras em que o local ganhou (precisam subir).
export function mergeProgress(server: Progress, local: Progress): { merged: Progress; push: Progress } {
  const merged: Progress = { ...server }
  const push: Progress = {}
  for (const [work, s] of Object.entries(local)) {
    const remote = server[work]
    if (!remote || s.at > remote.at) {
      const done = [...new Set([...(remote?.done ?? []), ...(s.done ?? [])])]
      merged[work] = { ...s, done }
      push[work] = merged[work]
    }
  }
  return { merged, push }
}

export function useProgress() {
  const uid = useAuthUsername() ?? 'anon'
  const [p, setP] = useState<Progress>(() => readJSON(mirrorKey(uid)))
  const pending = useRef<Progress>({})
  const timer = useRef<number>()

  const flush = useCallback(() => {
    window.clearTimeout(timer.current)
    const batch = pending.current
    if (!Object.keys(batch).length) return
    pending.current = {}
    library.saveMangaProgress(batch).catch(() => {
      // sem conexão: volta para a fila; o espelho local guarda e a próxima
      // abertura da estante sobe o que ficou
      pending.current = { ...batch, ...pending.current }
    })
  }, [])

  useEffect(() => {
    let alive = true
    const local = readJSON(mirrorKey(uid))
    const legacy = readJSON(LEGACY_KEY)
    library.mangaProgress().then((server) => {
      if (!alive) return
      const { merged, push } = mergeProgress(server ?? {}, { ...legacy, ...local })
      writeJSON(mirrorKey(uid), merged)
      setP(merged)
      if (Object.keys(push).length) {
        library.saveMangaProgress(push).then(() => {
          try { localStorage.removeItem(LEGACY_KEY) } catch { /* nada */ }
        }).catch(() => { /* fica no espelho; sobe na próxima */ })
      } else {
        try { localStorage.removeItem(LEGACY_KEY) } catch { /* nada */ }
      }
    }).catch(() => {
      if (alive) setP({ ...legacy, ...local })
    })
    const onHide = () => { if (document.visibilityState === 'hidden') flush() }
    document.addEventListener('visibilitychange', onHide)
    return () => {
      alive = false
      document.removeEventListener('visibilitychange', onHide)
      flush()
    }
  }, [uid, flush])

  const update = useCallback((work: string, s: Omit<ReadState, 'at' | 'done'>, finished: boolean) => {
    setP((prev) => {
      const old = prev[work]
      const done = new Set(old?.done ?? [])
      if (finished) done.add(s.chapter)
      if (old && old.chapter === s.chapter && old.page === s.page && old.pages === s.pages && done.size === old.done.length) return prev
      const st: ReadState = { ...s, at: Date.now(), done: [...done] }
      const next = { ...prev, [work]: st }
      writeJSON(mirrorKey(uid), next)
      pending.current[work] = st
      window.clearTimeout(timer.current)
      timer.current = window.setTimeout(flush, 1500)
      return next
    })
  }, [uid, flush])

  return { progress: p, update }
}

// ---------- leitura offline ----------

const CACHE = MANGA_CACHE
const LIST_PREFIX = '/__offline/manga/'
// O token da URL muda; a chave é só o caminho.
const pageKey = (path: string) => new URL(buildStreamUrlSync(path)).pathname
const listKey = (dir: string) => LIST_PREFIX + encodeURIComponent(dir)

export const offlineSupported = () => typeof window !== 'undefined' && 'caches' in window

// Só cai no offline quando não houve resposta (sem rede); 401/403 continuam erro.
export const isNetworkError = (e: unknown) => !(e as { response?: unknown })?.response

export async function downloadedChapters(): Promise<Set<string>> {
  if (!offlineSupported()) return new Set()
  const c = await caches.open(CACHE)
  const keys = await c.keys()
  const out = new Set<string>()
  for (const k of keys) {
    const p = new URL(k.url).pathname
    if (p.startsWith(LIST_PREFIX) && p !== WORKS_KEY) out.add(decodeURIComponent(p.slice(LIST_PREFIX.length)))
  }
  return out
}

// Baixa todas as páginas; a lista só é gravada no fim (capítulo incompleto
// não conta como baixado).
export async function downloadChapter(dir: string, pages: LibraryItem[], onStep?: (done: number) => void) {
  const c = await caches.open(CACHE)
  let n = 0
  const queue = [...pages]
  const worker = async () => {
    for (let it = queue.shift(); it; it = queue.shift()) {
      const key = pageKey(it.path)
      if (!(await c.match(key))) {
        const res = await fetch(buildStreamUrlSync(it.path), { credentials: 'same-origin' })
        if (!res.ok) throw new Error(`página ${it.name}: HTTP ${res.status}`)
        await c.put(key, res)
      }
      onStep?.(++n)
    }
  }
  await Promise.all([worker(), worker(), worker()])
  await c.put(listKey(dir), new Response(JSON.stringify(pages), { headers: { 'Content-Type': 'application/json' } }))
}

export async function removeChapter(dir: string) {
  const c = await caches.open(CACHE)
  const res = await c.match(listKey(dir))
  if (res) {
    const pages = (await res.json()) as LibraryItem[]
    await Promise.all(pages.map((p) => c.delete(pageKey(p.path))))
  }
  await c.delete(listKey(dir))
}

export async function offlinePages(dir: string): Promise<LibraryItem[] | null> {
  if (!offlineSupported()) return null
  const res = await (await caches.open(CACHE)).match(listKey(dir))
  return res ? ((await res.json()) as LibraryItem[]) : null
}

// URL de uma página baixada (blob local), ou null para usar a rede.
export async function offlinePageUrl(path: string): Promise<string | null> {
  if (!offlineSupported()) return null
  const res = await (await caches.open(CACHE)).match(pageKey(path))
  return res ? URL.createObjectURL(await res.blob()) : null
}

// Lista de obras de quando havia conexão, para abrir a estante sem rede.
const WORKS_KEY = LIST_PREFIX + '__works'
export async function rememberWorks<T>(works: T): Promise<T> {
  if (offlineSupported()) {
    caches.open(CACHE).then((c) => c.put(WORKS_KEY, new Response(JSON.stringify(works)))).catch(() => {})
  }
  return works
}
export async function offlineWorks<T>(): Promise<T | null> {
  if (!offlineSupported()) return null
  const res = await (await caches.open(CACHE)).match(WORKS_KEY)
  return res ? ((await res.json()) as T) : null
}
