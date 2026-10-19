import type { MediaInfo, PlaybackMode } from '@/types'

// Codecs que dependem do navegador/GPU. h264 8-bit, vp8, aac, mp3, opus, vorbis e flac
// tocam em qualquer navegador moderno e o servidor já assume isso.
const CAP_PROBES: Array<[string, string]> = [
  ['h264hi10', 'video/mp4; codecs="avc1.6E0028"'],
  ['hevc', 'video/mp4; codecs="hvc1.1.6.L120.90"'],
  ['hevc10', 'video/mp4; codecs="hvc1.2.4.L120.90"'],
  ['av1', 'video/mp4; codecs="av01.0.08M.08"'],
  ['vp9', 'video/webm; codecs="vp9"'],
  ['mkv', 'video/x-matroska'],
]
// ac3/eac3 ficam de fora de propósito: o Chrome responde canPlayType mas toca mudo

type CanPlay = (type: string) => string

// clientCaps lista o que o navegador diz tocar ("probably"/"maybe"), no formato do ?caps= do servidor
export function clientCaps(canPlay?: CanPlay): string {
  let probe = canPlay
  if (!probe) {
    if (typeof document === 'undefined') return ''
    const v = document.createElement('video')
    probe = (t) => v.canPlayType(t)
  }
  const caps = CAP_PROBES.filter(([, type]) => probe!(type) !== '').map(([name]) => name)
  return caps.join(',') || 'none'
}

// Próximo modo da cadeia de fallback; null = não há mais o que tentar
export function nextMode(mode: PlaybackMode): PlaybackMode | null {
  if (mode === 'direct') return 'remux'
  if (mode === 'remux') return 'transcode'
  return null
}

export function initialMode(info: MediaInfo | null): PlaybackMode {
  return info?.playback?.mode ?? 'direct'
}

const RANK: Record<PlaybackMode, number> = { direct: 0, remux: 1, transcode: 2 }

// Modo usado de fato: o mínimo exigido pelo arquivo/fallback, subindo para remux quando a faixa de
// áudio escolhida não é a que o arquivo toca sozinho ou quando há ajuste manual do áudio (só o
// stream processado consegue deslocar o som em relação à imagem).
export function effectiveMode(minMode: PlaybackMode, audioSel: number | null, audioDelayMs = 0): PlaybackMode {
  if ((audioSel !== null || audioDelayMs !== 0) && RANK[minMode] < RANK.remux) return 'remux'
  return minMode
}

// Segundos de silêncio que o servidor põe no começo de todo stream processado (streamLead em
// server-go/media_playback.go): no stream, tempo = tempo do arquivo - start + STREAM_LEAD.
export const STREAM_LEAD = 2

// Tempo do arquivo no instante 0 do <video>: tempo do arquivo = streamBase + video.currentTime.
// Com ajuste de áudio o servidor desloca o vídeo (o áudio fica fixo), e o relógio segue a imagem.
export function streamBase(mode: PlaybackMode, start: number, audioDelayMs = 0): number {
  if (mode === 'direct') return 0
  return start - STREAM_LEAD + audioDelayMs / 1000
}

// URL do stream: direct = arquivo cru (com Range); remux/transcode = ffmpeg a partir de start
export function buildPlaybackUrl(streamUrl: string, mode: PlaybackMode, audio: number | null, start: number, audioDelayMs = 0): string {
  if (mode === 'direct') return streamUrl
  const params = new URLSearchParams({ mode })
  if (audio !== null) params.set('audio', String(audio))
  if (start > 0) params.set('start', start.toFixed(3))
  if (audioDelayMs !== 0) params.set('adelay', String(Math.round(audioDelayMs)))
  return `${streamUrl}${streamUrl.includes('?') ? '&' : '?'}${params.toString()}`
}

export interface DecodeStats {
  videoWidth: number
  audioBytes?: number // webkitAudioDecodedByteCount (só Chromium)
  playedSeconds: number
}

// Falha silenciosa: o <video> não dispara erro mas não há imagem (mpeg4/wmv no Chrome)
// ou não há som (ac3/eac3/dts no Chrome). Devolve o motivo ou null se está tudo ok.
export function silentFailure(info: MediaInfo | null, s: DecodeStats): 'no-video' | 'no-audio' | null {
  const hasVideo = (info?.videoIndex ?? 0) >= 0 && !!info?.videoCodec
  if (hasVideo && s.videoWidth === 0) return 'no-video'
  const hasAudio = (info?.audio.length ?? 0) > 0
  if (hasAudio && s.audioBytes !== undefined && s.audioBytes === 0 && s.playedSeconds >= 1.5) return 'no-audio'
  return null
}

// Mensagem curta para o usuário sobre o modo atual
export function modeLabel(mode: PlaybackMode): string {
  if (mode === 'remux') return 'Convertendo contêiner/áudio'
  if (mode === 'transcode') return 'Convertendo vídeo'
  return ''
}
