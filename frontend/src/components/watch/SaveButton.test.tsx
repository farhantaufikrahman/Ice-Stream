import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import type { Playlist, PlaylistPage, PlaylistVideoResult, WatchLaterResult } from '../../api/types'
import { SaveButton } from './SaveButton'

// API palsu, supaya tes tidak butuh server.
const setWatchLater = vi.fn<(id: string, saved: boolean) => Promise<WatchLaterResult>>()
const listMyPlaylists = vi.fn<(token: string, videoId?: string) => Promise<PlaylistPage>>()
const setPlaylistVideo = vi.fn<(playlistId: string, videoId: string, saved: boolean) => Promise<PlaylistVideoResult>>()
const createPlaylist = vi.fn<(input: { title: string; visibility: string }) => Promise<Playlist>>()
vi.mock('../../api/endpoints', () => ({
  setWatchLater: (id: string, saved: boolean) => setWatchLater(id, saved),
  listMyPlaylists: (token: string, videoId?: string) => listMyPlaylists(token, videoId),
  setPlaylistVideo: (p: string, v: string, saved: boolean) => setPlaylistVideo(p, v, saved),
  createPlaylist: (input: { title: string; visibility: string }) => createPlaylist(input),
}))

function playlist(id: string, title: string, contains: boolean): Playlist {
  return {
    id,
    owner_id: '8',
    title,
    description: '',
    visibility: 'PRIVATE',
    video_count: contains ? 1 : 0,
    thumbnail_url: '',
    created_at: '2026-11-11T08:00:00Z',
    updated_at: '2026-11-11T08:00:00Z',
    contains_video: contains,
  }
}

function renderButton(props: Partial<Parameters<typeof SaveButton>[0]> = {}) {
  render(
    <MemoryRouter initialEntries={['/watch/42']}>
      <SaveButton videoId="42" saved={{ watchLater: false, inPlaylist: false }} loggedIn {...props} />
    </MemoryRouter>,
  )
}

/** Klik tombol Simpan, tunggu daftar playlist dimuat, kembalikan isi dialog. */
async function openDialog(buttonName = 'Simpan') {
  await act(async () => fireEvent.click(screen.getByRole('button', { name: buttonName })))
  return within(screen.getByRole('dialog', { name: 'Simpan ke...' }))
}

describe('SaveButton', () => {
  // Pakai blok { }: fungsi yang dikembalikan beforeEach dianggap Vitest
  // sebagai fungsi pembersih dan dipanggil setelah tes, dan mockReset()
  // mengembalikan mock-nya sendiri (sebuah fungsi).
  beforeEach(() => {
    setWatchLater.mockReset()
    setPlaylistVideo.mockReset()
    createPlaylist.mockReset()
    listMyPlaylists.mockReset()
    listMyPlaylists.mockResolvedValue({ playlists: [playlist('5', 'Belajar Go', false), playlist('6', 'Musik', true)], next_page_token: '' })
  })

  it('tamu diarahkan ke halaman Masuk lalu kembali ke video', () => {
    renderButton({ loggedIn: false })
    expect(screen.getByRole('link', { name: 'Simpan' })).toHaveAttribute('href', '/login?next=%2Fwatch%2F42')
  })

  it('tampil "Tersimpan" jika video ada di Tonton nanti atau playlist', () => {
    renderButton({ saved: { watchLater: false, inPlaylist: true } })
    expect(screen.getByRole('button', { name: 'Tersimpan' })).toBeInTheDocument()
  })

  it('dialog menampilkan Tonton nanti dan playlist beserta centangnya', async () => {
    renderButton()
    const dialog = await openDialog()
    expect(listMyPlaylists).toHaveBeenCalledWith('', '42')
    expect(dialog.getByRole('checkbox', { name: /Tonton nanti/ })).not.toBeChecked()
    expect(dialog.getByRole('checkbox', { name: /Belajar Go/ })).not.toBeChecked()
    expect(dialog.getByRole('checkbox', { name: /Musik/ })).toBeChecked()
  })

  it('menyimpan ke Tonton nanti, tombol berubah setelah dialog ditutup', async () => {
    setWatchLater.mockResolvedValue({ saved: true, added_at: '2026-11-11T08:00:00Z' })
    renderButton()
    const dialog = await openDialog()
    await act(async () => fireEvent.click(dialog.getByRole('checkbox', { name: /Tonton nanti/ })))
    expect(setWatchLater).toHaveBeenCalledWith('42', true)
    expect(dialog.getByRole('checkbox', { name: /Tonton nanti/ })).toBeChecked()

    fireEvent.click(dialog.getByRole('button', { name: 'Selesai' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Tersimpan' })).toBeInTheDocument()
  })

  it('mengeluarkan dari satu-satunya playlist membuat tombol kembali "Simpan"', async () => {
    setPlaylistVideo.mockResolvedValue({ saved: false })
    renderButton({ saved: { watchLater: false, inPlaylist: true } })
    const dialog = await openDialog('Tersimpan')
    await act(async () => fireEvent.click(dialog.getByRole('checkbox', { name: /Musik/ })))
    expect(setPlaylistVideo).toHaveBeenCalledWith('6', '42', false)
    fireEvent.click(dialog.getByRole('button', { name: 'Selesai' }))
    expect(screen.getByRole('button', { name: 'Simpan' })).toBeInTheDocument()
  })

  it('jika gagal, centang kembali seperti semula dan menampilkan pesan', async () => {
    setPlaylistVideo.mockRejectedValue(new ApiError(404, 'NOT_FOUND', 'playlist tidak ditemukan'))
    renderButton()
    const dialog = await openDialog()
    await act(async () => fireEvent.click(dialog.getByRole('checkbox', { name: /Belajar Go/ })))
    expect(dialog.getByRole('checkbox', { name: /Belajar Go/ })).not.toBeChecked()
    expect(dialog.getByRole('alert')).toHaveTextContent('playlist tidak ditemukan')
  })

  it('membuat playlist baru yang langsung berisi video ini', async () => {
    createPlaylist.mockResolvedValue({ ...playlist('9', 'Nanti malam', false), contains_video: undefined })
    setPlaylistVideo.mockResolvedValue({ saved: true, added_at: '2026-11-11T08:00:00Z' })
    renderButton()
    const dialog = await openDialog()
    fireEvent.click(dialog.getByRole('button', { name: /Playlist baru/ }))
    fireEvent.change(dialog.getByLabelText('Judul playlist'), { target: { value: '  Nanti malam ' } })
    fireEvent.click(dialog.getByRole('radio', { name: /Publik/ }))
    await act(async () => fireEvent.click(dialog.getByRole('button', { name: 'Buat' })))

    expect(createPlaylist).toHaveBeenCalledWith({ title: 'Nanti malam', description: '', visibility: 'PUBLIC' })
    expect(setPlaylistVideo).toHaveBeenCalledWith('9', '42', true)
    expect(dialog.getByRole('checkbox', { name: /Nanti malam/ })).toBeChecked()
  })

  it('judul playlist baru wajib diisi', async () => {
    renderButton()
    const dialog = await openDialog()
    fireEvent.click(dialog.getByRole('button', { name: /Playlist baru/ }))
    await act(async () => fireEvent.click(dialog.getByRole('button', { name: 'Buat' })))
    expect(createPlaylist).not.toHaveBeenCalled()
    expect(dialog.getByText('Judul wajib diisi.')).toBeInTheDocument()
  })
})
