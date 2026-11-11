import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { PlaylistInput } from '../../api/endpoints'
import type { Playlist } from '../../api/types'
import { PlaylistFormDialog } from './PlaylistFormDialog'

// API palsu, supaya tes tidak butuh server.
const createPlaylist = vi.fn<(input: PlaylistInput) => Promise<Playlist>>()
const updatePlaylist = vi.fn<(id: string, changes: Partial<PlaylistInput>) => Promise<Playlist>>()
vi.mock('../../api/endpoints', () => ({
  createPlaylist: (input: PlaylistInput) => createPlaylist(input),
  updatePlaylist: (id: string, changes: Partial<PlaylistInput>) => updatePlaylist(id, changes),
}))

const existing: Playlist = {
  id: '5',
  owner_id: '8',
  title: 'Belajar Go',
  description: 'dasar',
  visibility: 'PRIVATE',
  video_count: 2,
  thumbnail_url: '',
  created_at: '2026-11-11T08:00:00Z',
  updated_at: '2026-11-11T08:00:00Z',
}

describe('PlaylistFormDialog', () => {
  beforeEach(() => {
    createPlaylist.mockReset()
    updatePlaylist.mockReset()
  })

  it('membuat playlist baru, bawaannya privat', async () => {
    createPlaylist.mockResolvedValue(existing)
    const onSaved = vi.fn()
    render(<PlaylistFormDialog onClose={() => {}} onSaved={onSaved} />)
    expect(screen.getByRole('dialog', { name: 'Playlist baru' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Buat' })).toBeDisabled()

    fireEvent.change(screen.getByLabelText('Judul'), { target: { value: ' Belajar Go ' } })
    fireEvent.change(screen.getByLabelText('Deskripsi'), { target: { value: 'dasar ' } })
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Buat' })))
    expect(createPlaylist).toHaveBeenCalledWith({ title: 'Belajar Go', description: 'dasar', visibility: 'PRIVATE' })
    expect(onSaved).toHaveBeenCalledWith(existing)
  })

  it('saat edit hanya mengirim field yang berubah', async () => {
    updatePlaylist.mockResolvedValue({ ...existing, visibility: 'PUBLIC' })
    render(<PlaylistFormDialog playlist={existing} onClose={() => {}} onSaved={() => {}} />)
    fireEvent.click(screen.getByRole('radio', { name: /Publik/ }))
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Simpan' })))
    expect(updatePlaylist).toHaveBeenCalledWith('5', { visibility: 'PUBLIC' })
  })

  it('tanpa perubahan, dialog langsung ditutup tanpa request', async () => {
    const onClose = vi.fn()
    render(<PlaylistFormDialog playlist={existing} onClose={onClose} onSaved={() => {}} />)
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Simpan' })))
    expect(updatePlaylist).not.toHaveBeenCalled()
    expect(onClose).toHaveBeenCalled()
  })
})
