import type { AudioTrack, MediaInfo, SubtitleTrack } from '@/types'

const LANG_NAMES: Record<string, string> = {
  por: 'Português', pt: 'Português', eng: 'Inglês', en: 'Inglês', jpn: 'Japonês', ja: 'Japonês',
  spa: 'Espanhol', es: 'Espanhol', fre: 'Francês', fra: 'Francês', ger: 'Alemão', deu: 'Alemão',
  ita: 'Italiano', chi: 'Chinês', zho: 'Chinês', kor: 'Coreano', rus: 'Russo', ara: 'Árabe',
}

export function langName(lang: string): string {
  return LANG_NAMES[lang] ?? (lang ? lang.toUpperCase() : 'Desconhecido')
}

export function audioLabel(t: AudioTrack): string {
  const ch = t.channels >= 6 ? ' 5.1' : ''
  const title = t.title && t.title.toLowerCase() !== langName(t.lang).toLowerCase() ? ` · ${t.title}` : ''
  return `${langName(t.lang)}${title} (${t.codec.toUpperCase()}${ch})`
}

export function subtitleLabel(t: SubtitleTrack): string {
  const extra = t.title && t.title.toLowerCase() !== langName(t.lang).toLowerCase() ? ` · ${t.title}` : ''
  const origin = t.external ? ' · arquivo' : ''
  return `${langName(t.lang)}${extra}${origin}`
}

// Faixa que o arquivo toca por padrão (a marcada como default, senão a primeira)
export function defaultAudio(info: MediaInfo): AudioTrack | undefined {
  return info.audio.find((a) => a.default) ?? info.audio[0]
}

export function pickAudioByLang(info: MediaInfo, lang: string | null): AudioTrack | undefined {
  if (!lang) return undefined
  return info.audio.find((a) => a.lang === lang)
}

// Prefere legenda embutida/externa de texto no idioma, evitando "Signs/Forced" quando há opção completa
export function pickSubtitleByLang(info: MediaInfo, lang: string | null): SubtitleTrack | undefined {
  if (!lang) return undefined
  const matches = info.subtitles.filter((s) => s.supported && s.lang === lang)
  const isPartial = (s: SubtitleTrack) => /sign|forced|song/i.test(s.title)
  return matches.find((s) => !isPartial(s)) ?? matches[0]
}

// Faixa escolhida guardada no progresso (servidor) como "idioma|título": o título separa, por
// exemplo, "PTBR (WEB-DL)" de "ENG" quando o arquivo marca as duas como o mesmo idioma.
export function encodeAudioPref(t: AudioTrack): string {
  return t.title ? `${t.lang}|${t.title}` : t.lang
}

export function pickAudioByPref(info: MediaInfo, pref: string | null | undefined): AudioTrack | undefined {
  if (!pref) return undefined
  const cut = pref.indexOf('|')
  const lang = cut < 0 ? pref : pref.slice(0, cut)
  const title = cut < 0 ? '' : pref.slice(cut + 1)
  if (title) {
    const byTitle = info.audio.filter((a) => a.title === title)
    const hit = byTitle.find((a) => a.lang === lang) ?? byTitle[0]
    if (hit) return hit
  }
  return pickAudioByLang(info, lang)
}

const PREF_KEY_AUDIO = 'player_audio_lang'
const PREF_KEY_SUB = 'player_sub_lang' // 'off' = sem legenda

export function loadTrackPrefs(): { audio: string | null; sub: string | null } {
  try {
    return { audio: localStorage.getItem(PREF_KEY_AUDIO), sub: localStorage.getItem(PREF_KEY_SUB) }
  } catch {
    return { audio: null, sub: null }
  }
}

export function saveTrackPref(kind: 'audio' | 'sub', value: string) {
  try {
    localStorage.setItem(kind === 'audio' ? PREF_KEY_AUDIO : PREF_KEY_SUB, value)
  } catch {
    // sem storage: preferência vale só nesta sessão
  }
}
