import { act, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ChannelResult, SubscriptionResult } from '../../api/types'
import { ChannelResultCard } from './ChannelResultCard'

const setSubscription = vi.fn<(username: string, on: boolean) => Promise<SubscriptionResult>>()
vi.mock('../../api/endpoints', () => ({ setSubscription: (u: string, on: boolean) => setSubscription(u, on) }))
let viewerId = '9'
vi.mock('../../auth/useAuth', () => ({ useAuth: () => ({ status: 'authenticated', user: { id: viewerId } }) }))

const budi: ChannelResult = {
  id: '7',
  username: 'budi',
  display_name: 'Dapur Budi',
  bio: 'Resep anak kos',
  avatar_url: '',
  created_at: '2026-10-01T00:00:00Z',
  subscriber_count: 1200,
  viewer_subscribed: false,
}

function renderCard(channel: ChannelResult = budi) {
  render(
    <MemoryRouter>
      <ChannelResultCard channel={channel} />
    </MemoryRouter>,
  )
}

describe('ChannelResultCard', () => {
  beforeEach(() => {
    viewerId = '9'
    setSubscription.mockReset()
  })

  it('menampilkan kreator dan link ke kanal', () => {
    renderCard({ ...budi, bio: 'Resep anak kos\nbaris kedua' })
    expect(screen.getByText('Kreator')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Dapur Budi' })).toHaveAttribute('href', '/channel/budi')
    // Hanya baris pertama bio.
    expect(screen.getByText(/@budi · 1,2 rb pengikut · Resep anak kos$/)).toBeInTheDocument()
  })

  it('Ikuti langsung memperbarui tombol dan jumlah pengikut', async () => {
    setSubscription.mockResolvedValue({ subscribed: true, subscriber_count: 1201 })
    renderCard()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Ikuti' }))
    })
    expect(setSubscription).toHaveBeenCalledWith('budi', true)
    expect(screen.getByRole('button', { name: 'Mengikuti' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('kanal sendiri: Kelola video, bukan Ikuti', () => {
    viewerId = '7'
    renderCard()
    expect(screen.getByRole('link', { name: 'Kelola video' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Ikuti' })).not.toBeInTheDocument()
  })
})
