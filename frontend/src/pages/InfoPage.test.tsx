import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { describe, expect, it } from 'vitest'
import { InfoPage } from './InfoPage'

describe('InfoPage', () => {
  it('menampilkan judul dan tautan ke tiga halaman lain', () => {
    render(
      <MemoryRouter>
        <InfoPage topic="privasi" />
      </MemoryRouter>,
    )
    expect(screen.getByRole('heading', { level: 1, name: 'Privasi' })).toBeInTheDocument()
    expect(screen.getByText(/menghapusnya otomatis setelah 1 hari/)).toBeInTheDocument()
    const nav = screen.getByRole('navigation', { name: 'Halaman informasi lain' })
    expect(nav).toHaveTextContent('TentangPedoman komunitasBantuan')
    expect(screen.getByRole('link', { name: 'Bantuan' })).toHaveAttribute('href', '/bantuan')
  })
})
