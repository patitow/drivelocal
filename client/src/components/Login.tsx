import { useState } from 'react'
import { useAuth } from '@/contexts/AuthContext'
import { ForgotPassword } from './ForgotPassword'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Lock, User, Loader2 } from 'lucide-react'
import type { LoginRequest } from '@/types'

export function Login() {
  const { login, isLoading } = useAuth()
  const [credentials, setCredentials] = useState<LoginRequest>({
    username: '',
    password: '',
    rememberMe: false,
  })
  const [error, setError] = useState<string | null>(null)
  const [showForgotPassword, setShowForgotPassword] = useState(false)

  if (showForgotPassword) {
    return <ForgotPassword onBack={() => setShowForgotPassword(false)} />
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)

    if (!credentials.username || !credentials.password) {
      setError('Por favor, preencha todos os campos')
      return
    }

    const success = await login(credentials)
    if (!success) {
      setError('Credenciais inválidas. Tente novamente.')
    }
  }

  return (
    <div className="min-h-dvh flex items-center justify-center bg-gradient-to-br from-background via-background to-muted p-4">
      <Card className="w-full max-w-md">
        <CardHeader className="space-y-1">
          <div className="flex items-center justify-center mb-4">
            <div className="rounded-full bg-primary/10 p-3">
              <Lock className="w-8 h-8 text-primary" />
            </div>
          </div>
          <CardTitle className="text-2xl font-bold text-center">
            DriveLocal
          </CardTitle>
          <CardDescription className="text-center">
            Faça login para acessar seus arquivos
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-2">
              <label htmlFor="username" className="text-sm font-medium">
                Usuário
              </label>
              <div className="relative">
                <User className="absolute left-3 top-1/2 transform -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                <Input
                  id="username"
                  type="text"
                  placeholder="Digite seu usuário"
                  value={credentials.username}
                  onChange={(e) =>
                    setCredentials({ ...credentials, username: e.target.value })
                  }
                  disabled={isLoading}
                  className="pl-10"
                  autoComplete="username"
                />
              </div>
            </div>

            <div className="space-y-2">
              <label htmlFor="password" className="text-sm font-medium">
                Senha
              </label>
              <div className="relative">
                <Lock className="absolute left-3 top-1/2 transform -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                <Input
                  id="password"
                  type="password"
                  placeholder="Digite sua senha"
                  value={credentials.password}
                  onChange={(e) =>
                    setCredentials({ ...credentials, password: e.target.value })
                  }
                  disabled={isLoading}
                  className="pl-10"
                  autoComplete="current-password"
                />
              </div>
            </div>

            <label htmlFor="rememberMe" className="flex min-h-11 cursor-pointer items-center gap-3 py-1">
              <input
                type="checkbox"
                id="rememberMe"
                checked={credentials.rememberMe}
                onChange={(e) =>
                  setCredentials({ ...credentials, rememberMe: e.target.checked })
                }
                disabled={isLoading}
                className="h-5 w-5 rounded border-gray-300 text-primary focus:ring-primary"
              />
              <span className="text-sm font-medium">
                Lembrar-me
              </span>
            </label>

            {error && (
              <div className="p-3 rounded-md bg-destructive/10 border border-destructive/20">
                <p className="text-sm text-destructive">{error}</p>
              </div>
            )}

            <Button
              type="submit"
              className="w-full"
              disabled={isLoading}
            >
              {isLoading ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Entrando...
                </>
              ) : (
                'Entrar'
              )}
            </Button>

            <div className="text-center">
              <button
                type="button"
                onClick={() => setShowForgotPassword(true)}
                className="text-sm text-primary hover:underline"
                disabled={isLoading}
              >
                Esqueceu sua senha?
              </button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}

