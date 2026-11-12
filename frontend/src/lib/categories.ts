// Kategori dan tag video (langkah 23). Aturannya sama dengan VideoService
// (backend/internal/video/category.go). Server tetap memeriksa ulang;
// pengecekan di sini hanya supaya kreator langsung tahu salahnya.

import type { VideoChanges } from '../api/endpoints'
import type { VideoLanguage, VideoVisibility } from '../api/types'

/** Urutan sama dengan chip di beranda (desain Home.dc.html). */
export const CATEGORIES = [
  { value: 'anime', label: 'Anime' },
  { value: 'game', label: 'Game' },
  { value: 'musik', label: 'Musik' },
  { value: 'teknologi', label: 'Teknologi' },
  { value: 'kuliner', label: 'Kuliner' },
  { value: 'vlog', label: 'Vlog' },
  { value: 'edukasi', label: 'Edukasi' },
  { value: 'olahraga', label: 'Olahraga' },
] as const

export type Category = (typeof CATEGORIES)[number]['value']

const labels = new Map<string, string>(CATEGORIES.map((c) => [c.value, c.label]))

/** "game" -> "Game". Kategori yang tidak dikenal (atau kosong) -> null. */
export function categoryLabel(value: string | null | undefined): string | null {
  return (value && labels.get(value)) || null
}

/** true jika value adalah kategori yang dikenal, misalnya dari ?category= di URL. */
export function isCategory(value: string | null | undefined): value is Category {
  return Boolean(value && labels.has(value))
}

export const MAX_TAGS = 10
export const MIN_TAG_CHARS = 2
export const MAX_TAG_CHARS = 30

/**
 * Huruf (L), angka (Nd), dan tanda aksen (Mn), sama dengan unicode.IsLetter,
 * unicode.IsDigit, dan unicode.Mn di Go.
 */
const TAG_PATTERN = /^[\p{L}\p{Nd}\p{Mn}]+$/u

/** "#esKrim " -> "esKrim". NFC supaya "é" yang diketik dua cara dianggap sama. */
export function normalizeTag(raw: string): string {
  return raw.trim().replace(/^#/, '').normalize('NFC')
}

/** Pesan salah untuk satu tag (sudah dinormalkan), atau null jika benar. */
export function tagError(tag: string): string | null {
  const n = [...tag].length
  if (!TAG_PATTERN.test(tag) || n < MIN_TAG_CHARS || n > MAX_TAG_CHARS) {
    return `Tag "${tag}" harus ${MIN_TAG_CHARS}-${MAX_TAG_CHARS} huruf atau angka, tanpa spasi dan tanda baca.`
  }
  return null
}

export type AddTagsResult = { ok: true; tags: string[] } | { ok: false; message: string }

/**
 * Menambah tag dari teks yang diketik atau ditempel. Spasi, koma, dan "#"
 * memisahkan tag, jadi "#esKrim, anakKos" menjadi dua tag. Tag yang sudah
 * ada (tanpa melihat huruf besar/kecil) dilewati, sama seperti di server.
 */
export function addTags(current: string[], input: string): AddTagsResult {
  const seen = new Set(current.map((t) => t.toLowerCase()))
  const next = [...current]
  for (const piece of input.split(/[\s,#]+/)) {
    const tag = normalizeTag(piece)
    if (!tag) continue
    const err = tagError(tag)
    if (err) return { ok: false, message: err }
    const key = tag.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    next.push(tag)
  }
  if (next.length > MAX_TAGS) {
    return { ok: false, message: `Maksimal ${MAX_TAGS} tag.` }
  }
  return { ok: true, tags: next }
}

/** Untuk membandingkan daftar tag lama dan baru di form edit. */
export function sameTags(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((t, i) => t === b[i])
}

/** Isi form detail video (Unggah dan Edit video). */
export interface DetailsForm {
  title: string
  description: string
  category: string
  tags: string[]
  /** Langkah 26. Tidak diisi = tidak dibandingkan (form tanpa pilihan ini). */
  visibility?: VideoVisibility
  language?: VideoLanguage
  draft?: boolean
}

/**
 * Hanya field yang berubah dibanding video yang tersimpan (PATCH = ubah
 * sebagian). Objek kosong berarti tidak ada yang perlu dikirim.
 */
export function detailsChanges(saved: DetailsForm, form: DetailsForm): VideoChanges {
  const changes: VideoChanges = {}
  if (form.title.trim() !== saved.title) changes.title = form.title.trim()
  if (form.description.trim() !== saved.description) changes.description = form.description.trim()
  if (form.category !== saved.category) changes.category = form.category
  if (!sameTags(form.tags, saved.tags)) changes.tags = form.tags
  if (form.visibility !== undefined && form.visibility !== saved.visibility) changes.visibility = form.visibility
  if (form.language !== undefined && form.language !== saved.language) changes.language = form.language
  if (form.draft !== undefined && form.draft !== saved.draft) changes.draft = form.draft
  return changes
}
