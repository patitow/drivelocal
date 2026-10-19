import { describe, expect, it } from 'vitest'
import { buildPlaybackUrl, clientCaps, effectiveMode, initialMode, nextMode, silentFailure, STREAM_LEAD, streamBase } from './playback'
import { pickSubtitleByLang } from './tracks'
import type { MediaInfo } from '@/types'

const info = (over: Partial<MediaInfo> = {}): MediaInfo => ({
  duration: 30,
  videoIndex: 0,
  videoCodec: 'h264',
  audio: [{ order: 0, lang: 'jpn', title: '', codec: 'aac', channels: 2, default: true, playable: true }],
  subtitles: [],
  ...over,
})

describe('clientCaps', () => {
  it('lista só o que o navegador declara tocar', () => {
    const chrome = (t: string) => (/avc1|hvc1|av01|vp9|matroska/.test(t) ? 'probably' : '')
    expect(clientCaps(chrome)).toBe('h264hi10,hevc,hevc10,av1,vp9,mkv')
    const firefox = (t: string) => (/av01|vp9/.test(t) ? 'maybe' : '')
    expect(clientCaps(firefox)).toBe('av1,vp9')
  })

  it('manda "none" quando nada extra é suportado (não cai nas caps padrão do servidor)', () => {
    expect(clientCaps(() => '')).toBe('none')
  })
})

describe('cadeia de fallback', () => {
  it('direct → remux → transcode → fim', () => {
    expect(nextMode('direct')).toBe('remux')
    expect(nextMode('remux')).toBe('transcode')
    expect(nextMode('transcode')).toBeNull()
  })

  it('modo inicial vem do servidor, direct se ausente', () => {
    expect(initialMode(null)).toBe('direct')
    expect(initialMode(info({ playback: { mode: 'transcode', reason: 'hevc 10-bit' } }))).toBe('transcode')
  })

  it('faixa de áudio escolhida força ao menos remux, sem rebaixar transcode', () => {
    expect(effectiveMode('direct', null)).toBe('direct')
    expect(effectiveMode('direct', 1)).toBe('remux')
    expect(effectiveMode('remux', null)).toBe('remux')
    expect(effectiveMode('transcode', 1)).toBe('transcode')
  })
})

describe('buildPlaybackUrl', () => {
  const base = 'http://h/api/stream/a.mkv?token=t'
  it('direct usa o arquivo cru', () => {
    expect(buildPlaybackUrl(base, 'direct', 1, 10)).toBe(base)
  })
  it('remux/transcode levam modo, faixa e início', () => {
    expect(buildPlaybackUrl(base, 'remux', 1, 12.3456)).toBe(`${base}&mode=remux&audio=1&start=12.346`)
    expect(buildPlaybackUrl('http://h/s', 'transcode', null, 0)).toBe('http://h/s?mode=transcode')
  })
  it('ajuste de áudio vai como adelay em ms (só em stream processado)', () => {
    expect(buildPlaybackUrl(base, 'remux', null, 4, -250)).toBe(`${base}&mode=remux&start=4.000&adelay=-250`)
    expect(buildPlaybackUrl(base, 'remux', null, 4, 0)).toBe(`${base}&mode=remux&start=4.000`)
    expect(buildPlaybackUrl(base, 'direct', null, 4, 300)).toBe(base)
  })
})

describe('relógio do stream (offset)', () => {
  it('direct: o tempo do <video> já é o do arquivo', () => {
    expect(streamBase('direct', 12, 300)).toBe(0)
  })
  it('processado: start - STREAM_LEAD, mais o ajuste de áudio', () => {
    expect(STREAM_LEAD).toBe(2)
    expect(streamBase('remux', 0)).toBe(-2)
    expect(streamBase('transcode', 15.3)).toBeCloseTo(13.3, 9)
    // áudio +250 ms: o servidor adianta o vídeo 250 ms, o relógio acompanha a imagem
    expect(streamBase('remux', 10, 250)).toBeCloseTo(8.25, 9)
    expect(streamBase('remux', 10, -400)).toBeCloseTo(7.6, 9)
  })
  it('ajuste de áudio força stream processado', () => {
    expect(effectiveMode('direct', null, 100)).toBe('remux')
    expect(effectiveMode('direct', null, 0)).toBe('direct')
    expect(effectiveMode('transcode', null, -100)).toBe('transcode')
  })
})

describe('silentFailure', () => {
  it('sem imagem quando há vídeo', () => {
    expect(silentFailure(info(), { videoWidth: 0, playedSeconds: 0 })).toBe('no-video')
    expect(silentFailure(info({ videoIndex: -1, videoCodec: '' }), { videoWidth: 0, playedSeconds: 0 })).toBeNull()
  })
  it('sem áudio só depois de tocar um pouco', () => {
    expect(silentFailure(info(), { videoWidth: 320, audioBytes: 0, playedSeconds: 0.5 })).toBeNull()
    expect(silentFailure(info(), { videoWidth: 320, audioBytes: 0, playedSeconds: 2 })).toBe('no-audio')
    expect(silentFailure(info(), { videoWidth: 320, audioBytes: 4096, playedSeconds: 2 })).toBeNull()
    expect(silentFailure(info({ audio: [] }), { videoWidth: 320, audioBytes: 0, playedSeconds: 2 })).toBeNull()
    // navegador sem o contador (Firefox/Safari): não dá para afirmar nada
    expect(silentFailure(info(), { videoWidth: 320, playedSeconds: 2 })).toBeNull()
  })
})

describe('preferência de legenda com ASS/PGS', () => {
  it('escolhe ASS e PGS suportados no idioma, evitando Signs', () => {
    const mi = info({
      subtitles: [
        { id: 'emb:3', lang: 'por', title: 'Signs', codec: 'ass', external: false, default: false, supported: true, kind: 'ass' },
        { id: 'emb:4', lang: 'por', title: 'Full', codec: 'ass', external: false, default: false, supported: true, kind: 'ass' },
        { id: 'emb:5', lang: 'eng', title: '', codec: 'hdmv_pgs_subtitle', external: false, default: false, supported: true, kind: 'pgs' },
        { id: 'emb:6', lang: 'spa', title: '', codec: 'dvd_subtitle', external: false, default: false, supported: false, kind: '' },
      ],
    })
    expect(pickSubtitleByLang(mi, 'por')?.id).toBe('emb:4')
    expect(pickSubtitleByLang(mi, 'eng')?.id).toBe('emb:5')
    expect(pickSubtitleByLang(mi, 'spa')).toBeUndefined()
  })
})
