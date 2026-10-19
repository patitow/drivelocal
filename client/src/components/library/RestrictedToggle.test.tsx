import '@testing-library/jest-dom/vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@/__tests__/render'
import { library } from '@/lib/library'
import { RestrictedToggle } from './RestrictedToggle'

vi.mock('@/lib/library', async (orig) => {
  const real = await orig<typeof import('@/lib/library')>()
  return { ...real, library: { ...real.library, prefs: vi.fn(), setRestrictedMode: vi.fn() } }
})
const prefs = library.prefs as unknown as ReturnType<typeof vi.fn>
const setRestricted = library.setRestrictedMode as unknown as ReturnType<typeof vi.fn>

describe('RestrictedToggle', () => {
  beforeEach(() => {
    prefs.mockReset()
    setRestricted.mockReset().mockResolvedValue({})
  })

  it('liga e desliga o modo restrito', async () => {
    prefs.mockResolvedValue({ autoResume: true, autoplay: true, restrictedAllowed: true, restrictedMode: true })
    render(<RestrictedToggle showLabels />)
    const btn = await screen.findByRole('button', { name: /Modo restrito/ })
    expect(btn).toHaveAttribute('aria-pressed', 'true')
    prefs.mockResolvedValue({ autoResume: true, autoplay: true, restrictedAllowed: true, restrictedMode: false })
    await userEvent.click(btn)
    expect(setRestricted).toHaveBeenCalledWith(false)
    expect(await screen.findByRole('button', { name: /Modo restrito/, pressed: false })).toBeInTheDocument()
  })

  it('não aparece para quem não pode ver explícito', async () => {
    prefs.mockResolvedValue({ autoResume: true, autoplay: true, restrictedAllowed: false })
    render(<RestrictedToggle showLabels />)
    await vi.waitFor(() => expect(prefs).toHaveBeenCalled())
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})
