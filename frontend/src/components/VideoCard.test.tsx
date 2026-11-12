import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { describe, expect, it } from 'vitest'
import type { Video } from '../api/types'
import { VideoCard } from './VideoCard'

const video: Video = {
  id: '42',
  owner_id: '7',
  title: 'Belajar Go dari nol',
  description: '',
  status: 'READY',
  content_type: 'video/mp4',
  size_bytes: 1024,
  created_at: new Date().toISOString(),
  duration_ms: 75_000,
  thumbnail_url: 'http://media.test/thumb.jpg',
  view_count: 1500,
  comment_count: 0,
  like_count: 3,
  danmaku_count: 0,
  category: 'game',
  tags: [],
  visibility: 'PUBLIC',
  draft: false,
  language: '',
  custom_thumbnail: false,
  owner: { id: '7', username: 'budi', display_name: 'Budi', avatar_url: '' },
}

describe('VideoCard', () => {
  it('menampilkan judul, channel, durasi, dan link ke halaman tonton', () => {
    render(
      <MemoryRouter>
        <VideoCard video={video} />
      </MemoryRouter>,
    )
    expect(screen.getByRole('link')).toHaveAttribute('href', '/watch/42')
    expect(screen.getByText('Belajar Go dari nol')).toBeInTheDocument()
    expect(screen.getByText(/Budi ·/)).toBeInTheDocument()
    expect(screen.getByText('1:15')).toBeInTheDocument()
    expect(screen.getByText(/▶ 1,5/)).toBeInTheDocument()
    expect(screen.getByText('BU')).toBeInTheDocument()
    expect(screen.getByText('Game')).toHaveClass('thumb-category')
  })

  it('tanpa kategori tidak ada label di thumbnail', () => {
    const { container } = render(
      <MemoryRouter>
        <VideoCard video={{ ...video, category: '' }} />
      </MemoryRouter>,
    )
    expect(container.querySelector('.thumb-category')).toBeNull()
  })
})

describe('VideoCard hideOwner', () => {
  it('di halaman kanal menampilkan jumlah tayangan, bukan nama dan avatar kanal', () => {
    render(
      <MemoryRouter>
        <VideoCard video={video} hideOwner />
      </MemoryRouter>,
    )
    expect(screen.getByText(/1,5 rb tayangan ·/)).toBeInTheDocument()
    expect(screen.queryByText(/Budi ·/)).not.toBeInTheDocument()
    expect(screen.queryByText('BU')).not.toBeInTheDocument()
    expect(screen.queryByText(/▶/)).not.toBeInTheDocument()
  })
})
