import { useRef, useState, type FormEvent } from 'react'
import { errorMessage } from '../../api/client'
import { updateVideo } from '../../api/endpoints'
import type { Video } from '../../api/types'
import { detailsChanges } from '../../lib/categories'
import { descriptionError, titleError } from '../../lib/upload'
import { ErrorBox } from '../ErrorBox'
import { Modal } from '../Modal'
import { CategoryTagFields, type TagFieldHandle } from './CategoryTagFields'
import { VideoDetailsFields } from './VideoDetailsFields'

interface Props {
  video: Video
  onClose: () => void
  onSaved: (video: Video) => void
}

/** Dialog edit judul, deskripsi, kategori, dan tag (PATCH /videos/:id). */
export function EditVideoDialog({ video, onClose, onSaved }: Props) {
  const [title, setTitle] = useState(video.title)
  const [description, setDescription] = useState(video.description)
  const [category, setCategory] = useState(video.category)
  const [tags, setTags] = useState(video.tags)
  const tagsRef = useRef<TagFieldHandle>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const invalid = Boolean(titleError(title) || descriptionError(description))

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (invalid) return
    // Tag yang masih diketik ikut disimpan; null = teksnya salah.
    const finalTags = tagsRef.current ? tagsRef.current.flush() : tags
    if (!finalTags) return
    // Hanya field yang berubah yang dikirim (PATCH = ubah sebagian).
    const changes = detailsChanges(video, { title, description, category, tags: finalTags })
    if (Object.keys(changes).length === 0) {
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
        <CategoryTagFields
          idPrefix="edit"
          category={category}
          tags={tags}
          onCategoryChange={setCategory}
          onTagsChange={setTags}
          disabled={busy}
          emptyCategoryLabel="Tanpa kategori"
          tagsRef={tagsRef}
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
