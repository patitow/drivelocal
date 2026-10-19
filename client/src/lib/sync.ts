// Ajuste manual de sincronia por arquivo (ms), lembrado no localStorage.
//  - audio > 0: o som sai depois da imagem (som adiantado); < 0: o som sai antes (som atrasado,
//    ex.: fone Bluetooth). Aplicado pelo servidor no stream processado (?adelay=).
//  - sub > 0: a legenda aparece depois; < 0: aparece antes. Aplicado no próprio player.

export interface SyncDelays {
  audio: number
  sub: number
}

export type SyncKind = keyof SyncDelays

export const SYNC_STEP_MS = 50
// o servidor aceita até ±1500 ms de áudio (folga do stream processado); legenda pode estar bem mais longe
export const SYNC_LIMIT_MS: Record<SyncKind, number> = { audio: 1500, sub: 30000 }
export const NO_DELAY: SyncDelays = { audio: 0, sub: 0 }

const KEY_PREFIX = 'drivelocal:sync:'

type KV = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

function storage(): KV | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage
  } catch {
    return null
  }
}

export function clampDelay(kind: SyncKind, ms: number): number {
  if (!Number.isFinite(ms)) return 0
  const lim = SYNC_LIMIT_MS[kind]
  return Math.round(Math.min(lim, Math.max(-lim, ms)))
}

export function loadSync(path: string, store: KV | null = storage()): SyncDelays {
  try {
    const raw = store?.getItem(KEY_PREFIX + path)
    if (!raw) return { ...NO_DELAY }
    const v = JSON.parse(raw) as Partial<SyncDelays>
    return { audio: clampDelay('audio', Number(v.audio) || 0), sub: clampDelay('sub', Number(v.sub) || 0) }
  } catch {
    return { ...NO_DELAY }
  }
}

// Sem ajuste não guarda nada (e apaga o que havia)
export function saveSync(path: string, d: SyncDelays, store: KV | null = storage()): void {
  try {
    if (d.audio === 0 && d.sub === 0) store?.removeItem(KEY_PREFIX + path)
    else store?.setItem(KEY_PREFIX + path, JSON.stringify({ audio: d.audio, sub: d.sub }))
  } catch {
    // modo privado / cota cheia: o ajuste vale só nesta sessão
  }
}

// Passo de ajuste (dir = -1 ou +1); devolve novo objeto
export function stepDelay(d: SyncDelays, kind: SyncKind, dir: number, step = SYNC_STEP_MS): SyncDelays {
  return { ...d, [kind]: clampDelay(kind, d[kind] + dir * step) }
}

// Atalhos de teclado (como no VLC): G/H legenda, J/K áudio
export function syncKey(key: string): { kind: SyncKind; dir: number } | null {
  switch (key.toLowerCase()) {
    case 'g': return { kind: 'sub', dir: -1 }
    case 'h': return { kind: 'sub', dir: 1 }
    case 'j': return { kind: 'audio', dir: -1 }
    case 'k': return { kind: 'audio', dir: 1 }
  }
  return null
}

export function formatDelay(ms: number): string {
  if (ms === 0) return '0 ms'
  const sign = ms > 0 ? '+' : '−'
  const abs = Math.abs(ms)
  return abs >= 1000 ? `${sign}${(abs / 1000).toFixed(2).replace('.', ',')} s` : `${sign}${abs} ms`
}

export function describeDelay(kind: SyncKind, ms: number): string {
  const what = kind === 'audio' ? 'Áudio' : 'Legenda'
  if (ms === 0) return `${what}: sem ajuste`
  return `${what}: ${formatDelay(ms)} (${ms > 0 ? 'depois' : 'antes'} da imagem)`
}
