import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'
import { format, formatDistanceToNow } from 'date-fns'
import { ptBR } from 'date-fns/locale'
import { encodeMediaPath } from './mediaPath'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// File utilities
export function formatFileSize(bytes: number): string {
  if (bytes === 0) return '0 Bytes'
  
  const k = 1024
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

export function getFileExtension(filename: string): string {
  return filename.slice((filename.lastIndexOf('.') - 1 >>> 0) + 2)
}

export function isVideoFile(filename: string): boolean {
  const videoExtensions = ['mp4', 'avi', 'mov', 'wmv', 'flv', 'webm', 'mkv', 'm4v']
  const ext = getFileExtension(filename).toLowerCase()
  return videoExtensions.includes(ext)
}

export function isImageFile(filename: string): boolean {
  const imageExtensions = ['jpg', 'jpeg', 'png', 'gif', 'bmp', 'webp', 'svg', 'tiff']
  const ext = getFileExtension(filename).toLowerCase()
  return imageExtensions.includes(ext)
}

// Date utilities
export function formatDate(date: string | Date): string {
  const dateObj = typeof date === 'string' ? new Date(date) : date
  return format(dateObj, 'dd/MM/yyyy HH:mm', { locale: ptBR })
}

export function formatRelativeDate(date: string | Date): string {
  const dateObj = typeof date === 'string' ? new Date(date) : date
  return formatDistanceToNow(dateObj, { addSuffix: true, locale: ptBR })
}

// String utilities
export function truncateText(text: string, maxLength: number): string {
  if (text.length <= maxLength) return text
  return text.slice(0, maxLength) + '...'
}

export function capitalizeFirst(str: string): string {
  return str.charAt(0).toUpperCase() + str.slice(1)
}

// URL utilities
export function buildApiUrl(endpoint: string): string {
  // Use window.location.origin to go through Vite proxy in development
  const baseUrl = import.meta.env.VITE_API_URL || window.location.origin
  return `${baseUrl}${endpoint.startsWith('/') ? endpoint : `/${endpoint}`}`
}

// These functions now use the API client to include authentication tokens
// They import api lazily to avoid circular dependencies
let apiInstance: any = null

function getApi() {
  if (!apiInstance) {
    // Dynamic import to avoid circular dependency
    apiInstance = import('./api').then(m => m.api)
  }
  return apiInstance
}

export async function buildStreamUrl(filePath: string): Promise<string> {
  const api = await getApi()
  return api.getStreamUrl(filePath)
}

export async function buildThumbnailUrl(filePath: string): Promise<string> {
  const api = await getApi()
  return api.getThumbnailUrl(filePath)
}

export async function buildDownloadUrl(filePath: string): Promise<string> {
  const api = await getApi()
  return api.getDownloadUrl(filePath)
}

// Synchronous versions that get token from localStorage directly
// These are used for immediate URL generation (e.g., in src attributes)
export function buildStreamUrlSync(filePath: string): string {
  const baseUrl = window.location.origin
  return withMediaAuthToken(`${baseUrl}/api/stream/${encodeMediaPath(filePath)}`)
}

function getCookie(name: string): string | null {
  const value = `; ${document.cookie}`
  const parts = value.split(`; ${name}=`)
  if (parts.length === 2) {
    return parts.pop()?.split(';').shift() || null
  }
  return null
}

function withMediaAuthToken(url: string): string {
  const token = getCookie('imgToken') || localStorage.getItem('auth_token')
  if (!token) {
    return url
  }
  const separator = url.includes('?') ? '&' : '?'
  return `${url}${separator}token=${encodeURIComponent(token)}`
}

export function buildThumbnailUrlSync(filePath: string): string {
  const baseUrl = window.location.origin
  return withMediaAuthToken(`${baseUrl}/api/thumbnail/${encodeMediaPath(filePath)}`)
}

export function buildDownloadUrlSync(filePath: string): string {
  const baseUrl = window.location.origin
  return withMediaAuthToken(`${baseUrl}/api/download/${encodeMediaPath(filePath)}`)
}

// Array utilities
export function groupBy<T>(array: T[], key: keyof T): Record<string, T[]> {
  return array.reduce((groups, item) => {
    const group = String(item[key])
    groups[group] = groups[group] || []
    groups[group].push(item)
    return groups
  }, {} as Record<string, T[]>)
}

export function sortBy<T>(array: T[], key: keyof T, order: 'asc' | 'desc' = 'asc'): T[] {
  return [...array].sort((a, b) => {
    const aVal = a[key]
    const bVal = b[key]
    
    if (aVal < bVal) return order === 'asc' ? -1 : 1
    if (aVal > bVal) return order === 'asc' ? 1 : -1
    return 0
  })
}

// Validation utilities
export function isValidUrl(string: string): boolean {
  try {
    new URL(string)
    return true
  } catch {
    return false
  }
}

export function isValidEmail(email: string): boolean {
  const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
  return emailRegex.test(email)
}

// Storage utilities
export function getFromStorage<T>(key: string, defaultValue: T): T {
  try {
    const item = localStorage.getItem(key)
    return item ? JSON.parse(item) : defaultValue
  } catch {
    return defaultValue
  }
}

export function setToStorage<T>(key: string, value: T): void {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch (error) {
    // Silently fail - localStorage might be disabled
    if (import.meta.env.DEV) {
      console.error('Failed to save to localStorage:', error)
    }
  }
}

// Debounce utility
export function debounce<T extends (...args: unknown[]) => unknown>(
  func: T,
  wait: number
): (...args: Parameters<T>) => void {
  let timeout: ReturnType<typeof setTimeout> | null = null
  
  return (...args: Parameters<T>) => {
    if (timeout) clearTimeout(timeout)
    timeout = setTimeout(() => func(...args), wait)
  }
}

// Throttle utility
export function throttle<T extends (...args: unknown[]) => unknown>(
  func: T,
  limit: number
): (...args: Parameters<T>) => void {
  let inThrottle: boolean
  
  return (...args: Parameters<T>) => {
    if (!inThrottle) {
      func(...args)
      inThrottle = true
      setTimeout(() => (inThrottle = false), limit)
    }
  }
}
