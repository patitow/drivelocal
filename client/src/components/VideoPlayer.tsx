import { useState, useRef, useEffect, useCallback } from 'react'
import { Play, Pause, Volume2, VolumeX, Maximize, Download, X, Loader2, ChevronLeft, ChevronRight, Captions, AudioLines, Check, FileWarning, SkipForward } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { buildStreamUrlSync, buildDownloadUrlSync, buildFontUrlSync, buildSubtitleUrlSync } from '@/lib/utils'
import { saveAs } from 'file-saver'
import { toast } from 'sonner'
import { SERVER_RESTORED_EVENT } from '@/lib/reconnect'
import { api } from '@/lib/api'
import { parseVtt, type Cue } from '@/lib/vtt'
import { audioLabel, defaultAudio, encodeAudioPref, loadTrackPrefs, pickAudioByLang, pickAudioByPref, pickSubtitleByLang, saveTrackPref, subtitleLabel } from '@/lib/tracks'
import { buildPlaybackUrl, clientCaps, effectiveMode, initialMode, modeLabel, nextMode, silentFailure, streamBase } from '@/lib/playback'
import { clampDelay, describeDelay, formatDelay, loadSync, saveSync, syncKey, SYNC_STEP_MS, type SyncDelays, type SyncKind } from '@/lib/sync'
import { createAssRenderer, createPgsRenderer, type SubRenderer } from '@/lib/subRenderers'
import type { AudioTrack, MediaInfo, PlaybackMode, SubtitleTrack } from '@/types'
import { usePlaybackProgress } from '@/hooks/usePlaybackProgress'
import { useReleasingMediaRef } from '@/hooks/useReleasingMediaRef'
import { formatDuration, nextPlayableIndex, type TrackChoice } from '@/lib/library'

// APIs de tela cheia com prefixo (Safari, Firefox antigo, IE/Edge)
type VendorFullscreenElement = HTMLElement & {
  webkitRequestFullscreen?: () => Promise<void> | void
  mozRequestFullScreen?: () => Promise<void> | void
  msRequestFullscreen?: () => Promise<void> | void
}
type VendorFullscreenDocument = Document & {
  webkitExitFullscreen?: () => Promise<void> | void
  mozCancelFullScreen?: () => Promise<void> | void
  msExitFullscreen?: () => Promise<void> | void
}

const VTT_ENTITIES: Record<string, string> = { '&amp;': '&', '&lt;': '<', '&gt;': '>', '&nbsp;': ' ', '&lrm;': '', '&rlm;': '' }

// Renderiza o texto de uma cue sem innerHTML: só <i> e <b> viram estilo
function CueText({ text }: { text: string }) {
  let italic = false
  let bold = false
  return (
    <>
      {text.split(/(<\/?[ib]>)/).map((part, i) => {
        if (part === '<i>') { italic = true; return null }
        if (part === '</i>') { italic = false; return null }
        if (part === '<b>') { bold = true; return null }
        if (part === '</b>') { bold = false; return null }
        const plain = part.replace(/&(amp|lt|gt|nbsp|lrm|rlm);/g, (m) => VTT_ENTITIES[m] ?? m)
        return <span key={i} className={`${italic ? 'italic ' : ''}${bold ? 'font-bold' : ''}`}>{plain}</span>
      })}
    </>
  )
}

// Linha de ajuste de sincronia nos menus de áudio/legenda: − valor + e Redefinir
function SyncRow({ label, keys, value, onChange }: { label: string; keys: string; value: number; onChange: (ms: number) => void }) {
  const btn = 'rounded px-2 py-0.5 hover:bg-white/20 disabled:opacity-30 disabled:hover:bg-transparent'
  return (
    <div role="group" aria-label={label} className="mt-1 flex items-center gap-1 border-t border-white/10 px-3 py-2 text-xs text-white/60">
      <span className="flex-1 truncate" title={`Atalhos: ${keys}`}>{label} <span className="text-white/40">({keys})</span></span>
      <button type="button" className={btn} aria-label={`${label}: −${SYNC_STEP_MS} ms`} onClick={() => onChange(value - SYNC_STEP_MS)}>−</button>
      <span className="w-16 text-center tabular-nums text-white/90">{formatDelay(value)}</span>
      <button type="button" className={btn} aria-label={`${label}: +${SYNC_STEP_MS} ms`} onClick={() => onChange(value + SYNC_STEP_MS)}>+</button>
      <button type="button" className={btn} disabled={value === 0} onClick={() => onChange(0)}>Redefinir</button>
    </div>
  )
}

interface VideoPlayerProps {
  file: {
    name: string
    path: string
    isImage?: boolean
    isVideo?: boolean
  }
  files: Array<{ name: string; path: string; isImage?: boolean; isVideo?: boolean }>
  currentIndex: number
  onClose: () => void
  onNavigate: (index: number) => void
}

// Caminho que deve começar tocando ao abrir (definido pelo autoplay ao fim do anterior)
let autoplayPendingPath: string | null = null
const UP_NEXT_SECONDS = 8
const LONG_PAUSE_MS = 3 * 60 * 1000 // pausado além disso, remux/transcode retomam com stream novo
// Sem capítulo de abertura no arquivo: botão de pular este tanto (abertura típica de anime).
const INTRO_GUESS_S = 85

