import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import type { ReportReason } from '../../lib/report'
import { ReportDialog } from './ReportDialog'

// API palsu: tes tidak butuh server.
const reportVideo = vi.fn<(id: string, reason: ReportReason, details: string) => Promise<{ reported_at: string }>>()
vi.mock('../../api/endpoints', () => ({
  reportVideo: (id: string, reason: ReportReason, details: string) => reportVideo(id, reason, details),
}))

function renderDialog() {
  const onClose = vi.fn()
  render(<ReportDialog videoId="42" title="Bikin gelato" onClose={onClose} />)
  return onClose
}

describe('ReportDialog', () => {
  beforeEach(() => {
    reportVideo.mockReset()
  })

  it('tombol kirim baru aktif setelah alasan dipilih', async () => {
    reportVideo.mockResolvedValue({ reported_at: '2026-11-11T03:00:00Z' })
    renderDialog()
    const send = screen.getByRole('button', { name: 'Kirim laporan' })
    expect(send).toBeDisabled()
    fireEvent.click(screen.getByRole('radio', { name: 'Pelanggaran hak cipta' }))
    fireEvent.change(screen.getByLabelText(/Keterangan/), { target: { value: '  lagu saya  ' } })
    fireEvent.click(send)
    expect(await screen.findByText('Laporan terkirim')).toBeInTheDocument()
    expect(reportVideo).toHaveBeenCalledWith('42', 'copyright', 'lagu saya')
  })

  it('alasan Lainnya wajib diberi keterangan', () => {
    renderDialog()
    fireEvent.click(screen.getByRole('radio', { name: 'Lainnya' }))
    expect(screen.getByText('Ceritakan singkat apa masalahnya.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Kirim laporan' })).toBeDisabled()
    fireEvent.change(screen.getByLabelText(/Keterangan/), { target: { value: 'judul menipu' } })
    expect(screen.getByRole('button', { name: 'Kirim laporan' })).toBeEnabled()
  })

  it('menampilkan pesan dari server jika gagal', async () => {
    reportVideo.mockRejectedValue(new ApiError(400, 'INVALID_ARGUMENT', 'video_id: tidak bisa melaporkan video sendiri'))
    renderDialog()
    fireEvent.click(screen.getByRole('radio', { name: 'Spam atau penipuan' }))
    fireEvent.click(screen.getByRole('button', { name: 'Kirim laporan' }))
    expect(await screen.findByText(/tidak bisa melaporkan video sendiri/)).toBeInTheDocument()
    expect(screen.queryByText('Laporan terkirim')).not.toBeInTheDocument()
  })
})
