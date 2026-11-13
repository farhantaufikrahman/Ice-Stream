import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { describe, expect, it } from 'vitest'
import { ModerationNotice } from './ModerationNotice'

describe('ModerationNotice', () => {
  it('tidak tampil untuk video biasa', () => {
    const { container } = render(<ModerationNotice video={{ visibility: 'PUBLIC' }} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('menampilkan alasan dan catatan admin', () => {
    render(
      <MemoryRouter>
        <ModerationNotice video={{ visibility: 'BLOCKED', moderation: { reason: 'copyright', note: 'lagu orang lain', blocked_at: '2026-11-12T00:00:00Z' } }} />
      </MemoryRouter>,
    )
    expect(screen.getByRole('status')).toHaveTextContent('Video ini diturunkan admin')
    expect(screen.getByText('Pelanggaran hak cipta')).toBeInTheDocument()
    expect(screen.getByText(/lagu orang lain/)).toBeInTheDocument()
  })
})
