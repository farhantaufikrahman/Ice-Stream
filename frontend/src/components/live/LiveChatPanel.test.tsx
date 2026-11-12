import { act, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import type { LiveChatMessage } from '../../api/types'
import { LiveChatPanel } from './LiveChatPanel'

const sendLiveChat = vi.fn<(id: string, body: string, color: string) => Promise<LiveChatMessage>>()
vi.mock('../../api/endpoints', () => ({ sendLiveChat: (id: string, b: string, c: string) => sendLiveChat(id, b, c) }))

const msg = (id: string, userId: string, name: string, body: string): LiveChatMessage => ({
  id,
  stream_id: '3',
  user: { id: userId, username: name.toLowerCase(), display_name: name, avatar_url: '' },
  body,
  color: '#FFFFFF',
  created_at: '2026-11-12T07:00:00Z',
})

function renderPanel(props: Partial<Parameters<typeof LiveChatPanel>[0]> = {}) {
  const onSent = vi.fn()
  render(
    <MemoryRouter>
      <LiveChatPanel
        streamId="3"
        ownerId="7"
        viewerId="9"
        live
        loaded
        messages={[msg('1', '7', 'Kreator', 'Halo semua'), msg('2', '9', 'Saya', 'hadir')]}
        onSent={onSent}
        {...props}
      />
    </MemoryRouter>,
  )
  return onSent
}

describe('LiveChatPanel', () => {
  beforeEach(() => {
    sendLiveChat.mockReset()
  })

  it('nama pengirim dengan tanda Kreator dan Kamu', () => {
    renderPanel()
    expect(screen.getByText('Halo semua').closest('p')).toHaveTextContent('KreatorKreator Halo semua')
    expect(screen.getByText('hadir').closest('p')).toHaveTextContent('SayaKamu hadir')
    expect(screen.getByText('Mode lambat · 3 detik')).toBeInTheDocument()
  })

  it('mengirim pesan dengan warna terpilih', async () => {
    const sent = msg('3', '9', 'Saya', 'mantap')
    sendLiveChat.mockResolvedValue(sent)
    const onSent = renderPanel()
    fireEvent.click(screen.getByRole('radio', { name: 'Mint' }))
    fireEvent.change(screen.getByLabelText('Tulis pesan chat'), { target: { value: '  mantap ' } })
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Kirim' })))
    expect(sendLiveChat).toHaveBeenCalledWith('3', 'mantap', '#4FE3C1')
    expect(onSent).toHaveBeenCalledWith(sent)
    expect(screen.getByLabelText('Tulis pesan chat')).toHaveValue('')
  })

  it('pesan error dari server (mode lambat)', async () => {
    sendLiveChat.mockRejectedValue(new ApiError(429, 'RESOURCE_EXHAUSTED', 'tunggu sebentar sebelum mengirim pesan lagi'))
    renderPanel()
    fireEvent.change(screen.getByLabelText('Tulis pesan chat'), { target: { value: 'lagi' } })
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Kirim' })))
    expect(screen.getByRole('alert')).toHaveTextContent('tunggu sebentar')
  })

  it('belum login: ajakan masuk', () => {
    renderPanel({ viewerId: '' })
    expect(screen.getByRole('link', { name: 'Masuk' })).toBeInTheDocument()
    expect(screen.queryByLabelText('Tulis pesan chat')).not.toBeInTheDocument()
  })

  it('siaran selesai tanpa kotak kirim', () => {
    renderPanel({ live: false, messages: [] })
    expect(screen.getByText('Siaran selesai')).toBeInTheDocument()
    expect(screen.getByText('Tidak ada pesan di siaran ini.')).toBeInTheDocument()
    expect(screen.queryByLabelText('Tulis pesan chat')).not.toBeInTheDocument()
  })
})
