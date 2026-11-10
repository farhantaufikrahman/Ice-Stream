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
  created_at: new Date().toISOString(),
  duration_ms: 75_000,
  thumbnail_url: 'http://media.test/thumb.jpg',
  view_count: 1500,
  comment_count: 0,
  like_count: 3,
  owner: { id: '7', username: 'budi', display_name: 'Budi', avatar_url: '' },
}

describe('VideoCard', () => {
  it('menampilkan judul, channel, durasi, dan link ke halaman tonton', () => {
    render(
      <MemoryRouter>
        <VideoCard video={video} />
      </MemoryRouter>,
    )
    const title = screen.getByRole('link', { name: 'Belajar Go dari nol' })
    expect(title).toHaveAttribute('href', '/watch/42')
    expect(screen.getByText('Budi')).toBeInTheDocument()
    expect(screen.getByText('1:15')).toBeInTheDocument()
    expect(screen.getByText(/x ditonton/)).toBeInTheDocument()
  })
})
