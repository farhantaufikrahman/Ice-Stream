// Aturan halaman Pengaturan profil. Batasnya sama dengan UserService
// (backend/internal/user/service.go dan avatar.go). Server tetap memeriksa
// ulang; pengecekan di sini hanya supaya user langsung tahu salahnya.

import { charCount, formatBytes } from './upload'

export const MAX_DISPLAY_NAME_CHARS = 50
export const MAX_BIO_CHARS = 500
export const MAX_AVATAR_BYTES = 2 * 1024 * 1024
export const MIN_AVATAR_SIDE = 64

/** Format yang diterima server. Server mengenali format dari isi file, bukan dari nama. */
const avatarTypes = ['image/jpeg', 'image/png', 'image/webp']

/** Untuk atribut accept di <input type="file">. */
export const ACCEPT_AVATAR = [...avatarTypes, '.jpg', '.jpeg', '.png', '.webp'].join(',')

export function displayNameError(name: string): string | null {
  const n = charCount(name.trim())
  if (n === 0) return 'Nama tampilan wajib diisi.'
  if (n > MAX_DISPLAY_NAME_CHARS) return `Maksimal ${MAX_DISPLAY_NAME_CHARS} karakter.`
  return null
}

export function bioError(bio: string): string | null {
  return charCount(bio.trim()) > MAX_BIO_CHARS ? `Maksimal ${MAX_BIO_CHARS} karakter.` : null
}

/**
 * Memeriksa file foto profil sebelum diunggah. Ukuran minimal 64x64 piksel
 * dan isi file yang rusak diperiksa server (pesannya ikut ditampilkan).
 */
export function avatarFileError(file: { size: number; type: string }): string | null {
  if (!avatarTypes.includes(file.type)) return 'Format tidak didukung. Pilih gambar JPEG, PNG, atau WebP.'
  if (file.size <= 0) return 'File kosong.'
  if (file.size > MAX_AVATAR_BYTES) return `Ukuran maksimal ${formatBytes(MAX_AVATAR_BYTES)}.`
  return null
}
