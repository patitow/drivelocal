// Revisão de tópicos duplicados: aceitar funde no canônico (com desfazer), rejeitar descarta.
import { useState } from 'react'
import { toast } from 'sonner'
import { Ban, Check, GitMerge, RefreshCw, Undo2, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { library, type TopicDupGroup, type TopicDupMember } from '@/lib/library'
import { Status, errorMessage, useLoad } from './catalogShared'

const KIND_LABEL: Record<string, string> = { series: 'Série', creator: 'Criador', collection: 'Coleção' }

function MemberRow({ m, canonical, onPick }: { m: TopicDupMember; canonical: boolean; onPick: () => void }) {
  return (
    <li className="flex flex-wrap items-start gap-2 text-sm">
      <label className="flex min-w-0 flex-1 items-start gap-2">
        <input type="radio" checked={canonical} onChange={onPick} aria-label={`Usar "${m.title}" como canônico`} className="mt-1" />
        <span className="min-w-0">
          <span className="font-medium">{m.title}</span>
          <span className="text-xs text-muted-foreground"> · {m.count} itens{m.score ? ` · ${Math.round(m.score * 100)}%` : ''}</span>
          {m.examples?.length > 0 && (
            <span className="block truncate text-xs text-muted-foreground" title={m.examples.join('\n')}>ex.: {m.examples.join(' · ')}</span>
          )}
        </span>
      </label>
      {canonical && <Badge variant="secondary">canônico</Badge>}
    </li>
  )
}

function DupGroupCard({ g, busy, onAccept, onReject }: {
  g: TopicDupGroup
  busy: boolean
  onAccept: (canonicalId: number, memberIds: number[]) => void
  onReject: (ids: number[]) => void
}) {
  const all = [g.canonical, ...g.members]
  const [canon, setCanon] = useState(g.canonical.id)
  const others = all.filter((m) => m.id !== canon).map((m) => m.id)
  return (
    <li className="space-y-2 rounded-md border p-3" data-testid="dup-group">
      <div className="flex items-center gap-2">
        <Badge variant="outline">{KIND_LABEL[g.kind] ?? g.kind}</Badge>
        <span className="text-xs text-muted-foreground">semelhança {Math.round(g.score * 100)}%</span>
      </div>
      <ul className="space-y-1">
        {all.map((m) => <MemberRow key={m.id} m={m} canonical={m.id === canon} onPick={() => setCanon(m.id)} />)}
      </ul>
      <div className="flex gap-2">
        <Button size="sm" disabled={busy} onClick={() => onAccept(canon, others)}><GitMerge className="mr-1 h-4 w-4" />Juntar</Button>
        <Button size="sm" variant="outline" disabled={busy} onClick={() => onReject(all.map((m) => m.id))}><X className="mr-1 h-4 w-4" />Não são o mesmo</Button>
      </div>
    </li>
  )
}

export function TopicDupesReview() {
  const dupes = useLoad(['admin', 'topic-dupes'], () => library.admin.topicDupes())
  const merges = useLoad(['admin', 'topic-merges'], () => library.admin.topicMerges())
  const flags = useLoad(['admin', 'topic-flags'], () => library.admin.topicFlags())
  const blocks = useLoad(['admin', 'topic-blocks'], () => library.admin.topicBlocks())
  const [busy, setBusy] = useState(false)

  const run = async (fn: () => Promise<void>) => {
    setBusy(true)
    try {
      await fn()
    } catch (e) {
      toast.error(errorMessage(e))
    } finally {
      setBusy(false)
      dupes.reload()
      merges.reload()
      flags.reload()
      blocks.reload()
    }
  }
  const dissolve = (id: number) => run(async () => {
    const b = await library.admin.dissolveTopic(id)
    toast.success(`"${b.title}" extinto`, { action: { label: 'Desfazer', onClick: () => restore(b.kind, b.slug) } })
  })
  const keep = (id: number) => run(async () => { await library.admin.dismissTopicFlag(id) })
  const restore = (kind: string, slug: string) => run(async () => {
    await library.admin.restoreTopic(kind, slug)
    toast.success('Tópico de volta')
  })
  const flagged = flags.data ?? []
  const extinct = (blocks.data ?? []).slice(0, 20)
  const undo = (batchId: string) => run(async () => {
    await library.admin.undoTopicMerge(batchId)
    toast.success('Fusão desfeita')
  })
  const accept = (canonicalId: number, memberIds: number[]) => run(async () => {
    const r = await library.admin.acceptTopicDupes(canonicalId, memberIds)
    toast.success(`${r.merged} tópico(s) juntados`, { action: { label: 'Desfazer', onClick: () => undo(r.batchId) } })
  })
  const reject = (ids: number[]) => run(async () => { await library.admin.rejectTopicDupes(ids) })
  const scan = () => run(async () => {
    const r = await library.admin.scanTopicDupes()
    toast.success(`${r.suggested} nova(s) sugestão(ões) em ${r.topics} tópicos`)
  })

  const groups = dupes.data?.groups ?? []
  const history = (merges.data ?? []).slice(0, 10)
  return (
    <section className="space-y-3" aria-label="Tópicos duplicados">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="font-semibold">Tópicos duplicados {dupes.data ? `(${groups.length})` : ''}</h2>
        <Button size="sm" variant="outline" disabled={busy} onClick={scan}><RefreshCw className="mr-1 h-4 w-4" />Procurar</Button>
        {dupes.data && <span className="text-xs text-muted-foreground">limiar {Math.round(dupes.data.threshold * 100)}%</span>}
      </div>
      <Status loading={dupes.loading} error={dupes.error} />
      {dupes.data && groups.length === 0 && <p className="text-sm text-muted-foreground">Nenhuma duplicata pendente.</p>}
      <ul className="grid grid-cols-1 gap-2 lg:grid-cols-2">
        {groups.map((g) => (
          <DupGroupCard key={`${g.canonical.id}-${g.members.map((m) => m.id).join('-')}`} g={g} busy={busy} onAccept={accept} onReject={reject} />
        ))}
      </ul>
      {flagged.length > 0 && (
        <div className="space-y-2" data-testid="topic-flags">
          <h3 className="text-sm font-semibold">Marcados como inválidos ({flagged.length})</h3>
          <ul className="grid grid-cols-1 gap-2 lg:grid-cols-2">
            {flagged.map((f) => (
              <li key={f.topicId} className="flex flex-wrap items-center gap-2 rounded-md border p-3 text-sm">
                <Badge variant="outline">{KIND_LABEL[f.kind] ?? f.kind}</Badge>
                <span className="min-w-0 flex-1">
                  <span className="font-medium">{f.title}</span>
                  <span className="text-xs text-muted-foreground"> · {f.count} itens{f.by.length ? ` · por ${f.by.join(', ')}` : ''}</span>
                </span>
                <Button size="sm" variant="destructive" disabled={busy} onClick={() => dissolve(f.topicId)}><Ban className="mr-1 h-4 w-4" />Extinguir</Button>
                <Button size="sm" variant="outline" disabled={busy} onClick={() => keep(f.topicId)}>Manter</Button>
              </li>
            ))}
          </ul>
        </div>
      )}
      {extinct.length > 0 && (
        <details className="text-sm">
          <summary className="cursor-pointer text-muted-foreground">Extintos ({extinct.length})</summary>
          <ul className="mt-2 space-y-1">
            {extinct.map((b) => (
              <li key={`${b.kind}/${b.slug}`} className="flex flex-wrap items-center gap-2">
                <Ban className="h-4 w-4 text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate">{b.title} <span className="text-xs text-muted-foreground">· {b.count} itens</span></span>
                <Button size="sm" variant="ghost" disabled={busy} onClick={() => restore(b.kind, b.slug)} aria-label={`Desfazer extinção de ${b.title}`}>
                  <Undo2 className="mr-1 h-4 w-4" />Desfazer
                </Button>
              </li>
            ))}
          </ul>
        </details>
      )}
      {history.length > 0 && (
        <details className="text-sm">
          <summary className="cursor-pointer text-muted-foreground">Fusões recentes</summary>
          <ul className="mt-2 space-y-1">
            {history.map((m) => (
              <li key={m.batchId} className="flex flex-wrap items-center gap-2">
                <Check className="h-4 w-4 text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate">{m.merged.join(', ')} → <strong>{m.into}</strong></span>
                {m.undone ? (
                  <Badge variant="outline">desfeita</Badge>
                ) : (
                  <Button size="sm" variant="ghost" disabled={busy} onClick={() => undo(m.batchId)} aria-label={`Desfazer fusão em ${m.into}`}>
                    <Undo2 className="mr-1 h-4 w-4" />Desfazer
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </details>
      )}
    </section>
  )
}
