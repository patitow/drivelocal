// Regras da biblioteca num lugar só: o que é anime, filme de anime, visto e em andamento.
import type { LibraryItem } from './library'

export const ANIME_ROOT = 'Animes'
// Vídeo solto de anime com 40 min ou mais é filme; abaixo disso, episódio.
export const ANIME_FILM_MIN_S = 2400

export const isAnimeRoot = (root?: string | null) => root === ANIME_ROOT
export const isAnimePath = (path: string) => path.replace(/^\/+/, '').split('/')[0] === ANIME_ROOT

export function progressPct(item: LibraryItem): number {
  return item.progress && item.progress.durationS > 0 ? Math.min(100, (item.progress.positionS / item.progress.durationS) * 100) : 0
}

export const isWatched = (it: LibraryItem) => !!it.progress?.completed || progressPct(it) >= 95
export const isInProgress = (it: LibraryItem) => progressPct(it) > 0 && !isWatched(it)

// Botão de pular abertura sem capítulo no arquivo: só em episódio de anime (abertura de ~1:30
// logo no começo). Filme, série ocidental, gravação e vídeo de criador não seguem esse padrão.
export const introGuessable = (it: LibraryItem) =>
  it.kind === 'video' && isAnimeRoot(it.root) && it.episode != null && (it.durationS ?? 0) < ANIME_FILM_MIN_S
