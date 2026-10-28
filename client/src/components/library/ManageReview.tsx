// Gerenciar → Revisão: estado do catálogo (scan, IA), duplicatas de tópicos e
// sugestões pendentes (da IA e dos usuários).
import { useState } from 'react'
import { toast } from 'sonner'
import { Check, Loader2, Pause, Play, RefreshCw, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { library, type Suggestion } from '@/lib/library'
import { Status, errorMessage, useLoad } from './catalogShared'
import { TopicDupesReview } from './TopicDupesReview'

interface ScanInfo { startedAt?: string; finishedAt?: string; seen?: number; added?: number; updated?: number; moved?: number; missing?: number; restored?: number; errors?: number }
interface Stats {
  media?: number; missing?: number; topics?: number; playlists?: number; pendingSuggestions?: number; scanRunning?: boolean
  byKind?: Record<string, number>; byMaturity?: Record<string, number>; byRoot?: Record<string, number>; lastScan?: ScanInfo
}
interface AIStatus { paused?: boolean; running?: boolean; inWindow?: boolean; config?: { enabled?: boolean; window?: string; visionModel?: string } }

const fmt = (n?: number) => (n ?? 0).toLocaleString('pt-BR')
const MAT: Record<string, string> = { geral: 'Livre', sugestivo: 'Sugestivo', explicito: 'Explícito' }

function when(iso?: string) {
  if (!iso) return 'nunca'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' })
}

function Tile({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="rounded-xl border p-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="font-display text-2xl font-bold tabular-nums">{value}</p>
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  )
}

function Bars({ title, data, label }: { title: string; data?: Record<string, number>; label?: (k: string) => string }) {
  const rows = Object.entries(data ?? {}).sort((a, b) => b[1] - a[1])
  const max = Math.max(1, ...rows.map((r) => r[1]))
  if (!rows.length) return null
  return (
    <div className="space-y-1.5 rounded-xl border p-3">
      <p className="text-sm font-semibold">{title}</p>
      {rows.map(([k, v]) => (
        <div key={k} className="flex items-center gap-2 text-sm">
          <span className="w-36 shrink-0 truncate" title={k}>{label?.(k) ?? k}</span>
          <span className="h-2 flex-1 rounded-full bg-secondary"><span className="block h-full rounded-full bg-foreground/70" style={{ width: `${(v / max) * 100}%` }} /></span>
          <span className="w-16 shrink-0 text-right tabular-nums text-muted-foreground">{fmt(v)}</span>
        </div>
      ))}
    </div>
  )
}

export function ManageReview() {
  const stats = useLoad(['admin', 'stats'], () => library.admin.stats() as Promise<Stats>)
  const ai = useLoad(['admin', 'ai'], () => library.admin.aiStatus() as Promise<AIStatus>)
  const review = useLoad(['admin', 'review'], () => library.admin.review(50))
  const [busy, setBusy] = useState(false)
  const s = stats.data
  const last = s?.lastScan

  const decide = async (list: Suggestion[], accept: boolean) => {
    setBusy(true)
    try {
      await library.admin.decide(list.map((x) => x.id), accept)
      review.reload()
      stats.reload()
    } catch (e) {
      toast.error(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-6">
      <section aria-label="Catálogo" className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className="flex-1 font-semibold">Catálogo</h3>
          <Button size="sm" variant="outline" disabled={s?.scanRunning}
            onClick={() => library.admin.scan().then(() => { toast.success('Varredura iniciada'); setTimeout(stats.reload, 1500) }, (e) => toast.error(errorMessage(e)))}>
            {s?.scanRunning ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : <RefreshCw className="mr-1 h-4 w-4" />}
            {s?.scanRunning ? 'Varrendo…' : 'Varrer pastas agora'}
          </Button>
        </div>
        <Status loading={stats.loading} error={stats.error} />
        {s && (
          <>
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
              <Tile label="Itens" value={fmt(s.media)} hint={`${fmt(s.byKind?.video)} vídeos · ${fmt(s.byKind?.image)} imagens`} />
              <Tile label="Sumidos" value={fmt(s.missing)} hint="arquivos que não estão mais nas pastas" />
              <Tile label="Tópicos" value={fmt(s.topics)} hint={`${fmt(s.playlists)} playlists`} />
              <Tile label="Última varredura" value={last?.finishedAt ? when(last.finishedAt) : '—'}
                hint={last ? `${fmt(last.seen)} vistos · +${fmt(last.added)} novos · ${fmt(last.moved)} movidos${last.errors ? ` · ${last.errors} erros` : ''}` : undefined} />
            </div>
            <div className="grid grid-cols-1 gap-2 lg:grid-cols-2">
              <Bars title="Por pasta" data={s.byRoot} />
              <Bars title="Por maturidade" data={s.byMaturity} label={(k) => MAT[k] ?? k} />
            </div>
          </>
        )}
      </section>

      {ai.data && (
        <section aria-label="IA" className="flex flex-wrap items-center gap-3 rounded-xl border p-3 text-sm">
          <span className="font-semibold">IA de classificação</span>
          <Badge variant="secondary">{ai.data.config?.enabled === false ? 'desligada' : ai.data.paused ? 'pausada' : ai.data.running ? 'rodando' : 'aguardando'}</Badge>
          {ai.data.config?.window && <span className="text-muted-foreground">janela {ai.data.config.window}</span>}
          {ai.data.config?.visionModel && <span className="text-muted-foreground">{ai.data.config.visionModel}</span>}
          <span className="flex-1" />
          <Button size="sm" variant="outline" onClick={async () => {
            await library.admin.aiPause(!ai.data?.paused).catch((e) => toast.error(errorMessage(e)))
            ai.reload()
          }}>
            {ai.data.paused ? <><Play className="mr-1 h-4 w-4" />Retomar</> : <><Pause className="mr-1 h-4 w-4" />Pausar</>}
          </Button>
        </section>
      )}

      <TopicDupesReview />

      <section aria-label="Sugestões" className="space-y-3">
        <h3 className="font-semibold">Sugestões pendentes {review.data ? `(${review.data.total})` : ''}</h3>
        <Status loading={review.loading} error={review.error} />
        {review.data && review.data.suggestions.length === 0 && <p className="text-sm text-muted-foreground">Nada esperando revisão.</p>}
        {review.data && review.data.suggestions.length > 0 && (
          <div className="flex gap-2">
            <Button size="sm" disabled={busy} onClick={() => decide(review.data!.suggestions, true)}><Check className="mr-1 h-4 w-4" />Aceitar visíveis</Button>
            <Button size="sm" variant="outline" disabled={busy} onClick={() => decide(review.data!.suggestions, false)}><X className="mr-1 h-4 w-4" />Rejeitar visíveis</Button>
          </div>
        )}
        <ul className="space-y-2">
          {review.data?.suggestions.map((sg) => (
            <li key={sg.id} className="flex flex-wrap items-center gap-2 rounded-md border p-2 text-sm">
              <span className="min-w-0 flex-1 truncate" title={sg.path}>{sg.title || sg.path}</span>
              <Badge variant="secondary">{sg.kind}</Badge>
              <code className="max-w-xs truncate text-xs">{JSON.stringify(sg.value)}</code>
              {sg.confidence != null && <span className="text-xs text-muted-foreground">{Math.round(sg.confidence * 100)}%</span>}
              <Button size="icon" variant="ghost" className="h-8 w-8" aria-label="Aceitar" disabled={busy} onClick={() => decide([sg], true)}><Check className="h-4 w-4" /></Button>
              <Button size="icon" variant="ghost" className="h-8 w-8" aria-label="Rejeitar" disabled={busy} onClick={() => decide([sg], false)}><X className="h-4 w-4" /></Button>
            </li>
          ))}
        </ul>
      </section>
    </div>
  )
}
