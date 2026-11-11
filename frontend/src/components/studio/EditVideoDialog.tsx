import { useState, type FormEvent } from 'react'
import { errorMessage } from '../../api/client'
import { updateVideo } from '../../api/endpoints'
import type { Video } from '../../api/types'
import { descriptionError, titleError } from '../../lib/upload'
import { ErrorBox } from '../ErrorBox'
import { Modal } from '../Modal'
import { VideoDetailsFields } from './VideoDetailsFields'

interface Props {
  video: Video
  onClose: () => void
  onSaved: (video: Video) => void
}

/** Dialog edit judul dan deskripsi (PATCH /videos/:id). */
export function EditVideoDialog({ video, onClose, onSaved }: Props) {
  const [title, setTitle] = useState(video.title)
  const [description, setDescription] = useState(video.description)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const invalid = Boolean(titleError(title) || descriptionError(description))

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (invalid) return
    // Hanya field yang berubah yang dikirim (PATCH = ubah sebagian).
    const changes: { title?: string; description?: string } = {}
    if (title.trim() !== video.title) changes.title = title.trim()
    if (description.trim() !== video.description) changes.description = description.trim()
    if (changes.title === undefined && changes.description === undefined) {
      onClose()
      return
    }
    setBusy(true)
    setError(null)
    try {
      onSaved(await updateVideo(video.id, changes))
    } catch (err) {
      setError(errorMessage(err))
      setBusy(false)
    }
  }

  return (
    <Modal title="Edit video" onClose={onClose} locked={busy}>
      <form className="modal-body" onSubmit={onSubmit} noValidate>
        <VideoDetailsFields
          idPrefix="edit"
          title={title}
          description={description}
          onTitleChange={setTitle}
          onDescriptionChange={setDescription}
          disabled={busy}
        />
        {error && <ErrorBox message={error} />}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button type="submit" className="btn btn-primary" disabled={busy || invalid}>
            {busy ? 'Menyimpan…' : 'Simpan'}
          </button>
        </div>
      </form>
    </Modal>
  )
}
