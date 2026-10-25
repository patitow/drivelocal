import { useCallback, useRef, type MutableRefObject } from 'react'

// O Chrome só destrói o player de um <video> quando o elemento é coletado, e com 1000 vivos na aba
// recusa qualquer vídeo novo ("too many WebMediaPlayers"). Basta algo segurar o elemento depois de
// desmontado para vazar. Soltar o src ao desmontar destrói o player na hora.
export function releaseMedia(el: HTMLMediaElement | null) {
  if (!el || !el.hasAttribute('src')) return
  el.pause()
  el.removeAttribute('src')
  el.load()
}

// Ref de callback para <video>/<audio> que solta o player quando o elemento sai da tela;
// `target` recebe o elemento, para quem ainda precisa de um ref de objeto.
export function useReleasingMediaRef<T extends HTMLMediaElement>(target?: MutableRefObject<T | null>) {
  const current = useRef<T | null>(null)
  return useCallback((el: T | null) => {
    if (target) target.current = el
    if (current.current && current.current !== el) releaseMedia(current.current)
    current.current = el
  }, [target])
}
