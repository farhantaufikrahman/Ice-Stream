import { act, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { LiveChannel, StreamKeyResult } from '../api/types'
import { StudioLivePage } from './StudioLivePage'

const getMyLive = vi.fn<() => Promise<LiveChannel>>()
const resetStreamKey = vi.fn<() => Promise<StreamKeyResult>>()
const updateMyLive = vi.fn<(title: string, category: string) => Promise<LiveChannel>>()
vi.mock('../api/endpoints', () => ({
  getMyLive: () => getMyLive(),
  resetStreamKey: () => resetStreamKey(),
  updateMyLive: (t: string, c: string) => updateMyLive(t, c),
  createPublishToken: vi.fn(),
}))
vi.mock('../auth/useAuth', () => ({ useAuth: () => ({ status: 'authenticated', user: null }) }))

const channel: LiveChannel = {
  title: 'Siaran langsung',
  category: '',
  ingest_url: 'rtmp://localhost:1935/live',
  has_stream_key: false,
  current: null,
}

async function renderPage() {
  render(
    <MemoryRouter>
      <StudioLivePage />
    </MemoryRouter>,
  )
  await screen.findByRole('heading', { name: 'Detail siaran' })
}

describe('StudioLivePage', () => {
  beforeEach(() => {
    getMyLive.mockReset().mockResolvedValue(channel)
    resetStreamKey.mockReset()
    updateMyLive.mockReset()
  })

  it('kamera tidak bisa dibuka di luar secure context', async () => {
    await renderPage()
    expect(screen.getByText(/tidak bisa membuka kamera/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Mulai siaran' })).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent('Belum siaran')
  })

  it('OBS: URL server dan kunci stream yang hanya tampil sekali', async () => {
    resetStreamKey.mockResolvedValue({
      channel: { ...channel, has_stream_key: true, stream_key_created_at: '2026-11-12T07:00:00Z' },
      stream_key: '7?key=isk_rahasia',
    })
    await renderPage()
    fireEvent.click(screen.getByRole('tab', { name: 'Aplikasi (OBS)' }))
    expect(screen.getByLabelText('URL server (RTMP)')).toHaveValue('rtmp://localhost:1935/live')
    expect(screen.getByText('Kamu belum punya kunci stream.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Mulai siaran' })).not.toBeInTheDocument()

    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Buat kunci stream' })))
    const key = screen.getByLabelText('Kunci stream')
    expect(key).toHaveValue('7?key=isk_rahasia')
    expect(key).toHaveAttribute('type', 'password')
    fireEvent.click(screen.getByRole('button', { name: 'Tampilkan' }))
    expect(key).toHaveAttribute('type', 'text')

    // Kunci sudah ada: membuat ulang harus dikonfirmasi.
    fireEvent.click(screen.getByRole('button', { name: 'Buat kunci baru' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Kunci lama langsung tidak berlaku')
    fireEvent.click(screen.getByRole('button', { name: 'Batal' }))
    expect(resetStreamKey).toHaveBeenCalledTimes(1)
  })

  it('menyimpan judul dan kategori', async () => {
    updateMyLive.mockResolvedValue({ ...channel, title: 'Ngoding bareng', category: 'teknologi' })
    await renderPage()
    const save = screen.getByRole('button', { name: 'Simpan' })
    expect(save).toBeDisabled()
    fireEvent.change(screen.getByLabelText('Judul'), { target: { value: 'Ngoding bareng' } })
    fireEvent.change(screen.getByLabelText('Kategori'), { target: { value: 'teknologi' } })
    await act(async () => fireEvent.click(save))
    expect(updateMyLive).toHaveBeenCalledWith('Ngoding bareng', 'teknologi')
    expect(screen.getByText('Tersimpan.')).toBeInTheDocument()
  })

  it('sedang live dari OBS: status, penonton, dan tautan halaman penonton', async () => {
    getMyLive.mockResolvedValue({
      ...channel,
      has_stream_key: true,
      current: {
        id: '21',
        owner_id: '7',
        title: 'Siaran langsung',
        category: '',
        status: 'LIVE',
        started_at: new Date().toISOString(),
        viewer_count: 12,
        peak_viewers: 20,
      },
    })
    await renderPage()
    expect(screen.getByRole('status')).toHaveTextContent('Sedang siaran')
    expect(screen.getByText('Diterima server')).toBeInTheDocument()
    expect(screen.getByText('Puncak penonton').nextElementSibling).toHaveTextContent('20')
    expect(screen.getByRole('link', { name: 'Lihat halaman live sebagai penonton' })).toHaveAttribute('href', '/live/21')
    // Browser tidak boleh siaran bersamaan dengan OBS.
    expect(screen.getByRole('button', { name: 'Mulai siaran' })).toBeDisabled()
  })
})
