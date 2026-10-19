import { useCallback, useEffect, useRef, useState } from 'react'
import { library, resumePoint, shouldSaveProgress, type Prefs } from '@/lib/library'

export interface PlaybackProgressState {
  // true quando a consulta terminou (com ou sem dados); o player espera isso para começar
  ready: boolean
  // segundos de onde retomar (já respeitando a preferência de auto-retomar), ou null
  resumeAt: number | null
  prefs: Prefs | null
}

const DEFAULT_STATE: PlaybackProgressState = { ready: false, resumeAt: null, prefs: null }

// Posição salva por usuário no servidor (biblioteca). Se a biblioteca estiver
// desligada ou o arquivo fora do catálogo, o player segue como antes.
export function usePlaybackProgress(path: string) {
  const [state, setState] = useState<PlaybackProgressState>(DEFAULT_STATE)
  const lastSavedRef = useRef<number | null>(null)
  const availableRef = useRef(false)

  useEffect(() => {
    let cancelled = false
    setState(DEFAULT_STATE)
    lastSavedRef.current = null
    availableRef.current = false
    // não deixa o player esperando se o servidor demorar
    const timer = setTimeout(() => {
      if (!cancelled) setState((s) => (s.ready ? s : { ...s, ready: true }))
    }, 1500)
    library.progressByPath(path)
      .then((r) => {
        if (cancelled) return
        availableRef.current = true
        lastSavedRef.current = r.progress?.positionS ?? null
        const autoResume = r.prefs?.autoResume !== false
        setState({ ready: true, resumeAt: autoResume ? resumePoint(r.progress) : null, prefs: r.prefs ?? null })
      })
      .catch(() => {
        if (!cancelled) setState({ ready: true, resumeAt: null, prefs: null })
      })
      .finally(() => clearTimeout(timer))
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [path])

  // Grava a posição; sem `force` só grava a cada ~10 s de diferença.
  const report = useCallback((positionS: number, durationS: number, opts: { force?: boolean; completed?: boolean } = {}) => {
    if (!availableRef.current || !Number.isFinite(positionS)) return
    if (!shouldSaveProgress(lastSavedRef.current, positionS, opts.force)) return
    lastSavedRef.current = positionS
    void library.saveProgressByPath(path, positionS, Number.isFinite(durationS) ? durationS : 0, opts.completed).catch(() => {})
  }, [path])

  return { ...state, report }
}
