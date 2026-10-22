// Escolha de capa ou banner da ficha: da busca (link da web), colando um link,
// enviando do computador ou recortando um item da biblioteca (imagem ou quadro
// de vídeo, no modal). Tudo vira cópia local no servidor; links da web são baixados ao salvar.
import { useRef, useState } from 'react'
import { toast } from 'sonner'
import { Film, Globe, HardDrive, ImageIcon, Link2, Loader2, Upload, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'
import { library, type LibraryItem } from '@/lib/library'
import { errorMessage } from './catalogShared'
import { ArtCropModal } from './ArtCropModal'

export const isLocalArt = (v?: string) => !!v && v.startsWith('/api/art/')

export function ArtPicker({ label, value, onChange, items, wide, round }: {
  label: string
  value: string
  onChange: (v: string) => void
  // itens da entrada que podem dar a arte
  items: LibraryItem[]
  // banner (largo) ou capa (em pé)
  wide?: boolean
  // foto de criador (redonda, quadrada no recorte)
  round?: boolean
}) {
  const [mode, setMode] = useState<'' | 'link' | 'items'>('')
  const [link, setLink] = useState('')
  const [busy, setBusy] = useState(false)
  const file = useRef<HTMLInputElement>(null)

  const run = async (p: Promise<{ url: string }>) => {
    setBusy(true)
    try {
      onChange((await p).url)
      setMode('')
    } catch (e) {
      toast.error(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-2 rounded-xl border p-3">
      <div className="flex items-center gap-2">
        <h4 className="flex-1 text-sm font-semibold">{label}</h4>
        {value && (
          <span className={cn('inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px]',
            isLocalArt(value) ? 'bg-emerald-500/15 text-emerald-300' : 'bg-amber-500/15 text-amber-300')}
            title={isLocalArt(value) ? 'Guardada no servidor' : 'Será baixada para o servidor ao salvar'}>
            {isLocalArt(value) ? <><HardDrive className="h-3 w-3" />local</> : <><Globe className="h-3 w-3" />da web</>}
          </span>
        )}
      </div>
      <div className={cn('relative overflow-hidden rounded-lg bg-muted', wide ? 'aspect-[16/6]' : 'aspect-[2/3] max-h-56')}>
        {value
          ? <img src={value} alt={label} referrerPolicy="no-referrer" className="h-full w-full object-cover" />
          : <span className="flex h-full w-full items-center justify-center text-muted-foreground"><ImageIcon className="h-6 w-6" /></span>}
        {busy && <span className="absolute inset-0 flex items-center justify-center bg-black/50"><Loader2 className="h-6 w-6 animate-spin" /></span>}
      </div>
      <div className="flex flex-wrap gap-1.5">
        <Button type="button" size="sm" variant={mode === 'items' ? 'secondary' : 'outline'} disabled={busy || items.length === 0}
          title={items.length ? 'Usar uma imagem ou um quadro de vídeo desta entrada' : 'A entrada ainda não tem itens'}
          onClick={() => setMode('items')}><Film className="mr-1 h-4 w-4" />Dos itens</Button>
        <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => file.current?.click()}>
          <Upload className="mr-1 h-4 w-4" />Do computador
        </Button>
        <Button type="button" size="sm" variant={mode === 'link' ? 'secondary' : 'outline'} disabled={busy} onClick={() => setMode(mode === 'link' ? '' : 'link')}>
          <Link2 className="mr-1 h-4 w-4" />Link
        </Button>
        {value && (
          <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={() => onChange('')}>
            <X className="mr-1 h-4 w-4" />Remover
          </Button>
        )}
        <input ref={file} type="file" accept="image/jpeg,image/png,image/webp,image/gif" hidden aria-label={`Enviar ${label.toLowerCase()}`}
          onChange={(e) => {
            const f = e.target.files?.[0]
            e.target.value = ''
            if (f) void run(library.admin.uploadArt(f))
          }} />
      </div>
      {mode === 'link' && (
        <form className="flex gap-2" onSubmit={(e) => {
          e.preventDefault()
          if (link.trim()) void run(library.admin.storeArt({ url: link.trim() })).then(() => setLink(''))
        }}>
          <Input value={link} onChange={(e) => setLink(e.target.value)} placeholder="https://…" aria-label={`Link da ${label.toLowerCase()}`} className="flex-1" />
          <Button type="submit" size="sm" disabled={busy || !link.trim()}>Baixar</Button>
        </form>
      )}
      {mode === 'items' && (
        <ArtCropModal label={label} items={items} aspect={round ? 1 : wide ? 16 / 6 : 2 / 3}
          onDone={(url) => onChange(url)} onClose={() => setMode('')} />
      )}
    </div>
  )
}
