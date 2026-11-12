import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { BlockedVideoPage, BlockResult, Profile, ReportedVideoPage, Video, VideoReport } from '../api/types'
import type { ReportReason } from '../lib/report'
import { StudioModerationPage } from './StudioModerationPage'

const listReportedVideos = vi.fn<(token: string) => Promise<ReportedVideoPage>>()
const listVideoReports = vi.fn<(id: string) => Promise<VideoReport[]>>()
const blockVideo = vi.fn<(id: string, reason: ReportReason, note: string) => Promise<BlockResult>>()
const dismissReports = vi.fn<(id: string) => Promise<{ closed_reports: number }>>()
const unblockVideo = vi.fn<(id: string) => Promise<Video>>()
const listBlockedVideos = vi.fn<(token: string) => Promise<BlockedVideoPage>>()
vi.mock('../api/endpoints', () => ({
  listReportedVideos: (t: string) => listReportedVideos(t),
  listVideoReports: (id: string) => listVideoReports(id),
  blockVideo: (id: string, r: ReportReason, n: string) => blockVideo(id, r, n),
  dismissReports: (id: string) => dismissReports(id),
  unblockVideo: (id: string) => unblockVideo(id),
  listBlockedVideos: (t: string) => listBlockedVideos(t),
}))

let user: Profile | null = null
vi.mock('../auth/useAuth', () => ({ useAuth: () => ({ status: 'authenticated', user }) }))

const profile: Profile = {
  id: '1',
  username: 'admin',
  display_name: 'Admin',
  bio: '',
  avatar_url: '',
  created_at: '2026-11-01T00:00:00Z',
  subscriber_count: 0,
  role: 'ADMIN',
}

const video: Video = {
  id: '42',
  owner_id: '7',
  title: 'Gelato murah',
  description: '',
  status: 'READY',
  content_type: 'video/mp4',
  size_bytes: 1024,
  created_at: '2026-11-10T00:00:00Z',
  view_count: 0,
  comment_count: 0,
  like_count: 0,
  danmaku_count: 0,
  category: '',
  tags: [],
  visibility: 'PUBLIC',
  draft: false,
  language: '',
  custom_thumbnail: false,
  owner: { id: '7', username: 'budi', display_name: 'Budi', avatar_url: '' },
}

const queue: ReportedVideoPage = {
  videos: [
    {
      video,
      open_reports: 3,
      reasons: [
        { reason: 'spam', count: 2 },
        { reason: 'hate', count: 1 },
      ],
      first_reported_at: '2026-11-11T00:00:00Z',
      last_reported_at: '2026-11-12T00:00:00Z',
    },
  ],
  next_page_token: '',
}

function renderPage(path = '/studio/moderation') {
  render(
    <MemoryRouter initialEntries={[path]}>
      <StudioModerationPage />
    </MemoryRouter>,
  )
}

describe('StudioModerationPage', () => {
  beforeEach(() => {
    user = profile
    listReportedVideos.mockReset().mockResolvedValue(queue)
    listVideoReports.mockReset()
    blockVideo.mockReset()
    dismissReports.mockReset()
    unblockVideo.mockReset()
    listBlockedVideos.mockReset()
  })
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('user biasa tidak memuat apa pun', () => {
    user = { ...profile, role: 'USER' }
    renderPage()
    expect(screen.getByText('Halaman ini hanya untuk admin.')).toBeInTheDocument()
    expect(listReportedVideos).not.toHaveBeenCalled()
  })

  it('antrean laporan: alasan, jumlah, dan detail laporan', async () => {
    listVideoReports.mockResolvedValue([
      {
        reporter: { id: '9', username: 'sari', display_name: 'Sari', avatar_url: '' },
        reporter_id: '9',
        reason: 'spam',
        details: 'link judi',
        reported_at: '2026-11-12T00:00:00Z',
      },
    ])
    renderPage()
    expect(await screen.findByText('3 laporan')).toBeInTheDocument()
    expect(screen.getByText('Spam atau penipuan · 2')).toBeInTheDocument()
    expect(screen.getByText('Ujaran kebencian · 1')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Lihat laporan' }))
    expect(await screen.findByText('link judi')).toBeInTheDocument()
    expect(screen.getByText('@sari')).toBeInTheDocument()
    expect(listVideoReports).toHaveBeenCalledWith('42')
  })

  it('Turunkan: alasan terbanyak sudah terpilih, baris hilang setelah berhasil', async () => {
    blockVideo.mockResolvedValue({
      blocked: { video: { ...video, visibility: 'BLOCKED' }, reason: 'spam', note: '', blocked_at: '2026-11-12T01:00:00Z' },
      closed_reports: 3,
    })
    renderPage()
    fireEvent.click(await screen.findByRole('button', { name: 'Turunkan Gelato murah' }))
    const dialog = screen.getByRole('dialog', { hidden: true })
    expect(within(dialog).getByRole('radio', { name: 'Spam atau penipuan', hidden: true })).toBeChecked()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Turunkan', hidden: true }))
    expect(await screen.findByText('"Gelato murah" diturunkan. 3 laporan ditutup.')).toBeInTheDocument()
    expect(blockVideo).toHaveBeenCalledWith('42', 'spam', '')
    expect(screen.getByText('Tidak ada laporan yang perlu ditinjau.')).toBeInTheDocument()
  })

  it('Tolak laporan meminta konfirmasi', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true)
    dismissReports.mockResolvedValue({ closed_reports: 3 })
    renderPage()
    const btn = await screen.findByRole('button', { name: 'Tolak laporan untuk Gelato murah' })
    fireEvent.click(btn)
    expect(dismissReports).not.toHaveBeenCalled()
    fireEvent.click(btn)
    expect(await screen.findByText('3 laporan ditolak. "Gelato murah" tetap tayang.')).toBeInTheDocument()
    expect(confirm).toHaveBeenCalledTimes(2)
  })

  it('tab Diturunkan: Pulihkan', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    listBlockedVideos.mockResolvedValue({
      videos: [
        {
          video: { ...video, visibility: 'BLOCKED' },
          reason: 'hate',
          note: 'menghina suku',
          moderator: { id: '1', username: 'admin', display_name: 'Admin', avatar_url: '' },
          blocked_at: '2026-11-12T00:00:00Z',
        },
      ],
      next_page_token: '',
    })
    unblockVideo.mockResolvedValue({ ...video, visibility: 'UNLISTED' })
    renderPage('/studio/moderation?tab=blocked')
    expect(await screen.findByText('menghina suku')).toBeInTheDocument()
    expect(screen.getByText(/oleh @admin/)).toBeInTheDocument()
    // Tanpa tautan: admin tidak bisa membuka video BLOCKED.
    expect(screen.queryByRole('link', { name: 'Gelato murah' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Pulihkan Gelato murah' }))
    expect(await screen.findByText('"Gelato murah" dipulihkan sebagai Tidak publik.')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByText('Belum ada video yang diturunkan.')).toBeInTheDocument())
    expect(listReportedVideos).not.toHaveBeenCalled()
  })
})
