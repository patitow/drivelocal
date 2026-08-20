import { useEffect } from 'react'
import { useAppStore } from '@/lib/store'

export function useTheme() {
  const { settings } = useAppStore()

  useEffect(() => {
    const root = document.documentElement
    const theme = settings.theme

    const getSystemTheme = (): 'light' | 'dark' => {
      if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
        return 'dark'
      }
      return 'light'
    }

    const applyTheme = (themeToApply: 'light' | 'dark') => {
      if (themeToApply === 'dark') {
        root.classList.add('dark')
      } else {
        root.classList.remove('dark')
      }
    }

    if (theme === 'system') {
      const systemTheme = getSystemTheme()
      applyTheme(systemTheme)

      // Listen for system theme changes
      const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
      const handleChange = (e: MediaQueryListEvent) => {
        applyTheme(e.matches ? 'dark' : 'light')
      }

      // Modern browsers
      if (mediaQuery.addEventListener) {
        mediaQuery.addEventListener('change', handleChange)
        return () => mediaQuery.removeEventListener('change', handleChange)
      } else {
        // Fallback for older browsers
        mediaQuery.addListener(handleChange)
        return () => mediaQuery.removeListener(handleChange)
      }
    } else {
      applyTheme(theme)
    }
  }, [settings.theme])
}

