// Atalho do admin, nas páginas de série, filme, mangá e criador: abre a
// entrada no Gerenciar (ficha, capa, banner, itens).
import { useNavigate } from '@tanstack/react-router'
import { Pencil } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useAuthRole } from '@/contexts/AuthContext'

export function useEditEntry() {
  const navigate = useNavigate()
  return (ref: string) => void navigate({ to: '/colecao/$tab', params: { tab: 'manage' }, search: { editar: ref, volta: 1 } as never })
}

interface Props {
  entryRef?: string
  className?: string
  style?: React.CSSProperties
  // só o ícone (cards)
  compact?: boolean
}

export function EditEntryButton(p: Props) {
  // fora do app (estantes isoladas) não há login: sem botão
  if (useAuthRole() !== 'admin' || !p.entryRef) return null
  return <EditButton {...p} entryRef={p.entryRef} />
}

function EditButton({ entryRef, className, style, compact }: Props & { entryRef: string }) {
  const edit = useEditEntry()
  return (
    <button type="button" onClick={(e) => { e.stopPropagation(); edit(entryRef) }} aria-label="Editar no Gerenciar" title="Editar no Gerenciar"
      className={cn(compact
        ? 'flex h-9 w-9 items-center justify-center rounded-full bg-black/50 text-white hover:bg-black/70'
        : 'flex h-12 items-center gap-2 rounded-full border px-5 text-base font-medium hover:bg-secondary', className)} style={style}>
      <Pencil className="h-4 w-4" />{!compact && 'Editar'}
    </button>
  )
}
