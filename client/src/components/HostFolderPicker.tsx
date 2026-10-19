import { useEffect, useState } from 'react'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { ArrowUp, ChevronRight, Folder, HardDrive, Loader2, Lock } from 'lucide-react'
import type { BrowseResponse } from '@/types'

interface HostFolderPickerProps {
  /** Retorna true quando o helper aceitou a montagem (o seletor fecha). */
  // name só é enviado quando o usuário editou o campo; senão o helper escolhe o nome padrão.
  onConfirm: (hostPath: string, name?: string) => Promise<boolean>
  onCancel: () => void
}

// Caminho vazio representa a lista de drives.
function basenameOf(path: string): string {
  const parts = path.split(/[\\/]/).filter(Boolean)
  return parts.length > 1 ? parts[parts.length - 1] : ''
}

function breadcrumbsOf(path: string): Array<{ label: string; path: string }> {
  const parts = path.split(/[\\/]/).filter(Boolean)
  const crumbs: Array<{ label: string; path: string }> = []
  let acc = ''
  parts.forEach((part, i) => {
    if (i === 0) {
      acc = `${part}\\`
      crumbs.push({ label: acc, path: acc })
    } else {
      acc = acc.endsWith('\\') ? `${acc}${part}` : `${acc}\\${part}`
      crumbs.push({ label: part, path: acc })
    }
  })
  return crumbs
}

function browseErrorMessage(error: unknown): string {
  const data = (error as { response?: { data?: { error?: string; message?: string } } })?.response?.data
  if (data?.error === 'helper_unavailable') {
    return 'Helper de montagem offline no PC host. Não é possível navegar pelas pastas.'
  }
  return data?.message || 'Não foi possível listar esta pasta.'
}

export function HostFolderPicker({ onConfirm, onCancel }: HostFolderPickerProps) {
  const [data, setData] = useState<BrowseResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [nameEdited, setNameEdited] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  // Motivo de bloqueio da pasta atual, conhecido ao navegar por uma entrada da listagem.
  const [blockedReason, setBlockedReason] = useState<string | null>(null)

  const load = async (target: string, entry?: { selectable: boolean; reason?: string }) => {
    setLoading(true)
    setError(null)
    try {
      const res = await api.browseHostFolders(target || undefined)
      setData(res)
      setName(basenameOf(res.path))
      setNameEdited(false)
      setBlockedReason(entry && !entry.selectable ? entry.reason ?? 'Esta pasta não pode ser montada' : null)
    } catch (err) {
      setError(browseErrorMessage(err))
    } finally {
      setLoading(false)
    }
  }

  // Carga inicial: lista os drives
  useEffect(() => {
    void load('')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleConfirm = async () => {
    if (!data || !data.path) return
    const trimmed = name.trim()
    if (!trimmed) {
      setError('Informe um nome para a biblioteca.')
      return
    }
    if (trimmed.length > 64 || /[\\/:*?"<>|]/.test(trimmed) || trimmed === '.' || trimmed === '..') {
      setError('Nome inválido. Não use / \\ : * ? " < > | e use no máximo 64 caracteres.')
      return
    }
    setError(null)
    setSubmitting(true)
    try {
      await onConfirm(data.path, nameEdited ? trimmed : undefined)
    } finally {
      setSubmitting(false)
    }
  }

  const canConfirm = !!data && !!data.path && !blockedReason && !loading && !submitting

  return (
    <Card>
      <CardHeader>
        <CardTitle>Escolher pasta do PC</CardTitle>
        <CardDescription>
          Navegue até a pasta que será disponibilizada, somente leitura, em /data/&lt;nome&gt;.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <nav aria-label="Caminho atual" className="flex flex-wrap items-center gap-1 text-sm">
          <Button type="button" variant="ghost" size="sm" onClick={() => load('')}>
            Drives
          </Button>
          {breadcrumbsOf(data?.path ?? '').map((crumb) => (
            <span key={crumb.path} className="flex items-center gap-1">
              <ChevronRight className="w-4 h-4 text-muted-foreground" />
              <Button type="button" variant="ghost" size="sm" onClick={() => load(crumb.path)}>
                {crumb.label}
              </Button>
            </span>
          ))}
        </nav>

        {data && data.path && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => load(data.parent ?? '')}
            disabled={loading}
          >
            <ArrowUp className="w-4 h-4 mr-2" />
            Subir um nível
          </Button>
        )}

        {loading && (
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="w-4 h-4 animate-spin" />
            Carregando pastas...
          </p>
        )}

        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}

        {data && !loading && (
          data.entries.length === 0 ? (
            <p className="text-sm text-muted-foreground">Nenhuma subpasta aqui.</p>
          ) : (
            <ul className="divide-y rounded-md border max-h-80 overflow-auto">
              {data.entries.map((entry) => (
                <li key={entry.path} className="flex items-center gap-3 px-3 py-2">
                  <button
                    type="button"
                    onClick={() => load(entry.path, entry)}
                    className="flex min-w-0 flex-1 items-center gap-2 text-left hover:underline"
                  >
                    {!data.path ? (
                      <HardDrive className="w-4 h-4 shrink-0 text-muted-foreground" />
                    ) : entry.selectable ? (
                      <Folder className="w-4 h-4 shrink-0 text-muted-foreground" />
                    ) : (
                      <Lock className="w-4 h-4 shrink-0 text-muted-foreground" />
                    )}
                    <span className="truncate">{entry.name}</span>
                  </button>
                  {!entry.selectable && entry.reason && (
                    <span className="max-w-[50%] text-xs text-muted-foreground text-right">
                      {entry.reason}
                    </span>
                  )}
                </li>
              ))}
            </ul>
          )
        )}

        {data && data.path && (
          <div className="space-y-3 border-t pt-4">
            <p className="text-sm text-muted-foreground break-all">
              Pasta selecionada: <span className="font-medium text-foreground">{data.path}</span>
            </p>
            {blockedReason && (
              <p role="alert" className="text-sm text-destructive">
                {blockedReason}
              </p>
            )}
            <div>
              <label htmlFor="library-name" className="text-sm font-medium mb-2 block">
                Nome da biblioteca
              </label>
              <Input
                id="library-name"
                value={name}
                maxLength={64}
                onChange={(e) => {
                  setName(e.target.value)
                  setNameEdited(true)
                }}
                placeholder="ex.: Filmes"
              />
            </div>
            <div className="flex flex-col-reverse gap-2 sm:flex-row">
              <Button type="button" onClick={handleConfirm} disabled={!canConfirm}>
                {submitting && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
                Montar esta pasta
              </Button>
              <Button type="button" variant="outline" onClick={onCancel} disabled={submitting}>
                Cancelar
              </Button>
            </div>
          </div>
        )}

        {!data && !loading && (
          <div className="flex flex-col-reverse gap-2 sm:flex-row">
            <Button type="button" variant="outline" onClick={onCancel}>
              Cancelar
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
