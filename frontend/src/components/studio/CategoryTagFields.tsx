import { useImperativeHandle, useState, type ClipboardEvent, type KeyboardEvent, type Ref } from 'react'
import { addTags, CATEGORIES, MAX_TAGS } from '../../lib/categories'

interface Props {
  /** Awalan id input, supaya <label htmlFor> tetap unik di satu halaman. */
  idPrefix: string
  category: string
  tags: string[]
  onCategoryChange: (value: string) => void
  onTagsChange: (tags: string[]) => void
  disabled?: boolean
  /** Teks pilihan kosong: "Pilih kategori" (unggah) atau "Tanpa kategori" (edit). */
  emptyCategoryLabel: string
  /** Pesan salah untuk kategori, misalnya "Pilih kategori." saat Terbitkan. */
  categoryError?: string | null
  /** Untuk mengambil tag terakhir saat form dikirim (lihat TagFieldHandle). */
  tagsRef?: Ref<TagFieldHandle>
}

/**
 * Tag yang masih diketik (belum Enter) ikut disimpan saat form dikirim:
 * form memanggil flush() lebih dulu. Sengaja tidak memakai onBlur: tag baru
 * bisa membuat kotaknya bertambah tinggi, tombol Simpan bergeser saat
 * mouse ditekan, dan klik-nya meleset.
 */
export interface TagFieldHandle {
  /** Daftar tag termasuk teks yang sedang diketik, atau null jika teks itu salah (pesannya ditampilkan). */
  flush: () => string[] | null
}

/**
 * Kategori (select) dan tag (chip + input) untuk halaman Unggah dan dialog
 * Edit video, mengikuti desain Upload.dc.html. "Bahasa video" di desain
 * belum ditampilkan karena backend belum menyimpannya.
 */
export function CategoryTagFields({
  idPrefix,
  category,
  tags,
  onCategoryChange,
  onTagsChange,
  disabled,
  emptyCategoryLabel,
  categoryError,
  tagsRef,
}: Props) {
  return (
    <div className="field-row">
      <div className="field">
        <label htmlFor={`${idPrefix}-cat`}>Kategori</label>
        <select
          id={`${idPrefix}-cat`}
          value={category}
          onChange={(e) => onCategoryChange(e.target.value)}
          disabled={disabled}
          aria-invalid={categoryError ? true : undefined}
          aria-describedby={categoryError ? `${idPrefix}-cat-err` : undefined}
        >
          <option value="">{emptyCategoryLabel}</option>
          {CATEGORIES.map((c) => (
            <option key={c.value} value={c.value}>
              {c.label}
            </option>
          ))}
        </select>
        {categoryError && (
          <span id={`${idPrefix}-cat-err`} className="field-error">
            {categoryError}
          </span>
        )}
      </div>
      <TagInput idPrefix={idPrefix} tags={tags} onChange={onTagsChange} disabled={disabled} handleRef={tagsRef} />
    </div>
  )
}

/** Tombol yang menutup satu tag: Enter, koma, atau spasi. */
const COMMIT_KEYS = new Set(['Enter', ',', ' '])

function TagInput({
  idPrefix,
  tags,
  onChange,
  disabled,
  handleRef,
}: {
  idPrefix: string
  tags: string[]
  onChange: (tags: string[]) => void
  disabled?: boolean
  handleRef?: Ref<TagFieldHandle>
}) {
  const [draft, setDraft] = useState('')
  const [error, setError] = useState<string | null>(null)
  const inputId = `${idPrefix}-tags`

  /**
   * Menambah tag dari teks dan mengembalikan daftar barunya. Jika salah,
   * teksnya tetap di kotak supaya bisa diperbaiki, dan hasilnya null.
   */
  const commit = (text: string): string[] | null => {
    if (!text.trim()) {
      setDraft('')
      return tags
    }
    const result = addTags(tags, text)
    if (!result.ok) {
      setDraft(text)
      setError(result.message)
      return null
    }
    setError(null)
    setDraft('')
    onChange(result.tags)
    return result.tags
  }

  // https://react.dev/reference/react/useImperativeHandle
  useImperativeHandle(handleRef, () => ({ flush: () => commit(draft) }))

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    // isComposing: jangan memotong huruf Jepang/Mandarin yang sedang
    // diketik lewat IME. https://developer.mozilla.org/en-US/docs/Web/API/KeyboardEvent/isComposing
    if (e.nativeEvent.isComposing) return
    if (COMMIT_KEYS.has(e.key)) {
      // Enter tidak boleh mengirim form, koma dan spasi tidak ikut diketik.
      e.preventDefault()
      commit(draft)
    } else if (e.key === 'Backspace' && draft === '' && tags.length > 0) {
      onChange(tags.slice(0, -1))
      setError(null)
    }
  }

  const onPaste = (e: ClipboardEvent<HTMLInputElement>) => {
    // "#esKrim #anakKos" yang ditempel langsung menjadi dua tag.
    const text = e.clipboardData.getData('text')
    if (/[\s,#]/.test(text.trim())) {
      e.preventDefault()
      commit(draft + text)
    }
  }

  const remove = (tag: string) => {
    onChange(tags.filter((t) => t !== tag))
    setError(null)
  }

  return (
    <div className="field">
      <div className="field-head">
        <label htmlFor={inputId}>Tag</label>
        <span className="counter">
          {tags.length}/{MAX_TAGS}
        </span>
      </div>
      <div className={disabled ? 'tag-input is-disabled' : 'tag-input'}>
        {tags.map((t) => (
          <span key={t} className="tag-chip">
            #{t}
            <button type="button" onClick={() => remove(t)} disabled={disabled} aria-label={`Hapus tag ${t}`}>
              ×
            </button>
          </span>
        ))}
        <input
          id={inputId}
          type="text"
          value={draft}
          onChange={(e) => {
            setDraft(e.target.value)
            setError(null)
          }}
          onKeyDown={onKeyDown}
          onPaste={onPaste}
          disabled={disabled || tags.length >= MAX_TAGS}
          placeholder={tags.length >= MAX_TAGS ? 'Sudah 10 tag' : 'Tambah tag'}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? `${inputId}-err` : `${inputId}-hint`}
          autoComplete="off"
          enterKeyHint="done"
        />
      </div>
      {error ? (
        <span id={`${inputId}-err`} className="field-error">
          {error}
        </span>
      ) : (
        <span id={`${inputId}-hint`} className="muted small">
          Tekan Enter, spasi, atau koma untuk menambah tag.
        </span>
      )}
    </div>
  )
}
