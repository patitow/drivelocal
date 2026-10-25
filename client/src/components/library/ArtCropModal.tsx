// Escolha da arte a partir dos itens: a grade dos arquivos e, no escolhido, uma
// moldura móvel no formato da arte. Imagem: o recorte sai aqui mesmo (canvas) e
// sobe como arquivo. Vídeo: toca, para no quadro desejado e o servidor recorta
// esse quadro nas mesmas coordenadas.
import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { createPortal } from 'react-dom'
import { toast } from 'sonner'
import { ArrowLeft, Check, Loader2, Pause, Play, Video, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import { buildStreamUrlSync } from '@/lib/utils'
import { library, formatDuration, type LibraryItem } from '@/lib/library'
import { errorMessage } from './catalogShared'
import { Still } from './shelves/shelfUi'
import { useReleasingMediaRef } from '@/hooks/useReleasingMediaRef'

// recorte em frações da imagem (0 a 1)
export interface Crop { x: number; y: number; w: number; h: number }

const MAX_W = 1920

// maior moldura no formato `aspect` (largura/altura) que cabe, centrada
export function fitCrop(mediaAspect: number, aspect: number): Crop {
  // h em fração = w * mediaAspect / aspect
  let w = 1
  let h = mediaAspect / aspect
  if (h > 1) { w = aspect / mediaAspect; h = 1 }
  return { x: (1 - w) / 2, y: (1 - h) / 2, w, h }
}

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v))

export function ArtCropModal({ label, items, aspect, onDone, onClose }: {
  label: string
  items: LibraryItem[]
  // largura/altura da arte (foto 1, capa 2/3, banner 16/6)
  aspect: number
  onDone: (url: string) => void
  onClose: () => void
}) {
  const [it, setIt] = useState<LibraryItem | null>(null)
  useEffect(() => {
    const k = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', k)
    return () => window.removeEventListener('keydown', k)
  }, [onClose])

  return createPortal(
    <div className="dark fixed inset-0 z-[100] flex items-center justify-center bg-black/70 p-4">
      {/* clicar fora fecha; pelo teclado, Esc ou o botão Fechar */}
      <button type="button" aria-hidden="true" tabIndex={-1} data-testid="art-crop-backdrop" onClick={onClose} className="absolute inset-0 cursor-default" />
      <div role="dialog" aria-modal="true" aria-label={`${label} dos itens`}
        className="relative flex max-h-full w-full max-w-4xl flex-col overflow-hidden rounded-2xl border bg-popover text-popover-foreground shadow-2xl">
        <header className="flex items-center gap-2 border-b px-4 py-3">
          {it && <button type="button" aria-label="Voltar aos itens" onClick={() => setIt(null)} className="rounded-full p-1.5 hover:bg-secondary"><ArrowLeft className="h-4 w-4" /></button>}
          <h3 className="min-w-0 flex-1 truncate font-semibold">{it ? it.name : `${label}: escolha um item`}</h3>
          <button type="button" aria-label="Fechar" onClick={onClose} className="rounded-full p-1.5 hover:bg-secondary"><X className="h-4 w-4" /></button>
        </header>
        {it
          ? <Cropper key={it.id} it={it} label={label} aspect={aspect} onDone={(u) => { onDone(u); onClose() }} />
          : (
            <ul aria-label={`Itens para ${label.toLowerCase()}`} className="grid grid-cols-3 gap-2 overflow-auto p-4 sm:grid-cols-4 md:grid-cols-5">
              {items.slice(0, 200).map((m) => (
                <li key={m.id}>
                  <button type="button" onClick={() => setIt(m)} title={m.name} aria-label={`Usar ${m.name}`}
                    className="relative block aspect-video w-full overflow-hidden rounded-md bg-muted ring-primary hover:ring-2">
                    <Still path={m.path} className="h-full w-full object-cover" />
                    {m.kind === 'video' && (
                      <span className="absolute bottom-1 left-1 inline-flex items-center gap-1 rounded bg-black/70 px-1.5 py-0.5 text-[10px] text-white">
                        <Video className="h-3 w-3" />{m.durationS ? formatDuration(m.durationS) : 'vídeo'}
                      </span>
                    )}
                  </button>
                </li>
              ))}
            </ul>
          )}
      </div>
    </div>,
    document.body,
  )
}

