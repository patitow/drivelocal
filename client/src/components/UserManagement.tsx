import { useState, useEffect } from 'react'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { toast } from 'sonner'
import { Plus, Trash2, Edit2, Save, X, Shield, User as UserIcon } from 'lucide-react'
import type { User, CreateUserRequest } from '@/types'

export function UserManagement() {
  const [users, setUsers] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [showCreateForm, setShowCreateForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [formData, setFormData] = useState<CreateUserRequest>({
    username: '',
    password: '',
    email: '',
    role: 'user',
    permissions: ['read']
  })

  const loadUsers = async () => {
    try {
      setLoading(true)
      const userList = await api.getUsers()
      setUsers(userList)
    } catch (error: any) {
      if (error.response?.status === 403) {
        toast.error('Você não tem permissão para gerenciar usuários')
      } else {
        toast.error('Erro ao carregar usuários')
      }
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadUsers()
  }, [])

  const handleCreate = async () => {
    if (!formData.username || !formData.password || !formData.email) {
      toast.error('Preencha todos os campos obrigatórios')
      return
    }

    try {
      await api.createUser(formData)
      toast.success('Usuário criado com sucesso!')
      setShowCreateForm(false)
      setFormData({ username: '', password: '', email: '', role: 'user', permissions: ['read'] })
      loadUsers()
    } catch (error: any) {
      const message = error.response?.data?.error || 'Erro ao criar usuário'
      toast.error(message)
    }
  }

  const handleDelete = async (id: string, username: string) => {
    if (!confirm(`Tem certeza que deseja excluir o usuário "${username}"?`)) {
      return
    }

    try {
      await api.deleteUser(id)
      toast.success('Usuário excluído com sucesso!')
      loadUsers()
    } catch (error: any) {
      const message = error.response?.data?.error || 'Erro ao excluir usuário'
      toast.error(message)
    }
  }

  const handleUpdate = async (id: string) => {
    const user = users.find(u => u.id === id)
    if (!user) return

    try {
      await api.updateUser(id, {
        email: formData.email,
        role: formData.role,
        permissions: formData.permissions
      })
      toast.success('Usuário atualizado com sucesso!')
      setEditingId(null)
      setFormData({ username: '', password: '', email: '', role: 'user', permissions: ['read'] })
      loadUsers()
    } catch (error: any) {
      const message = error.response?.data?.error || 'Erro ao atualizar usuário'
      toast.error(message)
    }
  }

  const startEdit = (user: User) => {
    setEditingId(user.id)
    setFormData({
      username: user.username,
      password: '', // Don't show password
      email: user.email,
      role: user.role,
      permissions: user.permissions
    })
  }

  const cancelEdit = () => {
    setEditingId(null)
    setFormData({ username: '', password: '', email: '', role: 'user', permissions: ['read'] })
  }

  const togglePermission = (permission: string) => {
    setFormData(prev => ({
      ...prev,
      permissions: prev.permissions.includes(permission)
        ? prev.permissions.filter(p => p !== permission)
        : [...prev.permissions, permission]
    }))
  }

  if (loading) {
    return (
      <div className="p-6">
        <Card>
          <CardContent className="p-6">
            <p className="text-muted-foreground">Carregando usuários...</p>
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="space-y-6 p-0 lg:p-0">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="min-w-0">
          <h2 className="text-xl font-bold lg:text-2xl">Gerenciamento de Usuários</h2>
          <p className="text-muted-foreground mt-1">
            Crie e gerencie contas de acesso ao DriveLocal
          </p>
        </div>
        <Button onClick={() => setShowCreateForm(!showCreateForm)} className="w-full sm:w-auto">
          <Plus className="w-4 h-4 mr-2" />
          Novo Usuário
        </Button>
      </div>

      {showCreateForm && (
        <Card>
          <CardHeader>
            <CardTitle>Criar Novo Usuário</CardTitle>
            <CardDescription>Preencha os dados do novo usuário</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div>
                <label className="text-sm font-medium mb-2 block">Usuário *</label>
                <Input
                  value={formData.username}
                  onChange={(e) => setFormData({ ...formData, username: e.target.value })}
                  placeholder="nomeusuario"
                />
              </div>
              <div>
                <label className="text-sm font-medium mb-2 block">Email *</label>
                <Input
                  type="email"
                  value={formData.email}
                  onChange={(e) => setFormData({ ...formData, email: e.target.value })}
                  placeholder="usuario@exemplo.com"
                />
              </div>
            </div>
            <div>
              <label className="text-sm font-medium mb-2 block">Senha *</label>
              <Input
                type="password"
                value={formData.password}
                onChange={(e) => setFormData({ ...formData, password: e.target.value })}
                placeholder="Senha segura"
              />
            </div>
            <div>
              <label className="text-sm font-medium mb-2 block">Função</label>
              <select
                value={formData.role}
                onChange={(e) => setFormData({ ...formData, role: e.target.value })}
                className="h-11 w-full rounded-md border border-input bg-background px-3 text-base lg:h-10 lg:text-sm"
              >
                <option value="user">Usuário</option>
                <option value="admin">Administrador</option>
              </select>
            </div>
            <div>
              <label className="text-sm font-medium mb-2 block">Permissões</label>
              <div className="flex flex-wrap gap-2">
                {['read', 'write', 'delete', 'admin'].map(perm => (
                  <Badge
                    key={perm}
                    variant={formData.permissions.includes(perm) ? 'default' : 'outline'}
                    className="cursor-pointer"
                    onClick={() => togglePermission(perm)}
                  >
                    {perm}
                  </Badge>
                ))}
              </div>
            </div>
            <div className="flex flex-col-reverse gap-2 sm:flex-row">
              <Button onClick={handleCreate}>Criar Usuário</Button>
              <Button variant="outline" onClick={() => setShowCreateForm(false)}>
                Cancelar
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Usuários Cadastrados</CardTitle>
          <CardDescription>{users.length} usuário(s) cadastrado(s)</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            {users.map(user => (
              <div
                key={user.id}
                className="flex flex-col gap-3 p-4 border rounded-lg sm:flex-row sm:items-center sm:justify-between"
              >
                {editingId === user.id ? (
                  <div className="flex-1 space-y-3">
                    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                      <div>
                        <label className="text-sm font-medium mb-1 block">Email</label>
                        <Input
                          value={formData.email}
                          onChange={(e) => setFormData({ ...formData, email: e.target.value })}
                        />
                      </div>
                      <div>
                        <label className="text-sm font-medium mb-1 block">Função</label>
                        <select
                          value={formData.role}
                          onChange={(e) => setFormData({ ...formData, role: e.target.value })}
                          className="h-11 w-full rounded-md border border-input bg-background px-3 text-base lg:h-10 lg:text-sm"
                        >
                          <option value="user">Usuário</option>
                          <option value="admin">Administrador</option>
                        </select>
                      </div>
                    </div>
                    <div>
                      <label className="text-sm font-medium mb-1 block">Permissões</label>
                      <div className="flex flex-wrap gap-2">
                        {['read', 'write', 'delete', 'admin'].map(perm => (
                          <Badge
                            key={perm}
                            variant={formData.permissions.includes(perm) ? 'default' : 'outline'}
                            className="cursor-pointer"
                            onClick={() => togglePermission(perm)}
                          >
                            {perm}
                          </Badge>
                        ))}
                      </div>
                    </div>
                    <div className="flex gap-2">
                      <Button size="sm" onClick={() => handleUpdate(user.id)}>
                        <Save className="w-4 h-4 mr-1" />
                        Salvar
                      </Button>
                      <Button size="sm" variant="outline" onClick={cancelEdit}>
                        <X className="w-4 h-4 mr-1" />
                        Cancelar
                      </Button>
                    </div>
                  </div>
                ) : (
                  <>
                    <div className="flex min-w-0 flex-1 items-start gap-3">
                        {user.role === 'admin' ? (
                          <Shield className="w-5 h-5 text-primary" />
                        ) : (
                          <UserIcon className="w-5 h-5 text-muted-foreground" />
                        )}
                        <div>
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="font-medium">{user.username}</span>
                            <Badge variant={user.role === 'admin' ? 'default' : 'secondary'}>
                              {user.role}
                            </Badge>
                            {!user.active && (
                              <Badge variant="destructive">Inativo</Badge>
                            )}
                          </div>
                          <p className="text-sm text-muted-foreground">{user.email}</p>
                          <div className="flex flex-wrap gap-1 mt-1">
                            {user.permissions.map(perm => (
                              <Badge key={perm} variant="outline" className="text-xs">
                                {perm}
                              </Badge>
                            ))}
                          </div>
                        </div>
                      </div>
                    <div className="flex shrink-0 gap-2">
                      <Button
                        size="sm"
                        variant="outline"
                        aria-label={`Editar ${user.username}`}
                        onClick={() => startEdit(user)}
                      >
                        <Edit2 className="w-4 h-4" />
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        aria-label={`Excluir ${user.username}`}
                        onClick={() => handleDelete(user.id, user.username)}
                        className="text-destructive hover:text-destructive"
                      >
                        <Trash2 className="w-4 h-4" />
                      </Button>
                    </div>
                  </>
                )}
              </div>
            ))}
            {users.length === 0 && (
              <p className="text-center text-muted-foreground py-8">
                Nenhum usuário cadastrado
              </p>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

