import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import { ForgotPasswordPage } from './ForgotPasswordPage'

// API palsu: tes tidak butuh server.
const requestPasswordReset = vi.fn<(email: string) => Promise<void>>()
vi.mock('../api/endpoints', () => ({ requestPasswordReset: (email: string) => requestPasswordReset(email) }))

function renderPage() {
  render(
    <MemoryRouter initialEntries={['/forgot-password']}>
      <ForgotPasswordPage />
    </MemoryRouter>,
  )
}

describe('ForgotPasswordPage', () => {
  beforeEach(() => {
    requestPasswordReset.mockReset()
  })

  it('setelah dikirim, tidak mengatakan apakah email terdaftar', async () => {
    requestPasswordReset.mockResolvedValue()
    renderPage()
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: '  budi@example.com ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kirim tautan' }))

    expect(await screen.findByRole('heading', { name: 'Cek email kamu' })).toBeInTheDocument()
    expect(requestPasswordReset).toHaveBeenCalledWith('budi@example.com')
    expect(screen.getByText(/Jika/)).toHaveTextContent('Jika budi@example.com terdaftar di Ice-Stream')

    fireEvent.click(screen.getByRole('button', { name: 'Kirim ke email lain' }))
    expect(screen.getByLabelText('Email')).toBeInTheDocument()
  })

  it('menampilkan pesan dari server jika gagal', async () => {
    requestPasswordReset.mockRejectedValue(new ApiError(400, 'INVALID_ARGUMENT', 'email: format email tidak valid'))
    renderPage()
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'budi@example' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kirim tautan' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('format email tidak valid')
    expect(screen.queryByRole('heading', { name: 'Cek email kamu' })).not.toBeInTheDocument()
  })
})
