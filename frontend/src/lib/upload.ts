// Aturan dan format untuk halaman Unggah dan Studio. Batasnya sama dengan
// VideoService (backend/internal/video/service.go). Server tetap memeriksa
// ulang; pengecekan di sini hanya supaya user langsung tahu salahnya.

import type { VideoStatus } from '../api/types'

export const MAX_TITLE_CHARS = 100
export const MAX_DESCRIPTION_CHARS = 1000

/**
 * Batas ukuran default di server (MAX_UPLOAD_MB=2048). Jika nilai di server
 * diubah, server tetap menolak dengan pesan yang jelas.
 */
export const MAX_UPLOAD_BYTES = 2048 * 1024 * 1024

/** Tipe file yang diterima server, dengan ekstensi sebagai cadangan. */
const typeByExtension: Record<string, string> = {
  mp4: 'video/mp4',
  m4v: 'video/mp4',
  webm: 'video/webm',
  mov: 'video/quicktime',
  mkv: 'video/x-matroska',
}
const allowedTypes = new Set(Object.values(typeByExtension))

/** Untuk atribut accept di <input type="file">. */
export const ACCEPT_VIDEO = [...allowedTypes, ...Object.keys(typeByExtension).map((ext) => `.${ext}`)].join(',')

export type FileCheck = { ok: true; contentType: string } | { ok: false; message: string }

/**
 * Memeriksa file sebelum diunggah. Beberapa browser mengisi file.type
 * kosong untuk .mkv, jadi ekstensi dipakai sebagai cadangan.
 */
export function checkVideoFile(file: { name: string; size: number; type: string }): FileCheck {
  const ext = file.name.split('.').pop()?.toLowerCase() ?? ''
  const contentType = allowedTypes.has(file.type) ? file.type : typeByExtension[ext]
  if (!contentType) {
    return { ok: false, message: 'Format tidak didukung. Pilih file MP4, WebM, MOV, atau MKV.' }
  }
  if (file.size <= 0) {
    return { ok: false, message: 'File kosong.' }
  }
  if (file.size > MAX_UPLOAD_BYTES) {
    return { ok: false, message: `Ukuran maksimal ${formatBytes(MAX_UPLOAD_BYTES)}.` }
  }
  return { ok: true, contentType }
}

/**
 * Jumlah karakter seperti yang dihitung server (Go: utf8.RuneCountInString).
 * "abc".length di JavaScript menghitung unit UTF-16, jadi emoji terhitung 2.
 */
export function charCount(s: string): number {
  return [...s].length
}

/** "gelato-3_rasa.final.mp4" -> "gelato 3 rasa.final" (judul awal). */
export function titleFromFileName(name: string): string {
  const base = name.replace(/\.[^.]+$/, '').replace(/[_-]+/g, ' ').replace(/\s+/g, ' ').trim()
  const title = [...base].slice(0, MAX_TITLE_CHARS).join('').trim()
  return title || 'Video tanpa judul'
}

/** Pesan error judul, atau null jika valid. */
export function titleError(title: string): string | null {
  const n = charCount(title.trim())
  if (n === 0) return 'Judul wajib diisi.'
  if (n > MAX_TITLE_CHARS) return `Judul maksimal ${MAX_TITLE_CHARS} karakter.`
  return null
}

export function descriptionError(description: string): string | null {
  return charCount(description.trim()) > MAX_DESCRIPTION_CHARS
    ? `Deskripsi maksimal ${MAX_DESCRIPTION_CHARS} karakter.`
    : null
}

const decimal = new Intl.NumberFormat('id-ID', { maximumFractionDigits: 1 })

/** 671088640 -> "640 MB". Memakai 1024 seperti batas MAX_UPLOAD_MB di server. */
export function formatBytes(bytes: number): string {
  const units = ['B', 'KB', 'MB', 'GB']
  let value = bytes
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${decimal.format(i === 0 ? value : Math.round(value * 10) / 10)} ${units[i]}`
}

/** Perkiraan sisa waktu: "sisa sekitar 1 menit". */
export function formatEta(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return ''
  if (seconds < 60) return 'sisa kurang dari 1 menit'
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `sisa sekitar ${minutes} menit`
  return `sisa sekitar ${decimal.format(Math.round((minutes / 60) * 10) / 10)} jam`
}

/** Status yang masih bisa berubah sendiri (perlu dicek ulang berkala). */
export function isProcessing(status: VideoStatus): boolean {
  return status === 'UPLOADED' || status === 'PROCESSING'
}

/** Server hanya mengizinkan hapus saat status ini (deletableStatuses di backend). */
export function canDelete(status: VideoStatus): boolean {
  return status === 'PENDING_UPLOAD' || status === 'READY' || status === 'FAILED'
}
