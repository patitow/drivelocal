import React, { createContext, useContext, useState, useEffect, useCallback } from 'react'
import { api } from '@/lib/api'
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
        // If 401, user is not authenticated - this is expected
        if (error.response?.status === 401) {
          // Only reset state if not preserving (i.e., not after a successful login)
          if (!preserveState) {
            setIsAuthenticated(false)
            setUsername(null)
          }
        } else {
          // Other errors - log but don't retry
          if (import.meta.env.DEV) {
            console.error('Auth check error:', error)
          }
          // Only reset state if not preserving
          if (!preserveState) {
            setIsAuthenticated(false)
            setUsername(null)
            setRole(null)
            setPermissions([])
          }
        }
      }
    } catch (error) {
      if (import.meta.env.DEV) {
        console.error('Auth check failed:', error)
      }
      // Only reset state if not preserving
      if (!preserveState) {
        setIsAuthenticated(false)
        setUsername(null)
        setRole(null)
        setPermissions([])
      }
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
        setIsAuthenticated(false)
        setUsername(null)
        setRole(null)
        setPermissions([])
        toast.error('Sessão expirada. Por favor, faça login novamente.')
      }
    }

    window.addEventListener('auth:logout', handleLogout)
    return () => {
      mounted = false
      window.removeEventListener('auth:logout', handleLogout)
    }
  }, []) // Empty deps - only run once on mount

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

export function useAuth() {
  const context = useContext(AuthContext)
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return context
}

