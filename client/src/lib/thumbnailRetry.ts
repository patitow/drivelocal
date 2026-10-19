// O servidor responde erro enquanto gera a miniatura (o ffmpeg pode levar alguns
// segundos); tenta de novo algumas vezes, com espera crescente.
const MAX_RETRIES = 4
const retries = new Map<string, number>()

// Retorna a espera em ms antes da próxima tentativa, ou null quando esgotou.
export function consumeThumbnailRetry(path: string): number | null {
  const n = retries.get(path) ?? 0
  if (n >= MAX_RETRIES) {
    return null
  }
  retries.set(path, n + 1)
  return 800 * 2 ** n
}
