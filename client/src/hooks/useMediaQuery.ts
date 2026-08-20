import { useEffect, useState } from 'react'

const COMPACT_QUERY = '(max-width: 1023px)'

export function useMediaQuery(query: string) {
  const [matches, setMatches] = useState(() => {
    if (typeof window === 'undefined') {
      return false
    }
    return window.matchMedia(query).matches
  })

  useEffect(() => {
    const mediaQuery = window.matchMedia(query)
    const onChange = () => setMatches(mediaQuery.matches)
    onChange()
    mediaQuery.addEventListener('change', onChange)
    return () => mediaQuery.removeEventListener('change', onChange)
  }, [query])

  return matches
}

/** Phones and small tablets, including iPhone landscape (~844px). */
export function useIsCompactLayout() {
  return useMediaQuery(COMPACT_QUERY)
}
