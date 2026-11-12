import { useRef, useState } from 'react'
import { errorMessage } from '../../api/client'
import { chooseThumbnailFrame, uploadThumbnail } from '../../api/endpoints'
import type { Video } from '../../api/types'
import { formatDuration } from '../../lib/format'
import { ACCEPT_THUMBNAIL, thumbnailFileError } from '../../lib/visibility'
import { ErrorBox } from '../ErrorBox'

interface Props {
  video: Video
  onChange: (video: Video) => void
  disabled?: boolean
}

/**
 * Pilihan "Thumbnail" dari desain Upload.dc.html: beberapa bingkai buatan
 * transcoder ("Bingkai 00:45") dan "Unggah gambar". Pilihan langsung
 * disimpan ke server. Bingkai baru ada setelah video selesai diproses;
 * gambar sendiri boleh diunggah lebih awal.
 */
export function ThumbnailPicker({ video, onChange, disabled }: Props) {
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const frames = video.thumbnail_frames ?? []
  const locked = disabled || busy !== null

  const run = async (key: string, call: () => Promise<Video>) => {
    setBusy(key)
    setError(null)
    try {
      onChange(await call())
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(null)
    }
  }

  const onFile = (f: File | undefined) => {
    if (!f) return
    const err = thumbnailFileError(f)
    if (err) {
      setError(err)
      return
    }
    void run('upload', () => uploadThumbnail(video.id, f))
  }

  return (
    <fieldset className="choice-group" disabled={locked}>
      <legend>Thumbnail</legend>
      <div className="thumb-picks">
        {frames.map((f) => {
          const label = `Bingkai ${formatDuration(f.position_ms)}`
          const on = f.selected && !video.custom_thumbnail
          return (
            <button
              key={f.position_ms}
              type="button"
              className={on ? 'thumb-pick is-selected' : 'thumb-pick'}
              aria-pressed={on}
              onClick={() => !on && void run(`f${f.position_ms}`, () => chooseThumbnailFrame(video.id, f.position_ms))}
            >
              <img src={f.url} alt="" loading="lazy" />
              <span className="thumb-pick-label">{busy === `f${f.position_ms}` ? 'Menyimpan…' : label}</span>
            </button>
          )
        })}
        <button
          type="button"
          className={video.custom_thumbnail ? 'thumb-pick is-selected' : 'thumb-pick thumb-pick-upload'}
          aria-pressed={video.custom_thumbnail}
          onClick={() => fileRef.current?.click()}
        >
          {video.custom_thumbnail && video.thumbnail_url && <img src={video.thumbnail_url} alt="" />}
          <span className="thumb-pick-label">
            {busy === 'upload' ? 'Mengunggah…' : video.custom_thumbnail ? 'Ganti gambar' : 'Unggah gambar'}
          </span>
        </button>
        <input
          ref={fileRef}
          type="file"
          accept={ACCEPT_THUMBNAIL}
          className="sr-only"
          tabIndex={-1}
          aria-label="Pilih gambar thumbnail"
          onChange={(e) => {
            onFile(e.target.files?.[0])
            e.target.value = ''
          }}
        />
      </div>
      <p className="muted small">
        {frames.length === 0
          ? 'Bingkai dari video muncul setelah video selesai diproses. Gambar sendiri: JPEG, PNG, atau WebP, maksimal 2 MB, dipotong 16:9.'
          : 'Gambar sendiri: JPEG, PNG, atau WebP, maksimal 2 MB, dipotong 16:9.'}
      </p>
      {error && <ErrorBox message={error} />}
    </fieldset>
  )
}
