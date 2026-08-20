// Logger utility - only logs in development
const isDevelopment = import.meta.env.DEV || import.meta.env.MODE === 'development'

export const logger = {
  log: (...args: any[]) => {
    if (isDevelopment) {
      console.log(...args)
    }
  },
  warn: (...args: any[]) => {
    if (isDevelopment) {
      console.warn(...args)
    }
  },
  error: (...args: any[]) => {
    // Always log errors, even in production
    console.error(...args)
  },
  debug: (...args: any[]) => {
    if (isDevelopment) {
      console.debug(...args)
    }
  },
}

