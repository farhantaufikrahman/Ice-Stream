import { useRef, useState, type FormEvent } from 'react'
import { errorMessage } from '../../api/client'
import { updateVideo } from '../../api/endpoints'
import type { Video } from '../../api/types'
import { detailsChanges } from '../../lib/categories'
import { descriptionError, titleError } from '../../lib/upload'
import { ErrorBox } from '../ErrorBox'
import { Modal } from '../Modal'
import { CategoryTagFields, type TagFieldHandle } from './CategoryTagFields'
import { ThumbnailPicker } from './ThumbnailPicker'
import { VideoDetailsFields } from './VideoDetailsFields'
import { VisibilityFields } from './VisibilityFields'

interface Props {
  video: Video
  onClose: () => void
  onSaved: (video: Video) => void
  /** Thumbnail langsung disimpan server tanpa menutup dialog (langkah 26). */
  onThumbnailChange: (video: Video) => void
}

/**
 * Dialog edit judul, deskripsi, kategori, tag, bahasa, visibilitas, dan
 * thumbnail (PATCH /videos/:id). Untuk draf ada tombol "Simpan & terbitkan".
 */
export function EditVideoDialog({ video: initial, onClose, onSaved, onThumbnailChange }: Props) {
  // Salinan lokal supaya pilihan thumbnail langsung terlihat di dialog.
  const [video, setVideo] = useState(initial)
  const [title, setTitle] = useState(initial.title)
  const [description, setDescription] = useState(initial.description)
  const [category, setCategory] = useState(initial.category)
  const [tags, setTags] = useState(initial.tags)
  const [language, setLanguage] = useState(initial.language)
  const [visibility, setVisibility] = useState(initial.visibility)
  const tagsRef = useRef<TagFieldHandle>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const invalid = Boolean(titleError(title) || descriptionError(description))

  // Terbitkan = draf false. Kategori wajib saat terbit, sama seperti di Unggah.
  const [publishing, setPublishing] = useState(false)
  const save = async (publish: boolean) => {
    setPublishing(publish)
    if (invalid || (publish && !category)) return
    // Tag yang masih diketik ikut disimpan; null = teksnya salah.
    const finalTags = tagsRef.current ? tagsRef.current.flush() : tags
    if (!finalTags) return
    // Hanya field yang berubah yang dikirim (PATCH = ubah sebagian).
    const changes = detailsChanges(video, {
      title,
      description,
      category,
      tags: finalTags,
      language,
      visibility,
      draft: publish ? false : video.draft,
    })
    if (Object.keys(changes).length === 0) {
      if (video !== initial) onSaved(video)
      else onClose()
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

  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    void save(false)
  }

  return (
    <Modal title="Edit video" onClose={onClose} locked={busy}>
      <form className="modal-body" onSubmit={onSubmit} noValidate>
        {video.draft && <p className="muted small">Video ini masih draf: hanya kamu yang bisa melihatnya.</p>}
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
          categoryError={publishing && !category ? 'Pilih kategori sebelum menerbitkan.' : null}
          tagsRef={tagsRef}
          language={language}
          onLanguageChange={setLanguage}
        />
        <ThumbnailPicker
          video={video}
          disabled={busy}
          onChange={(v) => {
            setVideo(v)
            onThumbnailChange(v)
          }}
        />
        <VisibilityFields idPrefix="edit" value={visibility} onChange={setVisibility} disabled={busy} />
        {error && <ErrorBox message={error} />}
        <div className="form-actions">
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button type="submit" className={video.draft ? 'btn' : 'btn btn-primary'} disabled={busy || invalid}>
            {busy && !publishing ? 'Menyimpan…' : 'Simpan'}
          </button>
          {video.draft && (
            <button type="button" className="btn btn-primary" onClick={() => void save(true)} disabled={busy || invalid}>
              {busy && publishing ? 'Menerbitkan…' : 'Simpan & terbitkan'}
            </button>
          )}
        </div>
      </form>
    </Modal>
  )
}
