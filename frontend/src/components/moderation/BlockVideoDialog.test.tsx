import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import type { BlockResult } from '../../api/types'
import type { ReportReason } from '../../lib/report'
import { BlockVideoDialog } from './BlockVideoDialog'

const blockVideo = vi.fn<(id: string, reason: ReportReason, note: string) => Promise<BlockResult>>()
vi.mock('../../api/endpoints', () => ({
  blockVideo: (id: string, reason: ReportReason, note: string) => blockVideo(id, reason, note),
}))

describe('BlockVideoDialog', () => {
  beforeEach(() => {
    blockVideo.mockReset()
  })

  it('alasan Lainnya wajib diberi catatan', () => {
    render(<BlockVideoDialog videoId="42" title="Gelato" onClose={vi.fn()} onBlocked={vi.fn()} />)
    const send = screen.getByRole('button', { name: 'Turunkan', hidden: true })
    expect(send).toBeDisabled()
    fireEvent.click(screen.getByRole('radio', { name: 'Lainnya', hidden: true }))
    expect(send).toBeDisabled()
    fireEvent.change(screen.getByLabelText(/Catatan untuk kreator/), { target: { value: 'judul menipu' } })
    expect(send).toBeEnabled()
  })

  it('menampilkan pesan server jika video sudah diturunkan', async () => {
    blockVideo.mockRejectedValue(new ApiError(409, 'FAILED_PRECONDITION', 'video sudah diturunkan'))
    const onBlocked = vi.fn()
    render(<BlockVideoDialog videoId="42" title="Gelato" suggested="spam" onClose={vi.fn()} onBlocked={onBlocked} />)
    fireEvent.click(screen.getByRole('button', { name: 'Turunkan', hidden: true }))
    expect(await screen.findByText(/video sudah diturunkan/)).toBeInTheDocument()
    expect(onBlocked).not.toHaveBeenCalled()
    expect(blockVideo).toHaveBeenCalledWith('42', 'spam', '')
  })
})
