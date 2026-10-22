import { describe, expect, it } from 'vitest'
import type { AudioTrack, MediaInfo } from '@/types'
import { encodeAudioPref, pickAudioByPref } from './tracks'

const track = (order: number, lang: string, title: string): AudioTrack =>
  ({ order, lang, title, codec: 'aac', channels: 2, default: order === 0, playable: true } as AudioTrack)

const info = (audio: AudioTrack[]): MediaInfo => ({ audio, subtitles: [], fonts: [] } as unknown as MediaInfo)

describe('pickAudioByPref', () => {
  const eng = track(0, 'eng', 'ENG')
  const pt = track(1, 'por', 'PTBR (WEB-DL)')

  it('acha pela combinação idioma + título e só pelo idioma', () => {
    expect(pickAudioByPref(info([eng, pt]), encodeAudioPref(pt))).toBe(pt)
    expect(pickAudioByPref(info([eng, pt]), 'por')).toBe(pt)
  })

  it('usa o título quando o arquivo marca a dublagem com o idioma errado', () => {
    const ptAsEng = track(1, 'eng', 'PTBR (WEB-DL)')
    expect(pickAudioByPref(info([eng, ptAsEng]), encodeAudioPref(pt))).toBe(ptAsEng)
  })

  it('cai no idioma quando o título muda entre episódios', () => {
    const ptOther = track(1, 'por', 'PTBR')
    expect(pickAudioByPref(info([eng, ptOther]), encodeAudioPref(pt))).toBe(ptOther)
  })

  it('sem preferência ou sem faixa compatível: nada', () => {
    expect(pickAudioByPref(info([eng, pt]), null)).toBeUndefined()
    expect(pickAudioByPref(info([eng, pt]), 'jpn')).toBeUndefined()
  })
})
