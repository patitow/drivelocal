import { describe, expect, it } from 'vitest'
import { decodeDrivePath, encodeDriveUrl, encodeMediaPath, pathFromBrowserLocation } from './mediaPath'

const specialFolder =
  'Animes/[CaféSub] Example Show [BD+EXTRAS][FLAC][EN]'

describe('encodeMediaPath', () => {
  it('leaves ordinary nested folders readable and round-trips', () => {
    expect(encodeMediaPath('Animes/Naruto')).toBe('Animes/Naruto')
    expect(encodeMediaPath('Torrents/Animes')).toBe('Torrents/Animes')
    expect(encodeMediaPath('Movies/Simple Movie.mp4')).toBe('Movies/Simple%20Movie.mp4')
  })

  it('normalizes Windows separators used by local Go listings', () => {
    expect(encodeMediaPath('Animes\\Naruto\\ep01.mkv')).toBe('Animes/Naruto/ep01.mkv')
  })

  it('keeps slashes and percent-encodes plus, brackets and unicode', () => {
    const encoded = encodeMediaPath(specialFolder)
    expect(encoded).toContain('/')
    expect(encoded).not.toContain('+')
    expect(encoded).toContain('%2B')
    expect(encoded).toContain('%5B')
    expect(encoded).toContain('%C3%A9')
    expect(decodeURIComponent(encoded.split('/')[1])).toBe(
      '[CaféSub] Example Show [BD+EXTRAS][FLAC][EN]'
    )
  })
})

describe('drive URLs', () => {
  it('round-trips ordinary folder names that already worked', () => {
    for (const path of ['Animes', 'Animes/Naruto', 'Recorded Videos/Show Name', 'Torrents/Animes']) {
      expect(decodeDrivePath(encodeDriveUrl(path))).toBe(path)
    }
  })

  it('round-trips special folder names', () => {
    const url = encodeDriveUrl(specialFolder)
    expect(url.startsWith('/drive/Animes/')).toBe(true)
    expect(decodeDrivePath(url)).toBe(specialFolder)
  })

  it('decodes the old whole-path encodeURIComponent format', () => {
    const legacy = `/drive/${encodeURIComponent(specialFolder)}`
    expect(decodeDrivePath(legacy)).toBe(specialFolder)
  })

  it('treats /drive as the media root', () => {
    expect(decodeDrivePath('/drive')).toBe('')
    expect(encodeDriveUrl('')).toBe('/drive')
  })
})

describe('pathFromBrowserLocation', () => {
  it('reads ordinary /drive folders', () => {
    expect(pathFromBrowserLocation('/drive/Animes/Naruto', '')).toBe('Animes/Naruto')
    expect(pathFromBrowserLocation('/drive', '')).toBe('')
  })

  it('reads /drive paths', () => {
    expect(pathFromBrowserLocation(encodeDriveUrl(specialFolder), '')).toBe(specialFolder)
  })

  it('falls back to ?path= for old links', () => {
    expect(pathFromBrowserLocation('/', `?path=${encodeURIComponent(specialFolder)}`)).toBe(
      specialFolder
    )
  })
})
