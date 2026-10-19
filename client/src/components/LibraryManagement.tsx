import { useCallback, useEffect, useState } from 'react'
import { api } from '@/lib/api'
import { waitForServer } from '@/lib/reconnect'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { HostFolderPicker } from '@/components/HostFolderPicker'
import { toast } from 'sonner'
import { AlertTriangle, HardDrive, Loader2, Plus, Trash2 } from 'lucide-react'
import type { MountEntry, MountJob, MountJobAccepted, MountsResponse } from '@/types'

const TERMINAL_STATES: ReadonlySet<string> = new Set(['done', 'rolled_back', 'rollback_failed'])
const JOB_POLL_MS = 1500
const JOB_TIMEOUT_MS = 3 * 60 * 1000

const ERROR_MESSAGES: Record<string, string> = {
  invalid_path: 'Caminho inválido.',
  path_denied: 'Esta pasta não pode ser montada.',
  already_mounted: 'Esta pasta já está montada.',
  name_conflict: 'Já existe uma biblioteca com esse nome.',
  helper_unavailable: 'Helper de montagem offline no PC host.',
}

interface ApplyingState {
  op: 'add' | 'remove'
  name: string
}

function errorData(error: unknown): { error?: string; message?: string } {
  return (error as { response?: { data?: { error?: string; message?: string } } })?.response?.data ?? {}
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

export function LibraryManagement() {
  const [data, setData] = useState<MountsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [pickerOpen, setPickerOpen] = useState(false)
  const [applying, setApplying] = useState<ApplyingState | null>(null)

  const load = useCallback(async () => {
    try {
      setData(await api.getMounts())
    } catch (err) {
      if ((err as { response?: { status?: number } })?.response?.status === 403) {
        toast.error('Você não tem permissão para gerenciar bibliotecas')
      } else {
        toast.error('Erro ao carregar bibliotecas')
      }
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const helperOnline = data?.helper.available === true && !!data.revision
  const jobRunning = !!data?.activeJob && !TERMINAL_STATES.has(data.activeJob.state)
  const actionsDisabled = !helperOnline || jobRunning || applying !== null

  const isActive = (mount: MountEntry): boolean =>
    data?.containerMounts.includes(mount.target) ?? false

  const reportJob = (job: MountJob | null, serverBack: boolean) => {
    if (!job) {
      toast.error('Não foi possível confirmar a alteração. Verifique a lista de bibliotecas.')
      return
    }
    if (job.state === 'done') {
      if (!serverBack) {
        toast.error('O servidor não voltou a tempo. Recarregue a página.')
        return
      }
      toast.success(
        job.op === 'add' ? `Biblioteca "${job.name}" adicionada` : `Biblioteca "${job.name}" removida`
      )
      return
    }
    if (job.state === 'rolled_back') {
      toast.error(`Alteração revertida: ${job.error ?? 'erro ao aplicar a mudança'}`)
      return
    }
    // rollback_failed
    toast.error(
      `Falha ao reverter a alteração: ${job.error ?? 'erro desconhecido'}. Verifique o helper no PC host.`
    )
  }

  // Após o 202: acompanha o job por polling enquanto, em paralelo, aguarda o servidor voltar.
  // Erros de rede são tolerados durante o restart. Sai ao chegar em estado final; em `done`
  // exige que o servidor esteja de volta (novo bootId).
  const followJob = async (jobId: string, prevBootId: string | null) => {
    let serverBack = false
    const serverReady = waitForServer({ prevBootId }).then((ok) => {
      serverBack = ok
      return ok
    })

    const deadline = Date.now() + JOB_TIMEOUT_MS
    let finalJob: MountJob | null = null
    while (!finalJob && Date.now() < deadline) {
      try {
        const job = await api.getMountJob(jobId)
        if (TERMINAL_STATES.has(job.state)) finalJob = job
      } catch {
        // servidor ainda reiniciando: tenta de novo
      }
      if (!finalJob) await sleep(JOB_POLL_MS)
    }

    if (finalJob?.state === 'done' && !serverBack) {
      // Só confirma o sucesso depois que o servidor voltou.
      await serverReady
    }

    setApplying(null)
    reportJob(finalJob, serverBack)
    void load()
  }

  const handleStartError = (error: unknown) => {
    const { error: code, message } = errorData(error)
    if (code === 'revision_conflict') {
      toast.warning('A lista de bibliotecas mudou. Atualizando...')
      void load()
      return
    }
    if (code === 'busy') {
      toast.warning('Outra alteração está em andamento.')
      void load()
      return
    }
    if (code === 'helper_unavailable') {
      void load()
    }
    toast.error((code && ERROR_MESSAGES[code]) || message || 'Erro ao aplicar a alteração.')
  }

  // Resolve quando o helper aceita (202). O acompanhamento segue em segundo plano.
  const startJob = async (
    op: ApplyingState['op'],
    name: string,
    start: () => Promise<MountJobAccepted>
  ): Promise<boolean> => {
    let prevBootId: string | null
    try {
      prevBootId = await api.getBootId()
    } catch {
      toast.error('Servidor indisponível. Tente novamente em instantes.')
      return false
    }

    let accepted: MountJobAccepted
    try {
      accepted = await start()
    } catch (error) {
      handleStartError(error)
      return false
    }

    setApplying({ op, name })
    void followJob(accepted.jobId, prevBootId)
    return true
  }

  const handleAdd = async (hostPath: string, name?: string): Promise<boolean> => {
    if (!data?.revision) return false
    const baseRevision = data.revision
    const ok = await startJob('add', name ?? '', () => api.addMount({ hostPath, name, baseRevision }))
    if (ok) setPickerOpen(false)
    return ok
  }

  const handleRemove = async (mount: MountEntry) => {
    if (!data?.revision) return
    const baseRevision = data.revision
    const confirmed = window.confirm(
      `Remover a biblioteca "${mount.name}"?\n\nO servidor será reiniciado; reproduções em andamento serão interrompidas.`
    )
    if (!confirmed) return
    await startJob('remove', mount.name, () => api.removeMount(mount.name, baseRevision))
  }

  if (loading) {
    return (
      <Card>
        <CardContent className="p-6">
          <p className="text-muted-foreground">Carregando bibliotecas...</p>
        </CardContent>
      </Card>
    )
  }

  const mounts = data?.mounts ?? []

  return (
    <div className="space-y-6 p-0 lg:p-0">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="min-w-0">
          <h2 className="text-xl font-bold lg:text-2xl">Bibliotecas de mídia</h2>
          <p className="text-muted-foreground mt-1">
            Pastas do PC host disponibilizadas ao DriveLocal em modo somente leitura
          </p>
        </div>
        <Button
          onClick={() => setPickerOpen((open) => !open)}
          disabled={actionsDisabled}
          className="w-full sm:w-auto"
        >
          <Plus className="w-4 h-4 mr-2" />
          Adicionar pasta
        </Button>
      </div>

      {data && !data.helper.available && (
        <div role="alert" className="flex gap-3 rounded-md border border-amber-500 bg-amber-500/10 p-4 text-sm">
          <AlertTriangle className="w-5 h-5 shrink-0 text-amber-600" />
          <p>
            Helper de montagem offline no PC host. Inicie a tarefa "DriveLocal Mount Helper" para
            alterar as pastas. As bibliotecas atuais continuam montadas.
          </p>
        </div>
      )}

      {jobRunning && data?.activeJob && (
        <p role="status" className="text-sm text-muted-foreground">
          Alteração em andamento na biblioteca "{data.activeJob.name}". Aguarde a conclusão.
        </p>
      )}

      {pickerOpen && (
        <HostFolderPicker onConfirm={handleAdd} onCancel={() => setPickerOpen(false)} />
      )}

      <Card>
        <CardHeader>
          <CardTitle>Bibliotecas cadastradas</CardTitle>
          <CardDescription>{mounts.length} biblioteca(s)</CardDescription>
        </CardHeader>
        <CardContent>
          {data?.helper.available === false ? (
            // Helper offline: o servidor não envia a lista de cadastros, só os mounts ativos no container.
            data.containerMounts.length === 0 ? (
              <p className="text-muted-foreground">Nenhuma biblioteca montada no container.</p>
            ) : (
              <ul
                aria-label="Bibliotecas montadas (somente leitura)"
                className="divide-y rounded-md border"
              >
                {data.containerMounts.map((target) => (
                  <li key={target} className="flex items-center gap-3 px-4 py-3 text-sm">
                    <HardDrive className="w-4 h-4 shrink-0 text-muted-foreground" />
                    <span className="break-all">{target}</span>
                  </li>
                ))}
              </ul>
            )
          ) : mounts.length === 0 ? (
            <p className="text-muted-foreground">
              {helperOnline ? 'Nenhuma pasta adicionada ainda.' : 'Lista indisponível enquanto o helper estiver offline.'}
            </p>
          ) : (
            <div className="space-y-4">
              {mounts.map((mount) => {
                const active = isActive(mount)
                return (
                  <div
                    key={mount.name}
                    className="flex flex-col gap-3 p-4 border rounded-lg sm:flex-row sm:items-center sm:justify-between"
                  >
                    <div className="flex min-w-0 flex-1 items-start gap-3">
                      <HardDrive className="w-5 h-5 mt-0.5 shrink-0 text-muted-foreground" />
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="font-medium">{mount.name}</span>
                          <Badge variant={active ? 'default' : 'secondary'}>
                            {active ? 'Ativa' : 'Pendente'}
                          </Badge>
                          {mount.legacy && <Badge variant="outline">Legado</Badge>}
                        </div>
                        <p className="text-sm text-muted-foreground break-all">{mount.hostPath}</p>
                        <p className="text-xs text-muted-foreground">{mount.target}</p>
                      </div>
                    </div>
                    <Button
                      variant="destructive"
                      size="sm"
                      disabled={actionsDisabled}
                      onClick={() => handleRemove(mount)}
                      className="w-full sm:w-auto"
                    >
                      <Trash2 className="w-4 h-4 mr-2" />
                      Remover
                    </Button>
                  </div>
                )
              })}
            </div>
          )}
        </CardContent>
      </Card>

      {applying && (
        <div
          role="status"
          aria-live="polite"
          className="fixed inset-0 z-[70] flex items-center justify-center bg-black/60 p-4"
        >
          <div className="flex items-center gap-3 rounded-lg bg-card p-6 text-card-foreground shadow-lg">
            <Loader2 className="w-5 h-5 animate-spin" />
            <span>Aplicando mudança, reconectando…</span>
          </div>
        </div>
      )}
    </div>
  )
}
