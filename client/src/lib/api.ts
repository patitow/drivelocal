import axios, { type AxiosInstance, type AxiosRequestConfig, type AxiosResponse } from 'axios'
import type { ApiResponse, HealthResponse, NetworkInfo, DirectoryInfo, LoginRequest, LoginResponse, AuthStatus, User, CreateUserRequest, UpdateUserRequest, MountsResponse, BrowseResponse, MountJob, MountJobAccepted, MediaInfo } from '@/types'
import { encodeMediaPath } from '@/lib/mediaPath'
import { SERVER_UNAVAILABLE_EVENT, isServerUnavailableError } from '@/lib/reconnect'

type DirectoryPage = DirectoryInfo & { pagination?: { hasMore: boolean } }

// Use port 80 in production (same as frontend), 3000/5000 in development
const getApiBaseUrl = () => {
  if (import.meta.env.VITE_API_URL) {
    return import.meta.env.VITE_API_URL
  }
  // In development, frontend is on 3000 and API on 5000
  // Vite proxy handles /api -> localhost:5000 automatically
  // So we use the same origin (frontend port 3000) and let Vite proxy handle it
  // In production, everything is on the same port (80)
  return window.location.origin
}

const API_BASE_URL = getApiBaseUrl()

class ApiClient {
  private client: AxiosInstance
  private token: string | null = null

  constructor() {
    this.client = axios.create({
      baseURL: API_BASE_URL,
      timeout: 30000, // 30s for regular requests
      headers: {
        'Content-Type': 'application/json',
      },
      withCredentials: true, // Always send cookies
      paramsSerializer: (params) => {
        if (!params || typeof params !== 'object') {
          return ''
        }
        return Object.entries(params)
          .filter(([, value]) => value !== undefined && value !== null)
          .map(([key, value]) => `${encodeURIComponent(key)}=${encodeURIComponent(String(value))}`)
          .join('&')
      },
    })

    // Load token from localStorage on initialization (backward compatibility)
    this.loadToken()
    this.setupInterceptors()
  }

  private loadToken(): void {
    try {
      const storedToken = localStorage.getItem('auth_token')
      if (storedToken) {
        this.token = storedToken
      }
    } catch (error) {
      // Silently fail - token loading is optional
      if (import.meta.env.DEV) {
        console.error('Failed to load token from storage:', error)
      }
    }
  }

  setToken(token: string | null): void {
    this.token = token
    if (token) {
      localStorage.setItem('auth_token', token)
    } else {
      localStorage.removeItem('auth_token')
    }
  }

  getToken(): string | null {
    return this.token
  }

  private setupInterceptors(): void {
    // Request interceptor - Add token to requests (backward compatibility)
    this.client.interceptors.request.use(
      (config) => {
        // Always reload token from localStorage before each request (in case it was updated)
        const storedToken = localStorage.getItem('auth_token')
        if (storedToken && storedToken !== this.token) {
          this.token = storedToken
        }
        
        // Add token to Authorization header if available (for backward compatibility)
        // BUT: Don't add token to login endpoint (it doesn't need it)
        const isLoginEndpoint = config.url?.includes('/api/auth/login')
        if (!isLoginEndpoint) {
          if (this.token && !config.headers.Authorization) {
            config.headers.Authorization = `Bearer ${this.token}`
            // Token added silently - no logging needed
          }
        }
        
        // Cookies are sent automatically with withCredentials: true
        // But we still add token as query parameter for streaming/download endpoints if available
        if (this.token && (config.url?.includes('/stream/') || config.url?.includes('/download/'))) {
          const separator = config.url.includes('?') ? '&' : '?'
          config.url = `${config.url}${separator}token=${encodeURIComponent(this.token)}`
        }
        
        return config
      },
      (error) => {
        // Log errors only in development
        if (import.meta.env.DEV) {
          console.error('Request error:', error)
        }
        return Promise.reject(error)
      }
    )

    // Response interceptor - Handle 401 errors and auto-refresh
    this.client.interceptors.response.use(
      (response) => {
        return response
      },
      async (error) => {
        const originalRequest = error.config

        // Servidor indisponível (rede caiu, container reiniciando, proxy 502/503/504)
        if (isServerUnavailableError(error)) {
          window.dispatchEvent(new Event(SERVER_UNAVAILABLE_EVENT))
        }

        // Don't retry auth endpoints or if already retried
        const isAuthEndpoint = originalRequest?.url?.includes('/api/auth/')
        const isLoginEndpoint = originalRequest?.url?.includes('/api/auth/login')
        const isRefreshEndpoint = originalRequest?.url?.includes('/api/auth/refresh')
        const isStatusEndpoint = originalRequest?.url?.includes('/api/auth/status')
        const skipRetry = (originalRequest as any)?._skipAuthRetry
        
        // If it's an auth endpoint and we get 401, don't try refresh - just fail
        if (error.response?.status === 401 && (isAuthEndpoint || isStatusEndpoint || skipRetry)) {
          return Promise.reject(error)
        }
        
        // If we get a 401, try to refresh token (but not on auth endpoints)
        if (
          error.response?.status === 401 && 
          !originalRequest._retry &&
          !isAuthEndpoint &&
          !isLoginEndpoint &&
          !isRefreshEndpoint &&
          !isStatusEndpoint &&
          !skipRetry
        ) {
          originalRequest._retry = true
          
          try {
            const refreshed = await this.refreshToken()
            if (refreshed) {
              // Retry original request
              return this.client(originalRequest)
            }
          } catch (refreshError) {
            // Refresh failed, don't logout here - let the component handle it
            if (import.meta.env.DEV) {
              console.warn('Token refresh failed')
            }
            this.setToken(null)
          }
        }
        
        // Log errors only in development
        if (import.meta.env.DEV) {
          console.error('Response error:', error.response?.data || error.message)
        }
        return Promise.reject(error)
      }
    )
  }

