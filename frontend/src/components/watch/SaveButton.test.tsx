import { act, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import type { WatchLaterResult } from '../../api/types'
import { SaveButton } from './SaveButton'

// API palsu, supaya tes tidak butuh server.
const setWatchLater = vi.fn<(id: string, saved: boolean) => Promise<WatchLaterResult>>()
vi.mock('../../api/endpoints', () => ({ setWatchLater: (id: string, saved: boolean) => setWatchLater(id, saved) }))

function renderButton(props: Partial<Parameters<typeof SaveButton>[0]> = {}) {
  render(
    <MemoryRouter initialEntries={['/watch/42']}>
      <SaveButton videoId="42" saved={false} loggedIn {...props} />
    </MemoryRouter>,
  )
}

describe('SaveButton', () => {
  // Pakai blok { }: fungsi yang dikembalikan beforeEach dianggap Vitest
  // sebagai fungsi pembersih dan dipanggil setelah tes, dan mockReset()
  // mengembalikan mock-nya sendiri (sebuah fungsi).
  beforeEach(() => {
    setWatchLater.mockReset()
  })

  it('tamu diarahkan ke halaman Masuk lalu kembali ke video', () => {
    renderButton({ loggedIn: false })
    expect(screen.getByRole('link', { name: 'Simpan' })).toHaveAttribute('href', '/login?next=%2Fwatch%2F42')
  })

  it('menyimpan ke Tonton nanti', async () => {
    setWatchLater.mockResolvedValue({ saved: true, added_at: '2026-11-11T08:00:00Z' })
    renderButton()
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Simpan' })))
    expect(setWatchLater).toHaveBeenCalledWith('42', true)
    expect(screen.getByRole('button', { name: 'Tersimpan' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('mengeluarkan dari Tonton nanti', async () => {
    setWatchLater.mockResolvedValue({ saved: false })
    renderButton({ saved: true })
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Tersimpan' })))
    expect(setWatchLater).toHaveBeenCalledWith('42', false)
    expect(screen.getByRole('button', { name: 'Simpan' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('jika gagal, kembali seperti semula dan menampilkan pesan', async () => {
    setWatchLater.mockRejectedValue(new ApiError(404, 'NOT_FOUND', 'video tidak ditemukan'))
    renderButton()
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Simpan' })))
    expect(setWatchLater).toHaveBeenCalledWith('42', true)
    expect(screen.getByRole('button', { name: 'Simpan' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('alert')).toHaveTextContent('video tidak ditemukan')
  })
})
