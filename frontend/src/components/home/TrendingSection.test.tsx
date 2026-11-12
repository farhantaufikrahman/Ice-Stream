import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { TrendingList, TrendingVideo } from '../../api/types'
import { TrendingSection } from './TrendingSection'

// API palsu: tes tidak butuh server.
const listTrending = vi.fn<(category: string, limit: number) => Promise<TrendingList>>()
vi.mock('../../api/endpoints', () => ({ listTrending: (c: string, l: number) => listTrending(c, l) }))

function trending(id: string, title: string, recent: number): TrendingVideo {
  return {
    id,
    owner_id: '7',
    title,
    description: '',
    status: 'READY',
    content_type: 'video/mp4',
    size_bytes: 1,
    created_at: '2026-11-01T00:00:00Z',
    view_count: recent * 10,
    comment_count: 0,
    like_count: 0,
    danmaku_count: id === '1' ? 1200 : 0,
    category: 'kuliner',
    tags: [],
    owner: { id: '7', username: 'kyrie', display_name: 'Dapur Kyrie', avatar_url: '' },
    recent_views: recent,
  }
}

function renderSection(category = '') {
  return render(
    <MemoryRouter>
      <TrendingSection category={category} />
    </MemoryRouter>,
  )
}

describe('TrendingSection', () => {
  // Pakai kurung kurawal: fungsi yang di-return dari beforeEach dianggap
  // Vitest sebagai "cleanup" dan dipanggil setelah tes (mockReset()
  // me-return mock itu sendiri).
  beforeEach(() => {
    listTrending.mockReset()
  })

  it('peringkat 1 menjadi sorotan, lalu daftar 5 teratas', async () => {
    listTrending.mockResolvedValue({ videos: [trending('1', 'Gelato tiga rasa', 1500), trending('2', 'Es campur', 30)] })
    renderSection('kuliner')
    expect(await screen.findByText('Sorotan minggu ini')).toBeInTheDocument()
    expect(listTrending).toHaveBeenCalledWith('kuliner', 5)
    expect(screen.getByRole('heading', { name: 'Trending Kuliner' })).toBeInTheDocument()
    expect(screen.getByText('Dapur Kyrie · 15 rb tayangan · 1,2 rb danmaku')).toBeInTheDocument()
    expect(screen.getByText('Dapur Kyrie · 1,5 rb tayangan minggu ini')).toBeInTheDocument()
    expect(screen.getByLabelText('Peringkat 2')).toHaveTextContent('2')
    expect(screen.getByRole('link', { name: 'Lihat semua' })).toHaveAttribute('href', '/trending?category=kuliner')
  })

  it('tidak tampil jika belum ada tayangan minggu ini', async () => {
    listTrending.mockResolvedValue({ videos: [] })
    const { container } = renderSection()
    await waitFor(() => expect(listTrending).toHaveBeenCalled())
    expect(container).toBeEmptyDOMElement()
  })

  it('tidak tampil jika gagal dimuat (bagian pelengkap)', async () => {
    listTrending.mockRejectedValue(new TypeError('Failed to fetch'))
    const { container } = renderSection()
    await waitFor(() => expect(listTrending).toHaveBeenCalled())
    expect(container).toBeEmptyDOMElement()
  })
})
