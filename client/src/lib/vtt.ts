export interface Cue {
  start: number
  end: number
  text: string
}

function parseTimestamp(raw: string): number | null {
  const m = raw.trim().match(/^(?:(\d+):)?(\d{1,2}):(\d{2})[.,](\d{1,3})$/)
  if (!m) return null
  const [, h, min, sec, ms] = m
  return Number(h ?? 0) * 3600 + Number(min) * 60 + Number(sec) + Number(ms.padEnd(3, '0')) / 1000
}

// WebVTT -> cues. Tags de marcação (<i>, <c.x>, ...) viram texto puro; <i>/<b> são preservados como <i>/<b>.
export function parseVtt(source: string): Cue[] {
  const cues: Cue[] = []
  const blocks = source.replace(/\r\n?/g, '\n').split(/\n{2,}/)
  for (const block of blocks) {
    const lines = block.split('\n')
    const timingIdx = lines.findIndex((l) => l.includes('-->'))
    if (timingIdx === -1) continue
    const [startRaw, rest] = lines[timingIdx].split('-->')
    const start = parseTimestamp(startRaw)
    const end = parseTimestamp(rest.trim().split(/\s+/)[0] ?? '')
    if (start === null || end === null) continue
    const text = lines
      .slice(timingIdx + 1)
      .join('\n')
      .replace(/<(?!\/?[ib]>)[^>]+>/g, '')
      .trim()
    if (text) cues.push({ start, end, text })
  }
  return cues.sort((a, b) => a.start - b.start)
}

export function activeCues(cues: Cue[], time: number): Cue[] {
  return cues.filter((c) => time >= c.start && time < c.end)
}
