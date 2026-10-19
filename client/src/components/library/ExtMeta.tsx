// Sinopse, ano, gêneros e nota: a ficha do cadastro, ou a busca automática no
// AniList/TMDB (o servidor guarda em cache).
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ExternalLink } from 'lucide-react'
import { cn } from '@/lib/utils'
import { library, type ExtMeta } from '@/lib/library'

// kind null com ref: só a ficha do cadastro, sem busca automática.
export function useExtMeta(kind: 'anime' | 'manga' | 'movie' | null, title: string, ref?: string): ExtMeta | null {
  const q = useQuery({
    queryKey: ['ext-meta', kind, title, ref ?? ''],
    queryFn: () => library.meta(kind, title, ref),
    enabled: (!!kind || !!ref) && title.trim().length > 1,
    staleTime: Infinity,
    gcTime: 60 * 60_000,
    retry: false,
  })
  return q.data ?? null
}

export const SOURCE: Record<ExtMeta['source'], string> = {
  anilist: 'AniList', mal: 'MyAnimeList', kitsu: 'Kitsu', imdb: 'IMDb', tmdb: 'TMDB', tvmaze: 'TVmaze', wikipedia: 'Wikipédia', manual: 'Ficha',
}

export function MetaBlurb({ meta, tone = 'dark', accent, className }: { meta: ExtMeta; tone?: 'dark' | 'paper'; accent?: string; className?: string }) {
  const [open, setOpen] = useState(false)
  const muted = tone === 'paper' ? '#4A4740' : undefined
  const long = (meta.synopsis?.length ?? 0) > 260
  return (
    <div className={cn('max-w-3xl space-y-2.5', className)} data-testid="ext-meta">
      {meta.synopsis && (
        <p className={cn('whitespace-pre-line text-[15px] leading-relaxed', tone === 'dark' && 'text-white/80', !open && 'line-clamp-3')} style={{ color: muted }}>
          {meta.synopsis}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-[13px]" style={{ color: muted }}>
        {long && (
          <button type="button" onClick={() => setOpen((v) => !v)} className="font-semibold underline-offset-2 hover:underline" style={{ color: accent }}>
            {open ? 'Menos' : 'Ler mais'}
          </button>
        )}
        {meta.genres.slice(0, 4).map((g) => (
          <span key={g} className={cn('rounded-full border px-2 py-0.5', tone === 'dark' ? 'border-white/20 text-white/70' : 'border-[#181715]/30')}>{g}</span>
        ))}
        {meta.score ? <span className={tone === 'dark' ? 'text-white/70' : undefined}>★ {(meta.score / 10).toFixed(1)}</span> : null}
        {meta.url && (
          <a href={meta.url} target="_blank" rel="noreferrer noopener" className={cn('inline-flex items-center gap-1 hover:underline', tone === 'dark' && 'text-white/60')}>
            {SOURCE[meta.source] ?? meta.source}<ExternalLink className="h-3 w-3" />
          </a>
        )}
      </div>
    </div>
  )
}
