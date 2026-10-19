import React, { createContext, useContext, useState, useEffect, useCallback, useRef } from 'react'
import { api } from '@/lib/api'
import { SERVER_RESTORED_EVENT } from '@/lib/reconnect'
import { clearOfflineManga } from '@/lib/offlineCache'
import type { LoginRequest } from '@/types'
import { toast } from 'sonner'

interface AuthContextType {
  isAuthenticated: boolean
  isLoading: boolean
  username: string | null
  role: string | null
  permissions: string[]
  authEnabled: boolean
  login: (credentials: LoginRequest) => Promise<boolean>
  logout: () => Promise<void>
  checkAuth: () => Promise<void>
}

const AuthContext = createContext<AuthContextType | undefined>(undefined)

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [isLoading, setIsLoading] = useState(true)
  const [username, setUsername] = useState<string | null>(null)
  const [role, setRole] = useState<string | null>(null)
  const [permissions, setPermissions] = useState<string[]>([])
  const [authEnabled, setAuthEnabled] = useState(true)
  // Verificação que falhou por motivo que não é 401 (ex.: servidor reiniciando).
  // Nesse caso a sessão é mantida e a verificação é refeita quando o servidor voltar.
  const retryAfterRestoreRef = useRef(false)

  const checkAuth = useCallback(async (preserveState: boolean = false) => {
    try {
      setIsLoading(true)

      // First check if auth is enabled via health check
      const health = await api.checkHealth()

      if (!health.authEnabled) {
        // Auth is disabled, allow access
        setAuthEnabled(false)
        setIsAuthenticated(true)
        setIsLoading(false)
        return
      }

      setAuthEnabled(true)

      // Cookies are sent automatically, just check auth status
      // Don't retry on 401 - just set as not authenticated
      try {
        const status = await api.getAuthStatus()
        retryAfterRestoreRef.current = false
        if (status.authenticated) {
          setIsAuthenticated(true)
          setUsername(status.username)
          setRole(status.role || null)
          setPermissions(status.permissions || [])
        } else {
          // Only reset state if not preserving (i.e., not after a successful login)
          if (!preserveState) {
            setIsAuthenticated(false)
            setUsername(null)
            setRole(null)
            setPermissions([])
          }
        }
      } catch (error: any) {
        if (error.response?.status === 401) {
          // 401 - user is not authenticated - this is expected
          retryAfterRestoreRef.current = false
          if (!preserveState) {
            setIsAuthenticated(false)
            setUsername(null)
          }
        } else {
          // Erro que não é 401 (rede, 5xx, servidor reiniciando): NÃO desloga.
          // Refaz a verificação quando o servidor voltar.
          if (import.meta.env.DEV) {
            console.error('Auth check error (sessão mantida):', error)
          }
          retryAfterRestoreRef.current = true
        }
      }
    } catch (error) {
      if (import.meta.env.DEV) {
        console.error('Auth check failed (sessão mantida):', error)
      }
      // Falha ao checar saúde/servidor indisponível: mantém o estado e retenta após server:restored
      retryAfterRestoreRef.current = true
    } finally {
      setIsLoading(false)
    }
  }, [])

  const login = useCallback(async (credentials: LoginRequest): Promise<boolean> => {
    try {
      setIsLoading(true)
      const response = await api.login(credentials)
      
      // Token is stored automatically by api.login() if returned
      if (response.success) {
        // Set authenticated state immediately
        setIsAuthenticated(true)
        setUsername(response.username || null)
        setRole(response.role || null)
        setPermissions(response.permissions || [])
        toast.success('Login realizado com sucesso!')
        
        // Verify auth status after a delay to ensure token is available
        // Use preserveState=true to keep authenticated state even if checkAuth fails
        setTimeout(async () => {
          try {
            await checkAuth(true) // preserveState=true - don't reset if check fails
          } catch (error) {
            // If checkAuth fails, keep the authenticated state from login response
            // The token should still work for subsequent requests
            if (import.meta.env.DEV) {
              console.warn('Auth check after login failed, but login was successful:', error)
            }
          }
        }, 500)
        
        return true
      } else {
        toast.error('Credenciais inválidas')
        return false
      }
    } catch (error: any) {
      const errorMessage = error.response?.data?.error || error.message || 'Erro ao fazer login'
      toast.error(errorMessage)
      return false
    } finally {
      setIsLoading(false)
    }
  }, [checkAuth])

  const logout = useCallback(async () => {
    clearOfflineManga().catch(() => {})
    try {
      setIsLoading(true)
      await api.logout()
      setIsAuthenticated(false)
      setUsername(null)
      setRole(null)
      setPermissions([])
      toast.success('Logout realizado com sucesso!')
    } catch (error) {
      if (import.meta.env.DEV) {
        console.error('Logout error:', error)
      }
      // Clear local state even if API call fails
      setIsAuthenticated(false)
      setUsername(null)
      setRole(null)
      setPermissions([])
      api.setToken(null)
    } finally {
      setIsLoading(false)
    }
  }, [])

  // Check auth on mount only once
  useEffect(() => {
    let mounted = true
    
    const initAuth = async () => {
      await checkAuth()
    }
    
    initAuth()

    // Listen for logout events (from API interceptor)
    const handleLogout = () => {
      if (mounted) {
        clearOfflineManga().catch(() => {})
        setIsAuthenticated(false)
        setUsername(null)
        setRole(null)
        setPermissions([])
        toast.error('Sessão expirada. Por favor, faça login novamente.')
      }
    }

    // Servidor voltou: refaz a verificação que falhou durante a indisponibilidade
    const handleRestored = () => {
      if (mounted && retryAfterRestoreRef.current) {
        void checkAuth(true)
      }
    }

    window.addEventListener('auth:logout', handleLogout)
    window.addEventListener(SERVER_RESTORED_EVENT, handleRestored)
    return () => {
      mounted = false
      window.removeEventListener('auth:logout', handleLogout)
      window.removeEventListener(SERVER_RESTORED_EVENT, handleRestored)
    }
  }, [checkAuth]) // checkAuth é estável (useCallback com deps vazias)

  const value: AuthContextType = {
    isAuthenticated,
    isLoading,
    username,
    role,
    permissions,
    authEnabled,
    login,
    logout,
    checkAuth,
  }

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

// Usuário logado sem exigir o provider (componentes que também rodam isolados).
export function useAuthUsername(): string | null {
  return useContext(AuthContext)?.username ?? null
}

export function useAuth() {
  const context = useContext(AuthContext)
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return context
}

