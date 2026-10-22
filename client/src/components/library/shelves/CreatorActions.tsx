// Ações no perfil de um criador: renomear, juntar com outro (mesma pessoa com
// outro nome) ou extinguir (não é um criador válido), e mover um vídeo para
// outro criador. Admin faz na hora, com desfazer; os demais usuários mandam a
// sugestão de juntar/extinguir para a Revisão.
import { useMemo, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { toast } from 'sonner'
import { Ban, GitMerge, MoreHorizontal, Pencil, Plus, Search, UserMinus, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { library, type LibraryItem, type TopicInfo } from '@/lib/library'
import { errorMessage } from '../catalogShared'

// sem acento e minúsculo, para a busca
const foldText = (s: string) => s.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase()

// aviso com desfazer fica mais que o padrão (4 s)
const UNDO_MS = 10_000
const MENU_ITEM = 'flex w-full items-center gap-2.5 rounded-lg px-3 py-2.5 text-left text-sm hover:bg-secondary'
const BTN = 'h-10 rounded-full border px-4 text-sm hover:bg-secondary'
const BTN_MAIN = 'h-10 rounded-full bg-foreground px-4 text-sm font-semibold text-background disabled:opacity-50'

// Painel: no celular encosta embaixo da tela; no desktop abre sob o botão.
// centered: no desktop fica no meio da tela (quando o botão está num cartão
// pequeno, um painel ancorado nele seria cortado).
function Panel({ label, title, onClose, centered, children }: { label: string; title: string; onClose: () => void; centered?: boolean; children: ReactNode }) {
  const panel = (
    <div role="dialog" aria-label={label} aria-modal={centered || undefined} onClick={(e) => e.stopPropagation()}
      className={cn('fixed inset-x-3 bottom-3 z-50 space-y-3 rounded-xl border bg-popover p-4 text-left text-popover-foreground shadow-xl sm:inset-x-auto sm:bottom-auto sm:w-[360px]',
        centered ? 'sm:left-1/2 sm:top-1/2 sm:-translate-x-1/2 sm:-translate-y-1/2' : 'sm:absolute sm:right-0 sm:top-12 sm:z-20')}>
      <div className="flex items-center justify-between gap-2">
        <p className="font-semibold">{title}</p>
        <button type="button" onClick={onClose} aria-label="Fechar" className="rounded-full p-1 hover:bg-secondary"><X className="h-4 w-4" /></button>
      </div>
      {children}
    </div>
  )
  if (!centered) return panel
  // centrado: fora do card (cada card tem seu z-index, e os botões dos
  // vizinhos ficavam por cima), com um fundo que segura os cliques
  return createPortal(
    <div className="dark fixed inset-0 z-[100] bg-black/50" onClick={(e) => { e.stopPropagation(); onClose() }}>{panel}</div>,
    document.body,
  )
}

// Busca de criador pelo nome ou apelido; com onCreate, oferece criar um novo
// com o nome digitado.
function CreatorPicker({ all, exclude, onPick, onCreate }: { all: TopicInfo[]; exclude: number; onPick: (t: TopicInfo) => void; onCreate?: (name: string) => void }) {
  const [q, setQ] = useState('')
  const name = q.trim()
  const matches = useMemo(() => {
    const f = foldText(name)
    return all.filter((o) => o.id !== exclude && (!f || foldText(o.title).includes(f) || o.aliases?.some((a) => foldText(a).includes(f)))).slice(0, 8)
  }, [all, name, exclude])
  const exact = all.some((o) => foldText(o.title) === foldText(name))
  return (
    <>
      <label className="flex items-center gap-2 rounded-lg border px-3">
        <Search className="h-4 w-4 shrink-0 text-muted-foreground" />
        <input autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder="Buscar criador" aria-label="Buscar criador"
          className="h-10 min-w-0 flex-1 bg-transparent text-sm outline-none" />
      </label>
      <ul className="max-h-64 space-y-0.5 overflow-y-auto">
        {matches.map((o) => (
          <li key={o.id}>
            <button type="button" onClick={() => onPick(o)}
              className="flex w-full items-center justify-between gap-2 rounded-lg px-3 py-2 text-left text-sm hover:bg-secondary">
              <span className="truncate">{o.title}</span>
              <span className="shrink-0 text-xs text-muted-foreground">{o.count} itens</span>
            </button>
          </li>
        ))}
        {onCreate && name && !exact && (
          <li>
            <button type="button" onClick={() => onCreate(name)} className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-sm hover:bg-secondary">
              <Plus className="h-4 w-4 shrink-0" /><span className="truncate">Novo criador "{name}"</span>
            </button>
          </li>
        )}
        {matches.length === 0 && !onCreate && <li className="px-3 py-2 text-sm text-muted-foreground">Nenhum criador com esse nome.</li>}
      </ul>
    </>
  )
}

type Mode = null | 'menu' | 'rename' | 'merge' | 'invalid'

interface Props {
  t: TopicInfo
  all: TopicInfo[]
  isAdmin: boolean
  // depois de juntar/extinguir: a estante recarrega e o perfil fecha
  onChanged: () => void
  // depois de renomear: recarrega e o perfil continua aberto
  onRefresh: () => void
}

export function CreatorActions({ t, all, isAdmin, onChanged, onRefresh }: Props) {
  const [mode, setMode] = useState<Mode>(null)
  const [target, setTarget] = useState<TopicInfo | null>(null)
  const [keep, setKeep] = useState<number | null>(null)
  const [name, setName] = useState(t.title)
  const [busy, setBusy] = useState(false)

  const close = () => { setMode(null); setTarget(null); setKeep(null); setName(t.title) }
  const run = async (fn: () => Promise<void>) => {
    setBusy(true)
    try {
      await fn()
      close()
    } catch (e) {
      toast.error(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  const rename = () => run(async () => {
    const title = name.trim()
    if (!title || title === t.title) return
    const old = t.title
    await library.admin.renameTopic(t.id, title)
    onRefresh()
    toast.success(`Agora é "${title}"`, {
      duration: UNDO_MS, action: { label: 'Desfazer', onClick: () => { library.admin.renameTopic(t.id, old, true).then(onRefresh, (e) => toast.error(errorMessage(e))) } },
    })
  })

  const merge = () => target && run(async () => {
    if (!isAdmin) {
      await library.suggestTopic(t.id, 'merge', target.id)
      toast.success('Sugestão enviada para a Revisão')
      return
    }
    const canon = keep ?? target.id
    const other = canon === t.id ? target.id : t.id
    const title = canon === t.id ? t.title : target.title
    const r = await library.admin.acceptTopicDupes(canon, [other])
    onChanged()
    toast.success(`Juntados em "${title}"`, {
      duration: UNDO_MS, action: { label: 'Desfazer', onClick: () => { library.admin.undoTopicMerge(r.batchId).then(onChanged, (e) => toast.error(errorMessage(e))) } },
    })
  })

  const dissolve = () => run(async () => {
    if (!isAdmin) {
      await library.suggestTopic(t.id, 'invalid')
      toast.success('Sugestão enviada para a Revisão')
      return
    }
    const b = await library.admin.dissolveTopic(t.id)
    onChanged()
    toast.success(`"${b.title}" saiu dos criadores`, {
      duration: UNDO_MS, action: { label: 'Desfazer', onClick: () => { library.admin.restoreTopic(b.kind, b.slug).then(onChanged, (e) => toast.error(errorMessage(e))) } },
    })
  })

  const verb = isAdmin ? '' : 'Sugerir: '
  return (
    <div className="relative">
      <button type="button" onClick={() => (mode ? close() : setMode('menu'))} aria-label="Mais ações" aria-expanded={mode !== null}
        className="flex h-11 w-11 items-center justify-center rounded-full border hover:bg-secondary">
        <MoreHorizontal className="h-5 w-5" />
      </button>
      {mode === 'menu' && (
        <div role="menu" className="absolute right-0 top-12 z-20 w-64 overflow-hidden rounded-xl border bg-popover p-1 text-popover-foreground shadow-xl">
          {isAdmin && (
            <button role="menuitem" type="button" onClick={() => setMode('rename')} className={MENU_ITEM}>
              <Pencil className="h-4 w-4" />Renomear…
            </button>
          )}
          <button role="menuitem" type="button" onClick={() => setMode('merge')} className={MENU_ITEM}>
            <GitMerge className="h-4 w-4" />{verb}Juntar com outro criador…
          </button>
          <button role="menuitem" type="button" onClick={() => setMode('invalid')} className={cn(MENU_ITEM, 'text-destructive')}>
            <Ban className="h-4 w-4" />{verb}Não é um criador válido
          </button>
        </div>
      )}
      {mode === 'rename' && (
        <Panel label="Renomear criador" title="Renomear criador" onClose={close}>
          <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); void rename() }}>
            <input autoFocus value={name} onChange={(e) => setName(e.target.value)} aria-label="Nome do criador"
              className="h-10 w-full rounded-lg border bg-transparent px-3 text-sm outline-none focus:border-foreground" />
            <p className="text-sm text-muted-foreground">O nome antigo vira apelido: a busca e as pastas com ele continuam caindo aqui.</p>
            <div className="flex justify-end gap-2">
              <button type="button" onClick={close} className={BTN}>Cancelar</button>
              <button type="submit" disabled={busy || !name.trim() || name.trim() === t.title} className={BTN_MAIN}>Renomear</button>
            </div>
          </form>
        </Panel>
      )}
      {mode === 'merge' && (
        <Panel label="Juntar criadores" title="Juntar com outro criador" onClose={close}>
          <p className="text-sm text-muted-foreground">Mesma pessoa com outro nome? Os itens dos dois ficam num só, e o outro nome vira apelido.</p>
          {!target ? (
            <CreatorPicker all={all} exclude={t.id} onPick={(o) => { setTarget(o); setKeep(o.count >= t.count ? o.id : t.id) }} />
          ) : (
            <>
              {isAdmin && (
                <fieldset className="space-y-1.5">
                  <legend className="pb-1 text-sm">Qual nome fica?</legend>
                  {[t, target].map((o) => (
                    <label key={o.id} className={cn('flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-sm', keep === o.id && 'border-foreground')}>
                      <input type="radio" name="keep" checked={keep === o.id} onChange={() => setKeep(o.id)} />
                      <span className="min-w-0 flex-1 truncate">{o.title}</span>
                      <span className="shrink-0 text-xs text-muted-foreground">{o.count} itens</span>
                    </label>
                  ))}
                </fieldset>
              )}
              {!isAdmin && <p className="text-sm">Juntar <strong>{t.title}</strong> com <strong>{target.title}</strong>.</p>}
              <div className="flex justify-end gap-2">
                <button type="button" onClick={() => setTarget(null)} className={BTN}>Voltar</button>
                <button type="button" disabled={busy} onClick={merge} className={BTN_MAIN}>
                  {isAdmin ? 'Juntar' : 'Enviar sugestão'}
                </button>
              </div>
            </>
          )}
        </Panel>
      )}
      {mode === 'invalid' && (
        <Panel label="Não é um criador" title="Não é um criador válido" onClose={close}>
          <p className="text-sm text-muted-foreground">
            {isAdmin
              ? `"${t.title}" sai da estante e não volta na próxima varredura. Os arquivos continuam onde estão. Dá para desfazer na Revisão.`
              : `O admin vai ver a sugestão na Revisão e decidir se "${t.title}" sai da estante.`}
          </p>
          <div className="flex justify-end gap-2">
            <button type="button" onClick={close} className={BTN}>Cancelar</button>
            <button type="button" disabled={busy} onClick={dissolve} className="h-10 rounded-full bg-destructive px-4 text-sm font-semibold text-destructive-foreground disabled:opacity-50">
              {isAdmin ? 'Tirar dos criadores' : 'Enviar sugestão'}
            </button>
          </div>
        </Panel>
      )}
    </div>
  )
}

// "⋯" sobre um vídeo do perfil (admin): mover para outro criador ou tirar
// deste. O scan não desfaz; o aviso tem desfazer.
export function CreatorItemMenu({ item, t, all, onChanged }: { item: LibraryItem; t: TopicInfo; all: TopicInfo[]; onChanged: () => void }) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const label = item.title || item.name

  const act = async (actions: Record<string, unknown>, done: string) => {
    setBusy(true)
    try {
      const r = await library.admin.setItem(item.id, actions)
      setOpen(false)
      onChanged()
      toast.success(done, {
        duration: UNDO_MS, action: { label: 'Desfazer', onClick: () => { library.admin.undoChanges(r.result.batchId).then(onChanged, (e) => toast.error(errorMessage(e))) } },
      })
    } catch (e) {
      toast.error(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  const moveTo = (topic: { id: number } | { kind: 'creator'; title: string }, title: string) =>
    act({ topic, remove_topic: { id: t.id } }, `Movido para "${title}"`)

  return (
    <div className="absolute right-2 top-2 z-10" onClick={(e) => e.stopPropagation()}>
      <button type="button" aria-label={`Mais ações: ${label}`} aria-expanded={open} onClick={() => setOpen(!open)}
        className={cn('flex h-9 w-9 items-center justify-center rounded-full bg-black/55 text-white backdrop-blur transition-opacity hover:bg-black/75 focus-visible:opacity-100',
          'opacity-100 [@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-hover/item:opacity-100', open && '[@media(hover:hover)]:opacity-100')}>
        <MoreHorizontal className="h-5 w-5" />
      </button>
      {open && (
        <Panel label="Mover para outro criador" title="Mover para outro criador" onClose={() => setOpen(false)} centered>
          <p className="truncate text-sm text-muted-foreground">{label}</p>
          <CreatorPicker all={all} exclude={t.id}
            onPick={(o) => !busy && moveTo({ id: o.id }, o.title)}
            onCreate={(name) => !busy && moveTo({ kind: 'creator', title: name }, name)} />
          <div className="border-t pt-3">
            <button type="button" disabled={busy} onClick={() => act({ remove_topic: { id: t.id } }, `Tirado de "${t.title}"`)}
              className="flex h-10 items-center gap-2 rounded-full border px-4 text-sm hover:bg-secondary disabled:opacity-50">
              <UserMinus className="h-4 w-4" />Só tirar de "{t.title}"
            </button>
          </div>
        </Panel>
      )}
    </div>
  )
}
