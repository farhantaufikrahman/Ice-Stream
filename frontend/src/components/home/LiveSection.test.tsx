import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { LiveFilter } from '../../api/endpoints'
import type { LiveStream } from '../../api/types'
import { LiveSection } from './LiveSection'

const listLive = vi.fn<(f: LiveFilter) => Promise<LiveStream[]>>()
vi.mock('../../api/endpoints', () => ({ listLive: (f: LiveFilter) => listLive(f) }))

const stream: LiveStream = {
  id: '3',
  owner_id: '7',
  owner: { id: '7', username: 'kyrie', display_name: 'Ngoding Santai', avatar_url: '' },
  title: 'Ngoding Ice-Stream bareng',
  category: 'teknologi',
  status: 'LIVE',
  started_at: '2026-11-12T07:00:00Z',
  playback_url: 'http://live.example/live/7/index.m3u8',
  viewer_count: 1204,
  peak_viewers: 1300,
}

function renderSection(category = '') {
  return render(
    <MemoryRouter>
      <LiveSection category={category} />
    </MemoryRouter>,
  )
}

describe('LiveSection', () => {
  beforeEach(() => {
    listLive.mockReset()
  })

  it('kartu siaran dengan jumlah penonton, menuju halaman live', async () => {
    listLive.mockResolvedValue([stream])
    renderSection('teknologi')
    const card = await screen.findByRole('link', { name: /Ngoding Ice-Stream bareng/ })
    expect(card).toHaveAttribute('href', '/live/3')
    expect(card).toHaveTextContent('1,2 rb menonton')
    expect(card).toHaveTextContent('Ngoding Santai')
    expect(screen.getByRole('heading', { name: 'Sedang live' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Lihat semua' })).toHaveAttribute('href', '/live?category=teknologi')
    expect(listLive).toHaveBeenCalledWith({ limit: 4, category: 'teknologi' })
  })

  it('tidak tampil jika tidak ada yang live atau gagal', async () => {
    listLive.mockResolvedValue([])
    const { container, unmount } = renderSection()
    await waitFor(() => expect(listLive).toHaveBeenCalled())
    expect(container).toBeEmptyDOMElement()
    unmount()

    listLive.mockRejectedValue(new Error('down'))
    const second = renderSection()
    await waitFor(() => expect(listLive).toHaveBeenCalledTimes(2))
    expect(second.container).toBeEmptyDOMElement()
  })
})