  // Generic request method
  async request<T = unknown>(config: AxiosRequestConfig): Promise<ApiResponse<T>> {
    const response: AxiosResponse<T> = await this.client.request(config)
    return {
      data: response.data,
      status: response.status,
    }
  }

  // Health check
  async getHealth(): Promise<HealthResponse> {
    const response = await this.client.get<HealthResponse>('/api/health')
    return response.data
  }

  // Network info
  async getNetworkInfo(): Promise<NetworkInfo> {
    const response = await this.client.get<NetworkInfo>('/api/network-info')
    return response.data
  }

  // Lista a pasta inteira: o servidor pagina (100 por padrão), então busca as páginas até o fim
  async getFiles(path: string = ''): Promise<DirectoryInfo> {
    const params = { path: path.replace(/\\/g, '/'), limit: 1000 }
    let page = (await this.client.get<DirectoryPage>('/api/files', { params })).data
    const items = [...(page.items ?? [])]
    while (page.pagination?.hasMore && items.length > 0) {
      page = (await this.client.get<DirectoryPage>('/api/files', { params: { ...params, offset: items.length } })).data
      if (!page.items?.length) break
      items.push(...page.items)
    }
    return { currentPath: page.currentPath, items }
  }

  // Generic get method
  async get<T = unknown>(url: string, config?: any): Promise<T> {
    const response = await this.client.get<T>(url, config)
    return response.data
  }

  // Generic post method
  async post<T = unknown>(url: string, data?: any, config?: any): Promise<T> {
    const response = await this.client.post<T>(url, data, config)
    return response.data
  }

  // Download single file
  async downloadFile(filePath: string): Promise<Blob> {
    const response = await this.client.get(`/api/download/${encodeMediaPath(filePath)}`, {
      responseType: 'blob'
    })
    return response.data
  }

  // Download multiple files
  async downloadMultipleFiles(files: string[]): Promise<Blob> {
    const response = await this.client.post('/api/download-multiple', { files }, {
      responseType: 'blob'
    })
    return response.data
  }

  // Get file info
  async getFileInfo(filePath: string): Promise<any> {
    const response = await this.client.get(`/api/file-info/${encodeMediaPath(filePath)}`)
    return response.data
  }

  // Stream file
  getStreamUrl(filePath: string): string {
    // Cookies are sent automatically with withCredentials: true
    return `${API_BASE_URL}/api/stream/${encodeMediaPath(filePath)}`
  }

  // Get thumbnail URL
  getThumbnailUrl(filePath: string): string {
    return `${API_BASE_URL}/api/thumbnail/${encodeMediaPath(filePath)}`
  }

  // Get download URL
  getDownloadUrl(filePath: string): string {
    const url = `${API_BASE_URL}/api/download/${encodeMediaPath(filePath)}`
    if (this.token) {
      return `${url}?token=${this.token}`
    }
    return url
  }

  // Authentication methods
  async login(credentials: LoginRequest): Promise<LoginResponse> {
    const response = await this.client.post<LoginResponse>('/api/auth/login', credentials, {
      withCredentials: true // Important for cookies
    })
    // If server returns token (for IP access when cookies don't work), store it
    if (response.data.token) {
      this.setToken(response.data.token)
      // Store in localStorage as fallback
      try {
        localStorage.setItem('auth_token', response.data.token)
      } catch (error) {
        if (import.meta.env.DEV) {
          console.warn('Failed to store token in localStorage:', error)
        }
      }
    }
    return response.data
  }

