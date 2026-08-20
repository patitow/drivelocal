const retriedPaths = new Set<string>()

export function consumeThumbnailRetry(path: string): boolean {
  if (retriedPaths.has(path)) {
    return false
  }
  retriedPaths.add(path)
  return true
}