export function VideoPlayer({ file, files, currentIndex, onClose, onNavigate }: VideoPlayerProps) {
  const [isPlaying, setIsPlaying] = useState(false)
  const [isMuted, setIsMuted] = useState(false)
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [volume, setVolume] = useState(1)
  const [isLoading, setIsLoading] = useState(true)
  const [hasError, setHasError] = useState(false)
  const [hasStartedPlaying, setHasStartedPlaying] = useState(false) // Track if video has started playing
  const [showControls, setShowControls] = useState(true) // Control visibility of UI
  const [videoBox, setVideoBox] = useState<{ left: number; width: number } | null>(null)
  const controlsTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const swipeStartXRef = useRef<number | null>(null)
  const isPlayingRef = useRef(false)
  const lastTimeRef = useRef(0)
  const authRetriedRef = useRef(false)
  const [, setAuthSeq] = useState(0) // força novo render (e novo src) após renovar o token
  const currentPathRef = useRef(file.path)
  currentPathRef.current = file.path
  // Posição e estado de reprodução no momento de um erro, para retomar após o servidor voltar
  const resumeRef = useRef<{ time: number; wasPlaying: boolean } | null>(null)
  const pausedAtRef = useRef<number | null>(null) // quando pausou (pause longo: o stream processado no servidor expira)

  // Faixas de áudio/legenda e modo de reprodução. minMode vem do servidor (direct/remux/transcode)
  // e sobe na cadeia de fallback se o navegador falhar; audioSel !== null = faixa que exige remux.
  // Fora do modo direct o servidor gera o stream a partir de `offset` e o tempo do <video> é relativo a ele.
  const [info, setInfo] = useState<MediaInfo | null>(null)
  const [infoReady, setInfoReady] = useState(false)
  // faixas lembradas já aplicadas (o <video> só recebe src depois disso, para não carregar duas vezes)
  const [tracksReady, setTracksReady] = useState(false)
  // faixas escolhidas neste arquivo, gravadas junto com o progresso para valer nos próximos episódios
  const trackChoiceRef = useRef<TrackChoice>({})
  // última legenda ligada (tecla C religa a mesma)
  const lastSubIdRef = useRef<string | null>(null)
  const [minMode, setMinMode] = useState<PlaybackMode>('direct')
  const [audioSel, setAudioSel] = useState<number | null>(null)
  const [offset, setOffset] = useState(0)
  // Ajuste manual de sincronia (ms, por arquivo). sync = o que o usuário escolheu; streamDelay = o ajuste
  // de áudio com que o stream atual foi pedido ao servidor (só muda quando o stream é recarregado).
  const [sync, setSync] = useState<SyncDelays>(() => loadSync(file.path))
  const [streamDelay, setStreamDelay] = useState(() => loadSync(file.path).audio)
  const [osd, setOsd] = useState<string | null>(null)
  // "Retomando de X" discreto dentro do player (o toast global cobria o vídeo no celular)
  const [resumeHint, setResumeHint] = useState<number | null>(null)
  const resumeHintTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const earlyEndRef = useRef(-1) // onde o último "ended" antes da hora foi retomado
  const syncRef = useRef(sync)
  const streamDelayRef = useRef(streamDelay)
  const osdTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const audioDelayTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [subId, setSubId] = useState<string | null>(null)
  const [cues, setCues] = useState<Cue[]>([])
  const [activeText, setActiveText] = useState('')
  const [menu, setMenu] = useState<'audio' | 'subs' | null>(null)
  // ffprobe não abre o arquivo: aviso no lugar do player (não tenta tocar)
  const [broken, setBroken] = useState<string | null>(null)
  const [subLoading, setSubLoading] = useState(false)
  // contagem antes do próximo episódio (autoplay)
  const [upNext, setUpNext] = useState<{ index: number; left: number } | null>(null)
  const offsetRef = useRef(0)
  const audioSelRef = useRef<number | null>(null)
  const modeRef = useRef<PlaybackMode>('direct')
  const minModeRef = useRef<PlaybackMode>('direct')
  const seekSeqRef = useRef(0)
  const silentCheckRef = useRef<{ checked: boolean; startedAt: number | null }>({ checked: false, startedAt: null })
  const subRendererRef = useRef<SubRenderer | null>(null)
  const infoRef = useRef<MediaInfo | null>(null)
  const durationRef = useRef(0)
  const pendingPlayRef = useRef(false)
  const startAtRef = useRef<number | null>(null)
  const scrubbingRef = useRef(false)
  const scrubValueRef = useRef(0)

  const videoRef = useRef<HTMLVideoElement>(null)
  const videoElRef = useReleasingMediaRef(videoRef)
  const containerRef = useRef<HTMLDivElement>(null)
  const videoContainerRef = useRef<HTMLDivElement>(null)

  // URL do stream com o token congelado: o token é renovado a cada poucos minutos e, se a URL do
  // render mudasse junto, o <video> trocaria de src e voltaria ao começo do episódio. Só restartAt
  // (seek, troca de faixa, retry após token expirado) pega um token novo.
  const streamUrlRef = useRef({ path: '', url: '' })
  if (streamUrlRef.current.path !== file.path) {
    streamUrlRef.current = { path: file.path, url: buildStreamUrlSync(file.path) }
  }
  const streamUrl = streamUrlRef.current.url
  const mode = effectiveMode(minMode, audioSel, streamDelay)
  modeRef.current = mode
  const src = buildPlaybackUrl(streamUrl, mode, audioSel, offset, streamDelay)
  // tempo do arquivo = base + video.currentTime (stream processado começa streamLead s antes de offset)
  const base = streamBase(mode, offset, streamDelay)

  // Progresso por usuário (biblioteca): retomar posição, salvar periodicamente e autoplay do próximo
  const progress = usePlaybackProgress(file.path)
  const progressRef = useRef(progress)
  progressRef.current = progress
  const resumeAppliedRef = useRef<string | null>(null)

  const canGoPrevious = currentIndex > 0
  const canGoNext = currentIndex < files.length - 1

  const playNext = useCallback((index: number) => {
    setUpNext(null)
    autoplayPendingPath = files[index]?.path ?? null
    onNavigate(index)
  }, [files, onNavigate])

  // Contagem do próximo episódio; dar play de novo (voltou a cena) cancela.
  useEffect(() => {
    if (!upNext) return
    if (upNext.left <= 0) { playNext(upNext.index); return }
    const t = window.setTimeout(() => setUpNext((u) => (u ? { ...u, left: u.left - 1 } : u)), 1000)
    const video = videoRef.current
    const cancel = () => setUpNext(null)
    video?.addEventListener('play', cancel)
    return () => {
      window.clearTimeout(t)
      video?.removeEventListener('play', cancel)
    }
  }, [upNext, playNext])

  const handlePrevious = useCallback(() => {
    if (currentIndex > 0) {
      onNavigate(currentIndex - 1)
    }
  }, [currentIndex, onNavigate])

  const filesRef = useRef(files)
  filesRef.current = files
  const indexRef = useRef(currentIndex)
  indexRef.current = currentIndex
  const onNavigateRef = useRef(onNavigate)
  onNavigateRef.current = onNavigate

  const handleNext = useCallback(() => {
    if (currentIndex < files.length - 1) {
      onNavigate(currentIndex + 1)
    }
  }, [currentIndex, files.length, onNavigate])

  // Reset states when file changes
  useEffect(() => {
    setIsLoading(true)
    setHasError(false)
    setHasStartedPlaying(false)
    setCurrentTime(0)
    setDuration(0)
    setIsPlaying(false)
    setVideoBox(null)
    lastTimeRef.current = 0
    resumeRef.current = null
    setInfo(null)
    setInfoReady(false)
    setTracksReady(false)
    trackChoiceRef.current = {}
    lastSubIdRef.current = null
    setAudioSel(null)
    setMinMode('direct')
    setOffset(0)
    setSubId(null)
    setCues([])
    setActiveText('')
    setMenu(null)
    setBroken(null)
    setSubLoading(false)
    setUpNext(null)
    const saved = loadSync(file.path)
    syncRef.current = saved
    streamDelayRef.current = saved.audio
    setSync(saved)
    setStreamDelay(saved.audio)
    setOsd(null)
    setResumeHint(null)
    if (resumeHintTimerRef.current) clearTimeout(resumeHintTimerRef.current)
    earlyEndRef.current = -1
    if (audioDelayTimerRef.current) clearTimeout(audioDelayTimerRef.current)
    infoRef.current = null
    audioSelRef.current = null
    minModeRef.current = 'direct'
    seekSeqRef.current++
    startAtRef.current = null
    pendingPlayRef.current = false
    scrubbingRef.current = false
    authRetriedRef.current = false
  }, [file.path])

  useEffect(() => { audioSelRef.current = audioSel }, [audioSel])
  useEffect(() => { minModeRef.current = minMode }, [minMode])
  useEffect(() => { offsetRef.current = base }, [base])
  useEffect(() => { durationRef.current = duration }, [duration])
  useEffect(() => () => {
    if (resumeHintTimerRef.current) clearTimeout(resumeHintTimerRef.current)
    if (osdTimerRef.current) clearTimeout(osdTimerRef.current)
  }, [])
  // Trocou de stream: o <video> recomeça, então o estado de "já tocou" e a checagem de falha silenciosa também
  useEffect(() => {
    setHasStartedPlaying(false)
    silentCheckRef.current = { checked: false, startedAt: null }
  }, [src])

  // Descobre as faixas do arquivo (as preferências lembradas são aplicadas quando o progresso chega)
  useEffect(() => {
    let cancelled = false
    api.getMediaInfo(file.path, clientCaps())
      .then((mi) => {
        if (cancelled) return
        mi = { ...mi, audio: mi.audio ?? [], subtitles: mi.subtitles ?? [], fonts: mi.fonts ?? [] }
        infoRef.current = mi
        setInfo(mi)
        const startMode = initialMode(mi)
        minModeRef.current = startMode
        setMinMode(startMode)
      })
      .catch((e: { response?: { data?: { broken?: unknown } } }) => {
        // arquivo corrompido: o servidor diz o motivo; outros erros (sem ffprobe): toca o arquivo cru
        const why = e?.response?.data?.broken
        if (!cancelled && typeof why === 'string') setBroken(why)
      })
      .finally(() => { if (!cancelled) setInfoReady(true) })
    return () => { cancelled = true }
  }, [file.path])

  // Baixa e exibe a legenda escolhida: texto vira WebVTT no overlay; ASS vai para o JASSUB
  // (estilos/fontes do arquivo) e PGS para o libpgs, ambos desenhando num canvas sobre o vídeo.
  const subKind = info?.subtitles.find((s) => s.id === subId)?.kind || 'text'
  useEffect(() => {
    setCues([])
    setActiveText('')
    const video = videoRef.current
    if (!subId || !video) return
    let cancelled = false
    let renderer: SubRenderer | null = null
    setSubLoading(true)
    const loaded = () => { if (!cancelled) setSubLoading(false) }
    const fail = () => {
      if (cancelled) return
      setSubLoading(false)
      setSubId(null)
      toast.error('Não foi possível carregar a legenda')
    }
    const loadVtt = () => api.getSubtitleVtt(file.path, subId)
      .then((vtt) => { if (!cancelled) setCues(parseVtt(vtt)) })
      .then(loaded, fail)

    if (subKind === 'ass') {
      const fonts = (infoRef.current?.fonts ?? []).map((f) => buildFontUrlSync(file.path, f.index))
      createAssRenderer(video, buildSubtitleUrlSync(file.path, subId, 'ass'), fonts, offsetRef.current - syncRef.current.sub / 1000)
        .then((r) => { if (cancelled) r.destroy(); else { renderer = r; subRendererRef.current = r; loaded() } })
        // sem Worker/WebAssembly: mostra o texto do ASS como WebVTT
        .catch(() => { if (!cancelled) void loadVtt() })
    } else if (subKind === 'pgs') {
      createPgsRenderer(video, buildSubtitleUrlSync(file.path, subId, 'sup'), offsetRef.current - syncRef.current.sub / 1000)
        .then((r) => { if (cancelled) r.destroy(); else { renderer = r; subRendererRef.current = r; loaded() } })
        .catch(fail)
    } else {
      void loadVtt()
    }
    return () => {
      cancelled = true
      setSubLoading(false)
      renderer?.destroy()
      subRendererRef.current = null
    }
  }, [file.path, subId, subKind])

  // Legendas de texto embutidas ainda sendo extraídas (mkv grande): pergunta de
  // novo de tempos em tempos para o menu mostrar quando ficam prontas.
  const subsPending = !!info?.subtitles.some((t) => t.kind === 'text' && !t.external && t.ready === false)
  useEffect(() => {
    if (!subsPending) return
    let tries = 0
    const id = window.setInterval(() => {
      if (++tries > 60) { window.clearInterval(id); return }
      api.getMediaInfo(file.path, clientCaps()).then((fresh) => {
        const ready = new Map((fresh.subtitles ?? []).map((t) => [t.id, t.ready]))
        setInfo((prev) => {
          if (!prev) return prev
          const next = { ...prev, subtitles: prev.subtitles.map((t) => ({ ...t, ready: ready.get(t.id) ?? t.ready })) }
          infoRef.current = next
          return next
        })
      }).catch(() => {})
    }, 6000)
    return () => window.clearInterval(id)
  }, [subsPending, file.path])

  // O canvas da legenda segue o tempo do arquivo (tempo do <video> + base do stream) menos o ajuste da legenda
  useEffect(() => { subRendererRef.current?.setOffset(base - sync.sub / 1000) }, [base, sync.sub])

  // Legenda ativa lida a cada frame (timeupdate é grosseiro demais para falas curtas)
  useEffect(() => {
    if (cues.length === 0) return
    let raf = 0
    let last = ''
    const tick = () => {
      const video = videoRef.current
      if (video) {
        const t = offsetRef.current + video.currentTime - syncRef.current.sub / 1000
        const text = cues.filter((c) => t >= c.start && t < c.end).map((c) => c.text).join('\n')
        if (text !== last) {
          last = text
          setActiveText(text)
        }
      }
      raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [cues])

  useEffect(() => {
    isPlayingRef.current = isPlaying
  }, [isPlaying])

  // Aplica a dublagem/legenda lembradas: a última usada nesta série (servidor) e, sem ela, o
  // último idioma escolhido neste navegador.
  useEffect(() => {
    if (!infoReady || !progress.ready || tracksReady) return
    const mi = infoRef.current
    if (mi) {
      const local = loadTrackPrefs()
      const series = progress.tracks
      const native = defaultAudio(mi)
      const remembered = pickAudioByPref(mi, series?.audio) ?? pickAudioByLang(mi, local.audio)
      const wanted = remembered ?? native
      if (wanted && !(wanted.order === native?.order && wanted.playable)) {
        audioSelRef.current = wanted.order
        setAudioSel(wanted.order)
      }
      if (remembered) trackChoiceRef.current.audioTrack = encodeAudioPref(remembered)
      const subPref = series?.sub || local.sub
      const sub = subPref && subPref !== 'off' ? pickSubtitleByLang(mi, subPref) : undefined
      if (sub) setSubId(sub.id)
      if (subPref) trackChoiceRef.current.subtitleTrack = sub ? subPref : 'off'
    }
    setTracksReady(true)
  }, [infoReady, progress.ready, progress.tracks, tracksReady])

  // Retoma de onde o usuário parou (e dá play se veio do autoplay) assim que faixas e progresso chegaram
  useEffect(() => {
    if (!tracksReady || resumeAppliedRef.current === file.path) return
    resumeAppliedRef.current = file.path
    // autoplay: o próximo arquivo começa tocando (vale mesmo se o player for remontado)
    const play = autoplayPendingPath === file.path
    autoplayPendingPath = null
    if (play) {
      pendingPlayRef.current = true
      setIsPlaying(true)
    }
    const t = progress.resumeAt
    if (t === null) return
    if (modeRef.current === 'direct') {
      // aplicado no loadedmetadata do stream
      startAtRef.current = t
      lastTimeRef.current = t
      setCurrentTime(t)
    } else {
      void restartRef.current(t, play, true)
    }
    setResumeHint(t)
    if (resumeHintTimerRef.current) clearTimeout(resumeHintTimerRef.current)
    resumeHintTimerRef.current = setTimeout(() => setResumeHint(null), 6000)
  }, [tracksReady, progress.resumeAt, file.path])

  // Salva a posição a cada ~10 s tocando, ao pausar e ao trocar de arquivo/fechar
  useEffect(() => {
    const video = videoRef.current
    // report deste arquivo: no cleanup progressRef já aponta para o próximo
    const report = progressRef.current.report
    const save = (force: boolean) => {
      if (lastTimeRef.current > 0) report(lastTimeRef.current, durationRef.current, { force, tracks: trackChoiceRef.current })
    }
    const timer = setInterval(() => { if (isPlayingRef.current) save(false) }, 5000)
    const onPause = () => save(true)
    video?.addEventListener('pause', onPause)
    return () => {
      clearInterval(timer)
      video?.removeEventListener('pause', onPause)
      if (!video?.ended) save(true)
    }
  }, [file.path])

  // Servidor voltou após erro: recarrega o stream e retoma da posição em que parou
  useEffect(() => {
    const handleRestored = () => {
      const video = videoRef.current
      const resume = resumeRef.current
      if (!video || !resume) return
      resumeRef.current = null

      if (modeRef.current !== 'direct') {
        // remux/transcode: pede o stream de novo a partir da posição salva
        setIsPlaying(resume.wasPlaying)
        setHasError(false)
        restartRef.current(resume.time, resume.wasPlaying, true)
        return
      }

      const handleLoadedForResume = () => {
        video.currentTime = resume.time
        setCurrentTime(resume.time)
        if (resume.wasPlaying) {
          setIsPlaying(true)
          video.play().catch(() => setIsPlaying(false))
        }
      }
      video.addEventListener('loadedmetadata', handleLoadedForResume, { once: true })

      setHasError(false)
      setIsLoading(true)
      video.load()
    }

    window.addEventListener(SERVER_RESTORED_EVENT, handleRestored)
    return () => window.removeEventListener(SERVER_RESTORED_EVENT, handleRestored)
  }, [])

  // Compute the video's actual rendered box (object-fit: contain math) so the
  // controls bar can match a portrait/vertical video's width instead of
  // spanning the full letterboxed backdrop - otherwise clicking the seek bar
  // area beside a narrow video lands on the backdrop and closes the modal.
  const updateVideoBox = useCallback(() => {
    const video = videoRef.current
    const container = videoContainerRef.current
    if (!video || !container || !video.videoWidth || !video.videoHeight) return

    const containerWidth = container.clientWidth
    const containerHeight = container.clientHeight
    const videoRatio = video.videoWidth / video.videoHeight
    const containerRatio = containerWidth / containerHeight

    let displayWidth: number
    if (videoRatio > containerRatio) {
      displayWidth = containerWidth
    } else {
      displayWidth = containerHeight * videoRatio
    }

    setVideoBox({ left: (containerWidth - displayWidth) / 2, width: displayWidth })
  }, [])

  useEffect(() => {
    const container = videoContainerRef.current
    if (!container) return
    const observer = new ResizeObserver(() => updateVideoBox())
    observer.observe(container)
    return () => observer.disconnect()
  }, [updateVideoBox])

  useEffect(() => {
    const video = videoRef.current
    if (!video) return

    const handleLoadedMetadata = () => {
      if (modeRef.current !== 'direct') {
        // MP4 fragmentado não informa a duração: usa a do ffprobe
        setDuration(infoRef.current?.duration || 0)
      } else {
        setDuration(video.duration)
      }
      // sem imagem e sem erro (ex.: mpeg4/wmv no Chrome): tenta o próximo modo
      if (modeRef.current !== 'transcode' && silentFailure(infoRef.current, { videoWidth: video.videoWidth, playedSeconds: 0 })) {
        silentCheckRef.current.checked = true
        void fallbackRef.current('sem vídeo', true)
        return
      }
      if (startAtRef.current !== null) {
        // posição relativa ao início deste stream
        video.currentTime = startAtRef.current
        startAtRef.current = null
      } else if (modeRef.current !== 'direct') {
        // stream processado começa com streamLead s de silêncio antes do ponto pedido: pula para ele
        video.currentTime = Math.max(0, -offsetRef.current)
      }
      if (pendingPlayRef.current) {
        pendingPlayRef.current = false
        video.play().catch(() => setIsPlaying(false))
      }
      updateVideoBox()
    }

    const handleTimeUpdate = () => {
      if (scrubbingRef.current) return
      const t = Math.max(0, offsetRef.current + video.currentTime)
      lastTimeRef.current = t
      setCurrentTime(t)

      // som mudo sem erro (ac3/eac3/dts no Chrome): depois de ~1,5 s tocando sem decodificar áudio, faz fallback
      const check = silentCheckRef.current
      const audioBytes = (video as HTMLVideoElement & { webkitAudioDecodedByteCount?: number }).webkitAudioDecodedByteCount
      if (check.checked || modeRef.current !== 'direct' || audioBytes === undefined || video.paused) return
      if (check.startedAt === null) { check.startedAt = video.currentTime; return }
      const played = video.currentTime - check.startedAt
      if (played < 1.5) return
      check.checked = true
      if (silentFailure(infoRef.current, { videoWidth: video.videoWidth, audioBytes, playedSeconds: played }) === 'no-audio') {
        void fallbackRef.current('sem áudio', true)
      }
    }

    const handleEnded = () => {
      // "ended" longe do fim conhecido é o stream que caiu, não o episódio acabando:
      // retoma dali uma vez em vez de marcar como visto e oferecer o próximo
      const at = lastTimeRef.current
      if (durationRef.current > 0 && at < durationRef.current - 30 && Math.abs(at - earlyEndRef.current) > 5) {
        earlyEndRef.current = at
        void restartRef.current(at, true, true)
        return
      }
      setIsPlaying(false)
      const p = progressRef.current
      const dur = durationRef.current || video.duration || lastTimeRef.current
      p.report(dur, dur, { force: true, completed: true })
      if (p.prefs?.autoplay === false) return
      const next = nextPlayableIndex(filesRef.current, indexRef.current)
      if (next !== null) setUpNext({ index: next, left: UP_NEXT_SECONDS })
    }

    const handleError = () => {
      const code = video.error?.code
      if (code === MediaError.MEDIA_ERR_ABORTED) return
      void fallbackRef.current(`erro ${code ?? '?'}`)
    }

    const handleLoadStart = () => {
      setIsLoading(true)
      setHasError(false)
    }

    const handleCanPlay = () => {
      // Only hide loading if video hasn't started playing yet
      if (!hasStartedPlaying) {
        setIsLoading(false)
      }
    }

    const handleLoadedData = () => {
      // Only hide loading if video hasn't started playing yet
      if (!hasStartedPlaying) {
        setIsLoading(false)
      }
    }

    const handleWaiting = () => {
      // Only show loading if video hasn't started playing yet
      // During playback, buffering should not show loading overlay
      if (!hasStartedPlaying) {
        setIsLoading(true)
      }
    }

    const handlePause = () => {
      if (!video.ended) pausedAtRef.current = Date.now()
    }

    // Pause longo em remux/transcode: o servidor encerra o ffmpeg ocioso e depois apaga o arquivo,
    // e continuar lendo bytes de um stream refeito leva a um ponto errado. Pede o stream de novo
    // a partir da posição atual em vez de retomar o antigo.
    const handlePlay = () => {
      const pausedAt = pausedAtRef.current
      pausedAtRef.current = null
      if (pausedAt === null || modeRef.current === 'direct' || Date.now() - pausedAt < LONG_PAUSE_MS) return
      video.pause()
      pausedAtRef.current = null
      void restartRef.current(lastTimeRef.current, true, true)
    }

    const handlePlaying = () => {
      setHasStartedPlaying(true)
      setIsLoading(false)
      authRetriedRef.current = false // tocou: uma expiração futura pode renovar de novo
    }

    video.addEventListener('loadedmetadata', handleLoadedMetadata)
    video.addEventListener('timeupdate', handleTimeUpdate)
    video.addEventListener('ended', handleEnded)
    video.addEventListener('error', handleError)
    video.addEventListener('loadstart', handleLoadStart)
    video.addEventListener('canplay', handleCanPlay)
    video.addEventListener('loadeddata', handleLoadedData)
    video.addEventListener('waiting', handleWaiting)
    video.addEventListener('playing', handlePlaying)
    video.addEventListener('pause', handlePause)
    video.addEventListener('play', handlePlay)

    return () => {
      video.removeEventListener('loadedmetadata', handleLoadedMetadata)
      video.removeEventListener('timeupdate', handleTimeUpdate)
      video.removeEventListener('ended', handleEnded)
      video.removeEventListener('error', handleError)
      video.removeEventListener('loadstart', handleLoadStart)
      video.removeEventListener('canplay', handleCanPlay)
      video.removeEventListener('loadeddata', handleLoadedData)
      video.removeEventListener('waiting', handleWaiting)
      video.removeEventListener('playing', handlePlaying)
      video.removeEventListener('pause', handlePause)
      video.removeEventListener('play', handlePlay)
    }
  }, [src, hasStartedPlaying])

  const togglePlay = () => {
    const video = videoRef.current
    if (!video) return

    // estado lido do <video> (o atalho de teclado chama isto de um listener criado num render antigo)
    if (video.paused && !pendingPlayRef.current) {
      void video.play().catch(() => {})
      setIsPlaying(true)
    } else {
      pendingPlayRef.current = false
      video.pause()
      setIsPlaying(false)
    }
  }

  const toggleMute = () => {
    const video = videoRef.current
    if (!video) return

    video.muted = !isMuted
    setIsMuted(!isMuted)
  }

  const handleVolumeChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const video = videoRef.current
    if (!video) return

    const newVolume = parseFloat(e.target.value)
    video.volume = newVolume
    setVolume(newVolume)
    setIsMuted(newVolume === 0)
  }

  // Recomeça no instante t (segundos do arquivo), opcionalmente trocando modo mínimo/faixa de áudio.
  // direct: só reposiciona o <video>. remux: o vídeo copiado só começa em keyframe, então o stream
  // parte do keyframe anterior e o restante é avançado no próprio <video>. transcode: começa exato em t.
  const restartAt = async (t: number, play: boolean, reload = false, next?: { minMode?: PlaybackMode; audioSel?: number | null; audioDelay?: number }) => {
    const video = videoRef.current
    if (!video) return
    const seq = ++seekSeqRef.current
    const newMin = next?.minMode ?? minModeRef.current
    const newAudio = next && 'audioSel' in next ? next.audioSel ?? null : audioSelRef.current
    const newDelay = next?.audioDelay ?? streamDelayRef.current
    const newMode = effectiveMode(newMin, newAudio, newDelay)
    lastTimeRef.current = t
    setCurrentTime(t)

    if (!reload && newMode === 'direct' && modeRef.current === 'direct') {
      video.currentTime = t
      return
    }

    let start = 0
    if (newMode === 'transcode') {
      start = t
    } else if (newMode === 'remux' && t > 0) {
      start = await api.getKeyframe(file.path, t).catch(() => t)
      if (seq !== seekSeqRef.current) return // outro seek chegou antes
    }
    // URL lida agora (não a do render): pega o token renovado
    streamUrlRef.current = { path: file.path, url: buildStreamUrlSync(file.path) }
    const newSrc = buildPlaybackUrl(streamUrlRef.current.url, newMode, newAudio, start, newDelay)
    const newBase = streamBase(newMode, start, newDelay)
    // processado: sempre pula o silêncio inicial (streamLead); direct: só se não for o começo
    startAtRef.current = newMode !== 'direct' || t - newBase > 0.05 ? t - newBase : null
    pendingPlayRef.current = play
    setIsLoading(true)
    offsetRef.current = newBase
    minModeRef.current = newMin
    audioSelRef.current = newAudio
    streamDelayRef.current = newDelay
    setStreamDelay(newDelay)
    setOffset(start)
    setMinMode(newMin)
    setAudioSel(newAudio)
    // mesma URL (ex.: tentar de novo): o React não troca o src, então recarrega na mão
    if (newSrc === src) video.load()
  }
  const restartRef = useRef(restartAt)
  restartRef.current = restartAt

  // Falhou no modo atual (erro ou falha silenciosa): sobe um degrau direct → remux → transcode.
  // Antes confere se o servidor está de pé; se não, é queda de conexão e o reconnect retoma depois.
  // decodeFailure: o stream chegou mas o navegador não decodifica (sem som/imagem), então o token não tem culpa.
  const fallback = async (why: string, decodeFailure = false) => {
    const next = nextMode(modeRef.current)
    const resume = { time: lastTimeRef.current, wasPlaying: isPlayingRef.current || pendingPlayRef.current }
    const serverUp = await api.getHealth().then(() => true, () => false)
    // O token vai na URL do <video> e não passa pelo refresh do axios: se expirou,
    // renova uma vez e recarrega no mesmo modo antes de descer de degrau.
    if (serverUp && !decodeFailure && !authRetriedRef.current) {
      authRetriedRef.current = true
      const path = file.path
      const refreshed = await api.refreshToken().catch(() => false)
      if (currentPathRef.current !== path) return // trocou de vídeo enquanto renovava
      if (refreshed) {
        setAuthSeq((n) => n + 1)
        await restartRef.current(resume.time, resume.wasPlaying, true)
        return
      }
    }
    if (!next || !serverUp) {
      resumeRef.current = resume
      setIsLoading(false)
      setHasError(true)
      toast.error(serverUp ? 'Este vídeo não pôde ser reproduzido' : 'Failed to load video')
      return
    }
    if (import.meta.env.DEV) console.info(`[player] ${modeRef.current} falhou (${why}), tentando ${next}`)
    toast.info(`${modeLabel(next)} para reproduzir no navegador`)
    await restartRef.current(resume.time, resume.wasPlaying, true, { minMode: next })
  }
  const fallbackRef = useRef(fallback)
  fallbackRef.current = fallback

  // Vai para um instante (segundos do arquivo). Fora do modo direct isso pede outro stream ao servidor.
  const seekTo = useCallback((target: number) => {
    const video = videoRef.current
    if (!video) return
    const max = durationRef.current || video.duration || target
    const t = Math.max(0, Math.min(target, max))
    void restartRef.current(t, isPlayingRef.current)
  }, [])

  const handleSeek = (e: React.ChangeEvent<HTMLInputElement>) => {
    const newTime = parseFloat(e.target.value)
    if (modeRef.current !== 'direct') {
      // arrastando: só move a barra; o stream novo é pedido ao soltar
      scrubbingRef.current = true
      scrubValueRef.current = newTime
      setCurrentTime(newTime)
      return
    }
    seekTo(newTime)
  }

  const commitScrub = () => {
    if (!scrubbingRef.current) return
    scrubbingRef.current = false
    seekTo(scrubValueRef.current)
  }

  // Troca de faixa de áudio mantendo a posição e o estado de reprodução
  const selectAudio = (track: AudioTrack) => {
    setMenu(null)
    const mi = infoRef.current
    if (!mi) return
    const native = defaultAudio(mi)
    // faixa nativa e tocável volta para o modo do arquivo; qualquer outra exige remux
    const sel = native && track.order === native.order && track.playable ? null : track.order
    void restartAt(lastTimeRef.current, isPlayingRef.current, true, { audioSel: sel })
    saveTrackPref('audio', track.lang)
    rememberTracks({ audioTrack: encodeAudioPref(track) })
  }

  // Grava a escolha de faixa na hora (vale para o próximo episódio mesmo fechando logo em seguida)
  const rememberTracks = (choice: TrackChoice) => {
    trackChoiceRef.current = { ...trackChoiceRef.current, ...choice }
    progressRef.current.report(lastTimeRef.current, durationRef.current, { force: true, tracks: trackChoiceRef.current })
  }

  const showOsd = (text: string) => {
    setOsd(text)
    if (osdTimerRef.current) clearTimeout(osdTimerRef.current)
    osdTimerRef.current = setTimeout(() => setOsd(null), 1800)
  }

  // Ajuste manual de sincronia. Legenda: aplicada na hora. Áudio: o servidor gera o stream com o som
  // deslocado (?adelay=), então espera o usuário parar de ajustar e recarrega da posição atual.
  const changeSync = (kind: SyncKind, ms: number) => {
    const next = { ...syncRef.current, [kind]: clampDelay(kind, ms) }
    syncRef.current = next
    setSync(next)
    saveSync(file.path, next)
    showOsd(describeDelay(kind, next[kind]))
    if (kind !== 'audio') return
    if (audioDelayTimerRef.current) clearTimeout(audioDelayTimerRef.current)
    audioDelayTimerRef.current = setTimeout(() => {
      const want = syncRef.current.audio
      if (want === streamDelayRef.current) return
      void restartRef.current(lastTimeRef.current, isPlayingRef.current || pendingPlayRef.current, true, { audioDelay: want })
    }, 600)
  }
  const changeSyncRef = useRef(changeSync)
  changeSyncRef.current = changeSync

  const selectSubtitle = (track: SubtitleTrack | null) => {
    setMenu(null)
    setSubId(track ? track.id : null)
    const pref = track ? track.lang || 'off' : 'off'
    saveTrackPref('sub', pref)
    rememberTracks({ subtitleTrack: pref })
  }

  // Tecla C: desliga a legenda ou religa a última (sem ela, a do idioma preferido ou a primeira)
  const toggleSubtitles = () => {
    const mi = infoRef.current
    if (!mi) return
    if (subId) {
      lastSubIdRef.current = subId
      selectSubtitle(null)
      showOsd('Legenda desligada')
      return
    }
    const usable = mi.subtitles.filter((s) => s.supported)
    const track = usable.find((s) => s.id === lastSubIdRef.current)
      ?? pickSubtitleByLang(mi, loadTrackPrefs().sub)
      ?? pickSubtitleByLang(mi, 'por')
      ?? usable[0]
    if (!track) {
      showOsd('Sem legendas')
      return
    }
    selectSubtitle(track)
    showOsd(`Legenda: ${subtitleLabel(track)}`)
  }

  const toggleFullscreen = async () => {
    try {
      if (!document.fullscreenElement) {
        // Request fullscreen on the video container (better than Card)
        const element = videoContainerRef.current || containerRef.current
        if (element) {
          // Try standard API first
          if (element.requestFullscreen) {
            await element.requestFullscreen()
          } else {
            const el = element as VendorFullscreenElement
            // Safari, Firefox antigo, IE/Edge
            await (el.webkitRequestFullscreen ?? el.mozRequestFullScreen ?? el.msRequestFullscreen)?.call(el)
          }
        }
      } else {
        // Exit fullscreen
        if (document.exitFullscreen) {
          await document.exitFullscreen()
        } else {
          const doc = document as VendorFullscreenDocument
          await (doc.webkitExitFullscreen ?? doc.mozCancelFullScreen ?? doc.msExitFullscreen)?.call(doc)
        }
      }
    } catch (error) {
      if (import.meta.env.DEV) {
        console.error('Error toggling fullscreen:', error)
      }
      toast.error('Não foi possível entrar em tela cheia')
    }
  }


  const handleDownload = () => {
    const downloadUrl = buildDownloadUrlSync(file.path)
    saveAs(downloadUrl, file.name)
  }

  const formatTime = (time: number) => {
    const minutes = Math.floor(time / 60)
    const seconds = Math.floor(time % 60)
    return `${minutes}:${seconds.toString().padStart(2, '0')}`
  }

  const handleBackdropClick = (e: React.MouseEvent<HTMLDivElement>) => {
    // Close if clicking on the backdrop (not on the card content)
    if (e.target === e.currentTarget) {
      onClose()
    }
  }

  // Show controls when mouse moves, hide after 3 seconds
  const showControlsTemporarily = useCallback(() => {
    setShowControls(true)
    if (controlsTimeoutRef.current) {
      clearTimeout(controlsTimeoutRef.current)
    }
    controlsTimeoutRef.current = setTimeout(() => {
      setShowControls(false)
    }, 3000)
  }, [])

  // Handle mouse movement to show controls. Listens on the whole modal
  // (not just the video area) because the header floats on top as a sibling
  // element - movement over it wouldn't bubble into a video-scoped listener,
  // which made the header (and its close button) hide mid-hover.
  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const handleMouseMove = () => {
      showControlsTemporarily()
    }

    const handleMouseLeave = () => {
      setShowControls(false)
      if (controlsTimeoutRef.current) {
        clearTimeout(controlsTimeoutRef.current)
      }
    }

    container.addEventListener('mousemove', handleMouseMove)
    const canHover = window.matchMedia('(hover: hover)').matches
    if (canHover) {
      container.addEventListener('mouseleave', handleMouseLeave)
    }

    return () => {
      container.removeEventListener('mousemove', handleMouseMove)
      container.removeEventListener('mouseleave', handleMouseLeave)
      if (controlsTimeoutRef.current) {
        clearTimeout(controlsTimeoutRef.current)
      }
    }
  }, [showControlsTemporarily])

  useEffect(() => {
    if (!showControls) setMenu(null)
  }, [showControls])

  // Atalhos: lidos de um ref para o listener (criado uma vez) sempre chamar a versão atual
  const keyActionsRef = useRef({ togglePlay, toggleMute, toggleFullscreen, toggleSubtitles })
  keyActionsRef.current = { togglePlay, toggleMute, toggleFullscreen, toggleSubtitles }

  // Teclado: espaço play-pause, F tela cheia, C legenda, M mudo, setas busca/volume, Esc fecha.
  // Valem com o foco num botão do player (o espaço não "clica" o botão); só não valem digitando.
  useEffect(() => {
    let swallowSpaceUp = false
    const handleKeyDown = (e: KeyboardEvent) => {
      // Escape always closes the modal, regardless of focus
      if (e.key === 'Escape') {
        if (document.fullscreenElement) return // o navegador sai da tela cheia
        e.preventDefault()
        onClose()
        return
      }
      const el = document.activeElement as HTMLElement | null
      const typing = !!el && (el.isContentEditable || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT' ||
        (el.tagName === 'INPUT' && !['range', 'checkbox', 'radio', 'button'].includes((el as HTMLInputElement).type)))
      if (typing || e.ctrlKey || e.metaKey || e.altKey) return

      const video = videoRef.current
      if (!video) return

      const sk = syncKey(e.key)
      if (sk) {
        e.preventDefault()
        changeSyncRef.current(sk.kind, syncRef.current[sk.kind] + sk.dir * SYNC_STEP_MS)
        showControlsTemporarily()
        return
      }

      const act = keyActionsRef.current
      switch (e.key) {
        case ' ':
          e.preventDefault()
          swallowSpaceUp = true
          if (!e.repeat) act.togglePlay()
          break
        case 'f':
        case 'F':
          e.preventDefault()
          if (!e.repeat) void act.toggleFullscreen()
          break
        case 'c':
        case 'C':
          e.preventDefault()
          if (!e.repeat) act.toggleSubtitles()
          break
        case 'm':
        case 'M':
          e.preventDefault()
          if (!e.repeat) act.toggleMute()
          break
        case 'ArrowUp':
        case 'ArrowDown': {
          e.preventDefault()
          const v = Math.min(1, Math.max(0, video.volume + (e.key === 'ArrowUp' ? 0.1 : -0.1)))
          video.volume = v
          video.muted = v === 0
          setVolume(v)
          setIsMuted(v === 0)
          break
        }
        case 'ArrowLeft':
        case 'ArrowRight':
          e.preventDefault()
          seekTo(lastTimeRef.current + (e.key === 'ArrowRight' ? 5 : -5))
          break
        default:
          return
      }
      showControlsTemporarily()
    }
    // botão focado dispara o clique no keyup do espaço: engole esse keyup
    const handleKeyUp = (e: KeyboardEvent) => {
      if (e.key === ' ' && swallowSpaceUp) {
        swallowSpaceUp = false
        e.preventDefault()
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    document.addEventListener('keyup', handleKeyUp)
    return () => {
      document.removeEventListener('keydown', handleKeyDown)
      document.removeEventListener('keyup', handleKeyUp)
    }
  }, [onClose, showControlsTemporarily, seekTo])

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-50 flex flex-col overflow-hidden bg-black/90"
      role="presentation" // teclado: atalhos globais no document (Esc fecha)
      onClick={handleBackdropClick}
      onTouchStart={(e) => {
        showControlsTemporarily()
        const target = e.target as HTMLElement
        if (target.closest('input, button')) {
          swipeStartXRef.current = null
          return
        }
        if (e.touches.length === 1) {
          swipeStartXRef.current = e.touches[0].clientX
        }
      }}
      onTouchEnd={(e) => {
        if (swipeStartXRef.current === null) {
          return
        }
        const endX = e.changedTouches[0]?.clientX
        const startX = swipeStartXRef.current
        swipeStartXRef.current = null
        if (endX === undefined) {
          return
        }
        const dx = endX - startX
        if (Math.abs(dx) < 60) {
          return
        }
        if (dx < 0 && canGoNext) {
          handleNext()
        } else if (dx > 0 && canGoPrevious) {
          handlePrevious()
        }
      }}
    >
      {/* Header - floats over the video, fades with the other controls */}
      <div
        className={`absolute inset-x-0 top-0 z-20 flex items-center justify-between gap-2 bg-gradient-to-b from-black/70 to-transparent p-2 pt-[max(0.5rem,env(safe-area-inset-top))] transition-opacity duration-300 sm:p-4 ${
          showControls ? 'opacity-100' : 'opacity-0 pointer-events-none'
        }`}
      >
        <h3 className="min-w-0 flex-1 truncate text-sm font-semibold text-white sm:text-lg">{file.name}</h3>
        <div className="flex items-center gap-1 sm:gap-2">
          <Button variant="ghost" size="icon" className="text-white hover:bg-white/20 hover:text-white" aria-label="Baixar" onClick={handleDownload}>
            <Download className="w-4 h-4" />
          </Button>
          <Button variant="ghost" size="icon" className="text-white hover:bg-white/20 hover:text-white" aria-label="Fechar" onClick={onClose}>
            <X className="w-4 h-4" />
          </Button>
        </div>
      </div>

      {/* Video */}
      <div
        ref={videoContainerRef}
        className="relative flex flex-1 items-center justify-center overflow-hidden"
        role="presentation"
        onClick={handleBackdropClick}
      >
            {/* Navigation Buttons - Only show when controls are visible */}
            {canGoPrevious && showControls && (
              <Button
                variant="ghost"
                size="lg"
                className="absolute left-4 top-1/2 -translate-y-1/2 z-10 bg-black/40 hover:bg-black/60 text-white border-0 shadow-lg backdrop-blur-sm transition-all duration-300 p-3"
                onClick={handlePrevious}
                aria-label="Anterior"
              >
                <ChevronLeft className="w-6 h-6" />
              </Button>
            )}
            {canGoNext && showControls && (
              <Button
                variant="ghost"
                size="lg"
                className="absolute right-4 top-1/2 -translate-y-1/2 z-10 bg-black/40 hover:bg-black/60 text-white border-0 shadow-lg backdrop-blur-sm transition-all duration-300 p-3"
                onClick={handleNext}
                aria-label="Próximo"
              >
                <ChevronRight className="w-6 h-6" />
              </Button>
            )}

            {isLoading && !hasError && !broken && (
              <div className="absolute inset-0 flex items-center justify-center z-10 bg-black/50">
                <div className="flex flex-col items-center gap-4">
                  <Loader2 className="w-12 h-12 animate-spin text-white" />
                  <p className="text-white text-sm">Carregando vídeo...</p>
                </div>
              </div>
            )}
            
            {broken && (
              <div className="absolute inset-0 z-20 flex items-center justify-center bg-black/80 p-6" data-testid="broken-media">
                <div className="flex max-w-md flex-col items-center gap-4 text-center text-white">
                  <FileWarning className="h-12 w-12 text-amber-400" />
                  <p className="text-xl font-semibold">Arquivo corrompido ou incompleto</p>
                  <p className="text-sm text-white/70">
                    O servidor não conseguiu abrir este vídeo, então ele não vai tocar. Baixe de novo ou troque o arquivo.
                  </p>
                  <p className="break-words rounded bg-white/10 px-3 py-1.5 font-mono text-xs text-white/60">{broken}</p>
                  <div className="flex flex-wrap justify-center gap-2.5 pt-1">
                    {canGoNext && (
                      <Button onClick={handleNext} className="gap-1.5"><ChevronRight className="h-4 w-4" />Próximo</Button>
                    )}
                    <Button variant="outline" className="border-white/30 bg-transparent text-white hover:bg-white/10 hover:text-white" onClick={() => setBroken(null)}>
                      Tentar mesmo assim
                    </Button>
                  </div>
                </div>
              </div>
            )}

            {hasError && (
              <div className="absolute inset-0 flex items-center justify-center z-10 bg-black/50">
                <div className="flex flex-col items-center gap-4">
                  <p className="text-white text-sm">Erro ao carregar vídeo</p>
                  <Button variant="outline" onClick={() => {
                    setHasError(false)
                    void restartAt(lastTimeRef.current, false, true)
                  }}>
                    Tentar novamente
                  </Button>
                </div>
              </div>
            )}
            
            {/* legendas são desenhadas pelo player (VTT/ASS/PGS), não por <track> */}
            {/* eslint-disable-next-line jsx-a11y/media-has-caption */}
            <video
              ref={videoElRef}
              src={tracksReady && !broken ? src : undefined}
              data-playback-mode={mode}
              data-time-base={base}
              className="h-full w-full object-contain"
              // Chrome no Android tira o vídeo da composição (overlay de hardware) quando nada
              // translúcido o cobre — com os controles escondidos — e o canvas da legenda
              // passa a ser composto como preto opaco por cima. Opacidade < 1 impede a promoção.
              style={subId && (subKind === 'ass' || subKind === 'pgs') ? { opacity: 0.999 } : undefined}
              onClick={(e) => {
                e.stopPropagation()
                togglePlay()
              }}
              controls={false}
              preload="metadata"
            />
            
            {subLoading && (
              <div
                role="status"
                className={`pointer-events-none absolute left-1/2 z-20 flex max-w-[calc(100%-2rem)] -translate-x-1/2 items-center gap-2 rounded-full bg-black/70 px-3.5 py-1.5 text-sm text-white transition-all duration-300 ${showControls ? 'bottom-28' : 'bottom-8'}`}
              >
                <Loader2 className="h-4 w-4 shrink-0 animate-spin" />
                {info?.subtitles.find((t) => t.id === subId)?.ready === false
                  ? 'Extraindo legenda do arquivo… (arquivos grandes levam alguns minutos)'
                  : 'Carregando legenda…'}
              </div>
            )}

            {(() => {
              const intro = info?.intro
              const t = currentTime
              const target = intro
                ? (t >= intro.start && t < intro.end - 2 ? intro.end : null)
                : !info?.chapters?.length && canGoNext && duration > 600 && t > 3 && t < 180 ? t + INTRO_GUESS_S : null
              if (target === null || broken || upNext) return null
              return (
                <button
                  type="button"
                  onClick={(e) => { e.stopPropagation(); seekTo(target) }}
                  className={`absolute right-6 z-20 flex items-center gap-2 rounded-lg border border-white/40 bg-black/70 px-4 py-2.5 text-sm font-semibold text-white backdrop-blur hover:bg-white hover:text-black ${showControls ? 'bottom-28' : 'bottom-10'}`}
                >
                  <SkipForward className="h-4 w-4" />
                  {intro ? 'Pular abertura' : `Pular ${INTRO_GUESS_S} s`}
                </button>
              )
            })()}

            {upNext && files[upNext.index] && (
              <div className="absolute bottom-28 right-6 z-30 w-[min(340px,calc(100%-3rem))] rounded-xl border border-white/15 bg-black/85 p-4 text-white shadow-2xl backdrop-blur" role="alertdialog" aria-label="Próximo episódio">
                <p className="text-xs uppercase tracking-wide text-white/60">Próximo episódio em {upNext.left} s</p>
                <p className="mt-1 line-clamp-2 text-[15px] font-semibold" title={files[upNext.index].name}>{files[upNext.index].name}</p>
                <div className="mt-3 h-1 overflow-hidden rounded-full bg-white/15">
                  <div className="h-full bg-white transition-[width] duration-1000 ease-linear" style={{ width: `${((UP_NEXT_SECONDS - upNext.left) / UP_NEXT_SECONDS) * 100}%` }} />
                </div>
                <div className="mt-3 flex gap-2">
                  <Button size="sm" className="flex-1 gap-1.5" onClick={() => playNext(upNext.index)}><Play className="h-4 w-4 fill-current" />Assistir agora</Button>
                  <Button size="sm" variant="outline" className="border-white/30 bg-transparent text-white hover:bg-white/10 hover:text-white" onClick={() => setUpNext(null)}>Cancelar</Button>
                </div>
              </div>
            )}

            {resumeHint !== null && (
              <div
                data-testid="resume-hint"
                className="absolute left-3 top-[calc(3.5rem+env(safe-area-inset-top))] z-20 flex items-center gap-2 rounded-full bg-black/70 py-1 pl-3 pr-1 text-xs text-white backdrop-blur sm:left-6 sm:text-sm"
              >
                <span>Retomando de {formatDuration(resumeHint)}</span>
                <button
                  type="button"
                  className="rounded-full px-2 py-0.5 font-semibold hover:bg-white/15"
                  onClick={(e) => {
                    e.stopPropagation()
                    setResumeHint(null)
                    void restartRef.current(0, isPlayingRef.current || pendingPlayRef.current)
                  }}
                >
                  Do início
                </button>
              </div>
            )}

            {osd && (
              <div
                data-testid="sync-osd"
                className="pointer-events-none absolute left-1/2 top-16 z-20 -translate-x-1/2 rounded bg-black/70 px-3 py-1 text-sm text-white"
              >
                {osd}
              </div>
            )}

            {activeText && (
              <div
                className={`pointer-events-none absolute inset-x-0 z-10 flex justify-center px-4 transition-all duration-300 ${
                  showControls ? 'bottom-28' : 'bottom-8'
                }`}
              >
                <p
                  data-testid="subtitle-cue"
                  className="max-w-[90%] whitespace-pre-line rounded bg-black/60 px-3 py-1 text-center text-base leading-snug text-white sm:text-xl"
                  style={{ textShadow: '0 1px 3px #000' }}
                >
                  <CueText text={activeText} />
                </p>
              </div>
            )}

            {/* Controls Overlay - matches the video's own rendered width so it
                doesn't overhang into the backdrop next to a portrait video */}
            <div
              className={`absolute bottom-0 bg-gradient-to-t from-black/60 to-transparent p-4 pb-[max(1rem,env(safe-area-inset-bottom))] transition-all duration-300 ${
                isLoading || !showControls ? 'opacity-0 pointer-events-none' : 'opacity-100'
              }`}
              style={videoBox ? { left: videoBox.left, width: videoBox.width } : { left: 0, right: 0 }}
              role="presentation"
              onMouseMove={showControlsTemporarily}
              onClick={(e) => e.stopPropagation()}
            >
              {/* Progress Bar */}
              <div className="mb-2">
                <input
                  type="range"
                  min="0"
                  max={duration || 0}
                  value={currentTime}
                  onChange={handleSeek}
                  onPointerUp={commitScrub}
                  onKeyUp={commitScrub}
                  onTouchEnd={commitScrub}
                  aria-label="Posição do vídeo"
                  className="touch-range w-full cursor-pointer appearance-none bg-transparent"
                />
              </div>

              {/* Controls */}
              <div className="flex items-center justify-between gap-2">
                <div className="flex min-w-0 items-center gap-2 sm:gap-3">
                  <Button 
                    variant="ghost" 
                    size="icon" 
                    onClick={togglePlay}
                    aria-label={isPlaying ? 'Pausar' : 'Reproduzir'}
                    className="text-white hover:bg-white/20 hover:text-white"
                  >
                    {isPlaying ? <Pause className="w-5 h-5" /> : <Play className="w-5 h-5" />}
                  </Button>
                  
                  <span className="text-white/90 text-xs font-medium sm:text-sm">
                    {formatTime(currentTime)} / {formatTime(duration)}
                  </span>
                  {mode !== 'direct' && (
                    <span className="hidden truncate text-xs text-white/60 sm:inline" title={info?.playback?.reason}>
                      {modeLabel(mode)}
                    </span>
                  )}
                </div>

                <div className="relative flex items-center gap-1 sm:gap-3">
                  {menu && info && (
                    <div
                      role="menu"
                      aria-label={menu === 'audio' ? 'Faixas de áudio' : 'Legendas'}
                      className="absolute bottom-full right-0 mb-2 max-h-64 w-72 max-w-[80vw] overflow-y-auto rounded-md bg-black/90 py-1 text-sm text-white shadow-lg"
                    >
                      {menu === 'audio' && info.audio.map((t) => {
                        const native = defaultAudio(info)
                        const selected = audioSel === null ? t.order === native?.order : t.order === audioSel
                        return (
                          <button
                            key={t.order}
                            role="menuitemradio"
                            aria-checked={selected}
                            className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-white/20"
                            onClick={() => selectAudio(t)}
                          >
                            <Check className={`h-4 w-4 shrink-0 ${selected ? 'opacity-100' : 'opacity-0'}`} />
                            <span className="truncate">{audioLabel(t)}</span>
                          </button>
                        )
                      })}
                      {menu === 'audio' && (
                        <SyncRow label="Atraso do áudio" keys="J/K" value={sync.audio} onChange={(ms) => changeSync('audio', ms)} />
                      )}
                      {menu === 'subs' && (
                        <>
                          <button
                            role="menuitemradio"
                            aria-checked={subId === null}
                            className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-white/20"
                            onClick={() => selectSubtitle(null)}
                          >
                            <Check className={`h-4 w-4 shrink-0 ${subId === null ? 'opacity-100' : 'opacity-0'}`} />
                            Desativada
                          </button>
                          {info.subtitles.map((t) => (
                            <button
                              key={t.id}
                              role="menuitemradio"
                              aria-checked={subId === t.id}
                              disabled={!t.supported}
                              title={t.supported ? undefined : 'Formato de legenda não suportado (ex.: DVD/VobSub)'}
                              className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-white/20 disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent"
                              onClick={() => selectSubtitle(t)}
                            >
                              <Check className={`h-4 w-4 shrink-0 ${subId === t.id ? 'opacity-100' : 'opacity-0'}`} />
                              <span className="truncate">{subtitleLabel(t)}</span>
                              {t.kind === 'text' && !t.external && t.supported && (
                                <span className={`ml-auto shrink-0 text-[11px] ${t.ready ? 'text-emerald-300' : 'text-white/50'}`}
                                  title={t.ready ? 'Já extraída: abre na hora' : 'O servidor está extraindo esta legenda do arquivo'}>
                                  {t.ready ? 'pronta' : 'preparando…'}
                                </span>
                              )}
                            </button>
                          ))}
                          <SyncRow label="Atraso da legenda" keys="G/H" value={sync.sub} onChange={(ms) => changeSync('sub', ms)} />
                        </>
                      )}
                    </div>
                  )}

                  {info && info.audio.length > 0 && (
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => setMenu(menu === 'audio' ? null : 'audio')}
                      aria-label="Faixa de áudio"
                      aria-expanded={menu === 'audio'}
                      className="text-white hover:bg-white/20 hover:text-white"
                    >
                      <AudioLines className="w-5 h-5" />
                    </Button>
                  )}
                  {info && info.subtitles.length > 0 && (
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => setMenu(menu === 'subs' ? null : 'subs')}
                      aria-label="Legendas"
                      aria-expanded={menu === 'subs'}
                      className={`text-white hover:bg-white/20 hover:text-white ${subId ? 'bg-white/20' : ''}`}
                    >
                      <Captions className="w-5 h-5" />
                    </Button>
                  )}
                  <Button 
                    variant="ghost" 
                    size="icon" 
                    onClick={toggleMute}
                    aria-label={isMuted ? 'Ativar som' : 'Silenciar'}
                    className="text-white hover:bg-white/20 hover:text-white"
                  >
                    {isMuted ? <VolumeX className="w-5 h-5" /> : <Volume2 className="w-5 h-5" />}
                  </Button>
                  
                  <input
                    type="range"
                    min="0"
                    max="1"
                    step="0.05"
                    value={volume}
                    onChange={handleVolumeChange}
                    aria-label="Volume"
                    className="touch-range hidden w-20 cursor-pointer appearance-none bg-transparent sm:block sm:w-24"
                  />
                  
                  <Button 
                    variant="ghost" 
                    size="icon" 
                    onClick={toggleFullscreen}
                    aria-label="Tela cheia"
                    className="text-white hover:bg-white/20 hover:text-white"
                  >
                    <Maximize className="w-5 h-5" />
                  </Button>
                </div>
              </div>
            </div>
          </div>
    </div>
  )
}
