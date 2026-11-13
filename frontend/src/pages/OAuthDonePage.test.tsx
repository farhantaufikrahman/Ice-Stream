import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import { OAuthDonePage } from './OAuthDonePage'

// Status login palsu: tes tidak butuh server.
const loginWithCode = vi.fn<(code: string) => Promise<void>>()
vi.mock('../auth/useAuth', () => ({ useAuth: () => ({ loginWithCode }) }))

function renderPage(url: string) {
  render(
    <MemoryRouter initialEntries={[url]}>
      <Routes>
        <Route path="/oauth/selesai" element={<OAuthDonePage />} />
        <Route path="/studio" element={<h1>Halaman studio</h1>} />
        <Route path="/" element={<h1>Beranda</h1>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('OAuthDonePage', () => {
  // Pakai kurung kurawal: fungsi yang dikembalikan beforeEach dijalankan
  // Vitest sebagai teardown, dan mockReset() mengembalikan mock-nya.
  beforeEach(() => {
    loginWithCode.mockReset()
  })

  it('menukar kode sekali lalu pindah ke next', async () => {
    loginWithCode.mockResolvedValue()
    renderPage('/oauth/selesai#code=abc123&next=%2Fstudio')
    expect(await screen.findByRole('heading', { name: 'Halaman studio' })).toBeInTheDocument()
    expect(loginWithCode).toHaveBeenCalledExactlyOnceWith('abc123')
  })

  it('next ke situs lain diganti beranda', async () => {
    loginWithCode.mockResolvedValue()
    renderPage('/oauth/selesai#code=abc123&next=%2F%2Fjahat.com')
    expect(await screen.findByRole('heading', { name: 'Beranda' })).toBeInTheDocument()
  })

  it('menampilkan pesan untuk kode error dari server', () => {
    renderPage('/oauth/selesai#error=email_needs_password&next=%2Fstudio')
    expect(screen.getByRole('heading', { name: 'Gagal masuk' })).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('Lupa kata sandi?')
    expect(screen.getByRole('link', { name: 'Kembali ke halaman Masuk' })).toHaveAttribute('href', '/login?next=%2Fstudio')
    expect(loginWithCode).not.toHaveBeenCalled()
  })

  it('kode kedaluwarsa: pesan dari server', async () => {
    loginWithCode.mockRejectedValue(new ApiError(401, 'unauthenticated', 'kode login tidak valid atau kedaluwarsa'))
    renderPage('/oauth/selesai#code=lama')
    expect(await screen.findByRole('alert')).toHaveTextContent('kedaluwarsa')
  })

  it('tanpa kode dan tanpa error dianggap gagal', () => {
    renderPage('/oauth/selesai')
    expect(screen.getByRole('alert')).toHaveTextContent('Gagal masuk lewat penyedia')
  })
})