  async logout(): Promise<void> {
    try {
      await this.client.post('/api/auth/logout', {}, {
        withCredentials: true
      })
      } catch (error) {
        // Log errors only in development
        if (import.meta.env.DEV) {
          console.error('Logout error:', error)
        }
      } finally {
      this.setToken(null)
    }
  }

  async refreshToken(): Promise<boolean> {
    try {
      // Don't retry refresh if already retrying
      const response = await this.client.post('/api/auth/refresh', {}, {
        withCredentials: true,
        // @ts-ignore - Custom flag to prevent infinite loop
        _skipAuthRetry: true
      })
      return response.data.success === true
    } catch (error) {
      // Refresh failed - user needs to login again
      return false
    }
  }

  async forgotPassword(email: string): Promise<{ success: boolean; message: string }> {
    const response = await this.client.post('/api/auth/forgot-password', { email })
    return response.data
  }

  async resetPassword(email: string, token: string, newPassword: string): Promise<{ success: boolean; message: string }> {
    const response = await this.client.post('/api/auth/reset-password', {
      email,
      token,
      newPassword
    })
    return response.data
  }

  async getAuthStatus(): Promise<AuthStatus> {
    // Erros propagam (inclusive não-401) para o chamador decidir: só 401 significa "não logado".
    // Antes, erros de rede viravam authenticated:false e deslogavam o usuário.
    const response = await this.client.get<AuthStatus>('/api/auth/status', {
      withCredentials: true
    })
    return response.data
  }

  async checkHealth(): Promise<HealthResponse> {
    const response = await this.client.get<HealthResponse>('/api/health')
    return response.data
  }

  // User Management (Admin only)
  async getUsers(): Promise<User[]> {
    const response = await this.client.get<{ users: User[] }>('/api/users')
    return response.data.users
  }

  async createUser(user: CreateUserRequest): Promise<User> {
    const response = await this.client.post<{ success: boolean; user: User }>('/api/users', user)
    return response.data.user
  }

  async updateUser(id: string, updates: UpdateUserRequest): Promise<User> {
    const response = await this.client.put<{ success: boolean; user: User }>(`/api/users/${id}`, updates)
    return response.data.user
  }

  async deleteUser(id: string): Promise<void> {
    await this.client.delete(`/api/users/${id}`)
  }

  // Montagem de pastas do host (Admin only)
  async getMounts(): Promise<MountsResponse> {
    const response = await this.client.get<MountsResponse>('/api/admin/mounts')
    return response.data
  }

  // path omitido -> lista os drives
  async browseHostFolders(path?: string): Promise<BrowseResponse> {
    const response = await this.client.get<BrowseResponse>('/api/admin/mounts/browse', {
      params: { path },
    })
    return response.data
  }

  async addMount(input: { hostPath: string; name?: string; baseRevision: string }): Promise<MountJobAccepted> {
    const response = await this.client.post<MountJobAccepted>('/api/admin/mounts', input)
    return response.data
  }

  async removeMount(name: string, baseRevision: string): Promise<MountJobAccepted> {
    const response = await this.client.delete<MountJobAccepted>(
      `/api/admin/mounts/${encodeURIComponent(name)}`,
      { params: { baseRevision } }
    )
    return response.data
  }

  async getMountJob(id: string): Promise<MountJob> {
    const response = await this.client.get<MountJob>(`/api/admin/mounts/jobs/${encodeURIComponent(id)}`)
    return response.data
  }

  // caps = codecs que este navegador toca; o servidor devolve playback (direct/remux/transcode)
  async getMediaInfo(filePath: string, caps?: string): Promise<MediaInfo> {
    const response = await this.client.get<MediaInfo>(`/api/media-info/${encodeMediaPath(filePath)}`, {
      params: caps ? { caps } : undefined,
    })
    return response.data
  }

  // Keyframe em ou após t: no remux o vídeo copiado só começa em keyframe
  async getKeyframe(filePath: string, t: number): Promise<number> {
    const response = await this.client.get<{ time: number }>(`/api/keyframe/${encodeMediaPath(filePath)}`, {
      params: { t: t.toFixed(3) },
    })
    return response.data.time
  }

  // Legenda de texto convertida para WebVTT pelo servidor
  async getSubtitleVtt(filePath: string, id: string): Promise<string> {
    const response = await this.client.get<string>(`/api/subtitle/${encodeMediaPath(filePath)}`, {
      params: { id },
      responseType: 'text',
      // Num mkv grande o servidor lê o arquivo inteiro para extrair (pode passar de 1 min).
      timeout: 10 * 60 * 1000,
    })
    return response.data
  }

  // bootId muda a cada boot do servidor; null se o servidor não informar (versão antiga)
  async getBootId(): Promise<string | null> {
    const health = await this.getHealth()
    return health.bootId ?? null
  }
}

// Create singleton instance
export const api = new ApiClient()

// Export default for backward compatibility
export default api
