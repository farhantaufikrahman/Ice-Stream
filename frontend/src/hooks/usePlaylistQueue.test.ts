import { renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Playlist, PlaylistEntry, PlaylistVideoPage, Video } from '../api/types'
import { usePlaylistQueue } from './usePlaylistQueue'
import { ApiError } from '../api/client'

// API dan status login palsu, supaya tes tidak butuh server.
const getPlaylist = vi.fn<(id: string) => Promise<Playlist>>()
const listPlaylistVideos = vi.fn<(id: string, token: string) => Promise<PlaylistVideoPage>>()
vi.mock('../api/endpoints', () => ({
  MAX_PAGE_SIZE: 50,
  getPlaylist: (id: string) => getPlaylist(id),
  listPlaylistVideos: (id: string, token: string) => listPlaylistVideos(id, token),
}))
vi.mock('../auth/useAuth', () => ({ useAuth: () => ({ status: 'authenticated', user: { id: '8' } }) }))

function entry(id: string): PlaylistEntry {
  return { video: { id, title: `Video ${id}` } as Video, added_at: '2026-11-11T08:00:00Z' }
}

const playlist = { id: '5', title: 'Belajar', video_count: 4 } as Playlist

describe('usePlaylistQueue', () => {
  beforeEach(() => {
    getPlaylist.mockReset()
    listPlaylistVideos.mockReset()
    getPlaylist.mockResolvedValue(playlist)
    // Halaman 1: video 1 dan 2. Halaman 2: video 3 dan 4.
    listPlaylistVideos.mockImplementation(async (_id, token) =>
      token === '' ? { entries: [entry('1'), entry('2')], next_page_token: 'p2' } : { entries: [entry('3'), entry('4')], next_page_token: '' },
    )
  })

  it('tanpa ?list= tidak memuat apa pun', () => {
    const { result } = renderHook(() => usePlaylistQueue(null, '1'))
    expect(result.current).toBeNull()
    expect(getPlaylist).not.toHaveBeenCalled()
  })

  it('video di tengah halaman pertama: berikutnya tanpa request tambahan', async () => {
    const { result } = renderHook(() => usePlaylistQueue('5', '1'))
    await waitFor(() => expect(result.current?.playlist).toEqual(playlist))
    expect(result.current?.index).toBe(0)
    expect(result.current?.next?.video.id).toBe('2')
    expect(listPlaylistVideos).toHaveBeenCalledTimes(1)
  })

  it('video terakhir di halaman pertama: halaman berikutnya dimuat untuk mencari video selanjutnya', async () => {
    const { result } = renderHook(() => usePlaylistQueue('5', '2'))
    await waitFor(() => expect(result.current?.next?.video.id).toBe('3'))
    expect(listPlaylistVideos).toHaveBeenLastCalledWith('5', 'p2')
  })

  it('video ada di halaman kedua: dimuat sampai ketemu', async () => {
    const { result } = renderHook(() => usePlaylistQueue('5', '4'))
    await waitFor(() => expect(result.current?.index).toBe(3))
    expect(result.current?.next).toBeNull()
    expect(result.current?.needMore).toBe(false)
  })

  it('pindah ke video berikutnya tidak memuat ulang playlist', async () => {
    const { result, rerender } = renderHook(({ id }) => usePlaylistQueue('5', id), { initialProps: { id: '1' } })
    await waitFor(() => expect(result.current?.index).toBe(0))
    rerender({ id: '2' })
    await waitFor(() => expect(result.current?.next?.video.id).toBe('3'))
    expect(getPlaylist).toHaveBeenCalledTimes(1)
  })

  it('playlist tidak bisa dibuka: error', async () => {
    getPlaylist.mockRejectedValue(new ApiError(404, 'NOT_FOUND', 'playlist tidak ditemukan'))
    const { result } = renderHook(() => usePlaylistQueue('5', '1'))
    await waitFor(() => expect(result.current?.error).toBe('playlist tidak ditemukan'))
  })
})
