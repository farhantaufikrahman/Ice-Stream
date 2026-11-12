// Visibilitas, draf, dan bahasa video (langkah 26). Aturannya sama dengan
// VideoService (backend/internal/video/visibility.go).

import type { Video, VideoLanguage, VideoVisibility } from '../api/types'

/** Pilihan di desain Upload.dc.html, urutan sama. */
export const VISIBILITIES: { value: VideoVisibility; label: string; description: string }[] = [
  { value: 'PUBLIC', label: 'Publik', description: 'Semua orang bisa menemukan dan menonton.' },
  { value: 'UNLISTED', label: 'Tidak publik', description: 'Hanya orang yang punya tautan.' },
  { value: 'PRIVATE', label: 'Pribadi', description: 'Hanya kamu.' },
]

/** Pilihan "Bahasa video" di desain Upload, sama dengan CHECK videos_language_ck. */
export const LANGUAGES: { value: Exclude<VideoLanguage, ''>; label: string }[] = [
  { value: 'id', label: 'Bahasa Indonesia' },
  { value: 'en', label: 'English' },
  { value: 'ja', label: '日本語' },
]

export function visibilityLabel(v: VideoVisibility): string {
  return VISIBILITIES.find((x) => x.value === v)?.label ?? 'Publik'
}

/** "ja" -> "日本語". Kosong atau tidak dikenal -> null. */
export function languageLabel(code: string | undefined): string | null {
  return LANGUAGES.find((l) => l.value === code)?.label ?? null
}

/**
 * Tanggal yang ditampilkan untuk sebuah video: waktu terbit, atau waktu
 * diunggah untuk video lama/draf yang belum pernah terbit.
 */
export function videoDate(v: Pick<Video, 'published_at' | 'created_at'>): string {
  return v.published_at ?? v.created_at
}

/** true jika video tampil di beranda, pencarian, Trending, dan kanal. */
export function isListed(v: Pick<Video, 'status' | 'draft' | 'visibility'>): boolean {
  return v.status === 'READY' && !v.draft && v.visibility === 'PUBLIC'
}

/** Batas thumbnail buatan kreator, sama dengan video.MaxThumbnailBytes. */
export const MAX_THUMBNAIL_BYTES = 2 * 1024 * 1024
export const ACCEPT_THUMBNAIL = 'image/jpeg,image/png,image/webp'

/** Pesan salah untuk file thumbnail, atau null jika boleh dikirim. */
export function thumbnailFileError(f: File): string | null {
  if (!ACCEPT_THUMBNAIL.split(',').includes(f.type)) return 'Pilih gambar JPEG, PNG, atau WebP.'
  if (f.size > MAX_THUMBNAIL_BYTES) return 'Gambar maksimal 2 MB.'
  return null
}
