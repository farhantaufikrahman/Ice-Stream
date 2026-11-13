import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import type { AuthStatus } from '../auth/context'
import { ResetPasswordPage } from './ResetPasswordPage'

// API dan status login palsu: tes tidak butuh server.
const resetPassword = vi.fn<(token: string, password: string) => Promise<void>>()
vi.mock('../api/endpoints', () => ({ resetPassword: (t: string, p: string) => resetPassword(t, p) }))
let authStatus: AuthStatus = 'anonymous'
const logout = vi.fn<() => Promise<void>>()
vi.mock('../auth/useAuth', () => ({ useAuth: () => ({ status: authStatus, logout }) }))

function renderPage(url = '/reset-password?token=tok-123') {
  render(
    <MemoryRouter initialEntries={[url]}>
      <ResetPasswordPage />
    </MemoryRouter>,
  )
}

function fill(password: string, confirm = password) {
  fireEvent.change(screen.getByLabelText('Kata sandi baru'), { target: { value: password } })
  fireEvent.change(screen.getByLabelText('Ulangi kata sandi baru'), { target: { value: confirm } })
  fireEvent.click(screen.getByRole('button', { name: 'Simpan kata sandi' }))
}

describe('ResetPasswordPage', () => {
  beforeEach(() => {
    authStatus = 'anonymous'
    resetPassword.mockReset()
    logout.mockReset()
    logout.mockResolvedValue()
  })

  it('mengganti kata sandi memakai token dari tautan', async () => {
    resetPassword.mockResolvedValue()
    renderPage()
    fill('sandiBaru456')

    expect(await screen.findByRole('heading', { name: 'Kata sandi diganti' })).toBeInTheDocument()
    expect(resetPassword).toHaveBeenCalledWith('tok-123', 'sandiBaru456')
    expect(screen.getByRole('link', { name: 'Masuk' })).toHaveAttribute('href', '/login')
    expect(logout).not.toHaveBeenCalled()
  })

  it('tab yang sedang login ikut dikeluarkan', async () => {
    authStatus = 'authenticated'
    resetPassword.mockResolvedValue()
    renderPage()
    fill('sandiBaru456')
    expect(await screen.findByRole('heading', { name: 'Kata sandi diganti' })).toBeInTheDocument()
    expect(logout).toHaveBeenCalledOnce()
  })

  it('kedua isian harus sama', () => {
    renderPage()
    fill('sandiBaru456', 'sandiBaru457')
    expect(screen.getByRole('alert')).toHaveTextContent('Kedua kata sandi tidak sama.')
    expect(resetPassword).not.toHaveBeenCalled()
  })

  it('tautan kedaluwarsa menawarkan tautan baru', async () => {
    resetPassword.mockRejectedValue(new ApiError(409, 'FAILED_PRECONDITION', 'tautan reset tidak valid'))
    renderPage()
    fill('sandiBaru456')
    expect(await screen.findByRole('heading', { name: 'Tautan tidak berlaku' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Minta tautan baru' })).toHaveAttribute('href', '/forgot-password')
  })

  it('aturan kata sandi dari server ditampilkan', async () => {
    resetPassword.mockRejectedValue(new ApiError(400, 'INVALID_ARGUMENT', 'password: minimal 8 karakter dan maksimal 72 byte'))
    renderPage()
    fill('pendek')
    expect(await screen.findByRole('alert')).toHaveTextContent('minimal 8 karakter')
  })

  it('tanpa token langsung meminta tautan baru', () => {
    renderPage('/reset-password')
    expect(screen.getByRole('heading', { name: 'Tautan tidak berlaku' })).toBeInTheDocument()
    expect(screen.getByText(/tidak lengkap/)).toBeInTheDocument()
  })
})
