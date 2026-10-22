import { describe, expect, it } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { ArtFrame } from './ExtMeta'

const still = <span data-testid="still" />

describe('ArtFrame', () => {
  it('usa o banner da ficha', () => {
    const { container } = render(<ArtFrame art={{ banner: 'https://img/b.jpg', cover: 'https://img/c.jpg' }} fallback={still} />)
    expect(container.querySelectorAll('img')).toHaveLength(1)
    expect(container.querySelector('img')).toHaveAttribute('src', 'https://img/b.jpg')
  })

  it('sem banner, a capa sobre ela mesma desfocada', () => {
    const { container } = render(<ArtFrame art={{ cover: 'https://img/c.jpg' }} fallback={still} />)
    const imgs = container.querySelectorAll('img')
    expect(imgs).toHaveLength(2)
    imgs.forEach((i) => expect(i).toHaveAttribute('src', 'https://img/c.jpg'))
  })

  it('volta para a capa e depois para o still quando a imagem falha', () => {
    const { container } = render(<ArtFrame art={{ banner: 'https://img/b.jpg', cover: 'https://img/c.jpg' }} fallback={still} />)
    fireEvent.error(container.querySelector('img')!)
    const covers = container.querySelectorAll('img')
    expect(covers[1]).toHaveAttribute('src', 'https://img/c.jpg')
    fireEvent.error(covers[1])
    expect(screen.getByTestId('still')).toBeInTheDocument()
  })

  it('sem ficha, o still', () => {
    render(<ArtFrame fallback={still} />)
    expect(screen.getByTestId('still')).toBeInTheDocument()
  })
})
