/** Encode each path segment so '+', '[]', unicode and spaces survive URLs. */
export function encodeMediaPath(filePath: string): string {
  return filePath
    .replace(/\\/g, '/')
    .split('/')
    .map((segment) => encodeURIComponent(segment))
    .join('/')
}

export function encodeDriveUrl(filePath: string): string {
  const encoded = encodeMediaPath(filePath)
  return encoded && encoded !== '' ? `/drive/${encoded}` : '/drive'
}

export function decodeDrivePath(pathname: string): string {
  const match = pathname.match(/^\/drive(\/.*)?$/)
  if (!match?.[1] || match[1] === '/') {
    return ''
  }
  return match[1]
    .slice(1)
    .split('/')
    .map((segment) => {
      try {
        return decodeURIComponent(segment)
      } catch {
        return segment
      }
    })
    .join('/')
}

export function pathFromBrowserLocation(pathname: string, search: string): string {
  if (/^\/drive(\/.*)?$/.test(pathname)) {
    return decodeDrivePath(pathname)
  }
  try {
    return new URLSearchParams(search).get('path') || ''
  } catch {
    return ''
  }
}
