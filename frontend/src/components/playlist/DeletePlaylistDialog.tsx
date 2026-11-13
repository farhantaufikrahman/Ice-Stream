import { useState } from 'react'
import { errorMessage } from '../../api/client'
import { deletePlaylist } from '../../api/endpoints'
import type { Playlist } from '../../api/types'
import { ErrorBox } from '../ErrorBox'
import { Modal } from '../Modal'

interface Props {
  playlist: Playlist
  onClose: () => void
  onDeleted: () => void
}

/** Konfirmasi hapus playlist (DELETE /playlists/:id). */
export function DeletePlaylistDialog({ playlist, onClose, onDeleted }: Props) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const confirm = async () => {
    setBusy(true)
    setError(null)
    try {
      await deletePlaylist(playlist.id)
      onDeleted()
    } catch (err) {
      setError(errorMessage(err))
      setBusy(false)
    }
  }

  return (
    <Modal title="Hapus playlist?" onClose={onClose} locked={busy}>
      <div className="modal-body">
        <p className="modal-video-title">{playlist.title}</p>
        <p className="muted">Playlist ini akan dihapus. Video di dalamnya tidak ikut terhapus. Tindakan ini tidak bisa dibatalkan.</p>
        {error && <ErrorBox message={error} />}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button type="button" className="btn btn-danger" onClick={confirm} disabled={busy}>
            {busy ? 'Menghapus…' : 'Hapus'}
          </button>
        </div>
      </div>
    </Modal>
  )
}
