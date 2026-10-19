import { describe, expect, it } from 'vitest'
import { activeCues, parseVtt } from './vtt'

const SAMPLE = `WEBVTT

1
00:00:01.000 --> 00:00:03.500 align:middle
Olá <i>mundo</i>

00:01:02.250 --> 00:01:04.000
<c.yellow>Linha 1</c.yellow>
Linha 2
`

describe('parseVtt', () => {
  it('parses timings and keeps only i/b markup', () => {
    const cues = parseVtt(SAMPLE)
    expect(cues).toEqual([
      { start: 1, end: 3.5, text: 'Olá <i>mundo</i>' },
      { start: 62.25, end: 64, text: 'Linha 1\nLinha 2' },
    ])
  })

  it('handles hours, CRLF and ignores blocks without timing', () => {
    const cues = parseVtt('WEBVTT\r\n\r\nNOTE x\r\n\r\n01:00:00.000 --> 01:00:01.000\r\nA')
    expect(cues).toEqual([{ start: 3600, end: 3601, text: 'A' }])
  })
})

describe('activeCues', () => {
  it('returns cues covering the time, end exclusive', () => {
    const cues = parseVtt(SAMPLE)
    expect(activeCues(cues, 2)).toHaveLength(1)
    expect(activeCues(cues, 3.5)).toHaveLength(0)
    expect(activeCues(cues, 63)[0].text).toContain('Linha 2')
  })
})
