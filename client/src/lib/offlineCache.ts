// Cache do navegador com os capítulos de mangá baixados para ler sem conexão.
export const MANGA_CACHE = 'drivelocal-manga-v1'

// O cache é do navegador, não do usuário: sai junto com a sessão para o próximo
// login não ver o que o anterior baixou.
export async function clearOfflineManga() {
  if (typeof window !== 'undefined' && 'caches' in window) await caches.delete(MANGA_CACHE)
}
