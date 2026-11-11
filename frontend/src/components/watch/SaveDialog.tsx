import { useState, type FormEvent } from 'react'
import { errorMessage } from '../../api/client'
import { createPlaylist, listMyPlaylists, setPlaylistVideo, setWatchLater } from '../../api/endpoints'
import type { Playlist, PlaylistVisibility } from '../../api/types'
import { usePagedList } from '../../hooks/usePagedList'
import { titleError } from '../../lib/upload'
import { ErrorBox } from '../ErrorBox'
import { Modal } from '../Modal'
import { LockIcon } from '../playlist/icons'
import { VisibilityField } from '../playlist/VisibilityField'

/** Apakah video sudah disimpan, untuk tampilan tombol Simpan. */
export interface SaveState {
  watchLater: boolean
  inPlaylist: boolean
}

interface Props {
  videoId: string
  initial: SaveState
  /** Dipanggil saat dialog ditutup, hanya jika ada yang berubah. */
  onChange: (state: SaveState) => void
  onClose: () => void
}

/**
 * Dialog "Simpan ke...": centang Tonton nanti atau playlist mana saja.
 * Setiap centang langsung dikirim ke server (seperti YouTube), tanpa
 * tombol Simpan terpisah. Di bawahnya ada form singkat untuk membuat
 * playlist baru yang langsung berisi video ini.
 */
export function SaveDialog({ videoId, initial, onChange, onClose }: Props) {
  const playlists = usePagedList<Playlist>((token, signal) =>
    listMyPlaylists(token, videoId, signal).then((p) => ({ items: p.playlists, next: p.next_page_token })),
  )
  const [watchLater, setWatchLaterState] = useState(initial.watchLater)
  // Id yang sedang dikirim ke server ("later" untuk Tonton nanti), supaya
  // centang yang sama tidak ditekan dua kali sebelum ada jawaban.
  const [pending, setPending] = useState<ReadonlySet<string>>(new Set())
  const [error, setError] = useState<string | null>(null)

  const markPending = (id: string, on: boolean) =>
    setPending((s) => {
      const next = new Set(s)
      if (on) next.add(id)
      else next.delete(id)
      return next
    })

  // true setelah ada centang yang berhasil diubah. Tombol Simpan hanya
  // diperbarui jika ada perubahan, supaya dialog yang ditutup sebelum
  // daftar playlist selesai dimuat tidak mengubah tampilannya.
  const [dirty, setDirty] = useState(false)

  const close = () => {
    if (dirty) {
      // Jika belum semua playlist dimuat (lebih dari 50), nilai awal dari
      // server dipakai untuk playlist yang belum terlihat.
      const inPlaylist = playlists.items.some((p) => p.contains_video) || (playlists.hasMore && initial.inPlaylist)
      onChange({ watchLater, inPlaylist })
    }
    onClose()
  }

  const toggleWatchLater = async (want: boolean) => {
    // Optimistic update, sama seperti tombol Suka: centang langsung
    // berubah, lalu dikembalikan jika server menolak.
    setWatchLaterState(want)
    markPending('later', true)
    setError(null)
    try {
      const r = await setWatchLater(videoId, want)
      setWatchLaterState(r.saved)
      setDirty(true)
    } catch (err) {
      setWatchLaterState(!want)
      setError(errorMessage(err))
    } finally {
      markPending('later', false)
    }
  }

  const togglePlaylist = async (playlistId: string, want: boolean) => {
    // Bentuk fungsi (items) => ..., supaya dua centang yang dikirim
    // bersamaan tidak saling menimpa.
    const apply = (contains: boolean) =>
      playlists.update((items) => items.map((p) => (p.id === playlistId ? { ...p, contains_video: contains } : p)))
    apply(want)
    markPending(playlistId, true)
    setError(null)
    try {
      const r = await setPlaylistVideo(playlistId, videoId, want)
      apply(r.saved)
      setDirty(true)
    } catch (err) {
      apply(!want)
      setError(errorMessage(err))
    } finally {
      markPending(playlistId, false)
    }
  }

  const onCreated = (playlist: Playlist) => {
    playlists.update((items) => [playlist, ...items])
    if (playlist.contains_video) setDirty(true)
  }

  return (
    <Modal title="Simpan ke..." onClose={close}>
      <div className="modal-body">
        <ul className="save-list">
          <li>
            <label className="save-option">
              <input
                type="checkbox"
                checked={watchLater}
                disabled={pending.has('later')}
                onChange={(e) => toggleWatchLater(e.target.checked)}
              />
              <span className="save-option-title">Tonton nanti</span>
              <span className="muted small">
                <LockIcon /> Privat
              </span>
            </label>
          </li>
          {playlists.items.map((p) => (
            <li key={p.id}>
              <label className="save-option">
                <input
                  type="checkbox"
                  checked={p.contains_video ?? false}
                  disabled={pending.has(p.id)}
                  onChange={(e) => togglePlaylist(p.id, e.target.checked)}
                />
                <span className="save-option-title">{p.title}</span>
                {p.visibility === 'PRIVATE' ? (
                  <span className="muted small">
                    <LockIcon /> Privat
                  </span>
                ) : (
                  <span className="muted small">Publik</span>
                )}
              </label>
            </li>
          ))}
        </ul>
        {playlists.loading && <p className="muted small">Memuat playlist…</p>}
        {playlists.error && <ErrorBox message={playlists.error} onRetry={playlists.retry} />}
        {!playlists.loading && !playlists.error && playlists.hasMore && (
          <button type="button" className="btn btn-small load-more" onClick={playlists.loadMore}>
            Muat playlist lainnya
          </button>
        )}
        {error && <ErrorBox message={error} />}

        <NewPlaylistForm videoId={videoId} onCreated={onCreated} />

        <div className="form-actions">
          <button type="button" className="btn" onClick={close}>
            Selesai
          </button>
        </div>
      </div>
    </Modal>
  )
}

