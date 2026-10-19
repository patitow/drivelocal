// Modo restrito na barra lateral: liga ou desliga o conteúdo explícito para quem
// tem permissão de vê-lo. Ao trocar, todas as listas recarregam.
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Eye, EyeOff } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { library } from '@/lib/library'
import { cn } from '@/lib/utils'

export function RestrictedToggle({ showLabels }: { showLabels: boolean }) {
  const qc = useQueryClient()
  const prefs = useQuery({ queryKey: ['me-prefs'], queryFn: () => library.prefs(), staleTime: 60_000 })
  const [busy, setBusy] = useState(false)
  if (!prefs.data?.restrictedAllowed) return null
  const on = !!prefs.data.restrictedMode
  const label = on ? 'modo restrito ligado' : 'modo restrito desligado'
  const toggle = async () => {
    setBusy(true)
    try {
      await library.setRestrictedMode(!on)
      qc.setQueryData(['me-prefs'], { ...prefs.data, restrictedMode: !on })
      await qc.invalidateQueries()
    } finally {
      setBusy(false)
    }
  }
  return (
    <Button
      variant="ghost"
      aria-pressed={on}
      disabled={busy}
      className={cn('w-full min-h-11', showLabels ? 'justify-start' : 'justify-center')}
      onClick={() => void toggle()}
      title={showLabels ? (on ? 'Esconder conteúdo explícito' : 'Mostrar conteúdo explícito') : label}
    >
      <span className="relative flex h-6 w-6 flex-shrink-0 items-center justify-center">
        {on ? <Eye className="h-6 w-6" /> : <EyeOff className="h-6 w-6 opacity-60" />}
      </span>
      {showLabels && (
        <span className="ml-2 flex flex-1 items-center justify-between whitespace-nowrap">
          Modo restrito
          <span className={cn('relative ml-3 inline-flex h-5 w-9 rounded-full transition-colors', on ? 'bg-[#FF5A5A]' : 'bg-muted-foreground/30')}>
            <span className={cn('absolute top-0.5 h-4 w-4 rounded-full bg-white shadow transition-transform', on ? 'translate-x-[18px]' : 'translate-x-0.5')} />
          </span>
        </span>
      )}
    </Button>
  )
}
