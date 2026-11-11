import { useState, type FormEvent } from 'react'
import { errorMessage } from '../../api/client'
import { createPlaylist, updatePlaylist, type PlaylistInput } from '../../api/endpoints'
import type { Playlist, PlaylistVisibility } from '../../api/types'
import { descriptionError, titleError } from '../../lib/upload'
import { ErrorBox } from '../ErrorBox'
import { Modal } from '../Modal'
import { VideoDetailsFields } from '../studio/VideoDetailsFields'
import { VisibilityField } from './VisibilityField'

interface Props {
  /** Kosong = buat playlist baru; berisi = edit playlist ini. */
  playlist?: Playlist
  onClose: () => void
  onSaved: (playlist: Playlist) => void
}

/** Dialog buat atau edit playlist: judul, deskripsi, dan visibilitas. */
export function PlaylistFormDialog({ playlist, onClose, onSaved }: Props) {
  const [title, setTitle] = useState(playlist?.title ?? '')
  const [description, setDescription] = useState(playlist?.description ?? '')
  const [visibility, setVisibility] = useState<PlaylistVisibility>(playlist?.visibility ?? 'PRIVATE')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const invalid = Boolean(titleError(title) || descriptionError(description))

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (invalid) return
    const input: PlaylistInput = { title: title.trim(), description: description.trim(), visibility }
    // Saat edit, hanya field yang berubah yang dikirim (PATCH).
    let changes: Partial<PlaylistInput> = input
    if (playlist) {
      changes = {}
      if (input.title !== playlist.title) changes.title = input.title
      if (input.description !== playlist.description) changes.description = input.description
      if (input.visibility !== playlist.visibility) changes.visibility = input.visibility
      if (Object.keys(changes).length === 0) {
        onClose()
        return
      }
    }
    setBusy(true)
    setError(null)
    try {
      onSaved(playlist ? await updatePlaylist(playlist.id, changes) : await createPlaylist(input))
    } catch (err) {
      setError(errorMessage(err))
      setBusy(false)
    }
  }

  return (
    <Modal title={playlist ? 'Edit playlist' : 'Playlist baru'} onClose={onClose} locked={busy}>
      <form className="modal-body" onSubmit={onSubmit} noValidate>
        <VideoDetailsFields
          idPrefix="playlist"
          title={title}
          description={description}
          onTitleChange={setTitle}
          onDescriptionChange={setDescription}
          descriptionPlaceholder="Ceritakan isi playlist ini"
          disabled={busy}
        />
        <VisibilityField name="playlist-visibility" value={visibility} onChange={setVisibility} disabled={busy} />
        {error && <ErrorBox message={error} />}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button type="submit" className="btn btn-primary" disabled={busy || invalid}>
            {busy ? 'Menyimpan…' : playlist ? 'Simpan' : 'Buat'}
          </button>
        </div>
      </form>
    </Modal>
  )
}