/**
 * Tombol "Playlist baru" yang berubah menjadi form singkat (judul dan
 * visibilitas). Playlist baru langsung berisi video ini.
 */
function NewPlaylistForm({ videoId, onCreated }: { videoId: string; onCreated: (p: Playlist) => void }) {
  const [open, setOpen] = useState(false)
  const [title, setTitle] = useState('')
  const [visibility, setVisibility] = useState<PlaylistVisibility>('PRIVATE')
  const [touched, setTouched] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const tErr = titleError(title)

  if (!open) {
    return (
      <button type="button" className="btn btn-ghost save-new" onClick={() => setOpen(true)}>
        <PlusIcon /> Playlist baru
      </button>
    )
  }

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setTouched(true)
    if (tErr) return
    setBusy(true)
    setError(null)
    let created: Playlist | null = null
    try {
      created = await createPlaylist({ title: title.trim(), description: '', visibility })
      await setPlaylistVideo(created.id, videoId, true)
      onCreated({ ...created, contains_video: true })
      setOpen(false)
      setTitle('')
      setTouched(false)
    } catch (err) {
      // Playlist sudah dibuat tapi videonya gagal ditambahkan: tetap
      // tampilkan playlist-nya (tanpa centang) supaya bisa dicoba lagi.
      if (created) {
        onCreated({ ...created, contains_video: false })
        setOpen(false)
      }
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="save-new-form" onSubmit={onSubmit} noValidate>
      <div className="field">
        <label htmlFor="new-playlist-title">Judul playlist</label>
        <input
          id="new-playlist-title"
          type="text"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          disabled={busy}
          autoFocus
          aria-invalid={touched && tErr ? true : undefined}
          aria-describedby={touched && tErr ? 'new-playlist-title-err' : undefined}
        />
        {touched && tErr && (
          <span id="new-playlist-title-err" className="field-error">
            {tErr}
          </span>
        )}
      </div>
      <VisibilityField name="new-playlist-visibility" value={visibility} onChange={setVisibility} disabled={busy} />
      {error && <ErrorBox message={error} />}
      <div className="form-actions">
        <button type="button" className="btn" onClick={() => setOpen(false)} disabled={busy}>
          Batal
        </button>
        <button type="submit" className="btn btn-primary" disabled={busy}>
          {busy ? 'Membuat…' : 'Buat'}
        </button>
      </div>
    </form>
  )
}

function PlusIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" aria-hidden="true">
      <path d="M12 5v14M5 12h14" />
    </svg>
  )
}