function Cropper({ it, label, aspect, onDone }: { it: LibraryItem; label: string; aspect: number; onDone: (url: string) => void }) {
  const isVideo = it.kind === 'video'
  const img = useRef<HTMLImageElement>(null)
  const vid = useRef<HTMLVideoElement>(null)
  const vidElRef = useReleasingMediaRef(vid)
  const box = useRef<HTMLDivElement>(null)
  const [media, setMedia] = useState<{ w: number; h: number } | null>(null)
  const [crop, setCrop] = useState<Crop | null>(null)
  const [busy, setBusy] = useState(false)
  // vídeo: tempo, tocando e, se o navegador não toca o formato, o quadro vindo do servidor
  const [t, setT] = useState(0)
  const [dur, setDur] = useState(it.durationS ?? 0)
  const [playing, setPlaying] = useState(false)
  const [fallback, setFallback] = useState<string | null>(null)

  const ready = (w: number, h: number) => {
    if (!w || !h) return
    setMedia({ w, h })
    setCrop((c) => c ?? fitCrop(w / h, aspect))
  }

  // quadro do servidor (vídeo que o navegador não toca)
  const loadFrame = async (at: number) => {
    try {
      const r = await api.request<Blob>({ method: 'GET', url: '/api/admin/library/art/frame', params: { mediaId: it.id, at }, responseType: 'blob' })
      setFallback((old) => { if (old) URL.revokeObjectURL(old); return URL.createObjectURL(r.data) })
    } catch (e) {
      toast.error(errorMessage(e))
    }
  }
  useEffect(() => () => { if (fallback) URL.revokeObjectURL(fallback) }, [fallback])

  // arrastar a moldura (move) ou o canto (redimensiona, mantendo o formato)
  const drag = (e: ReactPointerEvent, mode: 'move' | 'size') => {
    if (!crop || !media || !box.current) return
    e.preventDefault()
    e.stopPropagation()
    const r = box.current.getBoundingClientRect()
    const start = { x: e.clientX, y: e.clientY, c: crop }
    const ratio = (media.w / media.h) / aspect // h = w * ratio
    const onMove = (ev: PointerEvent) => {
      const dx = (ev.clientX - start.x) / r.width
      const dy = (ev.clientY - start.y) / r.height
      const c = start.c
      if (mode === 'move') {
        setCrop({ ...c, x: clamp(c.x + dx, 0, 1 - c.w), y: clamp(c.y + dy, 0, 1 - c.h) })
      } else {
        // o canto segue o maior deslocamento, sem passar da borda
        const maxW = Math.min(1 - c.x, (1 - c.y) / ratio)
        const w = clamp(Math.max(c.w + dx, (c.h + dy) / ratio), 0.05, maxW)
        setCrop({ ...c, w, h: w * ratio })
      }
    }
    const onUp = () => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
    }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
  }

  const save = async () => {
    if (!crop || !media) return
    setBusy(true)
    try {
      if (isVideo) {
        vid.current?.pause()
        const at = vid.current && !fallback ? vid.current.currentTime : t
        onDone((await library.admin.storeArt({ mediaId: it.id, at, crop })).url)
      } else {
        const el = img.current!
        const sw = crop.w * el.naturalWidth
        const sh = crop.h * el.naturalHeight
        const scale = Math.min(1, MAX_W / sw)
        const cv = document.createElement('canvas')
        cv.width = Math.round(sw * scale)
        cv.height = Math.round(sh * scale)
        cv.getContext('2d')!.drawImage(el, crop.x * el.naturalWidth, crop.y * el.naturalHeight, sw, sh, 0, 0, cv.width, cv.height)
        const blob = await new Promise<Blob | null>((ok) => cv.toBlob(ok, 'image/jpeg', 0.92))
        if (!blob) throw new Error('não deu para recortar a imagem')
        onDone((await library.admin.uploadArt(new File([blob], 'arte.jpg', { type: 'image/jpeg' }))).url)
      }
    } catch (e) {
      toast.error(errorMessage(e))
      setBusy(false)
    }
  }

  const src = buildStreamUrlSync(it.path)
  // já medido: ocupa o espaço (até 60% da altura da tela), mesmo se o arquivo for pequeno
  const mediaCls = media ? 'block h-full w-full select-none' : 'block max-h-[60vh] max-w-full select-none'
  return (
    <div className="flex flex-col gap-3 overflow-auto p-4">
      <div className="flex justify-center">
        <div ref={box} className="relative touch-none overflow-hidden"
          style={media ? { width: `min(100%, calc(60vh * ${media.w / media.h}))`, aspectRatio: `${media.w} / ${media.h}` } : undefined}>
          {!isVideo && <img ref={img} src={src} alt={it.name} draggable={false} className={mediaCls}
            onLoad={(e) => ready(e.currentTarget.naturalWidth, e.currentTarget.naturalHeight)}
            onError={() => toast.error('Não deu para abrir a imagem')} />}
          {isVideo && !fallback && <video ref={vidElRef} src={src} muted playsInline preload="metadata" className={mediaCls}
            onLoadedMetadata={(e) => { ready(e.currentTarget.videoWidth, e.currentTarget.videoHeight); if (e.currentTarget.duration) setDur(e.currentTarget.duration); e.currentTarget.currentTime = (it.durationS ?? e.currentTarget.duration ?? 0) * 0.25 }}
            onTimeUpdate={(e) => setT(e.currentTarget.currentTime)}
            onPlay={() => setPlaying(true)} onPause={() => setPlaying(false)}
            onError={() => { const at = (it.durationS ?? 0) * 0.25; setT(at); void loadFrame(at) }} />}
          {isVideo && fallback && <img src={fallback} alt={it.name} draggable={false} className={mediaCls}
            onLoad={(e) => ready(e.currentTarget.naturalWidth, e.currentTarget.naturalHeight)} />}
          {crop && media && (
            <div aria-label="Moldura do recorte" onPointerDown={(e) => drag(e, 'move')}
              className="absolute cursor-move border-2 border-white shadow-[0_0_0_9999px_rgba(0,0,0,0.55)]"
              style={{ left: `${crop.x * 100}%`, top: `${crop.y * 100}%`, width: `${crop.w * 100}%`, height: `${crop.h * 100}%`, borderRadius: aspect === 1 ? '9999px' : undefined }}>
              <span aria-label="Redimensionar moldura" onPointerDown={(e) => drag(e, 'size')}
                className="absolute bottom-0 right-0 h-5 w-5 cursor-nwse-resize rounded-full border-2 border-white bg-primary" />
            </div>
          )}
          {!media && <span className="flex h-48 w-72 items-center justify-center"><Loader2 className="h-6 w-6 animate-spin" /></span>}
        </div>
      </div>
      {isVideo && (
        <div className="flex items-center gap-3">
          {!fallback && (
            <button type="button" aria-label={playing ? 'Pausar' : 'Tocar'} onClick={() => { const v = vid.current; if (v) { if (v.paused) void v.play(); else v.pause() } }}
              className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-foreground text-background">
              {playing ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
            </button>
          )}
          <input type="range" aria-label="Momento do vídeo" min={0} max={Math.max(1, dur)} step={0.04} value={Math.min(t, dur || t)} className="flex-1"
            onChange={(e) => { const v = Number(e.target.value); setT(v); if (vid.current && !fallback) vid.current.currentTime = v }}
            onPointerUp={() => { if (fallback) void loadFrame(t) }} onKeyUp={() => { if (fallback) void loadFrame(t) }} />
          <span className="w-24 shrink-0 text-right text-xs tabular-nums text-muted-foreground">{formatDuration(t)} / {formatDuration(dur)}</span>
        </div>
      )}
      <div className="flex items-center justify-between gap-2">
        <p className="text-xs text-muted-foreground">Arraste a moldura; o canto muda o tamanho.{isVideo ? ' Pare no quadro que quiser.' : ''}</p>
        <Button type="button" disabled={busy || !crop} onClick={() => void save()}>
          {busy ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : <Check className="mr-1 h-4 w-4" />}Usar como {label.toLowerCase()}
        </Button>
      </div>
    </div>
  )
}
