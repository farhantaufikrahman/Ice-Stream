import { act, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Video } from '../../api/types'
import { RecommendedSection } from './RecommendedSection'

// API palsu: tes tidak butuh server.
const listRecommended = vi.fn<(seed: string, category: string, limit: number) => Promise<Video[]>>()
vi.mock('../../api/endpoints', () => ({
  listRecommended: (s: string, c: string, l: number) => listRecommended(s, c, l),
}))
vi.mock('../../auth/useAuth', () => ({ useAuth: () => ({ status: 'guest', user: null }) }))

function video(id: string, title: string): Video {
  return {
    id,
    owner_id: '7',
    title,
    description: '',
    status: 'READY',
    content_type: 'video/mp4',
    size_bytes: 1,
    created_at: '2026-11-01T00:00:00Z',
    view_count: 10,
    comment_count: 0,
    like_count: 0,
    danmaku_count: 0,
    category: 'kuliner',
    tags: [],
    visibility: 'PUBLIC',
    draft: false,
    language: '',
    custom_thumbnail: false,
    owner: { id: '7', username: 'kyrie', display_name: 'Dapur Kyrie', avatar_url: '' },
  }
}

function renderSection(category = '') {
  return render(
    <MemoryRouter>
      <RecommendedSection category={category} />
    </MemoryRouter>,
  )
}

describe('RecommendedSection', () => {
  beforeEach(() => {
    listRecommended.mockReset()
    sessionStorage.clear()
  })

  it('menampilkan 12 rekomendasi dan memakai seed yang sama setelah dimuat ulang', async () => {
    listRecommended.mockResolvedValue([video('1', 'Gelato tiga rasa'), video('2', 'Es krim kopi')])
    const { unmount } = renderSection()
    expect(await screen.findByText('Gelato tiga rasa')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Rekomendasi untukmu' })).toBeInTheDocument()
    const [seed, category, limit] = listRecommended.mock.calls[0]
    expect(seed).toMatch(/^\d+$/)
    expect(category).toBe('')
    expect(limit).toBe(12)

    // Halaman dibuka lagi di tab yang sama: seed sama, susunan sama.
    unmount()
    renderSection()
    await screen.findByText('Gelato tiga rasa')
    expect(listRecommended.mock.calls[1][0]).toBe(seed)
  })

  it('Muat ulang meminta susunan baru dengan seed lain', async () => {
    listRecommended.mockResolvedValue([video('1', 'Gelato tiga rasa')])
    renderSection('kuliner')
    await screen.findByText('Gelato tiga rasa')
    expect(screen.getByRole('heading', { name: 'Rekomendasi Kuliner untukmu' })).toBeInTheDocument()
    const first = listRecommended.mock.calls[0][0]

    listRecommended.mockResolvedValue([video('3', 'Mi ayam')])
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Muat ulang' })))
    expect(await screen.findByText('Mi ayam')).toBeInTheDocument()
    expect(listRecommended).toHaveBeenCalledTimes(2)
    expect(listRecommended.mock.calls[1][0]).not.toBe(first)
    expect(listRecommended.mock.calls[1][1]).toBe('kuliner')
  })

  it('tidak ditampilkan jika belum ada video', async () => {
    listRecommended.mockResolvedValue([])
    renderSection()
    await act(async () => {})
    expect(screen.queryByRole('heading', { name: 'Rekomendasi untukmu' })).not.toBeInTheDocument()
  })
})
