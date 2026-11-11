import { useState } from 'react'
import { errorMessage } from '../../api/client'
import { deleteVideo } from '../../api/endpoints'
import type { Video } from '../../api/types'
import { ErrorBox } from '../ErrorBox'
import { Modal } from '../Modal'

interface Props {
  video: Video
  onClose: () => void
  onDeleted: (id: string) => void
}

/** Konfirmasi hapus video (DELETE /videos/:id). */
export function DeleteVideoDialog({ video, onClose, onDeleted }: Props) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const confirm = async () => {
    setBusy(true)
    setError(null)
    try {
      // Bisa sampai puluhan detik: server juga menghapus semua file HLS.
      await deleteVideo(video.id)
      onDeleted(video.id)
    } catch (err) {
      setError(errorMessage(err))
      setBusy(false)
    }
  }

  return (
    <Modal title="Hapus video?" onClose={onClose} locked={busy}>
      <div className="modal-body">
        <p className="modal-video-title">{video.title}</p>
        <p className="muted">
          Video beserta komentar, danmaku, dan riwayat tontonnya akan dihapus. Tindakan ini tidak bisa dibatalkan.
        </p>
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
