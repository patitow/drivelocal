import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ArtCropModal } from './ArtCropModal'

describe('ArtCropModal', () => {
  it('fecha ao clicar fora, mas não ao clicar dentro do diálogo', () => {
    const onClose = vi.fn()
    render(<ArtCropModal label="Capa" items={[]} aspect={2 / 3} onDone={() => {}} onClose={onClose} />)
    fireEvent.click(screen.getByRole('dialog'))
    expect(onClose).not.toHaveBeenCalled()
    fireEvent.click(screen.getByTestId('art-crop-backdrop'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
