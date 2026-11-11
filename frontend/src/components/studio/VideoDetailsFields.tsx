import { charCount, descriptionError, MAX_DESCRIPTION_CHARS, MAX_TITLE_CHARS, titleError } from '../../lib/upload'

interface Props {
  /** Awalan id input, supaya <label htmlFor> tetap unik di satu halaman. */
  idPrefix: string
  title: string
  description: string
  onTitleChange: (value: string) => void
  onDescriptionChange: (value: string) => void
  disabled?: boolean
}

/** Input judul dan deskripsi beserta penghitung karakter. Dipakai di Unggah dan Edit. */
export function VideoDetailsFields({ idPrefix, title, description, onTitleChange, onDescriptionChange, disabled }: Props) {
  const tErr = titleError(title)
  const dErr = descriptionError(description)
  const titleCount = charCount(title.trim())
  const descCount = charCount(description.trim())
  return (
    <>
      <div className="field">
        <div className="field-head">
          <label htmlFor={`${idPrefix}-title`}>Judul</label>
          <span className={titleCount > MAX_TITLE_CHARS ? 'counter is-over' : 'counter'}>
            {titleCount}/{MAX_TITLE_CHARS}
          </span>
        </div>
        <input
          id={`${idPrefix}-title`}
          type="text"
          value={title}
          onChange={(e) => onTitleChange(e.target.value)}
          disabled={disabled}
          required
          aria-invalid={tErr ? true : undefined}
          aria-describedby={tErr ? `${idPrefix}-title-err` : undefined}
        />
        {tErr && (
          <span id={`${idPrefix}-title-err`} className="field-error">
            {tErr}
          </span>
        )}
      </div>
      <div className="field">
        <div className="field-head">
          <label htmlFor={`${idPrefix}-desc`}>Deskripsi</label>
          <span className={descCount > MAX_DESCRIPTION_CHARS ? 'counter is-over' : 'counter'}>
            {descCount}/{MAX_DESCRIPTION_CHARS}
          </span>
        </div>
        <textarea
          id={`${idPrefix}-desc`}
          rows={5}
          value={description}
          onChange={(e) => onDescriptionChange(e.target.value)}
          disabled={disabled}
          placeholder="Ceritakan isi videomu"
          aria-invalid={dErr ? true : undefined}
          aria-describedby={dErr ? `${idPrefix}-desc-err` : undefined}
        />
        {dErr && (
          <span id={`${idPrefix}-desc-err`} className="field-error">
            {dErr}
          </span>
        )}
      </div>
    </>
  )
}
